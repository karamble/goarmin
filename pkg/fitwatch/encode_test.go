// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fitwatch

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
)

func intervals() Workout {
	return Workout{Name: "Intervals 6x400", Sport: "running", SubSport: "track", Steps: []Step{
		{Index: 0, Intensity: "warmup", Duration: "time", DurationValue: 600, Target: "heart_rate", TargetZone: 2, Notes: "Easy"},
		{Index: 1, Intensity: "active", Duration: "distance", DurationValue: 400, Target: "speed", TargetLow: 4.2, TargetHigh: 4.6},
		{Index: 2, Intensity: "recovery", Duration: "time", DurationValue: 90, Target: "heart_rate", TargetLow: 120, TargetHigh: 140},
		{Index: 3, Duration: "repeat", From: 1, Repeat: 6},
		{Index: 4, Intensity: "active", Duration: "time", DurationValue: 300, Target: "power", TargetLow: 80, TargetHigh: 95, TargetPct: true},
		{Index: 5, Intensity: "cooldown", Duration: "open", Target: "cadence", TargetLow: 80, TargetHigh: 90},
		{Index: 6, Intensity: "active", Duration: "hr_greater_than", DurationValue: 150},
	}}
}

func strength() Workout {
	return Workout{Name: "Core 20", Sport: "training", SubSport: "strength_training", Steps: []Step{
		{Index: 0, Intensity: "active", Duration: "reps", DurationValue: 12, Exercise: &Exercise{Category: "plank", Number: 43, Title: "Plank"},
			Notes: "Keep the hips level and breathe evenly through the whole set."},
		{Index: 1, Intensity: "rest", Duration: "time", DurationValue: 30},
		{Index: 2, Intensity: "active", Duration: "time", DurationValue: 45, Exercise: &Exercise{Category: "squat", Number: 29, Title: "Goblet Squat"}, WeightKg: 12.5},
		{Index: 3, Duration: "repeat", From: 0, Repeat: 3},
	}}
}

