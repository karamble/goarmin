// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package nodes is the server's side of its sync nodes: the computers that sync watches near
// them and upload the files. The Registry knows which node syncs which watch, keeps what each
// reported (status, Bluetooth), queues commands for them (sync now, live, files, unpair, …) and
// relays live readings to the dashboard. It is the API's sync role (api.Node): each call goes to
// the node that syncs the watch, or to the in-process hub of goarmin serve -with-sync.
package nodes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/nodeproto"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/realtime"
)

// reportGrace is how long after a server start a watch's sync node may take to report it: the
// node notices its broken command poll, reconnects and says hello.
const reportGrace = 3 * time.Minute

// unreported is the error for a watch no computer that syncs here has reported since the
// server started. It is nodehub.ErrUnknown to errors.Is, with words that say why.
type unreported struct {
	started time.Time
	fresh   bool // the server started moments ago: its nodes have not all reported yet
}

func (e unreported) Error() string {
	if e.fresh {
		return "the server restarted at " + e.started.Format("15:04:05") +
			" and the computer that syncs this watch has not reported in yet; it does within a minute, so try again shortly"
	}
	return "no computer that syncs here knows this watch: it is not paired with one (Devices › Add a watch), or its sync node is not running"
}

func (unreported) Is(target error) bool { return target == nodehub.ErrUnknown }

// unknown is the error for a watch nothing here reported.
func (g *Registry) unknown() error {
	return unreported{started: g.started, fresh: g.opt.Now().Sub(g.started) < reportGrace}
}

// ErrOffline is returned for a watch whose sync node has not been heard from for a while.
var ErrOffline = errors.New("the sync node is offline")

// ErrNoAnswer is returned when a node took a command but did not answer in time.
var ErrNoAnswer = errors.New("the sync node did not answer in time")

// ErrNoNode is returned when no sync node (and no in-process sync role) is there.
var ErrNoNode = errors.New("no sync node is connected")

// OfflineAfter is how long a node may stay silent before it counts as offline. A node polls
// for commands every nodeproto.CommandWait, so a live one is never silent that long.
const OfflineAfter = 75 * time.Second

// Options configures a Registry.
type Options struct {
	// Local is the in-process sync role (goarmin serve -with-sync), or nil.
	Local  *nodehub.Hub
	Logger *slog.Logger
	// Now is the clock (time.Now by default).
	Now func() time.Time
	// LiveLinger keeps a relayed live link open this long after its last viewer left (30 s);
	// LiveIdle ends one whose node stopped sending (90 s).
	LiveLinger, LiveIdle time.Duration
	// CallTimeout bounds how long a command waits for the node's answer (30 s).
	CallTimeout time.Duration
	// PairTouch is how often a node's pairing session is told someone looks at it here (1 min);
	// PairKeep is how long an ended one stays readable, so a reload lands on its end (10 min).
	PairTouch, PairKeep time.Duration
}

// Registry is the server's view of its sync nodes.
type Registry struct {
	opt Options
	log *slog.Logger

	mu      sync.Mutex
	remotes map[int64]*remote
	byAddr  map[string]int64 // watch address → the node that reported it last

	started time.Time // when the server started: nodes report their watches after that

	pairMu   sync.Mutex // one pairing start at a time
	mirror   *mirror    // the last pairing session that ran on a node
	starting int64      // the node a pairing start waits for (its pushes are not orphans)
}

// New builds a registry.
func New(opt Options) *Registry {
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.LiveLinger <= 0 {
		opt.LiveLinger = 30 * time.Second
	}
	if opt.LiveIdle <= 0 {
		opt.LiveIdle = 90 * time.Second
	}
	if opt.CallTimeout <= 0 {
		opt.CallTimeout = 30 * time.Second
	}
	if opt.PairTouch <= 0 {
		opt.PairTouch = time.Minute
	}
	if opt.PairKeep <= 0 {
		opt.PairKeep = 10 * time.Minute
	}
	return &Registry{opt: opt, log: opt.Logger, remotes: map[int64]*remote{}, byAddr: map[string]int64{}, started: opt.Now()}
}

// NodeInfo is who a node is, as the API resolved its token.
type NodeInfo struct {
	ID    int64
	Name  string
	User  int64
	Admin bool // its account is an admin: it may sync every account's watches
}

