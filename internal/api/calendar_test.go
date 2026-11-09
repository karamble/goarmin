// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/fitid/fittest"
	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

// calendarActivity is a short activity file of sport starting at start, recorded with the
// workout named wkt ("" for none).
func calendarActivity(sport typedef.Sport, start time.Time, wkt string) []byte {
	var msgs []proto.Message
	if wkt != "" {
		w := mesgdef.NewWorkout(nil)
		w.WktName, w.Sport = wkt, sport
		msgs = append(msgs, w.ToMesg(nil))
	}
	for k := range 20 {
		r := mesgdef.NewRecord(nil)
		r.Timestamp, r.HeartRate = start.Add(time.Duration(k)*time.Second), 130
		msgs = append(msgs, r.ToMesg(nil))
	}
	s := mesgdef.NewSession(nil)
	s.StartTime, s.Timestamp, s.Sport, s.TotalTimerTime, s.TotalElapsedTime = start, start.Add(20*time.Second), sport, 20000, 20000
	return fittest.Build(typedef.FileActivity, 3_500_000_001, start.Add(time.Minute), append(msgs, s.ToMesg(nil))...)
}

func shiftDay(d string, n int) string {
	t, _ := time.Parse(time.DateOnly, d)
	return t.AddDate(0, 0, n).Format(time.DateOnly)
}

