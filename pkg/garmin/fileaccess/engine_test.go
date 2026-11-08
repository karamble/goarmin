// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fileaccess_test

import (
	"bytes"
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/karamble/goarmin/pkg/garmin/fakewatch"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/multilink/memlink"
	"github.com/karamble/goarmin/pkg/garmin/smart"
	"github.com/karamble/goarmin/pkg/garmin/watch"
)

// fastMLR keeps lossy tests quick: the real 1 s RTO would make them take minutes.
var fastMLR = multilink.MLRConfig{InitialRTO: 30 * time.Millisecond, MaxRTO: 300 * time.Millisecond, AckDelay: 2 * time.Millisecond, MaxRetransmit: 60}

// newWatch makes a fake watch with the same fast MLR timers as the host.
func newWatch(opt fakewatch.Options) *fakewatch.Watch {
	opt.MLR = fastMLR
	return fakewatch.New(opt)
}

type session struct {
	conn  *watch.Conn
	link  *memlink.Link
	serve chan error
}

// connect links a host to the fake watch over memlink, with loss on MLR packets, and runs the
// handshake.
func connect(t *testing.T, w *fakewatch.Watch, loss float64, seed uint64) *session {
	t.Helper()
	var filter func(memlink.Side, []byte) int
	if loss > 0 {
		filter = memlink.RandomLoss(seed, loss, memlink.MLRAny)
	}
	a, b := memlink.Pipe(memlink.Options{Filter: filter})
	s := &session{link: a, serve: make(chan error, 1)}
	go func() { s.serve <- w.Serve(context.Background(), b) }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := watch.Handshake(ctx, a, watch.Options{MLR: fastMLR})
	if err != nil {
		a.Close()
		t.Fatalf("handshake: %v", err)
	}
	s.conn = conn
	t.Cleanup(s.close)
	return s
}

func (s *session) close() {
	s.conn.Close()
	s.link.Close()
	<-s.serve
}

func payload(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, seed^0xA5A5))
	b := make([]byte, n)
	for i := range b {
		if i%4 == 0 {
			b[i] = byte(r.Uint32()) // partly random, partly compressible, like FIT records
		} else {
			b[i] = byte(i / 64)
		}
	}
	return b
}

// populate adds 30 wanted files (health, metrics and sports) and 2 crash reports.
func populate(w *fakewatch.Watch) {
	for i := range 30 {
		typ := "FIT_TYPE_32"
		switch {
		case i%5 == 1:
			typ = "FIT_TYPE_44"
		case i%10 == 7:
			typ = "FIT_TYPE_3" // sports settings: pulled once, never archived
		}
		w.AddFile(typ, payload(uint64(i), 200+i*1500))
	}
	w.AddFile("ErrorShutdownReports", payload(100, 500))
	w.AddFile("ErrorShutdownReports", payload(101, 700))
}

func syncCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestSyncEndToEnd(t *testing.T) {
	store := fileaccess.NewMemStore()
	var w *fakewatch.Watch
	var early atomic.Int32
	w = newWatch(fakewatch.Options{PageSize: 7, Faults: fakewatch.Faults{
		// Runs when the ModifyFlagsRequest reaches the watch: the file must be stored by then.
		DropOnArchive: func(f *fakewatch.File, _ int) bool {
			if _, ok := store.File(f.UID); !ok {
				early.Add(1)
			}
			return false
		},
	}})
	populate(w)
	s := connect(t, w, 0.05, 1)

	var events []fileaccess.Event
	var mu sync.Mutex
	eng := fileaccess.New(s.conn, store, fileaccess.Options{OnEvent: func(ev fileaccess.Event) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}})
	rep, err := eng.Sync(syncCtx(t))
	if err != nil {
		t.Fatalf("sync: %v (report %+v)", err, rep)
	}
	if rep.Listed != 32 || rep.Ignored != 2 || rep.Added != 30 || rep.Pulled != 30 || rep.Archived != 27 || rep.Kept != 3 || rep.Failed != 0 {
		t.Fatalf("report %+v", rep)
	}
	if early.Load() != 0 {
		t.Fatalf("%d files were flagged on the watch before the store had them", early.Load())
	}
	for _, f := range w.Files() {
		got, stored := store.File(f.UID)
		switch f.TypeName {
		case "ErrorShutdownReports":
			if stored || f.Synced {
				t.Errorf("crash report %s was touched", f.UID)
			}
		case "FIT_TYPE_3":
			if !stored || f.Synced {
				t.Errorf("sports file %s: stored %v, synced %v; want stored, not synced", f.UID, stored, f.Synced)
			}
		default:
			if !stored || !f.Synced {
				t.Errorf("%s: stored %v, synced %v", f, stored, f.Synced)
			}
		}
		if stored && !bytes.Equal(got, f.Data) {
			t.Errorf("%s: stored bytes differ", f)
		}
	}
	// Health first: no sports file is pulled before the last file to archive.
	mu.Lock()
	lastArchivePull, firstKeptPull := -1, -1
	for i, ev := range events {
		if ev.Kind != fileaccess.EventPulled {
			continue
		}
		if ev.Item.TypeName == "FIT_TYPE_3" {
			if firstKeptPull < 0 {
				firstKeptPull = i
			}
		} else {
			lastArchivePull = i
		}
	}
	mu.Unlock()
	if firstKeptPull < lastArchivePull {
		t.Errorf("a pull-once file was pulled before the health files were done")
	}
	st := w.Stats()
	if st.Pages < 5 {
		t.Errorf("listed in %d pages, want paging", st.Pages)
	}
	services := map[multilink.Service]bool{}
	for _, svc := range st.PipeServices {
		services[svc] = true
	}
	if len(services) != len(multilink.FileTransferServices) {
		t.Errorf("pipes used %d services, want all %d in rotation", len(services), len(multilink.FileTransferServices))
	}
	if _, ok, _ := store.Cursor(context.Background()); !ok {
		t.Error("no cursor stored")
	}

	// A second run finds nothing new and pulls nothing.
	rep, err = eng.Sync(syncCtx(t))
	if err != nil || rep.Pulled != 0 || rep.Added != 0 || rep.Archived != 0 {
		t.Fatalf("second sync: %+v, %v", rep, err)
	}
	if got := w.Stats().Pulls; got != 30 {
		t.Fatalf("the watch served %d pulls, want 30", got)
	}
}

