// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gfdi

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/karamble/goarmin/pkg/clock"
	"github.com/karamble/goarmin/pkg/cobs"
)

// Transport carries a GFDI byte stream. A multilink.Channel satisfies it. Recv is closed when
// the stream ends.
type Transport interface {
	Write(data []byte) error
	Recv() <-chan []byte
}

// CompactMode selects the frame form of the session's own requests.
type CompactMode int

const (
	// CompactAuto sends plain frames until the peer sends a compact one, then compact frames,
	// as Garmin Connect does.
	CompactAuto CompactMode = iota
	// CompactAlways sends compact frames from the start.
	CompactAlways
	// CompactNever always sends plain frames (Gadgetbridge's behaviour).
	CompactNever
)

// compactSeqModulus is where compact sequence numbers wrap: Garmin Connect and the Fenix 8
// count 0..31 (the capture in docs/protocol/05).
const compactSeqModulus = 32

// Errors returned by a Session.
var (
	ErrSessionClosed = errors.New("gfdi: session closed")
	ErrTimeout       = errors.New("gfdi: timed out")
)

// ProtobufMessage is a complete, reassembled PROTOBUF_REQUEST or PROTOBUF_RESPONSE.
type ProtobufMessage struct {
	Kind      MessageID // ProtobufRequest or ProtobufResponse
	RequestID uint16
	Data      []byte // the serialized Smart message
}

// ProtobufReply tells the session how to acknowledge a protobuf message from the peer and
// whether to answer it.
type ProtobufReply struct {
	ChunkStatus byte // ChunkKept or ChunkDiscarded
	Code        byte // ProtobufNoError, ProtobufUnknownRequest, …
	// Response, if not nil, is sent as a PROTOBUF_RESPONSE with the message's request ID
	// after the acknowledgement.
	Response []byte
}

// Kept is the reply for a message that was handled; resp may be nil.
func Kept(resp []byte) ProtobufReply { return ProtobufReply{Response: resp} }

// Discarded is the reply for a message nobody handles, as Gadgetbridge sends it.
func Discarded() ProtobufReply {
	return ProtobufReply{ChunkStatus: ChunkDiscarded, Code: ProtobufUnknownRequest}
}

// SessionOptions configures a Session.
type SessionOptions struct {
	Compact CompactMode
	// Clock drives timeouts; clock.Real() by default.
	Clock clock.Clock
	// Logger receives debug messages; discarded by default.
	Logger *slog.Logger
	// RequestTimeout bounds the wait for a RESPONSE (15 s by default).
	RequestTimeout time.Duration
	// CallTimeout bounds the wait for the PROTOBUF_RESPONSE to a Call (30 s by default).
	CallTimeout time.Duration
	// MaxPacket is the peer's max packet size until SetMaxPacket is called (375 by default,
	// Gadgetbridge's value).
	MaxPacket int
	// MaxMessage caps a reassembled protobuf message (4 MiB by default).
	MaxMessage int
	// OnFrame receives every frame from the peer except RESPONSEs and protobuf messages. It
	// runs on the session's dispatch goroutine, one frame at a time, and must reply itself
	// (Reply). Nil acknowledges every frame with a bare ACK.
	OnFrame func(s *Session, f Frame)
	// OnProtobuf receives every complete protobuf message from the peer that is not the
	// answer to a Call, on the dispatch goroutine. Nil discards them.
	OnProtobuf func(s *Session, m ProtobufMessage) ProtobufReply
	// Trace, if set, sees every frame sent ("tx") and received ("rx").
	Trace func(dir string, f Frame)
}

// Session is one GFDI conversation over a Transport. It frames and unframes messages (COBS,
// length, CRC), matches RESPONSEs to requests, chunks and reassembles protobuf messages, and
// hands everything unsolicited to the handlers in order. Both ends of a link can use it: the
// fake watch runs one too.
type Session struct {
	t   Transport
	opt SessionOptions
	log *slog.Logger

	wmu sync.Mutex // serialises frame writes

	mu        sync.Mutex
	compact   bool
	nextSeq   int
	waiters   []*respWaiter
	nextReqID uint16
	calls     map[uint16]chan ProtobufMessage
	maxPacket int
	reasm     map[reasmKey]*reassembly
	recent    []recentKey
	err       error

	dmu    sync.Mutex
	dcond  *sync.Cond
	dq     []func()
	dclose bool

	done chan struct{}
	once sync.Once
	wg   sync.WaitGroup
}

