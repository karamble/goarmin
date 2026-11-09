// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package syncnode

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/pkg/fitid/fittest"
	"github.com/karamble/goarmin/pkg/garmin/fakewatch"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/multilink/memlink"
	"github.com/karamble/goarmin/pkg/garmin/smart"
)

const addr = "AA:BB:CC:DD:EE:01"

func TestSyncWithFakeWatch(t *testing.T) {
	now := time.Now()
	w := fakewatch.New(fakewatch.Options{})
	serial := uint32(3_500_000_001) // the fake's unit number
	for i := range 5 {
		w.AddFile("FIT_TYPE_32", fittest.File(typedef.FileMonitoringB, serial, now.Add(-time.Duration(i)*time.Hour), 40+i))
	}
	w.AddFile("FIT_TYPE_49", fittest.File(typedef.FileMonitoringDaily, serial, now, 10))
	bad := w.AddFile("FIT_TYPE_44", []byte("this is not a FIT file at all, but long enough"))

	var opened []int
	open := func(_ context.Context, pair int) (multilink.Link, io.Closer, error) {
		opened = append(opened, pair)
		if pair == 0 && len(opened) == 1 {
			return nil, nil, errors.New("characteristic busy") // first try on pair 0 fails
		}
		a, b := memlink.Pipe(memlink.Options{})
		go func() { _ = w.Serve(context.Background(), b) }()
		return a, a, nil
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	trace := &bytes.Buffer{}
	opt := Options{DataDir: dir, NoArchive: true, Trace: trace}
	res, err := SyncWith(ctx, opt, addr, open, []int{0, 1})
	if err != nil {
		t.Fatalf("sync: %v (%+v)", err, res.Report)
	}
	if res.Pair != 1 || res.Client != multilink.ClientGadgetbridge || !res.Reliable {
		t.Fatalf("pair %d client %d reliable %v", res.Pair, res.Client, res.Reliable)
	}
	if res.Battery.Percent != 80 || res.Battery.At.IsZero() {
		t.Fatalf("battery %+v (the fake watch reports 80 %% when asked)", res.Battery)
	}
	r := res.Report
	if r.Pulled != 6 || r.Failed != 1 || r.Deferred != 6 || w.Unsynced() != 7 {
		t.Fatalf("report %+v; unsynced %d", r, w.Unsynced())
	}
	if len(res.Files) != 6 {
		t.Fatalf("%d files in the spool", len(res.Files))
	}
	for _, f := range res.Files {
		if f.UID == bad.UID {
			t.Fatal("the invalid FIT file was stored")
		}
		if !strings.HasSuffix(f.Path, ".fit") {
			t.Errorf("%s: not stored as .fit", f.Path)
		}
	}
	node, err := loadNode(filepath.Join(dir, "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	rec := node.Watches[addr]
	if rec.Pair != 1 || rec.Unit != serial || rec.Product != 4536 || rec.LastSync == 0 {
		t.Fatalf("node record %+v", rec)
	}
	if !strings.Contains(trace.String(), `"layer":"gfdi"`) || !strings.Contains(trace.String(), `"layer":"ml"`) {
		t.Fatal("trace lacks packets or frames")
	}

	// Next run: the remembered pair first, nothing new, the bad file fails again; then the
	// archiving run flags the six stored files without pulling them again.
	opened = nil
	res, err = SyncWith(ctx, Options{DataDir: dir, NoArchive: true}, addr, open, []int{0, 1})
	if err != nil || len(res.Attempts) != 1 || res.Attempts[0].Pair != 1 || res.Report.Pulled != 0 || res.Report.Added != 0 {
		t.Fatalf("second run %+v / %+v, %v", res.Attempts, res.Report, err)
	}
	res, err = SyncWith(ctx, Options{DataDir: dir}, addr, open, []int{0, 1})
	if err != nil || res.Report.Archived != 6 || res.Report.Pulled != 0 || w.Unsynced() != 1 {
		t.Fatalf("archiving run %+v, %v; unsynced %d", res.Report, err, w.Unsynced())
	}
	if _, err := os.Stat(filepath.Join(res.StoreDir, "state.json")); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyFIT(t *testing.T) {
	good := fittest.File(typedef.FileMonitoringB, 1, time.Now(), 3)
	if err := VerifyFIT(fileaccess.Item{TypeName: "FIT_TYPE_32"}, good); err != nil {
		t.Fatal(err)
	}
	if err := VerifyFIT(fileaccess.Item{TypeName: "FIT_TYPE_32"}, good[:len(good)-2]); err == nil {
		t.Fatal("truncated FIT accepted")
	}
	if err := VerifyFIT(fileaccess.Item{TypeName: "GDXML_DEVICE"}, []byte("<xml/>")); err != nil {
		t.Fatalf("non-FIT item: %v", err)
	}
}

// TestSyncDeleteRequested deletes one stored activity on request during a -no-archive sync and
// refuses one the spool does not hold.
func TestSyncDeleteRequested(t *testing.T) {
	now := time.Now()
	serial := uint32(3_500_000_001)
	w := fakewatch.New(fakewatch.Options{})
	act := w.AddFile("FIT_TYPE_4", fittest.File(typedef.FileActivity, serial, now.Add(-2*time.Hour), 30))
	keep := w.AddFile("FIT_TYPE_4", fittest.File(typedef.FileActivity, serial, now.Add(-time.Hour), 20))
	open := func(_ context.Context, _ int) (multilink.Link, io.Closer, error) {
		a, b := memlink.Pipe(memlink.Options{})
		go func() { _ = w.Serve(context.Background(), b) }()
		return a, a, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	missing := act.UID
	missing.MSB++
	res, err := SyncWith(ctx, Options{DataDir: dir, NoArchive: true, Delete: []string{act.UID.String(), missing.String(), "garbage"}},
		addr, open, []int{0})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(res.Deleted) != 3 || res.Deleted[0].Err != nil {
		t.Fatalf("deletes %+v", res.Deleted)
	}
	if !errors.Is(res.Deleted[1].Err, fileaccess.ErrNotStored) || res.Deleted[2].Err == nil {
		t.Fatalf("unsafe deletes not refused: %+v", res.Deleted)
	}
	var left []string
	for _, f := range w.Files() {
		left = append(left, f.UID.String())
	}
	if len(left) != 1 || left[0] != keep.UID.String() {
		t.Fatalf("files left on the watch: %v", left)
	}
	for _, f := range res.Files {
		if f.UID == act.UID && f.Outcome != fileaccess.Deleted {
			t.Fatalf("spool record %+v", f)
		}
	}
}

// TestSyncInventory takes stock of the watch's workouts at the end of each sync: all of them,
// pulled before or not, and one deleted on the watch is missing from the next.
func TestSyncInventory(t *testing.T) {
	now := time.Now()
	serial := uint32(3_500_000_001)
	w := fakewatch.New(fakewatch.Options{})
	a := w.AddFile("FIT_TYPE_5", fittest.File(typedef.FileWorkout, serial, now.Add(-48*time.Hour), 3))
	b := w.AddFile("FIT_TYPE_5", fittest.File(typedef.FileWorkout, serial, now.Add(-24*time.Hour), 4))
	w.AddFile("FIT_TYPE_4", fittest.File(typedef.FileActivity, serial, now.Add(-time.Hour), 20))
	open := func(_ context.Context, _ int) (multilink.Link, io.Closer, error) {
		x, y := memlink.Pipe(memlink.Options{})
		go func() { _ = w.Serve(context.Background(), y) }()
		return x, x, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	stock := func() []string {
		t.Helper()
		res, err := SyncWith(ctx, Options{DataDir: dir, NoArchive: true, Inventory: InventoryTypes}, addr, open, []int{0})
		if err != nil {
			t.Fatalf("sync: %v", err)
		}
		if res.Inventory == nil || !slices.Equal(res.Inventory.Types, InventoryTypes) || res.Inventory.At.IsZero() {
			t.Fatalf("inventory %+v", res.Inventory)
		}
		var got []string
		for _, it := range res.Inventory.Items {
			got = append(got, it.UID.String())
		}
		slices.Sort(got)
		return got
	}
	want := []string{a.UID.String(), b.UID.String()}
	slices.Sort(want)
	if got := stock(); !slices.Equal(got, want) {
		t.Fatalf("first inventory %v, want %v", got, want)
	}
	w.RemoveFile(a.UID)
	if got := stock(); !slices.Equal(got, []string{b.UID.String()}) {
		t.Fatalf("after a removal %v", got)
	}
	res, err := SyncWith(ctx, Options{DataDir: dir, NoArchive: true}, addr, open, []int{0})
	if err != nil || res.Inventory != nil {
		t.Fatalf("without Inventory: %+v, %v", res.Inventory, err)
	}
}

// TestSyncSend pushes files after the pulls and before the inventory, which lists the new
// workout; a refused push does not stop the rest, and the workout comes back as the watch's.
func TestSyncSend(t *testing.T) {
	now := time.Now()
	serial := uint32(3_500_000_002)
	refuse := "FIT_TYPE_39"
	w := fakewatch.New(fakewatch.Options{Faults: fakewatch.Faults{PushResult: func(typeName string, _ int) (smart.ItemAccessResult, bool) {
		return smart.ItemAccessItemTypeNotSupported, typeName == refuse
	}}})
	w.AddFile("FIT_TYPE_5", fittest.File(typedef.FileWorkout, serial, now.Add(-48*time.Hour), 3))
	open := func(_ context.Context, _ int) (multilink.Link, io.Closer, error) {
		x, y := memlink.Pipe(memlink.Options{})
		go func() { _ = w.Serve(context.Background(), y) }()
		return x, x, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	workout := fittest.File(typedef.FileWorkout, serial, now.Add(-time.Hour), 5)
	res, err := SyncWith(ctx, Options{DataDir: t.TempDir(), NoArchive: true, Inventory: InventoryTypes, Send: []Outgoing{
		{Key: "odd", Type: refuse, Data: []byte("not a FIT file at all")},
		{Key: "library:x", Type: "FIT_TYPE_5", Data: workout, Announce: true},
	}}, addr, open, []int{0})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(res.Sent) != 2 || !res.Sent[0].Refused() || !res.Sent[0].Unsupported() || res.Sent[1].Err != nil || res.Sent[1].UID == "" ||
		res.Sent[1].Key != "library:x" || len(res.Sent[1].SHA256) != 64 {
		t.Fatalf("sent %+v", res.Sent)
	}
	if res.Inventory == nil || len(res.Inventory.Items) != 2 || !slices.ContainsFunc(res.Inventory.Items, func(it fileaccess.Item) bool {
		return it.UID.String() == res.Sent[1].UID
	}) {
		t.Fatalf("the inventory after the push: %+v", res.Inventory)
	}
}

// TestSyncHand: with Hand (a sync node), the watch flags only what the server took; the rest
// stays in the spool and on the watch until a later sync hands it over, without pulling it again.
func TestSyncHand(t *testing.T) {
	now := time.Now()
	serial := uint32(3_500_000_001)
	w := fakewatch.New(fakewatch.Options{})
	for i := range 4 {
		w.AddFile("FIT_TYPE_32", fittest.File(typedef.FileMonitoringB, serial, now.Add(-time.Duration(i)*time.Hour), 40+i))
	}
	open := func(_ context.Context, _ int) (multilink.Link, io.Closer, error) {
		a, b := memlink.Pipe(memlink.Options{})
		go func() { _ = w.Serve(context.Background(), b) }()
		return a, a, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()

	server := map[string][]byte{} // what the server took, by UID
	down := 2                     // the server refuses the first two files
	hand := func(_ context.Context, f fileaccess.StoredFile) error {
		if down > 0 {
			down--
			return errors.New("connection refused")
		}
		b, err := os.ReadFile(f.Path)
		if err != nil {
			return err
		}
		server[f.UID.String()] = b
		return nil
	}
	res, err := SyncWith(ctx, Options{DataDir: dir, Hand: hand}, addr, open, []int{0})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if r := res.Report; r.Pulled != 4 || r.Archived != 2 || r.Deferred != 2 || w.Unsynced() != 2 || len(server) != 2 {
		t.Fatalf("first run %+v; unsynced %d; server has %d", r, w.Unsynced(), len(server))
	}
	// Handed files left the spool; the others wait there.
	inSpool := 0
	for _, f := range res.Files {
		if _, err := os.Stat(f.Path); err == nil {
			inSpool++
			if server[f.UID.String()] != nil {
				t.Errorf("%s: handed over but still in the spool", f.UID)
			}
		}
	}
	if inSpool != 2 {
		t.Fatalf("%d files in the spool, want the 2 the server refused", inSpool)
	}

	// The server is back: the next sync hands the two over and flags them, pulling nothing.
	res, err = SyncWith(ctx, Options{DataDir: dir, Hand: hand}, addr, open, []int{0})
	if err != nil || res.Report.Pulled != 0 || res.Report.Archived != 2 || w.Unsynced() != 0 || len(server) != 4 {
		t.Fatalf("second run %+v, %v; unsynced %d; server has %d", res.Report, err, w.Unsynced(), len(server))
	}
	if got := w.Stats().Pulls; got != 4 {
		t.Fatalf("the watch served %d pulls, want 4 (none twice)", got)
	}
}
