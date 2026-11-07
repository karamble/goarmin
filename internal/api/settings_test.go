// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"net/http"
	"strings"
	"testing"
)

// put sends a raw JSON setting with an optional If-Match revision.
func (c *client) put(path, body, ifMatch string) (int, []byte) {
	c.t.Helper()
	req, _ := http.NewRequest("PUT", c.e.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", c.origin)
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	b := make([]byte, 4096)
	n, _ := resp.Body.Read(b)
	return resp.StatusCode, b[:n]
}

func TestSettings(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	const layout = `{"v":1,"cards":[{"id":"a1","type":"sleep","size":"M","options":{}}]}`
	if got := decodeJSON[settingJSON](t, a.want(200, "GET", "/api/me/settings/glance.layout", nil)); string(got.Value) != "null" || got.Rev != 0 {
		t.Fatalf("unset setting %+v", got)
	}
	a.want(404, "GET", "/api/me/settings/anything", nil)
	if code, _ := a.put("/api/me/settings/anything", `{}`, ""); code != 404 {
		t.Fatalf("unknown key: %d", code)
	}
	for _, bad := range []string{`[]`, `{"cards":[]}`, `{"v":1}`, `not json`} {
		if code, _ := a.put("/api/me/settings/glance.layout", bad, ""); code != 400 {
			t.Fatalf("bad layout %s: %d", bad, code)
		}
	}
	if code, _ := a.put("/api/me/settings/prefs", `{"x":"`+strings.Repeat("y", 5000)+`"}`, ""); code != 413 {
		t.Fatalf("oversized prefs: %d", code)
	}
	// Preferences are checked field by field.
	for _, bad := range []string{`{}`, `{"v":1,"colour":"red"}`, `{"v":1,"week_start":"friday"}`, `{"v":1,"clock":"36h"}`,
		`{"v":1,"intensity_goal":5}`, `{"v":1,"zones":{"source":"custom"}}`, `{"v":1,"zones":{"source":"custom","max_hr":300}}`,
		`{"v":1,"zones":{"source":"custom","max_hr":190,"edges":[100,90,140,160,180]}}`, `{"v":1,"zones":{"source":"custom","max_hr":190,"edges":[100,120]}}`,
		`{"v":1,"zones":{"source":"mine"}}`} {
		if code, _ := a.put("/api/me/settings/prefs", bad, ""); code != 400 {
			t.Fatalf("bad prefs %s: %d", bad, code)
		}
	}
	for _, good := range []string{`{"v":1}`, `{"v":1,"week_start":"sunday","clock":"12h","intensity_goal":200}`,
		`{"v":1,"zones":{"source":"custom","max_hr":190,"edges":[95,114,133,152,171]},"onboarding_done":true}`, `{"v":1,"zones":{"source":"watch"}}`} {
		if code, body := a.put("/api/me/settings/prefs", good, ""); code != 200 {
			t.Fatalf("good prefs %s: %d %s", good, code, body)
		}
	}
	// First write from rev 0, then a stale writer is refused.
	if code, body := a.put("/api/me/settings/glance.layout", layout, `"0"`); code != 200 || !strings.Contains(string(body), `"rev":1`) {
		t.Fatalf("first write: %d %s", code, body)
	}
	if code, _ := a.put("/api/me/settings/glance.layout", layout, `"0"`); code != 412 {
		t.Fatalf("stale write: %d", code)
	}
	if code, _ := a.put("/api/me/settings/glance.layout", layout, `"1"`); code != 200 {
		t.Fatalf("second write: %d", code)
	}
	all := decodeJSON[map[string]settingJSON](t, a.want(200, "GET", "/api/me/settings", nil))
	if all["glance.layout"].Rev != 2 || !strings.Contains(string(all["glance.layout"].Value), `"sleep"`) {
		t.Fatalf("all settings %+v", all)
	}

	// Another account has its own (empty) settings.
	inv := decodeJSON[map[string]any](t, a.want(201, "POST", "/api/admin/invites", map[string]string{}))
	b := e.client()
	b.want(201, "POST", "/api/invites/"+strings.TrimPrefix(inv["url"].(string), public+"/invite/")+"/accept",
		map[string]string{"email": "b@example.invalid", "password": password})
	if got := decodeJSON[map[string]settingJSON](t, b.want(200, "GET", "/api/me/settings", nil)); len(got) != 0 {
		t.Fatalf("b sees settings %+v", got)
	}

	a.want(204, "DELETE", "/api/me/settings/glance.layout", nil)
	if got := decodeJSON[settingJSON](t, a.want(200, "GET", "/api/me/settings/glance.layout", nil)); got.Rev != 0 {
		t.Fatalf("after delete %+v", got)
	}
}

func TestPatchMe(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	me := decodeJSON[map[string]any](t, a.want(200, "PATCH", "/api/me",
		map[string]string{"display_name": "  Runner  ", "timezone": "Europe/Berlin", "units": "imperial"}))
	if me["display_name"] != "Runner" || me["timezone"] != "Europe/Berlin" || me["units"] != "imperial" {
		t.Fatalf("me %v", me)
	}
	if srv := me["server"].(map[string]any); srv["sync"] != false || srv["tiles"] != false {
		t.Fatalf("server %v", srv)
	}
	a.want(400, "PATCH", "/api/me", map[string]string{"timezone": "Mars/Olympus"})
	a.want(400, "PATCH", "/api/me", map[string]string{"units": "furlongs"})
	a.want(400, "PATCH", "/api/me", map[string]string{"display_name": strings.Repeat("x", 101)})
	a.want(200, "PATCH", "/api/me", map[string]string{"timezone": ""})
}
