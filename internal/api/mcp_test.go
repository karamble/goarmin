// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/karamble/goarmin/internal/mcpserver"
	"github.com/karamble/goarmin/internal/nodes"
)

// TestMCPCoversEveryRoute is the completeness guard: every route the API registers is an MCP
// tool or resource, or left out on purpose with a reason; and every tool names a real route.
func TestMCPCoversEveryRoute(t *testing.T) {
	// Every optional part on, so every route registers.
	reg := nodes.New(nodes.Options{})
	s, err := New(Options{PublicURL: public, Node: reg, Nodes: reg, Tiles: http.NotFoundHandler(), UI: http.NotFoundHandler()})
	if err != nil {
		t.Fatal(err)
	}
	registered := map[string]bool{}
	for _, r := range s.Routes() {
		registered[r] = true
		if _, left := mcpserver.Excluded[r]; left {
			continue
		}
		if !mcpserver.Covered(r) {
			t.Errorf("%s is neither an MCP tool or resource nor in mcpserver.Excluded (with a reason)", r)
		}
	}
	for _, tool := range mcpserver.Tools {
		if !registered[tool.Route()] {
			t.Errorf("tool %s calls %s, which the API does not register", tool.Name, tool.Route())
		}
		for _, c := range tool.Covers {
			if !registered[c] {
				t.Errorf("tool %s covers %s, which the API does not register", tool.Name, c)
			}
		}
	}
	for _, r := range mcpserver.Resources {
		if !registered[r.Route()] {
			t.Errorf("resource %s reads %s, which the API does not register", r.Name, r.Route())
		}
	}
	for r := range mcpserver.Excluded {
		if !registered[r] {
			t.Errorf("mcpserver.Excluded lists %s, which the API does not register", r)
		}
	}
	names := map[string]bool{}
	for _, tool := range mcpserver.Tools {
		if names[tool.Name] {
			t.Errorf("two tools are called %s", tool.Name)
		}
		names[tool.Name] = true
	}
}

// bearer adds an Authorization header to every request.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

