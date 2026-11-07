// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gfdi

import (
	"encoding/binary"
	"fmt"
)

// ProtobufChunk is the payload of PROTOBUF_REQUEST (5043) and PROTOBUF_RESPONSE (5044): one
// slice of a serialized Smart message (docs/protocol/04 §1.2).
//
//	[request ID u16][offset u32][total length u32][chunk length u32][chunk bytes]
type ProtobufChunk struct {
	RequestID uint16
	Offset    uint32
	Total     uint32
	Data      []byte
}

// protobufHeader is the size of the chunk header; with the 4-byte GFDI header and the 2-byte
// CRC, a chunk frame is 20 bytes longer than its data.
const protobufHeader = 14

// ProtobufOverhead is the number of frame bytes around a chunk's data. Gadgetbridge sizes
// chunks as max_packet_size − ProtobufOverhead, which is proven on hardware.
const ProtobufOverhead = 4 + protobufHeader + 2

// Complete reports whether the chunk holds a whole message.
func (c ProtobufChunk) Complete() bool { return c.Offset == 0 && int(c.Total) == len(c.Data) }

// ParseProtobufChunk decodes a 5043/5044 payload.
func ParseProtobufChunk(p []byte) (ProtobufChunk, error) {
	if len(p) < protobufHeader {
		return ProtobufChunk{}, fmt.Errorf("gfdi: protobuf chunk: %w (%d bytes)", errShort, len(p))
	}
	c := ProtobufChunk{
		RequestID: binary.LittleEndian.Uint16(p[0:2]),
		Offset:    binary.LittleEndian.Uint32(p[2:6]),
		Total:     binary.LittleEndian.Uint32(p[6:10]),
	}
	n := binary.LittleEndian.Uint32(p[10:14])
	if int64(n) != int64(len(p)-protobufHeader) {
		return ProtobufChunk{}, fmt.Errorf("gfdi: protobuf chunk says %d data bytes, carries %d", n, len(p)-protobufHeader)
	}
	if uint64(c.Offset)+uint64(n) > uint64(c.Total) {
		return ProtobufChunk{}, fmt.Errorf("gfdi: protobuf chunk [%d, %d) exceeds total %d", c.Offset, uint64(c.Offset)+uint64(n), c.Total)
	}
	c.Data = append([]byte(nil), p[protobufHeader:]...)
	return c, nil
}

// Payload encodes the chunk.
func (c ProtobufChunk) Payload() []byte {
	b := make([]byte, protobufHeader, protobufHeader+len(c.Data))
	binary.LittleEndian.PutUint16(b[0:2], c.RequestID)
	binary.LittleEndian.PutUint32(b[2:6], c.Offset)
	binary.LittleEndian.PutUint32(b[6:10], c.Total)
	binary.LittleEndian.PutUint32(b[10:14], uint32(len(c.Data)))
	return append(b, c.Data...)
}

// SplitProtobuf cuts a serialized message into chunks of at most maxChunk data bytes. An
// empty message is one empty chunk.
func SplitProtobuf(requestID uint16, msg []byte, maxChunk int) []ProtobufChunk {
	maxChunk = max(1, maxChunk)
	total := uint32(len(msg))
	var out []ProtobufChunk
	for off := 0; off < len(msg) || len(out) == 0; off += maxChunk {
		end := min(off+maxChunk, len(msg))
		out = append(out, ProtobufChunk{RequestID: requestID, Offset: uint32(off), Total: total, Data: msg[off:end]})
	}
	return out
}

// Chunk statuses and status codes of a protobuf status (docs/protocol/04 §1.3).
const (
	ChunkKept      byte = 0
	ChunkDiscarded byte = 1

	ProtobufNoError         byte = 0
	ProtobufUnknownRequest  byte = 100
	ProtobufDuplicatePacket byte = 101
	ProtobufMissingPacket   byte = 102
	ProtobufExceededLength  byte = 103
	ProtobufParseError      byte = 200
	ProtobufUnknownError    byte = 201
)

// ProtobufStatus is the data of a RESPONSE to a 5043/5044 chunk:
//
//	[request ID u16][offset u32][chunk status u8][status code u8]
type ProtobufStatus struct {
	RequestID   uint16
	Offset      uint32
	ChunkStatus byte
	Code        byte
}

// OK reports a chunk that was kept without error.
func (s ProtobufStatus) OK() bool { return s.ChunkStatus == ChunkKept && s.Code == ProtobufNoError }

// ParseProtobufStatus decodes ResponseMessage.Data of a RESPONSE whose original is 5043/5044.
func ParseProtobufStatus(data []byte) (ProtobufStatus, error) {
	if len(data) < 8 {
		return ProtobufStatus{}, fmt.Errorf("gfdi: protobuf status: %w (%d bytes)", errShort, len(data))
	}
	return ProtobufStatus{
		RequestID:   binary.LittleEndian.Uint16(data[0:2]),
		Offset:      binary.LittleEndian.Uint32(data[2:6]),
		ChunkStatus: data[6],
		Code:        data[7],
	}, nil
}

// Bytes encodes the status as RESPONSE data.
func (s ProtobufStatus) Bytes() []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint16(b[0:2], s.RequestID)
	binary.LittleEndian.PutUint32(b[2:6], s.Offset)
	b[6], b[7] = s.ChunkStatus, s.Code
	return b
}

func (s ProtobufStatus) String() string {
	return fmt.Sprintf("request %d offset %d chunk status %d code %d", s.RequestID, s.Offset, s.ChunkStatus, s.Code)
}
