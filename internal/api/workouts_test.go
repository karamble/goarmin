// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/internal/nodeproto"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/auth"
	"github.com/karamble/goarmin/pkg/fitid/fittest"
	"github.com/karamble/goarmin/pkg/fitwatch"
)

// A workout a node handed over is in the library; the node's inventory says whether the watch
// keeps it. Only a node that may sync the watch reports for it.
func TestWorkoutLibraryThroughANode(t *testing.T) {
	ne := newNodeEnv(t)
	ctx := context.Background()
	admin := ne.admin()
	bob, err := ne.st.EnsureUser(ctx, "bob@example.invalid", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := auth.HashPassword(password)
	if err := ne.st.SetPassword(ctx, bob.ID, hash); err != nil {
		t.Fatal(err)
	}
	const unit = 3_912_345_678
	mine, err := ne.st.EnsureDevice(ctx, store.Device{UnitID: unit, Product: 4536, Address: "C0:FF:EE:12:34:56"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ne.st.ClaimDevice(ctx, mine.ID, 1); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	w := mesgdef.NewWorkout(nil)
	w.WktName, w.Sport = "Easy Run", typedef.SportRunning
	st := mesgdef.NewWorkoutStep(nil)
	st.MessageIndex, st.DurationType, st.DurationValue, st.Intensity = 0, typedef.WktStepDurationTime, 1200000, typedef.IntensityActive
	file := fittest.Build(typedef.FileWorkout, unit, now.Add(-time.Hour), w.ToMesg(nil), st.ToMesg(nil))
	const uid = "4319872000000001-0100000000000335"

	node := ne.nodeLogin("admin@example.invalid")
	var hr nodeproto.HelloResponse
	ne.nodeDo(node.Token, http.MethodPost, "/hello", nodeproto.HelloRequest{Watches: []nodeproto.Watch{
		{Address: "C0:FF:EE:12:34:56", Unit: unit, Product: 4536}}}, &hr)
	if code := upload(ne.env, node.Token, unit, uid+".fit", file); code != 200 {
		t.Fatalf("upload: %d", code)
	}
	library := func() map[string]any {
		t.Helper()
		list := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/workouts", nil))
		if len(list) != 1 {
			t.Fatalf("library %v", list)
		}
		return list[0]
	}
	watch := func(x map[string]any) map[string]any {
		t.Helper()
		ws, _ := x["watches"].([]any)
		if len(ws) != 1 {
			t.Fatalf("watches %v", x["watches"])
		}
		return ws[0].(map[string]any)
	}
	x := library()
	if x["name"] != "Easy Run" || x["origin"] != "watch" || x["estimated_s"] != float64(1200) || watch(x)["state"] != "on" ||
		watch(x)["device_id"] != float64(mine.ID) || watch(x)["checked_at"] != nil {
		t.Fatalf("after the upload: %v", x)
	}

	inv := func(token string, items ...nodeproto.InventoryItem) int {
		t.Helper()
		return ne.nodeDo(token, http.MethodPost, "/inventory", nodeproto.InventoryRequest{Unit: unit, Types: []string{"FIT_TYPE_5"},
			At: time.Now().Truncate(time.Second), Items: append([]nodeproto.InventoryItem{}, items...)}, nil)
	}
	if code := inv(node.Token, nodeproto.InventoryItem{UID: uid, Type: "FIT_TYPE_5", Size: 400}); code != http.StatusNoContent {
		t.Fatalf("inventory: %d", code)
	}
	if x := library(); watch(x)["state"] != "on" || watch(x)["checked_at"] == nil || watch(x)["since"] == nil {
		t.Fatalf("listed: %v", watch(x))
	}
	if code := inv(node.Token); code != http.StatusNoContent {
		t.Fatalf("empty inventory: %d", code)
	}
	x = library()
	if watch(x)["state"] != "removed" {
		t.Fatalf("not listed any more: %v", watch(x))
	}
	d := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/workouts/"+fmt.Sprint(x["id"]), nil))
	if vs, _ := d["versions"].([]any); len(vs) != 1 || len(d["steps"].([]any)) != 1 || watch(d)["state"] != "removed" {
		t.Fatalf("detail %v", d)
	}

	// Renamed in goarmin: its own version, shown in the library; the original stays.
	id := fmt.Sprint(x["id"])
	r := decodeJSON[map[string]any](t, admin.want(200, "PATCH", "/api/workouts/"+id, map[string]any{"name": "  Easy   Run 30 ", "description": "Slow"}))
	if r["name"] != "Easy Run 30" || r["origin"] != "goarmin" || r["description"] != "Slow" || len(r["versions"].([]any)) != 2 ||
		watch(r)["state"] != "removed" || len(r["steps"].([]any)) != 1 {
		t.Fatalf("renamed: %v", r)
	}
	if x := library(); x["name"] != "Easy Run 30" || x["id"] != r["id"] {
		t.Fatalf("the library after the rename: %v", x)
	}
	admin.want(400, "PATCH", "/api/workouts/"+id, map[string]any{"name": "   "})
	admin.want(400, "PATCH", "/api/workouts/"+id, map[string]any{"name": strings.Repeat("x", 41)})
	admin.want(400, "PATCH", "/api/workouts/"+id, map[string]any{})
	admin.want(404, "PATCH", "/api/workouts/999999", map[string]any{"name": "x"})
	admin.want(404, "DELETE", "/api/workouts/"+id, nil) // the watch's original
	admin.want(204, "DELETE", "/api/workouts/"+fmt.Sprint(r["id"]), nil)
	if x := library(); x["name"] != "Easy Run" || x["origin"] != "watch" {
		t.Fatalf("the edit undone: %v", x)
	}

	// Bob's node may not report for the admin's watch; an inventory needs its types and time.
	bobNode := ne.nodeLogin("bob@example.invalid")
	if code := inv(bobNode.Token); code != http.StatusForbidden {
		t.Fatalf("another account's node: %d", code)
	}
	if code := ne.nodeDo(node.Token, http.MethodPost, "/inventory", nodeproto.InventoryRequest{Unit: unit}, nil); code != http.StatusBadRequest {
		t.Fatalf("an inventory without types: %d", code)
	}
}

// TestWorkoutEditor makes a workout, edits it, downloads it as FIT and deletes it; another
// account sees none of it.
func TestWorkoutEditor(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	b := e.second(a)
	sports := decodeJSON[[]map[string]any](t, a.want(200, "GET", "/api/workout-sports", nil))
	if len(sports) < 10 || sports[0]["sport"] != "running" || sports[0]["label"] != "Running" {
		t.Fatalf("sports %v", sports)
	}
	body := map[string]any{"name": "  Core   30 ", "sport": "training", "sub_sport": "strength_training", "description": "Mat work",
		"steps": []map[string]any{
			{"index": 0, "intensity": "warmup", "duration": "time", "duration_value": 300},
			{"index": 1, "intensity": "active", "duration": "reps", "duration_value": 12, "exercise": map[string]any{"category": "plank", "number": 43},
				"weight_kg": 5, "notes": "Hips level"},
			{"index": 2, "intensity": "rest", "duration": "time", "duration_value": 30},
			{"index": 3, "duration": "repeat", "from": 1, "repeat": 3},
			{"index": 4, "intensity": "cooldown", "duration": "open"},
		}}
	x := decodeJSON[map[string]any](t, a.want(201, "POST", "/api/workouts", body))
	steps, _ := x["steps"].([]any)
	if x["name"] != "Core 30" || x["origin"] != "goarmin" || x["made_here"] != true || x["description"] != "Mat work" || len(steps) != 5 ||
		x["estimated_s"] != float64(300+30*3) {
		t.Fatalf("made %v", x)
	}
	ex := steps[1].(map[string]any)["exercise"].(map[string]any)
	if ex["title"] == "" || ex["name"] != ex["title"] || ex["category"] != "plank" {
		t.Fatalf("the exercise as stored: %v", ex)
	}
	id := fmt.Sprint(x["id"])
	a.want(409, "POST", "/api/workouts", body) // the name is taken
	for name, change := range map[string]func(map[string]any){
		"an unknown sport": func(m map[string]any) { m["sport"] = "quidditch" },
		"swimming":         func(m map[string]any) { m["sport"], m["sub_sport"] = "swimming", "lap_swimming" },
		"no steps":         func(m map[string]any) { m["steps"] = []any{} },
		"an unknown exercise": func(m map[string]any) {
			m["steps"] = []map[string]any{{"index": 0, "duration": "reps", "duration_value": 5, "exercise": map[string]any{"category": "plank", "number": 9999}}}
		},
		"a target without one": func(m map[string]any) {
			m["steps"] = []map[string]any{{"index": 0, "duration": "time", "duration_value": 60, "target": "heart_rate"}}
		},
		"a long description": func(m map[string]any) { m["description"] = strings.Repeat("d", MaxWorkoutDescription+1) },
	} {
		m := map[string]any{"name": "Other " + name, "sport": "training", "sub_sport": "strength_training", "steps": body["steps"]}
		change(m)
		if code, out := a.do("POST", "/api/workouts", m); code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, code, out)
		}
	}

	// Edited: the same entry, goarmin's version replaced.
	body["name"], body["steps"] = "Core 30 hard", append(body["steps"].([]map[string]any)[:4], map[string]any{"index": 4, "intensity": "cooldown", "duration": "time", "duration_value": 120})
	y := decodeJSON[map[string]any](t, a.want(200, "PUT", "/api/workouts/"+id, body))
	if y["id"] != x["id"] || y["name"] != "Core 30 hard" || len(y["versions"].([]any)) != 1 {
		t.Fatalf("edited %v", y)
	}
	if list := decodeJSON[[]map[string]any](t, a.want(200, "GET", "/api/workouts", nil)); len(list) != 1 {
		t.Fatalf("library %v", list)
	}

	// The FIT file reads back as the workout.
	fit := a.want(200, "GET", "/api/workouts/"+id+"/fit", nil)
	f, err := fitwatch.Decode(fit)
	if err != nil || f.Workout == nil || f.Workout.Name != "Core 30 hard" || len(f.Workout.Steps) != 5 || f.Workout.Steps[1].Exercise == nil ||
		f.Workout.Steps[1].Exercise.Number != 43 || f.Workout.Steps[1].WeightKg != 5 {
		t.Fatalf("the FIT file: %+v, %v", f, err)
	}

	// Another account: not found.
	b.want(404, "GET", "/api/workouts/"+id+"/fit", nil)
	b.want(404, "PUT", "/api/workouts/"+id, body)
	b.want(404, "DELETE", "/api/workouts/"+id, nil)
	if list := decodeJSON[[]map[string]any](t, b.want(200, "GET", "/api/workouts", nil)); len(list) != 0 {
		t.Fatalf("another account's library %v", list)
	}

	a.want(204, "DELETE", "/api/workouts/"+id, nil)
	a.want(404, "GET", "/api/workouts/"+id, nil)
}
