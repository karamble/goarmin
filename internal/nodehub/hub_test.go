// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package nodehub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/internal/ingest"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/blobstore"
	"github.com/karamble/goarmin/pkg/clock"
	"github.com/karamble/goarmin/pkg/fitid/fittest"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/gattlink"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/realtime"
	"github.com/karamble/goarmin/pkg/garmin/smart"
	"github.com/karamble/goarmin/pkg/garmin/watch"
)

const addr = "AA:BB:CC:DD:EE:01"
const unit = 3_500_000_001

type fake struct {
	mu        sync.Mutex
	syncs     []syncnode.Options
	files     int
	liveRuns  atomic.Int32
	liveEnded chan struct{}
	block     chan struct{} // if set, Sync waits on it
	// What the next syncs report: events in order, a battery reading, an error.
	events  []fileaccess.Event
	battery *watch.Battery
	err     error
	deleted []syncnode.DeleteOutcome
}

func (f *fake) sync(ctx context.Context, opt syncnode.Options) (syncnode.Result, error) {
	f.mu.Lock()
	f.syncs = append(f.syncs, opt)
	f.files++
	n := f.files
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	dir := filepath.Join(opt.DataDir, "watches", "unit")
	p := filepath.Join(dir, "files", "FIT_TYPE_32", "f"+string(rune('a'+n))+".fit")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return syncnode.Result{}, err
	}
	f.mu.Lock()
	events, battery, ferr, deleted := f.events, f.battery, f.err, f.deleted
	f.mu.Unlock()
	if errors.Is(ferr, gattlink.ErrNotInRange) {
		return syncnode.Result{}, ferr // as SyncOnce before its fix: a zero battery, no time
	}
	data := fittest.File(typedef.FileMonitoringB, unit, time.Now().Add(-time.Duration(n)*time.Hour), 5+n)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return syncnode.Result{}, err
	}
	for _, ev := range events {
		if opt.OnEvent != nil {
			opt.OnEvent(ev)
		}
	}
	res := syncnode.Result{StoreDir: dir, Report: fileaccess.Report{Pulled: 1, Bytes: int64(len(data))},
		Battery: watch.Battery{Percent: -1}, Deleted: deleted}
	if battery != nil {
		res.Battery = *battery
	}
	return res, ferr
}

func (f *fake) live(ctx context.Context, _ syncnode.Options, _ []multilink.Service, fn func(realtime.Reading)) (syncnode.Result, error) {
	f.liveRuns.Add(1)
	defer func() { f.liveEnded <- struct{}{} }()
	t := time.NewTicker(5 * time.Millisecond)
	defer t.Stop()
	for v := 60.0; ; v++ {
		select {
		case <-ctx.Done():
			return syncnode.Result{}, nil
		case <-t.C:
			fn(realtime.Reading{Name: "heart_rate", Value: v, Unit: "bpm", At: time.Now()})
		}
	}
}