type respWaiter struct {
	id      MessageID
	compact bool
	seq     uint8
	match   func(ResponseMessage) bool
	ch      chan ResponseMessage
}

type reasmKey struct {
	kind MessageID
	id   uint16
}

type reassembly struct {
	total uint32
	buf   []byte
}

type recentKey struct {
	kind MessageID
	id   uint16
	sum  uint32
	n    int
}

// NewSession starts a session on t.
func NewSession(t Transport, opt SessionOptions) *Session {
	if opt.Clock == nil {
		opt.Clock = clock.Real()
	}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opt.RequestTimeout <= 0 {
		opt.RequestTimeout = 15 * time.Second
	}
	if opt.CallTimeout <= 0 {
		opt.CallTimeout = 30 * time.Second
	}
	if opt.MaxPacket <= 0 {
		opt.MaxPacket = 375
	}
	if opt.MaxMessage <= 0 {
		opt.MaxMessage = 4 << 20
	}
	s := &Session{
		t: t, opt: opt, log: opt.Logger,
		compact:   opt.Compact == CompactAlways,
		calls:     map[uint16]chan ProtobufMessage{},
		maxPacket: opt.MaxPacket,
		reasm:     map[reasmKey]*reassembly{},
		done:      make(chan struct{}),
	}
	s.dcond = sync.NewCond(&s.dmu)
	s.wg.Add(2)
	go s.readLoop()
	go s.dispatchLoop()
	return s
}

// Done is closed when the session ends; Err tells why.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err returns why the session ended: nil while it runs or after Close.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close stops the session. It does not close the transport.
func (s *Session) Close() error {
	s.stop(nil)
	s.wg.Wait()
	return nil
}

func (s *Session) stop(err error) {
	s.once.Do(func() {
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		close(s.done)
		s.dmu.Lock()
		s.dclose = true
		s.dcond.Broadcast()
		s.dmu.Unlock()
	})
}

// SetMaxPacket sets the peer's max packet size (from its DEVICE_INFORMATION), which sizes
// outgoing protobuf chunks.
func (s *Session) SetMaxPacket(n int) {
	if n <= ProtobufOverhead {
		return
	}
	s.mu.Lock()
	s.maxPacket = n
	s.mu.Unlock()
}

// MaxPacket returns the max packet size in use.
func (s *Session) MaxPacket() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxPacket
}

// SetCompact switches the frame form of the session's own requests, e.g. for a fake watch,
// which sends DEVICE_INFORMATION plain and everything after it compact, as the Fenix 8 does.
func (s *Session) SetCompact(on bool) {
	s.mu.Lock()
	s.compact = on
	s.mu.Unlock()
}

// Compact reports whether the session's requests use compact frames.
func (s *Session) Compact() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compact
}

