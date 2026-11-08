// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package nodehub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/clock"
)

// Pairer finds, pairs and identifies watches: syncnode.BluezPairer, or a fake in tests.
type Pairer interface {
	// Scan reports the Garmin watches BlueZ knows while discovery runs, until fn returns false
	// or ctx ends.
	Scan(ctx context.Context, fn func([]bluez.Device) bool) error
	// Pair bonds with d (fresh false: it was bonded already). It cancels the pairing in BlueZ
	// when ctx ends.
	Pair(ctx context.Context, d bluez.Device, repair bool, p bluez.Prompter) (bluez.Device, bool, error)
	// Identify runs a handshake and asks the battery level; it saves nothing.
	Identify(ctx context.Context, addr string, fresh bool, step func(string)) (syncnode.Identity, error)
	// Unbond removes this computer's pairing with the watch.
	Unbond(ctx context.Context, addr string) error
	// Save adds the watch to node.json, which makes the hub sync it.
	Save(addr string, rec syncnode.WatchRecord) error
}

// States of a pairing session.
const (
	PairPrepare     = "prepare"       // started; Bluetooth not taken yet
	PairWaiting     = "waiting"       // waiting for Bluetooth (a sync is running)
	PairFinding     = "finding"       // discovery lists the watches
	PairPairing     = "pairing"       // bonding with the chosen watch
	PairConfirm     = "confirm"       // numeric comparison: do both codes match?
	PairEnter       = "enter_passkey" // type the code the watch shows
	PairShow        = "show_passkey"  // type this code on the watch
	PairIdentifying = "identifying"   // handshake: model, firmware, battery
	PairSetup       = "setup"         // nickname, after fetching, first sync
	PairDone        = "done"
	PairFailed      = "failed"
	PairCanceled    = "canceled"
	PairExpired     = "expired"
)

// Who owns a listed or identified watch, seen from the session's owner.
const (
	OwnerYours  = "yours"
	OwnerOther  = "other"  // another account: it cannot be added
	OwnerNobody = "nobody" // known here, waiting for an owner
	OwnerNew    = "new"    // never seen by this server
)

// How a watch came to be paired (PairView.Bond).
const (
	BondExisting = "existing"
	BondCode     = "code"
	BondNoCode   = "no_code"
)

// Codes of a session's errors, beside syncnode.Pair*.
const (
	CodeOwnedElsewhere = "owned_elsewhere"
	CodeIdentify       = "identify"
	CodeBusy           = "busy"
	CodeScan           = "scan"
	CodeStopping       = "stopping"
)

// Errors of the pairing calls.
var (
	ErrPairingActive  = errors.New("nodehub: someone is adding a watch right now")
	ErrNoSession      = errors.New("nodehub: no such pairing session")
	ErrWrongState     = errors.New("nodehub: not possible at this step")
	ErrOwnedElsewhere = errors.New("nodehub: this watch belongs to another account")
	ErrNotListed      = errors.New("nodehub: that watch is not in the list")
	// ErrCannotPair is returned by a hub without a PairBackend.
	ErrCannotPair = errors.New("nodehub: this computer cannot add watches")
)

// PairWatch is a watch in the session's list.
type PairWatch struct {
	Address    string
	Name       string
	RSSI       int16
	SeenNow    bool // advertising now (BlueZ has a signal strength)
	PairedHere bool // bonded with this computer
	Owner      string
}

// PairPrompt is a pairing question waiting for an answer.
type PairPrompt struct {
	Kind     string // confirm, enter, show
	Passkey  string // six digits; empty for enter
	Deadline time.Time
}

// PairStep is one item of the identify checklist.
type PairStep struct {
	Name string // syncnode.Step*
	Done bool
}

// PairIdentified is what the handshake told about the watch.
type PairIdentified struct {
	Unit     uint32
	Product  uint16
	Name     string
	Firmware string
	Battery  int // percent, -1 if not reported
}

