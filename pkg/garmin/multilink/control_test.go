// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package multilink

import (
	"bytes"
	"encoding/hex"
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

// Host messages as Gadgetbridge and Garmin Connect send them (docs/protocol/01 §2.3, 05 §3).
func TestEncode(t *testing.T) {
	cases := []struct {
		name string
		got  []byte
		want string
	}{
		{"CLOSE_ALL, Garmin Connect (12 bytes)", EncodeCloseAll(ClientGarminConnect), "00 05 01 00 00 00 00 00 00 00 00 00"},
		{"REGISTER GFDI reliable, Gadgetbridge", EncodeRegister(ClientGadgetbridge, ServiceGFDI, true), "00 00 02 00 00 00 00 00 00 00 01 00 02"},
		{"REGISTER REGISTRATION, Garmin Connect", EncodeRegister(ClientGarminConnect, ServiceRegistration, false), "00 00 01 00 00 00 00 00 00 00 04 00 00"},
		{"REGISTER file pipe 0x8018", EncodeRegister(ClientGarminConnect, 0x8018, true), "00 00 01 00 00 00 00 00 00 00 18 80 02"},
		{"CLOSE_HANDLE", EncodeCloseHandle(ClientGadgetbridge, ServiceGFDI, 0x80), "00 02 02 00 00 00 00 00 00 00 01 00 80"},
	}
	for _, c := range cases {
		if want := mustHex(t, c.want); !bytes.Equal(c.got, want) {
			t.Errorf("%s:\n got  % X\n want % X", c.name, c.got, want)
		}
	}
}

func TestParse(t *testing.T) {
	// REGISTER_ML_RESP from the Fenix 8 capture: GFDI, status 0, handle 0x80, reliable, extra 00.
	c, err := ParseControl(mustHex(t, "00 01 01 00 00 00 00 00 00 00 01 00 00 80 01 00"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Type != RegisterResponse || c.Client != 1 || c.Service != ServiceGFDI || c.Status != 0 ||
		c.Handle != 0x80 || !c.Reliable || !bytes.Equal(c.Extra, []byte{0}) {
		t.Fatalf("REGISTER_ML_RESP = %+v", c)
	}

	// A refused registration carries no handle.
	c, _ = ParseControl(mustHex(t, "00 01 02 00 00 00 00 00 00 00 01 00 03"))
	if c.Status != 3 || c.Handle != 0 || c.Reliable {
		t.Fatalf("refused REGISTER_ML_RESP = %+v", c)
	}

	// CLOSE_HANDLE_RESP: handle before status.
	c, _ = ParseControl(mustHex(t, "00 03 02 00 00 00 00 00 00 00 18 20 83 00"))
	if c.Type != CloseHandleResponse || c.Service != 0x2018 || c.Handle != 0x83 || c.Status != 0 {
		t.Fatalf("CLOSE_HANDLE_RESP = %+v", c)
	}
	if c.Service.String() != "FILE_TRANSFER_2" {
		t.Fatalf("service name %q", c.Service.String())
	}

	// CLOSE_ALL_RESP from the capture keeps its trailer as Extra.
	c, _ = ParseControl(mustHex(t, "00 06 01 00 00 00 00 00 00 00 00 00 01"))
	if c.Type != CloseAllResponse || !bytes.Equal(c.Extra, []byte{0, 0, 1}) {
		t.Fatalf("CLOSE_ALL_RESP = %+v", c)
	}

	// Our own requests decode back.
	c, _ = ParseControl(EncodeRegister(ClientGadgetbridge, ServiceKeepAlive, false))
	if c.Type != RegisterRequest || c.Service != ServiceKeepAlive || c.Reliable || c.Client != 2 {
		t.Fatalf("REGISTER_ML_REQ round trip = %+v", c)
	}

	if _, err := ParseControl([]byte{0x80, 0x40}); err == nil {
		t.Fatal("an MLR ACK is not a control message")
	}
}

// Watch responses, as the fake watch sends them, match the Fenix 8 capture.
func TestEncodeResponses(t *testing.T) {
	cases := []struct {
		name string
		got  []byte
		want string
	}{
		{"REGISTER_ML_RESP GFDI reliable", EncodeRegisterResponse(ClientGarminConnect, ServiceGFDI, 0, 0x80, true), "00 01 01 00 00 00 00 00 00 00 01 00 00 80 01 00"},
		{"CLOSE_HANDLE_RESP", EncodeCloseHandleResponse(ClientGadgetbridge, 0x2018, 0x83, 0), "00 03 02 00 00 00 00 00 00 00 18 20 83 00"},
		{"CLOSE_ALL_RESP", EncodeCloseAllResponse(ClientGarminConnect), "00 06 01 00 00 00 00 00 00 00 00 00 01"},
	}
	for _, c := range cases {
		if want := mustHex(t, c.want); !bytes.Equal(c.got, want) {
			t.Errorf("%s:\n got  % X\n want % X", c.name, c.got, want)
		}
	}
	c, _ := ParseControl(EncodeRegisterResponse(ClientGadgetbridge, ServiceKeepAlive, 0, 0x31, false))
	if c.Type != RegisterResponse || c.Service != ServiceKeepAlive || c.Status != 0 || c.Handle != 0x31 || c.Reliable {
		t.Fatalf("REGISTER_ML_RESP round trip = %+v", c)
	}
	c, _ = ParseControl(EncodeRegisterResponse(ClientGadgetbridge, ServiceGFDI, 4, 0, false))
	if c.Status != 4 || c.Handle != 0 {
		t.Fatalf("refused REGISTER_ML_RESP round trip = %+v", c)
	}
}

func TestServiceNames(t *testing.T) {
	for s, want := range map[Service]string{ServiceGFDI: "GFDI", ServiceKeepAlive: "KEEP_ALIVE", 0xE018: "FILE_TRANSFER_E", 0x0099: "SERVICE_0x0099"} {
		if s.String() != want {
			t.Errorf("Service(%d) = %q, want %q", s, s.String(), want)
		}
	}
}
