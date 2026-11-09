// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package api is goarmin's HTTP API: accounts (first-run setup, passwords, passkeys, invites,
// sessions) and the signed-in user's own health data. Every data route reads the user from the
// session and passes it to the store, so one account can never see another's data; admins
// manage accounts but get no health data of others either.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/karamble/goarmin/internal/importer"
	"github.com/karamble/goarmin/internal/nodes"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/auth"
	"github.com/karamble/goarmin/pkg/blobstore"
)

const (
	sessionCookie  = "goarmin_session"
	ceremonyCookie = "goarmin_passkey"
	sessionTTL     = 30 * 24 * time.Hour
	inviteTTL      = 7 * 24 * time.Hour
	ceremonyTTL    = 5 * time.Minute
	maxBody        = 1 << 20
)

// Options configures the API.
type Options struct {
	Store *store.Store
	Blobs *blobstore.Store
	// PublicURL is where users reach goarmin, e.g. https://health.example.org or
	// http://localhost:8642. Passkeys are bound to its host; https makes cookies Secure.
	PublicURL string
	// SetupToken, if set, lets whoever presents it create the first admin while no account can
	// log in. goarmin serve prints it at startup.
	SetupToken string
	// UI serves everything outside /api (the embedded dashboard).
	UI http.Handler
	// Version is shown in the dashboard.
	Version string
	// Tiles serves map tiles under /tiles/ (tilecache); nil without a tile cache.
	Tiles http.Handler
	// TilesDark: the tile style is dark already (the dashboard puts no dark filter over it).
	TilesDark bool
	// TilesAttribution is the map's credit, HTML, shown on every map.
	TilesAttribution string
	// Node is the sync role: the registry of sync nodes (with the in-process hub of serve
	// --with-sync among them), or nil when the server syncs nothing.
	Node Node
	// Nodes, if set, serves the sync node API (nodeproto) and the Sync nodes tab; it is usually
	// Node as well.
	Nodes *nodes.Registry
	// Imports runs uploads and USB imports; nil: the server does not import.
	Imports *importer.Runner
	// ImportMax and ImportMaxFiles bound one upload (8 GiB, 100,000 files).
	ImportMax      int64
	ImportMaxFiles int
	Logger         *slog.Logger
}

// Server is the API.
type Server struct {
	opt    Options
	st     *store.Store
	blobs  *blobstore.Store
	log    *slog.Logger
	public *url.URL
	wa     *webauthn.WebAuthn
	login  *auth.Limiter // per address and account
	ipOnly *auth.Limiter // per address

	mu         sync.Mutex
	ceremonies map[string]ceremony
}

// New builds the API.
func New(opt Options) (*Server, error) {
	u, err := url.Parse(opt.PublicURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("api: the public URL must be http(s)://host[:port]")
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          u.Hostname(),
		RPDisplayName: "goarmin",
		RPOrigins:     []string{u.Scheme + "://" + u.Host},
	})
	if err != nil {
		return nil, err
	}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{opt: opt, st: opt.Store, blobs: opt.Blobs, log: opt.Logger, public: u, wa: wa,
		login: auth.NewLimiter(10, 15*time.Minute), ipOnly: auth.NewLimiter(50, 15*time.Minute),
		ceremonies: map[string]ceremony{}}, nil
}