// Send writes one frame as it is.
func (s *Session) Send(f Frame) error {
	select {
	case <-s.done:
		return ErrSessionClosed
	default:
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.opt.Trace != nil {
		s.opt.Trace("tx", f)
	}
	return s.t.Write(cobs.Encode(f.Marshal()))
}

// Reply answers req with a RESPONSE in req's frame form.
func (s *Session) Reply(req Frame, status Status, data []byte) error {
	return s.Send(ReplyTo(req, status, data))
}

// Post sends a message of our own without waiting for its RESPONSE.
func (s *Session) Post(id MessageID, payload []byte) error {
	s.mu.Lock()
	compact := s.compact
	var seq uint8
	if compact {
		var ok bool
		if seq, ok = s.allocSeqLocked(); !ok {
			s.mu.Unlock()
			return errors.New("gfdi: all compact sequence numbers are in use")
		}
	}
	s.mu.Unlock()
	return s.Send(Frame{ID: id, Compact: compact, Seq: seq, Payload: payload})
}

// Request sends a message and waits for its RESPONSE.
func (s *Session) Request(ctx context.Context, id MessageID, payload []byte) (ResponseMessage, error) {
	return s.request(ctx, id, payload, nil)
}

func (s *Session) allocSeqLocked() (uint8, bool) {
	for range compactSeqModulus {
		seq := uint8(s.nextSeq)
		s.nextSeq = (s.nextSeq + 1) % compactSeqModulus
		busy := false
		for _, w := range s.waiters {
			if w.compact && w.seq == seq {
				busy = true
				break
			}
		}
		if !busy {
			return seq, true
		}
	}
	return 0, false
}

func (s *Session) request(ctx context.Context, id MessageID, payload []byte, match func(ResponseMessage) bool) (ResponseMessage, error) {
	w := &respWaiter{id: id, match: match, ch: make(chan ResponseMessage, 1)}
	s.mu.Lock()
	if s.err != nil || isClosed(s.done) {
		s.mu.Unlock()
		return ResponseMessage{}, ErrSessionClosed
	}
	if s.compact {
		seq, ok := s.allocSeqLocked()
		if !ok {
			s.mu.Unlock()
			return ResponseMessage{}, errors.New("gfdi: all compact sequence numbers are in use")
		}
		w.compact, w.seq = true, seq
	}
	s.waiters = append(s.waiters, w)
	s.mu.Unlock()
	defer s.dropWaiter(w)

	if err := s.Send(Frame{ID: id, Compact: w.compact, Seq: w.seq, Payload: payload}); err != nil {
		return ResponseMessage{}, err
	}
	timer := s.opt.Clock.NewTimer(s.opt.RequestTimeout)
	defer timer.Stop()
	select {
	case r := <-w.ch:
		return r, nil
	case <-ctx.Done():
		return ResponseMessage{}, ctx.Err()
	case <-timer.C():
		return ResponseMessage{}, fmt.Errorf("%w: no RESPONSE to %s within %s", ErrTimeout, id, s.opt.RequestTimeout)
	case <-s.done:
		return ResponseMessage{}, ErrSessionClosed
	}
}

func (s *Session) dropWaiter(w *respWaiter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, x := range s.waiters {
		if x == w {
			s.waiters = append(s.waiters[:i], s.waiters[i+1:]...)
			return
		}
	}
}

func isClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// SendProtobuf sends a serialized Smart message as kind (ProtobufRequest or
// ProtobufResponse), chunk by chunk, each one after the peer acknowledged the previous one.
func (s *Session) SendProtobuf(ctx context.Context, kind MessageID, requestID uint16, msg []byte) error {
	for _, c := range SplitProtobuf(requestID, msg, s.MaxPacket()-ProtobufOverhead) {
		r, err := s.request(ctx, kind, c.Payload(), func(r ResponseMessage) bool {
			st, err := ParseProtobufStatus(r.Data)
			return err != nil || st.RequestID == requestID
		})
		if err != nil {
			return fmt.Errorf("gfdi: %s %d chunk at %d: %w", kind, requestID, c.Offset, err)
		}
		if r.Status != StatusACK {
			return fmt.Errorf("gfdi: %s %d chunk at %d: status %d", kind, requestID, c.Offset, r.Status)
		}
		st, err := ParseProtobufStatus(r.Data)
		if err != nil {
			return fmt.Errorf("gfdi: %s %d chunk at %d: %w", kind, requestID, c.Offset, err)
		}
		if !st.OK() && st.Code != ProtobufDuplicatePacket {
			return fmt.Errorf("gfdi: %s %d chunk at %d refused: %s", kind, requestID, c.Offset, st)
		}
	}
	return nil
}

// Call sends a Smart message as a PROTOBUF_REQUEST with a new request ID and waits for the
// peer's PROTOBUF_RESPONSE with that ID.
func (s *Session) Call(ctx context.Context, msg []byte) (ProtobufMessage, error) {
	ch := make(chan ProtobufMessage, 1)
	s.mu.Lock()
	id := s.nextReqID
	for range 1 << 16 {
		if s.calls[id] == nil {
			break
		}
		id++
	}
	s.nextReqID = id + 1
	s.calls[id] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.calls, id)
		s.mu.Unlock()
	}()
	if err := s.SendProtobuf(ctx, ProtobufRequest, id, msg); err != nil {
		return ProtobufMessage{}, err
	}
	timer := s.opt.Clock.NewTimer(s.opt.CallTimeout)
	defer timer.Stop()
	select {
	case m := <-ch:
		return m, nil
	case <-ctx.Done():
		return ProtobufMessage{}, ctx.Err()
	case <-timer.C():
		return ProtobufMessage{}, fmt.Errorf("%w: no PROTOBUF_RESPONSE to request %d within %s", ErrTimeout, id, s.opt.CallTimeout)
	case <-s.done:
		return ProtobufMessage{}, ErrSessionClosed
	}
}

