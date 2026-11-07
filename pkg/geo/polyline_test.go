// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package geo

import (
	"math"
	"testing"
)

// TestPolylineReference is the example from the format's documentation.
func TestPolylineReference(t *testing.T) {
	pts := []Point{{38.5, -120.2}, {40.7, -120.95}, {43.252, -126.453}}
	if got := EncodePolyline(pts); got != "_p~iF~ps|U_ulLnnqC_mqNvxq`@" {
		t.Fatalf("encoded %q", got)
	}
	back := DecodePolyline(EncodePolyline(pts))
	for i := range pts {
		if math.Abs(back[i].Lat-pts[i].Lat) > 1e-5 || math.Abs(back[i].Lon-pts[i].Lon) > 1e-5 {
			t.Fatalf("round trip %v", back)
		}
	}
}

func TestSimplifyAndBounds(t *testing.T) {
	// A straight line with noise below the tolerance, one corner, and invalid points.
	var pts []Point
	for i := range 100 {
		pts = append(pts, Point{Lat: 50 + float64(i)*1e-4, Lon: 7 + 1e-7*float64(i%2)})
	}
	pts = append(pts, Point{Lat: 50.0099, Lon: 7.01}, Point{Lat: math.NaN(), Lon: 7}, Point{})
	s := Simplify(pts, 1e-5)
	if len(s) != 3 {
		t.Fatalf("simplified to %d points: %v", len(s), s)
	}
	b, ok := BoundsOf(pts)
	if !ok || b.MinLat != 50 || b.MaxLon != 7.01 || b.MinLon != 7 {
		t.Fatalf("bounds %+v", b)
	}
	if got := SimplifyTo(pts, 2); len(got) > 2 {
		t.Fatalf("SimplifyTo kept %d", len(got))
	}
	if _, ok := BoundsOf([]Point{{}}); ok {
		t.Fatal("0/0 counted as a position")
	}
}
