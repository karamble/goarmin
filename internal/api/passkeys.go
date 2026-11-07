// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/auth"
)

// ceremony is a passkey registration or login in progress.
type ceremony struct {
	data    webauthn.SessionData
	user    int64 // 0 for a login
	name    string
	expires time.Time
}

func (s *Server) startCeremony(w http.ResponseWriter, c ceremony) {
	id, _ := auth.NewToken()
	c.expires = time.Now().Add(ceremonyTTL)
	s.mu.Lock()
	now := time.Now()
	for k, v := range s.ceremonies {
		if now.After(v.expires) {
			delete(s.ceremonies, k)
		}
	}
	if len(s.ceremonies) > 10_000 { // a flood: drop everything rather than grow
		s.ceremonies = map[string]ceremony{}
	}
	s.ceremonies[id] = c
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: ceremonyCookie, Value: id, Path: "/api/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: s.secure(), MaxAge: int(ceremonyTTL / time.Second)})
}

// takeCeremony returns and forgets the ceremony of this browser.
func (s *Server) takeCeremony(w http.ResponseWriter, r *http.Request) (ceremony, bool) {
	ck, err := r.Cookie(ceremonyCookie)
	if err != nil {
		return ceremony{}, false
	}
	http.SetCookie(w, &http.Cookie{Name: ceremonyCookie, Value: "", Path: "/api/", HttpOnly: true, MaxAge: -1, Secure: s.secure()})
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.ceremonies[ck.Value]
	delete(s.ceremonies, ck.Value)
	if !ok || time.Now().After(c.expires) {
		return ceremony{}, false
	}
	return c, true
}

// waUser adapts an account to the webauthn library.
type waUser struct {
	u     store.User
	id    []byte
	creds []webauthn.Credential
}

func (w waUser) WebAuthnID() []byte          { return w.id }
func (w waUser) WebAuthnName() string        { return w.u.Email }
func (w waUser) WebAuthnDisplayName() string { return firstNonEmpty(w.u.DisplayName, w.u.Email) }
func (w waUser) WebAuthnCredentials() []webauthn.Credential {
	return w.creds
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Server) loadWAUser(r *http.Request, u store.User) (waUser, error) {
	id, err := s.st.WebAuthnID(r.Context(), u.ID, func() []byte { return auth.RandomBytes(32) })
	if err != nil {
		return waUser{}, err
	}
	stored, err := s.st.Credentials(r.Context(), u.ID)
	if err != nil {
		return waUser{}, err
	}
	wu := waUser{u: u, id: id}
	for _, c := range stored {
		var cred webauthn.Credential
		if err := json.Unmarshal([]byte(c.JSON), &cred); err == nil {
			wu.creds = append(wu.creds, cred)
		}
	}
	return wu, nil
}

func (s *Server) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request, u store.User) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	wu, err := s.loadWAUser(r, u)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	var exclude []protocol.CredentialDescriptor
	for _, c := range wu.creds {
		exclude = append(exclude, c.Descriptor())
	}
	creation, data, err := s.wa.BeginRegistration(wu,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(exclude))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "passkey"
	}
	s.startCeremony(w, ceremony{data: *data, user: u.ID, name: name})
	writeJSON(w, http.StatusOK, creation)
}

func (s *Server) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request, u store.User) {
	c, ok := s.takeCeremony(w, r)
	if !ok || c.user != u.ID {
		writeError(w, http.StatusBadRequest, "no passkey registration in progress, start again")
		return
	}
	wu, err := s.loadWAUser(r, u)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	cred, err := s.wa.FinishRegistration(wu, c.data, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the passkey could not be verified")
		s.log.Info("passkey registration failed", "user", u.ID, "err", err)
		return
	}
	b, err := json.Marshal(cred)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.st.AddCredential(r.Context(), u.ID, cred.ID, c.name, string(b)); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": base64.RawURLEncoding.EncodeToString(cred.ID), "name": c.name})
}

func (s *Server) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		writeError(w, http.StatusForbidden, "cross-site request refused")
		return
	}
	if s.ipOnly.Blocked(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	assertion, data, err := s.wa.BeginDiscoverableLogin()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.startCeremony(w, ceremony{data: *data})
	writeJSON(w, http.StatusOK, assertion)
}

func (s *Server) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		writeError(w, http.StatusForbidden, "cross-site request refused")
		return
	}
	c, ok := s.takeCeremony(w, r)
	if !ok || c.user != 0 {
		writeError(w, http.StatusBadRequest, "no passkey login in progress, start again")
		return
	}
	var found waUser
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		u, err := s.st.UserByWebAuthnID(r.Context(), userHandle)
		if err != nil {
			return nil, err
		}
		found, err = s.loadWAUser(r, u)
		return found, err
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	_, cred, err := s.wa.FinishPasskeyLogin(handler, c.data, r)
	if err != nil {
		s.ipOnly.Fail(clientIP(r))
		writeError(w, http.StatusUnauthorized, "the passkey was not accepted")
		s.log.Info("passkey login failed", "err", err)
		return
	}
	if b, err := json.Marshal(cred); err == nil {
		_ = s.st.UpdateCredential(r.Context(), found.u.ID, cred.ID, string(b))
	}
	if err := s.setSession(w, r, found.u.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserJSON(found.u))
}

func (s *Server) passkeys(w http.ResponseWriter, r *http.Request, u store.User) {
	list, err := s.st.Credentials(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, c := range list {
		m := map[string]any{"id": base64.RawURLEncoding.EncodeToString(c.ID), "name": c.Name, "created_at": c.CreatedAt}
		if !c.LastUsedAt.IsZero() {
			m["last_used_at"] = c.LastUsedAt
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deletePasskey(w http.ResponseWriter, r *http.Request, u store.User) {
	id, err := base64.RawURLEncoding.DecodeString(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "no such passkey")
		return
	}
	// Never remove the last way in.
	l, err := s.st.LoginByEmail(r.Context(), u.Email)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	creds, err := s.st.Credentials(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if l.PasswordHash == "" && len(creds) <= 1 {
		writeError(w, http.StatusConflict, "this is your only way to sign in: set a password or add another passkey first")
		return
	}
	switch err := s.st.DeleteCredential(r.Context(), u.ID, id); {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such passkey")
	case err != nil:
		s.internalError(w, r, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
