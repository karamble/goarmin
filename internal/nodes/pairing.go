// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package nodes

import (
	"context"
	"errors"

	"github.com/karamble/goarmin/internal/nodehub"
)

// ErrNoPairing is returned for Add a watch when no computer here can pair a watch.
var ErrNoPairing = errors.New("no computer here can pair a watch: Add a watch runs where the watch is (goarmin serve -with-sync)")

// CanPair reports whether Add a watch is possible: the in-process hub pairs.
func (g *Registry) CanPair() bool { return g.opt.Local != nil }

func (g *Registry) hub() (*nodehub.Hub, error) {
	if g.opt.Local == nil {
		return nil, ErrNoPairing
	}
	return g.opt.Local, nil
}

// StartPairing implements the API's pairing.
func (g *Registry) StartPairing(starter, owner int64) (nodehub.PairView, bool, error) {
	h, err := g.hub()
	if err != nil {
		return nodehub.PairView{}, false, err
	}
	return h.StartPairing(starter, owner)
}

// Pairing implements the API's pairing.
func (g *Registry) Pairing(user int64) (nodehub.PairView, bool) {
	if g.opt.Local == nil {
		return nodehub.PairView{}, false
	}
	return g.opt.Local.Pairing(user)
}

// PairingByID implements the API's pairing.
func (g *Registry) PairingByID(id string, user int64) (nodehub.PairView, error) {
	h, err := g.hub()
	if err != nil {
		return nodehub.PairView{}, nodehub.ErrNoSession
	}
	return h.PairingByID(id, user)
}

// PairEvents implements the API's pairing.
func (g *Registry) PairEvents(id string, user int64) (<-chan nodehub.PairView, func(), error) {
	h, err := g.hub()
	if err != nil {
		return nil, nil, nodehub.ErrNoSession
	}
	return h.PairEvents(id, user)
}

// PairScan implements the API's pairing.
func (g *Registry) PairScan(id string, user int64) error {
	h, err := g.hub()
	if err != nil {
		return nodehub.ErrNoSession
	}
	return h.PairScan(id, user)
}

// PairSelect implements the API's pairing.
func (g *Registry) PairSelect(id string, user int64, addr string, repair bool) error {
	h, err := g.hub()
	if err != nil {
		return nodehub.ErrNoSession
	}
	return h.PairSelect(id, user, addr, repair)
}

// PairConfirm implements the API's pairing.
func (g *Registry) PairConfirm(id string, user int64, match bool) error {
	h, err := g.hub()
	if err != nil {
		return nodehub.ErrNoSession
	}
	return h.PairConfirm(id, user, match)
}

// PairPasskey implements the API's pairing.
func (g *Registry) PairPasskey(id string, user int64, passkey uint32) error {
	h, err := g.hub()
	if err != nil {
		return nodehub.ErrNoSession
	}
	return h.PairPasskey(id, user, passkey)
}

// PairSetup implements the API's pairing.
func (g *Registry) PairSetup(ctx context.Context, id string, user int64, req nodehub.PairSetupRequest) (nodehub.PairView, error) {
	h, err := g.hub()
	if err != nil {
		return nodehub.PairView{}, nodehub.ErrNoSession
	}
	return h.PairSetup(ctx, id, user, req)
}

// CancelPairing implements the API's pairing.
func (g *Registry) CancelPairing(id string, user int64, admin bool) error {
	h, err := g.hub()
	if err != nil {
		return nodehub.ErrNoSession
	}
	return h.CancelPairing(id, user, admin)
}

// RunningPairing implements the API's pairing.
func (g *Registry) RunningPairing() (nodehub.PairView, bool) {
	if g.opt.Local == nil {
		return nodehub.PairView{}, false
	}
	return g.opt.Local.RunningPairing()
}
