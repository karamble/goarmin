// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package gfdi implements Garmin's GFDI message layer: frame encoding with the CRC-16 that
// ends every frame, message IDs and codecs, and the session logic a host needs to talk to a
// watch. See docs/protocol/02-gfdi-session.md and 05-fenix8-capture.md.
package gfdi

// crcTable is the 4-bit table of the reflected polynomial 0xA001, i.e. CRC-16/ARC computed a
// nibble at a time (low nibble first), as Garmin's FIT SDK and Gadgetbridge do.
var crcTable = [16]uint16{
	0x0000, 0xCC01, 0xD801, 0x1400, 0xF001, 0x3C00, 0x2800, 0xE401,
	0xA001, 0x6C00, 0x7800, 0xB401, 0x5000, 0x9C01, 0x8801, 0x4400,
}

// CRC16 returns the CRC-16/ARC of data, continuing from seed. Use seed 0 for a GFDI frame; file
// transfers chain the running CRC across chunks by passing the previous result as the seed.
func CRC16(seed uint16, data []byte) uint16 {
	crc := seed
	for _, b := range data {
		crc = ((crc >> 4) & 0x0FFF) ^ crcTable[crc&0xF] ^ crcTable[b&0xF]
		crc = ((crc >> 4) & 0x0FFF) ^ crcTable[crc&0xF] ^ crcTable[(b>>4)&0xF]
	}
	return crc
}
