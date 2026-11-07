// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"

	"github.com/karamble/goarmin/internal/ingest"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/auth"
	"github.com/karamble/goarmin/pkg/blobstore"
	"github.com/karamble/goarmin/pkg/fitid/fittest"
)

const public = "http://localhost:8080"

type env struct {
	t     *testing.T
	srv   *httptest.Server
	st    *store.Store
	in    *ingest.Ingester
	setup string
}

func newEnv(t *testing.T) *env { return newEnvWith(t, nil) }

func newEnvWith(t *testing.T, node Node) *env { return newEnvOpts(t, Options{Node: node}) }

func newEnvOpts(t *testing.T, opt Options) *env {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(dir, "goarmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := blobstore.Open(filepath.Join(dir, "raw"))
	if err != nil {
		t.Fatal(err)
	}
	token, _ := auth.NewToken()
	opt.Store, opt.Blobs, opt.PublicURL, opt.SetupToken = st, blobs, public, token
	s, err := New(opt)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, st: st, in: &ingest.Ingester{Store: st, Blobs: blobs}, setup: token}
}

type client struct {
	t      *testing.T
	e      *env
	hc     *http.Client
	origin string // sent with writes, like a browser
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: e.t, e: e, hc: &http.Client{Jar: jar}, origin: public}
}

// do sends a request; body is JSON-encoded unless it is []byte.
func (c *client) do(method, path string, body any) (int, []byte) {
	c.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	default:
		j, _ := json.Marshal(b)
		rd = bytes.NewReader(j)
	}
	req, err := http.NewRequest(method, c.e.srv.URL+path, rd)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if method != http.MethodGet && c.origin != "" {
		req.Header.Set("Origin", c.origin)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func (c *client) want(status int, method, path string, body any) []byte {
	c.t.Helper()
	got, out := c.do(method, path, body)
	if got != status {
		c.t.Fatalf("%s %s: %d %s, want %d", method, path, got, out, status)
	}
	return out
}

func decodeJSON[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return v
}

const password = "a long enough password"

// admin runs the first-run setup and returns the signed-in admin.
func (e *env) admin() *client {
	c := e.client()
	c.want(201, "POST", "/api/setup", map[string]string{"token": e.setup, "email": "admin@example.invalid", "name": "Admin", "password": password})
	return c
}

func TestSetupLoginLogout(t *testing.T) {
	e := newEnv(t)
	c := e.client()
	if v := decodeJSON[map[string]bool](t, c.want(200, "GET", "/api/setup", nil)); !v["needed"] {
		t.Fatal("setup not offered on an empty database")
	}
	c.want(401, "GET", "/api/me", nil)
	c.want(403, "POST", "/api/setup", map[string]string{"token": "wrong", "email": "a@example.invalid", "password": password})
	c.want(400, "POST", "/api/setup", map[string]string{"token": e.setup, "email": "a@example.invalid", "password": "short"})
	admin := e.admin()
	me := decodeJSON[userJSON](t, admin.want(200, "GET", "/api/me", nil))
	if !me.IsAdmin || me.Email != "admin@example.invalid" {
		t.Fatalf("me %+v", me)
	}
	e.client().want(409, "POST", "/api/setup", map[string]string{"token": e.setup, "email": "b@example.invalid", "password": password})

	admin.want(204, "POST", "/api/auth/logout", nil)
	admin.want(401, "GET", "/api/me", nil)
	admin.want(401, "POST", "/api/auth/login", map[string]string{"email": "admin@example.invalid", "password": "not the password!"})
	admin.want(401, "POST", "/api/auth/login", map[string]string{"email": "nobody@example.invalid", "password": password})
	admin.want(200, "POST", "/api/auth/login", map[string]string{"email": "ADMIN@example.invalid", "password": password})
	admin.want(200, "GET", "/api/me", nil)

	// Sessions: two browsers, one ends the other.
	other := e.client()
	other.want(200, "POST", "/api/auth/login", map[string]string{"email": "admin@example.invalid", "password": password})
	list := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/me/sessions", nil))
	if len(list) != 2 {
		t.Fatalf("%d sessions", len(list))
	}
	for _, s := range list {
		if s["current"] == false {
			admin.want(204, "DELETE", "/api/me/sessions/"+s["id"].(string), nil)
		}
	}
	other.want(401, "GET", "/api/me", nil)
	admin.want(200, "GET", "/api/me", nil)
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t)
	e.admin()
	c := e.client()
	for range 10 {
		c.want(401, "POST", "/api/auth/login", map[string]string{"email": "admin@example.invalid", "password": "wrong password here"})
	}
	c.want(429, "POST", "/api/auth/login", map[string]string{"email": "admin@example.invalid", "password": password})
}

