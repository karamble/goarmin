// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package multilink

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/karamble/goarmin/pkg/clock"
)

// streamRig drives one reliable stream with a fake clock and records what it emits.
type streamRig struct {
	t         *testing.T
	clk       *clock.Fake
	r         *reliable
	mu        sync.Mutex
	out       []func() []byte
	delivered []byte
	failed    error
}

func newRig(t *testing.T, maxWrite int, cfg MLRConfig) *streamRig {
	t.Helper()
	g := &streamRig{t: t, clk: clock.NewFake(time.Now())}
	g.r = newReliable(0x81, maxWrite, cfg, g.clk,
		func(build func() []byte, _ bool) {
			g.mu.Lock()
			g.out = append(g.out, build)
			g.mu.Unlock()
		},
		func(p []byte) {
			g.mu.Lock()
			g.delivered = append(g.delivered, p...)
			g.mu.Unlock()
		},
		func(err error) {
			g.mu.Lock()
			g.failed = err
			g.mu.Unlock()
		})
	return g
}

// take builds and returns the emitted packets, as the mux writer would send them now.
func (g *streamRig) take() [][]byte {
	g.mu.Lock()
	builds := g.out
	g.out = nil
	g.mu.Unlock()
	var out [][]byte
	for _, b := range builds {
		if p := b(); p != nil {
			out = append(out, p)
		}
	}
	return out
}

func pkt(handle byte, rn, sn int, payload ...byte) []byte {
	h := Header{Handle: handle, RN: rn, SN: sn}.Bytes()
	return append(h[:], payload...)
}

func headers(t *testing.T, pkts [][]byte) []Header {
	t.Helper()
	var hs []Header
	for _, p := range pkts {
		if len(p) < 2 {
			t.Fatalf("short packet % X", p)
		}
		hs = append(hs, ParseHeader(p[0], p[1]))
	}
	return hs
}

func TestReliableFragmentsAndWindow(t *testing.T) {
	g := newRig(t, 20, MLRConfig{})
	data := make([]byte, 40*18+5) // 41 fragments of up to 18 bytes
	for i := range data {
		data[i] = byte(i)
	}
	if err := g.r.write(data); err != nil {
		t.Fatal(err)
	}
	out := g.take()
	if len(out) != 32 {
		t.Fatalf("sent %d packets before any ACK, want the window of 32", len(out))
	}
	for i, h := range headers(t, out) {
		if h.Handle != 0x81 || h.SN != i || h.RN != 0 {
			t.Fatalf("packet %d header %+v", i, h)
		}
		if len(out[i]) != 20 {
			t.Fatalf("packet %d is %d bytes, want 20", i, len(out[i]))
		}
	}
	// An ACK for 10 fragments frees 10 slots and grows nothing past 32.
	g.r.onPacket(pkt(0x81, 10, 0))
	out = g.take()
	if len(out) != 9 { // 41 - 32 = 9 fragments were left
		t.Fatalf("sent %d packets after ACK 10, want 9", len(out))
	}
	if hs := headers(t, out); hs[0].SN != 32 || hs[8].SN != 40 {
		t.Fatalf("follow-up SNs %d..%d", hs[0].SN, hs[8].SN)
	}
	if last := out[8]; len(last) != 2+5 {
		t.Fatalf("last fragment is %d bytes, want 7", len(last))
	}
	// Everything acknowledged: flush returns at once.
	g.r.onPacket(pkt(0x81, 41, 0))
	if err := g.r.flush(nil); err != nil {
		t.Fatal(err)
	}
	if g.clk.Pending() != 0 {
		t.Fatalf("%d timers still armed after the last ACK", g.clk.Pending())
	}
}

