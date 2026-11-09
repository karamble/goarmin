// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package nodehub runs the sync role inside goarmin serve (--with-sync): it syncs the watches
// this computer knows on a schedule or on request, imports what it pulled into the database,
// and streams live values while somebody watches them. One Bluetooth operation runs at a time
// (the lane, and across processes the data directory's ble.lock): a sync and a live view never
// fight over the watch.
package nodehub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/karamble/goarmin/internal/ingest"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/clock"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/realtime"
	"github.com/karamble/goarmin/pkg/garmin/watch"
)

// Options configures a hub.
type Options struct {
	DataDir string
	Adapter string
	// Interval between scheduled syncs of each watch (30 min). The first runs shortly after Run starts.
	Interval time.Duration
	// LiveLinger keeps a live link open this long after its last viewer left (30 s).
	LiveLinger time.Duration
	// LiveMax ends a live link after this long (30 min), so a forgotten page never keeps the
	// watch from syncing; the viewer may open it again.
	LiveMax time.Duration
	// In is the server's ingest (with its store) in goarmin serve -with-sync. Backend, if set,
	// replaces it for syncs (a sync node's server over HTTP); nil means Local{In}. Pairing needs
	// In (Add a watch runs where the server's database is).
	In      *ingest.Ingester
	Backend Backend
	// PairBackend decides ownership for Add a watch; nil means the Backend, if it is one (Local
	// and a sync node's Remote are). Without one the hub does not pair.
	PairBackend PairBackend
	Logger      *slog.Logger

	// The Bluetooth side, replaced by tests; nil means syncnode's and BlueZ's.
	Sync         func(ctx context.Context, opt syncnode.Options) (syncnode.Result, error)
	Live         func(ctx context.Context, opt syncnode.Options, svcs []multilink.Service, fn func(realtime.Reading)) (syncnode.Result, error)
	Watches      func() (map[string]syncnode.WatchRecord, error)
	AdapterState func(ctx context.Context) (bluez.Adapter, error)
	PowerOn      func(ctx context.Context) error
	Forget       func(ctx context.Context, addr string) error

	// Pairing (Add a watch). Pairer is syncnode's BlueZ pairer unless replaced; FirstSession
	// lets it send the first-session events after a fresh pairing (GOARMIN_FIRST_SESSION).
	Pairer       Pairer
	FirstSession bool
	// Features announces the host's feature capabilities in every sync and live link, as
	// Garmin Connect does (syncnode.Options.Features).
	Features bool
	// NoArchive never flags files as synced on the watch, whatever each watch's setting (a
	// rehearsal: the watch keeps everything for the host that syncs it for real).
	NoArchive bool
	// Clock drives the pairing session's timers (the real clock by default).
	Clock clock.Clock
	// A session ends after PairIdle without anyone looking (3 min) or PairMax in all (10 min);
	// discovery pauses after PairScanFor (2 min); a pairing question waits PromptTimeout (30 s,
	// the Bluetooth pairing timeout).
	PairIdle, PairMax, PairScanFor, PromptTimeout time.Duration
}

// Status is the state of one watch.
type Status struct {
	Address   string
	Running   bool
	Live      bool
	LastStart time.Time
	LastEnd   time.Time
	LastOK    bool
	// LastGood is when the last good sync started.
	LastGood  time.Time
	LastError string
	// ErrorCode classifies LastError: syncnode.Code*, CodeNoOwner, CodeImport.
	ErrorCode string
	Pulled    int
	Imported  int
	NextSync  time.Time
	// Failures counts failed syncs in a row. A watch that is merely out of range does not count:
	// it is Away (since AwaySince, Misses tries in a row) and tried again with a growing pause.
	Failures  int
	Away      bool
	AwaySince time.Time
	Misses    int
	// Progress of the running sync (or the last one): Phase is connecting, listing, fetching or
	// importing while it runs; Listed items the watch offered, Done of Total handled.
	Phase               string
	Listed, Done, Total int
	// Node names the sync node that syncs the watch (empty: this server itself). Offline says
	// the node has not been heard from for a while (since LastSeen).
	Node     string
	Offline  bool
	LastSeen time.Time
}

// Codes of the hub's own failures (beside syncnode's).
const (
	CodeNoOwner = "no_owner" // nobody owns the watch now: not synced until someone adds it
	CodeImport  = "import"   // the files arrived but could not all be imported
)

