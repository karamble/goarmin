// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package btsnoop reads Android Bluetooth HCI snoop logs (btsnoop format, H4 datalink) and
// extracts the ATT traffic of each LE connection. It is used by tools/snoopdecode and by
// replay tests that run a real capture through goarmin's protocol stack.
package btsnoop

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// epochDelta is the number of microseconds between 0000-01-01 (the btsnoop epoch) and the Unix
// epoch.
const epochDelta = 0x00dcddb30f2f8000

// datalinkH4 is the only btsnoop datalink this package understands.
const datalinkH4 = 1002

// Record is one HCI packet from the log.
type Record struct {
	OrigLen, InclLen uint32
	// Received is true for controller → host traffic, i.e. data that came from the remote device.
	Received bool
	TS       time.Time
	// Data starts with the H4 packet type byte (0x01 command, 0x02 ACL, 0x04 event, …).
	Data []byte
}

// Read parses a btsnoop file. A truncated final record ends the log without an error, because
// Android often cuts the last record of a rotated log.
func Read(path string) ([]Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse parses btsnoop data from r.
func Parse(rd io.Reader) ([]Record, error) {
	r := bufio.NewReaderSize(rd, 1<<20)
	hdr := make([]byte, 16)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, err
	}
	if string(hdr[:8]) != "btsnoop\x00" {
		return nil, errors.New("not a btsnoop file")
	}
	if dl := binary.BigEndian.Uint32(hdr[12:16]); dl != datalinkH4 {
		return nil, fmt.Errorf("unsupported datalink %d (want %d H4)", dl, datalinkH4)
	}
	var out []Record
	rh := make([]byte, 24)
	for {
		if _, err := io.ReadFull(r, rh); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return out, nil
			}
			return nil, err
		}
		rec := Record{
			OrigLen:  binary.BigEndian.Uint32(rh[0:4]),
			InclLen:  binary.BigEndian.Uint32(rh[4:8]),
			Received: binary.BigEndian.Uint32(rh[8:12])&1 == 1,
		}
		us := int64(binary.BigEndian.Uint64(rh[16:24])) - epochDelta
		rec.TS = time.UnixMicro(us)
		rec.Data = make([]byte, rec.InclLen)
		if _, err := io.ReadFull(r, rec.Data); err != nil {
			return out, nil
		}
		out = append(out, rec)
	}
}

// AppendRecord encodes one record in btsnoop format. It exists for tests and tools that build
// synthetic logs.
func AppendRecord(dst []byte, rec Record) []byte {
	var rh [24]byte
	binary.BigEndian.PutUint32(rh[0:4], rec.OrigLen)
	binary.BigEndian.PutUint32(rh[4:8], uint32(len(rec.Data)))
	if rec.Received {
		binary.BigEndian.PutUint32(rh[8:12], 1)
	}
	binary.BigEndian.PutUint64(rh[16:24], uint64(rec.TS.UnixMicro()+epochDelta))
	dst = append(dst, rh[:]...)
	return append(dst, rec.Data...)
}

// Header returns the 16-byte btsnoop file header for the H4 datalink.
func Header() []byte {
	h := make([]byte, 16)
	copy(h, "btsnoop\x00")
	binary.BigEndian.PutUint32(h[8:12], 1)
	binary.BigEndian.PutUint32(h[12:16], datalinkH4)
	return h
}

// MACString formats a little-endian 6-byte Bluetooth address in capitals, AA:BB:CC:DD:EE:FF.
func MACString(b []byte) string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", b[5], b[4], b[3], b[2], b[1], b[0])
}
