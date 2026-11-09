// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/karamble/goarmin/pkg/fitwatch"
)

// strengthBody is a workout as the builder sends it: three sets of planks with a rest, then a
// timed squat.
func strengthBody(name string) map[string]any {
	return map[string]any{"name": name, "sport": "training", "sub_sport": "strength_training", "description": "  Core first.  ",
		"steps": []map[string]any{
			{"index": 0, "intensity": "active", "duration": "reps", "duration_value": 12, "target": "open",
				"exercise": map[string]any{"category": "plank", "number": 43}, "notes": "Hips level"},
			{"index": 1, "intensity": "rest", "duration": "time", "duration_value": 30, "target": "open"},
			{"index": 2, "duration": "repeat", "from": 0, "repeat": 3},
			{"index": 3, "intensity": "active", "duration": "time", "duration_value": 45,
				"exercise": map[string]any{"category": "squat", "number": 29}, "weight_kg": 12.5},
		}}
}

// TestWorkoutBuilder makes, checks, downloads, edits, duplicates, hides and deletes a workout,
// and keeps another account out of it.
func TestWorkoutBuilder(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	b := e.second(a)
	x := decodeJSON[map[string]any](t, a.want(201, "POST", "/api/workouts", strengthBody("Core   Sets")))
	id := fmt.Sprint(x["id"])
	steps := x["steps"].([]any)
	ex := steps[0].(map[string]any)["exercise"].(map[string]any)
	if x["name"] != "Core Sets" || x["origin"] != "goarmin" || x["description"] != "Core first." || x["tag"] == nil ||
		ex["title"] != "Plank" || ex["name"] != "Plank" || x["estimated_s"] != float64(3*12*fitwatch.RepSeconds+3*30+45) {
		t.Fatalf("made %v", x)
	}

	// What a watch would not take is refused with the reason.
	bad := func(change func(m map[string]any), want string) {
		t.Helper()
		m := strengthBody("Another")
		change(m)
		code, body := a.do("POST", "/api/workouts", m)
		if code != 400 || !strings.Contains(string(body), want) {
			t.Errorf("%s: %d %s", want, code, body)
		}
	}
	step := func(m map[string]any, i int) map[string]any { return m["steps"].([]map[string]any)[i] }
	bad(func(m map[string]any) { m["name"] = "  " }, "needs a name")
	bad(func(m map[string]any) { m["sport"] = "quidditch" }, "unknown sport")
	bad(func(m map[string]any) { step(m, 0)["exercise"] = map[string]any{"category": "plank", "number": 9999} }, "no exercise plank/9999")
	bad(func(m map[string]any) { step(m, 3)["target"], step(m, 3)["target_zone"] = "heart_rate", 9 }, "no such zone")
	bad(func(m map[string]any) {
		step(m, 3)["target"], step(m, 3)["target_low"], step(m, 3)["target_high"] = "heart_rate", 150, 120
	}, "low ≤ high")
	bad(func(m map[string]any) { step(m, 1)["duration_value"] = 0 }, "above 0")
	bad(func(m map[string]any) {
		m["steps"] = append(m["steps"].([]map[string]any), map[string]any{"index": 4, "duration": "repeat", "from": 0, "repeat": 2})
	}, "repeats cannot hold repeats")
	if code, _ := a.do("POST", "/api/workouts", strengthBody("core sets")); code != 409 {
		t.Fatalf("a second workout of that name: %d", code)
	}

	// The file a watch gets: written as Garmin Connect writes one, reading back the same.
	code, file := a.do("GET", "/api/workouts/"+id+"/fit", nil)
	f, err := fitwatch.Decode(file)
	if code != 200 || err != nil || f.Workout.Name != "Core Sets" || len(f.Workout.Steps) != 4 || f.Workout.Description != "Core first." ||
		f.Workout.Steps[0].Exercise.Title != "Plank" || f.Workout.Steps[0].Notes != "Hips level" || f.Workout.Steps[3].WeightKg != 12.5 {
		t.Fatalf("the file: %d %+v %v", code, f, err)
	}

	// Edited in place, guarded by the tag.
	edit := strengthBody("Core Sets")
	step(edit, 2)["repeat"] = 4
	body, _ := json.Marshal(edit)
	if code, _ := a.put("/api/workouts/"+id, string(body), `"0-0"`); code != 412 {
		t.Fatalf("a stale tag: %d", code)
	}
	code, out := a.put("/api/workouts/"+id, string(body), x["tag"].(string))
	if code != 200 {
		t.Fatalf("edit: %d %s", code, out)
	}
	if y := decodeJSON[map[string]any](t, a.want(200, "GET", "/api/workouts/"+id, nil)); y["steps"].([]any)[2].(map[string]any)["repeat"] != float64(4) ||
		y["tag"] == x["tag"] {
		t.Fatalf("after the edit: %v", y)
	}

	// Duplicates take the next free number.
	d2 := decodeJSON[map[string]any](t, a.want(201, "POST", "/api/workouts/"+id+"/duplicate", nil))
	d3 := decodeJSON[map[string]any](t, a.want(201, "POST", "/api/workouts/"+id+"/duplicate", nil))
	d4 := decodeJSON[map[string]any](t, a.want(201, "POST", "/api/workouts/"+fmt.Sprint(d2["id"])+"/duplicate", nil))
	named := decodeJSON[map[string]any](t, a.want(201, "POST", "/api/workouts/"+id+"/duplicate", map[string]any{"name": "Evening core"}))
	if d2["name"] != "Core Sets 2" || d3["name"] != "Core Sets 3" || d4["name"] != "Core Sets 4" || named["name"] != "Evening core" ||
		len(d2["steps"].([]any)) != 4 {
		t.Fatalf("duplicates %v, %v, %v, %v", d2["name"], d3["name"], d4["name"], named["name"])
	}

	// Hidden from the list; ?hidden=1 lists it; shown again.
	count := func(q string) int {
		return len(decodeJSON[[]map[string]any](t, a.want(200, "GET", "/api/workouts"+q, nil)))
	}
	if h := decodeJSON[map[string]any](t, a.want(200, "POST", "/api/workouts/"+fmt.Sprint(d3["id"])+"/hide", nil)); h["hidden"] != true {
		t.Fatalf("hidden %v", h)
	}
	if count("") != 4 || count("?hidden=1") != 5 {
		t.Fatalf("listed %d, with the hidden %d", count(""), count("?hidden=1"))
	}
	a.want(200, "DELETE", "/api/workouts/"+fmt.Sprint(d3["id"])+"/hide", nil)
	if count("") != 5 {
		t.Fatalf("shown again: %d", count(""))
	}

	// Another account sees none of it.
	for _, r := range [][2]string{{"GET", ""}, {"GET", "/fit"}, {"POST", "/duplicate"}, {"POST", "/hide"}, {"DELETE", ""}} {
		if code, _ := b.do(r[0], "/api/workouts/"+id+r[1], nil); code != 404 {
			t.Errorf("another account %s %s: %d", r[0], r[1], code)
		}
	}
	if code, _ := b.put("/api/workouts/"+id, string(body), ""); code != 404 {
		t.Errorf("another account's edit: %d", code)
	}

	// Deleted: a workout made in goarmin is gone.
	a.want(204, "DELETE", "/api/workouts/"+id, nil)
	a.want(404, "GET", "/api/workouts/"+id, nil)
}
