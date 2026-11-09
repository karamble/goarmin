// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/karamble/goarmin/internal/figures"
	"github.com/karamble/goarmin/internal/store"
)

// The exercise catalogue with its 3D figures. The data is built into the binary, so the answers
// are cached privately and revalidated by ETag.

func (s *Server) exercises(w http.ResponseWriter, r *http.Request, _ store.User) {
	d, err := figures.Load()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	cachedJSON(w, r, d.ETag, map[string]any{"categories": d.Categories, "exercises": d.Exercises})
}

func (s *Server) exercise(w http.ResponseWriter, r *http.Request, _ store.User) {
	d, err := figures.Load()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	n, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		writeError(w, http.StatusNotFound, "no such exercise")
		return
	}
	e, keys, ok := d.Lookup(r.PathValue("category"), n)
	if !ok {
		writeError(w, http.StatusNotFound, "no such exercise")
		return
	}
	cachedJSON(w, r, d.ETag, map[string]any{"exercise": e, "parts": d.Parts, "keys": keys})
}

// cachedJSON answers v, or 304 when the client has this version.
func cachedJSON(w http.ResponseWriter, r *http.Request, etag string, v any) {
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}