// PairView is a session as the wizard shows it.
type PairView struct {
	ID               string
	State            string
	Started, Expires time.Time
	Starter, Owner   int64
	Error, ErrorCode string
	// Retry says the error is one the user may retry from the list.
	Retry       bool
	Discovering bool
	Watches     []PairWatch
	Selected    string
	Prompt      *PairPrompt
	Steps       []PairStep
	Identified  *PairIdentified
	// Ownership is OwnerNew, OwnerNobody or OwnerYours once identified; FormerOwner says
	// another account had the watch before (older files on it are theirs).
	Ownership   string
	FormerOwner bool
	// Bond says how the watch is paired once the pairing step is over: BondExisting (the pairing
	// this computer had is used), BondCode (paired just now, a code confirmed or typed) or
	// BondNoCode (paired just now, the watch asked for no code).
	Bond string
	// Nickname and AfterFetch are the current settings of a watch that is yours already.
	Nickname, AfterFetch string
	DeviceID             int64
	// Version counts the session's changes, so a relayed copy keeps the newest.
	Version uint64
	// Node and NodeID name the sync node the session runs on, as the server relays it (empty:
	// the server pairs itself).
	Node   string
	NodeID int64
}

// Terminal reports whether the session has ended.
func (v PairView) Terminal() bool { return terminal(v.State) }

func terminal(state string) bool {
	switch state {
	case PairDone, PairFailed, PairCanceled, PairExpired:
		return true
	}
	return false
}

// PairSetupRequest is the wizard's Set up step.
type PairSetupRequest struct {
	Nickname string
	// GarminConnect says a phone with Garmin Connect still syncs the watch: files then stay
	// on it (keep); otherwise they are marked as synced (archive).
	GarminConnect bool
	// IncludeSynced also fetches older files another app synced (ignored for a watch someone
	// else owned before: those files are theirs).
	IncludeSynced bool
}

type pairCmd struct {
	kind   string // scan, select
	dev    bluez.Device
	repair bool
}

type promptAnswer struct {
	match   bool
	passkey uint32
}

