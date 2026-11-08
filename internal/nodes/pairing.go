// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package nodes

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/nodeproto"
)

// ErrNoPairing is returned for Add a watch when no computer here can pair a watch for the account.
var ErrNoPairing = errors.New("no computer here can pair a watch for this account: Add a watch runs on a computer " +
	"with Bluetooth (goarmin serve -with-sync, or a sync node signed in to this server)")

// Codes of a relayed session that the server ended itself.
const (
	CodeNodeOffline = "node_offline" // the node stopped answering
	CodeNodeGone    = "node_gone"    // the node no longer has the session (it restarted, or was signed out)
)

// Add a watch through a sync node: the session runs on the node, which holds Bluetooth, and the
// server keeps a mirror of it for the browser. Every step the browser takes becomes a command
// to the node, which answers with the session and pushes each later change (PairPush). Whose a
// watch is, and adding it to an account, the node asks the server (the node API's pairing
// routes), so ownership is decided where the database is. One session runs at a time on the
// server: this server's own (the in-process hub) or one node's.

// mirror is a pairing session that runs on a sync node, as the server relays it.
type mirror struct {
	r              *remote
	id             string
	starter, owner int64
	v              nodehub.PairView
	subs           map[chan nodehub.PairView]bool
	viewers        int
	seen           time.Time // someone looked at it here last
	ended          time.Time
	endedHere      bool // the server ended it (its node went away): later views from the node are stale
	done           chan struct{}
}

// viewLocked is m's session with its node's name.
func (g *Registry) viewLocked(m *mirror) nodehub.PairView {
	v := m.v
	v.Node, v.NodeID = m.r.Name, m.r.ID
	return v
}

// notifyMirrorLocked hands each browser stream the newest view, replacing one it has not read.
func (g *Registry) notifyMirrorLocked(m *mirror) {
	v := g.viewLocked(m)
	for ch := range m.subs {
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- v:
		default:
		}
	}
}

// setMirrorLocked takes a view of m's session from its node, unless the one kept is newer.
func (g *Registry) setMirrorLocked(m *mirror, v nodehub.PairView) {
	if v.ID != m.id || v.Version < m.v.Version || m.v.Terminal() {
		return
	}
	m.v = v
	if v.Terminal() {
		g.closeMirrorLocked(m)
	}
	g.notifyMirrorLocked(m)
}

func (g *Registry) closeMirrorLocked(m *mirror) {
	if m.ended.IsZero() {
		m.ended = g.opt.Now()
		close(m.done)
	}
}

// endMirrorLocked ends a relayed session here, without its node.
func (g *Registry) endMirrorLocked(m *mirror, state, code, msg string) {
	if m.v.Terminal() {
		return
	}
	m.v.State, m.v.ErrorCode, m.v.Error, m.v.Retry = state, code, msg, false
	m.v.Prompt, m.v.Discovering = nil, false
	m.v.Version++
	m.endedHere = true
	g.closeMirrorLocked(m)
	g.notifyMirrorLocked(m)
	g.log.Info("nodes: relayed pairing session ended", "node", m.r.ID, "state", state, "code", code)
}

// checkLocked ends m when its node went offline.
func (g *Registry) checkLocked(m *mirror) {
	if !m.v.Terminal() && !g.onlineLocked(m.r) {
		g.endMirrorLocked(m, nodehub.PairFailed, CodeNodeOffline,
			fmt.Sprintf("The computer that pairs (%s) went offline. Start again when it is back.", m.r.Name))
	}
}

// goneLocked ends m because its node no longer has the session.
func (g *Registry) goneLocked(m *mirror) {
	g.endMirrorLocked(m, nodehub.PairFailed, CodeNodeGone,
		fmt.Sprintf("The computer that pairs (%s) no longer has this attempt (it may have restarted). Start again.", m.r.Name))
}

// activeLocked returns the relayed session if it still runs.
func (g *Registry) activeLocked() *mirror {
	m := g.mirror
	if m == nil {
		return nil
	}
	g.checkLocked(m)
	if m.v.Terminal() {
		return nil
	}
	return m
}

