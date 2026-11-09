// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fitwatch

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
		w.Description = "A description"
		b, err := EncodeWorkout(w, EncodeOptions{Serial: 1_757_016_972, Created: created})
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
		// A workout made in goarmin gets Garmin Connect's fields for a user's workout, the total
		// from Total, which the reader takes as the workout's time.
		zero, ms, sw := uint8(0), uint32(Total(w.Steps))*1000, uint16(garminConnectSoftware)
		want.Extra = &WorkoutExtra{Software: &sw, F9: &zero, F10: &ms, F16: make([]byte, 16), F21: &ms, F23: &zero}
		for _, st := range w.Steps { // the titles in the order of their first step
			if e := st.Exercise; e != nil {
				k := [2]uint16{uint16(typedef.ExerciseCategoryFromString(e.Category)), uint16(e.Number)}
				if !slices.Contains(want.Extra.Titles, k) {
					want.Extra.Titles = append(want.Extra.Titles, k)
				}
			}
		}
		want.EstimatedS = Total(w.Steps)
		for i := range want.Steps {
			// Notes are whole: the step's field holds 50 characters, the memo all of them.
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
	b, err := EncodeWorkout(strength(), EncodeOptions{Serial: 42, Created: created})
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
			if f := m.FieldByNum(21); f == nil || f.Value.Uint32() != uint32(Total(strength().Steps))*1000 {
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

// TestEncodeLongDescription: as Garmin Connect writes it, a long description's field holds
// 157 bytes and "..." (cut at a character boundary), and right after the workout message a memo
// holds the whole text in 255-byte parts, the last padded with zeros. The reader takes the memo.
func TestEncodeLongDescription(t *testing.T) {
	desc := strings.Repeat("a", 156) + "ü" + strings.Repeat(" more text", 30)
	w := strength()
	w.Description = desc
	b, err := EncodeWorkout(w, EncodeOptions{Serial: 1, Created: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	fit, err := decoder.New(bytes.NewReader(b)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	var parts [][]byte
	afterWorkout := false
	for i, m := range fit.Messages {
		switch m.Num {
		case typedef.MesgNumWorkout:
			if got := mesgdef.NewWorkout(&m).WktDescription; got != strings.Repeat("a", 156)+"..." {
				t.Fatalf("description field %q (%d bytes)", got, len(got))
			}
			afterWorkout = fit.Messages[i+1].Num == typedef.MesgNumMemoGlob
		case typedef.MesgNumMemoGlob:
			g := mesgdef.NewMemoGlob(&m)
			if g.MesgNum == typedef.MesgNumWorkout && g.FieldNum == 17 {
				if int(g.PartIndex) != len(parts) || len(g.Data) != 255 {
					t.Fatalf("part %d of %d bytes", g.PartIndex, len(g.Data))
				}
				parts = append(parts, g.Data)
			}
		}
	}
	if !afterWorkout || len(parts) != 2 || parts[1][len(parts[1])-1] != 0 {
		t.Fatalf("%d parts, right after the workout %v", len(parts), afterWorkout)
	}
	f, err := Decode(b)
	if err != nil || f.Workout.Description != desc {
		t.Fatalf("read back %q, %v", f.Workout.Description, err)
	}
	// A short one: the field and a single memo with its zero.
	w.Description = "Short"
	b, _ = EncodeWorkout(w, EncodeOptions{Serial: 1, Created: time.Now()})
	if f, _ := Decode(b); f.Workout.Description != "Short" {
		t.Fatalf("short description %q", f.Workout.Description)
	}
}

// TestEncodeRepeatAsGarminConnect: a repeat step carries its first step, count and field 18 =
// 0, and nothing else; notes over 254 bytes come back whole from their memo parts.
func TestEncodeRepeatAsGarminConnect(t *testing.T) {
	w := strength()
	w.Steps[0].Notes = strings.Repeat("ä", 200) // 400 bytes
	b, err := EncodeWorkout(w, EncodeOptions{Serial: 1, Created: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	fit, err := decoder.New(bytes.NewReader(b)).Decode()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range fit.Messages {
		if m.Num != typedef.MesgNumWorkoutStep {
			continue
		}
		s := mesgdef.NewWorkoutStep(&m)
		if s.DurationType != typedef.WktStepDurationRepeatUntilStepsCmplt {
			continue
		}
		var valid []byte
		for _, f := range m.Fields {
			if f.Value.Valid(f.BaseType) {
				valid = append(valid, f.Num)
			}
		}
		if !slices.Equal(valid, []byte{254, 1, 2, 4, 18}) && !slices.Equal(valid, []byte{1, 2, 4, 254, 18}) {
			slices.Sort(valid)
			if !slices.Equal(valid, []byte{1, 2, 4, 18, 254}) {
				t.Fatalf("repeat step fields %v", valid)
			}
		}
		if f := m.FieldByNum(18); f == nil || f.Value.Uint8() != 0 {
			t.Fatalf("field 18 %v", f)
		}
	}
	f, err := Decode(b)
	if err != nil || f.Workout.Steps[0].Notes != w.Steps[0].Notes {
		t.Fatalf("notes read back %d runes, %v", len([]rune(f.Workout.Steps[0].Notes)), err)
	}
}

// TestEncodeKeepsExtra: an original's unnamed workout fields are written back as read; only
// the totals Extra lacks are computed.
func TestEncodeKeepsExtra(t *testing.T) {
	one, zero, total, z32 := uint8(1), uint8(0), uint32(540000), uint32(0)
	w := strength()
	w.Extra = &WorkoutExtra{F9: &one, F10: &z32, F16: append(make([]byte, 15), 1), F21: &total, F22: &z32, F23: &zero, F24: &zero}
	b, err := EncodeWorkout(w, EncodeOptions{Serial: 1, Created: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	f, err := Decode(b)
	if err == nil { // what the file says besides: Garmin Connect's software, the titles' order
		w.Extra.Software, w.Extra.Titles = f.Workout.Extra.Software, f.Workout.Extra.Titles
	}
	if err != nil || !reflect.DeepEqual(f.Workout.Extra, w.Extra) || f.Workout.EstimatedS != 540 || *w.Extra.Software != garminConnectSoftware {
		t.Fatalf("read back %+v (estimated %d), %v", f.Workout.Extra, f.Workout.EstimatedS, err)
	}
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
		if _, err := EncodeWorkout(w, EncodeOptions{Serial: 1, Created: time.Now()}); !errors.Is(err, ErrInvalidWorkout) {
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
		again, err := EncodeWorkout(*f.Workout, EncodeOptions{Serial: 7, Created: f.Created})
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

// TestEncodeLikeGarminFiles is opt-in: GOARMIN_WORKOUT_DIR names a folder of workout files
// Garmin Connect wrote (product 65534 in their file_id; the watch's own built-in workouts are
// left out). Each one read and written again says exactly the same, message by message, its
// identity aside (Compare).
func TestEncodeLikeGarminFiles(t *testing.T) {
	dir := os.Getenv("GOARMIN_WORKOUT_DIR")
	if dir == "" {
		t.Skip("set GOARMIN_WORKOUT_DIR to a folder of Garmin Connect's workout files")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	n := 0
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fit, err := decoder.New(bytes.NewReader(b)).Decode()
		if err != nil || len(fit.Messages) == 0 || fit.Messages[0].Num != typedef.MesgNumFileId {
			continue
		}
		if id := mesgdef.NewFileId(&fit.Messages[0]); id.Type != typedef.FileWorkout || id.Product != ConnectProduct {
			continue
		}
		f, err := Decode(b)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(path), err)
		}
		n++
		again, err := EncodeWorkout(*f.Workout, EncodeOptions{Serial: 7, Created: f.Created})
		if err != nil {
			t.Errorf("%s (%s): %v", filepath.Base(path), f.Workout.Name, err)
			continue
		}
		diff, err := Compare(b, again)
		if err != nil {
			t.Fatal(err)
		}
		if len(diff) > 0 {
			if len(diff) > 8 {
				diff = append(diff[:8], fmt.Sprintf("… %d more", len(diff)-8))
			}
			t.Errorf("%s (%s) written again differs:\n  %s", filepath.Base(path), f.Workout.Name, strings.Join(diff, "\n  "))
		}
	}
	t.Logf("%d Garmin Connect workout files written again", n)
}
