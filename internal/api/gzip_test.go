// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGzipJSONNotStreams(t *testing.T) {
	h := gzipped(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json":
			writeJSON(w, 200, map[string]string{"hello": strings.Repeat("x", 2000)})
		case "/sse":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			_, _ = io.WriteString(w, "event: reading\ndata: {}\n\n")
		case "/pre":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "gzip")
			zw := gzip.NewWriter(w)
			_, _ = io.WriteString(zw, `{"a":1}`)
			_ = zw.Close()
		}
	}))
	get := func(path string, gz bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		if gz {
			req.Header.Set("Accept-Encoding", "gzip, br")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	rec := get("/json", true)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("JSON not gzipped")
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]string
	if err := json.NewDecoder(zr).Decode(&v); err != nil || len(v["hello"]) != 2000 {
		t.Fatalf("decoded %v, %v", len(v["hello"]), err)
	}
	if rec := get("/json", false); rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("gzipped without Accept-Encoding")
	}
	if rec := get("/sse", true); rec.Header().Get("Content-Encoding") != "" || !strings.HasPrefix(rec.Body.String(), "event:") {
		t.Fatal("the event stream was compressed")
	}
	if rec := get("/pre", true); rec.Header().Values("Content-Encoding")[0] != "gzip" || len(rec.Header().Values("Content-Encoding")) != 1 {
		t.Fatal("an already encoded body was encoded again")
	}
}

// TestSeriesFromDay: local days resolve with the watch's offsets and the span is echoed.
func TestSeriesFromDay(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	day := e.healthData("admin@example.invalid")
	s := decodeJSON[seriesJSON](t, a.want(200, "GET", "/api/series/heart_rate?from_day="+day+"&max_points=500", nil))
	if s.To-s.From < 23*3600 || s.To-s.From > 25*3600 || len(s.V) == 0 {
		t.Fatalf("one local day: from %d to %d, %d values", s.From, s.To, len(s.V))
	}
	for _, ts := range s.T {
		if ts < s.From || ts >= s.To {
			t.Fatalf("sample at %d outside the day %d..%d", ts, s.From, s.To)
		}
	}
	a.want(400, "GET", "/api/series/heart_rate?from_day=yesterday", nil)
	week := time.Now().AddDate(0, 0, -6).Format(time.DateOnly)
	w := decodeJSON[seriesJSON](t, a.want(200, "GET", "/api/series/heart_rate?from_day="+week+"&to_day="+day, nil))
	if w.To-w.From < 6*86400 {
		t.Fatalf("a week spans %d s", w.To-w.From)
	}
}