// keep watches a relayed session until it ends: it notices its node going offline, and tells
// the node every PairTouch that someone looks at the session here, so the node's watchdog
// (nobody looking for a few minutes) counts the people at the browser.
func (g *Registry) keep(m *mirror) {
	t := time.NewTicker(max(g.opt.PairTouch/4, 50*time.Millisecond))
	defer t.Stop()
	var touched time.Time
	for {
		select {
		case <-m.done:
			return
		case <-t.C:
		}
		g.mu.Lock()
		g.checkLocked(m)
		now := g.opt.Now()
		touch := !m.v.Terminal() && now.Sub(touched) >= g.opt.PairTouch &&
			(m.viewers > 0 || now.Sub(m.seen) < g.opt.PairTouch)
		g.mu.Unlock()
		if !touch {
			continue
		}
		touched = now
		ctx, cancel := context.WithTimeout(context.Background(), g.opt.CallTimeout)
		res, err := g.call(ctx, m.r, nodeproto.Command{Kind: nodeproto.CmdPairTouch, Session: m.id})
		cancel()
		g.mu.Lock()
		if res.Pairing != nil { // the node's view: catches up on an end whose push got lost
			g.setMirrorLocked(m, *res.Pairing)
		}
		if errors.Is(err, nodehub.ErrNoSession) {
			g.goneLocked(m)
		}
		g.mu.Unlock()
	}
}

// CanPair reports whether Add a watch is possible here: this server pairs, or a sync node is
// online.
func (g *Registry) CanPair() bool {
	if g.opt.Local != nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, r := range g.remotes {
		if g.onlineLocked(r) {
			return true
		}
	}
	return false
}

// PairComputer is a computer that can pair a watch.
type PairComputer struct {
	Node    int64  // 0: this server
	Name    string // the sync node's name (empty for this server)
	Adapter *nodehub.Bluetooth
}

// mayPair reports whether node r may pair a watch for owner: one of its own account's, or
// anyone's for an admin's node (the ones it may sync).
func mayPair(r *remote, owner int64) bool { return owner != 0 && (r.User == owner || r.Admin) }

// PairComputers lists the computers that can pair a watch for owner now: this server, then the
// online sync nodes that may sync owner's watches, by name.
func (g *Registry) PairComputers(ctx context.Context, owner int64) []PairComputer {
	out := []PairComputer{}
	if g.opt.Local != nil {
		c := PairComputer{}
		if b, err := g.opt.Local.Bluetooth(ctx); err == nil {
			c.Adapter = &b
		}
		out = append(out, c)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	var rs []PairComputer
	for _, r := range g.remotes {
		if !g.onlineLocked(r) || !mayPair(r, owner) {
			continue
		}
		c := PairComputer{Node: r.ID, Name: r.Name}
		if r.adapter != nil {
			b := *r.adapter
			b.Busy = r.busy
			c.Adapter = &b
		}
		rs = append(rs, c)
	}
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].Name != rs[j].Name {
			return rs[i].Name < rs[j].Name
		}
		return rs[i].Node < rs[j].Node
	})
	return append(out, rs...)
}

// pairNodeLocked picks the node that pairs for owner: node, if it may and is online; for node 0
// the online one that may, with Bluetooth on, heard from last.
func (g *Registry) pairNodeLocked(node, owner int64) *remote {
	if node != 0 {
		if r := g.remotes[node]; r != nil && g.onlineLocked(r) && mayPair(r, owner) {
			return r
		}
		return nil
	}
	var best *remote
	on := func(r *remote) bool { return r.adapter != nil && r.adapter.Powered }
	for _, r := range g.remotes {
		if !g.onlineLocked(r) || !mayPair(r, owner) {
			continue
		}
		if best == nil || (on(r) && !on(best)) || (on(r) == on(best) && r.seen.After(best.seen)) {
			best = r
		}
	}
	return best
}

// StartPairing implements the API's pairing: the registry picks the computer.
func (g *Registry) StartPairing(starter, owner int64) (nodehub.PairView, bool, error) {
	return g.StartPairingOn(context.Background(), starter, owner, 0)
}

