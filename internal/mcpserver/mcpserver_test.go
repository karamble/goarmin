// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mcpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/karamble/goarmin/internal/store"
	pauth "github.com/karamble/goarmin/pkg/auth"
)

// fakeAPI answers the routes a test needs, as user u.
type fakeAPI struct {
	mu   sync.Mutex
	mux  *http.ServeMux
	seen []string
}

func (f *fakeAPI) ServeAs(u store.User, w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.seen = append(f.seen, fmt.Sprintf("%d %s %s", u.ID, r.Method, r.URL.RequestURI()))
	f.mu.Unlock()
	f.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type testEnv struct {
	t    *testing.T
	st   *store.Store
	api  *fakeAPI
	s    *Server
	srv  *httptest.Server
	user store.User
	now  time.Time
	mu   sync.Mutex
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "goarmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	u, err := st.EnsureUser(context.Background(), "a@example.invalid", "A")
	if err != nil {
		t.Fatal(err)
	}
	e := &testEnv{t: t, st: st, api: &fakeAPI{mux: http.NewServeMux()}, user: u, now: time.Now()}
	clock := func() time.Time { e.mu.Lock(); defer e.mu.Unlock(); return e.now }
	st.SetClock(clock)
	s, err := New(Options{Store: st, API: e.api, PublicURL: "http://goarmin.example:8642", Version: "test", Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	e.s = s
	e.srv = httptest.NewServer(s.Handler())
	t.Cleanup(e.srv.Close)
	return e
}

func (e *testEnv) token(scope string) string {
	e.t.Helper()
	secret, _ := pauth.NewToken()
	tok := TokenPrefix + secret
	if _, err := e.st.CreateMCPToken(context.Background(), e.user.ID, scope, tok[:8], pauth.HashToken(tok), scope, time.Time{}); err != nil {
		e.t.Fatal(err)
	}
	return tok
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func (e *testEnv) session(tok string) *mcp.ClientSession {
	e.t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearer{tok}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { cs.Close() })
	return cs
}

func callText(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res.Content[0].(*mcp.TextContent).Text, !res.IsError
}

func TestRateLimit(t *testing.T) {
	e := newTestEnv(t)
	e.api.mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"id": 1}) })
	e.api.mux.HandleFunc("POST /api/devices/{id}/unpair", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	e.api.mux.HandleFunc("GET /api/devices", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, []any{map[string]any{"id": 7, "nickname": "Blue", "model": "Fenix 8", "name": "fenix 8"}})
	})
	e.s.calls.Max, e.s.destructive.Max = 5, 2
	cs := e.session(e.token("write"))
	for range 2 {
		if text, ok := callText(t, cs, "unpair_device", map[string]any{"id": 7, "confirm": "blue"}); !ok {
			t.Fatalf("unpair: %s", text)
		}
	}
	if text, ok := callText(t, cs, "unpair_device", map[string]any{"id": 7, "confirm": "Fenix 8"}); ok || !strings.Contains(text, "too many removals") {
		t.Fatalf("a third removal in a minute: %s", text)
	}
	for range 3 {
		if _, ok := callText(t, cs, "get_account", nil); !ok {
			t.Fatal("reads stop with removals")
		}
	}
	if text, ok := callText(t, cs, "get_account", nil); ok || !strings.Contains(text, "too many calls") {
		t.Fatalf("the sixth call: %s", text)
	}
	e.mu.Lock()
	e.now = e.now.Add(61 * time.Second)
	e.mu.Unlock()
	if _, ok := callText(t, cs, "get_account", nil); !ok {
		t.Fatal("the limit does not slide")
	}
}

