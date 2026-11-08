// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/karamble/goarmin/pkg/fitactivity"
	"github.com/karamble/goarmin/pkg/garmin/gcexport"
	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

// TestActivitySummaryYields: an activity summary (FIT 20) never replaces the activity stored
// from its own file, and an activity file replaces a summary stored first.
func TestActivitySummaryYields(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	u := person(t, st, "a@example.invalid")
	start := time.Now().Add(-5 * time.Hour).Truncate(time.Second)
	act := func(name string) []fitactivity.Activity {
		return []fitactivity.Activity{{Index: 0, Sport: 1, Name: name, Start: start, End: start.Add(time.Hour), Elapsed: 3600, Timer: 3600,
			Distance: 10000, Calories: 600, AvgHR: 140, MaxHR: 170, AvgSpeed: math.NaN(), MaxSpeed: math.NaN(), AvgCadence: -1, AvgPower: -1,
			Ascent: -1, Descent: -1, TrainingEffect: math.NaN(), AnaerobicEffect: math.NaN(), TrainingLoad: math.NaN(), Polyline: "abc"}}
	}
	full := rawFile(t, st, u.ID, "run", 4, start)
	sum := rawFile(t, st, u.ID, "summary", 20, start)
	if _, err := st.WriteFileData(ctx, full, &wellness.Data{}, act("Run"), "fit", 7); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteFileData(ctx, sum, &wellness.Data{}, act("Summary"), "fit", 7); err != nil {
		t.Fatal(err)
	}
	list, err := st.ActivitiesIn(ctx, u.ID, "0000-01-01", "9999-12-31")
	if err != nil || len(list) != 1 || list[0].Name != "Run" || list[0].Polyline != "abc" {
		t.Fatalf("after the summary %+v %v", list, err)
	}

	st2 := openTest(t)
	u2 := person(t, st2, "a@example.invalid")
	full2, sum2 := rawFile(t, st2, u2.ID, "run", 4, start), rawFile(t, st2, u2.ID, "summary", 20, start)
	if _, err := st2.WriteFileData(ctx, sum2, &wellness.Data{}, act("Summary"), "fit", 7); err != nil {
		t.Fatal(err)
	}
	if _, err := st2.WriteFileData(ctx, full2, &wellness.Data{}, act("Run"), "fit", 7); err != nil {
		t.Fatal(err)
	}
	if list, _ := st2.ActivitiesIn(ctx, u2.ID, "0000-01-01", "9999-12-31"); len(list) != 1 || list[0].Name != "Run" {
		t.Fatalf("the activity file did not replace the summary: %+v", list)
	}
}

// TestWeighInSources: a scale's own file wins over the export's record of the same weighing,
// which only fills what the file lacks.
func TestWeighInSources(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	u := person(t, st, "a@example.invalid")
	at := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	const un = wellness.Unknown
	fit := wellness.WeighIn{At: at, Weight: 75.2, Fat: 18.5, Hydration: un, Bone: un, Muscle: un, VisceralFat: un, BMI: 23.1, BMR: un,
		ActiveMR: un, MetabolicAge: un, VisceralRating: un, Build: un}
	json := fit
	json.Weight, json.Fat, json.Muscle = 75.0, 19, 33.3
	f := rawFile(t, st, u.ID, "weight", 9, at)
	if _, err := st.WriteFileData(ctx, f, &wellness.Data{WeighIns: []wellness.WeighIn{fit}}, nil, "fit", 7); err != nil {
		t.Fatal(err)
	}
	if err := WriteWeighIn(ctx, st.db.W, u.ID, f.ID, FromJSON, json); err != nil {
		t.Fatal(err)
	}
	ws, err := st.WeighIns(ctx, u.ID, at.Add(-time.Hour).Unix(), at.Add(time.Hour).Unix())
	if err != nil || len(ws) != 1 || ws[0].WeightKg != 75.2 || *ws[0].FatPct != 18.5 || ws[0].Source != FromFIT {
		t.Fatalf("weigh-ins %+v %v", ws, err)
	}
	// The export's record alone is stored, then the file's replaces it.
	at2 := at.Add(24 * time.Hour)
	json.At, fit.At = at2, at2
	if err := WriteWeighIn(ctx, st.db.W, u.ID, f.ID, FromJSON, json); err != nil {
		t.Fatal(err)
	}
	if err := WriteWeighIn(ctx, st.db.W, u.ID, f.ID, FromFIT, fit); err != nil {
		t.Fatal(err)
	}
	ws, _ = st.WeighIns(ctx, u.ID, at2.Unix(), at2.Unix()+1)
	if len(ws) != 1 || ws[0].WeightKg != 75.2 || ws[0].Source != FromFIT || *ws[0].MuscleKg != 33.3 {
		t.Fatalf("the file over the export %+v", ws)
	}
}