// TestCalendar: a day holds its kinds of data, activities, Garmin Coach's workouts and the
// user's plans; a plan is done by the workout's name, else by its sport, and missed once its
// day is past.
func TestCalendar(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	ctx := context.Background()
	u, err := e.st.UserByEmail(ctx, "admin@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	z, err := e.st.Zone(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	today := z.Day(now.Unix())
	runAt := now.Add(-50 * time.Hour) // a run that followed Tempo
	walkAt := now.Add(-98 * time.Hour)
	dRun, dWalk := z.Day(runAt.Unix()), z.Day(walkAt.Unix())

	w := mesgdef.NewWorkout(nil)
	w.WktName, w.Sport = "Tempo", typedef.SportRunning
	st := mesgdef.NewWorkoutStep(nil)
	st.MessageIndex, st.DurationType, st.DurationValue, st.Intensity = 0, typedef.WktStepDurationTime, 1200000, typedef.IntensityActive
	files := map[string][]byte{
		"W.fit": fittest.Build(typedef.FileWorkout, 3_500_000_001, now.AddDate(0, 0, -10), w.ToMesg(nil), st.ToMesg(nil)),
		"R.fit": calendarActivity(typedef.SportRunning, runAt, "Tempo"),
		"K.fit": calendarActivity(typedef.SportWalking, walkAt, ""),
	}
	dir := t.TempDir()
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if rep, err := e.in.Import(ctx, u, store.SourceUSB, []string{dir}); err != nil || rep.Ingested != 3 {
		t.Fatalf("import: %+v, %v", rep, err)
	}
	// Garmin Coach: a run on the walk's day and two days before; a weigh-in on the walk's day.
	f, _, err := e.st.AddRawFile(ctx, store.RawFile{SHA256: strings.Repeat("e1", 32), Size: 1, UserID: u.ID, Source: store.SourceUSB, SourceName: "coach"})
	if err != nil {
		t.Fatal(err)
	}
	un := float64(wellness.Unknown)
	d := &wellness.Data{
		Suggestions: []wellness.Suggestion{
			{Day: dWalk, Name: "Base", Sport: int(typedef.SportRunning), SubSport: 0, Duration: 40 * time.Minute, Benefit: 2, AerobicTE: 2.5, AnaerobicTE: un},
			{Day: shiftDay(dWalk, -2), Name: "Base", Sport: int(typedef.SportRunning), Duration: 40 * time.Minute, Benefit: 2, AerobicTE: 2.5, AnaerobicTE: un},
		},
		WeighIns: []wellness.WeighIn{{At: walkAt.Add(time.Hour), Weight: 75, Fat: un, Hydration: un, Bone: un, Muscle: un, VisceralFat: un,
			BMI: un, BMR: wellness.Unknown, ActiveMR: wellness.Unknown, MetabolicAge: wellness.Unknown, VisceralRating: wellness.Unknown,
			Build: wellness.Unknown}},
	}
	if _, err := e.st.WriteFileData(ctx, f, d, nil, "fit", 8); err != nil {
		t.Fatal(err)
	}

	wks := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/workouts", nil))
	if len(wks) != 1 {
		t.Fatalf("workouts %v", wks)
	}
	wid := wks[0]["id"]
	plan := func(day string) map[string]any {
		return decodeJSON[map[string]any](t, admin.want(201, "POST", "/api/plans", map[string]any{"day": day, "workout_id": wid, "note": " hard "}))
	}
	pRun, pWalk, pPast, pAhead := plan(dRun), plan(dWalk), plan(shiftDay(dWalk, -2)), plan(shiftDay(today, 2))
	if pAhead["name"] != "Tempo" || pAhead["sport"] != "running" || pAhead["note"] != "hard" || pAhead["missed"] != false ||
		pAhead["done_activity"] != nil || pAhead["estimated_s"] != float64(1200) {
		t.Fatalf("a plan ahead: %v", pAhead)
	}

	cal := decodeJSON[struct {
		Today string           `json:"today"`
		Days  []map[string]any `json:"days"`
	}](t, admin.want(200, "GET", "/api/calendar?from="+shiftDay(dWalk, -3)+"&to="+shiftDay(today, 3), nil))
	if cal.Today != today {
		t.Fatalf("today %q, want %q", cal.Today, today)
	}
	byDay := map[string]map[string]any{}
	for _, x := range cal.Days {
		byDay[x["day"].(string)] = x
	}
	kinds := func(day string) map[string]any { return byDay[day]["kinds"].(map[string]any) }
	plans := func(day string) []any { return byDay[day]["plans"].([]any) }
	run := byDay[dRun]
	if run == nil || kinds(dRun)["activity"] != float64(1) || kinds(dRun)["plan"] != float64(1) {
		t.Fatalf("the run's day: %v", run)
	}
	acts := run["activities"].([]any)
	if acts[0].(map[string]any)["workout"] != "Tempo" {
		t.Fatalf("the run's workout: %v", acts)
	}
	if p := plans(dRun)[0].(map[string]any); p["done_activity"] != acts[0].(map[string]any)["id"] || p["id"] != pRun["id"] {
		t.Fatalf("done by name: %v", p)
	}
	if k := kinds(dWalk); k["weigh_in"] != float64(1) || k["plan"] != float64(2) || k["activity"] != float64(1) {
		t.Fatalf("the walk's day kinds: %v", k)
	}
	if p := plans(dWalk)[0].(map[string]any); p["done_activity"] == nil || p["missed"] != false || p["id"] != pWalk["id"] {
		t.Fatalf("done by sport (a walk for a run): %v", p)
	}
	if c := byDay[dWalk]["coach"].([]any)[0].(map[string]any); c["done"] != true || c["name"] != "Base" {
		t.Fatalf("Coach on the walk's day: %v", c)
	}
	past := shiftDay(dWalk, -2)
	if p := plans(past)[0].(map[string]any); p["missed"] != true || p["done_activity"] != nil || p["id"] != pPast["id"] {
		t.Fatalf("missed: %v", p)
	}
	if c := byDay[past]["coach"].([]any)[0].(map[string]any); c["missed"] != true {
		t.Fatalf("Coach missed: %v", c)
	}
	if byDay[shiftDay(today, 2)] == nil || byDay[shiftDay(today, 3)] != nil {
		t.Fatalf("days ahead: only the planned one: %v", cal.Days)
	}

	// Moving and removing; the routes check what they get.
	pid := fmt.Sprint(pAhead["id"])
	moved := decodeJSON[map[string]any](t, admin.want(200, "PATCH", "/api/plans/"+pid, map[string]any{"day": shiftDay(today, 4)}))
	if moved["day"] != shiftDay(today, 4) || moved["note"] != "hard" {
		t.Fatalf("moved: %v", moved)
	}
	admin.want(400, "PATCH", "/api/plans/"+pid, map[string]any{"workout_id": wid})
	admin.want(400, "PATCH", "/api/plans/"+pid, map[string]any{"day": "next week"})
	admin.want(400, "POST", "/api/plans", map[string]any{"day": now.AddDate(6, 0, 0).Format(time.DateOnly), "workout_id": wid})
	admin.want(400, "POST", "/api/plans", map[string]any{"workout_id": wid})
	admin.want(400, "POST", "/api/plans", map[string]any{"day": today, "workout_id": wid, "note": strings.Repeat("x", 501)})
	admin.want(404, "POST", "/api/plans", map[string]any{"day": today, "workout_id": 999999})
	admin.want(400, "GET", "/api/calendar?from="+shiftDay(today, -100)+"&to="+today, nil)

	// Another account sees and touches none of it.
	inv := decodeJSON[map[string]any](t, admin.want(201, "POST", "/api/admin/invites", map[string]string{}))
	other := e.client()
	other.want(201, "POST", "/api/invites/"+strings.TrimPrefix(inv["url"].(string), public+"/invite/")+"/accept",
		map[string]string{"email": "b@example.invalid", "password": password})
	other.want(404, "PATCH", "/api/plans/"+pid, map[string]any{"day": today})
	other.want(404, "DELETE", "/api/plans/"+pid, nil)
	other.want(404, "POST", "/api/plans", map[string]any{"day": today, "workout_id": wid})
	if c := decodeJSON[map[string]any](t, other.want(200, "GET", "/api/calendar?from="+shiftDay(dWalk, -3)+"&to="+today, nil)); len(c["days"].([]any)) != 0 {
		t.Fatalf("another account's calendar: %v", c)
	}
	admin.want(204, "DELETE", "/api/plans/"+pid, nil)
	admin.want(404, "DELETE", "/api/plans/"+pid, nil)
}

func TestPlanMatching(t *testing.T) {
	act := func(s typedef.Sport, sub typedef.SubSport) store.ActivitySummary {
		return store.ActivitySummary{Sport: int(s), SubSport: int(sub)}
	}
	for _, c := range []struct {
		sport, sub string
		a          store.ActivitySummary
		want       bool
	}{
		{"running", "generic", act(typedef.SportRunning, typedef.SubSportTrail), true},
		{"running", "", act(typedef.SportWalking, typedef.SubSportGeneric), true},
		{"cycling", "road", act(typedef.SportRunning, typedef.SubSportGeneric), false},
		{"fitness_equipment", "pilates", act(typedef.SportTraining, typedef.SubSportPilates), true},
		{"training", "yoga", act(typedef.SportTraining, typedef.SubSportStrengthTraining), true},
		{"mobility", "generic", act(typedef.SportTraining, typedef.SubSportYoga), false},
	} {
		if got := fits(c.sport, c.sub, c.a); got != c.want {
			t.Errorf("fits(%q, %q, %v/%v) = %v", c.sport, c.sub, typedef.Sport(c.a.Sport), typedef.SubSport(c.a.SubSport), got)
		}
	}
	// A plan finds its workout by the version it was planned with, else by name and sport.
	lib := store.Library{Workouts: []store.LibraryWorkout{{WorkoutRow: store.WorkoutRow{ID: 7}, Versions: []store.WorkoutRow{{ID: 7}, {ID: 9}}}}}
	lib.Workouts[0].Name, lib.Workouts[0].Sport = "Core 20", "training"
	if w := resolvePlan(lib, store.WorkoutPlan{WorkoutID: 9, Name: "x", Sport: "y"}); w == nil || w.ID != 7 {
		t.Fatalf("by version: %v", w)
	}
	if w := resolvePlan(lib, store.WorkoutPlan{WorkoutID: 3, Name: "core  20", Sport: "training"}); w == nil || w.ID != 7 {
		t.Fatalf("by name: %v", w)
	}
	if w := resolvePlan(lib, store.WorkoutPlan{WorkoutID: 3, Name: "Gone", Sport: "training"}); w != nil {
		t.Fatalf("a gone workout: %v", w)
	}
}
