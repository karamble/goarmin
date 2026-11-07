// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientForcesBrowserUA(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.UserAgent())
	}))
	defer srv.Close()

	c := NewClient(5 * time.Second)
	if _, err := c.Get(srv.URL); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "goarmin/1.0 (+https://example.invalid/bot)") // must never leave the process
	if _, err := c.Do(req); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(got))
	}
	for _, ua := range got {
		if ua != BrowserUA {
			t.Fatalf("User-Agent %q, want BrowserUA", ua)
		}
	}
	if req.Header.Get("User-Agent") == BrowserUA {
		t.Fatal("the caller's request was mutated; Transport must clone it")
	}
}

func TestBrowserUALooksLikeABrowser(t *testing.T) {
	for _, bad := range []string{"goarmin", "karamble", "http", "+", "@", "bot", "Go-http-client"} {
		if strings.Contains(strings.ToLower(BrowserUA), strings.ToLower(bad)) {
			t.Fatalf("BrowserUA contains %q", bad)
		}
	}
	if !strings.HasPrefix(BrowserUA, "Mozilla/5.0 (") || !strings.Contains(BrowserUA, "Chrome/") {
		t.Fatalf("BrowserUA %q is not a regular Chrome desktop string", BrowserUA)
	}
}