func TestReliableReceiveAndAck(t *testing.T) {
	g := newRig(t, 20, MLRConfig{})
	for sn := range 4 {
		g.r.onPacket(pkt(0x81, 0, sn, byte('a'+sn)))
	}
	if out := g.take(); len(out) != 0 {
		t.Fatalf("ACKed before the threshold or timer: % X", out)
	}
	// The fifth in-order packet triggers an immediate ACK.
	g.r.onPacket(pkt(0x81, 0, 4, 'e'))
	out := g.take()
	if len(out) != 1 || len(out[0]) != 2 || headers(t, out)[0].RN != 5 {
		t.Fatalf("after 5 packets got % X, want one ACK with RN 5", out)
	}
	// One more packet arms the 20 ms timer.
	g.r.onPacket(pkt(0x81, 0, 5, 'f'))
	g.clk.Advance(19 * time.Millisecond)
	if out := g.take(); len(out) != 0 {
		t.Fatalf("ACK before 20 ms: % X", out)
	}
	g.clk.Advance(time.Millisecond)
	if out := g.take(); len(out) != 1 || headers(t, out)[0].RN != 6 {
		t.Fatalf("delayed ACK % X, want RN 6", out)
	}
	// A duplicate is dropped and answered at once with the current RN.
	g.r.onPacket(pkt(0x81, 0, 3, 'x'))
	if out := g.take(); len(out) != 1 || headers(t, out)[0].RN != 6 {
		t.Fatalf("duplicate answered with % X, want ACK RN 6", out)
	}
	// So is a packet from the future.
	g.r.onPacket(pkt(0x81, 0, 9, 'z'))
	if out := g.take(); len(out) != 1 || headers(t, out)[0].RN != 6 {
		t.Fatalf("gap answered with % X, want ACK RN 6", out)
	}
	if string(g.delivered) != "abcdef" {
		t.Fatalf("delivered %q", g.delivered)
	}
	if s := g.r.snapshot(); s.Received != 6 || s.Duplicates != 2 || s.AcksSent != 4 {
		t.Fatalf("stats %+v", s)
	}
}

func TestReliablePiggybackAck(t *testing.T) {
	g := newRig(t, 20, MLRConfig{})
	g.r.onPacket(pkt(0x81, 0, 0, 'a'))
	g.r.onPacket(pkt(0x81, 0, 1, 'b'))
	// Sending data now carries RN 2 and makes the pending delayed ACK unnecessary.
	if err := g.r.write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	out := g.take()
	if len(out) != 1 || headers(t, out)[0].RN != 2 || headers(t, out)[0].SN != 0 {
		t.Fatalf("data packet % X, want RN 2 SN 0", out)
	}
	g.clk.Advance(50 * time.Millisecond)
	if out := g.take(); len(out) != 0 {
		t.Fatalf("redundant ACK after a piggy-backed one: % X", out)
	}
}

// dataOnly drops ACK-only packets.
func dataOnly(pkts [][]byte) [][]byte {
	var out [][]byte
	for _, p := range pkts {
		if len(p) > 2 {
			out = append(out, p)
		}
	}
	return out
}

func TestReliableRetransmitBackoff(t *testing.T) {
	g := newRig(t, 20, MLRConfig{})
	g.r.onPacket(pkt(0x81, 0, 0, 'a')) // RN for our first fragments becomes 1
	if err := g.r.write(bytes.Repeat([]byte{1}, 18*3)); err != nil {
		t.Fatal(err)
	}
	if first := g.take(); len(first) != 3 {
		t.Fatalf("sent %d", len(first))
	}
	// A later packet from the peer moves our RN on; retransmits keep the original RN 1.
	g.r.onPacket(pkt(0x81, 0, 1, 'b'))

	for i, rto := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		g.clk.Advance(rto - time.Millisecond)
		if out := dataOnly(g.take()); len(out) != 0 {
			t.Fatalf("round %d: retransmitted before the RTO of %s", i, rto)
		}
		g.clk.Advance(time.Millisecond)
		out := dataOnly(g.take())
		if len(out) != 3 {
			t.Fatalf("round %d: retransmitted %d packets, want all 3 (go-back-N)", i, len(out))
		}
		for j, h := range headers(t, out) {
			if h.SN != j || h.RN != 1 {
				t.Fatalf("round %d: retransmit header %+v, want SN %d RN 1", i, h, j)
			}
		}
	}
	// A partial ACK resets the RTO to 1 s.
	g.r.onPacket(pkt(0x81, 1, 0))
	g.clk.Advance(time.Second)
	out := dataOnly(g.take())
	if len(out) != 2 || headers(t, out)[0].SN != 1 {
		t.Fatalf("after partial ACK retransmitted % X, want SN 1 and 2 after 1 s", out)
	}
	if s := g.r.snapshot(); s.Retransmitted != 11 {
		t.Fatalf("retransmitted %d, want 11", s.Retransmitted)
	}
}