// remote is one sync node as the server knows it.
type remote struct {
	NodeInfo
	seen    time.Time
	version string
	adapter *nodehub.Bluetooth
	busy    *nodehub.Holder
	watches map[string]nodeproto.Watch // by address
	status  map[string]nodehub.Status
	queue   []nodeproto.Command
	wake    chan struct{} // closed when the queue gets a command
	waiting map[string]chan nodeproto.CommandResult
	lives   map[string]*relay
}

// node returns (making it if needed) the remote for info, refreshed; r.mu held.
func (g *Registry) nodeLocked(info NodeInfo) *remote {
	r := g.remotes[info.ID]
	if r == nil {
		r = &remote{watches: map[string]nodeproto.Watch{}, status: map[string]nodehub.Status{},
			wake: make(chan struct{}), waiting: map[string]chan nodeproto.CommandResult{}, lives: map[string]*relay{}}
		g.remotes[info.ID] = r
	}
	r.NodeInfo = info
	r.seen = g.opt.Now()
	return r
}

func (g *Registry) onlineLocked(r *remote) bool {
	return g.opt.Now().Sub(r.seen) < OfflineAfter
}

// --- What nodes report (the node API) ---

// Hello records the watches a node is paired with, its version and its adapter.
func (g *Registry) Hello(info NodeInfo, req nodeproto.HelloRequest) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := g.nodeLocked(info)
	r.version, r.adapter = req.Version, req.Adapter
	r.watches = map[string]nodeproto.Watch{}
	for _, w := range req.Watches {
		r.watches[w.Address] = w
		g.byAddr[w.Address] = info.ID
	}
	for addr, id := range g.byAddr {
		if _, still := r.watches[addr]; id == info.ID && !still {
			delete(g.byAddr, addr) // unpaired on the node
		}
	}
}

// Progress records a node's state: each watch's status, who holds its Bluetooth, its adapter.
func (g *Registry) Progress(info NodeInfo, req nodeproto.ProgressRequest) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := g.nodeLocked(info)
	for _, st := range req.Status {
		if _, ok := r.watches[st.Address]; ok {
			r.status[st.Address] = st
		}
	}
	r.busy = req.Busy
	if req.Adapter != nil {
		r.adapter = req.Adapter
	}
}

// Poll hands a node its waiting commands, waiting up to nodeproto.CommandWait (or ctx) for one.
func (g *Registry) Poll(ctx context.Context, info NodeInfo) []nodeproto.Command {
	g.mu.Lock()
	r := g.nodeLocked(info)
	if len(r.queue) == 0 {
		wake := r.wake
		g.mu.Unlock()
		t := time.NewTimer(nodeproto.CommandWait)
		defer t.Stop()
		select {
		case <-wake:
		case <-t.C:
		case <-ctx.Done():
		}
		g.mu.Lock()
		r.seen = g.opt.Now()
	}
	cmds := r.queue
	r.queue = nil
	g.mu.Unlock()
	return cmds
}

// Result delivers a node's answer to a command.
func (g *Registry) Result(info NodeInfo, id string, res nodeproto.CommandResult) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := g.nodeLocked(info)
	if ch, ok := r.waiting[id]; ok {
		delete(r.waiting, id)
		ch <- res
	}
}

// Drop forgets a node (its token was revoked).
func (g *Registry) Drop(id int64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := g.remotes[id]
	if r == nil {
		return
	}
	for addr, n := range g.byAddr {
		if n == id {
			delete(g.byAddr, addr)
		}
	}
	for _, l := range r.lives {
		l.endLocked(ErrOffline)
	}
	if m := g.mirror; m != nil && m.r == r {
		g.endMirrorLocked(m, nodehub.PairFailed, CodeNodeGone, "The computer that was pairing was signed out. Start again.")
	}
	close(r.wake) // a waiting poll returns now; the node's next request learns it was revoked
	delete(g.remotes, id)
}

// NodeState is what the dashboard's Sync nodes tab shows of a connected node.
type NodeState struct {
	Online   bool
	LastSeen time.Time
	Version  string
	Adapter  *nodehub.Bluetooth
	Busy     *nodehub.Holder
	Watches  []nodeproto.Watch
}

