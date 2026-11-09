// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gcexport

import (
	"math"
	"strings"
	"time"

	"github.com/karamble/goarmin/pkg/fitwatch"
)

// Workout is one of the account's Garmin Connect workouts, in the form a watch's workout file
// reads as (fitwatch.Workout), with Connect's own fields.
type Workout struct {
	fitwatch.Workout       // its description included
	ID               int64 // Garmin Connect's workoutId
	Created, Updated time.Time
}

// connectSports maps Garmin Connect's sport type keys to the FIT sport and sub sport a watch
// writes for the same workout (checked against the Fenix 8's files: pilates is
// fitness_equipment/pilates, yoga training/yoga, cardio_training training/cardio_training).
var connectSports = map[string][2]string{
	"running": {"running", "generic"}, "trail_running": {"running", "trail"}, "treadmill_running": {"running", "treadmill"},
	"cycling": {"cycling", "generic"}, "road_biking": {"cycling", "road"}, "mountain_biking": {"cycling", "mountain"},
	"indoor_cycling": {"cycling", "indoor_cycling"}, "swimming": {"swimming", "generic"},
	"lap_swimming": {"swimming", "lap_swimming"}, "open_water_swimming": {"swimming", "open_water"},
	"walking": {"walking", "generic"}, "hiking": {"hiking", "generic"},
	"strength_training": {"training", "strength_training"}, "cardio_training": {"training", "cardio_training"},
	"yoga": {"training", "yoga"}, "pilates": {"fitness_equipment", "pilates"}, "breathwork": {"training", "breathing"},
	"mobility": {"mobility", "generic"}, "hiit": {"hiit", "generic"}, "elliptical": {"fitness_equipment", "elliptical"},
	"stair_climbing": {"fitness_equipment", "stair_climbing"}, "indoor_rowing": {"rowing", "indoor_rowing"},
	"rowing": {"rowing", "generic"}, "multi_sport": {"multisport", "generic"}, "other": {"generic", "generic"},
}

// connectIntensity maps a step type key to the intensity a watch's file gives the step.
var connectIntensity = map[string]string{"warmup": "warmup", "cooldown": "cooldown", "interval": "active", "rest": "rest",
	"recovery": "recovery", "other": "active", "main": "active"}

// workouts reads the export's workout file: [{"workoutList": [workout, …]}].
func (r *Result) workouts(v any) {
	r.each(v, func(m obj) {
		list := m.list("workoutList")
		if _, ok := m["workoutName"]; ok {
			list = []obj{m} // a bare workout
		}
		for _, w := range list {
			if wk, ok := connectWorkout(w); ok {
				r.Workouts = append(r.Workouts, wk)
			}
		}
	})
}

func connectWorkout(m obj) (Workout, bool) {
	name := strings.TrimSpace(m.str("workoutName"))
	if name == "" {
		return Workout{}, false
	}
	key := m.str("sportType.sportTypeKey")
	sp, ok := connectSports[key]
	if !ok {
		sp = [2]string{key, "generic"}
	}
	out := Workout{Workout: fitwatch.Workout{Name: name, Sport: sp[0], SubSport: sp[1], Description: strings.TrimSpace(m.str("description"))}}
	if id := m.num("workoutId"); !math.IsNaN(id) {
		out.ID = int64(id)
	}
	out.Created, _ = m.time("createdDate")
	out.Updated, _ = m.time("updatedDate")
	if out.Updated.IsZero() {
		out.Updated = out.Created
	}
	out.Steps = flattenSteps(m.list("workoutSteps"), nil)
	if est := m.num("estimatedDurationInSecs"); !math.IsNaN(est) && est > 0 {
		out.EstimatedS = int(math.Round(est))
	} else {
		out.EstimatedS = fitwatch.Estimate(out.Steps)
	}
	return out, true
}

// flattenSteps turns Connect's nested steps into a watch file's flat list: a repeat group's
// steps come first, then a repeat step that goes back to the first of them.
func flattenSteps(list []obj, out []fitwatch.Step) []fitwatch.Step {
	for _, s := range list {
		if s.str("type") == "RepeatGroupDTO" || s.str("stepType.stepTypeKey") == "repeat" {
			from := len(out)
			out = flattenSteps(s.list("workoutSteps"), out)
			if len(out) == from {
				continue // an empty group
			}
			rep := fitwatch.Step{Index: len(out), Duration: "repeat", From: from, Repeat: max(1, s.int("numberOfIterations"))}
			// An AMRAP comes as a count in milliseconds: 600000 "iterations" for 10 minutes (its
			// workout's estimated duration says the same).
			if n := s.int("numberOfIterations"); n >= 1000 && n%1000 == 0 {
				rep.Duration, rep.Repeat, rep.DurationValue = "repeat_until_time", 0, float64(n/1000)
			}
			out = append(out, rep)
			continue
		}
		out = append(out, connectStep(s, len(out)))
	}
	return out
}

func connectStep(s obj, index int) fitwatch.Step {
	st := fitwatch.Step{Index: index, Notes: strings.TrimSpace(s.str("description")), Intensity: "active"}
	if i, ok := connectIntensity[s.str("stepType.stepTypeKey")]; ok {
		st.Intensity = i
	}
	v := s.num("endConditionValue")
	switch s.str("endCondition.conditionTypeKey") {
	case "time", "fixed.rest":
		st.Duration = "time"
	case "distance":
		st.Duration = "distance"
	case "reps":
		st.Duration = "reps"
	case "calories":
		st.Duration = "calories"
	default: // lap.button, and conditions without a counterpart
		st.Duration, v = "open", math.NaN()
	}
	if !math.IsNaN(v) && v > 0 {
		st.DurationValue = v
	}
	st.Target = "open"
	lo, hi := s.num("targetValueOne"), s.num("targetValueTwo")
	zone := s.int("zoneNumber")
	switch s.str("targetType.workoutTargetTypeKey") {
	case "heart.rate.zone":
		st.Target = "heart_rate"
	case "pace.zone", "speed.zone":
		st.Target = "speed" // m/s, as in a watch's file
	case "power.zone":
		st.Target = "power"
	case "cadence", "cadence.zone":
		st.Target = "cadence"
	}
	if st.Target != "open" {
		switch {
		case zone > 0:
			st.TargetZone = zone
		case !math.IsNaN(lo) && !math.IsNaN(hi):
			st.TargetLow, st.TargetHigh = math.Min(lo, hi), math.Max(lo, hi)
		}
	}
	if cat, ex := s.str("category"), s.str("exerciseName"); cat != "" && ex != "" {
		if n, ok := fitwatch.ExerciseNumber(cat, ex); ok {
			st.Exercise = &fitwatch.Exercise{Category: strings.ToLower(cat), Name: strings.ToLower(ex), Number: n}
		} else {
			st.Name = strings.ReplaceAll(strings.ToLower(ex), "_", " ")
		}
	}
	if w := s.num("weightValue"); !math.IsNaN(w) && w > 0 {
		switch s.str("weightUnit.unitKey") {
		case "pound":
			st.WeightKg = math.Round(w*0.45359237*100) / 100
		case "kilogram", "":
			st.WeightKg = w
		}
	}
	return st
}