// StartPairingOn opens the pairing session on a computer: node 0 lets the registry choose (this
// server if it pairs, else a sync node that may pair for owner). A running session of the same
// starter comes back with created false; anyone else's is nodehub.ErrPairingActive.
func (g *Registry) StartPairingOn(ctx context.Context, starter, owner, node int64) (nodehub.PairView, bool, error) {
	g.pairMu.Lock()
	defer g.pairMu.Unlock()
	g.mu.Lock()
	if m := g.activeLocked(); m != nil {
		defer g.mu.Unlock()
		if m.starter != starter {
			return nodehub.PairView{}, false, nodehub.ErrPairingActive
		}
		m.seen = g.opt.Now()
		return g.viewLocked(m), false, nil
	}
	g.mu.Unlock()
	if l := g.opt.Local; l != nil {
		v, running := l.RunningPairing()
		switch {
		case running && v.Starter != starter:
			return nodehub.PairView{}, false, nodehub.ErrPairingActive
		case running:
			v, err := l.PairingByID(v.ID, starter)
			return v, false, err
		case node == 0:
			return l.StartPairing(starter, owner)
		}
	}
	g.mu.Lock()
	r := g.pairNodeLocked(node, owner)
	if r == nil {
		g.mu.Unlock()
		return nodehub.PairView{}, false, ErrNoPairing
	}
	g.starting = r.ID
	g.mu.Unlock()
	res, err := g.call(ctx, r, nodeproto.Command{Kind: nodeproto.CmdPairStart, Starter: starter, Owner: owner})
	g.mu.Lock()
	defer g.mu.Unlock()
	g.starting = 0
	if err != nil {
		return nodehub.PairView{}, false, err
	}
	if res.Pairing == nil {
		return nodehub.PairView{}, false, errors.New("the sync node did not say how the pairing started")
	}
	v := *res.Pairing
	m := &mirror{r: r, id: v.ID, starter: v.Starter, owner: v.Owner, v: v, subs: map[chan nodehub.PairView]bool{},
		seen: g.opt.Now(), done: make(chan struct{})}
	g.mirror = m
	if v.Terminal() {
		g.closeMirrorLocked(m)
	} else {
		go g.keep(m)
	}
	g.log.Info("nodes: pairing through a sync node", "node", r.ID, "by", starter, "owner", owner)
	return g.viewLocked(m), res.Created, nil
}

// relayedLocked finds the relayed session id for user. found false: id is not a node's session.
func (g *Registry) relayedLocked(id string, user int64) (m *mirror, found bool, err error) {
	m = g.mirror
	if m == nil || m.id != id {
		return nil, false, nil
	}
	if m.starter != user {
		return nil, true, nodehub.ErrNoSession
	}
	g.checkLocked(m)
	m.seen = g.opt.Now()
	return m, true, nil
}

// Pairing implements the API's pairing: user's session, the newer of a node's and this server's.
func (g *Registry) Pairing(user int64) (nodehub.PairView, bool) {
	var mv nodehub.PairView
	mok := false
	g.mu.Lock()
	if m := g.mirror; m != nil && m.starter == user {
		g.checkLocked(m)
		if !m.v.Terminal() || g.opt.Now().Sub(m.ended) <= g.opt.PairKeep {
			m.seen = g.opt.Now()
			mv, mok = g.viewLocked(m), true
		}
	}
	g.mu.Unlock()
	if g.opt.Local != nil {
		if lv, ok := g.opt.Local.Pairing(user); ok && (!mok || lv.Started.After(mv.Started)) {
			return lv, true
		}
	}
	return mv, mok
}

// PairingByID implements the API's pairing.
func (g *Registry) PairingByID(id string, user int64) (nodehub.PairView, error) {
	g.mu.Lock()
	m, found, err := g.relayedLocked(id, user)
	if found {
		defer g.mu.Unlock()
		if err != nil {
			return nodehub.PairView{}, err
		}
		return g.viewLocked(m), nil
	}
	g.mu.Unlock()
	if g.opt.Local == nil {
		return nodehub.PairView{}, nodehub.ErrNoSession
	}
	return g.opt.Local.PairingByID(id, user)
}