// State returns what the registry knows of a node since the server started (ok false: nothing).
func (g *Registry) State(id int64) (NodeState, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := g.remotes[id]
	if r == nil {
		return NodeState{}, false
	}
	st := NodeState{Online: g.onlineLocked(r), LastSeen: r.seen, Version: r.version, Adapter: r.adapter, Busy: r.busy}
	for _, w := range r.watches {
		st.Watches = append(st.Watches, w)
	}
	sort.Slice(st.Watches, func(i, j int) bool { return st.Watches[i].Address < st.Watches[j].Address })
	return st, true
}

// --- Commands ---

func newID() string {
	b := make([]byte, 9)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// call queues a command for the node that syncs addr (or the node r) and waits for its answer.
func (g *Registry) call(ctx context.Context, r *remote, cmd nodeproto.Command) (nodeproto.CommandResult, error) {
	cmd.ID = newID()
	ch := make(chan nodeproto.CommandResult, 1)
	g.mu.Lock()
	if !g.onlineLocked(r) {
		g.mu.Unlock()
		return nodeproto.CommandResult{}, g.offline(r)
	}
	r.queue = append(r.queue, cmd)
	r.waiting[cmd.ID] = ch
	close(r.wake)
	r.wake = make(chan struct{})
	g.mu.Unlock()
	t := time.NewTimer(g.opt.CallTimeout)
	defer t.Stop()
	select {
	case res := <-ch:
		return res, resultErr(res)
	case <-t.C:
	case <-ctx.Done():
	}
	g.mu.Lock()
	delete(r.waiting, cmd.ID)
	g.mu.Unlock()
	if ctx.Err() != nil {
		return nodeproto.CommandResult{}, ctx.Err()
	}
	return nodeproto.CommandResult{}, ErrNoAnswer
}

// send queues a command without waiting for its answer.
func (g *Registry) sendLocked(r *remote, cmd nodeproto.Command) {
	cmd.ID = newID()
	r.queue = append(r.queue, cmd)
	close(r.wake)
	r.wake = make(chan struct{})
}

// AddWatch routes a watch to the node that just added it (Add a watch), before its next hello.
func (g *Registry) AddWatch(info NodeInfo, w nodeproto.Watch) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := g.nodeLocked(info)
	r.watches[w.Address] = w
	g.byAddr[w.Address] = info.ID
}

// Refresh asks a node to say hello again soon (a watch's config changed).
func (g *Registry) Refresh(addr string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if r := g.remotes[g.byAddr[addr]]; r != nil {
		g.sendLocked(r, nodeproto.Command{Kind: nodeproto.CmdHello})
	}
}

// offline says since when; r.mu not held.
func (g *Registry) offline(r *remote) error {
	return fmt.Errorf("%w (%s, last seen %s)", ErrOffline, r.Name, r.seen.Format(time.DateTime))
}

// resultErr turns a node's answer into the hub's errors, so the API answers as it does for the
// in-process hub.
func resultErr(res nodeproto.CommandResult) error {
	switch res.Code {
	case "":
		if res.Error != "" {
			return errors.New(res.Error)
		}
		return nil
	case nodeproto.CodeBusy:
		h := nodehub.Holder{}
		if res.Busy != nil {
			h = *res.Busy
		}
		return &nodehub.BusyError{Holder: h}
	case nodeproto.CodeUnknown:
		return nodehub.ErrUnknown
	}
	if err, ok := pairErr(res); ok {
		return err
	}
	return errors.New(res.Error)
}

// pairCodes are the pairing errors that keep their meaning across the hop.
var pairCodes = []struct {
	code string
	err  error
}{
	{nodeproto.CodeNoSession, nodehub.ErrNoSession},
	{nodeproto.CodeOwnedElsewhere, nodehub.ErrOwnedElsewhere},
	{nodeproto.CodeNotListed, nodehub.ErrNotListed},
	{nodeproto.CodePairingActive, nodehub.ErrPairingActive},
	{nodeproto.CodeStopping, nodehub.ErrStopping},
	{nodeproto.CodeCannotPair, nodehub.ErrCannotPair},
}