// backoff is the wait after n failed syncs in a row: 1, 2, 4, … minutes, at most interval.
func backoff(n int, interval time.Duration) time.Duration {
	if n <= 0 {
		return interval
	}
	if d := time.Minute << min(n-1, 10); d < interval {
		return d
	}
	return interval
}

// ErrBusy is returned when the watch is in use by another operation. The error itself is a
// *BusyError that names who holds Bluetooth.
var ErrBusy = errors.New("nodehub: the watch is busy, try again in a minute")

// BusyError says who holds Bluetooth; errors.Is(err, ErrBusy) is true for it.
type BusyError struct{ Holder Holder }

func (e *BusyError) Error() string {
	if e.Holder.Kind == "" {
		return ErrBusy.Error()
	}
	return fmt.Sprintf("nodehub: Bluetooth is in use (%s), try again in a minute", e.Holder.Kind)
}

// Is makes errors.Is(err, ErrBusy) hold.
func (e *BusyError) Is(target error) bool { return target == ErrBusy }

// ErrTimeLimit ends a live link that ran for LiveMax.
var ErrTimeLimit = errors.New("nodehub: the live link reached its time limit")

// ErrLinkClosed: the watch closed the live link by itself.
var ErrLinkClosed = errors.New("nodehub: the watch closed the live link")

// LiveHistory is how much of a live link's readings a new viewer gets first.
const LiveHistory = 10 * time.Minute

// ErrUnknown is returned for a watch this node does not know.
var ErrUnknown = errors.New("nodehub: this computer does not know that watch")

// ErrNoOwner is a sync's error for a watch nobody owns now: it waits for someone to add it.
var ErrNoOwner = errors.New("nodehub: nobody owns this watch now; add it under Devices › Add a watch")

// ErrStopping is returned while the server shuts down.
var ErrStopping = errors.New("nodehub: the server is stopping")

// errPaused skips a scheduled sync of a paused watch.
var errPaused = errors.New("nodehub: scheduled syncs are paused for this watch")

// LiveServices are streamed for a live view: every one goarmin decodes. Services the watch
// refuses are skipped, and some only send when their value changes.
var LiveServices = []multilink.Service{multilink.ServiceRealtimeHR, multilink.ServiceRealtimeSteps,
	multilink.ServiceRealtimeStress, multilink.ServiceRealtimeBodyBattery, multilink.ServiceRealtimeRespiration,
	multilink.ServiceRealtimeSpO2, multilink.ServiceRealtimeHRV, multilink.ServiceRealtimeCalories,
	multilink.ServiceRealtimeFloors, multilink.ServiceRealtimeIntensity}

// Request asks for a sync of one watch.
type Request struct {
	Addr string
	// By is how it started (store.StartedBy*; manual if empty).
	By            string
	IncludeSynced bool
	// Delete lists watch items (UIDs) to delete from the watch after the sync.
	Delete []string
}

// Holder is whoever has Bluetooth now.
type Holder struct {
	Kind  string // sync, live, pairing; command for another goarmin process
	Addr  string
	Since time.Time
}

// Hub is the in-process sync node.
type Hub struct {
	opt  Options
	st   *store.Store // nil on a sync node (no database here)
	be   Backend
	pb   PairBackend // nil: this hub does not pair
	log  *slog.Logger
	lane chan struct{} // one Bluetooth operation at a time in this process
	kick chan Request

	mu      sync.Mutex
	status  map[string]*Status
	lives   map[string]*live
	queued  map[string]bool // addresses with a requested sync waiting or running
	holder  *Holder
	stopped bool           // Run has ended: no new live links
	liveWG  sync.WaitGroup // running live links
	jobs    sync.WaitGroup // syncs started by Run, and the pairing session's work

	pairing     *pairSession // the server's pairing session (the last one, also once ended)
	pairStopped bool         // Run is ending: no new pairing sessions
}

