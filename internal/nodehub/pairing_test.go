// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package nodehub

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/karamble/goarmin/internal/ingest"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/blobstore"
	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/clock"
	"github.com/karamble/goarmin/pkg/garmin/gattlink"
	"github.com/karamble/goarmin/pkg/garmin/watch"
)

// fakePairer plays BlueZ and the watch for a pairing session.
type fakePairer struct {
	dir string

	mu        sync.Mutex
	devs      []bluez.Device
	bonded    map[string]bool
	confirm   bool   // Pair asks for a numeric comparison
	passkey   uint32 // the code both sides show
	block     bool   // Pair waits for its ctx
	identErr  error
	unit      uint32
	scans     int
	pairs     []string // addresses, "+repair" when repairing
	freshIDs  []bool   // Identify's fresh argument, per call
	unbonded  []string
	pairEnded chan struct{}
}

func newFakePairer(dir string) *fakePairer {
	return &fakePairer{dir: dir, bonded: map[string]bool{}, confirm: true, passkey: 482917, unit: unit,
		devs: []bluez.Device{
			{Address: addr, Name: "fenix 8 - 47mm", RSSI: -58, HasRSSI: true},
			{Address: "AA:BB:CC:DD:EE:02", Name: "Forerunner 965", RSSI: -80, HasRSSI: true},
		}, pairEnded: make(chan struct{}, 4)}
}

