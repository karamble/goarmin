// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import (
	"bytes"
	"testing"
)

func TestFeatureCapabilities(t *testing.T) {
	req := FeatureCapabilitiesRequest(GarminConnectFeatures)
	env, err := ParseEnvelope(req)
	if err != nil || !IsFeatureCapabilitiesRequest(env) {
		t.Fatalf("request % X: %v", req, err)
	}
	// garmin_connect_request { version: 2, wellness_threshold_met_support: true } as Garmin
	// Connect sends it (5A 04 08 02 10 01), then the empty file access (82 01 00) and device
	// status (9A 01 00) requests; no GUID.
	if want := []byte{0x5A, 0x04, 0x08, 0x02, 0x10, 0x01, 0x82, 0x01, 0x00, 0x9A, 0x01, 0x00}; !bytes.Contains(env.Payload, want) {
		t.Fatalf("payload % X", env.Payload)
	}
	if bytes.Contains(env.Payload, []byte{0x0A}) && env.Payload[2] == 0x0A {
		t.Fatal("a GUID was sent")
	}

	renv, err := ParseEnvelope(FeatureCapabilitiesResponse())
	if err != nil {
		t.Fatal(err)
	}
	f, ok := ParseFeatureCapabilitiesResponse(renv)
	if !ok || f.GUIDStatus != GUIDUnset || f.Version != 1 || !f.WellnessSupport || !f.LifestyleLoggingSupport ||
		!f.FileAccess || !f.CustomFlagSupport || f.ChecksumMethod != 1 || f.ChecksumMaxFileSize != 3145728 {
		t.Fatalf("parsed %+v", f)
	}
	if _, ok := ParseFeatureCapabilitiesResponse(env); ok {
		t.Fatal("the request parsed as a response")
	}

	n, _ := ParseEnvelope(WellnessThresholdMetNotification())
	if !IsWellnessThresholdMet(n) || IsLifestyleLoggingUpdate(n) || IsCutWellnessFilesRequest(n) {
		t.Fatal("wellness notification not recognised")
	}
	if GUIDMatch.String() != "MATCH" || GUIDStatus(9).String() != "UNSET" {
		t.Fatal("GUID status names")
	}
}
