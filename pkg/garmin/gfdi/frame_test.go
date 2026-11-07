// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gfdi

import (
	"bytes"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// DEVICE_INFORMATION of an Instinct 2S, from Gadgetbridge's CobsCoDecTest.
const instinctDevInfo = "2C00A0139600310F684C1BCA840508020B496E7374696E637420325308496E7374696E6374023253000004B8"

func TestParseFrameGadgetbridgeVectors(t *testing.T) {
	f, err := ParseFrame(mustHex(t, instinctDevInfo))
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != DeviceInformation || f.Compact {
		t.Fatalf("frame = %v", f)
	}
	d, err := ParseDeviceInfo(f.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if d.ProtocolVersion != 150 || d.ProductNumber != 3889 || d.SoftwareVersion != 1412 || d.MaxPacketSize != 520 {
		t.Fatalf("device info numbers: %+v", d)
	}
	if d.BluetoothName != "Instinct 2S" || d.DeviceName != "Instinct" || d.DeviceModel != "2S" {
		t.Fatalf("device info names: %+v", d)
	}
	if d.SoftwareVersionString() != "14.12" || !bytes.Equal(d.Tail, []byte{0, 0}) {
		t.Fatalf("version %q tail % X", d.SoftwareVersionString(), d.Tail)
	}

	fit, err := ParseFrame(mustHex(t, "2E00941302CA960015070000000703466F726572756E6E65722033350000000000000000000000000101010197F3"))
	if err != nil || fit.ID != FitData {
		t.Fatalf("FIT_DATA vector: %v %v", fit, err)
	}
}

func TestParseFrameErrors(t *testing.T) {
	good := mustHex(t, instinctDevInfo)
	if _, err := ParseFrame(good[:5]); !errors.Is(err, ErrShortFrame) {
		t.Errorf("short: %v", err)
	}
	if _, err := ParseFrame(append(slices.Clone(good), 0)); !errors.Is(err, ErrLength) {
		t.Errorf("trailing byte: %v", err)
	}
	bad := slices.Clone(good)
	bad[10] ^= 0xFF
	if _, err := ParseFrame(bad); !errors.Is(err, ErrCRC) {
		t.Errorf("corrupted byte: %v", err)
	}
}

func TestFrameRoundTrip(t *testing.T) {
	for _, f := range []Frame{
		{ID: SystemEvent, Payload: []byte{8, 0}},
		{ID: Configuration, Compact: true, Seq: 1, Payload: GarminConnectHostCapabilities().Payload()},
		{ID: AuthNegotiation, Compact: true, Seq: 127, Payload: []byte{0, 1, 0, 0, 0}},
		{ID: Response, Payload: nil},
	} {
		got, err := ParseFrame(f.Marshal())
		if err != nil {
			t.Fatalf("%v: %v", f, err)
		}
		if got.ID != f.ID || got.Compact != f.Compact || got.Seq != f.Seq || !bytes.Equal(got.Payload, f.Payload) {
			t.Fatalf("round trip %v -> %v", f, got)
		}
	}
}

// The SYNC_READY frame as Gadgetbridge sends it (docs/protocol/02 §5.4), rebuilt here.
func TestMarshalSyncReady(t *testing.T) {
	got := Frame{ID: SystemEvent, Payload: []byte{8, 0}}.Marshal()
	if want := mustHex(t, "08 00 A6 13 08 00 D5 C5"); !bytes.Equal(got, want) {
		t.Fatalf("SYNC_READY % X, want % X", got, want)
	}
}

func TestCompactHeaderLayout(t *testing.T) {
	b := Frame{ID: Configuration, Compact: true, Seq: 3}.Marshal()
	if b[2] != byte(Configuration-5000) || b[3] != 0x80|3 {
		t.Fatalf("compact ID bytes % X, want 32 83", b[2:4])
	}
}

func TestReplyToEchoesForm(t *testing.T) {
	req := Frame{ID: Configuration, Compact: true, Seq: 5}
	r := ReplyTo(req, StatusACK, nil)
	if r.ID != Response || !r.Compact || r.Seq != 5 {
		t.Fatalf("reply form %v", r)
	}
	m, err := ParseResponse(r.Payload)
	if err != nil || m.Original != Configuration || m.Status != StatusACK || len(m.Data) != 0 {
		t.Fatalf("reply payload %+v %v", m, err)
	}
	if plain := ReplyTo(Frame{ID: DeviceInformation}, StatusACK, []byte{1}); plain.Compact {
		t.Fatal("reply to a plain frame must be plain")
	}
}

// Garmin Connect's DEVICE_INFORMATION reply data starts 96 00 FF FF FF FF FF FF F1 2D FF FF
// (docs/protocol/05 §3.6).
func TestHostReplyData(t *testing.T) {
	h := GarminConnectHost("omarchy", "Linux", "goarmin")
	got := h.ReplyData()
	if prefix := mustHex(t, "96 00 FF FF FF FF FF FF F1 2D FF FF"); !bytes.HasPrefix(got, prefix) {
		t.Fatalf("reply data % X lacks Garmin Connect's prefix % X", got, prefix)
	}
	rest := got[12:]
	if want := append(append(append([]byte{7}, "omarchy"...), append([]byte{5}, "Linux"...)...), append([]byte{7}, "goarmin"...)...); !bytes.Equal(rest[:len(want)], want) {
		t.Fatalf("names % X", rest)
	}
	if got[len(got)-1] != 1 {
		t.Fatal("dual-pairing flag not set")
	}
}

func TestCapabilities(t *testing.T) {
	gc := GarminConnectHostCapabilities()
	if len(gc) != 17 || len(gc.Bits()) != 83 {
		t.Fatalf("Garmin Connect bitmap: %d bytes, %d bits", len(gc), len(gc.Bits()))
	}
	if !gc.Has(CapSync2) || gc.Has(CapMultiLinkService) || gc.Has(CapExploreSync) || gc.Has(CapFeatureCapabilitiesExchange) {
		t.Fatal("Garmin Connect bitmap flags differ from the capture")
	}
	parsed, err := ParseConfiguration(gc.Payload())
	if err != nil || !bytes.Equal(parsed, gc) {
		t.Fatalf("CONFIGURATION round trip: % X %v", parsed, err)
	}
	if _, err := ParseConfiguration([]byte{5, 1, 2}); err == nil {
		t.Fatal("truncated CONFIGURATION accepted")
	}
	if Capabilities(nil).Has(3) {
		t.Fatal("empty bitmap has a bit")
	}
}

// The DEVICE_INFORMATION encoder (fake watches) reproduces Gadgetbridge's Instinct 2S frame.
func TestDeviceInfoPayloadRoundTrip(t *testing.T) {
	f, err := ParseFrame(mustHex(t, instinctDevInfo))
	if err != nil {
		t.Fatal(err)
	}
	d, err := ParseDeviceInfo(f.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Payload(); !bytes.Equal(got, f.Payload) {
		t.Fatalf("re-encoded\n % X\nwant\n % X", got, f.Payload)
	}
	c := CapabilitiesFromBits(17, 0, 90, 128)
	if !c.Has(90) || !c.Has(128) || c.Has(1) || len(c) != 17 {
		t.Fatalf("bitmap % X", c)
	}
}