// pairErr turns a pairing error code back into the hub's error; a wrong state keeps the
// sentence that says what to do.
func pairErr(res nodeproto.CommandResult) (error, bool) {
	if res.Code == nodeproto.CodeWrongState {
		msg := strings.TrimPrefix(res.Error, nodehub.ErrWrongState.Error())
		return fmt.Errorf("%w: %s", nodehub.ErrWrongState, strings.TrimPrefix(msg, ": ")), true
	}
	for _, c := range pairCodes {
		if res.Code == c.code {
			return c.err, true
		}
	}
	return nil, false
}

// ResultFor turns an error of the node's hub into a command result (the node's side).
func ResultFor(err error) nodeproto.CommandResult {
	var be *nodehub.BusyError
	switch {
	case err == nil:
		return nodeproto.CommandResult{}
	case errors.As(err, &be):
		h := be.Holder
		return nodeproto.CommandResult{Code: nodeproto.CodeBusy, Error: err.Error(), Busy: &h}
	case errors.Is(err, nodehub.ErrBusy):
		return nodeproto.CommandResult{Code: nodeproto.CodeBusy, Error: err.Error()}
	case errors.Is(err, nodehub.ErrUnknown):
		return nodeproto.CommandResult{Code: nodeproto.CodeUnknown, Error: err.Error()}
	case errors.Is(err, nodehub.ErrWrongState):
		return nodeproto.CommandResult{Code: nodeproto.CodeWrongState, Error: err.Error()}
	}
	for _, c := range pairCodes {
		if errors.Is(err, c.err) {
			return nodeproto.CommandResult{Code: c.code, Error: err.Error()}
		}
	}
	return nodeproto.CommandResult{Code: nodeproto.CodeFailed, Error: err.Error()}
}

// --- The API's sync role (api.Node) ---

// route finds who syncs addr: the in-process hub (local true), a node, or nobody.
func (g *Registry) route(addr string) (local bool, r *remote) {
	if g.opt.Local != nil && g.opt.Local.Known(addr) {
		return true, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return false, g.remotes[g.byAddr[addr]]
}

// routeUnit finds who syncs the watch with this unit number.
func (g *Registry) routeUnit(unit uint32) (local bool, r *remote) {
	g.mu.Lock()
	for _, rr := range g.remotes {
		for _, w := range rr.watches {
			if w.Unit == unit {
				g.mu.Unlock()
				return false, rr
			}
		}
	}
	g.mu.Unlock()
	return g.opt.Local != nil, nil
}

// Available reports whether anything syncs watches here: the in-process hub or a node heard
// from since the server started.
func (g *Registry) Available() bool {
	if g.opt.Local != nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.remotes) > 0
}

