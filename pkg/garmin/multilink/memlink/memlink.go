// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package memlink connects two packet links in memory, with optional fault injection. It stands
// in for a BLE characteristic pair in tests: one end is the host, the other a fake watch. Both
// ends implement multilink.Link.
package memlink

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
)

// Side names an end of a pipe.
type Side int

// The two ends. By convention A is the host and B the watch.
const (
	A Side = iota
	B
)

func (s Side) String() string {
	if s == A {
		return "A"
	}
	return "B"
}

// ErrDisconnected is the error of a pipe closed with Close.
var ErrDisconnected = errors.New("memlink: disconnected")

// Options configures a pipe.
type Options struct {
	// MaxWrite is the largest packet either end may send (229 by default, the Fenix 8's ATT
	// MTU minus 3).
	MaxWrite int
	// Buffer is the number of packets in flight per direction before Send blocks (1024).
	Buffer int
	// Filter, if set, decides how often a packet sent from a side is delivered: 0 drops it,
	// 1 delivers it, 2 delivers it twice. It may be called from several goroutines.
	Filter func(from Side, pkt []byte) int
}

// Link is one end of a pipe.
type Link struct {
	side Side
	p    *pipe
	recv chan []byte
	peer *Link
	sent atomic.Int64
}

type pipe struct {
	opt  Options
	done chan struct{}
	once sync.Once
	mu   sync.Mutex
	err  error
}

// Pipe returns two connected ends.
func Pipe(opt Options) (a, b *Link) {
	if opt.MaxWrite <= 0 {
		opt.MaxWrite = 229
	}
	if opt.Buffer <= 0 {
		opt.Buffer = 1024
	}
	p := &pipe{opt: opt, done: make(chan struct{})}
	a = &Link{side: A, p: p, recv: make(chan []byte, opt.Buffer)}
	b = &Link{side: B, p: p, recv: make(chan []byte, opt.Buffer)}
	a.peer, b.peer = b, a
	return a, b
}

// Send delivers a copy of pkt to the other end, subject to the filter.
func (l *Link) Send(ctx context.Context, pkt []byte) error {
	if len(pkt) > l.p.opt.MaxWrite {
		return errors.New("memlink: packet larger than MaxWrite")
	}
	select {
	case <-l.p.done:
		return l.p.error()
	default:
	}
	n := 1
	if l.p.opt.Filter != nil {
		n = l.p.opt.Filter(l.side, pkt)
	}
	l.sent.Add(1)
	for range n {
		cp := append([]byte(nil), pkt...)
		select {
		case l.peer.recv <- cp:
		case <-ctx.Done():
			return ctx.Err()
		case <-l.p.done:
			return l.p.error()
		}
	}
	return nil
}

// Recv delivers the packets the other end sent.
func (l *Link) Recv() <-chan []byte { return l.recv }

// MaxWrite returns the packet size limit.
func (l *Link) MaxWrite() int { return l.p.opt.MaxWrite }

// Done is closed when the pipe is closed, from either end.
func (l *Link) Done() <-chan struct{} { return l.p.done }

// Err returns why the pipe closed, or nil while it is open.
func (l *Link) Err() error {
	select {
	case <-l.p.done:
		return l.p.error()
	default:
		return nil
	}
}

// Sent returns the number of packets this end has sent, before filtering.
func (l *Link) Sent() int { return int(l.sent.Load()) }

// Close disconnects both ends with ErrDisconnected.
func (l *Link) Close() error {
	l.CloseWithError(ErrDisconnected)
	return nil
}

// CloseWithError disconnects both ends with err.
func (l *Link) CloseWithError(err error) {
	l.p.once.Do(func() {
		l.p.mu.Lock()
		l.p.err = err
		l.p.mu.Unlock()
		close(l.p.done)
	})
}

func (p *pipe) error() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// RandomLoss returns a filter that drops packets matched by match with probability p, using a
// deterministic generator seeded with seed. A nil match applies to every packet.
func RandomLoss(seed uint64, p float64, match func(from Side, pkt []byte) bool) func(Side, []byte) int {
	var mu sync.Mutex
	rng := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))
	return func(from Side, pkt []byte) int {
		if match != nil && !match(from, pkt) {
			return 1
		}
		mu.Lock()
		drop := rng.Float64() < p
		mu.Unlock()
		if drop {
			return 0
		}
		return 1
	}
}

// MLRData matches MLR data packets (flag bit set, longer than an ACK-only header). It is a
// convenient match for RandomLoss: only the reliable layer is expected to recover from loss.
func MLRData(_ Side, pkt []byte) bool { return len(pkt) > 2 && pkt[0]&0x80 != 0 }

// MLRAny matches every MLR packet, ACK-only packets included.
func MLRAny(_ Side, pkt []byte) bool { return len(pkt) >= 2 && pkt[0]&0x80 != 0 }
