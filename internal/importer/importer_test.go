// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/internal/ingest"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/usbsource"
	"github.com/karamble/goarmin/pkg/blobstore"
	"github.com/karamble/goarmin/pkg/fitid/fittest"
	"github.com/karamble/goarmin/pkg/mtp"
)

const unit = 3_912_345_678

// tree is a watch's files over MTP, in memory.
type tree struct {
	mu    sync.Mutex
	files map[string][]byte
	mod   time.Time
	reads int
}

func (t *tree) sorted() []string {
	var ps []string
	for p := range t.files {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	return ps
}

func (t *tree) Walk(ctx context.Context, root string, fn func(mtp.Entry) error) error {
	t.mu.Lock()
	ps := t.sorted()
	t.mu.Unlock()
	for i, p := range ps {
		if err := fn(mtp.Entry{Handle: uint32(i + 1), Path: p, Name: filepath.Base(p), Size: int64(len(t.files[p])), Modified: t.mod}); err != nil {
			return err
		}
	}
	return nil
}

func (t *tree) Get(ctx context.Context, h uint32, w io.Writer) (int64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reads++
	n, err := w.Write(t.files[t.sorted()[h-1]])
	return int64(n), err
}

func (t *tree) Close() error { return nil }

type env struct {
	ctx     context.Context
	st      *store.Store
	r       *Runner
	a, b    store.User
	dir     string
	tree    *tree
	plugged bool
	mu      sync.Mutex
	done    chan store.Import
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "goarmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := blobstore.Open(filepath.Join(dir, "raw"))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{ctx: ctx, st: st, dir: dir, done: make(chan store.Import, 16)}
	e.a, _ = st.EnsureUser(ctx, "a@example.invalid", "")
	e.b, _ = st.EnsureUser(ctx, "b@example.invalid", "")
	now := time.Now()
	e.tree = &tree{mod: now.Add(-time.Hour).Truncate(time.Second), files: map[string][]byte{
		"GARMIN/Monitor/M1.FIT": fittest.File(typedef.FileMonitoringB, unit, now.Add(-3*time.Hour), 20),
		"GARMIN/Monitor/M2.FIT": fittest.File(typedef.FileMonitoringB, unit, now.Add(-2*time.Hour), 20),
		"GARMIN/Settings/a.txt": []byte("not FIT"),
	}}
	finder := &usbsource.Finder{
		Devices: func() ([]mtp.Info, error) {
			e.mu.Lock()
			defer e.mu.Unlock()
			if !e.plugged {
				return nil, nil
			}
			return []mtp.Info{{Path: "3-8", Vendor: usbsource.GarminVendor, Product: 0x4ff1, Manufacturer: "Garmin", ProductName: "fenix 8", Serial: "3912345678"}}, nil
		},
		Open: func(context.Context, mtp.Info) (usbsource.Tree, error) { return e.tree, nil },
	}
	e.r = New(Options{Store: st, In: &ingest.Ingester{Store: st, Blobs: blobs}, DataDir: dir, USB: finder,
		Every: 20 * time.Millisecond, Finished: func(i store.Import) { e.done <- i }})
	return e
}

func (e *env) plug(on bool) {
	e.mu.Lock()
	e.plugged = on
	e.mu.Unlock()
}

func (e *env) wait(t *testing.T) store.Import {
	t.Helper()
	select {
	case i := <-e.done:
		return i
	case <-time.After(10 * time.Second):
		t.Fatal("no import finished")
	}
	return store.Import{}
}

func report(t *testing.T, i store.Import) ingest.Report {
	t.Helper()
	var rep ingest.Report
	if err := json.Unmarshal(i.Report, &rep); err != nil {
		t.Fatal(err)
	}
	return rep
}

func zipOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}
	zw.Close()
	return b.Bytes()
}

