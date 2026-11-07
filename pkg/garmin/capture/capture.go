// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package capture rebuilds Garmin Multi-Link traffic from the ATT values of a Bluetooth HCI
// snoop log (pkg/btsnoop):
//   - control messages;
//   - the GFDI frames of each direction (MLR de-duplicated, COBS unframed, CRC checked);
//   - reassembled protobuf messages;
//   - the bytes of every file-pipe transfer.
//
// It reads passively, so unlike the live stack it never acknowledges or retransmits.
package capture

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/karamble/goarmin/pkg/btsnoop"
	"github.com/karamble/goarmin/pkg/cobs"
	"github.com/karamble/goarmin/pkg/garmin/gfdi"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
)

// Control is a Multi-Link control message.
type Control struct {
	TS       time.Time
	ToDevice bool
	multilink.Control
}

// Frame is one GFDI frame.
type Frame struct {
	TS       time.Time
	ToDevice bool
	gfdi.Frame
}

// Transfer is one file-pipe transfer: the bytes after the 6-byte configure (push) or after the
// watch's 3-byte reply (pull). They are zlib-compressed.
type Transfer struct {
	TS      time.Time
	Service multilink.Service
	Handle  byte
	Push    bool
	Token   uint16
	Reply   []byte
	Data    []byte
}

// Inflate decompresses the transfer.
func (t Transfer) Inflate() ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(t.Data))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(zr)
}

// Result is everything decoded from one connection.
type Result struct {
	Controls  []Control
	Frames    []Frame
	Transfers []Transfer
	// FrameErrors counts COBS segments or GFDI frames that failed to decode (bad CRC,
	// length, COBS code).
	FrameErrors int
	// Duplicates counts MLR packets dropped as retransmissions or out of order.
	Duplicates int
	// Unknown counts packets on handles that were never registered.
	Unknown int
}

// GarminConnections returns the connections that carry Multi-Link traffic: values on
// 6a4e28xx characteristics.
func GarminConnections(vals []btsnoop.ATTValue, conns map[uint16]*btsnoop.ConnInfo) []uint16 {
	seen := map[uint16]bool{}
	var out []uint16
	for _, v := range vals {
		if seen[v.ConnHandle] || !isML(conns, v) {
			continue
		}
		seen[v.ConnHandle] = true
		out = append(out, v.ConnHandle)
	}
	return out
}

func isML(conns map[uint16]*btsnoop.ConnInfo, v btsnoop.ATTValue) bool {
	c := conns[v.ConnHandle]
	if c == nil {
		return false
	}
	return strings.HasPrefix(c.UUIDByHandle[v.Handle], "6a4e28")
}

type handleInfo struct {
	svc      multilink.Service
	reliable bool
}

type dirKey struct {
	handle   byte
	toDevice bool
}

type decoder struct {
	res     Result
	handles map[byte]handleInfo
	expect  map[dirKey]int
	cobs    map[bool]*cobs.StreamDecoder
	pipes   map[byte]*Transfer
}

// Decode rebuilds the Multi-Link traffic of connection conn.
func Decode(vals []btsnoop.ATTValue, conns map[uint16]*btsnoop.ConnInfo, conn uint16) Result {
	d := &decoder{
		handles: map[byte]handleInfo{}, expect: map[dirKey]int{},
		cobs:  map[bool]*cobs.StreamDecoder{true: {}, false: {}},
		pipes: map[byte]*Transfer{},
	}
	for _, v := range vals {
		if v.ConnHandle != conn || !isML(conns, v) || len(v.Value) == 0 {
			continue
		}
		if multilink.IsControl(v.Value) {
			d.control(v)
			continue
		}
		d.data(v)
	}
	for h := range d.pipes {
		d.finishPipe(h)
	}
	return d.res
}

func (d *decoder) control(v btsnoop.ATTValue) {
	c, err := multilink.ParseControl(v.Value)
	if err != nil {
		return
	}
	d.res.Controls = append(d.res.Controls, Control{TS: v.TS, ToDevice: v.ToDevice, Control: c})
	switch c.Type {
	case multilink.RegisterResponse:
		if c.Status != 0 {
			return
		}
		d.finishPipe(c.Handle)
		// A new registration restarts the handle's MLR sequence numbers.
		delete(d.expect, dirKey{c.Handle, true})
		delete(d.expect, dirKey{c.Handle, false})
		d.handles[c.Handle] = handleInfo{svc: c.Service, reliable: c.Reliable}
	case multilink.CloseHandleResponse:
		d.finishPipe(c.Handle)
	}
}

