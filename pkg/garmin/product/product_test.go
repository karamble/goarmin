// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package product

import "testing"

func TestName(t *testing.T) {
	for p, want := range map[uint16]string{
		4536:  "Fenix 8",
		4534:  "Fenix 8 Small",
		4533:  "Fenix 8 Solar Large",
		4631:  "Fenix 8 Pro",
		4315:  "Forerunner 965",
		3992:  "Forerunner 255",
		3943:  "Epix Gen 2",
		4260:  "Venu 3",
		61234: "Garmin 61234",
	} {
		if got := Name(p); got != want {
			t.Errorf("Name(%d) = %q, want %q", p, got, want)
		}
	}
}
