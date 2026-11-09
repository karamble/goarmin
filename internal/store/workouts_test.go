// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/karamble/goarmin/pkg/fitwatch"
	"github.com/karamble/goarmin/pkg/garmin/gcexport"
)

// watchWorkout stores a workout file a sync pulled from device as "<uid>.fit" (device 0: a file
// from before files were linked to their device) and writes its workout.
func watchWorkout(t *testing.T, st *Store, user, device int64, uid, name, sport string, created time.Time) RawFile {
	t.Helper()
	sum := sha256.Sum256([]byte(fmt.Sprint(uid, name)))
	f, _, err := st.AddRawFile(context.Background(), RawFile{SHA256: hex.EncodeToString(sum[:]), Size: 1, UserID: user, DeviceID: device,
		Source: SourceBLE, SourceName: uid + ".fit", FIT: &FITIdentity{Type: WorkoutFITType, Manufacturer: 1, Serial: 7, TimeCreated: created.Unix()}})
	if err != nil {
		t.Fatal(err)
	}
	w := &fitwatch.File{Workout: &fitwatch.Workout{Name: name, Sport: sport, SubSport: "generic",
		Steps: []fitwatch.Step{{Index: 0, Duration: "time", DurationValue: 300, Intensity: "active"}}, EstimatedS: 300}}
	if err := st.WriteWatchFile(context.Background(), f, w); err != nil {
		t.Fatal(err)
	}
	return f
}

func item(uid string) InventoryItem { return InventoryItem{UID: uid, Type: "FIT_TYPE_5", Size: 400} }

func states(t *testing.T, lib Library, name string) (LibraryWorkout, []string) {
	t.Helper()
	for _, w := range lib.Workouts {
		if w.Name == name {
			var out []string
			for _, p := range w.Watches {
				out = append(out, p.State)
			}
			return w, out
		}
	}
	t.Fatalf("no workout %q in the library", name)
	return LibraryWorkout{}, nil
}

