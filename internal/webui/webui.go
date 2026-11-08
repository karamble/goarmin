// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package webui embeds the built dashboard.
//
// The Vite build writes into this package's dist/ directory rather than beside the frontend
// source, so the Go toolchain never walks node_modules to find the files it embeds. dist/ is
// build output and not tracked, apart from a .gitkeep that keeps the embed pattern valid; when
// dist/ holds no index.html, the placeholder page is served instead, so `go build` and
// `go test` work before the frontend exists.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

//go:embed placeholder.html
var placeholder []byte

// Built reports whether a real frontend build is embedded.
func Built() bool {
	_, err := fs.Stat(distFS, "dist/index.html")
	return err == nil
}

// Routes are the dashboard's own top-level paths besides "/": the sections of
// web/src/app/routes.tsx and the sign-in pages of App.tsx. A test keeps the list in step.
var Routes = []string{"health", "training", "live", "devices", "settings", "login", "setup", "invite"}

// isRoute reports whether p (without the leading slash) is one of the dashboard's pages.
func isRoute(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	for _, r := range Routes {
		if first == r {
			return true
		}
	}
	return false
}

// Handler serves the built dashboard. The dashboard's own routes get index.html (the
// single-page app routes from there, with its own not-found page inside a section); every other
// path that is not a bundled file is a plain 404. /api and /tiles are routed elsewhere by the
// caller.
func Handler() (http.Handler, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		index = placeholder
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" || p == "index.html" {
			serveIndex(w, index)
			return
		}
		if st, statErr := fs.Stat(sub, p); statErr != nil || st.IsDir() || strings.HasPrefix(path.Base(p), ".") {
			// A miss with a file extension is a stale asset reference, not an SPA route.
			// Serving index.html there hands the browser HTML where it expects a script,
			// which blanks the page with no clue why.
			if path.Ext(p) != "" || !isRoute(p) {
				http.NotFound(w, r)
				return
			}
			serveIndex(w, index)
			return
		}
		if strings.HasPrefix(p, "assets/") {
			// Vite emits content-hashed file names under assets/.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	}), nil
}

func serveIndex(w http.ResponseWriter, index []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The SPA shell must not be cached, or a redeploy leaves browsers loading an old bundle
	// against a new API. Hashed assets cache normally.
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(index)
}
