// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package figures

import (
	"strings"
	"testing"
)

// TestEmbedded: the shipped catalogue parses, covers the profile's categories, gives every
// exercise a pose, and the user's own exercises are posed one by one.
func TestEmbedded(t *testing.T) {
	d, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Exercises) < 1800 || len(d.Categories) < 50 {
		t.Fatalf("%d exercises in %d categories", len(d.Exercises), len(d.Categories))
	}
	if len(d.Parts) == 0 || d.Parts[0] != "pelvis" {
		t.Fatalf("parts %v", d.Parts)
	}
	e, keys, ok := d.Lookup("core", 51)
	if !ok || e.Quality != "own" || e.Mode != "loop" || len(keys) != 2 || len(keys[0]) != 3+4*len(d.Parts) {
		t.Fatalf("core/51 %+v, %d keys", e, len(keys))
	}
	if _, _, ok := d.Lookup("core", 99999); ok {
		t.Fatal("unknown exercise found")
	}
	n := 0
	for _, c := range d.Categories {
		n += c.Count
		if c.Title == "" || strings.Contains(c.Title, "_") {
			t.Fatalf("category %+v", c)
		}
	}
	if n != len(d.Exercises) || d.ETag == "" {
		t.Fatalf("counts %d of %d, etag %q", n, len(d.Exercises), d.ETag)
	}
}

// TestParseRejects: a pose set that does not fit the part list, a rotation that is no unit
// quaternion and an exercise without its set are refused.
func TestParseRejects(t *testing.T) {
	cat := `{"exercises":[{"key":"core/1","category":"core","number":1,"pose":"a","mode":"hold"}]}`
	for name, poses := range map[string]string{
		"width":    `{"parts":["pelvis"],"sets":{"a":[[0,1,0,0,0,0]]}}`,
		"not unit": `{"parts":["pelvis"],"sets":{"a":[[0,1,0,0,0,0,2]]}}`,
		"missing":  `{"parts":["pelvis"],"sets":{"b":[[0,1,0,0,0,0,1]]}}`,
		"no keys":  `{"parts":["pelvis"],"sets":{"a":[]}}`,
	} {
		if _, err := Parse([]byte(cat), []byte(poses)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Parse([]byte(cat), []byte(`{"parts":["pelvis"],"sets":{"a":[[0,1,0,0,0,0,1]]}}`)); err != nil {
		t.Fatal(err)
	}
	if Title("banded_exercises") != "Banded exercises" {
		t.Fatal(Title("banded_exercises"))
	}
}
