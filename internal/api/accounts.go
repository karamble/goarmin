// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/auth"
)

type userJSON struct {
	ID          int64     `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Timezone    string    `json:"timezone"`
	Units       string    `json:"units"`
	IsAdmin     bool      `json:"is_admin"`
	CreatedAt   time.Time `json:"created_at"`
}

func toUserJSON(u store.User) userJSON {
	return userJSON{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Timezone: u.Timezone, Units: u.Units,
		IsAdmin: u.IsAdmin, CreatedAt: u.CreatedAt}
}

// serverJSON tells the dashboard what this server can do.
type serverJSON struct {
	Version string `json:"version"`
	Sync    bool   `json:"sync"`  // something syncs watches: this server, or a sync node (live view, sync now)
	Pair    bool   `json:"pair"`  // Add a watch works here (the in-process sync role pairs)
	Tiles   bool   `json:"tiles"` // map tiles under /tiles
	// Map says how to show the tiles: dark already, and the credit (only with tiles).
	Map *mapJSON `json:"map,omitempty"`
}

func validEmail(e string) bool {
	e = strings.TrimSpace(e)
	at := strings.IndexByte(e, '@')
	return len(e) <= 254 && at > 0 && at < len(e)-1 && !strings.ContainsAny(e, " \t\r\n")
}

// setupStatus tells the UI whether to show the first-run setup.
func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	ok, err := s.st.CanLogIn(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"needed": !ok})
}

// setup creates the first admin. It needs the token goarmin serve printed and works only while
// no account can log in.
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		writeError(w, http.StatusForbidden, "cross-site request refused")
		return
	}
	var req struct {
		Token, Email, Name, Password string
	}
	if !decode(w, r, &req) {
		return
	}
	ip := clientIP(r)
	if s.ipOnly.Blocked(ip) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	ok, err := s.st.CanLogIn(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if ok || s.opt.SetupToken == "" {
		writeError(w, http.StatusConflict, "setup is done")
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(s.opt.SetupToken)) != 1 {
		s.ipOnly.Fail(ip)
		writeError(w, http.StatusForbidden, "wrong setup token (goarmin serve prints it at startup)")
		return
	}
	if !validEmail(req.Email) {
		writeError(w, http.StatusBadRequest, "that is not an email address")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "auth: "))
		return
	}
	u, err := s.st.ClaimSetup(r.Context(), req.Email, strings.TrimSpace(req.Name), hash)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.setSession(w, r, u.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.Info("setup: first admin created", "user", u.ID)
	writeJSON(w, http.StatusCreated, toUserJSON(u))
}

func (s *Server) passwordLogin(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		writeError(w, http.StatusForbidden, "cross-site request refused")
		return
	}
	var req struct{ Email, Password string }
	if !decode(w, r, &req) {
		return
	}
	ip := clientIP(r)
	key := ip + "|" + trimEmail(req.Email)
	if s.login.Blocked(key) || s.ipOnly.Blocked(ip) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	l, err := s.st.LoginByEmail(r.Context(), req.Email)
	switch {
	case errors.Is(err, store.ErrNotFound):
		auth.CheckUnknown(req.Password)
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	ok := false
	if err == nil && l.PasswordHash != "" && !l.Disabled {
		ok, _ = auth.CheckPassword(req.Password, l.PasswordHash)
	} else if err == nil {
		auth.CheckUnknown(req.Password)
	}
	if !ok {
		s.login.Fail(key)
		s.ipOnly.Fail(ip)
		writeError(w, http.StatusUnauthorized, "wrong email or password")
		return
	}
	s.login.Reset(key)
	if err := s.setSession(w, r, l.User.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserJSON(l.User))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		writeError(w, http.StatusForbidden, "cross-site request refused")
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		_ = s.st.DeleteSession(r.Context(), auth.HashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: s.secure(), MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, u store.User) {
	writeJSON(w, http.StatusOK, struct {
		userJSON
		Server serverJSON `json:"server"`
	}{toUserJSON(u), s.serverInfo()})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, u store.User) {
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &req) {
		return
	}
	l, err := s.st.LoginByEmail(r.Context(), u.Email)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if l.PasswordHash != "" {
		if ok, _ := auth.CheckPassword(req.Current, l.PasswordHash); !ok {
			writeError(w, http.StatusForbidden, "the current password is wrong")
			return
		}
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "auth: "))
		return
	}
	if err := s.st.SetPassword(r.Context(), u.ID, hash); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) sessions(w http.ResponseWriter, r *http.Request, u store.User) {
	list, err := s.st.Sessions(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	cur := ""
	if c, err := r.Cookie(sessionCookie); err == nil {
		cur = auth.HashToken(c.Value)[:16]
	}
	out := []map[string]any{}
	for _, x := range list {
		out = append(out, map[string]any{"id": x.ID, "created_at": x.CreatedAt, "last_seen": x.LastSeen,
			"expires_at": x.ExpiresAt, "user_agent": x.UserAgent, "ip": x.IP, "current": x.ID == cur})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request, u store.User) {
	switch err := s.st.DeleteSessionByID(r.Context(), u.ID, r.PathValue("id")); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such session")
	case err != nil:
		s.internalError(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) createInvite(w http.ResponseWriter, r *http.Request, u store.User) {
	var req struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Email != "" && !validEmail(req.Email) {
		writeError(w, http.StatusBadRequest, "that is not an email address")
		return
	}
	token, hash := auth.NewToken()
	if err := s.st.CreateInvite(r.Context(), hash, req.Email, u.ID, inviteTTL); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"url": s.inviteURL(token), "email": req.Email,
		"expires_at": time.Now().Add(inviteTTL)})
}

func (s *Server) inviteInfo(w http.ResponseWriter, r *http.Request) {
	inv, err := s.st.OpenInvite(r.Context(), auth.HashToken(r.PathValue("token")))
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "this invitation is used up or expired")
	case err != nil:
		s.internalError(w, r, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"email": inv.Email, "expires_at": inv.ExpiresAt})
	}
}

func (s *Server) acceptInvite(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		writeError(w, http.StatusForbidden, "cross-site request refused")
		return
	}
	var req struct{ Email, Name, Password string }
	if !decode(w, r, &req) {
		return
	}
	if s.ipOnly.Blocked(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	if !validEmail(req.Email) {
		writeError(w, http.StatusBadRequest, "that is not an email address")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "auth: "))
		return
	}
	u, err := s.st.AcceptInvite(r.Context(), auth.HashToken(r.PathValue("token")), req.Email, strings.TrimSpace(req.Name), hash)
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.ipOnly.Fail(clientIP(r))
		writeError(w, http.StatusNotFound, "this invitation is used up, expired or for another email")
		return
	case errors.Is(err, store.ErrExists):
		writeError(w, http.StatusConflict, "an account with this email exists already")
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	if err := s.setSession(w, r, u.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toUserJSON(u))
}

// adminUsers lists accounts: identity only, never health data.
func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request, _ store.User) {
	users, err := s.st.Users(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []userJSON{}
	for _, u := range users {
		out = append(out, toUserJSON(u))
	}
	writeJSON(w, http.StatusOK, out)
}

type mapJSON struct {
	Dark        bool   `json:"dark"`
	Attribution string `json:"attribution"`
}

// serverInfo is what the dashboard needs to know about this server.
func (s *Server) serverInfo() serverJSON {
	out := serverJSON{Version: s.opt.Version, Sync: s.node() != nil, Tiles: s.opt.Tiles != nil}
	if _, ok := s.opt.Node.(Pairing); ok && out.Sync {
		out.Pair = true
		if cp, ok := s.opt.Node.(interface{ CanPair() bool }); ok {
			out.Pair = cp.CanPair()
		}
	}
	if out.Tiles {
		out.Map = &mapJSON{Dark: s.opt.TilesDark, Attribution: s.opt.TilesAttribution}
	}
	return out
}