func TestEncodeWorkoutRoundTrip(t *testing.T) {
	created := time.Now().Truncate(time.Second)
	for _, w := range []Workout{intervals(), strength()} {
		b, err := EncodeWorkout(w, "A description", EncodeOptions{Serial: 1_757_016_972, Created: created})
		if err != nil {
			t.Fatalf("%s: %v", w.Name, err)
		}
		f, err := Decode(b)
		if err != nil {
			t.Fatalf("%s: %v", w.Name, err)
		}
		if f.Type != typedef.FileWorkout || !f.Created.Equal(created) || f.Workout == nil {
			t.Fatalf("%s: read back %+v", w.Name, f)
		}
		got := *f.Workout
		want := w
		want.EstimatedS = Estimate(w.Steps)
		for i := range want.Steps {
			// Notes over 50 characters are cut in the step and kept whole in the memo; the
			// reader takes the step's.
			if len(want.Steps[i].Notes) > 50 {
				want.Steps[i].Notes = want.Steps[i].Notes[:50]
			}
			if want.Steps[i].Exercise != nil {
				e := *want.Steps[i].Exercise
				e.Name = e.Title
				want.Steps[i].Exercise = &e
			}
			if want.Steps[i].Intensity == "" && !IsRepeat(want.Steps[i]) {
				want.Steps[i].Intensity = "active" // no intensity is written as active
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s round trip:\n got  %+v\n want %+v", w.Name, got, want)
		}
	}
}

// TestEncodeWorkoutAsGarminConnect checks the parts the Fenix 8 sees as Garmin Connect writes
// them (docs/protocol/08): the file_id, the total time, notes as a memo and exercise titles.
func TestEncodeWorkoutAsGarminConnect(t *testing.T) {
	created := time.Now().Truncate(time.Second)
	b, err := EncodeWorkout(strength(), "", EncodeOptions{Serial: 42, Created: created})
	if err != nil {
		t.Fatal(err)
	}
	fit, err := decoder.New(bytes.NewReader(b)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	var memos, titles, steps int
	for _, m := range fit.Messages {
		switch m.Num {
		case typedef.MesgNumFileId:
			id := mesgdef.NewFileId(&m)
			if id.Manufacturer != typedef.ManufacturerGarmin || id.Product != ConnectProduct || id.SerialNumber != 42 || !id.TimeCreated.Equal(created) {
				t.Errorf("file_id %+v", id)
			}
		case typedef.MesgNumWorkout:
			if f := m.FieldByNum(21); f == nil || f.Value.Uint32() != uint32(Estimate(strength().Steps))*1000 {
				t.Errorf("workout total time field: %v", f)
			}
		case typedef.MesgNumMemoGlob:
			memos++
			g := mesgdef.NewMemoGlob(&m)
			if g.MesgNum != typedef.MesgNumWorkoutStep || g.ParentIndex != 0 || g.FieldNum != 8 ||
				!strings.HasPrefix(string(g.Data), strength().Steps[0].Notes) || g.Data[len(g.Data)-1] != 0 {
				t.Errorf("memo %+v", g)
			}
		case typedef.MesgNumExerciseTitle:
			titles++
		case typedef.MesgNumWorkoutStep:
			steps++
		}
	}
	if memos != 1 || titles != 2 || steps != 4 {
		t.Fatalf("memos %d, titles %d, steps %d", memos, titles, steps)
	}
}

// TestEncodeLongDescription: a description over a FIT text field's 255 bytes (Garmin
// Connect's run to about 400) is cut at a character boundary instead of failing the file.
func TestEncodeLongDescription(t *testing.T) {
	desc := strings.Repeat("a", 253) + "ü more text after the limit"
	b, err := EncodeWorkout(strength(), desc, EncodeOptions{Serial: 1, Created: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	fit, err := decoder.New(bytes.NewReader(b)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range fit.Messages {
		if m.Num == typedef.MesgNumWorkout {
			if got := mesgdef.NewWorkout(&m).WktDescription; got != strings.Repeat("a", 253) {
				t.Fatalf("description %q (%d bytes)", got, len(got))
			}
			return
		}
	}
	t.Fatal("no workout message")
}

func TestValidateWorkout(t *testing.T) {
	ok := strength()
	for name, change := range map[string]func(*Workout){
		"no name":         func(w *Workout) { w.Name = " " },
		"long name":       func(w *Workout) { w.Name = strings.Repeat("x", MaxWorkoutName+1) },
		"unknown sport":   func(w *Workout) { w.Sport = "quidditch" },
		"no steps":        func(w *Workout) { w.Steps = nil },
		"repeat forward":  func(w *Workout) { w.Steps[3].From = 9 },
		"repeat once":     func(w *Workout) { w.Steps[3].Repeat = 1 },
		"zero time":       func(w *Workout) { w.Steps[1].DurationValue = 0 },
		"long notes":      func(w *Workout) { w.Steps[0].Notes = strings.Repeat("n", MaxStepNotes+1) },
		"duplicate index": func(w *Workout) { w.Steps[1].Index = 0 },
		"nested repeat": func(w *Workout) {
			w.Steps = append(w.Steps, Step{Index: 4, Duration: "repeat", From: 0, Repeat: 2})
		},
		"unknown exercise": func(w *Workout) { w.Steps[0].Exercise = &Exercise{Category: "juggling", Number: 1} },
	} {
		w := ok
		w.Steps = append([]Step(nil), ok.Steps...)
		change(&w)
		if _, err := EncodeWorkout(w, "", EncodeOptions{Serial: 1, Created: time.Now()}); !errors.Is(err, ErrInvalidWorkout) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestEncodeScheduleAsGarminConnect writes the schedule the capture holds: one workout on a
// day, named by its workout file's identity, the day at local midnight.
func TestEncodeScheduleAsGarminConnect(t *testing.T) {
	now := time.Now()
	day := now.AddDate(0, 0, 1)
	workout := FileID{Manufacturer: uint16(typedef.ManufacturerGarmin), Product: ConnectProduct, Serial: 1_757_016_972, Created: now.Add(-4 * time.Hour).Truncate(time.Second)}
	b, err := EncodeSchedule([]ScheduleEntry{{Workout: workout, Day: day}}, EncodeOptions{Serial: 1, Created: now})
	if err != nil {
		t.Fatal(err)
	}
	fit, err := decoder.New(bytes.NewReader(b)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	var got []*mesgdef.Schedule
	for _, m := range fit.Messages {
		switch m.Num {
		case typedef.MesgNumFileId:
			if id := mesgdef.NewFileId(&m); id.Type != typedef.FileSchedules || id.Product != ConnectProduct {
				t.Errorf("file_id %+v", id)
			}
		case typedef.MesgNumSchedule:
			got = append(got, mesgdef.NewSchedule(&m))
		}
	}
	y, mo, d := day.Date()
	if len(got) != 1 || got[0].SerialNumber != workout.Serial || !got[0].TimeCreated.Equal(workout.Created) ||
		got[0].Type != typedef.ScheduleWorkout || got[0].Completed != typedef.BoolFalse ||
		!got[0].ScheduledTime.Equal(time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("schedule %+v", got)
	}
	if _, err := EncodeSchedule([]ScheduleEntry{{Day: day}}, EncodeOptions{Serial: 1, Created: now}); err == nil {
		t.Fatal("an entry without its workout's identity")
	}
}

// TestEncodeLikeCapturedPushes is opt-in: GOARMIN_PUSHED_DIR names a folder of the files
// Garmin Connect pushed in a capture (tools/snoopdecode -out writes them as push_*.fit). Each
// workout read from there and written again by EncodeWorkout reads back the same.
func TestEncodeLikeCapturedPushes(t *testing.T) {
	dir := os.Getenv("GOARMIN_PUSHED_DIR")
	if dir == "" {
		t.Skip("set GOARMIN_PUSHED_DIR to a folder of pushed files")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "push_*.fit"))
	n := 0
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, err := Decode(b)
		if err != nil || f.Workout == nil {
			continue
		}
		n++
		again, err := EncodeWorkout(*f.Workout, "", EncodeOptions{Serial: 7, Created: f.Created})
		if err != nil {
			t.Errorf("%s (%s): %v", filepath.Base(path), f.Workout.Name, err)
			continue
		}
		g, err := Decode(again)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(g.Workout, f.Workout) {
			t.Errorf("%s (%s): written again it reads\n %+v\nnot\n %+v", filepath.Base(path), f.Workout.Name, g.Workout, f.Workout)
		}
	}
	t.Logf("%d captured workouts written again", n)
}