func TestConfirmAndArguments(t *testing.T) {
	e := newTestEnv(t)
	var patched []string
	e.api.mux.HandleFunc("GET /api/devices", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, []any{map[string]any{"id": 7, "nickname": "", "model": "Fenix 8", "name": "fenix 8"}})
	})
	e.api.mux.HandleFunc("PATCH /api/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		patched = append(patched, string(b))
		writeJSON(w, 200, map[string]any{"id": 7})
	})
	cs := e.session(e.token("write"))
	if _, ok := callText(t, cs, "update_device", map[string]any{"id": 7, "paused": true}); !ok || patched[0] != `{"paused":true}` {
		t.Fatalf("a plain update: %v", patched)
	}
	if text, ok := callText(t, cs, "update_device", map[string]any{"id": 7, "after_fetch": "archive_delete"}); ok ||
		!strings.Contains(text, "ask the user") || strings.Contains(text, "Fenix") || len(patched) != 1 {
		t.Fatalf("archive_delete without confirm: %s", text)
	}
	if text, ok := callText(t, cs, "update_device", map[string]any{"id": 8, "after_fetch": "archive_delete", "confirm": "Fenix 8"}); ok || !strings.Contains(text, "404") {
		t.Fatalf("another device: %s", text)
	}
	if _, ok := callText(t, cs, "update_device", map[string]any{"id": 7, "after_fetch": "archive_delete", "confirm": " fenix 8 "}); !ok ||
		patched[1] != `{"after_fetch":"archive_delete"}` {
		t.Fatalf("confirmed: %v", patched)
	}
	if text, ok := callText(t, cs, "update_device", map[string]any{"id": 7, "after_fetch": "wipe"}); ok || !strings.Contains(text, "one of") {
		t.Fatalf("an enum: %s", text)
	}
	if text, ok := callText(t, cs, "update_device", map[string]any{"id": 7.5}); ok || !strings.Contains(text, "integer") {
		t.Fatalf("a fraction for an id: %s", text)
	}
}

func TestLiveReadings(t *testing.T) {
	e := newTestEnv(t)
	old := liveOpen
	liveOpen = 2 * time.Second
	t.Cleanup(func() { liveOpen = old })
	e.api.mux.HandleFunc("GET /api/devices/{id}/live", func(w http.ResponseWriter, r *http.Request) {
		switch r.PathValue("id") {
		case "9":
			writeJSON(w, 409, map[string]any{"error": "The watch is syncing"})
			return
		case "8":
			<-r.Context().Done() // never opens
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprintf(w, "event: hello\ndata: {\"since\": 100}\n\n")
		switch r.PathValue("id") {
		case "5": // opened, but the watch sends nothing
			<-r.Context().Done()
			return
		case "7": // a node: the stream opens long before the first value
			select {
			case <-r.Context().Done():
				return
			case <-time.After(1500 * time.Millisecond):
			}
		}
		for i := 0; ; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
			fmt.Fprintf(w, "event: reading\ndata: {\"name\":\"heart_rate\",\"value\":%d,\"unit\":\"bpm\",\"t\":%d}\n\n", 60+i, 100+i)
			if i == 4 && r.PathValue("id") == "6" {
				fmt.Fprintf(w, "event: end\ndata: {\"reason\":\"closed\"}\n\n")
				return
			}
		}
	})
	cs := e.session(e.token("read"))
	// The second of reading starts with the first value, 1.5 s after the stream opened.
	text, ok := callText(t, cs, "get_live_readings", map[string]any{"id": 7, "seconds": 1})
	var v map[string]any
	_ = json.Unmarshal([]byte(text), &v)
	if !ok || v["ended"] != "time_up" || len(v["readings"].([]any)) < 5 || v["latest"].(map[string]any)["heart_rate"] == nil {
		t.Fatalf("live: %s", text)
	}
	if text, ok := callText(t, cs, "get_live_readings", map[string]any{"id": 5}); ok || !strings.Contains(text, "sent no values") {
		t.Fatalf("a link without values: %s", text)
	}
	text, ok = callText(t, cs, "get_live_readings", map[string]any{"id": 6, "seconds": 5})
	_ = json.Unmarshal([]byte(text), &v)
	if !ok || v["ended"] != "closed" || len(v["readings"].([]any)) != 5 {
		t.Fatalf("a link the watch closed: %s", text)
	}
	if text, ok = callText(t, cs, "get_live_readings", map[string]any{"id": 9}); ok || !strings.Contains(text, "syncing") {
		t.Fatalf("busy: %s", text)
	}
	if text, ok = callText(t, cs, "get_live_readings", map[string]any{"id": 8}); ok || !strings.Contains(text, "did not open") {
		t.Fatalf("a link that does not open: %s", text)
	}
}

