// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import (
	"bytes"
	"testing"
)

func TestBatteryRequestAndResponse(t *testing.T) {
	// Smart{device_status_service (8) {remote_device_battery_status_request (2) {}}}
	if got, want := BatteryRequest(), []byte{0x42, 0x02, 0x12, 0x00}; !bytes.Equal(got, want) {
		t.Fatalf("request % x, want % x", got, want)
	}
	env, err := ParseEnvelope(BatteryRequest())
	if err != nil || !IsBatteryRequest(env) {
		t.Fatalf("not a battery request: %v", err)
	}
	env, err = ParseEnvelope(BatteryResponse(76))
	if err != nil {
		t.Fatal(err)
	}
	b, ok := ParseBatteryResponse(env)
	if !ok || b.Status != BatteryStatusOK || b.Percent != 76 || b.HoursRemaining != -1 {
		t.Fatalf("response %+v, %v", b, ok)
	}
	// Another service's message is not an answer.
	other, _ := ParseEnvelope(CutWellnessFilesResponse(CutWellnessSuccess))
	if _, ok := ParseBatteryResponse(other); ok {
		t.Fatal("a cut-wellness answer read as a battery level")
	}
}