// Handler returns the whole site: /api routes and, for everything else, the UI.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.routes(mux)
	if s.opt.UI != nil {
		mux.Handle("/", s.opt.UI)
	}
	return securityHeaders(gzipped(s.withSession(mux)))
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"ok": true}) })
	// /healthz is for container health checks and uptime probes: no session needed, no database
	// touched, a constant body, and never logged.
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /api/setup", s.setupStatus)
	mux.HandleFunc("POST /api/setup", s.setup)
	mux.HandleFunc("POST /api/auth/login", s.passwordLogin)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("POST /api/auth/passkey/login/begin", s.passkeyLoginBegin)
	mux.HandleFunc("POST /api/auth/passkey/login/finish", s.passkeyLoginFinish)
	mux.HandleFunc("GET /api/invites/{token}", s.inviteInfo)
	mux.HandleFunc("POST /api/invites/{token}/accept", s.acceptInvite)

	mux.HandleFunc("GET /api/me", s.needUser(s.me))
	mux.HandleFunc("PATCH /api/me", s.needUser(s.patchMe))
	mux.HandleFunc("GET /api/me/settings", s.needUser(s.allSettings))
	mux.HandleFunc("GET /api/me/settings/{key}", s.needUser(s.getSetting))
	mux.HandleFunc("PUT /api/me/settings/{key}", s.needUser(s.putSetting))
	mux.HandleFunc("DELETE /api/me/settings/{key}", s.needUser(s.deleteSetting))
	mux.HandleFunc("POST /api/me/password", s.needUser(s.changePassword))
	mux.HandleFunc("GET /api/me/sessions", s.needUser(s.sessions))
	mux.HandleFunc("DELETE /api/me/sessions/{id}", s.needUser(s.deleteSession))
	mux.HandleFunc("GET /api/me/passkeys", s.needUser(s.passkeys))
	mux.HandleFunc("DELETE /api/me/passkeys/{id}", s.needUser(s.deletePasskey))
	mux.HandleFunc("POST /api/me/passkeys/begin", s.needUser(s.passkeyRegisterBegin))
	mux.HandleFunc("POST /api/me/passkeys/finish", s.needUser(s.passkeyRegisterFinish))

	mux.HandleFunc("GET /api/admin/users", s.needAdmin(s.adminUsers))
	mux.HandleFunc("POST /api/admin/invites", s.needAdmin(s.createInvite))

	mux.HandleFunc("GET /api/metrics", s.needUser(s.metrics))
	mux.HandleFunc("GET /api/daily", s.needUser(s.daily))
	mux.HandleFunc("GET /api/series/{metric}", s.needUser(s.series))
	mux.HandleFunc("GET /api/sleep", s.needUser(s.sleep))
	mux.HandleFunc("GET /api/sleep/{start}/stages", s.needUser(s.sleepStages))
	mux.HandleFunc("GET /api/activities", s.needUser(s.activities))
	mux.HandleFunc("GET /api/activities/{id}", s.needUser(s.activity))
	mux.HandleFunc("GET /api/activities/{id}/streams", s.needUser(s.activityStreams))
	mux.HandleFunc("GET /api/activities/{id}/neighbours", s.needUser(s.activityNeighbours))
	mux.HandleFunc("GET /api/activities/{id}/fit", s.needUser(s.activityFIT))
	if s.opt.Tiles != nil {
		// Signed-in users only: the cache must not become an open proxy to the tile server.
		tiles := http.StripPrefix("/tiles", s.opt.Tiles)
		mux.HandleFunc("GET /tiles/", s.needUser(func(w http.ResponseWriter, r *http.Request, _ store.User) { tiles.ServeHTTP(w, r) }))
	}
	mux.HandleFunc("GET /api/me/watch-profile", s.needUser(s.watchProfile))
	mux.HandleFunc("GET /api/me/zones", s.needUser(s.zones))
	mux.HandleFunc("GET /api/totals", s.needUser(s.totals))
	mux.HandleFunc("GET /api/workouts", s.needUser(s.workouts))
	mux.HandleFunc("GET /api/workouts/{id}", s.needUser(s.workout))
	mux.HandleFunc("GET /api/exercises", s.needUser(s.exercises))
	mux.HandleFunc("GET /api/exercises/{category}/{number}", s.needUser(s.exercise))
	mux.HandleFunc("GET /api/courses", s.needUser(s.courses))
	mux.HandleFunc("GET /api/courses/{id}", s.needUser(s.course))
	mux.HandleFunc("GET /api/courses/{id}/streams", s.needUser(s.courseStreams))
	mux.HandleFunc("GET /api/courses/{id}/fit", s.needUser(s.courseFIT))
	mux.HandleFunc("GET /api/coach", s.needUser(s.coach))
	mux.HandleFunc("GET /api/devices", s.needUser(s.devices))
	mux.HandleFunc("PATCH /api/devices/{id}", s.needUser(s.updateDevice))
	mux.HandleFunc("POST /api/devices/{id}/sync", s.needUser(s.syncDevice))
	mux.HandleFunc("GET /api/devices/{id}/sync-runs", s.needUser(s.syncRuns))
	mux.HandleFunc("GET /api/devices/{id}/live", s.needUser(s.live))
	mux.HandleFunc("GET /api/devices/{id}/summary", s.needUser(s.deviceSummary))
	mux.HandleFunc("GET /api/devices/{id}/files", s.needUser(s.watchFiles))
	mux.HandleFunc("POST /api/devices/{id}/files/delete", s.needUser(s.deleteWatchFiles))
	mux.HandleFunc("POST /api/devices/{id}/unpair", s.needUser(s.unpairDevice))
	mux.HandleFunc("POST /api/devices/{id}/release", s.needUser(s.releaseDevice))
	mux.HandleFunc("POST /api/devices/{id}/delete-data", s.needUser(s.deleteDeviceData))
	mux.HandleFunc("GET /api/devices/{id}/raw.zip", s.needUser(s.rawZip))
	mux.HandleFunc("GET /api/admin/devices", s.needAdmin(s.adminDevices))
	mux.HandleFunc("POST /api/imports", s.needUser(s.createImport))
	mux.HandleFunc("GET /api/imports", s.needUser(s.imports))
	mux.HandleFunc("GET /api/imports/{id}", s.needUser(s.getImport))
	mux.HandleFunc("DELETE /api/imports/{id}", s.needUser(s.cancelImport))
	mux.HandleFunc("GET /api/usb", s.needUser(s.usbWatches))
	mux.HandleFunc("POST /api/usb/{id}/import", s.needUser(s.importUSB))
	mux.HandleFunc("POST /api/admin/devices/{id}/release", s.needAdmin(s.adminRelease))
	mux.HandleFunc("POST /api/admin/devices/{id}/forget", s.needAdmin(s.adminForget))
	mux.HandleFunc("POST /api/pairing", s.needUser(s.startPairing))
	mux.HandleFunc("GET /api/pairing", s.needUser(s.myPairing))
	mux.HandleFunc("GET /api/pairing/computers", s.needUser(s.pairComputers))
	mux.HandleFunc("GET /api/pairing/{id}", s.needUser(s.getPairing))
	mux.HandleFunc("GET /api/pairing/{id}/events", s.needUser(s.pairingEvents))
	mux.HandleFunc("POST /api/pairing/{id}/scan", s.needUser(s.pairingScan))
	mux.HandleFunc("POST /api/pairing/{id}/select", s.needUser(s.pairingSelect))
	mux.HandleFunc("POST /api/pairing/{id}/confirm", s.needUser(s.pairingConfirm))
	mux.HandleFunc("POST /api/pairing/{id}/passkey", s.needUser(s.pairingPasskey))
	mux.HandleFunc("POST /api/pairing/{id}/setup", s.needUser(s.pairingSetup))
	mux.HandleFunc("DELETE /api/pairing/{id}", s.needUser(s.cancelPairing))
	mux.HandleFunc("GET /api/admin/pairing", s.needAdmin(s.adminPairing))
	mux.HandleFunc("DELETE /api/admin/pairing", s.needAdmin(s.adminCancelPairing))
	mux.HandleFunc("GET /api/sync-runs", s.needUser(s.syncLog))
	mux.HandleFunc("GET /api/bluetooth", s.needUser(s.bluetooth))
	mux.HandleFunc("POST /api/bluetooth/power", s.needUser(s.bluetoothPower))
	s.routesReaders(mux)
	s.routesNode(mux)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, http.StatusNotFound, "no such API route") })
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, "ok\n")
}

