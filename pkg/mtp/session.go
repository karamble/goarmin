// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mtp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"
)

// Transport carries PTP containers. One call is one USB bulk transfer: Send writes a command,
// Receive reads up to len(p) bytes and returns early at the end of a transfer (a short or
// empty packet).
type Transport interface {
	Send(p []byte) error
	Receive(p []byte) (int, error)
	Close() error
}

// bufSize is the read buffer: a multiple of every USB packet size, large enough that a FIT
// file takes a few transfers.
const bufSize = 128 << 10

// maxDepth bounds folder recursion in Walk (a device listing a folder inside itself).
const maxDepth = 24

// Session is an open MTP session. It is not safe for concurrent use.
type Session struct {
	t    Transport
	buf  []byte
	pend []byte // bytes of the next container that arrived with the previous one
	tid  uint32
	open bool
	bad  error // set once the device and the session disagree; every later call fails

	// Info is what the device said about itself when the session opened.
	Info DeviceInfo
	// Location interprets dates without a zone (the device's clock); time.Local by default.
	Location *time.Location
}

// NewSession reads the device's info and opens a session over t. On error it closes t.
func NewSession(ctx context.Context, t Transport) (*Session, error) {
	s := &Session{t: t, buf: make([]byte, bufSize), Location: time.Local}
	var info bytes.Buffer
	if _, err := s.do(ctx, opGetDeviceInfo, nil, &info); err != nil {
		t.Close()
		return nil, err
	}
	di, err := parseDeviceInfo(info.Bytes())
	if err != nil {
		t.Close()
		return nil, err
	}
	s.Info = di
	if _, err := s.do(ctx, opOpenSession, []uint32{1}, nil); err != nil {
		var re *ResponseError
		// A session left open by an earlier program that ended without closing it is usable.
		if !errors.As(err, &re) || re.Code != RespSessionAlreadyOpen {
			t.Close()
			return nil, err
		}
	}
	s.open = true
	return s, nil
}

// Close ends the session and releases the device.
func (s *Session) Close() error {
	if s.open && s.bad == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, _ = s.do(ctx, opCloseSession, nil, nil)
		cancel()
	}
	s.open = false
	return s.t.Close()
}

// StorageIDs lists the device's storages that are present (a storage id whose low half is 0
// names a slot without a medium).
func (s *Session) StorageIDs(ctx context.Context) ([]uint32, error) {
	var b bytes.Buffer
	if _, err := s.do(ctx, opGetStorageIDs, nil, &b); err != nil {
		return nil, err
	}
	d := &dataset{b: b.Bytes()}
	ids := d.u32s()
	if d.err != nil {
		return nil, fmt.Errorf("mtp: storage ids: %w", d.err)
	}
	out := ids[:0]
	for _, id := range ids {
		if id&0xFFFF != 0 {
			out = append(out, id)
		}
	}
	return out, nil
}

// StorageInfo describes one storage.
func (s *Session) StorageInfo(ctx context.Context, storage uint32) (StorageInfo, error) {
	var b bytes.Buffer
	if _, err := s.do(ctx, opGetStorageInfo, []uint32{storage}, &b); err != nil {
		return StorageInfo{}, err
	}
	return parseStorageInfo(b.Bytes())
}

// Root is the parent handle that lists the top of a storage.
const Root uint32 = 0xFFFFFFFF

// Handles lists the objects in a folder (parent), or at the top of the storage (Root).
func (s *Session) Handles(ctx context.Context, storage, parent uint32) ([]uint32, error) {
	var b bytes.Buffer
	if _, err := s.do(ctx, opGetObjectHandles, []uint32{storage, 0, parent}, &b); err != nil {
		return nil, err
	}
	d := &dataset{b: b.Bytes()}
	hs := d.u32s()
	if d.err != nil {
		return nil, fmt.Errorf("mtp: object handles: %w", d.err)
	}
	return hs, nil
}

// ObjectInfo describes one object.
func (s *Session) ObjectInfo(ctx context.Context, handle uint32) (ObjectInfo, error) {
	var b bytes.Buffer
	if _, err := s.do(ctx, opGetObjectInfo, []uint32{handle}, &b); err != nil {
		return ObjectInfo{}, err
	}
	return parseObjectInfo(b.Bytes(), s.Location)
}