// New builds a hub.
func New(opt Options) *Hub {
	if opt.Interval <= 0 {
		opt.Interval = 30 * time.Minute
	}
	if opt.LiveLinger <= 0 {
		opt.LiveLinger = 30 * time.Second
	}
	if opt.LiveMax <= 0 {
		opt.LiveMax = 30 * time.Minute
	}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opt.Sync == nil {
		opt.Sync = syncnode.SyncOnce
	}
	if opt.Live == nil {
		opt.Live = syncnode.Live
	}
	if opt.Watches == nil {
		dir := opt.DataDir
		opt.Watches = func() (map[string]syncnode.WatchRecord, error) { return syncnode.KnownWatches(dir) }
	}
	if opt.AdapterState == nil {
		name := opt.Adapter
		opt.AdapterState = func(ctx context.Context) (bluez.Adapter, error) {
			c, err := bluez.Open()
			if err != nil {
				return bluez.Adapter{}, err
			}
			defer c.Close()
			return c.Adapter(ctx, name)
		}
	}
	if opt.Forget == nil {
		dir, name := opt.DataDir, opt.Adapter
		opt.Forget = func(ctx context.Context, addr string) error { return syncnode.Forget(ctx, dir, name, addr) }
	}
	if opt.Clock == nil {
		opt.Clock = clock.Real()
	}
	if opt.PairIdle <= 0 {
		opt.PairIdle = 3 * time.Minute
	}
	if opt.PairMax <= 0 {
		opt.PairMax = 10 * time.Minute
	}
	if opt.PairScanFor <= 0 {
		opt.PairScanFor = 2 * time.Minute
	}
	if opt.PromptTimeout <= 0 {
		opt.PromptTimeout = 30 * time.Second
	}
	if opt.Pairer == nil {
		opt.Pairer = syncnode.BluezPairer{DataDir: opt.DataDir, Adapter: opt.Adapter, FirstSession: opt.FirstSession,
			Logger: opt.Logger}
	}
	if opt.PowerOn == nil {
		name := opt.Adapter
		opt.PowerOn = func(ctx context.Context) error {
			c, err := bluez.Open()
			if err != nil {
				return err
			}
			defer c.Close()
			return c.SetPowered(ctx, name, true)
		}
	}
	h := &Hub{opt: opt, be: opt.Backend, log: opt.Logger, lane: make(chan struct{}, 1), kick: make(chan Request, 8),
		status: map[string]*Status{}, lives: map[string]*live{}, queued: map[string]bool{}}
	if opt.In != nil {
		h.st = opt.In.Store
		if h.be == nil {
			h.be = Local{In: opt.In}
		}
	}
	h.pb = opt.PairBackend
	if pb, ok := h.be.(PairBackend); ok && h.pb == nil {
		h.pb = pb
	}
	return h
}

// take gets Bluetooth for one operation: the lane, then the data directory's lock (a goarmin
// command may be using it). With wait it queues for the lane; otherwise it is ErrBusy at once.
func (h *Hub) take(ctx context.Context, kind, addr string, wait bool) (func(), error) {
	if wait {
		select {
		case h.lane <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	} else {
		select {
		case h.lane <- struct{}{}:
		default:
			holder, _ := h.Busy()
			return nil, &BusyError{Holder: holder}
		}
	}
	unlock, err := syncnode.LockBluetooth(h.opt.DataDir)
	if err != nil {
		<-h.lane
		if errors.Is(err, syncnode.ErrBluetoothBusy) {
			return nil, &BusyError{Holder: Holder{Kind: "command"}}
		}
		return nil, err
	}
	h.mu.Lock()
	h.holder = &Holder{Kind: kind, Addr: addr, Since: time.Now()}
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		h.holder = nil
		h.mu.Unlock()
		unlock()
		<-h.lane
	}, nil
}

// Busy tells who has Bluetooth now, if anyone.
func (h *Hub) Busy() (Holder, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.holder == nil {
		return Holder{}, false
	}
	return *h.holder, true
}

// Run syncs on schedule and on request until ctx ends. Each watch is due Interval after its
// last good sync, sooner (backoff) after a failure. The loop never waits for Bluetooth itself:
// syncs run beside it, so a long live view or pairing never stalls the schedule.
func (h *Hub) Run(ctx context.Context) {
	first := time.NewTimer(5 * time.Second)
	defer first.Stop()
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	// When Run returns, every sync it started and every live link has ended, so the caller may
	// close the store afterwards.
	defer h.endLive()
	defer h.jobs.Wait()
	defer h.endPairing() // first: a pairing session's work counts in jobs
	h.seed(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			h.syncDue(ctx)
		case <-tick.C:
			h.syncDue(ctx)
		case req := <-h.kick:
			h.jobs.Add(1)
			go func() {
				defer h.jobs.Done()
				h.syncOne(ctx, req, true)
			}()
		}
	}
}