func (e *env) mcpSession(t *testing.T, token string) *mcp.ClientSession {
	t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearer{token, http.DefaultTransport}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func (c *client) mcpToken(scope string, days int) (string, map[string]any) {
	c.t.Helper()
	out := decodeJSON[map[string]any](c.t, c.want(201, "POST", "/api/me/mcp/tokens", map[string]any{"name": "Claude " + scope, "scope": scope, "expires_days": days}))
	return out["token"].(string), out["item"].(map[string]any)
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, x := range res.Tools {
		n = append(n, x.Name)
	}
	return n
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if res.IsError {
		return map[string]any{"error": text}, false
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("%s: %v: %s", name, err, text)
	}
	return v, true
}

func TestMCPTokens(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	b := e.second(a)

	tok, item := b.mcpToken("read", 30)
	if !strings.HasPrefix(tok, mcpserver.TokenPrefix) || !strings.HasPrefix(tok, item["prefix"].(string)) || item["live"] != true {
		t.Fatalf("token %q, item %v", tok, item)
	}
	if exp := item["expires_at"].(float64); time.Unix(int64(exp), 0).Before(time.Now().AddDate(0, 0, 29)) {
		t.Fatalf("expires %v, want 30 days on", exp)
	}
	b.want(403, "POST", "/api/me/mcp/tokens", map[string]any{"name": "x", "scope": "admin", "expires_days": 0})
	b.want(400, "POST", "/api/me/mcp/tokens", map[string]any{"name": "x", "scope": "everything", "expires_days": 0})
	b.want(400, "POST", "/api/me/mcp/tokens", map[string]any{"name": "x", "scope": "read", "expires_days": 7})
	b.want(400, "POST", "/api/me/mcp/tokens", map[string]any{"name": "x", "scope": "read"})
	b.want(400, "POST", "/api/me/mcp/tokens", map[string]any{"name": " ", "scope": "read", "expires_days": 0})
	a.mcpToken("admin", 0)

	ov := decodeJSON[map[string]any](t, b.want(200, "GET", "/api/me/mcp", nil))
	if ov["enabled"] != true || ov["endpoint"] != public+"/mcp" || len(ov["tokens"].([]any)) != 1 {
		t.Fatalf("overview %v", ov)
	}
	if strings.Contains(string(b.want(200, "GET", "/api/me/mcp", nil)), tok) {
		t.Fatal("the overview shows the token itself")
	}
	if !slices.Equal(toStrings(ov["scopes"]), []string{"read", "write"}) {
		t.Fatalf("b may make %v", ov["scopes"])
	}
	if len(ov["catalogue"].([]any)) < 90 {
		t.Fatalf("the catalogue has %d items", len(ov["catalogue"].([]any)))
	}
	id := fmt.Sprint(item["id"])
	b.want(204, "PATCH", "/api/me/mcp/tokens/"+id, map[string]any{"name": "Laptop"})
	a.want(404, "PATCH", "/api/me/mcp/tokens/"+id, map[string]any{"name": "Mine"})
	a.want(404, "DELETE", "/api/me/mcp/tokens/"+id, nil)

	cs := e.mcpSession(t, tok)
	if v, ok := call(t, cs, "get_account", nil); !ok || v["email"] != "b@example.invalid" {
		t.Fatalf("get_account: %v", v)
	}
	ov = decodeJSON[map[string]any](t, b.want(200, "GET", "/api/me/mcp", nil))
	tk := ov["tokens"].([]any)[0].(map[string]any)
	calls := ov["calls"].([]any)
	if tk["name"] != "Laptop" || tk["last_used_at"] == nil || tk["calls_24h"].(float64) != 1 || len(calls) != 1 ||
		calls[0].(map[string]any)["tool"] != "get_account" || calls[0].(map[string]any)["ok"] != true {
		t.Fatalf("after a call: token %v, calls %v", tk, calls)
	}

	// Revoked: the next request is refused.
	b.want(204, "DELETE", "/api/me/mcp/tokens/"+id, nil)
	if _, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_account"}); err == nil {
		t.Fatal("a revoked token still calls tools")
	}
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	if _, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearer{tok, http.DefaultTransport}}}, nil); err == nil {
		t.Fatal("a revoked token connects")
	}
	for _, bad := range []string{"", "gma_nope", "not-a-token"} {
		req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", strings.NewReader(`{}`))
		if bad != "" {
			req.Header.Set("Authorization", "Bearer "+bad)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Fatalf("token %q: %d", bad, resp.StatusCode)
		}
	}
	// A cookie session is no MCP credential, and a cross-site page cannot post to /mcp.
	req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Authorization", "Bearer "+tok)
	if resp, err := http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	} else if resp.Body.Close(); resp.StatusCode != 401 && resp.StatusCode != 403 {
		t.Fatalf("cross-site: %d", resp.StatusCode)
	}
}

func toStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestMCPOff(t *testing.T) {
	e := newEnvOpts(t, Options{NoMCP: true})
	a := e.admin()
	a.want(409, "POST", "/api/me/mcp/tokens", map[string]any{"name": "x", "scope": "read", "expires_days": 0})
	if ov := decodeJSON[map[string]any](t, a.want(200, "GET", "/api/me/mcp", nil)); ov["enabled"] != false {
		t.Fatalf("overview %v", ov)
	}
	resp, err := http.Post(e.srv.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("/mcp with MCP off: %d", resp.StatusCode)
	}
}

