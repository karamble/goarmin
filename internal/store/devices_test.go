// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), "goarmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func person(t *testing.T, st *Store, email string) User {
	t.Helper()
	u, err := st.EnsureUser(context.Background(), email, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetPassword(context.Background(), u.ID, "hash"); err != nil {
		t.Fatal(err)
	}
	return u
}

// handOver releases device at t (the store's clock) and lets b claim it.
func handOver(t *testing.T, st *Store, device, a, b int64, at int64) {
	t.Helper()
	st.SetClock(func() time.Time { return time.Unix(at, 0) })
	defer st.SetClock(time.Now)
	if err := st.ReleaseDevice(context.Background(), device, a); err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimDevice(context.Background(), device, b); err != nil {
		t.Fatal(err)
	}
}

func TestFormerOwnersOnlyRead(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	now := time.Now()
	a, b := person(t, st, "a@example.invalid"), person(t, st, "b@example.invalid")
	dev, err := st.EnsureDevice(ctx, Device{UnitID: 7, Name: "fenix 8 - 47mm", Address: "aa:bb:cc:dd:ee:07"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AssignIfUnowned(ctx, dev.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	run := func(at time.Time, ok bool) {
		if err := st.RecordSyncRun(ctx, SyncRun{DeviceID: dev.ID, Started: at, Finished: at.Add(time.Minute), OK: ok}); err != nil {
			t.Fatal(err)
		}
	}
	run(now.Add(-3*time.Hour), true)
	handOver(t, st, dev.ID, a.ID, b.ID, now.Add(-2*time.Hour).Unix())
	run(now.Add(-time.Hour), false)

	// A had it: reads it, with the period, but may not act on it any more.
	ad, err := st.UserDevice(ctx, a.ID, dev.ID)
	if err != nil || ad.Current || ad.OwnedFrom != 0 || ad.OwnedTo != now.Add(-2*time.Hour).Unix() {
		t.Fatalf("a's view %+v, %v", ad, err)
	}
	if _, err := st.OwnedDevice(ctx, a.ID, dev.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a acts on a handed-over watch: %v", err)
	}
	bd, err := st.OwnedDevice(ctx, b.ID, dev.ID)
	if err != nil || !bd.Current || bd.OwnedTo != 0 {
		t.Fatalf("b's view %+v, %v", bd, err)
	}
	if list, _ := st.UserDevices(ctx, a.ID); len(list) != 1 || list[0].Current {
		t.Fatalf("a's list %+v", list)
	}

	// Each sees only the syncs of their own time with the watch.
	ra, _ := st.SyncRuns(ctx, a.ID, dev.ID, 10)
	rb, _ := st.SyncRuns(ctx, b.ID, dev.ID, 10)
	if len(ra) != 1 || !ra[0].OK || len(rb) != 1 || rb[0].OK || ra[0].ID == 0 {
		t.Fatalf("a's runs %+v, b's runs %+v", ra, rb)
	}
	if all, _ := st.RecentSyncRuns(ctx, dev.ID, 10); len(all) != 2 || all[0].OK {
		t.Fatalf("all runs %+v", all)
	}
}

func TestSoleUser(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	if _, err := st.SoleUser(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nobody: %v", err)
	}
	if _, err := st.EnsureUser(ctx, "imported@example.invalid", ""); err != nil { // cannot sign in
		t.Fatal(err)
	}
	a := person(t, st, "a@example.invalid")
	if u, err := st.SoleUser(ctx); err != nil || u.ID != a.ID {
		t.Fatalf("one person: %+v, %v", u, err)
	}
	person(t, st, "b@example.invalid")
	if _, err := st.SoleUser(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("two people: %v", err)
	}
}

func TestDuplicatesStayWithinAnAccount(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	a, b := person(t, st, "a@example.invalid"), person(t, st, "b@example.invalid")
	id := &FITIdentity{Type: 4, Serial: 9, TimeCreated: time.Now().Add(-time.Hour).Unix(), Number: 1}
	fa, _, err := st.AddRawFile(ctx, RawFile{SHA256: "aa", Size: 1, UserID: a.ID, Source: SourceUSB, FIT: id})
	if err != nil {
		t.Fatal(err)
	}
	// The same recording in another account is its own file there, and a's keeps its data.
	fb, _, err := st.AddRawFile(ctx, RawFile{SHA256: "bb", Size: 1, UserID: b.ID, Source: SourceBLE, FIT: id})
	if err != nil || fb.DuplicateOf != 0 {
		t.Fatalf("b's copy %+v, %v", fb, err)
	}
	if fa2, _ := st.RawFileBySHA(ctx, "aa"); fa2.DuplicateOf != 0 {
		t.Fatalf("a's file became b's duplicate: %+v", fa2)
	}
	// Within one account a second copy is still a duplicate of the first.
	fc, _, err := st.AddRawFile(ctx, RawFile{SHA256: "cc", Size: 1, UserID: a.ID, Source: SourceBLE, FIT: id})
	if err != nil || fc.DuplicateOf != fa.ID {
		t.Fatalf("a's second copy %+v, %v", fc, err)
	}
}

func TestClaimAndRelease(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	a, b := person(t, st, "a@example.invalid"), person(t, st, "b@example.invalid")
	dev, _ := st.EnsureDevice(ctx, Device{UnitID: 8})
	if err := st.ClaimDevice(ctx, dev.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimDevice(ctx, dev.ID, b.ID); !errors.Is(err, ErrOwned) {
		t.Fatalf("claiming a's watch: %v", err)
	}
	if err := st.SetAfterFetch(ctx, dev.ID, AfterFetchArchive); err != nil {
		t.Fatal(err)
	}
	if err := st.ReleaseDevice(ctx, dev.ID, b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("b released a's watch: %v", err)
	}
	if err := st.ReleaseDevice(ctx, dev.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	// Released: nobody's, and AssignIfUnowned does not hand it to anyone.
	if _, err := st.AssignIfUnowned(ctx, dev.ID, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a released watch was reassigned: %v", err)
	}
	if err := st.ClaimDevice(ctx, dev.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	d, _ := st.OwnedDevice(ctx, b.ID, dev.ID)
	if !d.Current || d.AfterFetch != AfterFetchKeep {
		t.Fatalf("b's claimed watch %+v (settings start over)", d)
	}
	// The gap between release and claim belongs to the claimant: b's period starts at a's end.
	ad, _ := st.UserDevice(ctx, a.ID, dev.ID)
	if d.OwnedFrom != ad.OwnedTo {
		t.Fatalf("b from %d, a to %d", d.OwnedFrom, ad.OwnedTo)
	}
}
