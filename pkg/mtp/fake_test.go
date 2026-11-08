// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mtp

import (
	"bytes"
	"unicode/utf16"
)

// fakeObj is one object on the fake device; parent 0 is the top of its storage.
type fakeObj struct {
	handle, parent, storage uint32
	name                    string
	folder                  bool
	data                    []byte
	mod                     string
}

// fakeDevice answers PTP operations from an in-memory tree, transfer by transfer, the way a
// device on USB would. Its knobs inject the ways real devices differ or fail.
type fakeDevice struct {
	storages []uint32
	objs     []*fakeObj
	info     DeviceInfo

	chunk       int            // bytes per transfer (0: a container in one transfer)
	zlp         bool           // an empty transfer after a data container of whole chunks
	merge       bool           // the response arrives in the data's last transfer
	busy        map[uint16]int // op → how many more times to answer DeviceBusy
	alreadyOpen bool           // OpenSession answers SessionAlreadyOpen
	wrongTID    bool           // responses carry the wrong transaction id
	short       bool           // the next transfer is 5 bytes
	stopAfter   int            // Receive times out after this many transfers (0: never)
	rootRefused bool           // GetObjectHandles with Root answers InvalidParentObject

	out    [][]byte
	ops    []uint16
	tids   []uint32
	recvs  int
	open   bool
	closed bool
}

func (f *fakeDevice) Send(p []byte) error {
	h, err := parseHeader(p)
	if err != nil || h.Type != typeCommand {
		return ErrProtocol
	}
	var params []uint32
	for i := headerLen; i+4 <= int(h.Length) && i+4 <= len(p); i += 4 {
		params = append(params, le.Uint32(p[i:]))
	}
	f.ops = append(f.ops, h.Code)
	f.tids = append(f.tids, h.TID)
	tid := h.TID
	if f.wrongTID {
		tid++
	}
	if f.busy[h.Code] > 0 {
		f.busy[h.Code]--
		f.reply(h.Code, tid, nil, RespDeviceBusy)
		return nil
	}
	arg := func(i int) uint32 {
		if i < len(params) {
			return params[i]
		}
		return 0
	}
	switch h.Code {
	case opGetDeviceInfo:
		f.reply(h.Code, tid, encodeDeviceInfo(f.info), RespOK)
	case opOpenSession:
		if f.alreadyOpen {
			f.reply(h.Code, tid, nil, RespSessionAlreadyOpen)
			return nil
		}
		f.open = true
		f.reply(h.Code, tid, nil, RespOK)
	case opCloseSession:
		f.open = false
		f.reply(h.Code, tid, nil, RespOK)
	case opGetStorageIDs:
		w := &enc{}
		w.u32(uint32(len(f.storages)))
		for _, s := range f.storages {
			w.u32(s)
		}
		f.reply(h.Code, tid, w.Bytes(), RespOK)
	case opGetStorageInfo:
		w := &enc{}
		w.u16(3)
		w.u16(2)
		w.u16(0)
		w.u64(32 << 30)
		w.u64(20 << 30)
		w.u32(0xFFFFFFFF)
		w.str("Primary")
		w.str("GARMIN")
		f.reply(h.Code, tid, w.Bytes(), RespOK)
	case opGetObjectHandles:
		storage, parent := arg(0), arg(2)
		if parent == Root && f.rootRefused {
			f.reply(h.Code, tid, nil, RespInvalidParentObject)
			return nil
		}
		var hs []uint32
		for _, o := range f.objs {
			if storage != 0xFFFFFFFF && o.storage != storage {
				continue
			}
			if parent == 0 || (parent == Root && o.parent == 0) || o.parent == parent {
				hs = append(hs, o.handle)
			}
		}
		w := &enc{}
		w.u32(uint32(len(hs)))
		for _, x := range hs {
			w.u32(x)
		}
		f.reply(h.Code, tid, w.Bytes(), RespOK)
	case opGetObjectInfo:
		o := f.obj(arg(0))
		if o == nil {
			f.reply(h.Code, tid, nil, RespInvalidObjectHandle)
			return nil
		}
		f.reply(h.Code, tid, encodeObjectInfo(o), RespOK)
	case opGetObject:
		o := f.obj(arg(0))
		if o == nil || o.folder {
			f.reply(h.Code, tid, nil, RespInvalidObjectHandle)
			return nil
		}
		f.reply(h.Code, tid, o.data, RespOK)
	default:
		f.reply(h.Code, tid, nil, RespOperationNotSupported)
	}
	return nil
}