// securityHeaders sets a strict policy: everything comes from this origin. Map tiles are
// served from /tiles on the same origin, so no third party is ever contacted by the browser.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; "+
			"object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

type ctxKey int

const userKey ctxKey = 0

// withSession resolves the session cookie, if any, to its user.
func (s *Server) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" && len(c.Value) < 100 {
			if u, err := s.st.SessionUser(r.Context(), auth.HashToken(c.Value)); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), userKey, u))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func currentUser(r *http.Request) (store.User, bool) {
	u, ok := r.Context().Value(userKey).(store.User)
	return u, ok
}

// sameOrigin guards cookie-authenticated writes against cross-site requests: browsers send
// Origin (or Sec-Fetch-Site) with every POST, PUT and DELETE.
func (s *Server) sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		return err == nil && (u.Host == r.Host || u.Host == s.public.Host)
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	}
	return false
}

func isWrite(r *http.Request) bool {
	return r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
}

func (s *Server) needUser(h func(http.ResponseWriter, *http.Request, store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok := currentUser(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "sign in first")
			return
		}
		if isWrite(r) && !s.sameOrigin(r) {
			writeError(w, http.StatusForbidden, "cross-site request refused")
			return
		}
		h(w, r, u)
	}
}

func (s *Server) needAdmin(h func(http.ResponseWriter, *http.Request, store.User)) http.HandlerFunc {
	return s.needUser(func(w http.ResponseWriter, r *http.Request, u store.User) {
		if !u.IsAdmin {
			writeError(w, http.StatusForbidden, "admins only")
			return
		}
		h(w, r, u)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// internalError logs err and answers 500 without details.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("api", "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func (s *Server) secure() bool { return s.public.Scheme == "https" }

func (s *Server) setSession(w http.ResponseWriter, r *http.Request, user int64) error {
	token, hash := auth.NewToken()
	if err := s.st.CreateSession(r.Context(), user, hash, sessionTTL, r.UserAgent(), clientIP(r)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: s.secure(), MaxAge: int(sessionTTL / time.Second)})
	return nil
}

// clientIP is the TCP peer; goarmin does not trust forwarding headers.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) inviteURL(token string) string {
	return s.public.Scheme + "://" + s.public.Host + "/invite/" + token
}

// trimEmail normalises an email for lookups.
func trimEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }
