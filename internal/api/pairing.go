// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/garmin/product"
)

// Pairing is the part of the sync role that adds watches (Add a watch): nodehub.Hub. A Node
// without it answers the pairing routes with 503.
type Pairing interface {
	StartPairing(starter, owner int64) (nodehub.PairView, bool, error)
	Pairing(user int64) (nodehub.PairView, bool)
	PairingByID(id string, user int64) (nodehub.PairView, error)
	PairEvents(id string, user int64) (<-chan nodehub.PairView, func(), error)
	PairScan(id string, user int64) error
	PairSelect(id string, user int64, addr string, repair bool) error
	PairConfirm(id string, user int64, match bool) error
	PairPasskey(id string, user int64, passkey uint32) error
	PairSetup(ctx context.Context, id string, user int64, req nodehub.PairSetupRequest) (nodehub.PairView, error)
	CancelPairing(id string, user int64, admin bool) error
	RunningPairing() (nodehub.PairView, bool)
}

// pairer returns the sync role's pairing, or answers 503.
func (s *Server) pairer(w http.ResponseWriter) (Pairing, bool) {
	p, ok := s.opt.Node.(Pairing)
	if s.node() == nil || !ok {
		writeError(w, http.StatusServiceUnavailable, "this server does not sync watches (goarmin serve --with-sync)")
		return nil, false
	}
	if cp, ok := s.opt.Node.(interface{ CanPair() bool }); ok && !cp.CanPair() {
		writeError(w, http.StatusServiceUnavailable, "Add a watch runs on the computer the watch pairs with (goarmin serve -with-sync there)")
		return nil, false
	}
	return p, true
}

// pairError answers a failed pairing call.
func (s *Server) pairError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, nodehub.ErrNoSession):
		writeError(w, http.StatusNotFound, "no such pairing session")
	case errors.Is(err, nodehub.ErrWrongState):
		writeError(w, http.StatusConflict, strings.TrimPrefix(err.Error(), nodehub.ErrWrongState.Error()+": "))
	case errors.Is(err, nodehub.ErrOwnedElsewhere):
		writeError(w, http.StatusConflict, "This watch belongs to another account on this server.")
	case errors.Is(err, nodehub.ErrNotListed):
		writeError(w, http.StatusConflict, "That watch is not in the list (any more).")
	case errors.Is(err, nodehub.ErrPairingActive):
		writeError(w, http.StatusConflict, "Someone is adding a watch right now. Try again in a few minutes.")
	case errors.Is(err, nodehub.ErrStopping):
		writeError(w, http.StatusServiceUnavailable, "the server is stopping")
	default:
		s.internalError(w, r, err)
	}
}

// signal names a signal strength for people.
func signal(rssi int16) string {
	switch {
	case rssi >= -60:
		return "strong"
	case rssi >= -75:
		return "ok"
	}
	return "weak"
}

