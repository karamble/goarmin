// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package webui

import (
	"net/http"
	"net/http/httptest"
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

	// SPA routes fall back to the index.
	if r := get(t, h, "/sleep/last-night"); r.Code != http.StatusOK || r.Body.String() != root.Body.String() {
		t.Errorf("SPA route: %d, body differs from index", r.Code)
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