func newHub(t *testing.T) (*Hub, *fake, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(dir, "goarmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, _ := blobstore.Open(filepath.Join(dir, "raw"))
	f := &fake{liveEnded: make(chan struct{}, 4)}
	h := New(Options{DataDir: dir, In: &ingest.Ingester{Store: st, Blobs: blobs}, LiveLinger: 50 * time.Millisecond,
		Sync: f.sync, Live: f.live,
		Watches: func() (map[string]syncnode.WatchRecord, error) {
			return map[string]syncnode.WatchRecord{addr: {Unit: unit, Product: 4536, Name: "fenix 8 - 47mm", Firmware: "23.40"}}, nil
		}})
	return h, f, st
}

// scheduled and asked run one sync the way the schedule and Sync now do.
func scheduled(h *Hub, ctx context.Context) {
	h.syncOne(ctx, Request{Addr: addr, By: store.StartedBySchedule}, false)
}

func asked(h *Hub, ctx context.Context) {
	h.syncOne(ctx, Request{Addr: addr, By: store.StartedByManual}, true)
}

func TestSyncImportsAndFollowsSettings(t *testing.T) {
	ctx := context.Background()
	h, f, st := newHub(t)
	// Before the setup nobody owns the watch: it is not even connected.
	scheduled(h, ctx)
	if s := h.Status(addr); s.LastOK || s.Pulled != 0 || s.ErrorCode != CodeNoOwner || len(f.syncs) != 0 {
		t.Fatalf("a sync without an account: %+v, %d connections", s, len(f.syncs))
	}
	admin, err := st.ClaimSetup(ctx, "admin@example.invalid", "", "x")
	if err != nil {
		t.Fatal(err)
	}
	scheduled(h, ctx)
	s := h.Status(addr)
	if !s.LastOK || s.Pulled != 1 || s.Imported != 1 || s.Running || s.NextSync.IsZero() {
		t.Fatalf("status %+v", s)
	}
	dev, err := st.DeviceByUnit(ctx, unit)
	if err != nil || dev.Address != addr || dev.AfterFetch != store.AfterFetchKeep {
		t.Fatalf("device %+v, %v", dev, err)
	}
	if owner, err := st.Owner(ctx, dev.ID, time.Now().Unix()); err != nil || owner != admin.ID {
		t.Fatalf("owner %d, %v", owner, err)
	}
	// The first owner's period starts at the beginning, so the unowned try is in their log too.
	runs, err := st.SyncRuns(ctx, admin.ID, dev.ID, 10)
	if err != nil || len(runs) != 2 || !runs[0].OK || runs[0].Imported != 1 || runs[1].ErrorCode != CodeNoOwner {
		t.Fatalf("runs %+v, %v", runs, err)
	}
	if runs[0].OwnerID != admin.ID || runs[0].StartedBy != store.StartedBySchedule || runs[1].OwnerID != 0 {
		t.Fatalf("who and how: %+v", runs)
	}
	if !f.syncs[len(f.syncs)-1].NoArchive {
		t.Fatal("keep mode archived")
	}
	for mode, want := range map[string][2]bool{store.AfterFetchArchive: {false, false}, store.AfterFetchArchiveDelete: {false, true}} {
		if err := st.SetAfterFetch(ctx, dev.ID, mode); err != nil {
			t.Fatal(err)
		}
		scheduled(h, ctx)
		o := f.syncs[len(f.syncs)-1]
		if o.NoArchive != want[0] || o.DeleteActivities != want[1] || o.Device != addr {
			t.Fatalf("%s: options %+v", mode, o)
		}
	}
	if err := h.SyncNow("11:22:33:44:55:66"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown watch: %v", err)
	}
}

func TestLiveSharedAndLingers(t *testing.T) {
	h, f, st := newHub(t)
	if _, err := st.ClaimSetup(context.Background(), "admin@example.invalid", "", "x"); err != nil {
		t.Fatal(err)
	}
	a, err := h.Live(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.Live(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range []<-chan realtime.Reading{a.C, b.C} {
		if r := <-ch; r.Name != "heart_rate" {
			t.Fatalf("reading %+v", r)
		}
	}
	if f.liveRuns.Load() != 1 || !h.Status(addr).Live {
		t.Fatalf("%d live links for two viewers", f.liveRuns.Load())
	}
	// While live, a scheduled sync is skipped.
	scheduled(h, context.Background())
	if len(f.syncs) != 0 {
		t.Fatal("a scheduled sync ran during the live view")
	}
	a.Stop()
	b.Stop()
	select {
	case <-f.liveEnded:
	case <-time.After(5 * time.Second):
		t.Fatal("the live link did not close after its viewers left")
	}
	// The lane is free again: a sync runs.
	deadline := time.Now().Add(2 * time.Second)
	for h.Status(addr).Live && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	scheduled(h, context.Background())
	if len(f.syncs) != 1 {
		t.Fatalf("%d syncs after the live view", len(f.syncs))
	}
}

func TestLiveBusyDuringSync(t *testing.T) {
	h, f, st := newHub(t)
	if _, err := st.ClaimSetup(context.Background(), "admin@example.invalid", "", "x"); err != nil {
		t.Fatal(err)
	}
	f.block = make(chan struct{})
	done := make(chan struct{})
	go func() {
		asked(h, context.Background())
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !h.Status(addr).Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	_, err := h.Live(context.Background(), addr)
	var busy *BusyError
	if !errors.As(err, &busy) || !errors.Is(err, ErrBusy) || busy.Holder.Kind != "sync" || busy.Holder.Addr != addr {
		t.Fatalf("live during a sync: %v", err)
	}
	close(f.block)
	<-done
}

// TestLiveReplay: a viewer who arrives later first gets what the link already sent.
func TestLiveReplay(t *testing.T) {
	h, _, _ := newHub(t)
	a, err := h.Live(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Stop()
	var seen []float64
	for len(seen) < 5 {
		seen = append(seen, (<-a.C).Value)
	}
	b, err := h.Live(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Stop()
	for i, want := range seen {
		if got := (<-b.C).Value; got != want {
			t.Fatalf("replayed reading %d is %g, want %g", i, got, want)
		}
	}
	if !b.Since.Equal(a.Since) || b.Since.IsZero() {
		t.Fatalf("since %v and %v", a.Since, b.Since)
	}
}

// TestLiveTimeLimit: a link ends after LiveMax with ErrTimeLimit, and Bluetooth is free again.
func TestLiveTimeLimit(t *testing.T) {
	h, f, _ := newHub(t)
	fc := clock.NewFake(time.Now())
	h.opt.Clock = fc
	h.opt.LiveMax = time.Minute
	v, err := h.Live(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	<-v.C
	fc.Advance(time.Minute)
	for range v.C {
	}
	if !errors.Is(v.Err(), ErrTimeLimit) {
		t.Fatalf("end reason %v", v.Err())
	}
	<-f.liveEnded
	if _, ok := h.Busy(); ok {
		t.Fatal("Bluetooth still held after the time limit")
	}
}

// TestLiveEndReason: a link that cannot open ends with the Bluetooth error, for the page to show.
func TestLiveEndReason(t *testing.T) {
	h, _, _ := newHub(t)
	h.opt.Live = func(context.Context, syncnode.Options, []multilink.Service, func(realtime.Reading)) (syncnode.Result, error) {
		return syncnode.Result{}, gattlink.ErrNotInRange
	}
	v, err := h.Live(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	for range v.C {
	}
	if !errors.Is(v.Err(), gattlink.ErrNotInRange) || syncnode.ErrorCode(v.Err()) != syncnode.CodeNotInRange {
		t.Fatalf("end reason %v", v.Err())
	}
}

func TestBackoff(t *testing.T) {
	iv := 30 * time.Minute
	for n, want := range map[int]time.Duration{0: iv, 1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute, 5: 16 * time.Minute, 6: iv, 40: iv} {
		if got := backoff(n, iv); got != want {
			t.Errorf("backoff(%d) = %s, want %s", n, got, want)
		}
	}
}

func TestRunEndsLiveLinks(t *testing.T) {
	h, f, _ := newHub(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.Run(ctx)
		close(done)
	}()
	v, err := h.Live(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	<-v.C
	// The viewer never leaves; stopping the hub still ends its link before Run returns.
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return with a live link open")
	}
	select {
	case <-f.liveEnded:
	default:
		t.Fatal("Run returned before the live link ended")
	}
	for range v.C { // drain: the channel closes once the link ended
	}
	if !errors.Is(v.Err(), ErrStopping) {
		t.Fatalf("end reason %v", v.Err())
	}
	if _, err := h.Live(context.Background(), addr); !errors.Is(err, ErrStopping) {
		t.Fatalf("live after stop: %v", err)
	}
}

func TestStatusSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	h, _, st := newHub(t)
	if _, err := st.ClaimSetup(ctx, "admin@example.invalid", "", "x"); err != nil {
		t.Fatal(err)
	}
	dev, err := st.EnsureDevice(ctx, store.Device{UnitID: unit, Address: addr})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	good := now.Add(-2 * time.Hour)
	for _, r := range []store.SyncRun{
		{DeviceID: dev.ID, Started: good, Finished: good.Add(time.Minute), OK: true, Pulled: 3, Imported: 3},
		{DeviceID: dev.ID, Started: now.Add(-time.Hour), Finished: now.Add(-time.Hour + time.Minute), Error: "out of range"},
		{DeviceID: dev.ID, Started: now.Add(-30 * time.Minute), Finished: now.Add(-29 * time.Minute), Error: "out of range"},
	} {
		if err := st.RecordSyncRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	h.seed(ctx)
	s := h.Status(addr)
	if s.LastOK || s.Failures != 2 || s.LastError != "out of range" || s.LastStart.Unix() != now.Add(-30*time.Minute).Unix() {
		t.Fatalf("status after a restart %+v", s)
	}
	if want := now.Add(-29 * time.Minute).Add(backoff(2, h.opt.Interval)); s.NextSync.Unix() != want.Unix() {
		t.Fatalf("next sync %s, want %s", s.NextSync, want)
	}
}

func TestUnownedWatchOnASharedServer(t *testing.T) {
	ctx := context.Background()
	h, f, st := newHub(t)
	for _, e := range []string{"a@example.invalid", "b@example.invalid"} {
		u, err := st.EnsureUser(ctx, e, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetPassword(ctx, u.ID, "x"); err != nil {
			t.Fatal(err)
		}
	}
	// Two people: nobody gets the watch by default; it waits to be added, unconnected.
	scheduled(h, ctx)
	if s := h.Status(addr); s.LastOK || s.LastError != ErrNoOwner.Error() || s.Failures != 0 || len(f.syncs) != 0 {
		t.Fatalf("status %+v, %d connections", s, len(f.syncs))
	}
	dev, _ := st.DeviceByUnit(ctx, unit)
	if _, err := st.Owner(ctx, dev.ID, time.Now().Unix()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the watch got an owner: %v", err)
	}
}

// owned sets up a hub whose watch has an owner.
func owned(t *testing.T) (*Hub, *fake, *store.Store, store.User) {
	t.Helper()
	h, f, st := newHub(t)
	u, err := st.ClaimSetup(context.Background(), "admin@example.invalid", "", "x")
	if err != nil {
		t.Fatal(err)
	}
	return h, f, st, u
}

func item(n byte) fileaccess.Item {
	return fileaccess.Item{UID: smart.UUID{LSB: uint64(n)}, TypeName: "FIT_TYPE_32"}
}

func TestProgressBatteryAndDeletes(t *testing.T) {
	ctx := context.Background()
	h, f, st, _ := owned(t)
	f.events = []fileaccess.Event{
		{Kind: fileaccess.EventListed, Item: item(1)}, {Kind: fileaccess.EventListed, Item: item(2)},
		{Kind: fileaccess.EventListed, Item: item(3)},
		{Kind: fileaccess.EventQueued, Queued: 2},
		{Kind: fileaccess.EventPulled, Item: item(1)}, {Kind: fileaccess.EventArchived, Item: item(1)},
		{Kind: fileaccess.EventDeleted, Item: item(1)}, // the same item: counted once
		{Kind: fileaccess.EventPulled, Item: item(2)}, {Kind: fileaccess.EventDeferred, Item: item(2)},
	}
	f.battery = &watch.Battery{Percent: 76, Status: "good", At: time.Now()}
	f.deleted = []syncnode.DeleteOutcome{{UID: "x"}}
	asked(h, ctx)
	s := h.Status(addr)
	if s.Listed != 3 || s.Total != 2 || s.Done != 2 || s.Phase != "" {
		t.Fatalf("progress %+v", s)
	}
	dev, _ := st.DeviceByUnit(ctx, unit)
	if dev.Battery != 76 || dev.BatteryAt.IsZero() || dev.DeleteSupport != "yes" {
		t.Fatalf("device %+v", dev)
	}
	runs, _ := st.RecentSyncRuns(ctx, dev.ID, 1)
	if runs[0].StartedBy != store.StartedByManual {
		t.Fatalf("run %+v", runs[0])
	}
}

func TestOutOfRangeIsNotAFailure(t *testing.T) {
	ctx := context.Background()
	h, f, st, _ := owned(t)
	f.err = fmt.Errorf("%w: not seen", gattlink.ErrNotInRange)
	scheduled(h, ctx)
	scheduled(h, ctx)
	if dev, _ := st.DeviceByUnit(ctx, unit); dev.Battery != -1 {
		t.Fatalf("a sync that never reached the watch stored a battery: %+v", dev)
	}
	s := h.Status(addr)
	if !s.Away || s.Misses != 2 || s.Failures != 0 || s.ErrorCode != syncnode.CodeNotInRange {
		t.Fatalf("away %+v", s)
	}
	if got := s.NextSync.Sub(s.LastEnd); got != backoff(2, h.opt.Interval) {
		t.Fatalf("next try in %s", got)
	}
	f.err = errors.New("the pipe broke")
	scheduled(h, ctx)
	if s := h.Status(addr); s.Away || s.Failures != 1 || s.ErrorCode != syncnode.CodeTransfer {
		t.Fatalf("failure %+v", s)
	}
	f.err = nil
	scheduled(h, ctx)
	if s := h.Status(addr); !s.LastOK || s.Failures != 0 || s.Misses != 0 {
		t.Fatalf("recovered %+v", s)
	}
}

func TestPausedAndOwnerZone(t *testing.T) {
	ctx := context.Background()
	h, f, st, u := owned(t)
	dev, err := st.EnsureDevice(ctx, store.Device{UnitID: unit, Address: addr})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AssignIfUnowned(ctx, dev.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateProfile(ctx, u.ID, store.Profile{Timezone: ptr("Europe/Berlin")}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPaused(ctx, dev.ID, true); err != nil {
		t.Fatal(err)
	}
	scheduled(h, ctx)
	if len(f.syncs) != 0 {
		t.Fatal("a paused watch synced on schedule")
	}
	asked(h, ctx) // Sync now still works
	if len(f.syncs) != 1 || f.syncs[0].Location == nil || f.syncs[0].Location.String() != "Europe/Berlin" {
		t.Fatalf("asked sync %+v", f.syncs)
	}
}

func TestRequestsWaitForBluetooth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h, f, _, _ := owned(t)
	release, err := h.take(ctx, "pairing", addr, false)
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := h.Busy(); !ok || b.Kind != "pairing" {
		t.Fatalf("busy %+v %v", b, ok)
	}
	done := make(chan struct{})
	go func() {
		h.Run(ctx)
		close(done)
	}()
	// Asking twice while Bluetooth is taken queues one sync.
	if err := h.SyncNow(addr); err != nil {
		t.Fatal(err)
	}
	if err := h.SyncNow(addr); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	f.mu.Lock()
	n := len(f.syncs)
	f.mu.Unlock()
	if n != 0 {
		t.Fatal("a sync ran while Bluetooth was taken")
	}
	release()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		n = len(f.syncs)
		f.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	f.mu.Lock()
	n = len(f.syncs)
	f.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d syncs after Bluetooth was free", n)
	}
	cancel()
	<-done
}

func TestAnotherProcessHasBluetooth(t *testing.T) {
	ctx := context.Background()
	h, f, _, _ := owned(t)
	unlock, err := syncnode.LockBluetooth(h.opt.DataDir) // as goarmin sync on the same data would
	if err != nil {
		t.Fatal(err)
	}
	scheduled(h, ctx)
	_, err = h.Live(ctx, addr)
	var busy *BusyError
	if !errors.As(err, &busy) || busy.Holder.Kind != "command" {
		t.Fatalf("live while a command holds Bluetooth: %v", err)
	}
	if len(f.syncs) != 0 {
		t.Fatal("synced while a command held Bluetooth")
	}
	unlock()
	scheduled(h, ctx)
	if len(f.syncs) != 1 {
		t.Fatal("no sync after the command finished")
	}
}

func ptr[T any](v T) *T { return &v }
