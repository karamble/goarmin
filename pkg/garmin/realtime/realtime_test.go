// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package realtime

import (
	"testing"
	"time"

	"github.com/karamble/goarmin/pkg/garmin/multilink"
)

// TestDecode uses service messages as the Fenix 8 sent them (docs/protocol/06).
func TestDecode(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		svc  multilink.Service
		p    []byte
		want map[string]float64
	}{
		{multilink.ServiceRealtimeHR, []byte{0x03, 76, 59, 0xFF, 0xFF}, map[string]float64{"heart_rate": 76, "resting_hr": 59}},
		{multilink.ServiceRealtimeHR, []byte{0x00, 0, 0, 0xFF, 0xFF}, map[string]float64{}},
		{multilink.ServiceRealtimeSteps, []byte{98, 0, 0, 0, 0x96, 0x0F, 0, 0}, map[string]float64{"steps": 98, "step_goal": 3990}},
		{multilink.ServiceRealtimeBodyBattery, []byte{83}, map[string]float64{"body_battery": 83}},
		{multilink.ServiceRealtimeBodyBattery, []byte{0x7F}, map[string]float64{}},
		{multilink.ServiceRealtimeRespiration, []byte{14}, map[string]float64{"respiration": 14}},
		{multilink.ServiceRealtimeRespiration, []byte{0xFE}, map[string]float64{}},
		{multilink.ServiceRealtimeStress, []byte{0x16, 0x00, 0xFC, 0x00}, map[string]float64{"stress": 22}},
		{multilink.ServiceRealtimeHRV, []byte{0x20, 0x03, 0, 0, 0, 0}, map[string]float64{"rr_interval": 800}},
		{multilink.ServiceRealtimeFloors, []byte{1, 2}, map[string]float64{"raw": 0}},
		// As the Fenix 8 sent them on 2026-11-07.
		{multilink.ServiceRealtimeCalories, []byte{0x33, 0x09, 0, 0, 0xb0, 0, 0, 0}, map[string]float64{"calories": 2355, "active_calories": 176}},
		{multilink.ServiceRealtimeFloors, []byte{0x0a, 0, 0x0d, 0, 0x0a, 0}, map[string]float64{"floors_up": 10, "floors_down": 13, "floors_goal": 10}},
		{multilink.ServiceRealtimeIntensity, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x96, 0, 0, 0}, map[string]float64{"intensity_goal": 150, "raw": 0}},
	} {
		got := map[string]float64{}
		for _, r := range Decode(tc.svc, tc.p, now) {
			got[r.Name] = r.Value
			if r.Service != tc.svc || !r.At.Equal(now) {
				t.Errorf("%s: reading %+v", tc.svc, r)
			}
		}
		if len(got) != len(tc.want) {
			t.Errorf("%s % x: %v, want %v", tc.svc, tc.p, got, tc.want)
			continue
		}
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("%s % x: %s = %g, want %g", tc.svc, tc.p, k, got[k], v)
			}
		}
	}
}