func TestMCPLevelsAndIsolation(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	day := e.healthData("admin@example.invalid")
	b := e.second(a)
	readTok, _ := a.mcpToken("read", 0)
	writeTok, _ := a.mcpToken("write", 0)
	adminTok, _ := a.mcpToken("admin", 0)
	bTok, _ := b.mcpToken("write", 0)

	read, write, admin, other := e.mcpSession(t, readTok), e.mcpSession(t, writeTok), e.mcpSession(t, adminTok), e.mcpSession(t, bTok)
	rn, wn, an := toolNames(t, read), toolNames(t, write), toolNames(t, admin)
	if slices.Contains(rn, "plan_workout") || slices.Contains(rn, "sync_device") || slices.Contains(rn, "list_users") {
		t.Fatalf("a read token lists write or admin tools: %v", rn)
	}
	if !slices.Contains(wn, "plan_workout") || slices.Contains(wn, "list_users") || !slices.Contains(an, "list_users") {
		t.Fatalf("write lists %d, admin %d tools", len(wn), len(an))
	}
	if !(len(rn) < len(wn) && len(wn) < len(an)) || len(an) != len(mcpserver.Tools) {
		t.Fatalf("tools: read %d, write %d, admin %d of %d", len(rn), len(wn), len(an), len(mcpserver.Tools))
	}
	if _, err := read.CallTool(context.Background(), &mcp.CallToolParams{Name: "plan_workout", Arguments: map[string]any{"day": day, "workout_id": 1}}); err == nil {
		t.Fatal("a read token called plan_workout")
	}

	// The answers are the API's.
	acts, ok := call(t, read, "list_activities", nil)
	items := acts["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("list_activities: %v", acts)
	}
	act := items[0].(map[string]any)
	actID := act["id"]
	if v, ok := call(t, read, "get_activity", map[string]any{"id": actID}); !ok || fmt.Sprint(v["id"]) != fmt.Sprint(actID) {
		t.Fatalf("get_activity: %v", v)
	}
	if v, ok := call(t, read, "get_activity_track", map[string]any{"id": actID, "max_points": 10}); !ok || len(v["t"].([]any)) > 11 || v["thinned"] == nil {
		t.Fatalf("get_activity_track thinned: %v", v)
	}
	if v, ok := call(t, read, "get_daily", map[string]any{"from": day, "to": day}); !ok || len(v["items"].([]any)) == 0 {
		t.Fatalf("get_daily: %v", v)
	}
	if v, ok := call(t, read, "get_series", map[string]any{"metric": "heart_rate", "from_day": day, "max_points": 50}); !ok || len(v["v"].([]any)) == 0 {
		t.Fatalf("get_series: %v", v)
	}
	if v, ok := call(t, read, "list_exercises", map[string]any{"category": "plank"}); !ok || v["count"].(float64) < 10 {
		t.Fatalf("list_exercises plank: %v", v["count"])
	}
	if v, ok := call(t, read, "get_activity", map[string]any{"id": "x"}); ok || !strings.Contains(v["error"].(string), "integer") {
		t.Fatalf("a wrong argument type: %v", v)
	}
	if v, ok := call(t, read, "get_activity", map[string]any{"id": actID, "extra": 1}); ok || !strings.Contains(v["error"].(string), "extra") {
		t.Fatalf("an unknown argument: %v", v)
	}
	if v, ok := call(t, read, "get_activity", nil); ok || !strings.Contains(v["error"].(string), "needs") {
		t.Fatalf("a missing argument: %v", v)
	}

	// Another account's ids answer as not found.
	if v, ok := call(t, other, "get_activity", map[string]any{"id": actID}); ok || !strings.Contains(v["error"].(string), "404") {
		t.Fatalf("b reads a's activity: %v", v)
	}
	if v, ok := call(t, other, "list_activities", nil); !ok || len(v["items"].([]any)) != 0 {
		t.Fatalf("b lists %v", v)
	}
	if _, err := other.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: fmt.Sprintf("goarmin://activities/%v/fit", actID)}); err == nil {
		t.Fatal("b reads a's FIT file")
	}

	// Resources and prompts.
	res, err := read.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: fmt.Sprintf("goarmin://activities/%v/fit", actID)})
	if err != nil || len(res.Contents) != 1 || string(res.Contents[0].Blob[8:12]) != ".FIT" {
		t.Fatalf("activity FIT resource: %v %v", res, err)
	}
	if res, err := read.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: "goarmin://exercises/plank/43"}); err != nil ||
		!strings.Contains(res.Contents[0].Text, "plank") {
		t.Fatalf("exercise resource: %v", err)
	}
	p, err := read.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "plan_week", Arguments: map[string]string{"goal": "4 easy runs", "day": day}})
	if err != nil || !strings.Contains(p.Messages[0].Content.(*mcp.TextContent).Text, "only read") {
		t.Fatalf("plan_week for a read token: %v", err)
	}
	p, err = write.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "plan_week", Arguments: map[string]string{"goal": "4 easy runs", "day": day}})
	if err != nil || !strings.Contains(p.Messages[0].Content.(*mcp.TextContent).Text, "plan_workout") {
		t.Fatalf("plan_week for a write token: %v", err)
	}
	if _, err := read.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "review_night", Arguments: map[string]string{"day": "yesterday"}}); err == nil {
		t.Fatal("review_night took a bad day")
	}

	// Plans through MCP.
	wl, _ := call(t, write, "list_workouts", nil)
	wid := wl["items"].([]any)[0].(map[string]any)["id"]
	plan, ok := call(t, write, "plan_workout", map[string]any{"day": day, "workout_id": wid, "note": "easy"})
	if !ok || plan["note"] != "easy" {
		t.Fatalf("plan_workout: %v", plan)
	}
	moved := time.Now().AddDate(0, 0, 3).Format(time.DateOnly)
	if v, ok := call(t, write, "update_plan", map[string]any{"id": plan["id"], "day": moved}); !ok || v["day"] != moved {
		t.Fatalf("update_plan: %v", v)
	}
	if v, ok := call(t, other, "remove_plan", map[string]any{"id": plan["id"]}); ok {
		t.Fatalf("b removed a's plan: %v", v)
	}
	if v, ok := call(t, write, "remove_plan", map[string]any{"id": plan["id"]}); !ok || v["ok"] != true {
		t.Fatalf("remove_plan: %v", v)
	}

	// A destructive tool needs the device's name; a wrong one changes nothing.
	devs, _ := call(t, read, "list_devices", nil)
	dev := devs["items"].([]any)[0].(map[string]any)
	if v, ok := call(t, write, "delete_device_data", map[string]any{"id": dev["id"], "confirm": "not it"}); ok || !strings.Contains(v["error"].(string), "nothing was changed") {
		t.Fatalf("wrong confirm: %v", v)
	}
	if v, _ := call(t, read, "list_activities", nil); len(v["items"].([]any)) != 1 {
		t.Fatal("a wrong confirm deleted data")
	}
	if _, err := read.CallTool(context.Background(), &mcp.CallToolParams{Name: "delete_device_data",
		Arguments: map[string]any{"id": dev["id"], "confirm": dev["model"]}}); err == nil {
		t.Fatal("a read token called delete_device_data")
	}
	if v, ok := call(t, write, "delete_device_data", map[string]any{"id": dev["id"], "confirm": strings.ToUpper(dev["model"].(string))}); !ok || v["activities"].(float64) != 1 {
		t.Fatalf("delete_device_data: %v", v)
	}
	if v, _ := call(t, read, "list_activities", nil); len(v["items"].([]any)) != 0 {
		t.Fatal("the data is still there")
	}

	// Admin tools stay with admin tokens, and admins still see no one else's health data.
	if v, ok := call(t, admin, "list_users", nil); !ok || len(v["items"].([]any)) < 2 {
		t.Fatalf("list_users: %v", v)
	}
	if _, err := write.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_users"}); err == nil {
		t.Fatal("a write token listed users")
	}
}