// pairingJSON is a session as the wizard reads it, with this computer's Bluetooth state.
func (s *Server) pairingJSON(ctx context.Context, v nodehub.PairView) map[string]any {
	out := map[string]any{"id": v.ID, "state": v.State, "started_at": v.Started.Unix(), "expires_at": v.Expires.Unix(),
		"error": nil, "error_code": nil, "retry": v.Retry, "discovering": v.Discovering, "selected": nil, "prompt": nil,
		"identified": nil, "ownership": nil, "former_owner": v.FormerOwner, "device_id": nil, "busy": nil, "adapter": nil,
		"bond": nil}
	if v.Bond != "" {
		out["bond"] = v.Bond
	}
	owner := map[string]any{"id": v.Owner}
	if u, err := s.st.User(ctx, v.Owner); err == nil {
		owner["email"], owner["name"] = u.Email, u.DisplayName
	}
	out["owner"] = owner
	if v.Error != "" {
		out["error"], out["error_code"] = v.Error, v.ErrorCode
	}
	if v.Selected != "" {
		out["selected"] = v.Selected
	}
	watches := []map[string]any{}
	for _, w := range v.Watches {
		m := map[string]any{"address": w.Address, "name": w.Name, "rssi": nil, "signal": nil, "seen_now": w.SeenNow,
			"paired_here": w.PairedHere, "owner": w.Owner}
		if w.SeenNow {
			m["rssi"], m["signal"] = w.RSSI, signal(w.RSSI)
		}
		watches = append(watches, m)
	}
	out["watches"] = watches
	if p := v.Prompt; p != nil {
		out["prompt"] = map[string]any{"kind": p.Kind, "passkey": p.Passkey, "deadline": p.Deadline.Unix()}
	}
	steps := []map[string]any{}
	for _, st := range v.Steps {
		steps = append(steps, map[string]any{"name": st.Name, "done": st.Done})
	}
	out["steps"] = steps
	if id := v.Identified; id != nil {
		m := map[string]any{"unit": id.Unit, "product": id.Product, "model": product.Name(id.Product), "name": id.Name,
			"firmware": id.Firmware, "battery": nil}
		if id.Battery >= 0 {
			m["battery"] = id.Battery
		}
		out["identified"] = m
		out["ownership"] = v.Ownership
		if v.Ownership == nodehub.OwnerYours {
			out["nickname"], out["after_fetch"] = v.Nickname, v.AfterFetch
		}
	}
	if v.DeviceID != 0 {
		out["device_id"] = v.DeviceID
	}
	if !v.Terminal() {
		if b, err := s.opt.Node.Bluetooth(ctx); err == nil {
			state := b.PowerState
			if state == "" {
				state = map[bool]string{true: "on", false: "off"}[b.Powered]
			}
			out["adapter"] = map[string]any{"name": b.Name, "powered": b.Powered, "power_state": state, "pairable": b.Pairable}
			if b.Busy != nil && b.Busy.Kind != "pairing" {
				out["busy"] = map[string]any{"kind": b.Busy.Kind, "since": b.Busy.Since.Unix()}
			}
		}
	}
	return out
}

// startPairing opens the server's pairing session. {"owner": id} lets an admin add a watch for
// another account; the body is optional.
func (s *Server) startPairing(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.pairer(w)
	if !ok {
		return
	}
	var req struct {
		Owner int64 `json:"owner"`
	}
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	owner := u.ID
	if req.Owner != 0 && req.Owner != u.ID {
		if !u.IsAdmin {
			writeError(w, http.StatusForbidden, "only admins add watches for someone else")
			return
		}
		if _, err := s.st.User(r.Context(), req.Owner); err != nil {
			writeError(w, http.StatusNotFound, "no such account")
			return
		}
		owner = req.Owner
	}
	v, created, err := p.StartPairing(u.ID, owner)
	if err != nil {
		s.pairError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, s.pairingJSON(r.Context(), v))
}

// myPairing returns the user's session (running, or ended a little while ago), so a reload
// finds it again.
func (s *Server) myPairing(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.pairer(w)
	if !ok {
		return
	}
	v, ok := p.Pairing(u.ID)
	if !ok {
		writeError(w, http.StatusNotFound, "no pairing session")
		return
	}
	writeJSON(w, http.StatusOK, s.pairingJSON(r.Context(), v))
}

func (s *Server) getPairing(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.pairer(w)
	if !ok {
		return
	}
	v, err := p.PairingByID(r.PathValue("id"), u.ID)
	if err != nil {
		s.pairError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.pairingJSON(r.Context(), v))
}

// pairingEvents streams the session as Server-Sent Events:
//
//	event: session   data: the session, as GET /api/pairing/{id} (first at once, then on each change)
//	event: end       the session is over (done, failed, canceled, expired)
func (s *Server) pairingEvents(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.pairer(w)
	if !ok {
		return
	}
	ch, stop, err := p.PairEvents(r.PathValue("id"), u.ID)
	if err != nil {
		s.pairError(w, r, err)
		return
	}
	defer stop()
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // a stream outlives the server's write timeout
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		case v := <-ch:
			b, _ := json.Marshal(s.pairingJSON(r.Context(), v))
			if _, err := fmt.Fprintf(w, "event: session\ndata: %s\n\n", b); err != nil {
				return
			}
			if v.Terminal() {
				fmt.Fprint(w, "event: end\ndata: {}\n\n")
				_ = rc.Flush()
				return
			}
		}
		_ = rc.Flush()
	}
}