// Status returns how the watch at addr syncs.
func (g *Registry) Status(addr string) nodehub.Status {
	local, r := g.route(addr)
	if local {
		return g.opt.Local.Status(addr)
	}
	if r == nil {
		return nodehub.Status{Address: addr}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st := r.status[addr]
	st.Address, st.Node, st.LastSeen, st.Offline = addr, r.Name, r.seen, !g.onlineLocked(r)
	_, st.Live = r.lives[addr]
	return st
}

// Sync asks for a sync of a watch.
func (g *Registry) Sync(req nodehub.Request) error {
	local, r := g.route(req.Addr)
	switch {
	case local:
		return g.opt.Local.Sync(req)
	case r == nil:
		return g.unknown()
	}
	_, err := g.call(context.Background(), r, nodeproto.Command{Kind: nodeproto.CmdSync, Address: req.Addr, By: req.By,
		IncludeSynced: req.IncludeSynced, Delete: req.Delete})
	return err
}

// Files lists what a watch handed over, from its node's spool record.
func (g *Registry) Files(unit uint32) ([]fileaccess.StoredFile, error) {
	local, r := g.routeUnit(unit)
	switch {
	case r != nil:
		res, err := g.call(context.Background(), r, nodeproto.Command{Kind: nodeproto.CmdFiles, Unit: unit})
		return res.Files, err
	case local:
		return g.opt.Local.Files(unit)
	}
	return nil, ErrNoNode
}

// adapterNode picks the node whose Bluetooth the Devices page shows when there is no
// in-process hub: the online one heard from last.
func (g *Registry) adapterNode() *remote {
	g.mu.Lock()
	defer g.mu.Unlock()
	var best *remote
	for _, r := range g.remotes {
		if g.onlineLocked(r) && r.adapter != nil && (best == nil || r.seen.After(best.seen)) {
			best = r
		}
	}
	return best
}

// Bluetooth reports the sync computer's adapter: this server's, else a node's.
func (g *Registry) Bluetooth(ctx context.Context) (nodehub.Bluetooth, error) {
	if g.opt.Local != nil {
		return g.opt.Local.Bluetooth(ctx)
	}
	r := g.adapterNode()
	if r == nil {
		return nodehub.Bluetooth{}, ErrNoNode
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	b := *r.adapter
	b.Busy = r.busy
	return b, nil
}

// PowerOn switches the sync computer's adapter on.
func (g *Registry) PowerOn(ctx context.Context) error {
	if g.opt.Local != nil {
		return g.opt.Local.PowerOn(ctx)
	}
	r := g.adapterNode()
	if r == nil {
		return ErrNoNode
	}
	_, err := g.call(ctx, r, nodeproto.Command{Kind: nodeproto.CmdPowerOn})
	return err
}

// Known reports whether something here syncs the watch at addr.
func (g *Registry) Known(addr string) bool {
	local, r := g.route(addr)
	return local || r != nil
}

// Exclusive runs fn with the in-process Bluetooth held, if there is one; a node runs each
// command under its own lock.
func (g *Registry) Exclusive(ctx context.Context, kind string, fn func() error) error {
	if g.opt.Local != nil {
		return g.opt.Local.Exclusive(ctx, kind, fn)
	}
	return fn()
}

// ForgetLocked unpairs the watch at addr on whatever syncs it.
func (g *Registry) ForgetLocked(ctx context.Context, addr string) error {
	local, r := g.route(addr)
	switch {
	case local:
		return g.opt.Local.ForgetLocked(ctx, addr)
	case r == nil:
		return g.unknown()
	}
	_, err := g.call(ctx, r, nodeproto.Command{Kind: nodeproto.CmdForget, Address: addr})
	if err == nil {
		g.mu.Lock()
		delete(r.watches, addr)
		delete(r.status, addr)
		delete(g.byAddr, addr)
		g.mu.Unlock()
	}
	return err
}

// PurgeSpool empties the watch's spool wherever it is.
func (g *Registry) PurgeSpool(unit uint32) error {
	local, r := g.routeUnit(unit)
	switch {
	case r != nil:
		_, err := g.call(context.Background(), r, nodeproto.Command{Kind: nodeproto.CmdPurge, Unit: unit})
		return err
	case local:
		return g.opt.Local.PurgeSpool(unit)
	}
	return nil
}

// --- Live values through a node ---

// relay is a watch's live link on a node, as the server fans it out to its viewers.
type relay struct {
	g      *Registry
	r      *remote
	addr   string
	since  time.Time
	subs   map[chan realtime.Reading]bool
	recent []realtime.Reading
	linger *time.Timer
	idle   *time.Timer
	err    error
	ended  bool
}

// endLocked closes the relay for every viewer with err; g.mu held.
func (l *relay) endLocked(err error) {
	if l.ended {
		return
	}
	l.ended, l.err = true, err
	for ch := range l.subs {
		delete(l.subs, ch)
		close(ch)
	}
	if l.linger != nil {
		l.linger.Stop()
	}
	if l.idle != nil {
		l.idle.Stop()
	}
	if l.r.lives[l.addr] == l {
		delete(l.r.lives, l.addr)
	}
}

// Live subscribes to a watch's live values: in-process, or relayed from its node.
func (g *Registry) Live(ctx context.Context, addr string) (*nodehub.Viewer, error) {
	local, r := g.route(addr)
	switch {
	case local:
		return g.opt.Local.Live(ctx, addr)
	case r == nil:
		return nil, g.unknown()
	}
	g.mu.Lock()
	l := r.lives[addr]
	if l == nil {
		l = &relay{g: g, r: r, addr: addr, since: g.opt.Now(), subs: map[chan realtime.Reading]bool{}}
		r.lives[addr] = l
		g.mu.Unlock()
		if _, err := g.call(ctx, r, nodeproto.Command{Kind: nodeproto.CmdLiveStart, Address: addr}); err != nil {
			g.mu.Lock()
			l.endLocked(err)
			g.mu.Unlock()
			return nil, err
		}
		g.mu.Lock()
		if l.ended { // the link failed before the answer came
			err := l.err
			g.mu.Unlock()
			if err == nil {
				err = nodehub.ErrLinkClosed
			}
			return nil, err
		}
		l.idle = time.AfterFunc(g.opt.LiveIdle, func() { g.liveIdle(l) })
	}
	if l.linger != nil {
		l.linger.Stop()
		l.linger = nil
	}
	ch := make(chan realtime.Reading, len(l.recent)+256)
	for _, rd := range l.recent {
		ch <- rd
	}
	l.subs[ch] = true
	since := l.since
	g.mu.Unlock()
	stop := func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if !l.subs[ch] {
			return
		}
		delete(l.subs, ch)
		close(ch)
		if len(l.subs) == 0 && l.linger == nil && !l.ended {
			l.linger = time.AfterFunc(g.opt.LiveLinger, func() {
				g.mu.Lock()
				defer g.mu.Unlock()
				if len(l.subs) == 0 && !l.ended {
					g.sendLocked(l.r, nodeproto.Command{Kind: nodeproto.CmdLiveStop, Address: l.addr})
					l.endLocked(nil)
				}
			})
		}
	}
	reason := func() error {
		g.mu.Lock()
		defer g.mu.Unlock()
		return l.err
	}
	return nodehub.NewViewer(ch, since, stop, reason), nil
}

// liveIdle ends a relay whose node stopped sending.
func (g *Registry) liveIdle(l *relay) {
	g.mu.Lock()
	defer g.mu.Unlock()
	l.endLocked(nodehub.ErrLinkClosed)
}

// LiveBatch takes a node's live readings for one watch and passes them to its viewers.
func (g *Registry) LiveBatch(info NodeInfo, req nodeproto.LiveRequest) {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := g.nodeLocked(info)
	l := r.lives[req.Address]
	if l == nil {
		if req.End == nil && len(req.Readings) > 0 {
			// Readings for a link nobody here wants (the server restarted): stop it.
			g.sendLocked(r, nodeproto.Command{Kind: nodeproto.CmdLiveStop, Address: req.Address})
		}
		return
	}
	if !req.Since.IsZero() {
		l.since = req.Since
	}
	if l.idle != nil {
		l.idle.Reset(g.opt.LiveIdle)
	}
	for _, rd := range req.Readings {
		l.recent = append(l.recent, rd)
		for ch := range l.subs {
			select {
			case ch <- rd:
			default: // a slow viewer misses a value, nobody else does
			}
		}
	}
	if n := len(l.recent); n > 0 {
		last := l.recent[n-1].At
		cut := 0
		for cut < n && last.Sub(l.recent[cut].At) > nodehub.LiveHistory {
			cut++
		}
		l.recent = append(l.recent[:0], l.recent[cut:]...)
	}
	if req.End != nil {
		l.endLocked(liveEndErr(*req.End))
	}
}

// Live end codes a node sends.
const (
	EndTimeLimit = "time_limit"
	EndStopping  = "stopping"
	EndClosed    = "closed"
)

// LiveEndFor is the end a node reports for its viewer's error.
func LiveEndFor(err error) nodeproto.LiveEnd {
	switch {
	case err == nil:
		return nodeproto.LiveEnd{}
	case errors.Is(err, nodehub.ErrTimeLimit):
		return nodeproto.LiveEnd{Code: EndTimeLimit, Error: err.Error()}
	case errors.Is(err, nodehub.ErrStopping):
		return nodeproto.LiveEnd{Code: EndStopping, Error: err.Error()}
	case errors.Is(err, nodehub.ErrLinkClosed):
		return nodeproto.LiveEnd{Code: EndClosed, Error: err.Error()}
	default:
		return nodeproto.LiveEnd{Code: nodeproto.CodeFailed, Error: err.Error()}
	}
}

func liveEndErr(e nodeproto.LiveEnd) error {
	switch e.Code {
	case "":
		return nil
	case EndTimeLimit:
		return nodehub.ErrTimeLimit
	case EndStopping:
		return nodehub.ErrStopping
	case EndClosed:
		return nodehub.ErrLinkClosed
	default:
		return errors.New(e.Error)
	}
}