// syncDue starts the scheduled sync of the first due watch, if Bluetooth is free; the others
// get their turn on the next tick.
func (h *Hub) syncDue(ctx context.Context) {
	watches, err := h.opt.Watches()
	if err != nil {
		h.log.Warn("nodehub: reading the known watches", "err", err)
		return
	}
	addrs := make([]string, 0, len(watches))
	for a := range watches {
		addrs = append(addrs, a)
	}
	sort.Strings(addrs)
	now := time.Now()
	for _, a := range addrs {
		h.mu.Lock()
		due := !h.state(a).NextSync.After(now) && !h.queued[a]
		h.mu.Unlock()
		if !due {
			continue
		}
		release, err := h.take(ctx, "sync", a, false)
		if err != nil {
			h.log.Info("nodehub: Bluetooth busy, scheduled sync waits", "watch", a)
			return
		}
		h.jobs.Add(1)
		go func() {
			defer h.jobs.Done()
			defer release()
			h.run(ctx, Request{Addr: a, By: store.StartedBySchedule}, watches[a])
		}()
		return
	}
}

func (h *Hub) state(addr string) *Status {
	st := h.status[addr]
	if st == nil {
		st = &Status{Address: addr}
		h.status[addr] = st
	}
	return st
}

// SyncNow asks for a sync of the watch at addr soon (see Sync).
func (h *Hub) SyncNow(addr string) error {
	return h.Sync(Request{Addr: addr, By: store.StartedByManual})
}

// Sync queues a requested sync. Asking again while one is waiting or running changes nothing,
// unless the request deletes files.
func (h *Hub) Sync(req Request) error {
	watches, err := h.opt.Watches()
	if err != nil {
		return err
	}
	if _, ok := watches[req.Addr]; !ok {
		return ErrUnknown
	}
	if req.By == "" {
		req.By = store.StartedByManual
	}
	h.mu.Lock()
	if h.queued[req.Addr] && len(req.Delete) == 0 {
		h.mu.Unlock()
		return nil
	}
	h.queued[req.Addr] = true
	h.mu.Unlock()
	select {
	case h.kick <- req:
		return nil
	default:
		h.mu.Lock()
		delete(h.queued, req.Addr)
		h.mu.Unlock()
		return ErrBusy
	}
}

// Files lists the files the watch with this unit number handed over, from its spool's record.
func (h *Hub) Files(unit uint32) ([]fileaccess.StoredFile, error) {
	return fileaccess.ReadDirFiles(filepath.Join(h.opt.DataDir, "watches", strconv.FormatUint(uint64(unit), 10)))
}

// Bluetooth is this computer's adapter as the Devices page shows it.
type Bluetooth struct {
	bluez.Adapter
	Busy *Holder
}

// Bluetooth reports the adapter's state and who uses it now.
func (h *Hub) Bluetooth(ctx context.Context) (Bluetooth, error) {
	a, err := h.opt.AdapterState(ctx)
	if err != nil {
		return Bluetooth{}, err
	}
	b := Bluetooth{Adapter: a}
	if hd, ok := h.Busy(); ok {
		b.Busy = &hd
	}
	return b, nil
}

// Known reports whether this computer syncs the watch at addr (it is paired and in node.json).
func (h *Hub) Known(addr string) bool {
	w, err := h.opt.Watches()
	if err != nil {
		return false
	}
	_, ok := w[addr]
	return ok
}

// Exclusive runs fn while holding Bluetooth (waiting for a running sync or live view to end),
// for changes a sync must not see half done: unpairing, a hand-over, deleting a watch's data.
func (h *Hub) Exclusive(ctx context.Context, kind string, fn func() error) error {
	release, err := h.take(ctx, kind, "", true)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

// Unpair removes the pairing with the watch at addr (Bluetooth held) and forgets its status.
func (h *Hub) Unpair(ctx context.Context, addr string) error {
	return h.Exclusive(ctx, "unpair", func() error { return h.ForgetLocked(ctx, addr) })
}

// ForgetLocked unpairs the watch; the caller holds Bluetooth (inside Exclusive).
func (h *Hub) ForgetLocked(ctx context.Context, addr string) error {
	if err := h.opt.Forget(ctx, addr); err != nil {
		return err
	}
	h.mu.Lock()
	delete(h.status, addr)
	h.mu.Unlock()
	return nil
}

// PurgeSpool empties the watch's spool of fetched files (call inside Exclusive).
func (h *Hub) PurgeSpool(unit uint32) error { return syncnode.PurgeSpool(h.opt.DataDir, unit) }

// PowerOn switches the adapter on (never off: every sync depends on it).
func (h *Hub) PowerOn(ctx context.Context) error { return h.opt.PowerOn(ctx) }

// Status returns the state of the watch at addr.
func (h *Hub) Status(addr string) Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := *h.state(addr)
	_, st.Live = h.lives[addr]
	return st
}

