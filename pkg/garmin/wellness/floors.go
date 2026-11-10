// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wellness

import "math"

// FloorMM is one floor as a Garmin watch counts it: 10 ft (3.048 m) climbed or descended.
const FloorMM = 3048

// Floors is the whole floors a climb of mm millimetres makes, as the watch shows them: a floor
// counts once it is climbed in full (30.48 m is 10 floors, 34.41 m is 11).
func Floors(mm int64) int64 {
	if mm <= 0 {
		return 0
	}
	return mm / FloorMM
}

// FloorsFromMetres turns a floors goal the watch keeps in metres into floors, rounded: the
// Fenix 8's goal of 10 floors is stored as 30 m.
func FloorsFromMetres(m float64) int64 {
	if m <= 0 {
		return 0
	}
	return int64(math.Round(m * 1000 / FloorMM))
}