func TestImportFiles(t *testing.T) {
	e := newTestEnv(t)
	var names []string
	var sizes []int
	e.api.mux.HandleFunc("POST /api/imports", func(w http.ResponseWriter, r *http.Request) {
		mr, err := r.MultipartReader()
		if err != nil {
			writeJSON(w, 415, map[string]any{"error": "send the files as multipart/form-data"})
			return
		}
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			_, params, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
			names, sizes = append(names, params["filename"]), append(sizes, len(b))
		}
		writeJSON(w, 202, map[string]any{"id": 3, "status": "queued"})
	})
	cs := e.session(e.token("write"))
	files := []any{
		map[string]any{"name": "GARMIN/Activity/A.fit", "content_base64": base64.StdEncoding.EncodeToString([]byte("0123456789"))},
		map[string]any{"name": "B.zip", "content_base64": base64.RawStdEncoding.EncodeToString([]byte("abc"))},
	}
	if text, ok := callText(t, cs, "import_files", map[string]any{"files": files}); !ok || !strings.Contains(text, "queued") {
		t.Fatalf("import_files: %s", text)
	}
	if len(names) != 2 || names[0] != "GARMIN/Activity/A.fit" || sizes[0] != 10 || sizes[1] != 3 {
		t.Fatalf("parts %v %v", names, sizes)
	}
	if text, ok := callText(t, cs, "import_files", map[string]any{"files": []any{map[string]any{"name": "x", "content_base64": "!!"}}}); ok || !strings.Contains(text, "base64") {
		t.Fatalf("bad base64: %s", text)
	}
	if text, ok := callText(t, cs, "import_files", map[string]any{"files": []any{map[string]any{"name": "x", "data": "AA=="}}}); ok || !strings.Contains(text, "unknown field") {
		t.Fatalf("a wrong field: %s", text)
	}
}

func TestTrimming(t *testing.T) {
	n := 1001
	ts, hr := make([]any, n), make([]any, n)
	for i := range n {
		ts[i], hr[i] = json.Number(fmt.Sprint(i)), json.Number("120")
	}
	out := thinColumns(map[string]any{"fields": []any{"heart_rate"}, "t": ts, "heart_rate": hr}, map[string]any{"max_points": json.Number("100")}).(map[string]any)
	if got := len(out["t"].([]any)); got < 50 || got > 101 || out["t"].([]any)[len(out["t"].([]any))-1] != json.Number("1000") {
		t.Fatalf("thinned to %d, last %v", got, out["t"].([]any)[len(out["t"].([]any))-1])
	}
	if len(out["fields"].([]any)) != 1 {
		t.Fatal("thinning touched another column")
	}
	ex := filterExercises(map[string]any{"categories": []any{}, "exercises": []any{
		map[string]any{"category": "plank", "number": json.Number("43"), "name": "plank", "title": "Plank", "pose": "x"},
		map[string]any{"category": "core", "number": json.Number("51"), "name": "cat_cow", "title": "Cat Cow"},
	}}, map[string]any{"search": "COW"}).(map[string]any)
	if ex["count"] != 1 || ex["exercises"].([]any)[0].(map[string]any)["pose"] != nil {
		t.Fatalf("filtered %v", ex)
	}
}