func (f *fakeDevice) obj(h uint32) *fakeObj {
	for _, o := range f.objs {
		if o.handle == h {
			return o
		}
	}
	return nil
}

// reply queues the data container (when data is not nil) and the response, cut into
// transfers.
func (f *fakeDevice) reply(op uint16, tid uint32, data []byte, code uint16) {
	resp := container(typeResponse, code, tid, nil)
	if data == nil {
		f.out = append(f.out, resp)
		return
	}
	c := container(typeData, op, tid, data)
	var parts [][]byte
	if f.chunk <= 0 {
		parts = [][]byte{c}
	} else {
		for len(c) > 0 {
			n := min(f.chunk, len(c))
			parts = append(parts, c[:n])
			c = c[n:]
		}
	}
	whole := f.chunk > 0 && len(parts[len(parts)-1]) == f.chunk
	switch {
	case f.merge:
		last := append(append([]byte(nil), parts[len(parts)-1]...), resp...)
		parts[len(parts)-1] = last
		f.out = append(f.out, parts...)
	case f.zlp && whole:
		f.out = append(append(f.out, parts...), []byte{}, resp)
	default:
		f.out = append(append(f.out, parts...), resp)
	}
}

func (f *fakeDevice) Receive(p []byte) (int, error) {
	f.recvs++
	if f.stopAfter > 0 && f.recvs > f.stopAfter {
		return 0, ErrTimeout
	}
	if f.short {
		f.short = false
		return copy(p, []byte{1, 2, 3, 4, 5}), nil
	}
	if len(f.out) == 0 {
		return 0, ErrTimeout
	}
	b := f.out[0]
	n := copy(p, b)
	if n < len(b) {
		f.out[0] = b[n:]
	} else {
		f.out = f.out[1:]
	}
	return n, nil
}

func (f *fakeDevice) Close() error {
	f.closed = true
	return nil
}

func container(typ, code uint16, tid uint32, payload []byte) []byte {
	b := make([]byte, headerLen+len(payload))
	le.PutUint32(b[0:], uint32(len(b)))
	le.PutUint16(b[4:], typ)
	le.PutUint16(b[6:], code)
	le.PutUint32(b[8:], tid)
	copy(b[headerLen:], payload)
	return b
}

// enc writes PTP datasets.
type enc struct{ bytes.Buffer }

func (e *enc) u16(v uint16) { e.Write(le.AppendUint16(nil, v)) }
func (e *enc) u32(v uint32) { e.Write(le.AppendUint32(nil, v)) }
func (e *enc) u64(v uint64) { e.Write(le.AppendUint64(nil, v)) }

func (e *enc) str(s string) {
	if s == "" {
		e.WriteByte(0)
		return
	}
	units := append(utf16.Encode([]rune(s)), 0)
	e.WriteByte(byte(len(units)))
	for _, u := range units {
		e.u16(u)
	}
}

func (e *enc) u16s(vs []uint16) {
	e.u32(uint32(len(vs)))
	for _, v := range vs {
		e.u16(v)
	}
}

func encodeDeviceInfo(i DeviceInfo) []byte {
	e := &enc{}
	e.u16(i.StandardVersion)
	e.u32(i.VendorExtensionID)
	e.u16(i.VendorExtensionVersion)
	e.str(i.VendorExtensionDesc)
	e.u16(i.FunctionalMode)
	e.u16s(i.Operations)
	e.u16s(i.Events)
	e.u16s(i.Properties)
	e.u16s(i.CaptureFormats)
	e.u16s(i.PlaybackFormats)
	e.str(i.Manufacturer)
	e.str(i.Model)
	e.str(i.DeviceVersion)
	e.str(i.SerialNumber)
	return e.Bytes()
}

func encodeObjectInfo(o *fakeObj) []byte {
	e := &enc{}
	e.u32(o.storage)
	format, assoc := FormatUndefined, uint16(0)
	if o.folder {
		format, assoc = FormatAssociation, associationFolder
	}
	e.u16(format)
	e.u16(0)
	e.u32(uint32(len(o.data)))
	e.u16(0)
	for i := 0; i < 6; i++ {
		e.u32(0) // thumb size, width, height; image width, height, depth
	}
	e.u32(o.parent)
	e.u16(assoc)
	e.u32(0)
	e.u32(0)
	e.str(o.name)
	e.str("")
	e.str(o.mod)
	e.str("")
	return e.Bytes()
}
