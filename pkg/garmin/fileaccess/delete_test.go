// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fileaccess_test

import (
	"errors"
	"os"
	"testing"

	"github.com/karamble/goarmin/pkg/garmin/fakewatch"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/smart"
)

// fenix8Removal models the Fenix 8: it removes archived health files at once and keeps
// archived activities (docs/protocol/06, M2).
func fenix8Removal(f *fakewatch.File) bool { return f.TypeName != "FIT_TYPE_4" }

func onWatch(w *fakewatch.Watch, uid smart.UUID) bool {
	for _, f := range w.Files() {
		if f.UID == uid {
			return true
		}
	}
	return false
}

// TestSyncDeleteAfterArchive deletes activities after archiving them, copes with a watch that
// refuses one and with health files the watch has already removed on its own.
func TestSyncDeleteAfterArchive(t *testing.T) {
	var refuse smart.UUID
	w := newWatch(fakewatch.Options{
		RemoveOnArchive: fenix8Removal,
		Faults: fakewatch.Faults{DeleteResult: func(f *fakewatch.File, _ int) (smart.DeleteItemResult, bool) {
			return smart.DeleteItemOperationNotSupported, f.UID == refuse
		}},
	})
	health := w.AddFile("FIT_TYPE_32", payload(1, 900))
	act1 := w.AddFile("FIT_TYPE_4", payload(2, 3000))
	act2 := w.AddFile("FIT_TYPE_4", payload(3, 2500))
	sport := w.AddFile("FIT_TYPE_3", payload(4, 400))
	refuse = act2.UID

	store := fileaccess.NewMemStore()
	var deleted []smart.UUID
	rep, err := fileaccess.New(connect(t, w, 0, 0).conn, store, fileaccess.Options{
		// Ask for health files too: the watch has removed them by then.
		Delete: func(it fileaccess.Item) bool { return it.TypeName == "FIT_TYPE_4" || it.TypeName == "FIT_TYPE_32" },
		OnEvent: func(ev fileaccess.Event) {
			if ev.Kind == fileaccess.EventDeleted {
				deleted = append(deleted, ev.Item.UID)
			}
		},
	}).Sync(syncCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Pulled != 4 || rep.Archived != 3 || rep.Kept != 1 || rep.Deleted != 1 || rep.Refused != 1 || rep.Failed != 0 {
		t.Fatalf("report %+v", rep)
	}
	if len(deleted) != 1 || deleted[0] != act1.UID {
		t.Fatalf("deleted events %v, want only %s", deleted, act1.UID)
	}
	for uid, want := range map[smart.UUID]fileaccess.Outcome{
		health.UID: fileaccess.Archived, act1.UID: fileaccess.Deleted, act2.UID: fileaccess.Archived, sport.UID: fileaccess.Kept,
	} {
		if got, _ := store.Outcome(uid); got != want {
			t.Errorf("%s: outcome %v, want %v", uid, got, want)
		}
		if !store.Stored(uid) {
			t.Errorf("%s: file not stored", uid)
		}
	}
	for uid, want := range map[smart.UUID]bool{health.UID: false, act1.UID: false, act2.UID: true, sport.UID: true} {
		if onWatch(w, uid) != want {
			t.Errorf("%s on the watch: %v, want %v", uid, !want, want)
		}
	}
	if st := w.Stats(); st.Deletes != 1 {
		t.Errorf("watch deleted %d items, want 1", st.Deletes)
	}
}

// TestSyncDeleteNeedsArchive: with NoArchive, nothing is deleted.
func TestSyncDeleteNeedsArchive(t *testing.T) {
	w := newWatch(fakewatch.Options{})
	act := w.AddFile("FIT_TYPE_4", payload(5, 2000))
	rep, err := fileaccess.New(connect(t, w, 0, 0).conn, fileaccess.NewMemStore(), fileaccess.Options{
		NoArchive: true, Delete: func(fileaccess.Item) bool { return true },
	}).Sync(syncCtx(t))
	if err != nil || rep.Deferred != 1 || rep.Deleted != 0 {
		t.Fatalf("%+v, %v", rep, err)
	}
	if !onWatch(w, act.UID) || w.Stats().Deletes != 0 {
		t.Fatal("NoArchive deleted a file")
	}
}

// TestEngineDelete deletes one stored item on request, outside a sync, with a DirStore.
func TestEngineDelete(t *testing.T) {
	w := newWatch(fakewatch.Options{})
	act := w.AddFile("FIT_TYPE_4", payload(6, 2200))
	other := w.AddFile("FIT_TYPE_4", payload(7, 1800))
	store, err := fileaccess.OpenDirStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	eng := fileaccess.New(connect(t, w, 0, 0).conn, store, fileaccess.Options{NoArchive: true})
	if _, err := eng.Sync(syncCtx(t)); err != nil {
		t.Fatal(err)
	}

	unknown := smart.UUID{MSB: act.UID.MSB + 1000, LSB: act.UID.LSB}
	if err := eng.Delete(syncCtx(t), unknown, store.Stored); !errors.Is(err, fileaccess.ErrNotStored) {
		t.Fatalf("deleting an item that is not stored: %v", err)
	}
	if err := eng.Delete(syncCtx(t), act.UID, store.Stored); err != nil {
		t.Fatal(err)
	}
	if onWatch(w, act.UID) || !onWatch(w, other.UID) {
		t.Fatal("the wrong file left the watch")
	}
	files, err := store.Files()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f.UID != act.UID {
			continue
		}
		found = true
		if f.Outcome != fileaccess.Deleted || f.TypeName != "FIT_TYPE_4" {
			t.Errorf("stored record %+v", f)
		}
		if _, err := os.Stat(f.Path); err != nil {
			t.Errorf("the spool lost the file: %v", err)
		}
	}
	if !found {
		t.Fatal("the deleted item is missing from the store")
	}
	// The record survives a reopen, and a second delete of the same item is harmless.
	again, err := fileaccess.OpenDirStore(store.Dir())
	if err != nil || !again.Stored(act.UID) {
		t.Fatalf("reopened store: %v", err)
	}
	if err := eng.Delete(syncCtx(t), act.UID, store.Stored); err != nil {
		t.Fatalf("deleting it again: %v", err)
	}
}