func TestHostCheckAndLevels(t *testing.T) {
	e := newTestEnv(t)
	e.api.mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"id": 1}) })
	tok := e.token("read")
	req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", strings.NewReader(`{}`))
	req.Host = "attacker.example"
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("a rebinding Host: %d", resp.StatusCode)
	}
	// An admin token whose account is no admin (any more) is refused.
	other, err := e.st.EnsureUser(context.Background(), "b@example.invalid", "B")
	if err != nil || other.IsAdmin {
		t.Fatalf("second user %+v, %v", other, err)
	}
	secret, _ := pauth.NewToken()
	admin := TokenPrefix + secret
	if _, err := e.st.CreateMCPToken(context.Background(), other.ID, "x", admin[:8], pauth.HashToken(admin), "admin", time.Time{}); err != nil {
		t.Fatal(err)
	}
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	if cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearer{admin}}, DisableStandaloneSSE: true}, nil); err == nil {
		cs.Close()
		t.Fatal("a non-admin's admin token connects")
	}
	// Prompts write today in the account's zone.
	cs := e.session(tok)
	p, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "explain_day"})
	if err != nil || !strings.Contains(p.Messages[0].Content.(*mcp.TextContent).Text, e.now.UTC().Format(time.DateOnly)) &&
		!strings.Contains(p.Messages[0].Content.(*mcp.TextContent).Text, e.now.Format(time.DateOnly)) {
		t.Fatalf("explain_day: %v", err)
	}
	calls, _ := e.st.MCPCalls(context.Background(), e.user.ID, 10)
	if len(calls) == 0 || calls[0].Tool != "prompt:explain_day" {
		t.Fatalf("calls %+v", calls)
	}
}

func TestBridge(t *testing.T) {
	e := newTestEnv(t)
	e.api.mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"email": "a@example.invalid"})
	})
	for _, in := range []string{"http://h:1", "http://h:1/", "http://h:1/mcp", "http://h:1/mcp/"} {
		if got, err := Endpoint(in); err != nil || got != "http://h:1/mcp" {
			t.Fatalf("Endpoint(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := Endpoint("goarmin.example"); err == nil {
		t.Fatal("an address without scheme")
	}
	ep, _ := Endpoint(e.srv.URL)
	b := NewBridge(ep, e.token("read"), "test", nil)
	t.Cleanup(b.Close)
	srv, err := b.Server(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ct, st := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Run(ctx, st) }()
	local, err := mcp.NewClient(&mcp.Implementation{Name: "assistant", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { local.Close() })
	res, err := local.ListTools(ctx, nil)
	if err != nil || len(res.Tools) == 0 {
		t.Fatalf("tools through the bridge: %v", err)
	}
	for _, x := range res.Tools {
		if x.Name == "plan_workout" {
			t.Fatal("a read token's bridge offers plan_workout")
		}
	}
	if local.InitializeResult().Instructions == "" {
		t.Fatal("the bridge drops the server's instructions")
	}
	call := func() string {
		r, err := local.CallTool(ctx, &mcp.CallToolParams{Name: "get_account"})
		if err != nil || r.IsError {
			t.Fatalf("get_account through the bridge: %v %v", r, err)
		}
		return r.Content[0].(*mcp.TextContent).Text
	}
	if !strings.Contains(call(), "a@example.invalid") {
		t.Fatal("the bridge's answer")
	}
	// The server forgets the session (an idle timeout): the bridge connects again.
	b.mu.Lock()
	_ = b.cs.Close()
	b.mu.Unlock()
	if !strings.Contains(call(), "a@example.invalid") {
		t.Fatal("after the session ended")
	}
	p, err := local.GetPrompt(ctx, &mcp.GetPromptParams{Name: "review_week"})
	if err != nil || !strings.Contains(p.Messages[0].Content.(*mcp.TextContent).Text, "get_calendar") {
		t.Fatalf("a prompt through the bridge: %v", err)
	}
}
