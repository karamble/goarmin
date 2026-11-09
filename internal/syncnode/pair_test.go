// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package syncnode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/garmin/fakewatch"
	"github.com/karamble/goarmin/pkg/garmin/gattlink"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/multilink/memlink"
	"github.com/karamble/goarmin/pkg/garmin/watch"
)

// fakeBluez is a pairClient that records what pairing asked of BlueZ.
type fakeBluez struct {
	adapter  bluez.Adapter
	dev      bluez.Device
	pairErr  error
	bondless bool // Pair succeeds without a bond
	block    bool // Pair waits for ctx

	calls      []string
	agentOpt   *bluez.AgentOptions
	registered int
}

func (f *fakeBluez) Adapter(context.Context, string) (bluez.Adapter, error) { return f.adapter, nil }
func (f *fakeBluez) SetPairable(_ context.Context, _ string, on bool) error {
	f.calls = append(f.calls, "pairable")
	f.adapter.Pairable = on
	return nil
}
func (f *fakeBluez) Device(_ context.Context, _, addr string) (bluez.Device, error) {
	if f.dev.Address != addr {
		return bluez.Device{}, fmt.Errorf("%w: %s", bluez.ErrUnknownDevice, addr)
	}
	return f.dev, nil
}
func (f *fakeBluez) RemoveDevice(context.Context, string, bluez.Device) error {
	f.calls = append(f.calls, "remove")
	f.dev.Paired, f.dev.Bonded, f.dev.Trusted = false, false, false
	return nil
}
func (f *fakeBluez) ScanLE(_ context.Context, _ string, _ time.Duration, keep func(bluez.Device) bool,
	_ func([]bluez.Device) bool) ([]bluez.Device, error) {
	f.calls = append(f.calls, "scan")
	if keep(f.dev) {
		return []bluez.Device{f.dev}, nil
	}
	return nil, nil
}
func (f *fakeBluez) RegisterAgent(_ context.Context, _ bluez.Device, _ bluez.Prompter, opt bluez.AgentOptions) (*bluez.Agent, func(), error) {
	f.calls = append(f.calls, "agent")
	f.agentOpt = &opt
	f.registered++
	return nil, func() { f.registered--; f.calls = append(f.calls, "unregister") }, nil
}
func (f *fakeBluez) PairBonded(ctx context.Context, _ string, d bluez.Device) (bluez.Device, error) {
	f.calls = append(f.calls, "pair")
	if f.block {
		<-ctx.Done()
		return d, ctx.Err()
	}
	if f.pairErr != nil {
		return d, f.pairErr
	}
	f.dev.Paired = true
	if f.bondless {
		return f.dev, bluez.ErrNotBonded
	}
	f.dev.Bonded = true
	return f.dev, nil
}
func (f *fakeBluez) CancelPairing(context.Context, bluez.Device) error {
	f.calls = append(f.calls, "cancel")
	return nil
}
func (f *fakeBluez) SetTrusted(_ context.Context, _ bluez.Device, on bool) error {
	f.calls = append(f.calls, "trust")
	f.dev.Trusted = on
	return nil
}

func newFakeBluez() *fakeBluez {
	return &fakeBluez{adapter: bluez.Adapter{Name: "hci0", Powered: true, Pairable: true},
		dev: bluez.Device{Address: addr, Name: "fenix 8 - 47mm"}}
}

