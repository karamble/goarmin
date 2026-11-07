// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"reflect"
	"testing"
	"time"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

func TestBuildSeriesTotals(t *testing.T) {
	t0 := time.Now().Unix()
	// Two activity types; type 6 restarts (midnight) between its 2nd and 3rd sample.
	pts := []store.Point{
		{TS: t0, Sub: 6, Value: 100}, {TS: t0, Sub: 1, Value: 10},
		{TS: t0 + 60, Sub: 6, Value: 150}, {TS: t0 + 60, Sub: 1, Value: 30},
		{TS: t0 + 120, Sub: 6, Value: 5},
	}
	s := buildSeries(wellness.Steps, pts, t0, t0+180, 100)
	if !s.Total || !reflect.DeepEqual(s.T, []int64{t0, t0 + 60, t0 + 120}) || !reflect.DeepEqual(s.V, []float64{110, 70, 5}) {
		t.Fatalf("steps %+v", s)
	}
}

func TestBuildSeriesBuckets(t *testing.T) {
	t0 := time.Now().Unix()
	var pts []store.Point
	for i := range 100 {
		pts = append(pts, store.Point{TS: t0 + int64(i), Value: int64(60 + i%10)})
	}
	s := buildSeries(wellness.HeartRate, pts, t0, t0+100, 10)
	if len(s.T) != 10 || s.Bucket != 10 || s.Min[0] != 60 || s.Max[0] != 69 || s.V[0] != 64.5 {
		t.Fatalf("buckets %+v", s)
	}
	r := buildSeries(wellness.Respiration, []store.Point{{TS: t0, Value: 1885}}, t0, t0+1, 10)
	if r.V[0] != 18.85 || r.Unit != "brpm" {
		t.Fatalf("respiration %+v", r)
	}
}
