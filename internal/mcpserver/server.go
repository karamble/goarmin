// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package mcpserver is goarmin's MCP server (Model Context Protocol): the whole app for AI
// assistants. Every tool is a row in a table that names an API route; a call runs the API's own
// handler in-process as the token's account, so an assistant gets exactly what the dashboard
// shows, with the same validation, ownership and privacy rules.
//
// Access is by bearer tokens each account makes under Settings › MCP, at three levels: read,
// write and admin. A token's level decides which tools it even sees, and each call checks it
// again.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/karamble/goarmin/internal/store"
	pauth "github.com/karamble/goarmin/pkg/auth"
)

// TokenPrefix starts every MCP token, so a leaked one is recognisable.
const TokenPrefix = "gma_"

// Limits per token.
const (
	CallsPerMinute       = 120
	DestructivePerMinute = 10
	sessionTimeout       = 30 * time.Minute
	maxRequestBody       = 48 << 20 // import_files carries files as base64
)

// API serves an /api request as the signed-in user u, exactly as for the dashboard. The api
// package implements it.
type API interface {
	ServeAs(u store.User, w http.ResponseWriter, r *http.Request)
}

// Options configures the MCP server.
type Options struct {
	Store *store.Store
	API   API
	// PublicURL is where goarmin is reached (the endpoint is PublicURL + "/mcp").
	PublicURL string
	Version   string
	Logger    *slog.Logger
	// Now is the clock (tests); nil: time.Now.
	Now func() time.Time
}

// Server is the MCP endpoint.
type Server struct {
	opt     Options
	st      *store.Store
	log     *slog.Logger
	public  *url.URL
	now     func() time.Time
	servers map[Level]*mcp.Server
	handler http.Handler

	calls       *pauth.Limiter
	destructive *pauth.Limiter
}

// New builds the MCP server: one MCP server per access level, behind bearer-token checks.
func New(opt Options) (*Server, error) {
	u, err := url.Parse(opt.PublicURL)
	if err != nil || u.Host == "" {
		return nil, errors.New("mcpserver: the public URL must be http(s)://host[:port]")
	}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	s := &Server{opt: opt, st: opt.Store, log: opt.Logger, public: u, now: opt.Now, servers: map[Level]*mcp.Server{},
		calls: pauth.NewLimiter(CallsPerMinute, time.Minute), destructive: pauth.NewLimiter(DestructivePerMinute, time.Minute)}
	s.calls.Now, s.destructive.Now = opt.Now, opt.Now
	for _, l := range []Level{Read, Write, Admin} {
		s.servers[l] = s.build(l)
	}
	streams := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		if l, ok := levelOf(auth.TokenInfoFromContext(r.Context())); ok {
			return s.servers[l]
		}
		return nil
	}, &mcp.StreamableHTTPOptions{
		SessionTimeout: sessionTimeout,
		// The SDK's own check refuses a loopback request whose Host is not localhost, which also
		// refuses a reverse proxy on the same computer; hostCheck below does it with the public
		// host allowed. Every request needs a token anyway.
		DisableLocalhostProtection: true,
		MaxRequestBodyBytes:        maxRequestBody,
		Logger:                     s.log,
	})
	verified := auth.RequireBearerToken(s.verify, &auth.RequireBearerTokenOptions{AllowMissingExpiration: true})(streams)
	cop := http.NewCrossOriginProtection()
	_ = cop.AddTrustedOrigin(u.Scheme + "://" + u.Host)
	s.handler = s.hostCheck(cop.Handler(verified))
	return s, nil
}

// Handler serves /mcp.
func (s *Server) Handler() http.Handler { return s.handler }

// Endpoint is the URL assistants connect to.
func (s *Server) Endpoint() string { return s.public.Scheme + "://" + s.public.Host + "/mcp" }

// hostCheck refuses DNS rebinding: a request that reaches goarmin over loopback must name
// localhost or the public host. A long-lived event stream (GET) outlasts the server's write
// timeout, so its deadline is lifted.
func (s *Server) hostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
			if ap, err := netipAddr(local); err == nil && ap.IsLoopback() {
				host := r.Host
				if h, _, err := net.SplitHostPort(host); err == nil {
					host = h
				}
				host = strings.Trim(host, "[]")
				if host != "localhost" && host != "127.0.0.1" && host != "::1" && !strings.EqualFold(r.Host, s.public.Host) &&
					!strings.EqualFold(host, s.public.Hostname()) {
					http.Error(w, "forbidden: the Host header does not name this server", http.StatusForbidden)
					return
				}
			}
		}
		if r.Method == http.MethodGet {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
		}
		next.ServeHTTP(w, r)
	})
}