// upload stages files as the API does and queues them.
func (e *env) upload(t *testing.T, u store.User, files map[string][]byte) store.Import {
	t.Helper()
	imp, dir, err := e.r.NewUpload(e.ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var staged []ingest.StagedFile
	for i, name := range names {
		disk := filepath.Join(dir, strings.Repeat("0", 5)+string(rune('1'+i)))
		if err := os.WriteFile(disk, files[name], 0o600); err != nil {
			t.Fatal(err)
		}
		staged = append(staged, ingest.StagedFile{Disk: disk, Name: name, Size: int64(len(files[name]))})
	}
	if err := e.r.QueueUpload(e.ctx, imp.ID, "upload", staged); err != nil {
		t.Fatal(err)
	}
	return imp
}

func TestUploadWithExport(t *testing.T) {
	e := newEnv(t)
	go e.r.Run(e.ctx)
	now := time.Now()
	fit := fittest.File(typedef.FileActivity, unit, now.Add(-24*time.Hour), 5)
	imp := e.upload(t, e.a, map[string][]byte{
		"export.zip": zipOf(t, map[string][]byte{
			"DI_CONNECT/DI-Connect-Fitness/a.json":                   []byte(`{"x":1}`),
			"DI_CONNECT/DI-Connect-Uploaded-Files/Part1.zip":         zipOf(t, map[string][]byte{"1.fit": fit}),
			"DI_CONNECT/DI-Connect-Uploaded-Files/UploadedFiles.zip": []byte("broken"),
		}),
		"loose.fit": fittest.File(typedef.FileMonitoringB, unit, now.Add(-5*time.Hour), 3),
	})
	done := e.wait(t)
	rep := report(t, done)
	if done.ID != imp.ID || done.Status != store.ImportDone || done.Source != store.SourceGCExport || rep.New != 2 || rep.Kept != 1 ||
		rep.Failed != 1 || rep.Archives != 2 || !rep.Export {
		t.Fatalf("%+v\n%+v", done, rep)
	}
	if _, err := os.Stat(e.r.staging(imp.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging left: %v", err)
	}
	// Another account cannot see or cancel it.
	if _, err := e.st.Import(e.ctx, e.b.ID, imp.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("b sees a's import: %v", err)
	}
	if err := e.r.Cancel(e.ctx, e.b, imp.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("b cancels: %v", err)
	}
	if err := e.r.Cancel(e.ctx, e.a, imp.ID); !errors.Is(err, ErrFinished) {
		t.Fatalf("cancel a finished import: %v", err)
	}
}

func TestCancelQueued(t *testing.T) {
	e := newEnv(t) // not running: the import stays queued
	imp := e.upload(t, e.a, map[string][]byte{"x.fit": fittest.File(typedef.FileMonitoringB, unit, time.Now(), 2)})
	if err := e.r.Cancel(e.ctx, e.a, imp.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := e.st.Import(e.ctx, e.a.ID, imp.ID)
	if got.Status != store.ImportCanceled {
		t.Fatalf("%+v", got)
	}
	go e.r.Run(e.ctx)
	select {
	case i := <-e.done:
		t.Fatalf("a canceled import ran: %+v", i)
	case <-time.After(100 * time.Millisecond):
	}
	up, _, err := e.r.NewUpload(e.ctx, e.a)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.r.Cancel(e.ctx, e.a, up.ID); !errors.Is(err, ErrUploading) {
		t.Fatalf("cancel while uploading: %v", err)
	}
}

func TestRecover(t *testing.T) {
	e := newEnv(t)
	queued := e.upload(t, e.a, map[string][]byte{"x.fit": fittest.File(typedef.FileMonitoringB, unit, time.Now(), 2)})
	usb, _ := e.st.CreateImport(e.ctx, store.Import{UserID: e.a.ID, Kind: store.ImportUSB, Source: store.SourceUSB, Status: store.ImportRunning})
	half, _, _ := e.r.NewUpload(e.ctx, e.a)
	// A new runner, as after a restart: the complete upload runs, the rest end.
	r2 := New(Options{Store: e.st, In: e.r.opt.In, DataDir: e.dir, Finished: func(i store.Import) { e.done <- i }})
	go r2.Run(e.ctx)
	done := e.wait(t)
	if done.ID != queued.ID || done.Status != store.ImportDone || report(t, done).New != 1 {
		t.Fatalf("%+v", done)
	}
	for _, id := range []int64{usb.ID, half.ID} {
		if got, _ := e.st.Import(e.ctx, e.a.ID, id); got.Status != store.ImportFailed || got.Error == "" {
			t.Fatalf("%+v", got)
		}
	}
	if _, err := os.Stat(e.r.staging(half.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the half upload's staging stayed: %v", err)
	}
}

func TestUSBPluggedIn(t *testing.T) {
	e := newEnv(t)
	dev, err := e.st.EnsureDevice(e.ctx, store.Device{UnitID: unit})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.ClaimDevice(e.ctx, dev.ID, e.a.ID); err != nil {
		t.Fatal(err)
	}
	e.plug(true)
	go e.r.Run(e.ctx)
	first := e.wait(t)
	rep := report(t, first)
	if first.Kind != store.ImportUSB || first.StartedBy != "plugged" || first.UserID != e.a.ID || first.DeviceID != dev.ID ||
		first.Status != store.ImportDone || rep.Seen != 2 || rep.New != 2 || rep.Other != 1 {
		t.Fatalf("%+v\n%+v", first, rep)
	}
	// The watch reports its unit number as its serial: goarmin knew it, and now remembers it.
	if d, err := e.st.DeviceByUSBSerial(e.ctx, "3912345678"); err != nil || d.ID != dev.ID {
		t.Fatalf("serial: %+v, %v", d, err)
	}
	// Staying plugged in starts nothing new.
	select {
	case i := <-e.done:
		t.Fatalf("imported again while plugged in: %+v", i)
	case <-time.After(150 * time.Millisecond):
	}
	// Unplugged and plugged in again: nothing is read again.
	reads := e.tree.reads
	e.plug(false)
	time.Sleep(80 * time.Millisecond)
	e.plug(true)
	second := e.wait(t)
	if rep := report(t, second); rep.Unchanged != 2 || rep.New != 0 || e.tree.reads != reads {
		t.Fatalf("second: %+v, reads %d → %d", rep, reads, e.tree.reads)
	}
	// A file that changed on the watch is read again.
	e.tree.mu.Lock()
	e.tree.files["GARMIN/Monitor/M2.FIT"] = fittest.File(typedef.FileMonitoringB, unit, time.Now().Add(-90*time.Minute), 25)
	e.tree.mu.Unlock()
	imp, err := e.r.ImportUSB(e.ctx, e.a, "usb-3912345678", "manual")
	if err != nil {
		t.Fatal(err)
	}
	third := e.wait(t)
	if rep := report(t, third); third.ID != imp.ID || rep.Unchanged != 1 || rep.New != 1 || third.StartedBy != "manual" {
		t.Fatalf("third: %+v %+v", third, rep)
	}

	// Turned off, plugging in imports nothing.
	if err := e.st.SetUSBAuto(e.ctx, dev.ID, false); err != nil {
		t.Fatal(err)
	}
	e.plug(false)
	time.Sleep(80 * time.Millisecond)
	e.plug(true)
	select {
	case i := <-e.done:
		t.Fatalf("imported with usb_auto off: %+v", i)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestUSBOwnership(t *testing.T) {
	e := newEnv(t)
	e.plug(true)
	// Nobody knows the watch yet: it is listed, not imported, until someone asks.
	ws, err := e.r.Watches(e.ctx)
	if err != nil || len(ws) != 1 || ws[0].Device.ID != 0 || ws[0].ID != "usb-3912345678" {
		t.Fatalf("%+v, %v", ws, err)
	}
	go e.r.Run(e.ctx)
	select {
	case i := <-e.done:
		t.Fatalf("an unknown watch was imported by itself: %+v", i)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := e.r.ImportUSB(e.ctx, e.b, "usb-3912345678", "manual"); err != nil {
		t.Fatal(err)
	}
	if done := e.wait(t); report(t, done).New != 2 {
		t.Fatalf("%+v", done)
	}
	// Now it is b's: a cannot import it, and sees it as someone else's.
	ws, _ = e.r.Watches(e.ctx)
	if len(ws) != 1 || ws[0].Owner != e.b.ID || ws[0].Device.ID == 0 {
		t.Fatalf("%+v", ws)
	}
	if _, err := e.r.ImportUSB(e.ctx, e.a, "usb-3912345678", "manual"); !errors.Is(err, ErrOtherAccount) {
		t.Fatalf("a imports b's watch: %v", err)
	}
	if _, err := e.r.ImportUSB(e.ctx, e.a, "usb-nope", "manual"); !errors.Is(err, ErrNoWatch) {
		t.Fatalf("missing watch: %v", err)
	}
}
