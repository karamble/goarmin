// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fileaccess_test

import (
	"cmp"
	"context"
	"slices"
	"testing"

	"github.com/karamble/goarmin/pkg/garmin/fakewatch"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/smart"
)

func uids(items []fileaccess.Item) []smart.UUID {
	var out []smart.UUID
	for _, it := range items {
		out = append(out, it.UID)
	}
	slices.SortFunc(out, func(a, b smart.UUID) int { return cmp.Or(cmp.Compare(a.MSB, b.MSB), cmp.Compare(a.LSB, b.LSB)) })
	return out
}

// An inventory lists every workout the watch keeps, synced or not, across pages, and leaves the
// store, its cursor and the watch's flags alone. A workout deleted on the watch is missing from
// the next one.
func TestInventory(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		w := newWatch(fakewatch.Options{PageSize: 3, Faults: fakewatch.Faults{RefuseTypedListings: refuse}})
		var want []fileaccess.Item
		for i := range 5 {
			f := w.AddFile("FIT_TYPE_5", payload(uint64(i), 300))
			want = append(want, fileaccess.Item{UID: f.UID})
		}
		for i := range 4 {
			w.AddFile("FIT_TYPE_32", payload(uint64(10+i), 900))
		}
		store := fileaccess.NewMemStore()
		s := connect(t, w, 0, 0)
		eng := fileaccess.New(s.conn, store, fileaccess.Options{})
		if _, err := eng.Sync(syncCtx(t)); err != nil {
			t.Fatal(err)
		}
		synced := w.Unsynced()
		cur, haveCur, _ := store.Cursor(context.Background())

		got, err := eng.Inventory(syncCtx(t), []string{"FIT_TYPE_5"})
		if err != nil {
			t.Fatalf("refuse=%v: %v", refuse, err)
		}
		if !slices.Equal(uids(got), uids(want)) {
			t.Fatalf("refuse=%v: inventory %v, want %v", refuse, uids(got), uids(want))
		}
		for _, it := range got {
			if it.TypeName != "FIT_TYPE_5" || it.Size != 300 {
				t.Fatalf("refuse=%v: item %+v", refuse, it)
			}
		}
		if cur2, have2, _ := store.Cursor(context.Background()); cur2 != cur || have2 != haveCur || w.Unsynced() != synced {
			t.Fatalf("refuse=%v: the inventory changed the cursor (%d→%d) or flags", refuse, cur, cur2)
		}
		if n := w.Stats().TypedListings; n != 1 {
			t.Fatalf("refuse=%v: %d listings by type, want 1", refuse, n)
		}

		w.RemoveFile(want[2].UID)
		got, err = eng.Inventory(syncCtx(t), []string{"FIT_TYPE_5"})
		if err != nil || len(got) != 4 || slices.Contains(uids(got), want[2].UID) {
			t.Fatalf("refuse=%v: after a removal %v, %v", refuse, uids(got), err)
		}
	}
}

func TestInventoryNeedsTypes(t *testing.T) {
	s := connect(t, newWatch(fakewatch.Options{}), 0, 0)
	if _, err := fileaccess.New(s.conn, fileaccess.NewMemStore(), fileaccess.Options{}).Inventory(syncCtx(t), nil); err == nil {
		t.Fatal("an inventory without types did not fail")
	}
}
