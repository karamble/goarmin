// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fitactivity

import (
	"testing"

	"github.com/muktihari/fit/profile/typedef"
)

func TestSportName(t *testing.T) {
	for _, c := range []struct {
		s    typedef.Sport
		sub  typedef.SubSport
		want string
	}{
		{typedef.SportSwimming, typedef.SubSportLapSwimming, "Pool swim"},
		{typedef.SportRunning, typedef.SubSportTrail, "Trail run"},
		{typedef.SportRunning, typedef.SubSportGeneric, "Run"},
		{typedef.SportWalking, typedef.SubSportGeneric, "Walk"},
		{typedef.SportTraining, typedef.SubSportStrengthTraining, "Strength"},
		{typedef.SportFitnessEquipment, typedef.SubSportElliptical, "Elliptical"},
		{typedef.SportCycling, typedef.SubSportCyclocross, "Ride"},
		{typedef.SportBoxing, typedef.SubSportGeneric, "Boxing"},
	} {
		if got := SportName(c.s, c.sub); got != c.want {
			t.Errorf("SportName(%s, %s) = %q, want %q", c.s, c.sub, got, c.want)
		}
	}
}

func TestLabel(t *testing.T) {
	unknown := typedef.Sport(60) // not in the SDK's enum; the Fenix 8's Health Snapshot
	if got := Label(unknown, typedef.SubSportGeneric, "Health Snapshot"); got != "Health Snapshot" {
		t.Errorf("unknown sport: %q", got)
	}
	if got := Label(typedef.SportRunning, typedef.SubSportTrail, "My Trail"); got != "Trail run" {
		t.Errorf("known sport: %q", got)
	}
	if got := Label(unknown, typedef.SubSportGeneric, ""); got != SportName(unknown, typedef.SubSportGeneric) {
		t.Errorf("unknown without a profile name: %q", got)
	}
}
