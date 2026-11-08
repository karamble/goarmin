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

func TestKindOf(t *testing.T) {
	for p, want := range map[uint16]Kind{
		4536: Watch, 3888: Watch, 4315: Watch, 3461: Scale, 0xFFFE: App, 61234: Other,
	} {
		if got := KindOf(p); got != want {
			t.Errorf("KindOf(%d) %s = %q, want %q", p, Name(p), got, want)
		}
	}
	// Every family of the tables lands somewhere sensible: no Edge is a watch, no HRM a watch.
	for p := 1; p < 0xFFFE; p++ {
		n, k := Name(uint16(p)), KindOf(uint16(p))
		switch {
		case len(n) >= 4 && n[:4] == "Edge" && k != BikeComputer, len(n) >= 3 && n[:3] == "HRM" && k != Sensor:
			t.Errorf("%d %s is %s", p, n, k)
		}
	}
	if Scale.Words() != "scale" || BikeComputer.Words() != "bike computer" || Kind("").Words() != "device" {
		t.Fatal("words")
	}
}
