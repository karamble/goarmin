// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package btsnoop

import (
	"encoding/binary"
	"fmt"
	"time"
)

// ATT opcodes this package extracts values from.
const (
	attFindInfoResp     = 0x05
	attReadByTypeResp   = 0x09
	attReadByGroupResp  = 0x11
	attWriteReq         = 0x12
	attNotification     = 0x1B
	attIndication       = 0x1D
	attWriteCmd         = 0x52
	l2capCIDATT         = 0x0004
	hciPacketACL        = 0x02
	hciPacketEvent      = 0x04
	hciEventLEMeta      = 0x3E
	leConnComplete      = 0x01
	leEnhConnComplete   = 0x0A
	leEnhConnCompleteV2 = 0x29
)

// ATTValue is one characteristic value seen on the air: a write from the phone or a
// notification/indication from the remote device.
type ATTValue struct {
	TS         time.Time
	ConnHandle uint16
	// ToDevice is true for phone → device writes.
	ToDevice bool
	Op       byte
	Handle   uint16
	Value    []byte
}

// ConnInfo collects what the log reveals about one LE connection.
type ConnInfo struct {
	// Peer is the remote address in capitals, if the connection setup was captured.
	Peer string
	// UUIDByHandle maps ATT handles to UUIDs learnt from GATT discovery responses. Service
	// declarations are prefixed with "svc ".
	UUIDByHandle map[uint16]string
	Values       int
	FirstSeen    time.Time
	Truncated    int
}

// UUIDString formats a little-endian 16-bit or 128-bit UUID.
func UUIDString(b []byte) string {
	switch len(b) {
	case 2:
		return fmt.Sprintf("%04x", binary.LittleEndian.Uint16(b))
	case 16:
		r := make([]byte, 16)
		for i := range b {
			r[i] = b[15-i]
		}
		return fmt.Sprintf("%x-%x-%x-%x-%x", r[0:4], r[4:6], r[6:8], r[8:10], r[10:16])
	}
	return fmt.Sprintf("%x", b)
}

type aclKey struct {
	conn uint16
	rx   bool
}

// ExtractATT reassembles ACL → L2CAP and returns every ATT value write and notification, in
// log order. conns is filled with per-connection information (peer address, GATT handles).
// truncated counts records the log cut short.
func ExtractATT(recs []Record, conns map[uint16]*ConnInfo) (vals []ATTValue, truncated int) {
	reasm := map[aclKey][]byte{}
	want := map[aclKey]int{}
	getConn := func(h uint16) *ConnInfo {
		c := conns[h]
		if c == nil {
			c = &ConnInfo{UUIDByHandle: map[uint16]string{}}
			conns[h] = c
		}
		return c
	}
	for _, rec := range recs {
		if len(rec.Data) < 1 {
			continue
		}
		if rec.InclLen < rec.OrigLen {
			truncated++
		}
		switch rec.Data[0] {
		case hciPacketEvent:
			ev := rec.Data[1:]
			if len(ev) < 2 {
				continue
			}
			code, params := ev[0], ev[2:]
			if code == hciEventLEMeta && len(params) >= 1 {
				sub := params[0]
				if (sub == leConnComplete || sub == leEnhConnComplete || sub == leEnhConnCompleteV2) && len(params) >= 12 {
					status := params[1]
					h := binary.LittleEndian.Uint16(params[2:4]) & 0x0FFF
					if status == 0 {
						getConn(h).Peer = MACString(params[6:12])
					}
				}
			}
		case hciPacketACL:
			if len(rec.Data) < 5 {
				continue
			}
			hf := binary.LittleEndian.Uint16(rec.Data[1:3])
			h := hf & 0x0FFF
			pb := (hf >> 12) & 0x3
			dl := int(binary.LittleEndian.Uint16(rec.Data[3:5]))
			payload := rec.Data[5:]
			if len(payload) > dl {
				payload = payload[:dl]
			}
			k := aclKey{h, rec.Received}
			if rec.InclLen < rec.OrigLen {
				getConn(h).Truncated++
			}
			if pb == 0x2 || pb == 0x0 { // start of an L2CAP PDU
				if len(payload) < 4 {
					continue
				}
				reasm[k] = append([]byte(nil), payload...)
				want[k] = int(binary.LittleEndian.Uint16(payload[0:2])) + 4
			} else if pb == 0x1 { // continuation
				if _, ok := reasm[k]; !ok {
					continue
				}
				reasm[k] = append(reasm[k], payload...)
			}
			buf := reasm[k]
			if len(buf) < want[k] {
				continue
			}
			delete(reasm, k)
			if binary.LittleEndian.Uint16(buf[2:4]) != l2capCIDATT {
				continue
			}
			att := buf[4:want[k]]
			if len(att) < 1 {
				continue
			}
			c := getConn(h)
			if c.FirstSeen.IsZero() {
				c.FirstSeen = rec.TS
			}
			op := att[0]
			switch op {
			case attReadByGroupResp: // services
				if len(att) < 2 {
					continue
				}
				n := int(att[1])
				for p := 2; n >= 4 && p+n <= len(att); p += n {
					start := binary.LittleEndian.Uint16(att[p:])
					c.UUIDByHandle[start] = "svc " + UUIDString(att[p+4:p+n])
				}
			case attReadByTypeResp: // characteristic declarations
				if len(att) < 2 {
					continue
				}
				n := int(att[1])
				for p := 2; n >= 7 && p+n <= len(att); p += n {
					vh := binary.LittleEndian.Uint16(att[p+3:])
					c.UUIDByHandle[vh] = UUIDString(att[p+5 : p+n])
				}
			case attFindInfoResp:
				if len(att) < 2 {
					continue
				}
				ul := 2
				if att[1] == 2 {
					ul = 16
				}
				for p := 2; p+2+ul <= len(att); p += 2 + ul {
					hh := binary.LittleEndian.Uint16(att[p:])
					if _, ok := c.UUIDByHandle[hh]; !ok {
						c.UUIDByHandle[hh] = UUIDString(att[p+2 : p+2+ul])
					}
				}
			case attWriteReq, attWriteCmd, attNotification, attIndication:
				if len(att) < 3 {
					continue
				}
				c.Values++
				vals = append(vals, ATTValue{
					TS: rec.TS, ConnHandle: h, ToDevice: !rec.Received, Op: op,
					Handle: binary.LittleEndian.Uint16(att[1:3]),
					Value:  append([]byte(nil), att[3:]...),
				})
			}
		}
	}
	return vals, truncated
}