func TestCrossSiteWritesRefused(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	admin.origin = "https://evil.example"
	admin.want(403, "POST", "/api/me/password", map[string]string{"current": password, "new": "another long password"})
	admin.want(403, "POST", "/api/admin/invites", map[string]string{})
	admin.origin = public
	admin.want(204, "POST", "/api/me/password", map[string]string{"current": password, "new": "another long password"})
	admin.want(403, "POST", "/api/me/password", map[string]string{"current": "wrong", "new": "yet another long one"})
}

func TestInvites(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	inv := decodeJSON[map[string]any](t, admin.want(201, "POST", "/api/admin/invites", map[string]string{"email": "b@example.invalid"}))
	url := inv["url"].(string)
	if !strings.HasPrefix(url, public+"/invite/") {
		t.Fatalf("invite url %q", url)
	}
	token := strings.TrimPrefix(url, public+"/invite/")
	b := e.client()
	b.want(200, "GET", "/api/invites/"+token, nil)
	b.want(404, "POST", "/api/invites/"+token+"/accept", map[string]string{"email": "c@example.invalid", "password": password})
	u := decodeJSON[userJSON](t, b.want(201, "POST", "/api/invites/"+token+"/accept",
		map[string]string{"email": "b@example.invalid", "name": "B", "password": password}))
	if u.IsAdmin {
		t.Fatal("an invited user became admin")
	}
	b.want(200, "GET", "/api/me", nil)
	b.want(403, "GET", "/api/admin/users", nil)
	b.want(403, "POST", "/api/admin/invites", map[string]string{})
	e.client().want(404, "POST", "/api/invites/"+token+"/accept", map[string]string{"email": "b@example.invalid", "password": password})
	users := decodeJSON[[]userJSON](t, admin.want(200, "GET", "/api/admin/users", nil))
	if len(users) != 2 {
		t.Fatalf("%d users", len(users))
	}
}

func TestPasskeys(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	key := newSoftKey(t, "localhost", public)
	opts := admin.want(200, "POST", "/api/me/passkeys/begin", map[string]string{"name": "laptop"})
	admin.want(201, "POST", "/api/me/passkeys/finish", key.register(t, opts))
	list := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/me/passkeys", nil))
	if len(list) != 1 || list[0]["name"] != "laptop" {
		t.Fatalf("passkeys %v", list)
	}
	// A finish without its begin is refused.
	admin.want(400, "POST", "/api/me/passkeys/finish", key.register(t, opts))

	// Sign in with the passkey alone, twice (the counter rises).
	for range 2 {
		c := e.client()
		opts := c.want(200, "POST", "/api/auth/passkey/login/begin", nil)
		me := decodeJSON[userJSON](t, c.want(200, "POST", "/api/auth/passkey/login/finish", key.login(t, opts)))
		if me.Email != "admin@example.invalid" {
			t.Fatalf("signed in as %+v", me)
		}
		c.want(200, "GET", "/api/me", nil)
	}
	// Another key the server never saw.
	stranger := newSoftKey(t, "localhost", public)
	stranger.user = key.user
	c := e.client()
	opts = c.want(200, "POST", "/api/auth/passkey/login/begin", nil)
	c.want(401, "POST", "/api/auth/passkey/login/finish", stranger.login(t, opts))
	// A key for another site.
	phish := newSoftKey(t, "localhost", "https://evil.example")
	phish.id, phish.key, phish.user = key.id, key.key, key.user
	opts = c.want(200, "POST", "/api/auth/passkey/login/begin", nil)
	c.want(401, "POST", "/api/auth/passkey/login/finish", phish.login(t, opts))

	admin.want(204, "DELETE", "/api/me/passkeys/"+list[0]["id"].(string), nil)
}

