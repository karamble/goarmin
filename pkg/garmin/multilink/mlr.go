// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package multilink implements Garmin's Multi-Link layer on top of one BLE characteristic
// pair: the control channel on handle 0x00 that registers services, and MLR, the reliable
// byte stream used for GFDI and file pipes. See docs/protocol/01-ble-transport.md §2–3.
package multilink

import "fmt"

// MaxSeq is the largest MLR sequence or request number; numbers wrap modulo SeqModulus.
const (
	MaxSeq     = 0x3F
	SeqModulus = MaxSeq + 1
)

// mlrFlag marks a packet as MLR; reliable handles are 0x80 | n with n in 0..7.
const mlrFlag = 0x80

// Header is the 2-byte header that starts every MLR packet.
//
//	byte 0: 1 | handle&7 (3 bits) | RN bits 5..2
//	byte 1: RN bits 1..0 | SN (6 bits)
//
// RN (request number) is cumulative: the next sequence number the sender expects to receive.
type Header struct {
	// Handle is the full reliable handle, 0x80 | n.
	Handle byte
	RN, SN int
}

// IsMLR reports whether a packet's first byte has the MLR flag set. Note that non-reliable
// handles may also have the high bit set (Gadgetbridge #5476), so callers must check that the
// handle is registered as reliable before treating the packet as MLR.
func IsMLR(b0 byte) bool { return b0&mlrFlag != 0 }

// ParseHeader decodes an MLR header.
func ParseHeader(b0, b1 byte) Header {
	return Header{
		Handle: ((b0 & 0x70) >> 4) | mlrFlag,
		RN:     int(b0&0x0F)<<2 | int(b1>>6),
		SN:     int(b1 & MaxSeq),
	}
}

// Bytes encodes the header. It panics if Handle is not a reliable handle or RN/SN are out of
// range, since that is always a programming error.
func (h Header) Bytes() [2]byte {
	if h.Handle&mlrFlag == 0 || h.Handle > 0x87 {
		panic(fmt.Sprintf("multilink: handle 0x%02X is not a reliable handle (0x80..0x87)", h.Handle))
	}
	if h.RN < 0 || h.RN > MaxSeq || h.SN < 0 || h.SN > MaxSeq {
		panic(fmt.Sprintf("multilink: RN %d / SN %d out of range 0..%d", h.RN, h.SN, MaxSeq))
	}
	return [2]byte{
		mlrFlag | (h.Handle&0x07)<<4 | byte(h.RN>>2),
		byte(h.RN&0x03)<<6 | byte(h.SN),
	}
}
