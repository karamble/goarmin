// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/karamble/goarmin/internal/mcpserver"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/auth"
)

// maxMCPTokens bounds the live tokens of one account.
const maxMCPTokens = 50

// routesMCP mounts the MCP endpoint and the dashboard's token routes (Settings › MCP). The
// token routes take a signed-in browser session only: an assistant cannot make itself tokens.
func (s *Server) routesMCP(mux *router) {
	if s.mcp != nil {
		mux.Handle("/mcp", s.mcp.Handler())
	} else {
		mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "MCP is off on this server (goarmin serve -no-mcp)", http.StatusNotFound)
		})
	}
	mux.HandleFunc("GET /api/me/mcp", s.needUser(s.mcpOverview))
	mux.HandleFunc("POST /api/me/mcp/tokens", s.needUser(s.createMCPToken))
	mux.HandleFunc("PATCH /api/me/mcp/tokens/{id}", s.needUser(s.renameMCPToken))
	mux.HandleFunc("DELETE /api/me/mcp/tokens/{id}", s.needUser(s.revokeMCPToken))
}

func unixOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Unix()
}

func mcpTokenJSON(t store.MCPToken, calls int, now time.Time) map[string]any {
	return map[string]any{"id": t.ID, "name": t.Name, "prefix": t.Prefix, "scope": t.Scope, "created_at": t.CreatedAt.Unix(),
		"expires_at": unixOrNil(t.ExpiresAt), "last_used_at": unixOrNil(t.LastUsed), "revoked_at": unixOrNil(t.Revoked),
		"live": t.Live(now), "calls_24h": calls}
}

// mcpOverview is the Settings › MCP tab: whether MCP is on, its endpoint, the account's
// tokens, its recent calls and the catalogue of what MCP offers.
func (s *Server) mcpOverview(w http.ResponseWriter, r *http.Request, u store.User) {
	now := time.Now()
	tokens, err := s.st.MCPTokens(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	counts, err := s.st.MCPCallCounts(r.Context(), u.ID, now.Add(-24*time.Hour))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	calls, err := s.st.MCPCalls(r.Context(), u.ID, 50)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	tl := []map[string]any{}
	for _, t := range tokens {
		tl = append(tl, mcpTokenJSON(t, counts[t.ID], now))
	}
	cl := []map[string]any{}
	for _, c := range calls {
		cl = append(cl, map[string]any{"id": c.ID, "token_id": c.TokenID, "token": c.TokenName, "at": c.At.Unix(), "tool": c.Tool,
			"ok": c.OK, "ms": c.Ms, "error": c.Error})
	}
	scopes := []string{store.MCPRead, store.MCPWrite}
	if u.IsAdmin {
		scopes = append(scopes, store.MCPAdmin)
	}
	out := map[string]any{"enabled": s.mcp != nil, "endpoint": s.public.Scheme + "://" + s.public.Host + "/mcp", "tokens": tl,
		"calls": cl, "scopes": scopes, "catalogue": mcpserver.Catalogue(), "retention_days": int(store.MCPRetention / (24 * time.Hour)),
		"limits": map[string]int{"calls_per_minute": mcpserver.CallsPerMinute, "destructive_per_minute": mcpserver.DestructivePerMinute},
		"secure": s.secure()}
	writeJSON(w, http.StatusOK, out)
}

// mcpExpiries are the lifetimes a token may get, in days; 0 is never.
var mcpExpiries = map[int]bool{0: true, 30: true, 90: true, 365: true}

func validTokenName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	return name, name != "" && utf8.RuneCountInString(name) <= 60 && !strings.ContainsAny(name, "\x00\r\n")
}

// createMCPToken makes a token and shows it once; only its hash is kept.
func (s *Server) createMCPToken(w http.ResponseWriter, r *http.Request, u store.User) {
	if s.mcp == nil {
		writeError(w, http.StatusConflict, "MCP is off on this server (goarmin serve -no-mcp)")
		return
	}
	var req struct {
		Name        string `json:"name"`
		Scope       string `json:"scope"`
		ExpiresDays *int   `json:"expires_days"`
	}
	if !decode(w, r, &req) {
		return
	}
	name, ok := validTokenName(req.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "give the token a name of at most 60 characters")
		return
	}
	switch req.Scope {
	case store.MCPRead, store.MCPWrite:
	case store.MCPAdmin:
		if !u.IsAdmin {
			writeError(w, http.StatusForbidden, "only admins can make admin tokens")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "scope must be read, write or admin")
		return
	}
	if req.ExpiresDays == nil || !mcpExpiries[*req.ExpiresDays] {
		writeError(w, http.StatusBadRequest, "expires_days must be 30, 90, 365 or 0 (never)")
		return
	}
	existing, err := s.st.MCPTokens(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	live := 0
	now := time.Now()
	for _, t := range existing {
		if t.Live(now) {
			live++
		}
	}
	if live >= maxMCPTokens {
		writeError(w, http.StatusConflict, "an account has at most 50 live tokens: revoke one first")
		return
	}
	secret, _ := auth.NewToken()
	token := mcpserver.TokenPrefix + secret
	var expires time.Time
	if *req.ExpiresDays > 0 {
		expires = now.AddDate(0, 0, *req.ExpiresDays)
	}
	t, err := s.st.CreateMCPToken(r.Context(), u.ID, name, token[:len(mcpserver.TokenPrefix)+4], auth.HashToken(token), req.Scope, expires)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.Info("mcp: token made", "user", u.ID, "token", t.ID, "scope", t.Scope)
	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "item": mcpTokenJSON(t, 0, now)})
}

func mcpTokenID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such token")
		return 0, false
	}
	return id, true
}

func (s *Server) renameMCPToken(w http.ResponseWriter, r *http.Request, u store.User) {
	id, ok := mcpTokenID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name, ok := validTokenName(req.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "give the token a name of at most 60 characters")
		return
	}
	if err := s.st.RenameMCPToken(r.Context(), u.ID, id, name); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such token")
		return
	} else if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// revokeMCPToken ends a token at once: its next call is refused.
func (s *Server) revokeMCPToken(w http.ResponseWriter, r *http.Request, u store.User) {
	id, ok := mcpTokenID(w, r)
	if !ok {
		return
	}
	if err := s.st.RevokeMCPToken(r.Context(), u.ID, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such token, or it is revoked already")
		return
	} else if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.Info("mcp: token revoked", "user", u.ID, "token", id)
	w.WriteHeader(http.StatusNoContent)
}