func TestWorkoutLibrary(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	now := time.Now().Truncate(time.Second)
	u, other := person(t, st, "a@example.invalid"), person(t, st, "b@example.invalid")
	dev, err := st.EnsureDevice(ctx, Device{UnitID: 11, Name: "fenix 8 - 47mm", Address: "AA:BB:CC:DD:EE:11"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AssignIfUnowned(ctx, dev.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	const (
		uidPlank = "4319872000000001-0100000000000335"
		uidSpine = "4319872000000002-010000000000041c"
		uidOld   = "4319872000000003-0100000000000500"
		uidAgain = "4319872000000004-0100000000000335"
	)
	plank := watchWorkout(t, st, u.ID, dev.ID, uidPlank, "Plank Challenge", "training", now.AddDate(0, 0, -20))
	spine := watchWorkout(t, st, u.ID, dev.ID, uidSpine, "Articulate Your Spine", "fitness_equipment", now.AddDate(0, 0, -10))
	watchWorkout(t, st, u.ID, 0, uidOld, "Coherence", "training", now.AddDate(0, 0, -30)) // never linked to its device
	for _, f := range []struct {
		uid string
		id  int64
	}{{uidPlank, plank.ID}, {uidSpine, spine.ID}} {
		if err := st.LinkWatchItem(ctx, dev.ID, f.uid, "FIT_TYPE_5", f.id); err != nil {
			t.Fatal(err)
		}
	}
	// Garmin Connect's list holds the plank workout too (spelled differently) and one more.
	jf, _, err := st.AddRawFile(ctx, RawFile{SHA256: strings.Repeat("cd", 32), Size: 1, UserID: u.ID, Source: SourceGCExport,
		SourceName: "x.zip/DI_CONNECT/DI-Connect-Fitness/a_workout.json"})
	if err != nil {
		t.Fatal(err)
	}
	gc := func(name, sport string, days int) gcexport.Workout {
		return gcexport.Workout{Workout: fitwatch.Workout{Name: name, Sport: sport, SubSport: "generic",
			Steps: []fitwatch.Step{{Index: 0, Duration: "reps", DurationValue: 10}}, EstimatedS: 600},
			Description: "From the library", Updated: now.AddDate(0, 0, days)}
	}
	exp := &gcexport.Result{Kind: "workouts", Workouts: []gcexport.Workout{gc("plank  challenge", "training", -5), gc("Alignment 101", "training", -40)}}
	if _, err := st.WriteExportFile(ctx, jf, exp, "gcjson", 2); err != nil {
		t.Fatal(err)
	}

	lib, err := st.WorkoutLibrary(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Watches) != 1 || lib.Watches[0].ID != dev.ID || !lib.Watches[0].InventoryAt.IsZero() || len(lib.Workouts) != 4 {
		t.Fatalf("watches %+v, %d workouts", lib.Watches, len(lib.Workouts))
	}
	w, s := states(t, lib, "Plank Challenge")
	if w.Origin != OriginWatch || w.ID != plank.ID || len(w.Versions) != 2 || w.Versions[1].Origin != OriginGarminConnect ||
		!slices.Equal(w.Origins, []string{OriginWatch, OriginGarminConnect}) || !slices.Equal(s, []string{PresenceOn}) ||
		w.Description != "From the library" {
		t.Fatalf("plank %+v, states %v", w, s)
	}
	if _, s := states(t, lib, "Coherence"); !slices.Equal(s, []string{PresenceUnknown}) {
		t.Fatalf("an unlinked file before any inventory: %v", s)
	}
	a, s := states(t, lib, "Alignment 101")
	if a.Origin != OriginGarminConnect || a.Description != "From the library" || a.DeviceID != 0 || !slices.Equal(s, []string{PresenceUnknown}) {
		t.Fatalf("Connect's own %+v, %v", a, s)
	}

	// The watch takes stock: plank and the old file are on it, the spine workout is gone.
	at := now.Add(-time.Hour)
	if err := st.SetWatchInventory(ctx, dev.ID, WatchInventory{Types: []string{"FIT_TYPE_5"}, At: at,
		Items: []InventoryItem{item(uidPlank), item(uidOld)}}); err != nil {
		t.Fatal(err)
	}
	lib, _ = st.WorkoutLibrary(ctx, u.ID)
	if !lib.Watches[0].InventoryAt.Equal(at) {
		t.Fatalf("inventory at %v", lib.Watches[0].InventoryAt)
	}
	for name, want := range map[string]string{"Plank Challenge": PresenceOn, "Coherence": PresenceOn,
		"Articulate Your Spine": PresenceRemoved, "Alignment 101": PresenceAbsent} {
		w, s := states(t, lib, name)
		if !slices.Equal(s, []string{want}) {
			t.Fatalf("%s: %v, want %s", name, s, want)
		}
		if want == PresenceOn && !w.Watches[0].At.Equal(at) || want == PresenceRemoved && !w.Watches[0].At.Equal(at) {
			t.Fatalf("%s since %v", name, w.Watches[0].At)
		}
	}

	// The plank workout is sent to the watch again: a new item, the same bytes. The old item
	// goes; the workout stays on the watch.
	if err := st.LinkWatchItem(ctx, dev.ID, uidAgain, "FIT_TYPE_5", plank.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWatchInventory(ctx, dev.ID, WatchInventory{Types: []string{"FIT_TYPE_5"}, At: now,
		Items: []InventoryItem{item(uidAgain), item(uidOld)}}); err != nil {
		t.Fatal(err)
	}
	lib, _ = st.WorkoutLibrary(ctx, u.ID)
	if _, s := states(t, lib, "Plank Challenge"); !slices.Equal(s, []string{PresenceOn}) {
		t.Fatalf("sent again: %v", s)
	}
	// The spine workout back on the watch under its old item.
	if err := st.SetWatchInventory(ctx, dev.ID, WatchInventory{Types: []string{"FIT_TYPE_5"}, At: now.Add(time.Minute),
		Items: []InventoryItem{item(uidAgain), item(uidOld), item(uidSpine)}}); err != nil {
		t.Fatal(err)
	}
	lib, _ = st.WorkoutLibrary(ctx, u.ID)
	if _, s := states(t, lib, "Articulate Your Spine"); !slices.Equal(s, []string{PresenceOn}) {
		t.Fatalf("back on the watch: %v", s)
	}

	// By id: any version finds the workout.
	got, ws, err := st.LibraryWorkoutByID(ctx, u.ID, w.Versions[1].ID)
	if err != nil || got.ID != plank.ID || len(ws) != 1 {
		t.Fatalf("by the Connect version's id: %+v, %v", got, err)
	}
	// Another account sees none of it, nor the watch.
	if lib, err := st.WorkoutLibrary(ctx, other.ID); err != nil || len(lib.Workouts) != 0 || len(lib.Watches) != 0 {
		t.Fatalf("another account: %+v, %v", lib, err)
	}
	if _, _, err := st.LibraryWorkoutByID(ctx, other.ID, plank.ID); err != ErrNotFound {
		t.Fatalf("another account by id: %v", err)
	}

	// Deleting the watch's data leaves its items known but without files.
	if _, err := st.DeleteDeviceData(ctx, u.ID, dev.ID); err != nil {
		t.Fatal(err)
	}
	lib, _ = st.WorkoutLibrary(ctx, u.ID)
	for _, w := range lib.Workouts {
		if w.Origin == OriginWatch && w.DeviceID == dev.ID {
			t.Fatalf("a deleted watch's workout is left: %+v", w)
		}
	}
	var linked int
	if err := st.db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM watch_items WHERE device_id = ? AND raw_file_id IS NOT NULL
		AND raw_file_id NOT IN (SELECT id FROM raw_files)`, dev.ID).Scan(&linked); err != nil || linked != 0 {
		t.Fatalf("%d items linked to deleted files, %v", linked, err)
	}
}

// TestWorkoutRename renames a watch's workout in goarmin: one entry, shown under the new name,
// also once the watch hands back the renamed file goarmin sent it.
func TestWorkoutRename(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	now := time.Now().Truncate(time.Second)
	u, other := person(t, st, "a@example.invalid"), person(t, st, "b@example.invalid")
	dev, err := st.EnsureDevice(ctx, Device{UnitID: 12, Name: "fenix 8 - 47mm", Address: "AA:BB:CC:DD:EE:12"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AssignIfUnowned(ctx, dev.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	const uidCopy, uidPlank, uidBack = "4319872000000011-0100000000000335", "4319872000000012-0100000000000336", "4319872000000013-0100000000000337"
	cp := watchWorkout(t, st, u.ID, dev.ID, uidCopy, "Copy of Hone & Tone", "training", now.AddDate(0, 0, -3))
	plank := watchWorkout(t, st, u.ID, dev.ID, uidPlank, "Plank", "training", now.AddDate(0, 0, -2))
	for uid, id := range map[string]int64{uidCopy: cp.ID, uidPlank: plank.ID} {
		if err := st.LinkWatchItem(ctx, dev.ID, uid, "FIT_TYPE_5", id); err != nil {
			t.Fatal(err)
		}
	}
	// Deleted on the watch.
	if err := st.SetWatchInventory(ctx, dev.ID, WatchInventory{Types: []string{"FIT_TYPE_5"}, At: now.Add(-time.Hour),
		Items: []InventoryItem{item(uidPlank)}}); err != nil {
		t.Fatal(err)
	}
	lib, err := st.WorkoutLibrary(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := states(t, lib, "Copy of Hone & Tone")
	w := orig.Workout
	w.Name = "Hone & Tone"
	id, err := st.SaveWorkoutVersion(ctx, u.ID, orig.EntryKey, w, "Toning")
	if err != nil {
		t.Fatal(err)
	}
	lib, _ = st.WorkoutLibrary(ctx, u.ID)
	if lib.Workouts[0].Name != "Hone & Tone" || lib.Workouts[1].Name != "Plank" {
		t.Fatalf("not by the shown name: %q, %q", lib.Workouts[0].Name, lib.Workouts[1].Name)
	}
	got, s := states(t, lib, "Hone & Tone")
	if len(lib.Workouts) != 2 || got.ID != id || got.Origin != OriginGoarmin || got.Description != "Toning" || len(got.Versions) != 2 ||
		!slices.Equal(got.Origins, []string{OriginGoarmin, OriginWatch}) || !slices.Equal(s, []string{PresenceRemoved}) {
		t.Fatalf("renamed %+v, states %v (of %d)", got, s, len(lib.Workouts))
	}
	// Renamed again: the same version.
	w.Name = "Hone and Tone"
	if again, err := st.SaveWorkoutVersion(ctx, u.ID, orig.EntryKey, w, "Toning"); err != nil || again != id {
		t.Fatalf("renamed again: %d, %v", again, err)
	}
	// Another workout's name is taken, case and spacing aside.
	w.Name = " plank "
	if _, err := st.SaveWorkoutVersion(ctx, u.ID, orig.EntryKey, w, ""); !errors.Is(err, ErrWorkoutNameTaken) {
		t.Fatalf("a taken name: %v", err)
	}
	w.Name = "Hone & Tone"
	if _, err := st.SaveWorkoutVersion(ctx, u.ID, orig.EntryKey, w, "Toning"); err != nil {
		t.Fatal(err)
	}

	// goarmin sends it; the watch lists it and hands the file back under the new name.
	back := watchWorkout(t, st, u.ID, dev.ID, uidBack, "Hone & Tone", "training", now)
	if err := st.LinkWatchItem(ctx, dev.ID, uidBack, "FIT_TYPE_5", back.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.SetWatchInventory(ctx, dev.ID, WatchInventory{Types: []string{"FIT_TYPE_5"}, At: now,
		Items: []InventoryItem{item(uidPlank), item(uidBack)}}); err != nil {
		t.Fatal(err)
	}
	lib, _ = st.WorkoutLibrary(ctx, u.ID)
	got, s = states(t, lib, "Hone & Tone")
	if len(lib.Workouts) != 2 || got.ID != id || len(got.Versions) != 3 || !slices.Equal(s, []string{PresenceOn}) {
		t.Fatalf("handed back: %+v, states %v (of %d)", got, s, len(lib.Workouts))
	}

	// Only goarmin's own version can be removed, and only by its account.
	if err := st.DeleteWorkoutVersion(ctx, u.ID, cp.ID); err != ErrNotFound {
		t.Fatalf("removing a watch's version: %v", err)
	}
	if err := st.DeleteWorkoutVersion(ctx, other.ID, id); err != ErrNotFound {
		t.Fatalf("another account: %v", err)
	}
	if err := st.DeleteWorkoutVersion(ctx, u.ID, id); err != nil {
		t.Fatal(err)
	}
	lib, _ = st.WorkoutLibrary(ctx, u.ID)
	got, s = states(t, lib, "Hone & Tone") // the watch's newer file, still one entry
	if len(lib.Workouts) != 2 || got.Origin != OriginWatch || len(got.Versions) != 2 || !slices.Equal(s, []string{PresenceOn}) {
		t.Fatalf("the edit removed: %+v, states %v (of %d)", got, s, len(lib.Workouts))
	}
}

// TestWorkoutOutbox switches workouts on and off for a watch and follows what the next sync
// sends: what the watch lacks, not what it refused or deleted since.
func TestWorkoutOutbox(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	now := time.Now().Truncate(time.Second)
	clock := now
	st.SetClock(func() time.Time { return clock })
	u, other := person(t, st, "a@example.invalid"), person(t, st, "b@example.invalid")
	dev, err := st.EnsureDevice(ctx, Device{UnitID: 13, Name: "fenix 8 - 47mm", Address: "AA:BB:CC:DD:EE:13"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AssignIfUnowned(ctx, dev.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	const uidA, uidB, uidSent = "4319872000000021-0100000000000001", "4319872000000022-0100000000000002", "4319872000000023-0100000000000003"
	a := watchWorkout(t, st, u.ID, dev.ID, uidA, "Alpha", "training", now.AddDate(0, 0, -3))
	b := watchWorkout(t, st, u.ID, dev.ID, uidB, "Bravo", "training", now.AddDate(0, 0, -3))
	for uid, id := range map[string]int64{uidA: a.ID, uidB: b.ID} {
		if err := st.LinkWatchItem(ctx, dev.ID, uid, "FIT_TYPE_5", id); err != nil {
			t.Fatal(err)
		}
	}
	jf, _, err := st.AddRawFile(ctx, RawFile{SHA256: strings.Repeat("ef", 32), Size: 1, UserID: u.ID, Source: SourceGCExport,
		SourceName: "x.zip/DI_CONNECT/DI-Connect-Fitness/a_workout.json"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteExportFile(ctx, jf, &gcexport.Result{Kind: "workouts", Workouts: []gcexport.Workout{{Workout: fitwatch.Workout{
		Name: "Charlie", Sport: "training", SubSport: "generic", Steps: []fitwatch.Step{{Index: 0, Duration: "reps", DurationValue: 10}}},
		Updated: now.AddDate(0, 0, -5)}}}, "gcjson", 2); err != nil {
		t.Fatal(err)
	}
	stock := func(at time.Time, uids ...string) {
		t.Helper()
		var items []InventoryItem
		for _, uid := range uids {
			items = append(items, item(uid))
		}
		if err := st.SetWatchInventory(ctx, dev.ID, WatchInventory{Types: []string{"FIT_TYPE_5"}, At: at, Items: items}); err != nil {
			t.Fatal(err)
		}
	}
	stock(now.Add(-2*time.Hour), uidA, uidB)
	stock(now.Add(-time.Hour), uidA) // Bravo deleted on the watch
	type want struct {
		state, pending string
		on             bool
	}
	check := func(step string, expect map[string]want) {
		t.Helper()
		lib, err := st.WorkoutLibrary(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		for name, e := range expect {
			w, _ := states(t, lib, name)
			p := w.Watches[0]
			if p.State != e.state || p.Pending != e.pending || p.Want != e.on {
				t.Errorf("%s, %s: state %s, pending %q, want %v; expected %+v (failed %q)", step, name, p.State, p.Pending, p.Want, e, p.Failed)
			}
		}
		out, err := st.WatchOutbox(ctx, u.ID, dev.ID)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, o := range out {
			names = append(names, o.Version.Name)
		}
		var sends []string
		for name, e := range expect {
			if e.pending == PendingSend {
				sends = append(sends, name)
			}
		}
		slices.Sort(sends)
		if !slices.Equal(names, sends) {
			t.Errorf("%s: outbox %v, want %v", step, names, sends)
		}
	}
	check("never switched", map[string]want{"Alpha": {PresenceOn, "", true}, "Bravo": {PresenceRemoved, "", false}, "Charlie": {PresenceAbsent, "", false}})

	clock = now.Add(time.Minute)
	key := func(name string) string { return WorkoutKey(name, "training") }
	for name, on := range map[string]bool{"Alpha": false, "Bravo": true, "Charlie": true} {
		if err := st.SetWorkoutTarget(ctx, u.ID, dev.ID, key(name), on); err != nil {
			t.Fatal(err)
		}
	}
	check("switched", map[string]want{"Alpha": {PresenceOn, PendingRemove, false}, "Bravo": {PresenceRemoved, PendingSend, true},
		"Charlie": {PresenceAbsent, PendingSend, true}})
	out, _ := st.WatchOutbox(ctx, u.ID, dev.ID)
	if out[1].Version.Origin != OriginGarminConnect || out[0].Version.RawFileID != b.ID || out[1].EntryKey != key("Charlie") {
		t.Fatalf("outbox versions %+v", out)
	}
	if out, err := st.WatchOutbox(ctx, other.ID, dev.ID); err != nil || out != nil {
		t.Fatalf("another account's outbox for the watch: %v, %v", out, err)
	}

	// The sync: Charlie goes on, Bravo is refused.
	clock = now.Add(2 * time.Minute)
	if err := st.RecordSends(ctx, u.ID, dev.ID, []SendRecord{
		{EntryKey: key("Charlie"), Reason: SendReasonLibrary, SHA256: "aa", UID: uidSent},
		{EntryKey: key("Bravo"), Reason: SendReasonLibrary, SHA256: "bb", Err: "the watch refused", Refused: true},
	}, clock); err != nil {
		t.Fatal(err)
	}
	check("sent", map[string]want{"Bravo": {PresenceRemoved, "", true}, "Charlie": {PresenceOn, "", true}})
	stock(now.Add(3*time.Minute), uidA, uidSent)
	check("listed", map[string]want{"Alpha": {PresenceOn, PendingRemove, false}, "Charlie": {PresenceOn, "", true}})
	if d, _ := st.DeviceByID(ctx, dev.ID); d.SendSupport != "yes" {
		t.Fatalf("send support %q", d.SendSupport)
	}

	// Deleted on the watch: no longer wanted, not sent again, until switched on anew.
	stock(now.Add(4*time.Minute), uidA)
	check("deleted on the watch", map[string]want{"Charlie": {PresenceRemoved, "", false}})
	clock = now.Add(5 * time.Minute)
	for _, name := range []string{"Bravo", "Charlie"} {
		if err := st.SetWorkoutTarget(ctx, u.ID, dev.ID, key(name), true); err != nil {
			t.Fatal(err)
		}
	}
	check("switched on again", map[string]want{"Bravo": {PresenceRemoved, PendingSend, true}, "Charlie": {PresenceRemoved, PendingSend, true}})

	// A watch that takes no pushes gets none.
	if err := st.RecordSends(ctx, u.ID, dev.ID, []SendRecord{{EntryKey: key("Bravo"), Reason: SendReasonLibrary, SHA256: "bb",
		Err: "not supported", Refused: true, Unsupported: true}}, now.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if d, _ := st.DeviceByID(ctx, dev.ID); d.SendSupport != "no" {
		t.Fatalf("send support after a refusal as unsupported: %q", d.SendSupport)
	}
	check("no pushes", map[string]want{"Bravo": {PresenceRemoved, "", true}, "Charlie": {PresenceRemoved, "", true}})
}