// The link drops after a file was stored but before the watch flagged it. The next run
// archives it without pulling it again.
func TestSyncCrashBetweenPersistAndArchive(t *testing.T) {
	var drops atomic.Int32
	w := newWatch(fakewatch.Options{Faults: fakewatch.Faults{
		DropOnArchive: func(f *fakewatch.File, attempt int) bool {
			return attempt == 0 && drops.Add(1) == 3
		},
	}})
	for i := range 6 {
		w.AddFile("FIT_TYPE_32", payload(uint64(i), 3000))
	}
	store := fileaccess.NewMemStore()
	s := connect(t, w, 0, 0)
	rep, err := fileaccess.New(s.conn, store, fileaccess.Options{}).Sync(syncCtx(t))
	if err == nil {
		t.Fatalf("sync survived the link loss: %+v", rep)
	}
	if rep.Archived != 2 || rep.Pulled != 3 {
		t.Fatalf("first run %+v", rep)
	}
	pend, _ := store.Pending(context.Background())
	if len(pend) != 4 || !pend[0].Pulled {
		t.Fatalf("pending after the crash: %+v", pend)
	}
	victim := pend[0].UID

	s = connect(t, w, 0, 0)
	rep, err = fileaccess.New(s.conn, store, fileaccess.Options{}).Sync(syncCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Pulled != 3 || rep.Archived != 4 {
		t.Fatalf("second run %+v, want 3 pulls and 4 archives", rep)
	}
	if w.Unsynced() != 0 {
		t.Fatalf("%d files still unsynced", w.Unsynced())
	}
	if o, _ := store.Outcome(victim); o != fileaccess.Archived {
		t.Fatalf("the interrupted file finished as %v", o)
	}
	if got := w.Stats().Pulls; got != 6 {
		t.Fatalf("the watch served %d pulls, want 6 (none twice)", got)
	}
}

func TestSyncNoArchiveThenArchive(t *testing.T) {
	w := newWatch(fakewatch.Options{})
	for i := range 4 {
		w.AddFile("FIT_TYPE_44", payload(uint64(i), 1000))
	}
	store := fileaccess.NewMemStore()
	s := connect(t, w, 0, 0)
	rep, err := fileaccess.New(s.conn, store, fileaccess.Options{NoArchive: true}).Sync(syncCtx(t))
	if err != nil || rep.Pulled != 4 || rep.Deferred != 4 || w.Unsynced() != 4 {
		t.Fatalf("no-archive run %+v, %v; unsynced %d", rep, err, w.Unsynced())
	}
	rep, err = fileaccess.New(s.conn, store, fileaccess.Options{}).Sync(syncCtx(t))
	if err != nil || rep.Pulled != 0 || rep.Archived != 4 || w.Unsynced() != 0 {
		t.Fatalf("archive run %+v, %v; unsynced %d", rep, err, w.Unsynced())
	}
}

// ArchiveAllowed defers the items it refuses (stored, still on the watch) and a later run
// archives them once allowed, without pulling them again.
func TestSyncArchiveAllowed(t *testing.T) {
	w := newWatch(fakewatch.Options{})
	for i := range 4 {
		w.AddFile("FIT_TYPE_32", payload(uint64(i), 1000))
	}
	store := fileaccess.NewMemStore()
	s := connect(t, w, 0, 0)
	refused := map[smart.UUID]bool{}
	allow := func(it fileaccess.Item) bool {
		if len(refused) < 2 && !refused[it.UID] {
			refused[it.UID] = true
			return false
		}
		return !refused[it.UID]
	}
	rep, err := fileaccess.New(s.conn, store, fileaccess.Options{ArchiveAllowed: allow}).Sync(syncCtx(t))
	if err != nil || rep.Pulled != 4 || rep.Archived != 2 || rep.Deferred != 2 || w.Unsynced() != 2 {
		t.Fatalf("first run %+v, %v; unsynced %d", rep, err, w.Unsynced())
	}
	pend, _ := store.Pending(context.Background())
	if len(pend) != 2 || !pend[0].Pulled || !pend[1].Pulled {
		t.Fatalf("pending after the first run: %+v", pend)
	}
	rep, err = fileaccess.New(s.conn, store, fileaccess.Options{ArchiveAllowed: func(fileaccess.Item) bool { return true }}).Sync(syncCtx(t))
	if err != nil || rep.Pulled != 0 || rep.Archived != 2 || w.Unsynced() != 0 {
		t.Fatalf("second run %+v, %v; unsynced %d", rep, err, w.Unsynced())
	}
	if got := w.Stats().Pulls; got != 4 {
		t.Fatalf("the watch served %d pulls, want 4 (none twice)", got)
	}
}

// Every pull fault fails one item once; the next run fetches them all.
func TestSyncFaults(t *testing.T) {
	var target [5]smart.UUID
	is := func(f *fakewatch.File, k int) bool { return f.UID == target[k] }
	w := newWatch(fakewatch.Options{Faults: fakewatch.Faults{
		StallPull:   func(f *fakewatch.File, a int) bool { return a == 0 && is(f, 0) },
		AbortPull:   func(f *fakewatch.File, a int) bool { return a == 0 && is(f, 1) },
		CorruptPull: func(f *fakewatch.File, a int) bool { return a == 0 && is(f, 2) },
		PullResult: func(f *fakewatch.File, a int) (smart.ItemAccessResult, bool) {
			switch {
			case a == 0 && is(f, 3):
				return smart.ItemAccessNoAvailableResources, true
			case is(f, 4):
				return smart.ItemAccessUnknownItem, true
			}
			return 0, false
		},
	}})
	for i := range 10 {
		f := w.AddFile("FIT_TYPE_32", payload(uint64(i), 4000))
		if i%2 == 0 {
			target[i/2] = f.UID // faults on every other file, so they never come three in a row
		}
	}
	store := fileaccess.NewMemStore()
	s := connect(t, w, 0, 0)
	opt := fileaccess.Options{IdleTimeout: 300 * time.Millisecond, StatusTimeout: 300 * time.Millisecond}
	rep, err := fileaccess.New(s.conn, store, opt).Sync(syncCtx(t))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if rep.Failed != 4 || rep.Gone != 1 || rep.Archived != 5 {
		t.Fatalf("first run %+v, want 4 failed, 1 gone, 5 archived", rep)
	}
	if w.Stats().Cancels < 2 {
		t.Errorf("failed transfers were not cancelled (%d cancels)", w.Stats().Cancels)
	}
	rep, err = fileaccess.New(s.conn, store, opt).Sync(syncCtx(t))
	if err != nil || rep.Archived != 4 || rep.Failed != 0 {
		t.Fatalf("second run %+v, %v", rep, err)
	}
	if o, _ := store.Outcome(target[4]); o != fileaccess.Gone {
		t.Fatalf("vanished item finished as %v", o)
	}
	if w.Unsynced() != 1 { // only the "unknown" one is left on the watch
		t.Fatalf("%d unsynced", w.Unsynced())
	}
}

// Three consecutive failures end the sync.
func TestSyncGivesUp(t *testing.T) {
	w := newWatch(fakewatch.Options{Faults: fakewatch.Faults{
		PullResult: func(*fakewatch.File, int) (smart.ItemAccessResult, bool) {
			return smart.ItemAccessInternalError, true
		},
	}})
	for i := range 5 {
		w.AddFile("FIT_TYPE_32", payload(uint64(i), 100))
	}
	s := connect(t, w, 0, 0)
	rep, err := fileaccess.New(s.conn, fileaccess.NewMemStore(), fileaccess.Options{}).Sync(syncCtx(t))
	if err == nil || rep.Failed != 3 {
		t.Fatalf("got %+v, %v; want an error after 3 failures", rep, err)
	}
}

// A file the watch records during the sync is announced and fetched in the same run.
func TestSyncPicksUpNewItems(t *testing.T) {
	w := newWatch(fakewatch.Options{})
	w.AddFile("FIT_TYPE_32", payload(1, 2000))
	store := fileaccess.NewMemStore()
	s := connect(t, w, 0, 0)
	var once sync.Once
	var late *fakewatch.File
	eng := fileaccess.New(s.conn, store, fileaccess.Options{OnEvent: func(ev fileaccess.Event) {
		if ev.Kind == fileaccess.EventPulled {
			once.Do(func() { late = w.AddFile("FIT_TYPE_49", payload(2, 900)) })
		}
	}})
	// Give the ItemAddedNotification time to arrive before the round ends.
	rep, err := eng.Sync(syncCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Archived != 2 {
		deadline := time.Now().Add(2 * time.Second)
		for rep.Archived < 2 && time.Now().Before(deadline) {
			r, err := eng.Sync(syncCtx(t))
			if err != nil {
				t.Fatal(err)
			}
			rep.Archived += r.Archived
		}
		t.Logf("the announcement arrived after the round; picked up by a later run")
	}
	if _, ok := store.File(late.UID); !ok || w.Unsynced() != 0 {
		t.Fatalf("late file not synced (report %+v)", rep)
	}
}

func TestSyncBusy(t *testing.T) {
	stalled := make(chan struct{})
	var once sync.Once
	w := newWatch(fakewatch.Options{Faults: fakewatch.Faults{StallPull: func(*fakewatch.File, int) bool {
		once.Do(func() { close(stalled) })
		return true
	}}})
	w.AddFile("FIT_TYPE_32", payload(1, 100))
	s := connect(t, w, 0, 0)
	eng := fileaccess.New(s.conn, fileaccess.NewMemStore(), fileaccess.Options{IdleTimeout: time.Second})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = eng.Sync(syncCtx(t))
	}()
	<-stalled // the first sync is inside its pull
	if _, err := eng.Sync(syncCtx(t)); !errors.Is(err, fileaccess.ErrBusy) {
		t.Fatalf("concurrent Sync: %v, want ErrBusy", err)
	}
	<-done
}

func TestDefaultPolicy(t *testing.T) {
	for name, want := range map[string]fileaccess.Action{
		"FIT_TYPE_32": fileaccess.PullArchive, "FIT_TYPE_44": fileaccess.PullArchive, "FIT_TYPE_4": fileaccess.PullArchive,
		"FIT_TYPE_72": fileaccess.PullArchive, "BACKUP_PRIMARY": fileaccess.Ignore,
		"FIT_TYPE_3": fileaccess.PullOnce, "FIT_TYPE_5": fileaccess.PullOnce,
		"ErrorShutdownReports": fileaccess.Ignore, "KPI": fileaccess.Ignore, "": fileaccess.Ignore, "FIT_TYPE_x": fileaccess.Ignore,
	} {
		if got := fileaccess.DefaultPolicy(fileaccess.Item{TypeName: name}); got != want {
			t.Errorf("%q: %v, want %v", name, got, want)
		}
	}
}

// TestSyncSettle: a file the watch announces just after the listing is pulled in the same
// sync, as the Fenix 8 does with its monitoring data.
func TestSyncSettle(t *testing.T) {
	w := newWatch(fakewatch.Options{})
	w.AddFile("FIT_TYPE_32", payload(1, 700))
	store := fileaccess.NewMemStore()
	listed := make(chan struct{})
	var once sync.Once
	eng := fileaccess.New(connect(t, w, 0, 0).conn, store, fileaccess.Options{
		Settle: 3 * time.Second,
		OnEvent: func(ev fileaccess.Event) {
			if ev.Kind == fileaccess.EventPulled {
				once.Do(func() { close(listed) })
			}
		},
	})
	go func() {
		<-listed
		time.Sleep(100 * time.Millisecond)
		w.AddFile("FIT_TYPE_32", payload(2, 900)) // announced with ItemAddedNotification
	}()
	start := time.Now()
	rep, err := eng.Sync(syncCtx(t))
	if err != nil || rep.Pulled != 2 || rep.Archived != 2 {
		t.Fatalf("report %+v, %v", rep, err)
	}
	if time.Since(start) > 2500*time.Millisecond {
		t.Fatalf("the sync waited for the whole settle time after the new file (%s)", time.Since(start))
	}
}

// TestSyncCutWellness: wellness data the watch has not written yet arrives only when the sync
// asks for it, as on the Fenix 8.
func TestSyncCutWellness(t *testing.T) {
	w := newWatch(fakewatch.Options{})
	w.AddUnwritten("FIT_TYPE_32", payload(3, 1500))
	rep, err := fileaccess.New(connect(t, w, 0, 0).conn, fileaccess.NewMemStore(), fileaccess.Options{
		Settle: time.Second,
	}).Sync(syncCtx(t))
	if err != nil || rep.Pulled != 0 || rep.WellnessCut != "" {
		t.Fatalf("without the cut: %+v, %v", rep, err)
	}
	rep, err = fileaccess.New(connect(t, w, 0, 0).conn, fileaccess.NewMemStore(), fileaccess.Options{
		CutWellness: true, Settle: 3 * time.Second,
	}).Sync(syncCtx(t))
	if err != nil || rep.Pulled != 1 || rep.Archived != 1 || rep.WellnessCut != "SUCCESS" {
		t.Fatalf("with the cut: %+v, %v", rep, err)
	}
	if st := w.Stats(); st.Cuts != 1 {
		t.Fatalf("%d cuts", st.Cuts)
	}
}
