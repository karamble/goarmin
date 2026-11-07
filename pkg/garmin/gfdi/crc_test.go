// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gfdi

import (
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// CRC-16/ARC standard check value.
func TestCRC16CheckValue(t *testing.T) {
	if got := CRC16(0, []byte("123456789")); got != 0xBB3D {
		t.Fatalf("CRC16(\"123456789\") = 0x%04X, want 0xBB3D", got)
	}
}

// A bit-at-a-time reference implementation of CRC-16/ARC, to cross-check the nibble table.
func crcBitwise(seed uint16, data []byte) uint16 {
	crc := seed
	for _, b := range data {
		crc ^= uint16(b)
		for range 8 {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func TestCRC16MatchesBitwiseReference(t *testing.T) {
	data := make([]byte, 512)
	for i := range data {
		data[i] = byte(i*37 + 11)
	}
	for n := 0; n <= len(data); n += 7 {
		if got, want := CRC16(0, data[:n]), crcBitwise(0, data[:n]); got != want {
			t.Fatalf("n=%d: CRC16 = 0x%04X, reference 0x%04X", n, got, want)
		}
	}
}

// Chaining the running CRC over chunks equals the CRC over the whole input; Garmin file
// transfers rely on this.
func TestCRC16Chaining(t *testing.T) {
	data := []byte("the quick brown fox jumps over the lazy dog")
	if whole, chained := CRC16(0, data), CRC16(CRC16(0, data[:17]), data[17:]); whole != chained {
		t.Fatalf("chained CRC 0x%04X != whole CRC 0x%04X", chained, whole)
	}
}

// A FIT_DATA frame from Gadgetbridge's GarminSupportTest: the length field covers the whole
// frame and the last two bytes are the CRC of everything before them.
func TestCRC16GadgetbridgeFrameVector(t *testing.T) {
	f, err := hex.DecodeString("2E00941302CA960015070000000703466F726572756E6E65722033350000000000000000000000000101010197F3")
	if err != nil {
		t.Fatal(err)
	}
	if got := int(binary.LittleEndian.Uint16(f)); got != len(f) {
		t.Fatalf("length field %d, frame length %d", got, len(f))
	}
	if id := binary.LittleEndian.Uint16(f[2:]); id != 5012 {
		t.Fatalf("message ID %d, want 5012 (FIT_DATA)", id)
	}
	if got, want := CRC16(0, f[:len(f)-2]), binary.LittleEndian.Uint16(f[len(f)-2:]); got != want {
		t.Fatalf("CRC 0x%04X, frame says 0x%04X", got, want)
	}
}