// Get downloads an object into w and returns how many bytes it wrote. If w fails, the rest
// of the transfer is read and dropped, so the session stays usable, and w's error returned.
func (s *Session) Get(ctx context.Context, handle uint32, w io.Writer) (int64, error) {
	cw := &countWriter{w: w}
	_, err := s.do(ctx, opGetObject, []uint32{handle}, cw)
	return cw.n, err
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// Entry is one object found by Walk.
type Entry struct {
	Storage uint32
	Handle  uint32
	// Path is slash-separated from the top of the storage ("GARMIN/Activity/x.fit").
	Path     string
	Name     string
	Folder   bool
	Size     int64 // -1 when the device reports 4 GiB or more
	Modified time.Time
}

// Walk calls fn for every object under root on every storage, folders before what they hold.
// root is a slash-separated folder path matched without regard to case ("GARMIN"); "" walks
// each storage from the top. A storage without root is skipped. If fn returns fs.SkipDir for
// a folder, Walk does not enter it, and for a file it skips the rest of that folder; fs.SkipAll
// ends the walk without an error, and any other error ends it with that error. Objects whose names could
// not be a file name ("", ".", "..", or with a slash or NUL) are left out.
func (s *Session) Walk(ctx context.Context, root string, fn func(Entry) error) error {
	ids, err := s.StorageIDs(ctx)
	if err != nil {
		return err
	}
	var parts []string
	for _, p := range strings.Split(strings.Trim(root, "/"), "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	for _, id := range ids {
		parent, prefix, found, err := s.find(ctx, id, parts)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		seen := map[uint32]bool{}
		if err := s.walk(ctx, id, parent, prefix, 0, seen, fn); err != nil {
			if errors.Is(err, fs.SkipAll) {
				return nil
			}
			return err
		}
	}
	return nil
}

// find resolves a folder path from the top of a storage.
func (s *Session) find(ctx context.Context, storage uint32, parts []string) (handle uint32, prefix string, found bool, err error) {
	handle = Root
	for _, want := range parts {
		hs, err := s.children(ctx, storage, handle)
		if err != nil {
			return 0, "", false, err
		}
		match := false
		for _, h := range hs {
			o, err := s.ObjectInfo(ctx, h)
			if err != nil {
				return 0, "", false, err
			}
			if o.Folder() && strings.EqualFold(o.Name, want) {
				handle, prefix, match = h, path.Join(prefix, o.Name), true
				break
			}
		}
		if !match {
			return 0, "", false, nil
		}
	}
	return handle, prefix, true, nil
}

// children lists a folder. A device that refuses Root for the top of a storage is asked for
// every object instead, keeping those whose parent is 0.
func (s *Session) children(ctx context.Context, storage, parent uint32) ([]uint32, error) {
	hs, err := s.Handles(ctx, storage, parent)
	var re *ResponseError
	if parent != Root || !errors.As(err, &re) || (re.Code != RespInvalidParentObject && re.Code != RespParameterNotSupported) {
		return hs, err
	}
	all, err := s.Handles(ctx, storage, 0)
	if err != nil {
		return nil, err
	}
	var top []uint32
	for _, h := range all {
		o, err := s.ObjectInfo(ctx, h)
		if err != nil {
			return nil, err
		}
		if o.Parent == 0 || o.Parent == Root {
			top = append(top, h)
		}
	}
	return top, nil
}

func (s *Session) walk(ctx context.Context, storage, parent uint32, prefix string, depth int, seen map[uint32]bool, fn func(Entry) error) error {
	if depth > maxDepth {
		return nil
	}
	hs, err := s.children(ctx, storage, parent)
	if err != nil {
		return err
	}
	for _, h := range hs {
		if seen[h] {
			continue
		}
		seen[h] = true
		o, err := s.ObjectInfo(ctx, h)
		if err != nil {
			return err
		}
		if !safeName(o.Name) {
			continue
		}
		e := Entry{Storage: storage, Handle: h, Path: path.Join(prefix, o.Name), Name: o.Name, Folder: o.Folder(), Size: o.Size, Modified: o.Modified}
		err = fn(e)
		if errors.Is(err, fs.SkipDir) {
			if e.Folder {
				continue
			}
			return nil // as filepath.WalkDir: skip the rest of this folder
		}
		if err != nil {
			return err
		}
		if e.Folder {
			if err := s.walk(ctx, storage, h, e.Path, depth+1, seen, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

func safeName(n string) bool {
	return n != "" && n != "." && n != ".." && !strings.ContainsAny(n, "/\x00")
}

// do runs one transaction: the command, the data the device sends (into w; nil drops it) and
// the response. It returns the response's parameters.
func (s *Session) do(ctx context.Context, op uint16, params []uint32, w io.Writer) ([]uint32, error) {
	if s.bad != nil {
		return nil, s.bad
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Transactions outside a session, OpenSession included, use id 0; inside, 1, 2, …
	var tid uint32
	if s.open {
		s.tid++
		if s.tid == 0 || s.tid == 0xFFFFFFFF {
			s.tid = 1
		}
		tid = s.tid
	}
	if err := s.t.Send(command(op, tid, params...)); err != nil {
		return nil, s.fail(err)
	}
	b, err := s.recv()
	if err != nil {
		return nil, s.fail(err)
	}
	h, err := parseHeader(b)
	if err != nil {
		return nil, s.fail(err)
	}
	var werr error
	if h.Type == typeData {
		if h.Code != op || h.TID != tid {
			return nil, s.fail(fmt.Errorf("%w: data for 0x%04X/%d during 0x%04X/%d", ErrProtocol, h.Code, h.TID, op, tid))
		}
		if werr, err = s.data(ctx, h, b[headerLen:], w); err != nil {
			return nil, s.fail(err)
		}
		if b, err = s.recv(); err != nil {
			return nil, s.fail(err)
		}
		if h, err = parseHeader(b); err != nil {
			return nil, s.fail(err)
		}
	}
	if h.Type != typeResponse || h.TID != tid {
		return nil, s.fail(fmt.Errorf("%w: container type %d for transaction %d during 0x%04X/%d", ErrProtocol, h.Type, h.TID, op, tid))
	}
	if int(h.Length) > len(b) || (h.Length-headerLen)%4 != 0 || h.Length > headerLen+5*4 {
		return nil, s.fail(fmt.Errorf("%w: response of %d bytes", ErrProtocol, h.Length))
	}
	if int(h.Length) < len(b) {
		s.pend = append([]byte(nil), b[h.Length:]...)
	}
	out := make([]uint32, (h.Length-headerLen)/4)
	for i := range out {
		out[i] = le.Uint32(b[headerLen+4*i:])
	}
	if h.Code != RespOK {
		return out, &ResponseError{Op: op, Code: h.Code}
	}
	return out, werr
}

// data reads a data phase whose container header is h and whose first bytes are first. A
// write error is returned separately: the transfer is still read to its end.
func (s *Session) data(ctx context.Context, h header, first []byte, w io.Writer) (werr, err error) {
	if w == nil {
		w = io.Discard
	}
	unknown := h.Length == 0xFFFFFFFF // 4 GiB or more: read until a transfer ends short
	left := int64(h.Length) - headerLen
	chunk := first
	full := len(first)+headerLen == len(s.buf)
	for {
		if !unknown && int64(len(chunk)) > left {
			s.pend = append([]byte(nil), chunk[left:]...)
			chunk = chunk[:left]
		}
		if werr == nil && len(chunk) > 0 {
			if _, e := w.Write(chunk); e != nil {
				werr, w = e, io.Discard
			}
		}
		left -= int64(len(chunk))
		if (!unknown && left <= 0) || (unknown && !full) {
			return werr, nil
		}
		if err := ctx.Err(); err != nil {
			return werr, err
		}
		if chunk, err = s.recv(); err != nil {
			return werr, err
		}
		full = len(chunk) == len(s.buf)
	}
}

// recv returns the start of the next container: bytes left over from the last transfer, or
// the next transfer, skipping the empty packets that end a transfer of a full length.
func (s *Session) recv() ([]byte, error) {
	if len(s.pend) > 0 {
		b := s.pend
		s.pend = nil
		return b, nil
	}
	for zeros := 0; zeros < 4; zeros++ {
		n, err := s.t.Receive(s.buf)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			return s.buf[:n], nil
		}
	}
	return nil, fmt.Errorf("%w: only empty transfers", ErrProtocol)
}

func (s *Session) fail(err error) error {
	if s.bad == nil {
		s.bad = err
	}
	return err
}
