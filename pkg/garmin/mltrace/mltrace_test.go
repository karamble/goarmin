// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mltrace

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/karamble/goarmin/pkg/cobs"
	"github.com/karamble/goarmin/pkg/garmin/capture"
	"github.com/karamble/goarmin/pkg/garmin/gfdi"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
)

// TestRoundTrip writes a registration and a plain GFDI frame, reads the trace back and decodes
// it with pkg/garmin/capture as if it came from a btsnoop log.
func TestRoundTrip(t *testing.T) {
	now := time.Now()
	start := now.Add(-time.Minute)
	elapsed := 2 * time.Second
	var buf bytes.Buffer
	w := NewWriter(&buf, start)
	w.since = func(time.Time) time.Duration { return elapsed }

	reg := multilink.EncodeRegister(multilink.ClientGadgetbridge, multilink.ServiceGFDI, false)
	resp := multilink.EncodeRegisterResponse(multilink.ClientGadgetbridge, multilink.ServiceGFDI, 0, 0x01, false)
	frame := gfdi.Frame{ID: gfdi.DeviceInformation, Payload: []byte{1, 2, 3}}
	data := append([]byte{0x01}, cobs.Encode(frame.Marshal())...)
	w.Packet("tx", reg)
	w.Packet("rx", resp)
	w.Frame("rx", frame)
	elapsed = 2500 * time.Millisecond
	w.Packet("rx", data)

	tr, err := Read(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Start.Equal(start) {
		t.Fatalf("start %v, want %v", tr.Start, start)
	}
	if len(tr.Packets) != 3 {
		t.Fatalf("%d packets, want 3 (the frame summary is skipped)", len(tr.Packets))
	}
	if p := tr.Packets[0]; !p.ToDevice || !bytes.Equal(p.Value, reg) || p.At != 2*time.Second {
		t.Fatalf("packet 0 = %+v", p)
	}
	if p := tr.Packets[2]; p.ToDevice || p.At != 2500*time.Millisecond {
		t.Fatalf("packet 2 = %+v", p)
	}

	vals, conns := tr.ATT(tr.Start)
	if got := capture.GarminConnections(vals, conns); len(got) != 1 || got[0] != ATTConn {
		t.Fatalf("Garmin connections %v", got)
	}
	res := capture.Decode(vals, conns, ATTConn)
	if len(res.Controls) != 2 || res.FrameErrors != 0 || len(res.Frames) != 1 {
		t.Fatalf("decode: %s", res)
	}
	f := res.Frames[0]
	if f.Frame.ID != gfdi.DeviceInformation || !bytes.Equal(f.Frame.Payload, frame.Payload) || f.ToDevice {
		t.Fatalf("frame %+v", f)
	}
	if !f.TS.Equal(start.Add(2500 * time.Millisecond)) {
		t.Fatalf("frame time %v", f.TS)
	}
}

// TestReadWithoutMeta reads a trace written before the meta line existed.
func TestReadWithoutMeta(t *testing.T) {
	tr, err := Read(strings.NewReader(`{"dir":"rx","hex":"0001","layer":"ml","t":0.5}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Start.IsZero() || len(tr.Packets) != 1 || tr.Packets[0].At != 500*time.Millisecond {
		t.Fatalf("%+v", tr)
	}
	if _, err := Read(strings.NewReader(`{"layer":"ml","dir":"up","hex":"00","t":0}`)); err == nil {
		t.Fatal("bad direction accepted")
	}
}