// TestExportRanks: a day's value from a watch's file wins over the export's, whichever comes
// first; the export fills days the watch's files lack. An export's night merges with the watch's.
func TestExportRanks(t *testing.T) {
	ctx := context.Background()
	at := time.Now().Add(-72 * time.Hour).UTC().Truncate(time.Second)
	day := at.Format(time.DateOnly)
	other := at.AddDate(0, 0, -1).Format(time.DateOnly)
	fitVal := &wellness.Data{Daily: []wellness.Daily{{Kind: wellness.TrainingReadiness, At: at, Day: day, Value: 80}}}
	exp := &gcexport.Result{Kind: "readiness", Daily: []wellness.Daily{
		{Kind: wellness.TrainingReadiness, At: at.Add(time.Hour), Day: day, Value: 55},
		{Kind: wellness.TrainingReadiness, At: at.Add(-24 * time.Hour), Day: other, Value: 60},
	}}
	for _, fitFirst := range []bool{true, false} {
		st := openTest(t)
		u := person(t, st, "a@example.invalid")
		ff := rawFile(t, st, u.ID, "metrics", 44, at)
		jf, _, err := st.AddRawFile(ctx, RawFile{SHA256: strings.Repeat("ab", 32), Size: 1, UserID: u.ID, Source: SourceGCExport,
			SourceName: "x.zip/DI_CONNECT/DI-Connect-Metrics/TrainingReadinessDTO_1_2_3.json"})
		if err != nil {
			t.Fatal(err)
		}
		writeFIT := func() {
			if _, err := st.WriteFileData(ctx, ff, fitVal, nil, "fit", 7); err != nil {
				t.Fatal(err)
			}
		}
		writeJSON := func() {
			if _, err := st.WriteExportFile(ctx, jf, exp, "gcjson", 1); err != nil {
				t.Fatal(err)
			}
		}
		if fitFirst {
			writeFIT()
			writeJSON()
		} else {
			writeJSON()
			writeFIT()
		}
		vals, err := st.DailyValues(ctx, u.ID, other, day)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]int64{}
		for _, v := range vals {
			got[v.Day] = v.Value
		}
		if got[day] != 80 || got[other] != 60 {
			t.Fatalf("fit first %v: %v", fitFirst, got)
		}
	}

	st := openTest(t)
	u := person(t, st, "a@example.invalid")
	wake := at
	stages := unknownSleep(wake.Add(-7*time.Hour), wake)
	stages.Stages, stages.Deep, stages.Light, stages.REM, stages.Awake, stages.Unmeasurable = true, 1, 2, 3, 4, 0
	if _, err := st.WriteFileData(ctx, rawFile(t, st, u.ID, "sleep", 49, wake), &wellness.Data{Sleep: []wellness.Sleep{stages}}, nil, "fit", 7); err != nil {
		t.Fatal(err)
	}
	night := unknownSleep(wake.Add(-7*time.Hour-2*time.Minute), wake.Add(time.Minute))
	night.Score, night.Deep, night.Light, night.REM, night.Awake, night.Unmeasurable = 77, 9, 9, 9, 9, 0
	jf, _, _ := st.AddRawFile(ctx, RawFile{SHA256: strings.Repeat("cd", 32), Size: 1, UserID: u.ID, Source: SourceGCExport, SourceName: "x/sleepData.json"})
	if _, err := st.WriteExportFile(ctx, jf, &gcexport.Result{Kind: "sleep", Sleep: []wellness.Sleep{night}}, "gcjson", 1); err != nil {
		t.Fatal(err)
	}
	got := nights(t, st, u.ID)
	if len(got) != 1 || *got[0].Deep != 1 || *got[0].Score != 77 || got[0].Start != stages.Start.Unix() {
		t.Fatalf("merged night %+v", got)
	}
}
