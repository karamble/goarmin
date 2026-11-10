// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"context"
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

// TestDailyFloors: a day's climb is served as whole floors with the goal the watch keeps (its
// ascent goal in metres); a day's own goal (a Garmin Connect export's) wins; the calendar shows
// floors as a kind of its own, which preferences may hide.
func TestDailyFloors(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	ctx := context.Background()
	u, err := e.st.UserByEmail(ctx, "admin@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	// Two days back at ten, well inside one local day: running totals of the climb (mm).
	now := time.Now()
	y, m, d := now.AddDate(0, 0, -2).Date()
	base := time.Date(y, m, d, 10, 0, 0, 0, time.Local)
	serial := uint32(3_500_000_000 + u.ID)
	ft := func(t time.Time) proto.Value { return proto.Uint32(fittest.FITTime(t)) }
	var msgs []proto.Message
	for i, up := range []uint32{3048, 12192, 34412} {
		at := base.Add(time.Duration(i) * 15 * time.Minute)
		msgs = append(msgs, fittest.Message(55, fittest.F{Num: 253, Value: ft(at)}, fittest.F{Num: 27, Value: proto.Uint8(70)},
			fittest.F{Num: 5, Value: proto.Uint8(6)}, fittest.F{Num: 3, Value: proto.Uint32(uint32(500 * (i + 1)))},
			fittest.F{Num: 35, Value: proto.Uint32(up)}, fittest.F{Num: 36, Value: proto.Uint32(uint32(2600 * (i + 1)))}))
	}
	goal := mesgdef.NewGoal(nil)
	goal.Type, goal.TargetValue = typedef.GoalAscent, 30000 // 30 m: the Fenix 8's 10 floors
	files := map[string][]byte{
		"M.fit": fittest.Build(typedef.FileMonitoringB, serial, base.Add(time.Hour), msgs...),
		"G.fit": fittest.Build(typedef.FileGoals, serial, base, goal.ToMesg(nil)),
	}
	dir := t.TempDir()
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if rep, err := e.in.Import(ctx, u, store.SourceUSB, []string{dir}); err != nil || rep.Ingested != len(files) {
		t.Fatalf("import: %+v, %v", rep, err)
	}
	from, to := now.AddDate(0, 0, -4).Format(time.DateOnly), now.Format(time.DateOnly)
	climbed := func() map[string]any {
		t.Helper()
		var hit map[string]any
		for _, x := range decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/daily?from="+from+"&to="+to, nil)) {
			if x["floors_up"] != nil {
				if hit != nil {
					t.Fatalf("two days with floors: %v and %v", hit, x)
				}
				hit = x
			}
		}
		if hit == nil {
			t.Fatal("no day has floors")
		}
		return hit
	}
	day := climbed()
	if day["floors_up"] != float64(11) || day["floors_down"] != float64(2) || day["floors_goal"] != float64(10) {
		t.Fatalf("34.4 m up, 7.8 m down, a 30 m goal: %v", day)
	}

	// The day's own goal from an export wins over the watch's.
	f, _, err := e.st.AddRawFile(ctx, store.RawFile{SHA256: strings.Repeat("f1", 32), Size: 1, UserID: u.ID, Source: store.SourceUSB, SourceName: "uds"})
	if err != nil {
		t.Fatal(err)
	}
	dd := &wellness.Data{Daily: []wellness.Daily{{Kind: wellness.FloorsGoal, At: base, Day: day["day"].(string), Value: 12}}}
	if _, err := e.st.WriteFileData(ctx, f, dd, nil, "fit", 8); err != nil {
		t.Fatal(err)
	}
	if got := climbed()["floors_goal"]; got != float64(12) {
		t.Fatalf("the day's goal: %v", got)
	}

	cal := decodeJSON[struct {
		Days []map[string]any `json:"days"`
	}](t, admin.want(200, "GET", "/api/calendar?from="+from+"&to="+to, nil))
	found := false
	for _, x := range cal.Days {
		if x["day"] == day["day"] {
			found = x["kinds"].(map[string]any)["floors"] == float64(1)
		}
	}
	if !found {
		t.Fatalf("the calendar has no floors on %v: %v", day["day"], cal.Days)
	}
	admin.want(200, "PUT", "/api/me/settings/prefs", map[string]any{"v": 1, "calendar_hidden": []string{"floors"}})
}
