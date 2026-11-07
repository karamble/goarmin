// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fileaccess_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/karamble/goarmin/pkg/garmin/fakewatch"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/smart"
)

func TestDirStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	s, err := fileaccess.OpenDirStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := fileaccess.Item{UID: smart.UUID{MSB: 0x4551174800000075, LSB: 0x1000000000000ce}, TypeName: "FIT_TYPE_32", Size: 4}
	b := fileaccess.Item{UID: smart.UUID{MSB: 1, LSB: 2}, TypeName: "../evil", Size: 3}
	next := uint32(38519)
	if n, err := s.SaveListing(ctx, []fileaccess.Item{a, b, a}, &next); err != nil || n != 2 {
		t.Fatalf("SaveListing: %d, %v", n, err)
	}
	fitBytes := []byte{14, 0x10, 0, 0, 0, 0, 0, 0, '.', 'F', 'I', 'T', 0, 0}
	if err := s.Persist(ctx, a, fitBytes); err != nil {
		t.Fatal(err)
	}
	if err := s.Persist(ctx, b, []byte("xyz")); err != nil {
		t.Fatal(err)
	}

	// A new process sees the same state.
	s, err = fileaccess.OpenDirStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok, _ := s.Cursor(ctx); !ok || c != 38519 {
		t.Fatalf("cursor %d %v", c, ok)
	}
	pend, _ := s.Pending(ctx)
	if len(pend) != 2 || !pend[0].Pulled || pend[0].UID != a.UID || pend[0].TypeName != "FIT_TYPE_32" {
		t.Fatalf("pending %+v", pend)
	}
	if err := s.Finish(ctx, a.UID, fileaccess.Archived); err != nil {
		t.Fatal(err)
	}
	s, _ = fileaccess.OpenDirStore(dir)
	if n, _ := s.SaveListing(ctx, []fileaccess.Item{a}, nil); n != 0 {
		t.Fatal("a finished item became pending again")
	}
	files, err := s.Files()
	if err != nil || len(files) != 2 {
		t.Fatalf("files %+v, %v", files, err)
	}
	for _, f := range files {
		got, err := os.ReadFile(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(dir, f.Path)
		switch f.UID {
		case a.UID:
			if rel != filepath.Join("files", "FIT_TYPE_32", a.UID.String()+".fit") || f.Outcome != fileaccess.Archived || !bytes.Equal(got, fitBytes) {
				t.Errorf("a: %s %v", rel, f.Outcome)
			}
		case b.UID:
			if rel != filepath.Join("files", "unknown", b.UID.String()+".bin") || f.Outcome != 0 {
				t.Errorf("b: %s %v (the type name must not escape the store)", rel, f.Outcome)
			}
		}
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*", "*", ".tmp-*")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

// The process dies between storing a file and archiving it: a new process with a fresh
// DirStore on the same directory finishes the job without pulling the file again.
func TestDirStoreCrashRecovery(t *testing.T) {
	var drops atomic.Int32
	w := newWatch(fakewatch.Options{Faults: fakewatch.Faults{
		DropOnArchive: func(_ *fakewatch.File, attempt int) bool { return attempt == 0 && drops.Add(1) == 2 },
	}})
	for i := range 4 {
		w.AddFile("FIT_TYPE_44", payload(uint64(i), 2500))
	}
	dir := t.TempDir()
	s1, err := fileaccess.OpenDirStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileaccess.New(connect(t, w, 0, 0).conn, s1, fileaccess.Options{}).Sync(syncCtx(t)); err == nil {
		t.Fatal("first run survived the link loss")
	}
	s2, err := fileaccess.OpenDirStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := fileaccess.New(connect(t, w, 0, 0).conn, s2, fileaccess.Options{}).Sync(syncCtx(t))
	if err != nil || rep.Pulled != 2 || rep.Archived != 3 || w.Unsynced() != 0 || w.Stats().Pulls != 4 {
		t.Fatalf("second run %+v, %v; unsynced %d, pulls %d", rep, err, w.Unsynced(), w.Stats().Pulls)
	}
	files, _ := s2.Files()
	if len(files) != 4 {
		t.Fatalf("%d files stored", len(files))
	}
	for _, f := range files {
		got, _ := os.ReadFile(f.Path)
		for _, wf := range w.Files() {
			if wf.UID == f.UID && !bytes.Equal(got, wf.Data) {
				t.Errorf("%s differs", f.Path)
			}
		}
	}
}

// IncludeSynced finds files another host already archived.
func TestSyncIncludeSynced(t *testing.T) {
	w := newWatch(fakewatch.Options{})
	for i := range 3 {
		w.AddFile("FIT_TYPE_4", payload(uint64(i), 1200))
	}
	// Another host archives all three.
	other := fileaccess.NewMemStore()
	if _, err := fileaccess.New(connect(t, w, 0, 0).conn, other, fileaccess.Options{}).Sync(syncCtx(t)); err != nil {
		t.Fatal(err)
	}
	ours := fileaccess.NewMemStore()
	rep, err := fileaccess.New(connect(t, w, 0, 0).conn, ours, fileaccess.Options{NoArchive: true}).Sync(syncCtx(t))
	if err != nil || rep.Pulled != 0 {
		t.Fatalf("default listing pulled %+v, %v", rep, err)
	}
	rep, err = fileaccess.New(connect(t, w, 0, 0).conn, ours, fileaccess.Options{NoArchive: true, IncludeSynced: true}).Sync(syncCtx(t))
	if err != nil || rep.Pulled != 3 {
		t.Fatalf("listing with synced items: %+v, %v", rep, err)
	}
}