// Notify sends a Smart message as a PROTOBUF_REQUEST that expects no answer, such as a
// notification, and waits until the peer acknowledged it.
func (s *Session) Notify(ctx context.Context, msg []byte) error {
	s.mu.Lock()
	id := s.nextReqID
	s.nextReqID++
	s.mu.Unlock()
	return s.SendProtobuf(ctx, ProtobufRequest, id, msg)
}

func (s *Session) readLoop() {
	defer s.wg.Done()
	var dec cobs.StreamDecoder
	for {
		select {
		case <-s.done:
			return
		case p, ok := <-s.t.Recv():
			if !ok {
				s.stop(errors.New("gfdi: transport closed"))
				return
			}
			frames, err := dec.Feed(p)
			if err != nil {
				s.log.Debug("gfdi: COBS", "err", err)
			}
			for _, raw := range frames {
				f, err := ParseFrame(raw)
				if err != nil {
					s.log.Debug("gfdi: bad frame", "err", err, "len", len(raw))
					continue
				}
				s.onFrame(f)
			}
		}
	}
}

func (s *Session) onFrame(f Frame) {
	if s.opt.Trace != nil {
		s.opt.Trace("rx", f)
	}
	if f.Compact && s.opt.Compact == CompactAuto {
		s.mu.Lock()
		s.compact = true
		s.mu.Unlock()
	}
	switch f.ID {
	case Response:
		s.onResponse(f)
	case ProtobufRequest, ProtobufResponse:
		s.onProtobufChunk(f)
	case CancelProtobuf:
		if len(f.Payload) >= 2 {
			id := uint16(f.Payload[0]) | uint16(f.Payload[1])<<8
			s.mu.Lock()
			delete(s.reasm, reasmKey{ProtobufRequest, id})
			delete(s.reasm, reasmKey{ProtobufResponse, id})
			s.mu.Unlock()
		}
		_ = s.Reply(f, StatusACK, f.Payload[:min(2, len(f.Payload))])
	default:
		s.dispatch(func() {
			if s.opt.OnFrame != nil {
				s.opt.OnFrame(s, f)
				return
			}
			_ = s.Reply(f, StatusACK, nil)
		})
	}
}

func (s *Session) onResponse(f Frame) {
	r, err := ParseResponse(f.Payload)
	if err != nil {
		s.log.Debug("gfdi: bad RESPONSE", "err", err)
		return
	}
	s.mu.Lock()
	var hit *respWaiter
	for i, w := range s.waiters {
		if w.id != r.Original || (f.Compact && w.compact && w.seq != f.Seq) || (w.match != nil && !w.match(r)) {
			continue
		}
		hit = w
		s.waiters = append(s.waiters[:i], s.waiters[i+1:]...)
		break
	}
	s.mu.Unlock()
	if hit == nil {
		s.log.Debug("gfdi: RESPONSE without a waiter", "original", r.Original, "status", r.Status)
		return
	}
	hit.ch <- r
}

func (s *Session) ackChunk(f Frame, c ProtobufChunk, chunkStatus, code byte) {
	st := ProtobufStatus{RequestID: c.RequestID, Offset: c.Offset, ChunkStatus: chunkStatus, Code: code}
	_ = s.Reply(f, StatusACK, st.Bytes())
}