// syncOne runs a requested sync, waiting for Bluetooth when wait is set.
func (h *Hub) syncOne(ctx context.Context, req Request, wait bool) {
	defer func() {
		h.mu.Lock()
		delete(h.queued, req.Addr)
		h.mu.Unlock()
	}()
	release, err := h.take(ctx, "sync", req.Addr, wait)
	if err != nil {
		if !wait {
			h.log.Info("nodehub: watch busy, sync skipped", "watch", req.Addr)
		}
		return
	}
	defer release()
	watches, err := h.opt.Watches()
	if err != nil {
		return
	}
	if rec, ok := watches[req.Addr]; ok {
		h.run(ctx, req, rec)
	}
}

// run syncs one watch (Bluetooth held) and keeps its status.
func (h *Hub) run(ctx context.Context, req Request, rec syncnode.WatchRecord) {
	addr := req.Addr
	h.mu.Lock()
	st := h.state(addr)
	st.Running, st.LastStart = true, time.Now()
	st.Phase, st.Listed, st.Done, st.Total = "connecting", 0, 0, 0
	h.mu.Unlock()

	run, err := h.sync(ctx, req, rec)
	h.mu.Lock()
	defer h.mu.Unlock()
	st.Running, st.Phase, st.LastEnd = false, "", time.Now()
	switch {
	case errors.Is(err, errPaused):
		st.NextSync = st.LastEnd.Add(h.opt.Interval)
		return
	case err == nil:
		st.LastOK, st.LastError, st.ErrorCode, st.LastGood = true, "", "", st.LastStart
		st.Failures, st.Away, st.AwaySince, st.Misses = 0, false, time.Time{}, 0
		st.NextSync = st.LastStart.Add(h.opt.Interval)
		h.log.Info("nodehub: synced", "watch", addr, "by", req.By, "listed", run.Listed, "pulled", run.Pulled,
			"archived", run.Archived, "imported", run.Imported, "took", st.LastEnd.Sub(st.LastStart).Round(time.Second))
	case run.ErrorCode == syncnode.CodeNotInRange:
		h.log.Info("nodehub: the watch is not in range", "watch", addr, "retry_in", backoff(st.Misses+1, h.opt.Interval))
		st.LastOK, st.LastError, st.ErrorCode = false, err.Error(), run.ErrorCode
		if !st.Away {
			st.Away, st.AwaySince = true, st.LastStart
		}
		st.Misses++
		st.NextSync = st.LastEnd.Add(backoff(st.Misses, h.opt.Interval))
	case run.ErrorCode == CodeNoOwner:
		st.LastOK, st.LastError, st.ErrorCode = false, err.Error(), run.ErrorCode
		st.NextSync = st.LastEnd.Add(h.opt.Interval)
	default:
		st.LastOK, st.LastError, st.ErrorCode = false, err.Error(), run.ErrorCode
		st.Failures++
		st.Away, st.AwaySince, st.Misses = false, time.Time{}, 0
		st.NextSync = st.LastEnd.Add(backoff(st.Failures, h.opt.Interval))
		h.log.Warn("nodehub: sync failed", "watch", addr, "err", err, "retry_in", backoff(st.Failures, h.opt.Interval))
	}
	st.Pulled, st.Imported = run.Pulled, run.Imported
}

// progress turns the sync's item events into the watch's status.
func (h *Hub) progress(addr string) func(fileaccess.Event) {
	done := map[string]bool{}
	return func(ev fileaccess.Event) {
		h.mu.Lock()
		defer h.mu.Unlock()
		st := h.state(addr)
		switch ev.Kind {
		case fileaccess.EventListed:
			st.Listed++
			st.Phase = "listing"
		case fileaccess.EventQueued:
			st.Total += ev.Queued
			st.Phase = "fetching"
		case fileaccess.EventArchived, fileaccess.EventKept, fileaccess.EventDeferred, fileaccess.EventGone,
			fileaccess.EventFailed, fileaccess.EventDeleted:
			if uid := ev.Item.UID.String(); !done[uid] {
				done[uid] = true
				st.Done = len(done)
			}
		}
	}
}

