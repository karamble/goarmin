// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package geo has small helpers for GPS tracks: bounds, simplification and the encoded
// polyline format that map libraries read (Google's algorithm, 5 decimals).
package geo

import (
	"math"
	"strings"
)

// Point is a position in degrees.
type Point struct{ Lat, Lon float64 }

// Bounds is the smallest box around a track.
type Bounds struct{ MinLat, MinLon, MaxLat, MaxLon float64 }

// Valid reports whether p is a real position (not NaN, not 0/0, within range).
func (p Point) Valid() bool {
	return !math.IsNaN(p.Lat) && !math.IsNaN(p.Lon) && !(p.Lat == 0 && p.Lon == 0) &&
		p.Lat >= -90 && p.Lat <= 90 && p.Lon >= -180 && p.Lon <= 180
}

// BoundsOf returns the bounds of the valid points, and false if there are none.
func BoundsOf(pts []Point) (Bounds, bool) {
	b := Bounds{MinLat: 91, MinLon: 181, MaxLat: -91, MaxLon: -181}
	ok := false
	for _, p := range pts {
		if !p.Valid() {
			continue
		}
		ok = true
		b.MinLat, b.MaxLat = math.Min(b.MinLat, p.Lat), math.Max(b.MaxLat, p.Lat)
		b.MinLon, b.MaxLon = math.Min(b.MinLon, p.Lon), math.Max(b.MaxLon, p.Lon)
	}
	return b, ok
}

// Simplify reduces a track with Ramer–Douglas–Peucker: no removed point lies farther than
// tolerance (degrees, about 1e-5 per metre) from the result. Invalid points are dropped.
func Simplify(pts []Point, tolerance float64) []Point {
	var in []Point
	for _, p := range pts {
		if p.Valid() {
			in = append(in, p)
		}
	}
	if len(in) < 3 {
		return in
	}
	keep := make([]bool, len(in))
	keep[0], keep[len(in)-1] = true, true
	type span struct{ a, b int }
	stack := []span{{0, len(in) - 1}}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		far, idx := 0.0, -1
		for i := s.a + 1; i < s.b; i++ {
			if d := segDist(in[i], in[s.a], in[s.b]); d > far {
				far, idx = d, i
			}
		}
		if idx >= 0 && far > tolerance {
			keep[idx] = true
			stack = append(stack, span{s.a, idx}, span{idx, s.b})
		}
	}
	out := make([]Point, 0, len(in)/4)
	for i, p := range in {
		if keep[i] {
			out = append(out, p)
		}
	}
	return out
}

// SimplifyTo simplifies with a growing tolerance until at most max points remain.
func SimplifyTo(pts []Point, max int) []Point {
	tol := 1e-5
	out := Simplify(pts, tol)
	for len(out) > max && tol < 1 {
		tol *= 2
		out = Simplify(pts, tol)
	}
	return out
}

// segDist is the distance of p from segment a–b in degrees, longitude scaled by cos(lat).
func segDist(p, a, b Point) float64 {
	k := math.Cos(a.Lat * math.Pi / 180)
	px, py := (p.Lon-a.Lon)*k, p.Lat-a.Lat
	bx, by := (b.Lon-a.Lon)*k, b.Lat-a.Lat
	l2 := bx*bx + by*by
	if l2 == 0 {
		return math.Hypot(px, py)
	}
	t := math.Max(0, math.Min(1, (px*bx+py*by)/l2))
	return math.Hypot(px-t*bx, py-t*by)
}

// EncodePolyline encodes points in the polyline format with 5 decimals.
func EncodePolyline(pts []Point) string {
	var sb strings.Builder
	var plat, plon int64
	for _, p := range pts {
		lat, lon := int64(math.Round(p.Lat*1e5)), int64(math.Round(p.Lon*1e5))
		encodeValue(&sb, lat-plat)
		encodeValue(&sb, lon-plon)
		plat, plon = lat, lon
	}
	return sb.String()
}

func encodeValue(sb *strings.Builder, v int64) {
	u := uint64(v << 1)
	if v < 0 {
		u = ^u
	}
	for u >= 0x20 {
		sb.WriteByte(byte((0x20 | (u & 0x1f)) + 63))
		u >>= 5
	}
	sb.WriteByte(byte(u + 63))
}

// DecodePolyline is the inverse of EncodePolyline.
func DecodePolyline(s string) []Point {
	var out []Point
	var lat, lon int64
	i := 0
	next := func() (int64, bool) {
		var res uint64
		var shift uint
		for i < len(s) {
			b := uint64(s[i]) - 63
			i++
			res |= (b & 0x1f) << shift
			shift += 5
			if b < 0x20 {
				v := int64(res >> 1)
				if res&1 != 0 {
					v = ^v
				}
				return v, true
			}
		}
		return 0, false
	}
	for i < len(s) {
		dlat, ok1 := next()
		dlon, ok2 := next()
		if !ok1 || !ok2 {
			break
		}
		lat, lon = lat+dlat, lon+dlon
		out = append(out, Point{Lat: float64(lat) / 1e5, Lon: float64(lon) / 1e5})
	}
	return out
}