// TestLastPasskeyKept: an account whose only way in is one passkey cannot delete it.
func TestLastPasskeyKept(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	key := newSoftKey(t, "localhost", public)
	opts := admin.want(200, "POST", "/api/me/passkeys/begin", map[string]string{})
	admin.want(201, "POST", "/api/me/passkeys/finish", key.register(t, opts))
	me := decodeJSON[userJSON](t, admin.want(200, "GET", "/api/me", nil))
	if err := e.st.SetPassword(context.Background(), me.ID, ""); err != nil {
		t.Fatal(err)
	}
	list := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/me/passkeys", nil))
	admin.want(409, "DELETE", "/api/me/passkeys/"+list[0]["id"].(string), nil)
}

// healthData gives the user a day of monitoring data and an activity, three hours back, and
// returns that local day (yesterday just after midnight).
func (e *env) healthData(email string) string {
	t := e.t
	ctx := context.Background()
	u, err := e.st.EnsureUser(ctx, email, "")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-3 * time.Hour).Truncate(time.Minute)
	serial := uint32(3_500_000_000 + u.ID)
	ft := func(t time.Time) proto.Value { return proto.Uint32(fittest.FITTime(t)) }
	var msgs []proto.Message
	for i := range 6 {
		at := base.Add(time.Duration(i) * 10 * time.Minute)
		msgs = append(msgs, fittest.Message(55, fittest.F{Num: 253, Value: ft(at)}, fittest.F{Num: 27, Value: proto.Uint8(uint8(60 + i))},
			fittest.F{Num: 5, Value: proto.Uint8(6)}, fittest.F{Num: 3, Value: proto.Uint32(uint32(100 * (i + 1)))}))
	}
	mon := fittest.Build(typedef.FileMonitoringB, serial, base.Add(time.Hour), msgs...)
	var amsgs []proto.Message
	for k := range 30 {
		r := mesgdef.NewRecord(nil)
		r.Timestamp = base.Add(time.Duration(k) * time.Second)
		r.PositionLat, r.PositionLong = int32((50+float64(k)*1e-4)*(1<<31)/180), int32(7*(1<<31)/180)
		r.HeartRate = 120
		amsgs = append(amsgs, r.ToMesg(nil))
	}
	s := mesgdef.NewSession(nil)
	s.StartTime, s.Timestamp, s.Sport, s.TotalTimerTime, s.TotalElapsedTime = base, base.Add(30*time.Second), typedef.SportRunning, 30000, 30000
	amsgs = append(amsgs, s.ToMesg(nil))
	act := fittest.Build(typedef.FileActivity, serial, base.Add(time.Minute), amsgs...)
	// The watch's own files: a workout, a course, totals, goals and settings.
	wk := mesgdef.NewWorkout(nil)
	wk.WktName, wk.Sport = "Easy run", typedef.SportRunning
	step := mesgdef.NewWorkoutStep(nil)
	step.MessageIndex, step.DurationType, step.DurationValue = 0, typedef.WktStepDurationTime, 600000
	cs := mesgdef.NewCourse(nil)
	cs.Name, cs.Sport = "Loop", typedef.SportRunning
	cmsgs := []proto.Message{cs.ToMesg(nil)}
	for k := range 10 {
		r := mesgdef.NewRecord(nil)
		r.Timestamp, r.Distance = base.Add(time.Duration(k)*time.Second), uint32(k*1000)
		r.PositionLat, r.PositionLong = int32((50+float64(k)*1e-4)*(1<<31)/180), int32(7*(1<<31)/180)
		cmsgs = append(cmsgs, r.ToMesg(nil))
	}
	tot := mesgdef.NewTotals(nil)
	tot.Sport, tot.TimerTime, tot.Distance, tot.Calories, tot.Sessions = typedef.SportRunning, 3600, 10000, 700, 2
	goal := mesgdef.NewGoal(nil)
	goal.Type, goal.TargetValue = typedef.GoalActiveMinutes, 150
	dev := mesgdef.NewDeviceSettings(nil)
	dev.TimeMode = []typedef.TimeMode{typedef.TimeModeHour24}
	files := map[string][]byte{
		"M.fit": mon, "A.fit": act,
		"W.fit": fittest.Build(typedef.FileWorkout, serial, base, wk.ToMesg(nil), step.ToMesg(nil)),
		"C.fit": fittest.Build(typedef.FileCourse, serial, base, cmsgs...),
		"T.fit": fittest.Build(typedef.FileTotals, serial, base, tot.ToMesg(nil)),
		"G.fit": fittest.Build(typedef.FileGoals, serial, base, goal.ToMesg(nil)),
		"S.fit": fittest.Build(typedef.FileSettings, serial, base, dev.ToMesg(nil)),
	}
	dir := t.TempDir()
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if rep, err := e.in.Import(ctx, u, store.SourceUSB, []string{dir}); err != nil || rep.Ingested != len(files) {
		t.Fatalf("import for %s: %+v, %v", email, rep, err)
	}
	return base.In(time.Local).Format(time.DateOnly)
}