func (f *fakePairer) Scan(ctx context.Context, fn func([]bluez.Device) bool) error {
	f.mu.Lock()
	f.scans++
	f.mu.Unlock()
	for {
		f.mu.Lock()
		devs := slices.Clone(f.devs)
		for i := range devs {
			devs[i].Bonded = f.bonded[devs[i].Address]
		}
		f.mu.Unlock()
		if !fn(devs) {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func (f *fakePairer) Pair(ctx context.Context, d bluez.Device, repair bool, p bluez.Prompter) (bluez.Device, bool, error) {
	defer func() { f.pairEnded <- struct{}{} }()
	f.mu.Lock()
	call := d.Address
	if repair {
		call += "+repair"
	}
	f.pairs = append(f.pairs, call)
	bonded, ask, block, pk := f.bonded[d.Address], f.confirm, f.block, f.passkey
	f.mu.Unlock()
	if bonded && !repair {
		d.Bonded = true
		return d, false, nil
	}
	if block {
		<-ctx.Done()
		return d, false, ctx.Err()
	}
	if ask {
		ok, err := p.RequestConfirmation(d, pk)
		if err != nil {
			return d, false, fmt.Errorf("bluez: pair: %w", dbus.Error{Name: "org.bluez.Error.AuthenticationCanceled"})
		}
		if !ok {
			return d, false, fmt.Errorf("bluez: pair: %w", dbus.Error{Name: "org.bluez.Error.AuthenticationRejected"})
		}
	}
	f.mu.Lock()
	f.bonded[d.Address] = true
	f.mu.Unlock()
	d.Bonded = true
	return d, true, nil
}

func (f *fakePairer) Identify(_ context.Context, a string, fresh bool, step func(string)) (syncnode.Identity, error) {
	f.mu.Lock()
	f.freshIDs = append(f.freshIDs, fresh)
	err, u := f.identErr, f.unit
	f.mu.Unlock()
	if err != nil {
		return syncnode.Identity{}, err
	}
	for _, s := range []string{syncnode.StepConnected, syncnode.StepLink, syncnode.StepBattery} {
		step(s)
	}
	return syncnode.Identity{Address: a,
		Record:  syncnode.WatchRecord{Unit: u, Product: 4536, Name: "fenix 8 - 47mm", Firmware: "23.40", Pair: 1, Client: 2},
		Battery: watch.Battery{Percent: 73, At: time.Now()}}, nil
}

func (f *fakePairer) Unbond(_ context.Context, a string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.bonded, a)
	f.unbonded = append(f.unbonded, a)
	return nil
}

func (f *fakePairer) Save(a string, rec syncnode.WatchRecord) error {
	return syncnode.Save(f.dir, a, rec)
}

// pairHub is a hub with a fake pairer, a fake clock and node.json in its data directory.
func pairHub(t *testing.T) (*Hub, *fakePairer, *clock.Fake, *store.Store, *fake) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(dir, "goarmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, _ := blobstore.Open(filepath.Join(dir, "raw"))
	fp := newFakePairer(dir)
	fc := clock.NewFake(time.Now())
	f := &fake{liveEnded: make(chan struct{}, 4)}
	h := New(Options{DataDir: dir, In: &ingest.Ingester{Store: st, Blobs: blobs}, Pairer: fp, Clock: fc,
		Sync: f.sync, Live: f.live, LiveLinger: time.Hour})
	t.Cleanup(h.endPairing)
	return h, fp, fc, st, f
}

func user(t *testing.T, st *store.Store, email string) store.User {
	t.Helper()
	u, err := st.EnsureUser(context.Background(), email, "")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// waitFor polls the session until cond holds.
func waitFor(t *testing.T, h *Hub, id string, uid int64, what string, cond func(PairView) bool) PairView {
	t.Helper()
	return waitForIn(t, h, id, uid, what, 3*time.Second, cond)
}

func waitForIn(t *testing.T, h *Hub, id string, uid int64, what string, d time.Duration, cond func(PairView) bool) PairView {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		v, err := h.PairingByID(id, uid)
		if err == nil && cond(v) {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s: view %+v, err %v", what, v, err)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func inState(state string) func(PairView) bool {
	return func(v PairView) bool { return v.State == state }
}

func listed(v PairView) bool { return v.State == PairFinding && len(v.Watches) == 2 }

// waitIdle waits until the session no longer holds Bluetooth.
func waitIdle(t *testing.T, h *Hub) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, busy := h.Busy(); !busy {
			return
		}
		if time.Now().After(deadline) {
			b, _ := h.Busy()
			t.Fatalf("Bluetooth still held by %+v", b)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestPairingHappyPath(t *testing.T) {
	ctx := context.Background()
	h, fp, _, st, _ := pairHub(t)
	a := user(t, st, "a@example.invalid")
	v, created, err := h.StartPairing(a.ID, a.ID)
	if err != nil || !created || v.State != PairPrepare || v.ID == "" {
		t.Fatalf("start: %+v %v %v", v, created, err)
	}
	if _, busy := h.Busy(); busy {
		t.Fatal("a session that has not scanned holds Bluetooth")
	}
	if err := h.PairScan(v.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	v = waitFor(t, h, v.ID, a.ID, "the list", listed)
	if w := v.Watches[0]; w.Address != addr || !w.SeenNow || w.Owner != OwnerNew || w.PairedHere {
		t.Fatalf("first watch %+v (strongest first)", w)
	}
	if b, busy := h.Busy(); !busy || b.Kind != "pairing" || !v.Discovering {
		t.Fatalf("busy %+v %v, discovering %v", b, busy, v.Discovering)
	}
	if err := h.PairSelect(v.ID, a.ID, "aa:bb:cc:dd:ee:01", false); err != nil {
		t.Fatal(err)
	}
	v = waitFor(t, h, v.ID, a.ID, "the code", inState(PairConfirm))
	if v.Prompt == nil || v.Prompt.Kind != "confirm" || v.Prompt.Passkey != "482917" || v.Discovering {
		t.Fatalf("prompt %+v, discovering %v", v.Prompt, v.Discovering)
	}
	if err := h.PairConfirm(v.ID, a.ID, true); err != nil {
		t.Fatal(err)
	}
	v = waitFor(t, h, v.ID, a.ID, "setup", inState(PairSetup))
	if v.Bond != BondCode {
		t.Fatalf("bond %q after a confirmed code", v.Bond)
	}
	if v.Identified == nil || v.Identified.Unit != unit || v.Identified.Battery != 73 || v.Ownership != OwnerNew || v.FormerOwner {
		t.Fatalf("identified %+v ownership %q", v.Identified, v.Ownership)
	}
	for _, s := range v.Steps {
		if !s.Done {
			t.Fatalf("step %s not done", s.Name)
		}
	}
	waitIdle(t, h) // setup holds no Bluetooth
	if ws, _ := syncnode.KnownWatches(fp.dir); len(ws) != 0 {
		t.Fatalf("saved before setup: %v", ws)
	}
	v, err = h.PairSetup(ctx, v.ID, a.ID, PairSetupRequest{Nickname: " Trail ", IncludeSynced: true})
	if err != nil || v.State != PairDone || v.DeviceID == 0 {
		t.Fatalf("setup: %+v %v", v, err)
	}
	dev, err := st.DeviceByID(ctx, v.DeviceID)
	if err != nil || dev.Nickname != "Trail" || dev.AfterFetch != store.AfterFetchArchive || dev.Battery != 73 ||
		dev.Address != addr {
		t.Fatalf("device %+v %v", dev, err)
	}
	if o, err := st.Owner(ctx, dev.ID, time.Now().Unix()); err != nil || o != a.ID {
		t.Fatalf("owner %d %v", o, err)
	}
	if ws, _ := syncnode.KnownWatches(fp.dir); ws[addr].Unit != unit || ws[addr].Pair != 1 {
		t.Fatalf("node.json %v", ws)
	}
	select {
	case req := <-h.kick:
		if req.Addr != addr || req.By != store.StartedByWizard || !req.IncludeSynced {
			t.Fatalf("first sync %+v", req)
		}
	default:
		t.Fatal("no first sync requested")
	}
	if got, ok := h.Pairing(a.ID); !ok || got.State != PairDone {
		t.Fatalf("a reload does not find the finished session: %+v %v", got, ok)
	}
	if !slices.Equal(fp.freshIDs, []bool{true}) {
		t.Fatalf("identify fresh %v", fp.freshIDs)
	}
}

func TestPairingMismatchAndTimeout(t *testing.T) {
	h, fp, fc, st, _ := pairHub(t)
	a := user(t, st, "a@example.invalid")
	v, _, _ := h.StartPairing(a.ID, a.ID)
	_ = h.PairScan(v.ID, a.ID)
	waitFor(t, h, v.ID, a.ID, "the list", listed)
	_ = h.PairSelect(v.ID, a.ID, addr, false)
	waitFor(t, h, v.ID, a.ID, "the code", inState(PairConfirm))
	if err := h.PairConfirm(v.ID, a.ID, false); err != nil {
		t.Fatal(err)
	}
	v = waitFor(t, h, v.ID, a.ID, "back to the list", func(v PairView) bool { return v.ErrorCode != "" && v.State == PairFinding })
	if v.ErrorCode != syncnode.PairMismatch || !v.Retry || len(v.Watches) != 2 || !v.Discovering {
		t.Fatalf("after a mismatch: %+v", v)
	}
	if b, busy := h.Busy(); !busy || b.Kind != "pairing" {
		t.Fatal("a retry must keep Bluetooth")
	}

	// Nobody answers: the prompt gives up after PromptTimeout.
	if err := h.PairSelect(v.ID, a.ID, addr, false); err != nil {
		t.Fatal(err)
	}
	v = waitFor(t, h, v.ID, a.ID, "the code again", inState(PairConfirm))
	if v.Error != "" {
		t.Fatalf("select keeps the old error: %q", v.Error)
	}
	fc.Advance(h.opt.PromptTimeout)
	v = waitFor(t, h, v.ID, a.ID, "the timeout", func(v PairView) bool { return v.ErrorCode != "" })
	if v.ErrorCode != syncnode.PairTimeout || v.State != PairFinding {
		t.Fatalf("after the timeout: %+v", v)
	}
	if err := h.PairConfirm(v.ID, a.ID, true); !errors.Is(err, ErrWrongState) {
		t.Fatalf("a late answer: %v", err)
	}
	if fp.bonded[addr] {
		t.Fatal("bonded although nobody confirmed")
	}
}

func TestPairingCancel(t *testing.T) {
	h, fp, _, st, _ := pairHub(t)
	a := user(t, st, "a@example.invalid")
	v, _, _ := h.StartPairing(a.ID, a.ID)
	_ = h.PairScan(v.ID, a.ID)
	waitFor(t, h, v.ID, a.ID, "the list", listed)
	_ = h.PairSelect(v.ID, a.ID, addr, false)
	waitFor(t, h, v.ID, a.ID, "the code", inState(PairConfirm))
	if err := h.CancelPairing(v.ID, a.ID, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fp.pairEnded:
	case <-time.After(3 * time.Second):
		t.Fatal("the pairing did not end")
	}
	waitIdle(t, h)
	if v, _ := h.PairingByID(v.ID, a.ID); v.State != PairCanceled || v.Prompt != nil {
		t.Fatalf("after cancel: %+v", v)
	}
	if err := h.PairScan(v.ID, a.ID); !errors.Is(err, ErrWrongState) {
		t.Fatalf("scan after cancel: %v", err)
	}
	if v2, created, err := h.StartPairing(a.ID, a.ID); err != nil || !created || v2.ID == v.ID {
		t.Fatalf("a new session after cancel: %+v %v %v", v2, created, err)
	}
}

func TestPairingAlreadyBondedAndRepair(t *testing.T) {
	h, fp, _, st, _ := pairHub(t)
	a := user(t, st, "a@example.invalid")
	fp.bonded[addr] = true
	v, _, _ := h.StartPairing(a.ID, a.ID)
	_ = h.PairScan(v.ID, a.ID)
	v = waitFor(t, h, v.ID, a.ID, "the list", listed)
	if !v.Watches[0].PairedHere {
		t.Fatalf("not flagged as paired here: %+v", v.Watches[0])
	}
	_ = h.PairSelect(v.ID, a.ID, addr, false)
	v = waitFor(t, h, v.ID, a.ID, "setup", inState(PairSetup))
	if !slices.Equal(fp.freshIDs, []bool{false}) || v.Bond != BondExisting {
		t.Fatalf("an existing bond counts as fresh: %v, bond %q", fp.freshIDs, v.Bond)
	}
	_ = h.CancelPairing(v.ID, a.ID, false)
	waitIdle(t, h)

	v, _, _ = h.StartPairing(a.ID, a.ID)
	_ = h.PairScan(v.ID, a.ID)
	waitFor(t, h, v.ID, a.ID, "the list", listed)
	_ = h.PairSelect(v.ID, a.ID, addr, true)
	waitFor(t, h, v.ID, a.ID, "the code", inState(PairConfirm))
	_ = h.PairConfirm(v.ID, a.ID, true)
	v = waitFor(t, h, v.ID, a.ID, "setup", inState(PairSetup))
	if !slices.Equal(fp.pairs, []string{addr, addr + "+repair"}) || !slices.Equal(fp.freshIDs, []bool{false, true}) || v.Bond != BondCode {
		t.Fatalf("pairs %v fresh %v bond %q", fp.pairs, fp.freshIDs, v.Bond)
	}
	// A cancelled session keeps the fresh bond: the next attempt skips the code.
	_ = h.CancelPairing(v.ID, a.ID, false)
	waitIdle(t, h)
	if !fp.bonded[addr] || len(fp.unbonded) != 0 {
		t.Fatal("cancelling removed the pairing")
	}
}

func TestPairingOwnedElsewhere(t *testing.T) {
	ctx := context.Background()
	h, fp, _, st, _ := pairHub(t)
	a, b := user(t, st, "a@example.invalid"), user(t, st, "b@example.invalid")
	dev, err := st.EnsureDevice(ctx, store.Device{UnitID: unit, Address: addr})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimDevice(ctx, dev.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	v, _, _ := h.StartPairing(a.ID, a.ID)
	_ = h.PairScan(v.ID, a.ID)
	v = waitFor(t, h, v.ID, a.ID, "the list", listed)
	if v.Watches[0].Owner != OwnerOther {
		t.Fatalf("flag %+v", v.Watches[0])
	}
	if err := h.PairSelect(v.ID, a.ID, addr, false); !errors.Is(err, ErrOwnedElsewhere) {
		t.Fatalf("select another account's watch: %v", err)
	}
	if err := h.PairSelect(v.ID, a.ID, "11:22:33:44:55:66", false); !errors.Is(err, ErrNotListed) {
		t.Fatalf("select an unlisted address: %v", err)
	}

	// The other watch is B's too, under another address: only the handshake finds out.
	other, _ := st.EnsureDevice(ctx, store.Device{UnitID: 42, Address: "11:22:33:44:55:66"})
	if err := st.ClaimDevice(ctx, other.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	fp.mu.Lock()
	fp.unit = 42
	fp.mu.Unlock()
	if err := h.PairSelect(v.ID, a.ID, "AA:BB:CC:DD:EE:02", false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, h, v.ID, a.ID, "the code", inState(PairConfirm))
	_ = h.PairConfirm(v.ID, a.ID, true)
	v = waitFor(t, h, v.ID, a.ID, "the end", inState(PairFailed))
	if v.ErrorCode != CodeOwnedElsewhere || v.Retry {
		t.Fatalf("view %+v", v)
	}
	waitIdle(t, h)
	if !slices.Equal(fp.unbonded, []string{"AA:BB:CC:DD:EE:02"}) {
		t.Fatalf("the fresh pairing stays: %v", fp.unbonded)
	}
	if ws, _ := syncnode.KnownWatches(fp.dir); len(ws) != 0 {
		t.Fatalf("saved: %v", ws)
	}
	if o, _ := st.Owner(ctx, other.ID, time.Now().Unix()); o != b.ID {
		t.Fatalf("owner changed to %d", o)
	}
}

func TestPairingFormerOwnerAndRace(t *testing.T) {
	ctx := context.Background()
	h, _, _, st, _ := pairHub(t)
	// Each ownership change a minute after the last, all in the past: periods never start in
	// the same second.
	var mu sync.Mutex
	at := time.Now().Add(-time.Hour)
	st.SetClock(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		at = at.Add(time.Minute)
		return at
	})
	a, b := user(t, st, "a@example.invalid"), user(t, st, "b@example.invalid")
	dev, _ := st.EnsureDevice(ctx, store.Device{UnitID: unit, Address: addr})
	if err := st.ClaimDevice(ctx, dev.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.ReleaseDevice(ctx, dev.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	v, _, _ := h.StartPairing(a.ID, a.ID)
	_ = h.PairScan(v.ID, a.ID)
	v = waitFor(t, h, v.ID, a.ID, "the list", listed)
	if v.Watches[0].Owner != OwnerNobody {
		t.Fatalf("a released watch: %+v", v.Watches[0])
	}
	_ = h.PairSelect(v.ID, a.ID, addr, false)
	waitFor(t, h, v.ID, a.ID, "the code", inState(PairConfirm))
	_ = h.PairConfirm(v.ID, a.ID, true)
	v = waitFor(t, h, v.ID, a.ID, "setup", inState(PairSetup))
	if v.Ownership != OwnerNobody || !v.FormerOwner {
		t.Fatalf("ownership %q former %v", v.Ownership, v.FormerOwner)
	}
	v, err := h.PairSetup(ctx, v.ID, a.ID, PairSetupRequest{GarminConnect: true, IncludeSynced: true})
	if err != nil {
		t.Fatal(err)
	}
	req := <-h.kick
	if req.IncludeSynced {
		t.Fatal("older files of the former owner were requested")
	}
	if d, _ := st.DeviceByID(ctx, dev.ID); d.AfterFetch != store.AfterFetchKeep {
		t.Fatalf("after fetch %q with Garmin Connect", d.AfterFetch)
	}
	if o, _ := st.Owner(ctx, dev.ID, time.Now().Unix()); o != a.ID {
		t.Fatalf("owner %d", o)
	}

	// Someone claims the watch between identify and setup.
	if err := st.ReleaseDevice(ctx, dev.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	v, _, _ = h.StartPairing(a.ID, a.ID)
	_ = h.PairScan(v.ID, a.ID)
	waitFor(t, h, v.ID, a.ID, "the list", listed)
	_ = h.PairSelect(v.ID, a.ID, addr, false)
	waitFor(t, h, v.ID, a.ID, "setup", inState(PairSetup)) // bonded now: no code
	if err := st.ClaimDevice(ctx, dev.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.PairSetup(ctx, v.ID, a.ID, PairSetupRequest{}); !errors.Is(err, ErrOwnedElsewhere) {
		t.Fatalf("setup of a watch claimed meanwhile: %v", err)
	}
	if v, _ := h.PairingByID(v.ID, a.ID); v.State != PairFailed {
		t.Fatalf("state %q", v.State)
	}
}

func TestPairingWaitsForBluetooth(t *testing.T) {
	ctx := context.Background()
	h, fp, _, st, f := pairHub(t)
	a := user(t, st, "a@example.invalid")
	if err := syncnode.Save(fp.dir, addr, syncnode.WatchRecord{Unit: unit}); err != nil {
		t.Fatal(err)
	}
	release, err := h.take(ctx, "sync", addr, false)
	if err != nil {
		t.Fatal(err)
	}
	v, _, _ := h.StartPairing(a.ID, a.ID)
	_ = h.PairScan(v.ID, a.ID)
	waitFor(t, h, v.ID, a.ID, "waiting", inState(PairWaiting))
	time.Sleep(20 * time.Millisecond)
	if v, _ := h.PairingByID(v.ID, a.ID); v.State != PairWaiting {
		t.Fatalf("state %q while a sync runs", v.State)
	}
	release()
	waitFor(t, h, v.ID, a.ID, "the list", listed)

	// While the session holds Bluetooth, the schedule waits and a live view is refused.
	scheduled(h, ctx)
	if len(f.syncs) != 0 {
		t.Fatal("a scheduled sync ran during pairing")
	}
	_, err = h.Live(ctx, addr)
	var busy *BusyError
	if !errors.Is(err, ErrBusy) || !errors.As(err, &busy) || busy.Holder.Kind != "pairing" {
		t.Fatalf("live during pairing: %v", err)
	}
	_ = h.CancelPairing(v.ID, a.ID, false)
	waitIdle(t, h)

	// A live link nobody watches any more (it lingers) gives way to a pairing.
	lv, err := h.Live(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	<-lv.C
	lv.Stop()
	v, _, _ = h.StartPairing(a.ID, a.ID)
	_ = h.PairScan(v.ID, a.ID)
	waitFor(t, h, v.ID, a.ID, "the list after the live link", listed)
}

func TestPairingWatchdog(t *testing.T) {
	h, _, fc, st, _ := pairHub(t)
	a := user(t, st, "a@example.invalid")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	v, _, _ := h.StartPairing(a.ID, a.ID)
	_ = h.PairScan(v.ID, a.ID)
	waitFor(t, h, v.ID, a.ID, "the list", listed)
	fc.Advance(h.opt.PairIdle - time.Second)
	if v, _ := h.PairingByID(v.ID, a.ID); v.State != PairFinding { // this look resets the idle time
		t.Fatalf("expired early: %q", v.State)
	}
	fc.Advance(2 * time.Second)
	if v, _ := h.PairingByID(v.ID, a.ID); v.Terminal() {
		t.Fatalf("a look did not count: %q", v.State)
	}
	fc.Advance(h.opt.PairIdle)
	if v, _ := h.PairingByID(v.ID, a.ID); v.State != PairExpired {
		t.Fatalf("not expired without a viewer: %q", v.State)
	}
	waitIdle(t, h)

	// With a viewer it lasts until PairMax.
	v, _, _ = h.StartPairing(a.ID, a.ID)
	events, stop, err := h.PairEvents(v.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first := <-events; first.ID != v.ID {
		t.Fatalf("first event %+v", first)
	}
	if err := fc.WaitForTimers(ctx, 2); err != nil {
		t.Fatal(err)
	}
	fc.Advance(h.opt.PairIdle * 2)
	if got := <-pairEvent(t, events); got.Terminal() {
		t.Fatalf("expired with a viewer: %+v", got)
	}
	fc.Advance(h.opt.PairMax)
	got := waitFor(t, h, v.ID, a.ID, "the end", func(v PairView) bool { return v.Terminal() })
	if got.State != PairExpired {
		t.Fatalf("state %q", got.State)
	}
	stop()
	stop() // twice is fine
}

// pairEvent nudges the session to publish its view and returns the stream.
func pairEvent(t *testing.T, ch <-chan PairView) <-chan PairView {
	t.Helper()
	out := make(chan PairView, 1)
	select {
	case v := <-ch:
		out <- v
	case <-time.After(50 * time.Millisecond):
		out <- PairView{State: PairPrepare} // nothing changed: still running
	}
	return out
}

func TestPairingSessions(t *testing.T) {
	ctx := context.Background()
	h, _, _, st, _ := pairHub(t)
	a, b := user(t, st, "a@example.invalid"), user(t, st, "b@example.invalid")
	v, _, _ := h.StartPairing(a.ID, a.ID)
	if again, created, err := h.StartPairing(a.ID, a.ID); err != nil || created || again.ID != v.ID {
		t.Fatalf("starting twice: %+v %v %v", again, created, err)
	}
	if _, _, err := h.StartPairing(b.ID, b.ID); !errors.Is(err, ErrPairingActive) {
		t.Fatalf("a second person: %v", err)
	}
	if _, err := h.PairingByID(v.ID, b.ID); !errors.Is(err, ErrNoSession) {
		t.Fatalf("someone else's session: %v", err)
	}
	if _, ok := h.Pairing(b.ID); ok {
		t.Fatal("someone else's session listed as theirs")
	}
	if err := h.PairScan(v.ID, b.ID); !errors.Is(err, ErrNoSession) {
		t.Fatalf("someone else scans: %v", err)
	}
	if err := h.CancelPairing(v.ID, b.ID, false); !errors.Is(err, ErrNoSession) {
		t.Fatalf("someone else cancels: %v", err)
	}
	if err := h.CancelPairing("", b.ID, true); err != nil {
		t.Fatalf("an admin cancels: %v", err)
	}

	// An admin adds a watch for b.
	v, _, err := h.StartPairing(a.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = h.PairScan(v.ID, a.ID)
	waitFor(t, h, v.ID, a.ID, "the list", listed)
	_ = h.PairSelect(v.ID, a.ID, addr, false)
	waitFor(t, h, v.ID, a.ID, "the code", inState(PairConfirm))
	_ = h.PairConfirm(v.ID, a.ID, true)
	waitFor(t, h, v.ID, a.ID, "setup", inState(PairSetup))
	v, err = h.PairSetup(ctx, v.ID, a.ID, PairSetupRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if o, _ := st.Owner(ctx, v.DeviceID, time.Now().Unix()); o != b.ID {
		t.Fatalf("owner %d, want b", o)
	}
	if _, err := h.PairSetup(ctx, v.ID, a.ID, PairSetupRequest{}); !errors.Is(err, ErrWrongState) {
		t.Fatalf("setup twice: %v", err)
	}
}

func TestRunEndsPairing(t *testing.T) {
	h, fp, _, st, _ := pairHub(t)
	a := user(t, st, "a@example.invalid")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.Run(ctx)
		close(done)
	}()
	fp.mu.Lock()
	fp.block = true
	fp.mu.Unlock()
	v, _, _ := h.StartPairing(a.ID, a.ID)
	_ = h.PairScan(v.ID, a.ID)
	waitFor(t, h, v.ID, a.ID, "the list", listed)
	_ = h.PairSelect(v.ID, a.ID, addr, false)
	waitFor(t, h, v.ID, a.ID, "pairing", inState(PairPairing))
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not end with a pairing running")
	}
	if v, _ := h.PairingByID(v.ID, a.ID); v.State != PairCanceled || v.ErrorCode != CodeStopping {
		t.Fatalf("after Run: %+v", v)
	}
	if _, _, err := h.StartPairing(a.ID, a.ID); !errors.Is(err, ErrStopping) {
		t.Fatalf("start after Run: %v", err)
	}
	if _, busy := h.Busy(); busy {
		t.Fatal("Bluetooth still held")
	}
}

// The scripted Bluetooth of serve -fake-pairer runs a whole session without BlueZ.
func TestScriptedPairer(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "goarmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	blobs, _ := blobstore.Open(filepath.Join(dir, "raw"))
	h := New(Scripted(Options{DataDir: dir, In: &ingest.Ingester{Store: st, Blobs: blobs}}))
	defer h.endPairing()
	a := user(t, st, "a@example.invalid")
	v, _, _ := h.StartPairing(a.ID, a.ID)
	_ = h.PairScan(v.ID, a.ID)
	v = waitForIn(t, h, v.ID, a.ID, "both watches", 5*time.Second, func(v PairView) bool { return len(v.Watches) == 2 })
	if v.Watches[0].Address != ScriptedNew || v.Watches[1].Address != ScriptedPaired || !v.Watches[1].PairedHere {
		t.Fatalf("watches %+v", v.Watches)
	}
	_ = h.PairSelect(v.ID, a.ID, ScriptedNew, false)
	v = waitForIn(t, h, v.ID, a.ID, "the code", 5*time.Second, inState(PairConfirm))
	if len(v.Prompt.Passkey) != 6 {
		t.Fatalf("passkey %q", v.Prompt.Passkey)
	}
	_ = h.PairConfirm(v.ID, a.ID, true)
	waitForIn(t, h, v.ID, a.ID, "setup", 5*time.Second, inState(PairSetup))
	v, err = h.PairSetup(ctx, v.ID, a.ID, PairSetupRequest{})
	if err != nil || v.State != PairDone {
		t.Fatalf("setup %+v %v", v, err)
	}
	if !h.Known(ScriptedNew) {
		t.Fatal("not in node.json")
	}
	if b, err := h.Bluetooth(ctx); err != nil || b.Alias != "scripted" {
		t.Fatalf("adapter %+v %v", b, err)
	}
	if _, err := h.opt.Sync(ctx, syncnode.Options{}); !errors.Is(err, gattlink.ErrNotInRange) {
		t.Fatalf("a scripted sync: %v", err)
	}
}

// A watch that asks no code (it re-pairs a computer it knows, or has no screen) is paired
// without one, and the session says so.
func TestPairingWithoutACode(t *testing.T) {
	h, fp, _, st, _ := pairHub(t)
	a := user(t, st, "a@example.invalid")
	fp.mu.Lock()
	fp.confirm = false
	fp.mu.Unlock()
	v, _, _ := h.StartPairing(a.ID, a.ID)
	_ = h.PairScan(v.ID, a.ID)
	waitFor(t, h, v.ID, a.ID, "the list", listed)
	_ = h.PairSelect(v.ID, a.ID, addr, false)
	v = waitFor(t, h, v.ID, a.ID, "setup", inState(PairSetup))
	if v.Bond != BondNoCode || !slices.Equal(fp.freshIDs, []bool{true}) {
		t.Fatalf("bond %q fresh %v", v.Bond, fp.freshIDs)
	}
}
