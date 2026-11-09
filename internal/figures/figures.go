// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package figures holds the exercise figures: every exercise of Garmin's FIT profile with the key
// poses of its 3D mannequin. tools/figures builds the data with Blender (make figures); the
// dashboard draws the mannequin (web/src/figures) and takes the poses from here.
//
// A key pose is the root's position followed by one local rotation [x, y, z, w] per moving part,
// in glTF axes (Y up): node.position for the root, node.quaternion for each part in Parts order.
// Parts not listed keep their rest transform.
package figures

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
)

//go:embed data/catalog.json
var catalogJSON []byte

//go:embed data/poses.json
var posesJSON []byte

// Exercise is one exercise of the FIT profile.
type Exercise struct {
	Key         string `json:"key"` // category/number
	Category    string `json:"category"`
	CategoryNum int    `json:"category_num"`
	Number      int    `json:"number"`
	Name        string `json:"name"`    // the profile's name
	Title       string `json:"title"`   // readable
	Quality     string `json:"quality"` // own (posed on its own), family (a movement family), category (its category's)
	Family      string `json:"family"`
	Pose        string `json:"pose"` // the pose set it shares with similar exercises
	Mode        string `json:"mode"` // hold (one key) or loop (several)
}

// Category is a group of exercises.
type Category struct {
	Name   string `json:"name"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Count  int    `json:"count"`
}

// Data is the parsed catalogue.
type Data struct {
	Exercises  []Exercise
	Categories []Category
	Parts      []string
	ETag       string // changes with the data
	sets       map[string][][]float64
	byKey      map[string]int
}

var (
	once    sync.Once
	loaded  *Data
	loadErr error
)

// Load parses the embedded catalogue once.
func Load() (*Data, error) {
	once.Do(func() { loaded, loadErr = Parse(catalogJSON, posesJSON) })
	return loaded, loadErr
}

// Key is an exercise's key.
func Key(category string, number int) string { return fmt.Sprintf("%s/%d", category, number) }

// Parse reads catalog.json and poses.json and checks that they fit together.
func Parse(catalog, poses []byte) (*Data, error) {
	var c struct {
		Exercises []Exercise `json:"exercises"`
	}
	if err := json.Unmarshal(catalog, &c); err != nil {
		return nil, fmt.Errorf("figures: catalog: %w", err)
	}
	var p struct {
		Parts []string               `json:"parts"`
		Sets  map[string][][]float64 `json:"sets"`
	}
	if err := json.Unmarshal(poses, &p); err != nil {
		return nil, fmt.Errorf("figures: poses: %w", err)
	}
	width := 3 + 4*len(p.Parts)
	for id, keys := range p.Sets {
		if len(keys) == 0 {
			return nil, fmt.Errorf("figures: pose set %s has no keys", id)
		}
		for _, k := range keys {
			if len(k) != width {
				return nil, fmt.Errorf("figures: pose set %s: a key has %d numbers, want %d", id, len(k), width)
			}
			for i := 3; i < width; i += 4 {
				n := math.Sqrt(k[i]*k[i] + k[i+1]*k[i+1] + k[i+2]*k[i+2] + k[i+3]*k[i+3])
				if math.Abs(n-1) > 0.01 {
					return nil, fmt.Errorf("figures: pose set %s: part %s is no rotation (|q| = %.3f)", id, p.Parts[(i-3)/4], n)
				}
			}
		}
	}
	d := &Data{Exercises: c.Exercises, Parts: p.Parts, sets: p.Sets, byKey: map[string]int{}}
	cats := map[string]int{}
	for i, e := range d.Exercises {
		if _, ok := p.Sets[e.Pose]; !ok {
			return nil, fmt.Errorf("figures: %s has no pose set %s", e.Key, e.Pose)
		}
		d.byKey[e.Key] = i
		j, ok := cats[e.Category]
		if !ok {
			j = len(d.Categories)
			cats[e.Category] = j
			d.Categories = append(d.Categories, Category{Name: e.Category, Number: e.CategoryNum, Title: Title(e.Category)})
		}
		d.Categories[j].Count++
	}
	h := sha256.New()
	h.Write(catalog)
	h.Write(poses)
	d.ETag = `"` + hex.EncodeToString(h.Sum(nil))[:16] + `"`
	return d, nil
}

// Lookup finds an exercise and its key poses.
func (d *Data) Lookup(category string, number int) (Exercise, [][]float64, bool) {
	i, ok := d.byKey[Key(category, number)]
	if !ok {
		return Exercise{}, nil, false
	}
	e := d.Exercises[i]
	return e, d.sets[e.Pose], true
}

// Title turns a profile name into words: banded_exercises → Banded exercises.
func Title(name string) string {
	s := strings.ReplaceAll(name, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
