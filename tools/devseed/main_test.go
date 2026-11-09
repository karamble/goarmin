// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/karamble/goarmin/internal/config"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

func TestSeed(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "goarmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	u, err := st.EnsureUser(ctx, "tester@example.invalid", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	days, err := seed(ctx, st, u.ID, 10, now, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	from, to := days[0], days[len(days)-1]
	if to != now.Format(time.DateOnly) || len(days) < 10 {
		t.Fatalf("days %v", days)
	}
	sums, err := st.DaySummaries(ctx, u.ID, from, to)
	if err != nil {
		t.Fatal(err)
	}
	full := 0
	for _, s := range sums {
		if s.Steps != nil && s.HRMin != nil && s.HRMax != nil && s.StressAvg != nil && s.BBMax != nil && s.RespirationAvg != nil {
			full++
		}
	}
	if full < 9 { // the evening before the first day, and today, may be partial
		t.Fatalf("%d of %d days have every summary value", full, len(sums))
	}
	nights, err := st.SleepSessions(ctx, u.ID, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(nights) != 10 || nights[0].Deep == nil || nights[0].Score == nil {
		t.Fatalf("%d nights, first %+v", len(nights), nights[0])
	}
	hrv, err := st.HRVNights(ctx, u.ID, from, to)
	if err != nil || len(hrv) < 9 {
		t.Fatalf("%d HRV nights, %v", len(hrv), err)
	}
	pts, err := st.Series(ctx, u.ID, wellness.SleepStage, nights[0].Start, nights[0].End+1)
	if err != nil || len(pts) < 10 {
		t.Fatalf("%d stage changes, %v", len(pts), err)
	}
	acts, err := st.ActivitiesIn(ctx, u.ID, from, to)
	if err != nil || len(acts) < 2 {
		t.Fatalf("%d activities, %v", len(acts), err)
	}
	laps, err := st.Laps(ctx, u.ID, acts[0].ID)
	if err != nil || len(laps) < 2 {
		t.Fatalf("%d laps, %v", len(laps), err)
	}
	if _, data, err := st.Stream(ctx, u.ID, acts[0].ID); err != nil || len(data) == 0 {
		t.Fatalf("no track: %v", err)
	}
	vals, err := st.DailyValues(ctx, u.ID, from, to)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[wellness.Kind]int{}
	for _, v := range vals {
		kinds[v.Kind]++
	}
	for _, k := range []wellness.Kind{wellness.AcuteLoad, wellness.ChronicLoad, wellness.TrainingStatus, wellness.Race5K, wellness.EnduranceScore} {
		if kinds[k] < 9 {
			t.Fatalf("%v on %d days", k, kinds[k])
		}
	}
	if z, err := st.ActivityZones(ctx, u.ID, acts[0].ID); err != nil || z[-1] == nil {
		t.Fatalf("no zones on the activity: %v", err)
	}
	if ws, err := st.LatestWatchSettings(ctx, u.ID); err != nil || ws.Settings.Clock != "24h" || len(ws.Settings.Bikes) != 1 {
		t.Fatalf("settings %+v, %v", ws, err)
	}
	wk, _ := st.Workouts(ctx, u.ID, 0)
	cs, _ := st.Courses(ctx, u.ID, 0)
	tot, _ := st.LifetimeTotals(ctx, u.ID)
	if len(wk) != 6 || len(cs) != 1 || len(tot) != 2 || wk[0].EstimatedS == 0 {
		t.Fatalf("%d workouts, %d courses, %d totals", len(wk), len(cs), len(tot))
	}
	// The library: Core 20 from the watch and Garmin Connect is one workout; on the made-up
	// watch two are on it, one was removed there, two never were.
	lib, err := st.WorkoutLibrary(ctx, u.ID)
	if err != nil || len(lib.Workouts) != 5 || len(lib.Watches) != 1 || lib.Watches[0].InventoryAt.IsZero() {
		t.Fatalf("library %d workouts, %d watches, %v", len(lib.Workouts), len(lib.Watches), err)
	}
	states := map[string]int{}
	for _, w := range lib.Workouts {
		states[w.Watches[0].State]++
	}
	if states[store.PresenceOn] != 2 || states[store.PresenceRemoved] != 1 || states[store.PresenceAbsent] != 2 {
		t.Fatalf("where the workouts are: %v", states)
	}
	if plans, err := st.Plans(ctx, u.ID, now.AddDate(0, 0, -10).Format(time.DateOnly), now.AddDate(0, 0, 10).Format(time.DateOnly)); err != nil || len(plans) != 6 {
		t.Fatalf("%d plans, %v", len(plans), err)
	}
	ahead, err := st.SuggestedWorkouts(ctx, u.ID, to, now.AddDate(0, 0, 6).Format(time.DateOnly))
	if err != nil || len(ahead) < 6 {
		t.Fatalf("%d planned workouts this week, %v", len(ahead), err)
	}

	future, err := st.Series(ctx, u.ID, wellness.HeartRate, now.Unix()+1, now.Add(48*time.Hour).Unix())
	if err != nil || len(future) != 0 {
		t.Fatalf("%d samples after now, %v", len(future), err)
	}

	// Seeding again changes nothing (one record per day, replaced in place).
	if _, err := seed(ctx, st, u.ID, 10, now, time.Local); err != nil {
		t.Fatal(err)
	}
	again, _ := st.SleepSessions(ctx, u.ID, from, to)
	if len(again) != 10 {
		t.Fatalf("%d nights after seeding twice", len(again))
	}
}

func TestRefusesTheRealData(t *testing.T) {
	if err := checkTarget(config.DataDir()); err == nil {
		t.Fatal("the real data directory was accepted")
	}
	if err := checkTarget(config.DataDir() + "/"); err == nil {
		t.Fatal("the real data directory with a slash was accepted")
	}
	if err := checkTarget(""); err == nil {
		t.Fatal("an empty directory was accepted")
	}
	if err := checkTarget(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