// pairingAction runs one step of the session and answers with the session afterwards.
func (s *Server) pairingAction(w http.ResponseWriter, r *http.Request, u store.User, act func(p Pairing, id string) error) {
	p, ok := s.pairer(w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	if err := act(p, id); err != nil {
		s.pairError(w, r, err)
		return
	}
	v, err := p.PairingByID(id, u.ID)
	if err != nil {
		s.pairError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.pairingJSON(r.Context(), v))
}

func (s *Server) pairingScan(w http.ResponseWriter, r *http.Request, u store.User) {
	s.pairingAction(w, r, u, func(p Pairing, id string) error { return p.PairScan(id, u.ID) })
}

func (s *Server) pairingSelect(w http.ResponseWriter, r *http.Request, u store.User) {
	var req struct {
		Address string `json:"address"`
		Repair  bool   `json:"repair"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.pairingAction(w, r, u, func(p Pairing, id string) error { return p.PairSelect(id, u.ID, req.Address, req.Repair) })
}

func (s *Server) pairingConfirm(w http.ResponseWriter, r *http.Request, u store.User) {
	var req struct {
		Match *bool `json:"match"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Match == nil {
		writeError(w, http.StatusBadRequest, `say whether the codes match: {"match": true}`)
		return
	}
	s.pairingAction(w, r, u, func(p Pairing, id string) error { return p.PairConfirm(id, u.ID, *req.Match) })
}

func (s *Server) pairingPasskey(w http.ResponseWriter, r *http.Request, u store.User) {
	var req struct {
		Passkey *int64 `json:"passkey"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Passkey == nil || *req.Passkey < 0 || *req.Passkey > 999999 {
		writeError(w, http.StatusBadRequest, "the code has six digits")
		return
	}
	s.pairingAction(w, r, u, func(p Pairing, id string) error { return p.PairPasskey(id, u.ID, uint32(*req.Passkey)) })
}

// pairingSetup adds the identified watch: {nickname, garmin_connect, include_synced}.
func (s *Server) pairingSetup(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.pairer(w)
	if !ok {
		return
	}
	var req struct {
		Nickname      string `json:"nickname"`
		GarminConnect bool   `json:"garmin_connect"`
		IncludeSynced bool   `json:"include_synced"`
	}
	if !decode(w, r, &req) {
		return
	}
	n := strings.TrimSpace(req.Nickname)
	if utf8.RuneCountInString(n) > 60 || strings.ContainsAny(n, "\x00\r\n") {
		writeError(w, http.StatusBadRequest, "the name must be at most 60 characters on one line")
		return
	}
	v, err := p.PairSetup(r.Context(), r.PathValue("id"), u.ID,
		nodehub.PairSetupRequest{Nickname: n, GarminConnect: req.GarminConnect, IncludeSynced: req.IncludeSynced})
	if err != nil {
		s.pairError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.pairingJSON(r.Context(), v))
}

func (s *Server) cancelPairing(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.pairer(w)
	if !ok {
		return
	}
	if err := p.CancelPairing(r.PathValue("id"), u.ID, false); err != nil {
		s.pairError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// adminPairing tells admins who is adding a watch now: {id, state, started_at, starter, owner}
// (identity only), or 404.
func (s *Server) adminPairing(w http.ResponseWriter, r *http.Request, _ store.User) {
	p, ok := s.pairer(w)
	if !ok {
		return
	}
	v, ok := p.RunningPairing()
	if !ok {
		writeError(w, http.StatusNotFound, "no pairing session runs")
		return
	}
	who := func(id int64) map[string]any {
		m := map[string]any{"id": id}
		if u, err := s.st.User(r.Context(), id); err == nil {
			m["email"], m["name"] = u.Email, u.DisplayName
		}
		return m
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": v.ID, "state": v.State, "started_at": v.Started.Unix(),
		"starter": who(v.Starter), "owner": who(v.Owner)})
}

// adminCancelPairing ends whatever pairing session runs (a stuck one, someone else's).
func (s *Server) adminCancelPairing(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.pairer(w)
	if !ok {
		return
	}
	if err := p.CancelPairing("", u.ID, true); err != nil {
		s.pairError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
