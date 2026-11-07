// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package smart encodes and decodes the Garmin "Smart" protobuf messages that travel inside
// GFDI PROTOBUF_REQUEST/PROTOBUF_RESPONSE (5043/5044): the envelope, the FileAccess ("new
// sync") service and the replies a host gives to watch requests it does not serve. The codecs
// are hand-written on google.golang.org/protobuf/encoding/protowire so that the proto2 wire
// details match Garmin Connect byte for byte. See docs/protocol/04 and 05.
//
// Conventions shared by every message type:
//   - Optional scalars and enums are pointers, so presence round-trips: a field set to a
//     pointer to zero is still emitted (PullItemDataRequest.Offset = U64(0) encodes as 20 00).
//   - Repeated fields are slices and are encoded unpacked, as proto2 [packed=false] requires.
//     The decoder also accepts the packed form for repeated enums.
//   - Each message keeps the raw bytes of fields it does not know and appends them, unchanged,
//     when it is marshalled again. The exception is UUID, a plain comparable value.
//   - A message field that occurs twice is merged, as protobuf does: later scalars win and
//     repeated fields are appended.
//   - Decoding never panics. Truncated input returns an error wrapping io.ErrUnexpectedEOF, and
//     a known field with an impossible wire type returns an error wrapping ErrWireType.
package smart
