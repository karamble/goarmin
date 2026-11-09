// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
