// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wellness

import "testing"

func TestFloors(t *testing.T) {
	for mm, want := range map[int64]int64{0: 0, -5: 0, 3047: 0, 3048: 1, 27432: 9, 30480: 10, 34412: 11, 40968: 13} {
		if got := Floors(mm); got != want {
			t.Errorf("Floors(%d) = %d, want %d", mm, got, want)
		}
	}
	for m, want := range map[float64]int64{0: 0, 3: 1, 30: 10, 30.48: 10, 45: 15} {
		if got := FloorsFromMetres(m); got != want {
			t.Errorf("FloorsFromMetres(%v) = %d, want %d", m, got, want)
		}
	}
}