func (h *Hub) sync(ctx context.Context, req Request, rec syncnode.WatchRecord) (store.SyncRun, error) {
	addr := req.Addr
	run := store.SyncRun{Started: time.Now(), StartedBy: req.By}
	w, err := h.be.Watch(ctx, addr, rec)
	if err != nil {
		return run, err
	}
	if w.Paused && req.By == store.StartedBySchedule {
		return run, errPaused
	}
	run.DeviceID = w.DeviceID
	spool := filepath.Join(h.opt.DataDir, "watches", strconv.FormatUint(uint64(rec.Unit), 10), "files")
	out := Outcome{Battery: noBattery()}
	finish := func(err error) (store.SyncRun, error) {
		run.Finished, run.OK = time.Now(), err == nil
		if err != nil {
			run.Error = err.Error()
			if run.ErrorCode == "" {
				run.ErrorCode = syncnode.ErrorCode(err)
			}
		}
		out.Run = run
		if rerr := h.be.Finish(ctx, w, out); rerr != nil {
			h.log.Warn("nodehub: recording the sync", "err", rerr)
		}
		return run, err
	}

	// Whose watch is it now? Nobody's: it is not synced (what the watch records meanwhile belongs
	// to whoever adds it next). Files from an earlier owner's time still waiting in the spool are
	// imported for them.
	if w.NoOwner != nil {
		n, _ := h.be.Import(ctx, w, spool)
		run.Imported += n
		run.ErrorCode = CodeNoOwner
		return finish(w.NoOwner)
	}
	run.OwnerID = w.Owner

	// A sync node first hands over what an earlier sync left in the spool (the server was away),
	// so the watch may flag those files in this sync.
	if _, ok := h.be.(Hander); ok {
		if n, ierr := h.be.Import(ctx, w, spool); ierr != nil {
			h.log.Info("nodehub: files from an earlier sync still wait for the server", "watch", addr, "err", ierr)
		} else {
			run.Imported += n
		}
	}

	sctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	sopt := syncnode.Options{DataDir: h.opt.DataDir, Adapter: h.opt.Adapter, Device: addr,
		NoArchive: h.opt.NoArchive || w.AfterFetch == store.AfterFetchKeep, DeleteActivities: w.AfterFetch == store.AfterFetchArchiveDelete,
		IncludeSynced: req.IncludeSynced, Delete: req.Delete,
		IsStored: func(uid string) bool { return h.be.Stored(ctx, w, uid) },
		Location: w.Zone, Logger: h.log, OnEvent: h.progress(addr), Features: h.opt.Features}
	inv, stock := h.be.(Inventorier)
	if stock {
		sopt.Inventory = syncnode.InventoryTypes
	}
	ob, sends := h.be.(Outboxer)
	if sends {
		files, oerr := ob.Outbox(ctx, w)
		if oerr != nil {
			h.log.Warn("nodehub: what to send to the watch", "watch", addr, "err", oerr)
		}
		sopt.Send = files
	}
	handed := 0
	if hd, ok := h.be.(Hander); ok {
		sopt.Hand = func(ctx context.Context, f fileaccess.StoredFile) error {
			if err := hd.Hand(ctx, w, f); err != nil {
				return err
			}
			handed++
			return nil
		}
	}
	res, err := h.opt.Sync(sctx, sopt)
	if res.WellnessMet > 0 {
		h.log.Info("nodehub: the watch reported its wellness threshold met", "watch", addr, "times", res.WellnessMet)
	}
	run.Listed, run.Failed = res.Report.Listed, res.Report.Failed
	run.Pulled, run.Archived, run.Deleted, run.Bytes = res.Report.Pulled, res.Report.Archived, res.Report.Deleted, res.Report.Bytes
	run.Imported += handed
	out.Battery = res.Battery
	out.DeleteSupport = deleteSupport(res.Deleted)
	if res.StoreDir != "" {
		spool = filepath.Join(res.StoreDir, "files")
	}
	if err != nil {
		run.ErrorCode = syncnode.ErrorCode(err)
	}

	// Whatever reached the spool is imported, also after a failed sync, each file into the
	// account that owned the watch when it was recorded. (A sync node handed most of it over
	// already; what is left waits for the server.)
	h.mu.Lock()
	h.state(addr).Phase = "importing"
	h.mu.Unlock()
	n, ierr := h.be.Import(ctx, w, spool)
	run.Imported += n
	if ierr != nil && err == nil {
		if _, node := h.be.(Hander); !node {
			err = ierr
			run.ErrorCode = CodeImport
		}
	}
	// What became of the pushes, before the inventory that lists them.
	if sends && len(res.Sent) > 0 {
		ok := 0
		for _, o := range res.Sent {
			if o.Err == nil {
				ok++
			}
		}
		h.log.Info("nodehub: sent to the watch", "watch", addr, "files", ok, "of", len(sopt.Send))
		run.Sent = ok
		if serr := ob.Sent(ctx, w, res.Sent, time.Now()); serr != nil {
			h.log.Warn("nodehub: recording what was sent to the watch", "watch", addr, "err", serr)
		}
	}
	// What the watch keeps now, after the files are stored (so they link to its items).
	if stock && res.Inventory != nil {
		if ierr := inv.Inventory(ctx, w, *res.Inventory); ierr != nil {
			h.log.Warn("nodehub: recording what the watch keeps", "watch", addr, "err", ierr)
		}
	}
	return finish(err)
}

