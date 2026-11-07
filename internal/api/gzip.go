// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// compressible are the content types worth gzipping: API JSON and the dashboard's text assets
// and fonts. Event streams and already-encoded bodies (activity tracks) pass through untouched.
var compressible = []string{"application/json", "text/html", "text/css", "text/javascript", "application/javascript",
	"image/svg+xml", "font/ttf"}

var gzPool = sync.Pool{New: func() any { w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed); return w }}

// gzipped compresses compressible responses for clients that accept gzip.
func gzipped(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		gw := &gzipWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
}

func (g *gzipWriter) decide() {
	if g.decided {
		return
	}
	g.decided = true
	h := g.Header()
	ct := h.Get("Content-Type")
	if h.Get("Content-Encoding") != "" || ct == "" {
		return
	}
	for _, c := range compressible {
		if strings.HasPrefix(ct, c) {
			h.Set("Content-Encoding", "gzip")
			h.Add("Vary", "Accept-Encoding")
			h.Del("Content-Length")
			g.gz = gzPool.Get().(*gzip.Writer)
			g.gz.Reset(g.ResponseWriter)
			return
		}
	}
}

func (g *gzipWriter) WriteHeader(code int) {
	if code != http.StatusNotModified && code != http.StatusNoContent {
		g.decide()
	} else {
		g.decided = true
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.decided {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.decide()
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

// Flush keeps streaming responses streaming.
func (g *gzipWriter) Flush() {
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the connection (write deadlines for SSE).
func (g *gzipWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gzipWriter) close() {
	if g.gz != nil {
		_ = g.gz.Close()
		gzPool.Put(g.gz)
		g.gz = nil
	}
}