func (d *decoder) data(v btsnoop.ATTValue) {
	b := v.Value
	var h byte
	var payload []byte
	if multilink.IsMLR(b[0]) && len(b) >= 2 {
		hdr := multilink.ParseHeader(b[0], b[1])
		if hi, ok := d.handles[hdr.Handle]; ok && hi.reliable {
			if len(b) == 2 {
				return // ACK only
			}
			k := dirKey{hdr.Handle, v.ToDevice}
			if hdr.SN != d.expect[k] {
				d.res.Duplicates++
				return
			}
			d.expect[k] = (hdr.SN + 1) % multilink.SeqModulus
			h, payload = hdr.Handle, b[2:]
		}
	}
	if payload == nil {
		if _, ok := d.handles[b[0]]; !ok {
			d.res.Unknown++
			return
		}
		h, payload = b[0], b[1:]
	}
	hi := d.handles[h]
	switch {
	case hi.svc == multilink.ServiceGFDI:
		frames, err := d.cobs[v.ToDevice].Feed(payload)
		if err != nil {
			d.res.FrameErrors++
		}
		for _, raw := range frames {
			f, err := gfdi.ParseFrame(raw)
			if err != nil {
				d.res.FrameErrors++
				continue
			}
			d.res.Frames = append(d.res.Frames, Frame{TS: v.TS, ToDevice: v.ToDevice, Frame: f})
		}
	case hi.svc&0x0FFF == 0x018:
		d.pipeData(v, h, hi, payload)
	}
}

// pipeData follows a pipe: 00 <dir> <token u16> 00 00 from the host, a 3-byte reply from the
// watch, then zlib data in the transfer's direction.
func (d *decoder) pipeData(v btsnoop.ATTValue, h byte, hi handleInfo, p []byte) {
	t := d.pipes[h]
	if v.ToDevice && t == nil && len(p) >= 6 && p[0] == 0 && p[1] <= 1 && p[4] == 0 && p[5] == 0 {
		t = &Transfer{TS: v.TS, Service: hi.svc, Handle: h, Push: p[1] == 1, Token: binary.LittleEndian.Uint16(p[2:])}
		d.pipes[h] = t
		p = p[6:]
	}
	if t == nil {
		return
	}
	if !v.ToDevice && len(t.Reply) < 3 {
		n := min(3-len(t.Reply), len(p))
		t.Reply, p = append(t.Reply, p[:n]...), p[n:]
	}
	if v.ToDevice == t.Push {
		t.Data = append(t.Data, p...)
	}
}

func (d *decoder) finishPipe(h byte) {
	if t := d.pipes[h]; t != nil {
		delete(d.pipes, h)
		d.res.Transfers = append(d.res.Transfers, *t)
	}
}

// Message is a reassembled protobuf message.
type Message struct {
	TS        time.Time
	ToDevice  bool
	Kind      gfdi.MessageID
	RequestID uint16
	Data      []byte
}

// Messages reassembles the protobuf messages among frames. errs counts chunks that did not
// fit (bad header, wrong offset).
func Messages(frames []Frame) (msgs []Message, errs int) {
	type key struct {
		toDevice bool
		kind     gfdi.MessageID
		id       uint16
	}
	buf := map[key][]byte{}
	for _, f := range frames {
		if f.ID != gfdi.ProtobufRequest && f.ID != gfdi.ProtobufResponse {
			continue
		}
		c, err := gfdi.ParseProtobufChunk(f.Payload)
		if err != nil {
			errs++
			continue
		}
		k := key{f.ToDevice, f.ID, c.RequestID}
		switch {
		case c.Offset == 0:
			buf[k] = append([]byte(nil), c.Data...)
		case uint32(len(buf[k])) == c.Offset:
			buf[k] = append(buf[k], c.Data...)
		case uint32(len(buf[k])) > c.Offset:
			continue // a resent chunk
		default:
			errs++
			delete(buf, k)
			continue
		}
		if uint32(len(buf[k])) == c.Total {
			msgs = append(msgs, Message{TS: f.TS, ToDevice: f.ToDevice, Kind: f.ID, RequestID: c.RequestID, Data: buf[k]})
			delete(buf, k)
		}
	}
	return msgs, errs
}

// String summarises a result.
func (r Result) String() string {
	return fmt.Sprintf("%d control messages, %d GFDI frames, %d transfers, %d frame errors, %d MLR duplicates, %d unknown",
		len(r.Controls), len(r.Frames), len(r.Transfers), r.FrameErrors, r.Duplicates, r.Unknown)
}
