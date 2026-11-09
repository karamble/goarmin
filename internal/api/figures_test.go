// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestExercises: the catalogue and an exercise's poses for signed-in users, 404 for an unknown
// exercise, 401 when signed out, and 304 for a client that has this version.
func TestExercises(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	list := decodeJSON[struct {
		Categories []map[string]any `json:"categories"`
		Exercises  []map[string]any `json:"exercises"`
	}](t, admin.want(200, "GET", "/api/exercises", nil))
	if len(list.Exercises) < 1800 || len(list.Categories) < 50 {
		t.Fatalf("%d exercises, %d categories", len(list.Exercises), len(list.Categories))
	}
	one := decodeJSON[struct {
		Exercise map[string]any `json:"exercise"`
		Parts    []string       `json:"parts"`
		Keys     [][]float64    `json:"keys"`
	}](t, admin.want(200, "GET", "/api/exercises/core/51", nil))
	if one.Exercise["title"] != "Cat Cow" || len(one.Keys) != 2 || len(one.Keys[0]) != 3+4*len(one.Parts) {
		t.Fatalf("core/51 %v, %d keys", one.Exercise, len(one.Keys))
	}
	admin.want(404, "GET", "/api/exercises/core/99999", nil)
	admin.want(404, "GET", "/api/exercises/core/x", nil)
	e.client().want(401, "GET", "/api/exercises", nil)

	req := httptest.NewRequest("GET", "/api/exercises/core/51", nil)
	rec := httptest.NewRecorder()
	cachedJSON(rec, req, `"v1"`, map[string]int{"a": 1})
	if rec.Code != http.StatusOK || rec.Header().Get("ETag") != `"v1"` {
		t.Fatalf("first answer %d %v", rec.Code, rec.Header())
	}
	req.Header.Set("If-None-Match", `"v1"`)
	rec = httptest.NewRecorder()
	cachedJSON(rec, req, `"v1"`, map[string]int{"a": 1})
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("revalidation %d %q", rec.Code, rec.Body.String())
	}
}