// Once the peer has sent us staleRNLimit packets since a fragment's first send, its
// retransmission carries the current RN: the original one could be misread after the wrap.
func TestReliableRetransmitRefreshesStaleRN(t *testing.T) {
	g := newRig(t, 20, MLRConfig{})
	_ = g.r.write([]byte("x"))
	g.take()
	for sn := range staleRNLimit {
		g.r.onPacket(pkt(0x81, 0, sn, 'p'))
	}
	g.clk.Advance(time.Second)
	out := dataOnly(g.take())
	if len(out) != 1 || headers(t, out)[0].RN != staleRNLimit {
		t.Fatalf("retransmit % X, want RN %d", out, staleRNLimit)
	}
}

func TestReliableBackoffCap(t *testing.T) {
	g := newRig(t, 20, MLRConfig{MaxRetransmit: 100})
	_ = g.r.write([]byte{1})
	g.take()
	var times []time.Duration
	start := g.clk.Now()
	for range 8 {
		for len(g.take()) == 0 {
			g.clk.Advance(100 * time.Millisecond)
		}
		times = append(times, g.clk.Now().Sub(start).Round(100*time.Millisecond))
	}
	want := []time.Duration{1, 3, 7, 15, 31, 51, 71, 91}
	for i := range want {
		if times[i] != want[i]*time.Second {
			t.Fatalf("retransmit times %v, want %v s (RTO doubling, capped at 20 s)", times, want)
		}
	}
}

func TestReliableIgnoresStaleAck(t *testing.T) {
	g := newRig(t, 20, MLRConfig{})
	_ = g.r.write(bytes.Repeat([]byte{1}, 18*3))
	g.take()
	g.r.onPacket(pkt(0x81, 10, 0)) // RN beyond what was sent
	g.r.onPacket(pkt(0x81, 60, 0)) // RN behind (wrapped)
	if s := g.r.snapshot(); s.StaleAcks != 2 {
		t.Fatalf("stale ACKs counted %d, want 2", s.StaleAcks)
	}
	g.r.mu.Lock()
	outstanding := len(g.r.sent)
	g.r.mu.Unlock()
	if outstanding != 3 {
		t.Fatalf("%d fragments outstanding after stale ACKs, want 3", outstanding)
	}
	g.r.onPacket(pkt(0x81, 3, 0))
	if err := g.r.flush(nil); err != nil {
		t.Fatal(err)
	}
}

func TestReliableRetransmitLimit(t *testing.T) {
	g := newRig(t, 20, MLRConfig{InitialRTO: 10 * time.Millisecond, MaxRTO: 10 * time.Millisecond, MaxRetransmit: 3})
	_ = g.r.write([]byte{1})
	sent := len(g.take())
	for range 10 {
		g.clk.Advance(10 * time.Millisecond)
		sent += len(g.take())
	}
	if !errors.Is(g.failed, ErrRetransmitLimit) {
		t.Fatalf("fail called with %v, want ErrRetransmitLimit", g.failed)
	}
	if sent != 1+3 {
		t.Fatalf("%d packets, want the original plus 3 retransmits", sent)
	}
	if err := g.r.write([]byte{2}); !errors.Is(err, ErrClosed) {
		t.Fatalf("write after failure: %v", err)
	}
	if err := g.r.flush(nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("flush after failure: %v", err)
	}
}

// A packet still waiting in the writer's queue is not queued again by a retransmission, and
// one that was acknowledged before it went out is not sent at all.
func TestReliableQueueDedup(t *testing.T) {
	g := newRig(t, 20, MLRConfig{})
	_ = g.r.write(bytes.Repeat([]byte{1}, 18*3))
	g.clk.Advance(time.Second) // retransmit timeout while the first sends still wait
	g.clk.Advance(2 * time.Second)
	g.mu.Lock()
	queued := len(g.out)
	g.mu.Unlock()
	if queued != 3 {
		t.Fatalf("%d builders queued, want 3 (one per fragment)", queued)
	}
	g.r.onPacket(pkt(0x81, 2, 0)) // the peer got the first two after all
	out := g.take()
	if len(out) != 1 || headers(t, out)[0].SN != 2 {
		t.Fatalf("sent % X, want only SN 2", out)
	}
	// ACKs collapse the same way and carry the RN of the moment they go out.
	for sn := range 3 {
		g.r.onPacket(pkt(0x81, 3, sn, 'x'))
		g.r.onPacket(pkt(0x81, 3, sn, 'x')) // duplicate: immediate ACK
	}
	out = g.take()
	if len(out) != 1 || headers(t, out)[0].RN != 3 {
		t.Fatalf("ACKs % X, want one with RN 3", out)
	}
}

