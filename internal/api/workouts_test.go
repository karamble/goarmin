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

// TestEditedExtra: an edited original keeps its workout message's own fields and its titles'
// order, written as Garmin Connect writes today; its stated total only while the steps take as
// long as before.
func TestEditedExtra(t *testing.T) {
	one, total, sw := uint8(1), uint32(1_800_000), uint16(2517)
	old := fitwatch.Workout{EstimatedS: 1800, Steps: []fitwatch.Step{{Index: 0, Duration: "time", DurationValue: 60}},
		Extra: &fitwatch.WorkoutExtra{Software: &sw, NoDescriptionMemo: true, NoSecondary: true, NoTitles: true, F9: &one, F10: &total, F21: &total,
			F16: make([]byte, 16), Titles: [][2]uint16{{19, 43}}}}
	same := fitwatch.Workout{Steps: []fitwatch.Step{{Index: 0, Duration: "time", DurationValue: 60, Notes: "slow"}}}
	x := editedExtra(old, same)
	if x == nil || x.F9 == nil || *x.F9 != 1 || x.F21 == nil || *x.F21 != total || len(x.Titles) != 1 {
		t.Fatalf("the same timing: %+v", x)
	}
	if x.Software != nil || x.NoDescriptionMemo || x.NoSecondary || x.NoTitles {
		t.Errorf("an edit is written as Garmin Connect writes today: %+v", x)
	}
	if old.Extra.Software == nil || !old.Extra.NoTitles {
		t.Error("the original's fields were changed")
	}
	longer := fitwatch.Workout{Steps: []fitwatch.Step{{Index: 0, Duration: "time", DurationValue: 90}}}
	if x := editedExtra(old, longer); x == nil || x.F10 != nil || x.F21 != nil || x.F9 == nil {
		t.Errorf("longer steps: %+v", x)
	}
	if x := editedExtra(fitwatch.Workout{}, longer); x != nil {
		t.Errorf("nothing kept: %+v", x)
	}
}
