// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gcexport

import (
	"testing"
	"time"
)

func exec(stepType, cond string, value float64, extra map[string]any) map[string]any {
	s := map[string]any{"type": "ExecutableStepDTO", "stepType": map[string]any{"stepTypeKey": stepType},
		"endCondition": map[string]any{"conditionTypeKey": cond}, "endConditionValue": value,
		"targetType": map[string]any{"workoutTargetTypeKey": "no.target"}}
	for k, v := range extra {
		s[k] = v
	}
	return s
}

func repeat(n int, steps ...map[string]any) map[string]any {
	list := make([]any, len(steps))
	for i, s := range steps {
		list[i] = s
	}
	return map[string]any{"type": "RepeatGroupDTO", "stepType": map[string]any{"stepTypeKey": "repeat"},
		"numberOfIterations": n, "workoutSteps": list}
}

// The export's workout file becomes workouts shaped like a watch's: sports as the watch writes
// them, Connect's step types as intensities, repeat groups flattened, exercises by number.
func TestWorkouts(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	created, updated := now.AddDate(0, -3, 0), now.AddDate(0, -1, 0)
	file := []any{map[string]any{"workoutList": []any{
		map[string]any{
			"workoutId": 658980089, "workoutName": " Core Flow ", "description": "Slow and steady",
			"sportType": map[string]any{"sportTypeKey": "pilates"}, "estimatedDurationInSecs": 0,
			"createdDate": created.Format(gmt), "updatedDate": updated.Format(gmt),
			"workoutSteps": []any{
				exec("warmup", "lap.button", 0, nil),
				repeat(3,
					exec("interval", "time", 45, map[string]any{"category": "CORE", "exerciseName": "THE_HUNDRED"}),
					exec("rest", "time", 15, map[string]any{"description": "Breathe"}),
				),
				exec("interval", "reps", 12, map[string]any{"category": "PLANK", "exerciseName": "NOT_IN_THE_PROFILE",
					"weightValue": 10, "weightUnit": map[string]any{"unitKey": "pound"}}),
				exec("cooldown", "time", 60, map[string]any{"targetType": map[string]any{"workoutTargetTypeKey": "heart.rate.zone"}, "zoneNumber": 1}),
			},
		},
		map[string]any{
			"workoutName": "10-Minute AMRAP", "sportType": map[string]any{"sportTypeKey": "hiit"}, "estimatedDurationInSecs": 600,
			"workoutSteps": []any{repeat(600000,
				exec("interval", "reps", 10, map[string]any{"category": "SQUAT", "exerciseName": "AIR_SQUAT"}),
				exec("interval", "reps", 10, map[string]any{"category": "PUSH_UP", "exerciseName": "PUSH_UP"}),
			)},
		},
		map[string]any{"workoutName": "", "sportType": map[string]any{"sportTypeKey": "running"}},
	}}}
	r, err := Read("DI_CONNECT/DI-Connect-Fitness/someone_workout.json", mustJSON(t, file))
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != "workouts" || r.Document == nil || len(r.Workouts) != 2 {
		t.Fatalf("kind %q, %d workouts, document %v", r.Kind, len(r.Workouts), r.Document != nil)
	}
	w := r.Workouts[0]
	if w.Name != "Core Flow" || w.Sport != "fitness_equipment" || w.SubSport != "pilates" || w.ID != 658980089 ||
		w.Description != "Slow and steady" || !w.Created.Equal(created) || !w.Updated.Equal(updated) {
		t.Fatalf("workout %+v", w)
	}
	s := w.Steps
	if len(s) != 6 {
		t.Fatalf("%d steps: %+v", len(s), s)
	}
	for i, st := range s {
		if st.Index != i {
			t.Fatalf("step %d has index %d", i, st.Index)
		}
	}
	if s[0].Intensity != "warmup" || s[0].Duration != "open" || s[0].Target != "open" {
		t.Fatalf("warm-up %+v", s[0])
	}
	if s[1].Intensity != "active" || s[1].Duration != "time" || s[1].DurationValue != 45 || s[1].Exercise == nil ||
		s[1].Exercise.Category != "core" || s[1].Exercise.Name != "the_hundred" || s[1].Exercise.Number != 72 {
		t.Fatalf("exercise step %+v (%+v)", s[1], s[1].Exercise)
	}
	if s[2].Intensity != "rest" || s[2].Notes != "Breathe" || s[2].DurationValue != 15 {
		t.Fatalf("rest %+v", s[2])
	}
	if s[3].Duration != "repeat" || s[3].From != 1 || s[3].Repeat != 3 {
		t.Fatalf("repeat %+v", s[3])
	}
	if s[4].Exercise != nil || s[4].Name != "not in the profile" || s[4].Duration != "reps" || s[4].WeightKg != 4.54 {
		t.Fatalf("unknown exercise %+v", s[4])
	}
	if s[5].Intensity != "cooldown" || s[5].Target != "heart_rate" || s[5].TargetZone != 1 {
		t.Fatalf("cool-down %+v", s[5])
	}
	// Connect said 0: the steps' own estimate, 45 + 15 three times and the last minute.
	if w.EstimatedS != 3*60+60 {
		t.Fatalf("estimate %d", w.EstimatedS)
	}

	a := r.Workouts[1]
	if a.Sport != "hiit" || len(a.Steps) != 3 || a.EstimatedS != 600 || !a.Updated.IsZero() {
		t.Fatalf("AMRAP %+v", a)
	}
	if rep := a.Steps[2]; rep.Duration != "repeat_until_time" || rep.From != 0 || rep.DurationValue != 600 || rep.Repeat != 0 {
		t.Fatalf("AMRAP repeat %+v", rep)
	}
}