func TestPairWith(t *testing.T) {
	ctx := context.Background()
	t.Run("fresh", func(t *testing.T) {
		f := newFakeBluez()
		f.adapter.Pairable = false
		d, fresh, err := pairWith(ctx, f, PairOptions{Adapter: "hci0", Device: f.dev})
		if err != nil || !fresh || !d.Bonded || !d.Trusted {
			t.Fatalf("pair: %+v fresh %v err %v", d, fresh, err)
		}
		if want := []string{"pairable", "agent", "pair", "trust", "unregister"}; !slices.Equal(f.calls, want) {
			t.Fatalf("calls %v, want %v", f.calls, want)
		}
		if f.agentOpt == nil || f.agentOpt.Default {
			t.Fatal("the agent must not become the default agent")
		}
	})
	t.Run("already bonded", func(t *testing.T) {
		f := newFakeBluez()
		f.dev.Paired, f.dev.Bonded = true, true
		_, fresh, err := pairWith(ctx, f, PairOptions{Adapter: "hci0", Device: f.dev})
		if err != nil || fresh || !slices.Equal(f.calls, []string{"trust"}) {
			t.Fatalf("fresh %v err %v calls %v", fresh, err, f.calls)
		}
	})
	t.Run("repair", func(t *testing.T) {
		f := newFakeBluez()
		f.dev.Paired, f.dev.Bonded, f.dev.Trusted = true, true, true
		_, fresh, err := pairWith(ctx, f, PairOptions{Adapter: "hci0", Device: f.dev, Repair: true})
		if err != nil || !fresh {
			t.Fatalf("fresh %v err %v", fresh, err)
		}
		if want := []string{"remove", "scan", "agent", "pair", "trust", "unregister"}; !slices.Equal(f.calls, want) {
			t.Fatalf("calls %v, want %v", f.calls, want)
		}
	})
	t.Run("no bond", func(t *testing.T) {
		f := newFakeBluez()
		f.bondless = true
		_, _, err := pairWith(ctx, f, PairOptions{Adapter: "hci0", Device: f.dev})
		if PairCode(err) != PairNotBonded || f.registered != 0 {
			t.Fatalf("err %v (%s), agents left %d", err, PairCode(err), f.registered)
		}
	})
	t.Run("rejected", func(t *testing.T) {
		f := newFakeBluez()
		f.pairErr = fmt.Errorf("bluez: pair: %w", dbus.Error{Name: "org.bluez.Error.AuthenticationRejected"})
		_, _, err := pairWith(ctx, f, PairOptions{Adapter: "hci0", Device: f.dev})
		if PairCode(err) != PairAuthFailed || f.registered != 0 || slices.Contains(f.calls, "trust") {
			t.Fatalf("err %v, agents %d, calls %v", err, f.registered, f.calls)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		f := newFakeBluez()
		f.block = true
		cctx, cancel := context.WithCancel(ctx)
		go func() { time.Sleep(20 * time.Millisecond); cancel() }()
		_, _, err := pairWith(cctx, f, PairOptions{Adapter: "hci0", Device: f.dev})
		if !errors.Is(err, context.Canceled) || !slices.Contains(f.calls, "cancel") || f.registered != 0 {
			t.Fatalf("err %v calls %v agents %d", err, f.calls, f.registered)
		}
	})
	t.Run("adapter off", func(t *testing.T) {
		f := newFakeBluez()
		f.adapter.Powered = false
		_, _, err := pairWith(ctx, f, PairOptions{Adapter: "hci0", Device: f.dev})
		if PairCode(err) != PairAdapterOff || len(f.calls) != 0 {
			t.Fatalf("err %v calls %v", err, f.calls)
		}
	})
	t.Run("gone", func(t *testing.T) {
		f := newFakeBluez()
		_, _, err := pairWith(ctx, f, PairOptions{Adapter: "hci0", Device: bluez.Device{Address: "AA:BB:CC:DD:EE:99"}})
		if PairCode(err) != PairGone {
			t.Fatalf("err %v (%s)", err, PairCode(err))
		}
	})
}

func TestPairCode(t *testing.T) {
	be := func(name string) error {
		return fmt.Errorf("bluez: pair: %w", dbus.Error{Name: "org.bluez.Error." + name})
	}
	for _, c := range []struct {
		err  error
		want string
	}{
		{nil, ""},
		{be("AuthenticationFailed"), PairAuthFailed},
		{be("AuthenticationCanceled"), PairAuthFailed},
		{be("AuthenticationTimeout"), PairTimeout},
		{be("ConnectionAttemptFailed"), PairConnectFailed},
		{be("Failed"), PairConnectFailed},
		{be("InProgress"), PairInProgress},
		{be("DoesNotExist"), PairGone},
		{be("NotReady"), PairAdapterOff},
		{fmt.Errorf("x: %w", bluez.ErrNotBonded), PairNotBonded},
		{fmt.Errorf("%w (hci0)", gattlink.ErrAdapterOff), PairAdapterOff},
		{context.DeadlineExceeded, PairTimeout},
		{errors.New("odd"), PairFailed},
	} {
		if got := PairCode(c.err); got != c.want {
			t.Errorf("PairCode(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}

func TestIsGarmin(t *testing.T) {
	for _, c := range []struct {
		d    bluez.Device
		want bool
	}{
		{bluez.Device{Name: "fenix 8 - 47mm"}, true},
		{bluez.Device{Name: "Forerunner 965"}, true},
		{bluez.Device{Name: "phone", ManufacturerData: map[uint16][]byte{garminCompanyID: {1}}}, true},
		{bluez.Device{Name: "headphones"}, false},
	} {
		if got := IsGarmin(c.d); got != c.want {
			t.Errorf("IsGarmin(%q) = %v", c.d.Name, got)
		}
	}
}

func fakeOpener(w *fakewatch.Watch) watch.Opener {
	return func(_ context.Context, _ int) (multilink.Link, io.Closer, error) {
		a, b := memlink.Pipe(memlink.Options{})
		go func() { _ = w.Serve(context.Background(), b) }()
		return a, a, nil
	}
}

// TakeStock lists the watch's workouts and touches nothing: no pulls, no flags, no node.json.
func TestTakeStockWith(t *testing.T) {
	w := fakewatch.New(fakewatch.Options{})
	a := w.AddFile("FIT_TYPE_5", []byte("a workout"))
	w.AddFile("FIT_TYPE_32", []byte("monitoring"))
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id, items, err := takeStockWith(ctx, IdentifyOptions{DataDir: dir, Address: addr}, InventoryTypes, fakeOpener(w), []int{0})
	if err != nil {
		t.Fatal(err)
	}
	if id.Record.Unit != 3_500_000_001 || len(items) != 1 || items[0].UID != a.UID || items[0].TypeName != "FIT_TYPE_5" {
		t.Fatalf("record %+v, items %+v", id.Record, items)
	}
	if st := w.Stats(); st.Pulls != 0 || st.Archives != 0 || st.TypedListings != 1 || w.Unsynced() != 2 {
		t.Fatalf("taking stock touched files: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(dir, "node.json")); !os.IsNotExist(err) {
		t.Fatalf("taking stock wrote node.json (%v)", err)
	}
}

// Identify learns the watch from its handshake and battery report and saves nothing.
func TestIdentifyWith(t *testing.T) {
	w := fakewatch.New(fakewatch.Options{Battery: 64})
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var steps []string
	id, err := identifyWith(ctx, IdentifyOptions{DataDir: dir, Address: addr, Step: func(s string) { steps = append(steps, s) }},
		fakeOpener(w), []int{0})
	if err != nil {
		t.Fatal(err)
	}
	if id.Record.Unit != 3_500_000_001 || id.Record.Product != 4536 || id.Record.Name != "fenix 8 - 47mm" ||
		id.Record.Firmware == "" || id.Record.Client != multilink.ClientGadgetbridge {
		t.Fatalf("record %+v", id.Record)
	}
	if id.Battery.Percent != 64 || id.Battery.At.IsZero() {
		t.Fatalf("battery %+v", id.Battery)
	}
	if !slices.Equal(steps, []string{StepLink, StepBattery}) {
		t.Fatalf("steps %v", steps)
	}
	if _, err := os.Stat(filepath.Join(dir, "node.json")); !os.IsNotExist(err) {
		t.Fatalf("identify wrote node.json (%v)", err)
	}
	if st := w.Stats(); st.Listings != 0 || st.Pulls != 0 {
		t.Fatalf("identify touched files: %+v", st)
	}
	if ev := w.Stats().SystemEvents; slices.Contains(ev, byte(watch.EventPairComplete)) {
		t.Fatalf("first-session events without FirstSession: %v", ev)
	}

	w2 := fakewatch.New(fakewatch.Options{})
	if _, err := identifyWith(ctx, IdentifyOptions{DataDir: dir, Address: addr, FirstSession: true}, fakeOpener(w2), []int{0}); err != nil {
		t.Fatal(err)
	}
	ev := w2.Stats().SystemEvents
	for _, want := range []watch.SystemEvent{watch.EventPairComplete, watch.EventSyncComplete, watch.EventSetupWizardComplete} {
		if !slices.Contains(ev, byte(want)) {
			t.Fatalf("FirstSession sent %v, missing %d", ev, want)
		}
	}
}

// Save keeps the time of the last sync, and a sync that ends keeps a watch saved meanwhile.
func TestSaveKeepsOthers(t *testing.T) {
	dir := t.TempDir()
	synced := time.Now().Add(-time.Hour).Unix()
	if err := updateNode(dir, func(n *nodeFile) bool {
		n.Watches[addr] = WatchRecord{Unit: 1, LastSync: synced}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, addr, WatchRecord{Unit: 1, Firmware: "new"}); err != nil {
		t.Fatal(err)
	}
	const other = "AA:BB:CC:DD:EE:02"
	w := fakewatch.New(fakewatch.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	open := func(c context.Context, n int) (multilink.Link, io.Closer, error) {
		// The wizard adds another watch while this one syncs.
		if err := Save(dir, other, WatchRecord{Unit: 2}); err != nil {
			t.Error(err)
		}
		return fakeOpener(w)(c, n)
	}
	if _, err := SyncWith(ctx, Options{DataDir: dir, NoArchive: true}, addr, open, []int{0}); err != nil {
		t.Fatal(err)
	}
	ws, err := KnownWatches(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ws[other]; !ok {
		t.Fatalf("the sync dropped the watch saved meanwhile: %v", ws)
	}
	if ws[addr].LastSync <= synced {
		t.Fatalf("last sync not updated: %+v", ws[addr])
	}
	if err := ForgetRecord(dir, addr); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, addr, WatchRecord{Unit: 1}); err != nil {
		t.Fatal(err)
	}
	if ws, _ := KnownWatches(dir); ws[addr].LastSync != 0 {
		t.Fatalf("a forgotten watch came back with its old sync time: %+v", ws[addr])
	}
}

// Options.Features reaches the watch and its answer comes back in the result.
func TestSyncAnnouncesFeatures(t *testing.T) {
	w := fakewatch.New(fakewatch.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := SyncWith(ctx, Options{DataDir: t.TempDir(), NoArchive: true, Features: true}, addr, fakeOpener(w), []int{0})
	if err != nil {
		t.Fatal(err)
	}
	if w.Stats().FeatureRequests != 1 || res.Features == nil || !res.Features.WellnessSupport {
		t.Fatalf("%d requests, features %+v", w.Stats().FeatureRequests, res.Features)
	}
}