// caller is who is calling: the token and its account, as the verifier found them.
type caller struct {
	token store.MCPToken
	user  store.User
	level Level
}

func callerOf(ti *auth.TokenInfo) (caller, bool) {
	if ti == nil || ti.Extra == nil {
		return caller{}, false
	}
	c, ok := ti.Extra["caller"].(caller)
	return c, ok
}

func levelOf(ti *auth.TokenInfo) (Level, bool) {
	c, ok := callerOf(ti)
	return c.level, ok
}

// verify checks a token on every request: it exists, is not revoked or expired, its account is
// enabled, and an admin token's account is still an admin.
func (s *Server) verify(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
	if !strings.HasPrefix(token, TokenPrefix) || len(token) > 200 {
		return nil, fmt.Errorf("%w: not a goarmin MCP token (make one under Settings › MCP)", auth.ErrInvalidToken)
	}
	t, u, err := s.st.MCPTokenByHash(ctx, pauth.HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: this token was revoked, has expired or is unknown", auth.ErrInvalidToken)
	}
	if err != nil {
		return nil, err
	}
	l, ok := ParseLevel(t.Scope)
	if !ok {
		return nil, fmt.Errorf("%w: this token has no valid access level", auth.ErrInvalidToken)
	}
	if l == Admin && !u.IsAdmin {
		return nil, fmt.Errorf("%w: this admin token's account is no longer an admin", auth.ErrInvalidToken)
	}
	// Sessions are bound to the token, not the account: another token of the same account,
	// perhaps with a lower level, cannot take a session over.
	return &auth.TokenInfo{UserID: "token-" + strconv.FormatInt(t.ID, 10), Scopes: []string{t.Scope},
		Extra: map[string]any{"caller": caller{token: t, user: u, level: l}}}, nil
}

// build makes the MCP server for one access level: the tools up to that level, the resources
// and the prompts.
func (s *Server) build(l Level) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "goarmin", Title: "goarmin", Version: s.opt.Version}, &mcp.ServerOptions{
		Instructions: instructions(l),
		Capabilities: &mcp.ServerCapabilities{},
		Logger:       s.log,
	})
	for i := range Tools {
		t := &Tools[i]
		if t.Level > l {
			continue
		}
		srv.AddTool(t.mcpTool(), s.toolHandler(t))
	}
	for i := range Resources {
		r := &Resources[i]
		srv.AddResourceTemplate(r.template(), s.resourceHandler(r))
	}
	for i := range Prompts {
		p := &Prompts[i]
		srv.AddPrompt(p.prompt(), s.promptHandler(p, l))
	}
	return srv
}

// instructions tell an assistant what goarmin is and how its tools behave.
func instructions(l Level) string {
	b := strings.Builder{}
	b.WriteString("goarmin is a self-hosted replacement for Garmin Connect: health, sleep, training and watch data " +
		"from the user's own Garmin watches. Every tool acts as the account that made this token and sees only its data. " +
		"Days are local dates (YYYY-MM-DD) in the watch's time zone; times are Unix seconds; values are SI units unless a field says otherwise. ")
	switch l {
	case Read:
		b.WriteString("This token can only read.")
	case Write:
		b.WriteString("This token can also change things: plans, syncs, settings, imports and pairing. " +
			"Tools that remove data from a watch or goarmin need confirm set to the watch's name; ask the user to type it, never guess it.")
	case Admin:
		b.WriteString("This token can also administer the server: users, invites, every watch and the sync nodes. " +
			"Tools that remove data need confirm set to the watch's name; ask the user to type it, never guess it.")
	}
	return b.String()
}

// logCall records a call in the account's log; a failure to log never fails the call.
func (s *Server) logCall(c caller, tool string, start time.Time, errText string) {
	ms := s.now().Sub(start).Milliseconds()
	if err := s.st.LogMCPCall(context.WithoutCancel(context.Background()), store.MCPCall{TokenID: c.token.ID, UserID: c.user.ID,
		At: start, Tool: tool, OK: errText == "", Ms: ms, Error: errText}); err != nil {
		s.log.Warn("mcp: call log", "err", err)
	}
}

// limit counts a call against the token's limits; it says why when the call must wait.
func (s *Server) limit(c caller, destructive bool) string {
	key := strconv.FormatInt(c.token.ID, 10)
	if s.calls.Blocked(key) {
		return fmt.Sprintf("too many calls: a token makes at most %d a minute; wait a little", CallsPerMinute)
	}
	if destructive && s.destructive.Blocked(key) {
		return fmt.Sprintf("too many removals: a token makes at most %d a minute; wait a little", DestructivePerMinute)
	}
	s.calls.Fail(key)
	if destructive {
		s.destructive.Fail(key)
	}
	return ""
}