// TestIsolation: every data route answers with the signed-in user's own data only.
func TestIsolation(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	day := e.healthData("admin@example.invalid")
	inv := decodeJSON[map[string]any](t, admin.want(201, "POST", "/api/admin/invites", map[string]string{}))
	token := strings.TrimPrefix(inv["url"].(string), public+"/invite/")
	b := e.client()
	b.want(201, "POST", "/api/invites/"+token+"/accept", map[string]string{"email": "b@example.invalid", "password": password})

	acts := decodeJSON[map[string][]map[string]any](t, admin.want(200, "GET", "/api/activities", nil))["items"]
	if len(acts) != 1 {
		t.Fatalf("admin has %d activities", len(acts))
	}
	id := fmt.Sprint(acts[0]["id"])
	admin.want(200, "GET", "/api/activities/"+id, nil)
	admin.want(200, "GET", "/api/activities/"+id+"/streams", nil)
	if fit := admin.want(200, "GET", "/api/activities/"+id+"/fit", nil); string(fit[8:12]) != ".FIT" {
		t.Fatal("the FIT download is not a FIT file")
	}
	aday := acts[0]["day"].(string)
	if got := decodeJSON[map[string][]map[string]any](t, admin.want(200, "GET", "/api/activities?from_day="+aday+"&to_day="+aday, nil))["items"]; len(got) != 1 {
		t.Fatalf("the activity's day lists %d activities", len(got))
	}
	yesterday := time.Now().AddDate(0, 0, -400).Format(time.DateOnly)
	if got := decodeJSON[map[string][]map[string]any](t, admin.want(200, "GET", "/api/activities?from_day="+yesterday+"&to_day="+yesterday, nil))["items"]; len(got) != 0 {
		t.Fatalf("an empty day lists %d activities", len(got))
	}
	admin.want(400, "GET", "/api/activities?from_day="+day+"&to_day="+yesterday, nil)
	nb := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/activities/"+id+"/neighbours", nil))
	if nb["prev"] != nil || nb["next"] != nil {
		t.Fatalf("the only activity has neighbours %v", nb)
	}
	if d := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/daily?from="+day+"&to="+day, nil)); len(d) == 0 || d[0]["steps"] == nil {
		t.Fatalf("admin's day %v", d)
	}
	hr := decodeJSON[seriesJSON](t, admin.want(200, "GET", "/api/series/heart_rate", nil))
	if len(hr.V) == 0 {
		t.Fatal("admin has no heart rate")
	}

	// B sees none of it.
	if got := decodeJSON[map[string][]map[string]any](t, b.want(200, "GET", "/api/activities", nil))["items"]; len(got) != 0 {
		t.Fatalf("b sees %d activities", len(got))
	}
	for _, p := range []string{"/api/activities/" + id, "/api/activities/" + id + "/streams", "/api/activities/" + id + "/fit",
		"/api/activities/" + id + "/neighbours"} {
		b.want(404, "GET", p, nil)
	}
	if got := decodeJSON[map[string][]map[string]any](t, b.want(200, "GET", "/api/activities?from_day="+aday+"&to_day="+aday, nil))["items"]; len(got) != 0 {
		t.Fatalf("b sees %d activities on the day", len(got))
	}
	if d := decodeJSON[[]map[string]any](t, b.want(200, "GET", "/api/daily?from="+day+"&to="+day, nil)); len(d) != 0 {
		t.Fatalf("b sees days %v", d)
	}
	if s := decodeJSON[seriesJSON](t, b.want(200, "GET", "/api/series/heart_rate", nil)); len(s.V) != 0 {
		t.Fatalf("b sees heart rate %v", s.V)
	}
	if d := decodeJSON[[]map[string]any](t, b.want(200, "GET", "/api/devices", nil)); len(d) != 0 {
		t.Fatalf("b sees devices %v", d)
	}

	// The watch's own files: the owner sees them, B does not.
	wks := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/workouts", nil))
	crs := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/courses", nil))
	if len(wks) != 1 || wks[0]["name"] != "Easy run" || wks[0]["estimated_s"] != float64(600) || len(crs) != 1 || crs[0]["name"] != "Loop" {
		t.Fatalf("workouts %v, courses %v", wks, crs)
	}
	wid, cid := fmt.Sprint(wks[0]["id"]), fmt.Sprint(crs[0]["id"])
	if w := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/workouts/"+wid, nil)); len(w["steps"].([]any)) != 1 {
		t.Fatalf("workout %v", w)
	}
	admin.want(200, "GET", "/api/courses/"+cid, nil)
	admin.want(200, "GET", "/api/courses/"+cid+"/streams", nil)
	if fit := admin.want(200, "GET", "/api/courses/"+cid+"/fit", nil); string(fit[8:12]) != ".FIT" {
		t.Fatal("the course download is not a FIT file")
	}
	if tot := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/totals", nil)); len(tot["sports"].([]any)) != 1 {
		t.Fatalf("totals %v", tot)
	}
	prof := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/me/watch-profile", nil))
	if prof["settings"].(map[string]any)["clock"] != "24h" || len(prof["goals"].([]any)) != 1 {
		t.Fatalf("watch profile %v", prof)
	}
	for _, p := range []string{"/api/workouts/" + wid, "/api/courses/" + cid, "/api/courses/" + cid + "/streams", "/api/courses/" + cid + "/fit"} {
		b.want(404, "GET", p, nil)
	}
	for _, p := range []string{"/api/workouts", "/api/courses"} {
		if got := decodeJSON[[]map[string]any](t, b.want(200, "GET", p, nil)); len(got) != 0 {
			t.Fatalf("b sees %s: %v", p, got)
		}
	}
	if tot := decodeJSON[map[string]any](t, b.want(200, "GET", "/api/totals", nil)); len(tot["sports"].([]any)) != 0 {
		t.Fatalf("b sees totals %v", tot)
	}
	if prof := decodeJSON[map[string]any](t, b.want(200, "GET", "/api/me/watch-profile", nil)); prof["settings"] != nil || len(prof["goals"].([]any)) != 0 {
		t.Fatalf("b sees the watch profile %v", prof)
	}
	if z := decodeJSON[map[string]any](t, b.want(200, "GET", "/api/me/zones", nil)); z["default"] != nil || len(z["profiles"].([]any)) != 0 {
		t.Fatalf("b sees zones %v", z)
	}
	// Signed out: nothing.
	anon := e.client()
	for _, p := range []string{"/api/activities", "/api/daily", "/api/series/heart_rate", "/api/devices", "/api/metrics", "/api/workouts",
		"/api/courses", "/api/totals", "/api/me/watch-profile", "/api/me/zones"} {
		anon.want(401, "GET", p, nil)
	}
	b.want(404, "GET", "/api/series/no_such_metric", nil)
	b.want(400, "GET", "/api/daily?from=2&to=1", nil)
}

// TestTiles: map tiles are served to signed-in users only, and /api/me says they exist.
func TestTiles(t *testing.T) {
	var asked []string
	e := newEnvOpts(t, Options{TilesDark: true, TilesAttribution: "© Jawg Maps · © OpenStreetMap contributors",
		Tiles: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			asked = append(asked, r.URL.Path)
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("png"))
		})})
	e.client().want(401, "GET", "/tiles/a/5/16/11.png", nil)
	if len(asked) != 0 {
		t.Fatal("a signed-out request reached the tile cache")
	}
	admin := e.admin()
	if got := admin.want(200, "GET", "/tiles/a/5/16/11.png", nil); string(got) != "png" || len(asked) != 1 || asked[0] != "/a/5/16/11.png" {
		t.Fatalf("tile %q, asked %v", got, asked)
	}
	me := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/me", nil))
	srv := me["server"].(map[string]any)
	if srv["tiles"] != true || srv["map"].(map[string]any)["dark"] != true || !strings.Contains(srv["map"].(map[string]any)["attribution"].(string), "Jawg") {
		t.Fatalf("me %v", me)
	}
}