func TestReliableSequenceWraps(t *testing.T) {
	g := newRig(t, 3, MLRConfig{}) // one payload byte per packet
	var want []byte
	for i := range 200 {
		want = append(want, byte(i))
	}
	_ = g.r.write(want)
	var got []byte
	for len(got) < len(want) {
		out := g.take()
		if len(out) == 0 {
			t.Fatalf("stalled after %d bytes", len(got))
		}
		for _, p := range out {
			got = append(got, p[2:]...)
		}
		h := ParseHeader(out[len(out)-1][0], out[len(out)-1][1])
		g.r.onPacket(pkt(0x81, (h.SN+1)%SeqModulus, 0))
	}
	if !bytes.Equal(got, want) {
		t.Fatal("bytes differ across sequence wrap")
	}
}

// Two streams connected through a lossy, reordering-free wire move a large payload each way.
func TestReliablePairLossy(t *testing.T) {
	for seed := range uint64(6) {
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) { testReliablePairLossy(t, seed) })
	}
}

func testReliablePairLossy(t *testing.T, seed uint64) {
	clk := clock.NewFake(time.Now())
	rng := rand.New(rand.NewPCG(seed, 2))
	type wire struct{ q []func() []byte }
	var ab, ba wire
	var mu sync.Mutex
	var gotA, gotB []byte
	cfg := MLRConfig{MaxRetransmit: 50}
	a := newReliable(0x80, 60, cfg, clk, func(p func() []byte, _ bool) {
		mu.Lock()
		ab.q = append(ab.q, p)
		mu.Unlock()
	}, func(p []byte) { gotA = append(gotA, p...) }, func(err error) { t.Errorf("A failed: %v", err) })
	b := newReliable(0x80, 60, cfg, clk, func(p func() []byte, _ bool) {
		mu.Lock()
		ba.q = append(ba.q, p)
		mu.Unlock()
	}, func(p []byte) { gotB = append(gotB, p...) }, func(err error) { t.Errorf("B failed: %v", err) })

	payload := func(seed uint64, n int) []byte {
		r := rand.New(rand.NewPCG(seed, seed))
		p := make([]byte, n)
		for i := range p {
			p[i] = byte(r.Uint32())
		}
		return p
	}
	wantB, wantA := payload(3, 50_000), payload(4, 20_000)
	_ = a.write(wantB)
	_ = b.write(wantA)

	deliver := func(q *wire, to *reliable) bool {
		mu.Lock()
		pkts := q.q
		q.q = nil
		mu.Unlock()
		for _, build := range pkts {
			p := build()
			if p == nil || rng.Float64() < 0.10 {
				continue // skipped or lost
			}
			to.onPacket(p)
		}
		return len(pkts) > 0
	}
	for step := 0; len(gotB) < len(wantB) || len(gotA) < len(wantA); step++ {
		if step > 200_000 {
			t.Fatalf("no progress: A got %d/%d, B got %d/%d", len(gotA), len(wantA), len(gotB), len(wantB))
		}
		moved := deliver(&ab, b)
		moved = deliver(&ba, a) || moved
		if !moved {
			clk.Advance(10 * time.Millisecond)
		}
	}
	if !bytes.Equal(gotB, wantB) || !bytes.Equal(gotA, wantA) {
		t.Fatal("payload corrupted")
	}
	// Keep the wire running until the final ACKs got through and both sides are idle.
	idle := func(r *reliable) bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return len(r.sent) == 0 && len(r.queue) == 0
	}
	for step := 0; !idle(a) || !idle(b); step++ {
		if step > 100_000 {
			t.Fatal("streams never went idle")
		}
		deliver(&ab, b)
		deliver(&ba, a)
		clk.Advance(100 * time.Millisecond)
	}
	if a.snapshot().Retransmitted == 0 {
		t.Fatal("10% loss caused no retransmissions?")
	}
}
