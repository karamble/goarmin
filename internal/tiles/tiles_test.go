// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package tiles

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/karamble/goarmin/pkg/httpx"
)

func pngTile(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	img.Set(1, 1, color.RGBA{200, 245, 66, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestFetchesWithBrowserUAAndCachesPrivately: the upstream sees a browser User-Agent only, the
// tile lands under <data>/tiles, and it is served as private.
func TestFetchesWithBrowserUAAndCachesPrivately(t *testing.T) {
	tile := pngTile(t)
	var hits atomic.Int32
	var ua atomic.Value
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		ua.Store(r.UserAgent())
		w.Header().Set("Content-Type", "image/png")
		w.Write(tile)
	}))
	defer up.Close()
	dir := t.TempDir()
	c, err := New(Options{DataDir: dir, Upstream: up.URL + "/{z}/{x}/{y}.png", Client: up.Client()})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Dark || !strings.Contains(c.Attribution, "OpenStreetMap") {
		t.Fatalf("a plain style: dark %v, attribution %q", c.Dark, c.Attribution)
	}
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		c.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/a/5/16/11.png", nil))
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != CacheControl || !bytes.Equal(rec.Body.Bytes(), tile) {
			t.Fatalf("request %d: %d %q", i, rec.Code, rec.Header().Get("Cache-Control"))
		}
	}
	if hits.Load() != 1 || ua.Load() != httpx.BrowserUA {
		t.Fatalf("%d upstream fetches with User-Agent %v", hits.Load(), ua.Load())
	}
	if _, err := os.Stat(filepath.Join(dir, "tiles", "5", "16", "11.png")); err != nil {
		t.Fatalf("tile not cached: %v", err)
	}
}

// TestNoServerAndNoOSM: without a configured tile server there are no tiles, and
// OpenStreetMap's own servers are refused.
func TestNoServerAndNoOSM(t *testing.T) {
	if _, err := New(Options{DataDir: t.TempDir()}); err != ErrNoUpstream {
		t.Fatalf("no server: %v", err)
	}
	if _, err := New(Options{DataDir: t.TempDir(), Upstream: "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"}); err != ErrOSM {
		t.Fatalf("OpenStreetMap: %v", err)
	}
}

// TestTokenNeverLogged: a {token} URL gets the token on the wire, never in the log, and a
// failing upstream's error (which names the URL) is redacted too.
func TestTokenNeverLogged(t *testing.T) {
	const token = "s3cr3t-t0ken-for-the-test"
	var got atomic.Value
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.URL.Query().Get("access-token"))
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer up.Close()
	var logs bytes.Buffer
	m, err := New(Options{DataDir: t.TempDir(), Upstream: up.URL + "/style/{z}/{x}/{y}.png?access-token={token}", Token: token, Client: up.Client(),
		Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	rec := httptest.NewRecorder()
	m.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/a/5/16/11.png", nil))
	if got.Load() != token {
		t.Fatalf("upstream got token %v", got.Load())
	}
	if logs.Len() == 0 || strings.Contains(logs.String(), token) {
		t.Fatalf("log (%d bytes) holds the token:\n%s", logs.Len(), logs.String())
	}
	if _, err := New(Options{DataDir: t.TempDir(), Upstream: "jawg-matrix"}); err != ErrNoToken {
		t.Fatalf("a preset without its token: %v", err)
	}
	p, err := New(Options{DataDir: t.TempDir(), Upstream: "jawg-matrix", Token: token})
	if err != nil || !p.Dark || !strings.Contains(p.Attribution, "Jawg") || !strings.Contains(p.Attribution, "OpenStreetMap") {
		t.Fatalf("jawg-matrix: %+v, %v", p, err)
	}
	p.Close()
}