func (s *Session) onProtobufChunk(f Frame) {
	c, err := ParseProtobufChunk(f.Payload)
	if err != nil {
		s.log.Debug("gfdi: bad protobuf chunk", "err", err)
		if len(f.Payload) >= 6 {
			c = ProtobufChunk{RequestID: uint16(f.Payload[0]) | uint16(f.Payload[1])<<8}
			s.ackChunk(f, c, ChunkDiscarded, ProtobufUnknownError)
		} else {
			_ = s.Reply(f, StatusDecodeError, nil)
		}
		return
	}
	key := reasmKey{f.ID, c.RequestID}
	var data []byte
	s.mu.Lock()
	if c.Complete() {
		delete(s.reasm, key)
		data = c.Data
	} else {
		r := s.reasm[key]
		switch {
		case c.Offset == 0:
			if int64(c.Total) > int64(s.opt.MaxMessage) {
				delete(s.reasm, key)
				s.mu.Unlock()
				s.ackChunk(f, c, ChunkDiscarded, ProtobufExceededLength)
				return
			}
			r = &reassembly{total: c.Total, buf: make([]byte, 0, c.Total)}
			s.reasm[key] = r
		case r == nil || r.total != c.Total || c.Offset > uint32(len(r.buf)):
			s.mu.Unlock()
			s.ackChunk(f, c, ChunkDiscarded, ProtobufMissingPacket)
			return
		case c.Offset < uint32(len(r.buf)):
			s.mu.Unlock()
			s.ackChunk(f, c, ChunkKept, ProtobufDuplicatePacket)
			return
		}
		r.buf = append(r.buf, c.Data...)
		if uint32(len(r.buf)) < r.total {
			s.mu.Unlock()
			s.ackChunk(f, c, ChunkKept, ProtobufNoError)
			return
		}
		delete(s.reasm, key)
		data = r.buf
	}
	rk := recentKey{f.ID, c.RequestID, crc32.ChecksumIEEE(data), len(data)}
	for _, k := range s.recent {
		if k == rk {
			// The peer resends a message whose acknowledgement it missed; acknowledge it
			// again but do not process it twice.
			s.mu.Unlock()
			s.ackChunk(f, c, ChunkKept, ProtobufNoError)
			return
		}
	}
	s.recent = append(s.recent, rk)
	if len(s.recent) > 32 {
		s.recent = s.recent[1:]
	}
	m := ProtobufMessage{Kind: f.ID, RequestID: c.RequestID, Data: data}
	if f.ID == ProtobufResponse {
		if ch := s.calls[c.RequestID]; ch != nil {
			delete(s.calls, c.RequestID)
			s.mu.Unlock()
			s.ackChunk(f, c, ChunkKept, ProtobufNoError)
			ch <- m
			return
		}
	}
	s.mu.Unlock()
	s.dispatch(func() {
		rep := Discarded()
		if s.opt.OnProtobuf != nil {
			rep = s.opt.OnProtobuf(s, m)
		}
		s.ackChunk(f, c, rep.ChunkStatus, rep.Code)
		if rep.Response != nil {
			// Sent from its own goroutine: chunked responses wait for acknowledgements, and
			// the next message should not wait for them. request gives up when the session
			// stops.
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				if err := s.SendProtobuf(context.Background(), ProtobufResponse, m.RequestID, rep.Response); err != nil {
					s.log.Debug("gfdi: protobuf response", "request", m.RequestID, "err", err)
				}
			}()
		}
	})
}

// dispatch queues f for the dispatch goroutine; the queue is unbounded so the reader never
// blocks on a slow handler.
func (s *Session) dispatch(f func()) {
	s.dmu.Lock()
	defer s.dmu.Unlock()
	if s.dclose {
		return
	}
	s.dq = append(s.dq, f)
	s.dcond.Signal()
}

func (s *Session) dispatchLoop() {
	defer s.wg.Done()
	for {
		s.dmu.Lock()
		for len(s.dq) == 0 && !s.dclose {
			s.dcond.Wait()
		}
		if s.dclose {
			s.dmu.Unlock()
			return
		}
		f := s.dq[0]
		s.dq = s.dq[1:]
		s.dmu.Unlock()
		f()
	}
}
