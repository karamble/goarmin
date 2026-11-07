// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package btsnoop

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

// acl builds an H4 ACL packet carrying one L2CAP PDU (or a fragment of one).
func acl(handle uint16, pb uint16, payload []byte) []byte {
	b := []byte{hciPacketACL, 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(b[1:3], handle|pb<<12)
	binary.LittleEndian.PutUint16(b[3:5], uint16(len(payload)))
	return append(b, payload...)
}

func l2capATT(att []byte) []byte {
	b := make([]byte, 4, 4+len(att))
	binary.LittleEndian.PutUint16(b[0:2], uint16(len(att)))
	binary.LittleEndian.PutUint16(b[2:4], l2capCIDATT)
	return append(b, att...)
}

func leConnCompleteEvent(handle uint16, peer []byte) []byte {
	params := make([]byte, 19)
	params[0] = leConnComplete
	params[1] = 0 // status OK
	binary.LittleEndian.PutUint16(params[2:4], handle)
	copy(params[6:12], peer)
	return append([]byte{hciPacketEvent, hciEventLEMeta, byte(len(params))}, params...)
}

func TestReadAndExtractATT(t *testing.T) {
	now := time.Now().Truncate(time.Microsecond)
	const conn = 0x040
	peerLE := []byte{0x56, 0x34, 0x12, 0xEE, 0xFF, 0xC0} // little-endian on the air

	notif := l2capATT(append([]byte{attNotification, 0x1d, 0x00}, 0xCA, 0xFE))
	write := l2capATT(append([]byte{attWriteCmd, 0x20, 0x00}, 0x00, 0x05, 0x01))
	// A notification split across two ACL fragments.
	long := l2capATT(append([]byte{attNotification, 0x1d, 0x00}, bytes.Repeat([]byte{0x42}, 40)...))
	// A characteristic declaration response: handle 0x001c, props, value handle 0x001d, 128-bit UUID.
	uuid128 := []byte{0x66, 0x9a, 0x0c, 0x20, 0x00, 0x08, 0x9a, 0x94, 0xe3, 0x11, 0x7b, 0x66, 0x11, 0x28, 0x4e, 0x6a}
	decl := append([]byte{attReadByTypeResp, 21, 0x1c, 0x00, 0x10, 0x1d, 0x00}, uuid128...)

	recs := []Record{
		{TS: now, Received: true, Data: leConnCompleteEvent(conn, peerLE)},
		{TS: now.Add(time.Millisecond), Received: true, Data: acl(conn, 0x2, l2capATT(decl))},
		{TS: now.Add(2 * time.Millisecond), Received: true, Data: acl(conn, 0x2, notif)},
		{TS: now.Add(3 * time.Millisecond), Received: false, Data: acl(conn, 0x0, write)},
		{TS: now.Add(4 * time.Millisecond), Received: true, Data: acl(conn, 0x2, long[:20])},
		{TS: now.Add(5 * time.Millisecond), Received: true, Data: acl(conn, 0x1, long[20:])},
	}
	file := Header()
	for _, r := range recs {
		r.OrigLen = uint32(len(r.Data))
		file = AppendRecord(file, r)
	}

	got, err := Parse(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(recs) {
		t.Fatalf("parsed %d records, want %d", len(got), len(recs))
	}
	if !got[0].TS.Equal(now) || !got[0].Received || got[3].Received {
		t.Fatalf("record header fields not round-tripped: %+v", got[0])
	}

	conns := map[uint16]*ConnInfo{}
	vals, trunc := ExtractATT(got, conns)
	if trunc != 0 {
		t.Fatalf("truncated = %d, want 0", trunc)
	}
	c := conns[conn]
	if c == nil || c.Peer != "C0:FF:EE:12:34:56" {
		t.Fatalf("peer = %+v, want C0:FF:EE:12:34:56", c)
	}
	if u := c.UUIDByHandle[0x001d]; u != "6a4e2811-667b-11e3-949a-0800200c9a66" {
		t.Fatalf("UUID for handle 0x001d = %q", u)
	}
	if len(vals) != 3 {
		t.Fatalf("got %d ATT values, want 3: %+v", len(vals), vals)
	}
	if v := vals[0]; v.ToDevice || v.Op != attNotification || v.Handle != 0x001d || !bytes.Equal(v.Value, []byte{0xCA, 0xFE}) {
		t.Errorf("notification = %+v", v)
	}
	if v := vals[1]; !v.ToDevice || v.Op != attWriteCmd || v.Handle != 0x0020 || !bytes.Equal(v.Value, []byte{0x00, 0x05, 0x01}) {
		t.Errorf("write = %+v", v)
	}
	if v := vals[2]; len(v.Value) != 40 || !v.TS.Equal(now.Add(5*time.Millisecond)) {
		t.Errorf("reassembled notification: len %d at %v", len(v.Value), v.TS)
	}
}

func TestParseRejectsOtherFormats(t *testing.T) {
	if _, err := Parse(bytes.NewReader([]byte("definitely not btsnoop"))); err == nil {
		t.Fatal("want an error for a non-btsnoop file")
	}
	h := Header()
	binary.BigEndian.PutUint32(h[12:16], 1001)
	if _, err := Parse(bytes.NewReader(h)); err == nil {
		t.Fatal("want an error for an unsupported datalink")
	}
}

func TestTruncatedRecordsCounted(t *testing.T) {
	now := time.Now()
	data := acl(0x040, 0x2, l2capATT([]byte{attNotification, 0x1d, 0x00, 0x01}))
	file := AppendRecord(Header(), Record{OrigLen: uint32(len(data)) + 10, TS: now, Received: true, Data: data})
	recs, err := Parse(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	conns := map[uint16]*ConnInfo{}
	if _, trunc := ExtractATT(recs, conns); trunc != 1 || conns[0x040].Truncated != 1 {
		t.Fatalf("truncated = %d / %d, want 1", trunc, conns[0x040].Truncated)
	}
}
