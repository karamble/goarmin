// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHandler(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatal(err)
	}
	root := get(t, h, "/")
	if root.Code != http.StatusOK || !strings.HasPrefix(root.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("GET / = %d %q", root.Code, root.Header().Get("Content-Type"))
	}
	if root.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("index Cache-Control = %q, want no-cache", root.Header().Get("Cache-Control"))
	}
	if !Built() && !strings.Contains(root.Body.String(), "Dashboard not built") {
		t.Errorf("without a frontend build, / should serve the placeholder")
	}

	// The dashboard's routes get the index; anything else is a plain 404.
	for _, p := range []string{"/health/sleep", "/training/activities/12", "/live", "/devices/watches/3", "/settings/profile", "/login", "/setup", "/invite/abc"} {
		if r := get(t, h, p); r.Code != http.StatusOK || r.Body.String() != root.Body.String() {
			t.Errorf("SPA route %s: %d, body differs from index", p, r.Code)
		}
	}
	for _, p := range []string{"/sleep/last-night", "/wp-admin", "/healthy", "/livex", "/admin"} {
		if r := get(t, h, p); r.Code != http.StatusNotFound || r.Body.String() != "404 page not found\n" {
			t.Errorf("unknown path %s: %d %q, want the plain 404", p, r.Code, r.Body.String())
		}
	}
	// Missing assets are 404, not HTML.
	if r := get(t, h, "/assets/missing-abc123.js"); r.Code != http.StatusNotFound {
		t.Errorf("missing asset: %d, want 404", r.Code)
	}
	// Dot files in dist (the .gitkeep) are never served.
	if r := get(t, h, "/.gitkeep"); r.Code != http.StatusNotFound {
		t.Errorf("/.gitkeep: %d, want 404", r.Code)
	}
}

// TestRoutesMatchDashboard: Routes names exactly the top-level paths the dashboard routes
// (sections in routes.tsx, sign-in pages in App.tsx), so a new section never answers 404.
func TestRoutesMatchDashboard(t *testing.T) {
	var want []string
	for file, re := range map[string]*regexp.Regexp{
		"../../web/src/app/routes.tsx": regexp.MustCompile(`id: '[a-z]+', path: '/([a-z-]*)'`),
		"../../web/src/app/App.tsx":    regexp.MustCompile(`<Route path="/([a-z-]+)`),
	} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		m := re.FindAllStringSubmatch(string(b), -1)
		if len(m) == 0 {
			t.Fatalf("no routes found in %s: the pattern is out of date", file)
		}
		for _, x := range m {
			if x[1] != "" { // "/" (the glance) is served anyway
				want = append(want, x[1])
			}
		}
	}
	got := slices.Clone(Routes)
	slices.Sort(got)
	slices.Sort(want)
	want = slices.Compact(want)
	if !slices.Equal(got, want) {
		t.Fatalf("webui.Routes = %v, the dashboard routes %v", got, want)
	}
}
