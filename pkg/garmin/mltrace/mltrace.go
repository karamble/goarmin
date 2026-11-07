// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package mltrace records the Multi-Link traffic of a live connection as JSON lines and reads
// such a trace back, also as Bluetooth ATT values that the btsnoop decoders understand.
//
// A trace holds one object per line:
//
//	{"layer":"meta","start":"<RFC 3339 time>"}           first line: when the trace started
//	{"layer":"ml","dir":"tx","hex":"...","t":1.25}      a Multi-Link packet, tx = to the watch
//	{"layer":"gfdi","dir":"rx","msg":"...","len":44,"t":1.25,"seq":3}   a GFDI frame (summary)
//
// t is seconds since the start. Traces written before the meta line existed have no start time.
package mltrace

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/karamble/goarmin/pkg/btsnoop"
	"github.com/karamble/goarmin/pkg/garmin/gfdi"
)

// Writer writes a trace; it is safe for concurrent use.
type Writer struct {
	mu    sync.Mutex
	enc   *json.Encoder
	start time.Time
	since func(time.Time) time.Duration
}

// NewWriter starts a trace on w that began at start.
func NewWriter(w io.Writer, start time.Time) *Writer {
	t := &Writer{enc: json.NewEncoder(w), start: start, since: time.Since}
	t.mu.Lock()
	_ = t.enc.Encode(map[string]any{"layer": "meta", "start": start.Format(time.RFC3339Nano)})
	t.mu.Unlock()
	return t
}

func (t *Writer) write(v map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v["t"] = t.since(t.start).Seconds()
	_ = t.enc.Encode(v)
}

// Packet records a Multi-Link packet; dir is "tx" (to the watch) or "rx". It has the signature
// of watch.Options.TracePacket.
func (t *Writer) Packet(dir string, pkt []byte) {
	t.write(map[string]any{"layer": "ml", "dir": dir, "hex": hex.EncodeToString(pkt)})
}

// Frame records a GFDI frame's ID, length and compact sequence number. It has the signature of
// watch.Options.TraceFrame.
func (t *Writer) Frame(dir string, f gfdi.Frame) {
	v := map[string]any{"layer": "gfdi", "dir": dir, "msg": f.ID.String(), "len": len(f.Payload)}
	if f.Compact {
		v["seq"] = f.Seq
	}
	t.write(v)
}

// Packet is a Multi-Link packet read back from a trace.
type Packet struct {
	At       time.Duration // since the trace started
	ToDevice bool
	Value    []byte
}

// Trace is a trace read back.
type Trace struct {
	// Start is when the trace started; zero for traces without a meta line.
	Start   time.Time
	Packets []Packet
}

// Read reads a trace. GFDI summary lines are skipped: the packets hold everything.
func Read(r io.Reader) (Trace, error) {
	var tr Trace
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for n := 1; sc.Scan(); n++ {
		var l struct {
			Layer, Dir, Hex, Start string
			T                      float64
		}
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			return tr, fmt.Errorf("mltrace: line %d: %w", n, err)
		}
		switch l.Layer {
		case "meta":
			st, err := time.Parse(time.RFC3339Nano, l.Start)
			if err != nil {
				return tr, fmt.Errorf("mltrace: line %d: %w", n, err)
			}
			tr.Start = st
		case "ml":
			b, err := hex.DecodeString(l.Hex)
			if err != nil {
				return tr, fmt.Errorf("mltrace: line %d: %w", n, err)
			}
			if l.Dir != "tx" && l.Dir != "rx" {
				return tr, fmt.Errorf("mltrace: line %d: direction %q", n, l.Dir)
			}
			tr.Packets = append(tr.Packets, Packet{At: time.Duration(l.T * float64(time.Second)),
				ToDevice: l.Dir == "tx", Value: b})
		}
	}
	return tr, sc.Err()
}

// The ATT connection and handles ATT gives the trace: one connection, the write and notify
// characteristics of Multi-Link pair 0.
const (
	ATTConn     uint16 = 0x0040
	ATTTxHandle uint16 = 0x0010 // 6a4e2820, written by the host
	ATTRxHandle uint16 = 0x0012 // 6a4e2810, notified by the watch
)

// ATT returns the packets as the ATT values of one connection (ATTConn) that a btsnoop log of
// the same traffic would hold, with the GATT knowledge that names the Multi-Link
// characteristics. Timestamps are start plus each packet's offset; pass tr.Start when it is
// set.
func (tr Trace) ATT(start time.Time) ([]btsnoop.ATTValue, map[uint16]*btsnoop.ConnInfo) {
	vals := make([]btsnoop.ATTValue, 0, len(tr.Packets))
	for _, p := range tr.Packets {
		v := btsnoop.ATTValue{TS: start.Add(p.At), ConnHandle: ATTConn, ToDevice: p.ToDevice, Value: p.Value}
		if p.ToDevice {
			v.Op, v.Handle = attWriteCmd, ATTTxHandle
		} else {
			v.Op, v.Handle = attNotify, ATTRxHandle
		}
		vals = append(vals, v)
	}
	info := &btsnoop.ConnInfo{
		UUIDByHandle: map[uint16]string{
			ATTTxHandle: "6a4e2820-667b-11e3-949a-0800200c9a66",
			ATTRxHandle: "6a4e2810-667b-11e3-949a-0800200c9a66",
		},
		Values:    len(vals),
		FirstSeen: start,
	}
	return vals, map[uint16]*btsnoop.ConnInfo{ATTConn: info}
}

// ATT opcodes the values carry.
const (
	attWriteCmd = 0x52
	attNotify   = 0x1b
)
