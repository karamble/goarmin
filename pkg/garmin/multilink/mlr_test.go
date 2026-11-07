// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package multilink

import "testing"

// Header examples from docs/protocol/01-ble-transport.md §3.2.
func TestParseHeaderExamples(t *testing.T) {
	cases := []struct {
		b0, b1 byte
		want   Header
	}{
		{0x80, 0x40, Header{Handle: 0x80, RN: 1, SN: 0}}, // ACK-only, RN=1
		{0x91, 0x43, Header{Handle: 0x81, RN: 5, SN: 3}},
		{0xFF, 0xFF, Header{Handle: 0x87, RN: 63, SN: 63}},
	}
	for _, c := range cases {
		if got := ParseHeader(c.b0, c.b1); got != c.want {
			t.Errorf("ParseHeader(%02X %02X) = %+v, want %+v", c.b0, c.b1, got, c.want)
		}
	}
}

func TestHeaderRoundTrip(t *testing.T) {
	for h := byte(0x80); h <= 0x87; h++ {
		for rn := 0; rn <= MaxSeq; rn++ {
			for sn := 0; sn <= MaxSeq; sn++ {
				want := Header{Handle: h, RN: rn, SN: sn}
				b := want.Bytes()
				if !IsMLR(b[0]) {
					t.Fatalf("%+v: encoded byte 0 %02X lacks the MLR flag", want, b[0])
				}
				if got := ParseHeader(b[0], b[1]); got != want {
					t.Fatalf("round trip %+v -> % X -> %+v", want, b, got)
				}
			}
		}
	}
}

func TestHeaderBytesPanicsOnBadInput(t *testing.T) {
	for _, h := range []Header{{Handle: 0x05}, {Handle: 0x88}, {Handle: 0x80, RN: 64}, {Handle: 0x80, SN: -1}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Bytes(%+v) did not panic", h)
				}
			}()
			_ = h.Bytes()
		}()
	}
}
