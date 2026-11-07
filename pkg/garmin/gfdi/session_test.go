// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gfdi

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/karamble/goarmin/pkg/clock"
	"github.com/karamble/goarmin/pkg/cobs"
)

// chanT is an in-memory Transport end.
type chanT struct {
	in   chan []byte
	out  chan []byte
	once *sync.Once
}

func (c chanT) Write(p []byte) error {
	c.out <- append([]byte(nil), p...)
	return nil
}
func (c chanT) Recv() <-chan []byte { return c.in }
func (c chanT) closeIn()            { c.once.Do(func() { close(c.in) }) }

func pipeT() (a, b chanT) {
	ab, ba := make(chan []byte, 4096), make(chan []byte, 4096)
	return chanT{in: ba, out: ab, once: &sync.Once{}}, chanT{in: ab, out: ba, once: &sync.Once{}}
}

func ctxT(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// rawPeer drives a session with hand-made frames and decodes what it sends back.
type rawPeer struct {
	t   *testing.T
	tr  chanT // the peer's end
	dec cobs.StreamDecoder
}

func (p *rawPeer) send(f Frame) { p.tr.out <- cobs.Encode(f.Marshal()) }

func (p *rawPeer) next() Frame {
	p.t.Helper()
	for {
		select {
		case b := <-p.tr.in:
			frames, err := p.dec.Feed(b)
			if err != nil {
				p.t.Fatal(err)
			}
			if len(frames) == 0 {
				continue
			}
			if len(frames) > 1 {
				p.t.Fatalf("%d frames in one write", len(frames))
			}
			f, err := ParseFrame(frames[0])
			if err != nil {
				p.t.Fatal(err)
			}
			return f
		case <-time.After(5 * time.Second):
			p.t.Fatal("session sent nothing")
		}
	}
}

func (p *rawPeer) nextStatus() (ResponseMessage, ProtobufStatus) {
	p.t.Helper()
	f := p.next()
	if f.ID != Response {
		p.t.Fatalf("got %v, want a RESPONSE", f)
	}
	r, err := ParseResponse(f.Payload)
	if err != nil {
		p.t.Fatal(err)
	}
	st, err := ParseProtobufStatus(r.Data)
	if err != nil {
		p.t.Fatal(err)
	}
	return r, st
}

func newRaw(t *testing.T, opt SessionOptions) (*Session, *rawPeer) {
	t.Helper()
	a, b := pipeT()
	s := NewSession(a, opt)
	t.Cleanup(func() { s.Close() })
	return s, &rawPeer{t: t, tr: b}
}

// Gadgetbridge's DISCARDED status for request 7 (docs/protocol/02 §5.13).
func TestProtobufStatusGolden(t *testing.T) {
	st := ProtobufStatus{RequestID: 7, ChunkStatus: ChunkDiscarded, Code: ProtobufUnknownRequest}
	got := ReplyTo(Frame{ID: ProtobufRequest}, StatusACK, st.Bytes()).Marshal()
	want := []byte{0x11, 0x00, 0x88, 0x13, 0xB3, 0x13, 0x00, 0x07, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x64, 0x1D, 0xFF}
	if !bytes.Equal(got, want) {
		t.Fatalf("got  % X\nwant % X", got, want)
	}
}

func TestProtobufChunkRoundTrip(t *testing.T) {
	msg := make([]byte, 1000)
	for i := range msg {
		msg[i] = byte(i * 7)
	}
	chunks := SplitProtobuf(9, msg, 375-ProtobufOverhead)
	if len(chunks) != 3 || chunks[1].Offset != 355 || len(chunks[2].Data) != 1000-2*355 {
		t.Fatalf("chunks %d, second at %d", len(chunks), chunks[1].Offset)
	}
	var re []byte
	for _, c := range chunks {
		f := Frame{ID: ProtobufRequest, Payload: c.Payload()}
		if n := len(f.Marshal()); n > 375 {
			t.Fatalf("chunk frame %d bytes exceeds max packet 375", n)
		}
		p, err := ParseProtobufChunk(c.Payload())
		if err != nil {
			t.Fatal(err)
		}
		if p.Total != 1000 || p.RequestID != 9 {
			t.Fatalf("chunk %+v", p)
		}
		re = append(re, p.Data...)
	}
	if !bytes.Equal(re, msg) {
		t.Fatal("reassembly differs")
	}
	if one := SplitProtobuf(1, nil, 100); len(one) != 1 || !one[0].Complete() {
		t.Fatalf("empty message: %+v", one)
	}
	bad := chunks[0].Payload()
	binary.LittleEndian.PutUint32(bad[10:14], 5)
	if _, err := ParseProtobufChunk(bad); err == nil {
		t.Fatal("wrong chunk length accepted")
	}
}

// Responses are matched by original ID and compact sequence number, even out of order.
func TestSessionRequestMatching(t *testing.T) {
	s, peer := newRaw(t, SessionOptions{Compact: CompactAlways})
	ctx := ctxT(t)
	type res struct {
		r   ResponseMessage
		err error
	}
	r1, r2 := make(chan res, 1), make(chan res, 1)
	go func() { r, err := s.Request(ctx, DeviceSettings, []byte{1}); r1 <- res{r, err} }()
	f1 := peer.next()
	go func() { r, err := s.Request(ctx, DeviceSettings, []byte{2}); r2 <- res{r, err} }()
	f2 := peer.next()
	if !f1.Compact || !f2.Compact || f1.Seq == f2.Seq {
		t.Fatalf("requests %v and %v", f1, f2)
	}
	peer.send(ReplyTo(f2, StatusACK, []byte{0xB2}))
	peer.send(ReplyTo(f1, StatusACK, []byte{0xB1}))
	a, b := <-r1, <-r2
	if a.err != nil || b.err != nil {
		t.Fatal(a.err, b.err)
	}
	if !bytes.Equal(a.r.Data, []byte{0xB1}) || !bytes.Equal(b.r.Data, []byte{0xB2}) {
		t.Fatalf("responses crossed: %v %v", a.r.Data, b.r.Data)
	}
}

func TestSessionCompactAuto(t *testing.T) {
	s, peer := newRaw(t, SessionOptions{})
	if err := s.Post(SystemEvent, []byte{8, 0}); err != nil {
		t.Fatal(err)
	}
	if f := peer.next(); f.Compact {
		t.Fatalf("compact before the peer used it: %v", f)
	}
	peer.send(Frame{ID: BatteryStatus, Compact: true, Seq: 9, Payload: []byte{80}})
	if f := peer.next(); f.ID != Response || !f.Compact || f.Seq != 9 {
		t.Fatalf("default reply %v, want a compact RESPONSE with seq 9", f)
	}
	// Our own requests now go compact, numbered from 0 and wrapping at 32.
	for i := range 34 {
		if err := s.Post(SystemEvent, []byte{8, 0}); err != nil {
			t.Fatal(err)
		}
		if f := peer.next(); !f.Compact || int(f.Seq) != i%32 {
			t.Fatalf("request %d: %v", i, f)
		}
	}
}

func TestSessionRequestTimeout(t *testing.T) {
	clk := clock.NewFake(time.Now())
	s, peer := newRaw(t, SessionOptions{Clock: clk, RequestTimeout: 3 * time.Second})
	errc := make(chan error, 1)
	go func() {
		_, err := s.Request(context.Background(), SupportedFileTypesRequest, nil)
		errc <- err
	}()
	peer.next()
	if err := clk.WaitForTimers(ctxT(t), 1); err != nil {
		t.Fatal(err)
	}
	clk.Advance(3 * time.Second)
	if err := <-errc; !errors.Is(err, ErrTimeout) {
		t.Fatalf("got %v, want ErrTimeout", err)
	}
}

// Two sessions: a chunked Call one way, a chunked answer the other, handlers in between.
func TestSessionProtobufCall(t *testing.T) {
	a, b := pipeT()
	big := func(seed uint64, n int) []byte {
		r := rand.New(rand.NewPCG(seed, seed))
		p := make([]byte, n)
		for i := range p {
			p[i] = byte(r.Uint32())
		}
		return p
	}
	req, resp := big(1, 1500), big(2, 5000)
	var handled atomic.Int32
	watch := NewSession(b, SessionOptions{Compact: CompactAlways, MaxPacket: 120,
		OnProtobuf: func(_ *Session, m ProtobufMessage) ProtobufReply {
			handled.Add(1)
			if m.Kind != ProtobufRequest || !bytes.Equal(m.Data, req) {
				t.Errorf("watch got %v %d bytes", m.Kind, len(m.Data))
				return Discarded()
			}
			return Kept(resp)
		}})
	defer watch.Close()
	host := NewSession(a, SessionOptions{})
	defer host.Close()
	host.SetMaxPacket(200)
	ctx := ctxT(t)
	m, err := host.Call(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != ProtobufResponse || m.RequestID != 0 || !bytes.Equal(m.Data, resp) {
		t.Fatalf("reply %v id %d, %d bytes", m.Kind, m.RequestID, len(m.Data))
	}
	if !host.Compact() {
		t.Fatal("host did not switch to compact frames")
	}
	// The next call gets the next request ID.
	m, err = host.Call(ctx, req)
	if err != nil || m.RequestID != 1 {
		t.Fatalf("second call: id %d, %v", m.RequestID, err)
	}
	if handled.Load() != 2 {
		t.Fatalf("handler ran %d times", handled.Load())
	}
}

func TestSessionReassemblyErrors(t *testing.T) {
	var got []ProtobufMessage
	var mu sync.Mutex
	s, peer := newRaw(t, SessionOptions{MaxMessage: 1000, OnProtobuf: func(_ *Session, m ProtobufMessage) ProtobufReply {
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
		return Kept(nil)
	}})
	_ = s
	msg := []byte("0123456789abcdefghij")
	chunk := func(off, n int) Frame {
		c := ProtobufChunk{RequestID: 5, Offset: uint32(off), Total: uint32(len(msg)), Data: msg[off : off+n]}
		return Frame{ID: ProtobufRequest, Compact: true, Seq: uint8(off), Payload: c.Payload()}
	}
	expect := func(what string, wantSeq uint8, wantStatus, wantCode byte) {
		t.Helper()
		r, st := peer.nextStatus()
		if r.Original != ProtobufRequest || st.RequestID != 5 || st.ChunkStatus != wantStatus || st.Code != wantCode {
			t.Fatalf("%s: %v %s", what, r.Original, st)
		}
	}
	peer.send(chunk(10, 5)) // no start
	expect("chunk without start", 10, ChunkDiscarded, ProtobufMissingPacket)
	peer.send(chunk(0, 10))
	expect("first chunk", 0, ChunkKept, ProtobufNoError)
	peer.send(chunk(0, 5)) // overlaps what we hold
	expect("restart", 0, ChunkKept, ProtobufNoError)
	peer.send(chunk(5, 5))
	expect("second chunk", 5, ChunkKept, ProtobufNoError)
	peer.send(chunk(5, 5)) // duplicate
	expect("duplicate", 5, ChunkKept, ProtobufDuplicatePacket)
	peer.send(chunk(15, 5)) // gap
	expect("gap", 15, ChunkDiscarded, ProtobufMissingPacket)
	peer.send(chunk(10, 10))
	expect("last chunk", 10, ChunkKept, ProtobufNoError)
	// The whole message again (the peer missed our ACK): ACKed, not processed twice.
	c := ProtobufChunk{RequestID: 5, Total: uint32(len(msg)), Data: msg}
	peer.send(Frame{ID: ProtobufRequest, Payload: c.Payload()})
	expect("resent message", 0, ChunkKept, ProtobufNoError)
	// Too large.
	huge := ProtobufChunk{RequestID: 5, Total: 5000, Data: make([]byte, 10)}
	peer.send(Frame{ID: ProtobufRequest, Payload: huge.Payload()})
	expect("oversized", 0, ChunkDiscarded, ProtobufExceededLength)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || !bytes.Equal(got[0].Data, msg) {
		t.Fatalf("handler saw %d messages", len(got))
	}
}

func TestSessionUnhandledProtobufDiscarded(t *testing.T) {
	_, peer := newRaw(t, SessionOptions{})
	c := ProtobufChunk{RequestID: 7, Total: 3, Data: []byte{1, 2, 3}}
	peer.send(Frame{ID: ProtobufRequest, Compact: true, Seq: 4, Payload: c.Payload()})
	r, st := peer.nextStatus()
	if r.Status != StatusACK || st.ChunkStatus != ChunkDiscarded || st.Code != ProtobufUnknownRequest {
		t.Fatalf("got %v / %s", r.Status, st)
	}
}

func TestSessionTransportClosed(t *testing.T) {
	a, b := pipeT()
	s := NewSession(a, SessionOptions{})
	defer s.Close()
	errc := make(chan error, 1)
	go func() {
		_, err := s.Request(context.Background(), SupportedFileTypesRequest, nil)
		errc <- err
	}()
	<-b.in
	a.closeIn()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not end")
	}
	if err := <-errc; !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("pending request: %v", err)
	}
	if err := s.Post(SystemEvent, nil); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("post after close: %v", err)
	}
}

func TestSessionHandlersInOrder(t *testing.T) {
	var seen []MessageID
	var mu sync.Mutex
	done := make(chan struct{})
	_, peer := newRaw(t, SessionOptions{OnFrame: func(s *Session, f Frame) {
		mu.Lock()
		seen = append(seen, f.ID)
		n := len(seen)
		mu.Unlock()
		_ = s.Reply(f, StatusACK, nil)
		if n == 3 {
			close(done)
		}
	}})
	for _, id := range []MessageID{DeviceInformation, Configuration, CurrentTimeRequest} {
		peer.send(Frame{ID: id, Payload: []byte{0}})
	}
	<-done
	for range 3 {
		if f := peer.next(); f.ID != Response {
			t.Fatalf("reply %v", f)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if seen[0] != DeviceInformation || seen[1] != Configuration || seen[2] != CurrentTimeRequest {
		t.Fatalf("order %v", seen)
	}
}
