// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package cobs implements Consistent Overhead Byte Stuffing as Garmin watches use it on their
// GFDI byte stream: every frame is COBS-encoded and wrapped in a leading and a trailing 0x00
// delimiter.
//
// StreamDecoder treats the input as a byte stream and returns every complete frame, so a
// single Bluetooth notification may carry the end of one frame and the start of the next (a
// case Gadgetbridge's decoder mishandles, see docs/protocol/01-ble-transport.md §4.2).
package cobs

import (
	"errors"
	"fmt"
)

// ErrTruncated reports a code byte that points past the end of the segment.
var ErrTruncated = errors.New("cobs: truncated segment")

// ErrZeroInSegment reports a 0x00 byte inside a segment, which COBS never produces.
var ErrZeroInSegment = errors.New("cobs: zero byte inside segment")

// Encode returns src COBS-encoded and wrapped in 0x00 delimiters, the framing Garmin uses. An
// input that ends in 0x00, or with a full 254-byte run, ends with a 0x01 code, matching
// Gadgetbridge's encoder byte for byte.
func Encode(src []byte) []byte {
	out := make([]byte, 0, len(src)+len(src)/254+4)
	out = append(out, 0x00)
	codeIdx := len(out)
	out = append(out, 0)
	code := byte(1)
	for _, b := range src {
		if b == 0 {
			out[codeIdx] = code
			codeIdx = len(out)
			out = append(out, 0)
			code = 1
			continue
		}
		out = append(out, b)
		code++
		if code == 0xFF {
			out[codeIdx] = code
			codeIdx = len(out)
			out = append(out, 0)
			code = 1
		}
	}
	out[codeIdx] = code
	return append(out, 0x00)
}

// Decode decodes one COBS segment, i.e. the bytes between two 0x00 delimiters.
func Decode(seg []byte) ([]byte, error) {
	out := make([]byte, 0, len(seg))
	for i := 0; i < len(seg); {
		code := int(seg[i])
		if code == 0 {
			return nil, ErrZeroInSegment
		}
		i++
		end := i + code - 1
		if end > len(seg) {
			return nil, ErrTruncated
		}
		for _, b := range seg[i:end] {
			if b == 0 {
				return nil, ErrZeroInSegment
			}
		}
		out = append(out, seg[i:end]...)
		i = end
		if code < 0xFF && i < len(seg) {
			out = append(out, 0)
		}
	}
	return out, nil
}

// DefaultMaxFrame bounds the buffered bytes of one incomplete frame. Garmin watches announce
// a maximum GFDI packet size (4000 on the Fenix 8); COBS adds less than 1 %.
const DefaultMaxFrame = 64 << 10

// StreamDecoder reassembles COBS frames from an arbitrarily chunked byte stream.
type StreamDecoder struct {
	// MaxFrame caps the bytes buffered for one frame; 0 means DefaultMaxFrame. When exceeded,
	// the buffered bytes are dropped and Feed reports an error.
	MaxFrame int
	buf      []byte
}

// Feed appends p and returns every frame completed by it, in order. A segment that fails to
// decode is skipped and reported through err; frames decoded around it are still returned.
func (d *StreamDecoder) Feed(p []byte) (frames [][]byte, err error) {
	d.buf = append(d.buf, p...)
	for {
		i := 0
		for i < len(d.buf) && d.buf[i] == 0 {
			i++
		}
		d.buf = d.buf[i:]
		end := -1
		for k, c := range d.buf {
			if c == 0 {
				end = k
				break
			}
		}
		if end < 0 {
			limit := d.MaxFrame
			if limit <= 0 {
				limit = DefaultMaxFrame
			}
			if len(d.buf) > limit {
				n := len(d.buf)
				d.buf = nil
				if err == nil {
					err = fmt.Errorf("cobs: frame exceeds %d bytes (%d buffered), dropped", limit, n)
				}
			}
			return frames, err
		}
		seg := d.buf[:end]
		frame, decErr := Decode(seg)
		d.buf = d.buf[end:]
		if decErr != nil {
			if err == nil {
				err = fmt.Errorf("cobs: %d-byte segment: %w", len(seg), decErr)
			}
			continue
		}
		frames = append(frames, frame)
	}
}

// Buffered returns the number of bytes held for an incomplete frame.
func (d *StreamDecoder) Buffered() int { return len(d.buf) }

// Reset drops any partial frame.
func (d *StreamDecoder) Reset() { d.buf = d.buf[:0] }