// PairEvents implements the API's pairing. A stream of a node's session counts as a viewer.
func (g *Registry) PairEvents(id string, user int64) (<-chan nodehub.PairView, func(), error) {
	g.mu.Lock()
	m, found, err := g.relayedLocked(id, user)
	if !found {
		g.mu.Unlock()
		if g.opt.Local == nil {
			return nil, nil, nodehub.ErrNoSession
		}
		return g.opt.Local.PairEvents(id, user)
	}
	defer g.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	ch := make(chan nodehub.PairView, 1)
	ch <- g.viewLocked(m)
	m.subs[ch] = true
	m.viewers++
	stopped := false
	stop := func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if !stopped {
			stopped = true
			delete(m.subs, ch)
			m.viewers--
			m.seen = g.opt.Now()
		}
	}
	return ch, stop, nil
}

// pairStep sends one step of a node's session id to the node and keeps the session it answers
// with. relayed false: id is not a node's session (this server's hub handles it).
func (g *Registry) pairStep(ctx context.Context, id string, user int64, cmd nodeproto.Command) (v nodehub.PairView, relayed bool, err error) {
	g.mu.Lock()
	m, found, err := g.relayedLocked(id, user)
	if !found || err != nil {
		g.mu.Unlock()
		return nodehub.PairView{}, found, err
	}
	if m.v.Terminal() {
		v = g.viewLocked(m)
		g.mu.Unlock()
		return v, true, fmt.Errorf("%w: This attempt has ended. Start again.", nodehub.ErrWrongState)
	}
	r := m.r
	g.mu.Unlock()
	cmd.Session, cmd.User = id, m.starter
	res, err := g.call(ctx, r, cmd)
	g.mu.Lock()
	defer g.mu.Unlock()
	if res.Pairing != nil {
		g.setMirrorLocked(m, *res.Pairing)
	}
	switch {
	case errors.Is(err, ErrOffline):
		g.checkLocked(m)
	case errors.Is(err, nodehub.ErrNoSession):
		g.goneLocked(m)
	}
	return g.viewLocked(m), true, err
}

// PairScan implements the API's pairing.
func (g *Registry) PairScan(id string, user int64) error {
	if _, relayed, err := g.pairStep(context.Background(), id, user, nodeproto.Command{Kind: nodeproto.CmdPairScan}); relayed {
		return err
	}
	if g.opt.Local == nil {
		return nodehub.ErrNoSession
	}
	return g.opt.Local.PairScan(id, user)
}

// PairSelect implements the API's pairing.
func (g *Registry) PairSelect(id string, user int64, addr string, repair bool) error {
	cmd := nodeproto.Command{Kind: nodeproto.CmdPairSelect, Address: addr, Repair: repair}
	if _, relayed, err := g.pairStep(context.Background(), id, user, cmd); relayed {
		return err
	}
	if g.opt.Local == nil {
		return nodehub.ErrNoSession
	}
	return g.opt.Local.PairSelect(id, user, addr, repair)
}

// PairConfirm implements the API's pairing.
func (g *Registry) PairConfirm(id string, user int64, match bool) error {
	cmd := nodeproto.Command{Kind: nodeproto.CmdPairConfirm, Match: match}
	if _, relayed, err := g.pairStep(context.Background(), id, user, cmd); relayed {
		return err
	}
	if g.opt.Local == nil {
		return nodehub.ErrNoSession
	}
	return g.opt.Local.PairConfirm(id, user, match)
}

// PairPasskey implements the API's pairing.
func (g *Registry) PairPasskey(id string, user int64, passkey uint32) error {
	cmd := nodeproto.Command{Kind: nodeproto.CmdPairPasskey, Passkey: passkey}
	if _, relayed, err := g.pairStep(context.Background(), id, user, cmd); relayed {
		return err
	}
	if g.opt.Local == nil {
		return nodehub.ErrNoSession
	}
	return g.opt.Local.PairPasskey(id, user, passkey)
}

// PairSetup implements the API's pairing: the node adds the watch (asking the server to claim
// it) and starts its first sync.
func (g *Registry) PairSetup(ctx context.Context, id string, user int64, req nodehub.PairSetupRequest) (nodehub.PairView, error) {
	if v, relayed, err := g.pairStep(ctx, id, user, nodeproto.Command{Kind: nodeproto.CmdPairSetup, Setup: &req}); relayed {
		return v, err
	}
	if g.opt.Local == nil {
		return nodehub.PairView{}, nodehub.ErrNoSession
	}
	return g.opt.Local.PairSetup(ctx, id, user, req)
}

