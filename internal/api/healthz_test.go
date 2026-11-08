// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"io"
	"net/http"
	"testing"
)

// TestHealthz: /healthz answers without a session and without the database (closed here), with
// a constant plain-text body, for container health checks and uptime probes.
func TestHealthz(t *testing.T) {
	e := newEnv(t)
	e.st.Close()
	resp, err := http.Get(e.srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "ok\n" {
		t.Fatalf("GET /healthz: %d %q, want 200 \"ok\\n\"", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q", cc)
	}
}