// pairSession is the one pairing session of the server. A worker goroutine does the Bluetooth
// part (scan, pair, identify) while holding Bluetooth; the calls from the API change the view
// and hand it commands. Lock order: Hub.mu before pairSession.mu.
type pairSession struct {
	h              *Hub
	id             string // fixed at the start, like starter and owner
	starter, owner int64
	ctx            context.Context
	cancel         context.CancelFunc
	cmds           chan pairCmd

	mu         sync.Mutex
	v          PairView
	ended      time.Time
	devices    map[string]bluez.Device
	owners     map[string]string
	viewers    int
	lastSeen   time.Time
	subs       map[chan PairView]bool
	answer     chan promptAnswer
	bzCancel   chan struct{}
	promptCode string // mismatch or timeout: how our prompter ended the last prompt
	asked      string // the kind of the last pairing question (confirm, enter, show), "" if none
	lane       func()
	scanStop   context.CancelFunc
	scanDone   chan struct{}
	scanTimer  clock.Timer
	settingUp  bool
	fresh      bool
	flagsRetry time.Time // when to ask again whose the listed watches are, after a failed lookup
	identity   syncnode.Identity
	idleT      clock.Timer
	maxT       clock.Timer
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// StartPairing opens the server's pairing session for owner, started by starter (an admin may
// add a watch for someone else). A running session of the same starter comes back with created
// false; one of somebody else is ErrPairingActive.
func (h *Hub) StartPairing(starter, owner int64) (PairView, bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pairStopped {
		return PairView{}, false, ErrStopping
	}
	if h.pb == nil {
		return PairView{}, false, ErrCannotPair
	}
	if s := h.pairing; s != nil {
		s.mu.Lock()
		active, mine, v := !terminal(s.v.State), s.starter == starter, s.viewLocked()
		if active && mine {
			s.lastSeen = h.opt.Clock.Now()
		}
		s.mu.Unlock()
		if active {
			if mine {
				return v, false, nil
			}
			return PairView{}, false, ErrPairingActive
		}
	}
	clk := h.opt.Clock
	now := clk.Now()
	ctx, cancel := context.WithCancel(context.Background())
	id := newSessionID()
	s := &pairSession{h: h, id: id, starter: starter, owner: owner, ctx: ctx, cancel: cancel, cmds: make(chan pairCmd, 4),
		v:       PairView{ID: id, State: PairPrepare, Started: now, Expires: now.Add(h.opt.PairMax), Starter: starter, Owner: owner},
		devices: map[string]bluez.Device{}, owners: map[string]string{}, subs: map[chan PairView]bool{}, lastSeen: now}
	s.mu.Lock()
	s.maxT = clk.AfterFunc(h.opt.PairMax, func() {
		s.end(PairExpired, "", "This took longer than 10 minutes, so it stopped. Start again.")
	})
	s.idleT = clk.AfterFunc(h.opt.PairIdle, s.idleCheck)
	v := s.viewLocked()
	s.mu.Unlock()
	h.pairing = s
	h.jobs.Add(1)
	go s.work()
	h.log.Info("nodehub: pairing session started", "by", starter, "owner", owner)
	return v, true, nil
}

// session returns the session id if user started it (admin: any session, for cancelling).
func (h *Hub) session(id string, user int64, admin bool) (*pairSession, error) {
	h.mu.Lock()
	s := h.pairing
	h.mu.Unlock()
	if s == nil || (id != "" && s.id != id) {
		return nil, ErrNoSession
	}
	if s.starter != user && !admin {
		return nil, ErrNoSession
	}
	return s, nil
}

// Pairing returns user's session: one that runs, or one that ended less than PairMax ago (so
// a reload after the setup lands on the first sync).
func (h *Hub) Pairing(user int64) (PairView, bool) {
	s, err := h.session("", user, false)
	if err != nil {
		return PairView{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := h.opt.Clock.Now()
	if terminal(s.v.State) && now.Sub(s.ended) > h.opt.PairMax {
		return PairView{}, false
	}
	s.lastSeen = now
	return s.viewLocked(), true
}

// PairingView returns the session id whoever started it (a sync node answering the server, which
// checked who may see it).
func (h *Hub) PairingView(id string) (PairView, error) {
	h.mu.Lock()
	s := h.pairing
	h.mu.Unlock()
	if s == nil || s.id != id {
		return PairView{}, ErrNoSession
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewLocked(), nil
}

// RunningPairing returns the session that runs now, whoever started it (for admins: who is
// adding a watch, so a stuck session can be ended).
func (h *Hub) RunningPairing() (PairView, bool) {
	h.mu.Lock()
	s := h.pairing
	h.mu.Unlock()
	if s == nil {
		return PairView{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if terminal(s.v.State) {
		return PairView{}, false
	}
	return s.viewLocked(), true
}

// PairingByID returns the session id to its starter.
func (h *Hub) PairingByID(id string, user int64) (PairView, error) {
	s, err := h.session(id, user, false)
	if err != nil {
		return PairView{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSeen = h.opt.Clock.Now()
	return s.viewLocked(), nil
}

// PairEvents streams the session's view: the current one at once, then each change (a slow
// reader sees the latest). The stream counts as a viewer for the watchdog until stop.
func (h *Hub) PairEvents(id string, user int64) (<-chan PairView, func(), error) {
	s, err := h.session(id, user, false)
	if err != nil {
		return nil, nil, err
	}
	ch, stop := s.subscribe(true)
	return ch, stop, nil
}

// PairFollow streams the session id like PairEvents, without counting as a viewer: a sync node
// relays its session to the server, where the people looking at it are counted (PairTouch).
func (h *Hub) PairFollow(id string) (<-chan PairView, func(), error) {
	h.mu.Lock()
	s := h.pairing
	h.mu.Unlock()
	if s == nil || s.id != id {
		return nil, nil, ErrNoSession
	}
	ch, stop := s.subscribe(false)
	return ch, stop, nil
}

// PairTouch tells the session id that someone looks at it elsewhere (the server shows a sync
// node's session in a browser), so the watchdog lets it be. ErrNoSession: this hub does not
// have it (any more).
func (h *Hub) PairTouch(id string) error {
	h.mu.Lock()
	s := h.pairing
	h.mu.Unlock()
	if s == nil || s.id != id {
		return ErrNoSession
	}
	s.mu.Lock()
	s.lastSeen = h.opt.Clock.Now()
	s.mu.Unlock()
	return nil
}

// subscribe hands out a stream of the session's views; viewer counts it for the watchdog.
func (s *pairSession) subscribe(viewer bool) (<-chan PairView, func()) {
	h := s.h
	ch := make(chan PairView, 1)
	s.mu.Lock()
	ch <- s.viewLocked()
	s.subs[ch] = true
	if viewer {
		s.viewers++
	}
	s.mu.Unlock()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.subs, ch)
			if !viewer {
				return
			}
			s.viewers--
			s.lastSeen = h.opt.Clock.Now()
			if s.viewers == 0 && !terminal(s.v.State) {
				s.idleT.Stop()
				s.idleT = h.opt.Clock.AfterFunc(h.opt.PairIdle, s.idleCheck)
			}
		})
	}
	return ch, stop
}

// PairScan starts discovery: from the start (taking Bluetooth, waiting for a running sync), or
// again after it paused or a pairing failed.
func (h *Hub) PairScan(id string, user int64) error {
	s, err := h.session(id, user, false)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSeen = h.opt.Clock.Now()
	switch {
	case s.v.State == PairPrepare:
		s.v.State = PairWaiting
	case s.v.State == PairFinding && !s.v.Discovering:
	case s.v.State == PairFinding:
		s.clearErrorLocked()
		s.notifyLocked()
		return nil // discovery runs already
	default:
		return fmt.Errorf("%w: Discovery can start only before a watch is chosen.", ErrWrongState)
	}
	s.clearErrorLocked()
	s.notifyLocked()
	select {
	case s.cmds <- pairCmd{kind: "scan"}:
	default:
	}
	return nil
}

// PairSelect pairs with a listed watch (repair: remove its pairing first).
func (h *Hub) PairSelect(id string, user int64, addr string, repair bool) error {
	s, err := h.session(id, user, false)
	if err != nil {
		return err
	}
	addr, err = bluez.NormalizeAddress(addr)
	if err != nil {
		return ErrNotListed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSeen = h.opt.Clock.Now()
	if s.v.State != PairFinding || s.lane == nil {
		return fmt.Errorf("%w: Choose a watch from the list first.", ErrWrongState)
	}
	d, ok := s.devices[addr]
	if !ok {
		return ErrNotListed
	}
	if s.owners[addr] == OwnerOther {
		return ErrOwnedElsewhere
	}
	s.clearErrorLocked()
	s.v.State, s.v.Selected, s.v.Steps, s.v.Identified = PairPairing, addr, nil, nil
	s.notifyLocked()
	select {
	case s.cmds <- pairCmd{kind: "select", dev: d, repair: repair}:
	default:
	}
	return nil
}

// PairConfirm answers the numeric comparison.
func (h *Hub) PairConfirm(id string, user int64, match bool) error {
	return h.answerPrompt(id, user, PairConfirm, promptAnswer{match: match})
}

// PairPasskey answers a passkey request with the code the watch shows.
func (h *Hub) PairPasskey(id string, user int64, passkey uint32) error {
	if passkey > 999999 {
		return fmt.Errorf("%w: The code has six digits.", ErrWrongState)
	}
	return h.answerPrompt(id, user, PairEnter, promptAnswer{passkey: passkey, match: true})
}

func (h *Hub) answerPrompt(id string, user int64, state string, a promptAnswer) error {
	s, err := h.session(id, user, false)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSeen = h.opt.Clock.Now()
	if s.v.State != state || s.answer == nil {
		return fmt.Errorf("%w: The watch is not asking for that now.", ErrWrongState)
	}
	select {
	case s.answer <- a:
	default:
	}
	return nil
}

// PairSetup adds the identified watch to the owner's account and starts its first sync.
func (h *Hub) PairSetup(ctx context.Context, id string, user int64, req PairSetupRequest) (PairView, error) {
	s, err := h.session(id, user, false)
	if err != nil {
		return PairView{}, err
	}
	s.mu.Lock()
	s.lastSeen = h.opt.Clock.Now()
	if s.v.State != PairSetup || s.settingUp {
		s.mu.Unlock()
		return PairView{}, fmt.Errorf("%w: The watch is not ready to be set up.", ErrWrongState)
	}
	s.settingUp = true
	ident, owner, fresh, former := s.identity, s.owner, s.fresh, s.v.FormerOwner
	s.mu.Unlock()
	view, err := h.setup(ctx, s, ident, owner, fresh, former, req)
	s.mu.Lock()
	s.settingUp = false
	s.mu.Unlock()
	return view, err
}

func (h *Hub) setup(ctx context.Context, s *pairSession, ident syncnode.Identity, owner int64, fresh, former bool,
	req PairSetupRequest) (PairView, error) {
	mode := store.AfterFetchArchive
	if req.GarminConnect {
		mode = store.AfterFetchKeep
	}
	rec := ident.Record
	id, err := h.pb.Claim(ctx, PairClaim{Owner: owner, Address: ident.Address, Record: rec, Battery: ident.Battery,
		Nickname: strings.TrimSpace(req.Nickname), AfterFetch: mode})
	if errors.Is(err, ErrOwnedElsewhere) {
		return h.ownedElsewhere(s, ident.Address, fresh)
	}
	if err != nil {
		return PairView{}, err
	}
	if err := h.opt.Pairer.Save(ident.Address, rec); err != nil {
		return PairView{}, err
	}
	h.mu.Lock()
	delete(h.status, ident.Address) // a watch paired again starts with a clean status
	h.mu.Unlock()
	if err := h.Sync(Request{Addr: ident.Address, By: store.StartedByWizard, IncludeSynced: req.IncludeSynced && !former}); err != nil {
		h.log.Warn("nodehub: first sync not queued (the schedule picks it up)", "watch", ident.Address, "err", err)
	}
	h.log.Info("nodehub: watch added", "watch", ident.Address, "unit", rec.Unit, "owner", owner)
	s.mu.Lock()
	s.v.DeviceID = id
	s.mu.Unlock()
	s.end(PairDone, "", "")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewLocked(), nil
}

const msgOwnedElsewhere = "This watch belongs to another account on this server. Its owner can hand it over " +
	"under Devices › the watch › Remove."

// ownedElsewhere ends the session for a watch that turned out to be someone else's, removing a
// pairing made just now.
func (h *Hub) ownedElsewhere(s *pairSession, addr string, fresh bool) (PairView, error) {
	if fresh {
		uctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := h.opt.Pairer.Unbond(uctx, addr); err != nil {
			h.log.Warn("nodehub: removing the pairing of another account's watch", "watch", addr, "err", err)
		}
		cancel()
	}
	s.end(PairFailed, CodeOwnedElsewhere, msgOwnedElsewhere)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.viewLocked(), ErrOwnedElsewhere
}

// CancelPairing ends a session: its starter's, or (admin) any; id "" means the running one.
func (h *Hub) CancelPairing(id string, user int64, admin bool) error {
	s, err := h.session(id, user, admin)
	if err != nil {
		return err
	}
	s.end(PairCanceled, "", "")
	return nil
}

// endPairing refuses new sessions and ends the running one (the server stops).
func (h *Hub) endPairing() {
	h.mu.Lock()
	h.pairStopped = true
	s := h.pairing
	h.mu.Unlock()
	if s != nil {
		s.end(PairCanceled, CodeStopping, "The server is stopping.")
	}
}

// closeIdleLive ends live links nobody watches (they linger 30 s), so a pairing waiting for
// Bluetooth does not wait for them.
func (h *Hub) closeIdleLive() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, l := range h.lives {
		if len(l.subs) == 0 {
			l.cancel()
		}
	}
}

// ownerFlags asks whose the watches at addrs are, seen from owner, with the unit numbers
// node.json knows.
func (h *Hub) ownerFlags(ctx context.Context, owner int64, addrs []string) (map[string]string, error) {
	known, _ := h.opt.Watches()
	cs := make([]PairCandidate, 0, len(addrs))
	for _, a := range addrs {
		cs = append(cs, PairCandidate{Address: a, Unit: known[a].Unit})
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return h.pb.OwnerFlags(ctx, owner, cs)
}

// end puts the session in a final state and stops its Bluetooth work. It reports false if the
// session had ended already.
func (s *pairSession) end(state, code, msg string) bool {
	s.mu.Lock()
	if terminal(s.v.State) {
		s.mu.Unlock()
		return false
	}
	s.v.State, s.v.ErrorCode, s.v.Error, s.v.Retry = state, code, msg, false
	s.v.Prompt, s.v.Discovering = nil, false
	s.ended = s.h.opt.Clock.Now()
	s.idleT.Stop()
	s.maxT.Stop()
	s.notifyLocked()
	s.mu.Unlock()
	s.cancel()
	if state != PairDone {
		s.h.log.Info("nodehub: pairing session ended", "state", state, "code", code)
	}
	return true
}

// idleCheck expires the session when nobody has looked at it for PairIdle.
func (s *pairSession) idleCheck() {
	s.mu.Lock()
	if terminal(s.v.State) {
		s.mu.Unlock()
		return
	}
	now := s.h.opt.Clock.Now()
	idle := now.Sub(s.lastSeen)
	if s.viewers == 0 && idle >= s.h.opt.PairIdle {
		s.mu.Unlock()
		s.end(PairExpired, "", "Nobody looked at this for a few minutes, so it stopped. Start again.")
		return
	}
	wait := s.h.opt.PairIdle
	if s.viewers == 0 {
		wait -= idle
	}
	s.idleT = s.h.opt.Clock.AfterFunc(wait, s.idleCheck)
	s.mu.Unlock()
}

func (s *pairSession) viewLocked() PairView {
	v := s.v
	v.Watches = slices.Clone(s.v.Watches)
	v.Steps = slices.Clone(s.v.Steps)
	if s.v.Prompt != nil {
		p := *s.v.Prompt
		v.Prompt = &p
	}
	if s.v.Identified != nil {
		i := *s.v.Identified
		v.Identified = &i
	}
	return v
}

// notifyLocked hands every stream the current view, replacing one it has not read yet.
func (s *pairSession) notifyLocked() {
	s.v.Version++
	v := s.viewLocked()
	for ch := range s.subs {
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

func (s *pairSession) clearErrorLocked() {
	s.v.Error, s.v.ErrorCode, s.v.Retry = "", "", false
}

func (s *pairSession) setErrorLocked(code, msg string, retry bool) {
	s.v.Error, s.v.ErrorCode, s.v.Retry = msg, code, retry
}

// work does the session's Bluetooth part, one command at a time, until the session ends.
func (s *pairSession) work() {
	defer s.h.jobs.Done()
	defer s.releaseLane()
	defer s.stopScan()
	for {
		select {
		case <-s.ctx.Done():
			return
		case c := <-s.cmds:
			switch c.kind {
			case "scan":
				s.scan()
			case "select":
				s.pairAndIdentify(c.dev, c.repair)
			}
		}
	}
}

func (s *pairSession) releaseLane() {
	s.mu.Lock()
	release := s.lane
	s.lane = nil
	s.mu.Unlock()
	if release != nil {
		release()
	}
}

// stopScan ends a running discovery and waits until BlueZ stopped it.
func (s *pairSession) stopScan() {
	s.mu.Lock()
	stop, done := s.scanStop, s.scanDone
	s.scanStop, s.scanDone = nil, nil
	if s.scanTimer != nil {
		s.scanTimer.Stop()
		s.scanTimer = nil
	}
	s.mu.Unlock()
	if stop != nil {
		stop()
		<-done
	}
}

// scan takes Bluetooth if the session does not hold it yet, then (re)starts discovery for
// PairScanFor.
func (s *pairSession) scan() {
	s.mu.Lock()
	held := s.lane != nil
	s.mu.Unlock()
	if !held {
		s.h.closeIdleLive()
		release, err := s.h.take(s.ctx, "pairing", "", true)
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			s.mu.Lock()
			s.v.State = PairPrepare
			s.setErrorLocked(CodeBusy, "Another goarmin command is using Bluetooth. Try again when it has finished.", true)
			s.notifyLocked()
			s.mu.Unlock()
			return
		}
		s.mu.Lock()
		s.lane = release
		s.mu.Unlock()
	}
	s.stopScan()
	sctx, stop := context.WithCancel(s.ctx)
	done := make(chan struct{})
	s.mu.Lock()
	s.v.State, s.v.Discovering = PairFinding, true
	s.scanStop, s.scanDone = stop, done
	s.scanTimer = s.h.opt.Clock.AfterFunc(s.h.opt.PairScanFor, stop)
	s.notifyLocked()
	s.mu.Unlock()
	go func() {
		defer close(done)
		err := s.h.opt.Pairer.Scan(sctx, func(devs []bluez.Device) bool {
			s.list(sctx, devs)
			return sctx.Err() == nil
		})
		s.mu.Lock()
		defer s.mu.Unlock()
		s.v.Discovering = false
		if err != nil && sctx.Err() == nil {
			code := syncnode.PairCode(err)
			if code != syncnode.PairAdapterOff {
				code = CodeScan
			}
			s.setErrorLocked(code, "Bluetooth discovery failed: "+err.Error(), true)
		}
		if !terminal(s.v.State) {
			s.notifyLocked()
		}
	}()
}

// list takes a scan's devices into the view, strongest signal first.
func (s *pairSession) list(ctx context.Context, devs []bluez.Device) {
	s.mu.Lock()
	owner := s.owner
	var unknown []string
	for _, d := range devs {
		if _, ok := s.owners[d.Address]; !ok {
			unknown = append(unknown, d.Address)
		}
	}
	ask := len(unknown) > 0 && !s.h.opt.Clock.Now().Before(s.flagsRetry)
	s.mu.Unlock()
	var flags map[string]string
	if ask {
		var err error
		if flags, err = s.h.ownerFlags(ctx, owner, unknown); err != nil && ctx.Err() == nil {
			s.h.log.Info("nodehub: looking up the listed watches", "err", err)
			s.mu.Lock()
			s.flagsRetry = s.h.opt.Clock.Now().Add(10 * time.Second) // not on every scan tick
			s.mu.Unlock()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for a, f := range flags {
		s.owners[a] = f
	}
	ws := make([]PairWatch, 0, len(devs))
	byAddr := make(map[string]bluez.Device, len(devs))
	for _, d := range devs {
		byAddr[d.Address] = d
		flag := s.owners[d.Address]
		if flag == "" {
			flag = OwnerNew // not looked up yet (the server did not answer): the setup decides
		}
		ws = append(ws, PairWatch{Address: d.Address, Name: d.DisplayName(), RSSI: d.RSSI, SeenNow: d.HasRSSI,
			PairedHere: d.Bonded, Owner: flag})
	}
	sort.SliceStable(ws, func(i, j int) bool {
		if ws[i].SeenNow != ws[j].SeenNow {
			return ws[i].SeenNow
		}
		if ws[i].RSSI != ws[j].RSSI {
			return ws[i].RSSI > ws[j].RSSI
		}
		return ws[i].Address < ws[j].Address
	})
	s.devices = byAddr
	if !slices.Equal(ws, s.v.Watches) && !terminal(s.v.State) {
		s.v.Watches = ws
		s.notifyLocked()
	}
}

// pairAndIdentify bonds with the chosen watch and identifies it; a retryable failure goes
// back to the list with discovery running again.
func (s *pairSession) pairAndIdentify(dev bluez.Device, repair bool) {
	s.stopScan()
	s.mu.Lock()
	s.promptCode, s.asked = "", ""
	s.v.Bond = ""
	s.mu.Unlock()
	s.h.log.Info("nodehub: pairing", "watch", dev.Address, "repair", repair, "bonded_before", dev.Bonded)
	d, fresh, err := s.h.opt.Pairer.Pair(s.ctx, dev, repair, &webPrompter{s: s})
	if s.ctx.Err() != nil {
		return
	}
	if err != nil {
		s.mu.Lock()
		code := s.promptCode
		if code == "" {
			code = syncnode.PairCode(err)
		}
		s.v.Prompt = nil
		s.setErrorLocked(code, pairMessage(code, err), true)
		s.mu.Unlock()
		s.h.log.Info("nodehub: pairing failed", "watch", dev.Address, "code", code, "err", err)
		s.scan()
		return
	}
	s.mu.Lock()
	s.fresh = fresh
	switch {
	case !fresh:
		s.v.Bond = BondExisting
	case s.asked != "":
		s.v.Bond = BondCode
	default:
		s.v.Bond = BondNoCode
	}
	bond, asked := s.v.Bond, s.asked
	s.v.State, s.v.Prompt = PairIdentifying, nil
	s.mu.Unlock()
	s.h.log.Info("nodehub: paired", "watch", d.Address, "bond", bond, "question", asked, "bonded", d.Bonded)
	s.mu.Lock()
	s.v.Steps = []PairStep{{Name: syncnode.StepConnected}, {Name: syncnode.StepLink}, {Name: syncnode.StepBattery}}
	s.notifyLocked()
	s.mu.Unlock()
	ident, err := s.h.opt.Pairer.Identify(s.ctx, d.Address, fresh, s.step)
	if s.ctx.Err() != nil {
		return
	}
	if err != nil {
		s.mu.Lock()
		s.setErrorLocked(CodeIdentify, "Paired, but goarmin could not talk to the watch yet. Choose it again to retry.", true)
		s.mu.Unlock()
		s.h.log.Info("nodehub: identify failed", "watch", d.Address, "err", err)
		s.scan()
		return
	}
	ictx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
	who, err := s.h.pb.Identified(ictx, s.owner, ident.Record.Unit)
	cancel()
	ownership, former := who.Ownership, who.Former
	if err != nil {
		s.mu.Lock()
		s.setErrorLocked(CodeIdentify, "goarmin could not look the watch up: "+err.Error(), true)
		s.mu.Unlock()
		s.scan()
		return
	}
	if ownership == OwnerOther {
		_, _ = s.h.ownedElsewhere(s, d.Address, fresh)
		return
	}
	s.stopScan()
	s.releaseLane() // setup takes the user a while; the first sync takes Bluetooth again
	s.mu.Lock()
	defer s.mu.Unlock()
	if terminal(s.v.State) {
		return
	}
	s.identity = ident
	s.clearErrorLocked()
	s.v.State = PairSetup
	s.v.Identified = &PairIdentified{Unit: ident.Record.Unit, Product: ident.Record.Product, Name: ident.Record.Name,
		Firmware: ident.Record.Firmware, Battery: ident.Battery.Percent}
	s.v.Ownership, s.v.FormerOwner = ownership, former
	if ownership == OwnerYours {
		s.v.Nickname, s.v.AfterFetch = who.Nickname, who.AfterFetch
	}
	for i := range s.v.Steps {
		s.v.Steps[i].Done = true
	}
	s.notifyLocked()
}

// step ticks an identify step off.
func (s *pairSession) step(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.v.Steps {
		if s.v.Steps[i].Name == name {
			s.v.Steps[i].Done = true
		}
	}
	if !terminal(s.v.State) {
		s.notifyLocked()
	}
}

// pairMessage is the wizard's sentence for a pairing error.
func pairMessage(code string, err error) string {
	switch code {
	case syncnode.PairMismatch:
		return "The codes did not match, so the pairing was refused. Try again."
	case syncnode.PairTimeout:
		return "Nobody confirmed the code in time. Try again."
	case syncnode.PairAuthFailed:
		return "The watch refused the pairing or gave up. Put it in pairing mode again and retry."
	case syncnode.PairConnectFailed:
		return "goarmin could not connect to the watch. Keep it close and try again."
	case syncnode.PairGone:
		return "The watch is no longer visible. Put it in pairing mode again."
	case syncnode.PairInProgress:
		return "Another pairing with this watch is running. Wait a moment and try again."
	case syncnode.PairAdapterOff:
		return "This computer's Bluetooth is off."
	case syncnode.PairNotBonded:
		return "The watch paired but kept no bond. Choose it again with Pair again."
	}
	return "Pairing failed: " + err.Error()
}

// webPrompter answers BlueZ's pairing questions through the session: it publishes the
// question and waits for the wizard's answer, PromptTimeout, BlueZ's cancel or the session's
// end.
type webPrompter struct{ s *pairSession }

var errPromptTimeout = errors.New("nodehub: nobody answered the pairing question in time")

func (p *webPrompter) ask(state, kind string, passkey uint32, show bool) (promptAnswer, error) {
	s := p.s
	clk := s.h.opt.Clock
	ans, bz := make(chan promptAnswer, 1), make(chan struct{}, 1)
	t := clk.NewTimer(s.h.opt.PromptTimeout) // before the question shows, so its deadline holds
	defer t.Stop()
	s.mu.Lock()
	if terminal(s.v.State) {
		s.mu.Unlock()
		return promptAnswer{}, context.Canceled
	}
	s.answer, s.bzCancel = ans, bz
	pk := ""
	if show {
		pk = fmt.Sprintf("%06d", passkey)
	}
	s.v.State, s.asked = state, kind
	s.v.Prompt = &PairPrompt{Kind: kind, Passkey: pk, Deadline: clk.Now().Add(s.h.opt.PromptTimeout)}
	s.notifyLocked()
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.answer, s.bzCancel = nil, nil
		if !terminal(s.v.State) {
			s.v.State, s.v.Prompt = PairPairing, nil
			s.notifyLocked()
		}
	}()
	select {
	case a := <-ans:
		if !a.match {
			s.setPromptCode(syncnode.PairMismatch)
		}
		return a, nil
	case <-t.C():
		s.setPromptCode(syncnode.PairTimeout)
		return promptAnswer{}, errPromptTimeout
	case <-bz:
		return promptAnswer{}, errors.New("nodehub: Bluetooth cancelled the pairing question")
	case <-s.ctx.Done():
		return promptAnswer{}, s.ctx.Err()
	}
}

func (s *pairSession) setPromptCode(code string) {
	s.mu.Lock()
	s.promptCode = code
	s.mu.Unlock()
}

func (p *webPrompter) RequestConfirmation(_ bluez.Device, passkey uint32) (bool, error) {
	a, err := p.ask(PairConfirm, "confirm", passkey, true)
	return a.match, err
}

func (p *webPrompter) RequestPasskey(bluez.Device) (uint32, error) {
	a, err := p.ask(PairEnter, "enter", 0, false)
	return a.passkey, err
}

func (p *webPrompter) DisplayPasskey(_ bluez.Device, passkey uint32, _ uint16) {
	s := p.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if terminal(s.v.State) {
		return
	}
	s.v.State, s.asked = PairShow, "show"
	s.v.Prompt = &PairPrompt{Kind: "show", Passkey: fmt.Sprintf("%06d", passkey),
		Deadline: s.h.opt.Clock.Now().Add(s.h.opt.PromptTimeout)}
	s.notifyLocked()
}

func (p *webPrompter) Cancel() {
	s := p.s
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bzCancel != nil {
		select {
		case s.bzCancel <- struct{}{}:
		default:
		}
	}
}