// CancelPairing implements the API's pairing: its starter's session, or (admin) any; id ""
// means the running one. A node that does not answer still has its session ended here.
func (g *Registry) CancelPairing(id string, user int64, admin bool) error {
	g.mu.Lock()
	m := g.mirror
	mine := m != nil && (m.id == id || (id == "" && admin && g.activeLocked() == m))
	if !mine {
		g.mu.Unlock()
		if g.opt.Local == nil {
			return nodehub.ErrNoSession
		}
		return g.opt.Local.CancelPairing(id, user, admin)
	}
	if m.starter != user && !admin {
		g.mu.Unlock()
		return nodehub.ErrNoSession
	}
	if m.v.Terminal() {
		g.mu.Unlock()
		return nil
	}
	r := m.r
	g.mu.Unlock()
	res, _ := g.call(context.Background(), r, nodeproto.Command{Kind: nodeproto.CmdPairCancel, Session: m.id, User: user, Admin: admin})
	g.mu.Lock()
	defer g.mu.Unlock()
	if res.Pairing != nil {
		g.setMirrorLocked(m, *res.Pairing)
	}
	g.endMirrorLocked(m, nodehub.PairCanceled, "", "")
	return nil
}

// RunningPairing implements the API's pairing.
func (g *Registry) RunningPairing() (nodehub.PairView, bool) {
	g.mu.Lock()
	if m := g.activeLocked(); m != nil {
		defer g.mu.Unlock()
		return g.viewLocked(m), true
	}
	g.mu.Unlock()
	if g.opt.Local == nil {
		return nodehub.PairView{}, false
	}
	return g.opt.Local.RunningPairing()
}

// RelayedSession returns the session that runs on node now, if one does: the node API's pairing
// routes answer only within it.
func (g *Registry) RelayedSession(node int64) (nodehub.PairView, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if m := g.activeLocked(); m != nil && m.r.ID == node {
		return g.viewLocked(m), true
	}
	return nodehub.PairView{}, false
}

// PairPush takes a node's pairing session after a change. A session the server does not relay
// (it restarted, or ended the session itself) is cancelled on the node.
func (g *Registry) PairPush(info NodeInfo, v nodehub.PairView) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := g.nodeLocked(info)
	if m := g.mirror; m != nil && m.r.ID == info.ID && m.id == v.ID {
		if m.endedHere && !v.Terminal() {
			g.sendLocked(r, nodeproto.Command{Kind: nodeproto.CmdPairCancel, Session: v.ID, User: v.Starter, Admin: true})
			return
		}
		g.setMirrorLocked(m, v)
		return
	}
	if !v.Terminal() && g.starting != info.ID {
		g.sendLocked(r, nodeproto.Command{Kind: nodeproto.CmdPairCancel, Session: v.ID, User: v.Starter, Admin: true})
	}
}

// BluetoothOf reports a computer's adapter: node 0 is Bluetooth's usual answer, else that node's.
func (g *Registry) BluetoothOf(ctx context.Context, node int64) (nodehub.Bluetooth, error) {
	if node == 0 {
		return g.Bluetooth(ctx)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	r := g.remotes[node]
	switch {
	case r == nil:
		return nodehub.Bluetooth{}, ErrNoNode
	case !g.onlineLocked(r):
		return nodehub.Bluetooth{}, g.offline(r)
	case r.adapter == nil:
		return nodehub.Bluetooth{}, errors.New("the sync node has not reported its Bluetooth yet")
	}
	b := *r.adapter
	b.Busy = r.busy
	return b, nil
}

// PowerOnOf switches a computer's adapter on: node 0 as PowerOn, else that node's.
func (g *Registry) PowerOnOf(ctx context.Context, node int64) error {
	if node == 0 {
		return g.PowerOn(ctx)
	}
	g.mu.Lock()
	r := g.remotes[node]
	g.mu.Unlock()
	if r == nil {
		return ErrNoNode
	}
	_, err := g.call(ctx, r, nodeproto.Command{Kind: nodeproto.CmdPowerOn})
	return err
}