// noBattery is an outcome's battery before the watch reported one.
func noBattery() watch.Battery { return watch.Battery{Percent: -1} }

// deleteSupport tells whether the watch deletes files on request, from the outcome of the
// deletes a sync asked for: "yes", "no", or "" when none showed it.
func deleteSupport(outs []syncnode.DeleteOutcome) string {
	for _, o := range outs {
		var de *fileaccess.DeleteError
		switch {
		case o.Err == nil:
			return "yes"
		case errors.As(o.Err, &de) && de.Unsupported():
			return "no"
		}
	}
	return ""
}

// live is one watch's live link with its viewers.
type live struct {
	cancel   context.CancelFunc
	release  func()
	subs     map[chan realtime.Reading]bool
	linger   *time.Timer
	since    time.Time
	recent   []realtime.Reading // the last LiveHistory of readings, for new viewers
	timedOut bool
	err      error // why the link ended, set before the viewers' channels close
}

// Viewer is one subscriber of a watch's live link.
type Viewer struct {
	// C carries the readings: first the link's recent ones (up to LiveHistory), then live
	// ones. It closes when the link ends or the viewer stops.
	C <-chan realtime.Reading
	// Since is when the link opened.
	Since time.Time
	stop  func()
	err   func() error
}

// NewViewer builds a viewer from its parts, for other implementations of a live source (test
// stubs among them).
func NewViewer(c <-chan realtime.Reading, since time.Time, stop func(), err func() error) *Viewer {
	return &Viewer{C: c, Since: since, stop: stop, err: err}
}

// Stop ends this viewer; the link closes LiveLinger after its last viewer stopped.
func (v *Viewer) Stop() { v.stop() }

// Err is why the link ended once C has closed: nil when its viewers left, ErrTimeLimit after
// LiveMax, ErrStopping when the server stops, ErrLinkClosed or the Bluetooth error otherwise.
func (v *Viewer) Err() error { return v.err() }

// Live subscribes to the watch's live values. The link opens with the first viewer (about 10 s)
// and closes LiveLinger after the last one stops, or after LiveMax. A busy watch is a
// *BusyError (errors.Is ErrBusy).
func (h *Hub) Live(ctx context.Context, addr string) (*Viewer, error) {
	watches, err := h.opt.Watches()
	if err != nil {
		return nil, err
	}
	if _, ok := watches[addr]; !ok {
		return nil, ErrUnknown
	}
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return nil, ErrStopping
	}
	l := h.lives[addr]
	if l == nil {
		h.mu.Unlock()
		release, err := h.take(ctx, "live", addr, false)
		if err != nil {
			return nil, err
		}
		h.mu.Lock()
		if h.stopped {
			h.mu.Unlock()
			release()
			return nil, ErrStopping
		}
		if l = h.lives[addr]; l != nil { // another viewer opened it meanwhile
			release()
		} else {
			lctx, cancel := context.WithCancel(context.Background())
			l = &live{cancel: cancel, release: release, subs: map[chan realtime.Reading]bool{}, since: h.opt.Clock.Now()}
			h.lives[addr] = l
			h.liveWG.Add(1)
			go h.runLive(lctx, addr, l)
		}
	}
	if l.linger != nil {
		l.linger.Stop()
		l.linger = nil
	}
	// Room for the history and a backlog; the history goes in first, under the lock, so no
	// live reading overtakes it.
	ch := make(chan realtime.Reading, len(l.recent)+256)
	for _, r := range l.recent {
		ch <- r
	}
	l.subs[ch] = true
	h.mu.Unlock()
	stop := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if !l.subs[ch] {
			return
		}
		delete(l.subs, ch)
		close(ch)
		if len(l.subs) == 0 && l.linger == nil {
			l.linger = time.AfterFunc(h.opt.LiveLinger, func() {
				h.mu.Lock()
				idle := len(l.subs) == 0
				h.mu.Unlock()
				if idle {
					l.cancel()
				}
			})
		}
	}
	reason := func() error {
		h.mu.Lock()
		defer h.mu.Unlock()
		return l.err
	}
	return NewViewer(ch, l.since, stop, reason), nil
}

