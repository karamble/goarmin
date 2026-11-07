// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package watch

import (
	"testing"
	"time"
)

func TestParseBattery(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		in      []byte
		ok      bool
		percent int
		status  string
	}{
		{[]byte{0x10, 0xc8, 76, 0, 0, 0}, true, 76, "good"}, // status good, "volts", 76 %
		{[]byte{0x30, 0xb4, 12, 0, 0, 0}, true, 12, "low"},  // low
		{[]byte{0x05, 0x00, 100}, true, 100, ""},            // unknown status nibble: keep the charge
		{[]byte{0x70, 0x00, 50, 0, 0, 0}, false, 0, ""},     // invalid
		{[]byte{0x10, 0x00, 101}, false, 0, ""},             // not a percentage
		{[]byte{0x10, 0x00}, false, 0, ""},                  // too short
	} {
		b, ok := ParseBattery(c.in, now)
		if ok != c.ok || (ok && (b.Percent != c.percent || b.Status != c.status || !b.At.Equal(now))) {
			t.Errorf("ParseBattery(% x) = %+v, %v", c.in, b, ok)
		}
	}
}