func (h *Hub) runLive(ctx context.Context, addr string, l *live) {
	defer h.liveWG.Done()
	limit := h.opt.Clock.AfterFunc(h.opt.LiveMax, func() {
		h.mu.Lock()
		l.timedOut = true
		h.mu.Unlock()
		l.cancel()
	})
	defer limit.Stop()
	// Messages goarmin cannot read yet are logged each time they change, so their format can be
	// worked out from the log later (intensity minutes, active time).
	lastRaw := map[multilink.Service]string{}
	_, err := h.opt.Live(ctx, syncnode.Options{DataDir: h.opt.DataDir, Adapter: h.opt.Adapter, Device: addr, Logger: h.log,
		Features: h.opt.Features},
		LiveServices, func(r realtime.Reading) {
			if r.Name == "raw" {
				if hx := fmt.Sprintf("% x", r.Raw); lastRaw[r.Service] != hx {
					lastRaw[r.Service] = hx
					h.log.Info("nodehub: realtime message not read yet", "watch", addr, "service", r.Service, "bytes", hx)
				}
			}
			h.mu.Lock()
			l.recent = append(l.recent, r)
			cut := 0
			for cut < len(l.recent) && r.At.Sub(l.recent[cut].At) > LiveHistory {
				cut++
			}
			if cut > 0 {
				l.recent = append(l.recent[:0], l.recent[cut:]...)
			}
			for ch := range l.subs {
				select {
				case ch <- r:
				default: // a slow viewer misses a value, nobody else does
				}
			}
			h.mu.Unlock()
		})
	l.release() // Bluetooth is free before the link counts as ended
	if err != nil && ctx.Err() == nil {
		h.log.Warn("nodehub: live link ended", "watch", addr, "err", err)
	}
	h.mu.Lock()
	switch {
	case l.timedOut:
		l.err = ErrTimeLimit
	case ctx.Err() != nil && h.stopped:
		l.err = ErrStopping
	case ctx.Err() != nil: // its viewers left
	case err != nil:
		l.err = err
	default:
		l.err = ErrLinkClosed
	}
	delete(h.lives, addr)
	for ch := range l.subs {
		delete(l.subs, ch)
		close(ch)
	}
	if l.linger != nil {
		l.linger.Stop()
	}
	h.mu.Unlock()
	l.cancel()
}

// endLive refuses new live links, ends the open ones and waits until their Bluetooth links are
// closed.
func (h *Hub) endLive() {
	h.mu.Lock()
	h.stopped = true
	for _, l := range h.lives {
		l.cancel()
	}
	h.mu.Unlock()
	h.liveWG.Wait()
}

// seed fills the status of each known watch from its last recorded syncs, so a restart does not
// forget when it last synced, how often it failed and when it is due.
func (h *Hub) seed(ctx context.Context) {
	watches, err := h.opt.Watches()
	if err != nil {
		return
	}
	for addr, rec := range watches {
		runs, err := h.be.History(ctx, rec, 50)
		if err != nil || len(runs) == 0 {
			continue
		}
		last := runs[0]
		fails, misses := 0, 0
		awaySince, good := time.Time{}, time.Time{}
		for _, r := range runs {
			if r.OK {
				good = r.Started
				break
			}
			switch r.ErrorCode {
			case syncnode.CodeNotInRange:
				if fails == 0 {
					awaySince = r.Started
					misses++
				}
			case CodeNoOwner:
			default:
				fails++
			}
		}
		h.mu.Lock()
		st := h.state(addr)
		if st.LastStart.IsZero() {
			st.LastStart, st.LastEnd, st.LastOK, st.LastError = last.Started, last.Finished, last.OK, last.Error
			st.ErrorCode, st.Pulled, st.Imported, st.Failures = last.ErrorCode, last.Pulled, last.Imported, fails
			st.Listed, st.LastGood = last.Listed, good
			switch {
			case last.OK:
				st.NextSync = last.Started.Add(h.opt.Interval)
			case last.ErrorCode == syncnode.CodeNotInRange:
				st.Away, st.AwaySince, st.Misses = true, awaySince, misses
				st.NextSync = last.Finished.Add(backoff(misses, h.opt.Interval))
			default:
				st.NextSync = last.Finished.Add(backoff(max(fails, 1), h.opt.Interval))
			}
		}
		h.mu.Unlock()
	}
}
