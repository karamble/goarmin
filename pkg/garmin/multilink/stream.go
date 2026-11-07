// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package multilink

import (
	"errors"
	"sync"
	"time"

	"github.com/karamble/goarmin/pkg/clock"
)

// MLRConfig holds the MLR parameters (docs/protocol/01-ble-transport.md §3.3). The zero value
// of a field means the Gadgetbridge default, which works with Garmin watches.
type MLRConfig struct {
	Window       int           // max unacknowledged packets in flight (32)
	InitialRTO   time.Duration // first retransmission timeout (1 s)
	MaxRTO       time.Duration // backoff cap (20 s)
	AckDelay     time.Duration // delayed-ACK timer (20 ms)
	AckThreshold int           // in-order packets that trigger an immediate ACK (5)
	// MaxRetransmit is the number of consecutive timeouts without progress after which the
	// stream fails (8, about 110 s with the default backoff). Gadgetbridge retries forever.
	MaxRetransmit int
}

func (c MLRConfig) withDefaults() MLRConfig {
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	defDur := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&c.Window, 32)
	c.Window = min(c.Window, MaxSeq)
	defDur(&c.InitialRTO, time.Second)
	defDur(&c.MaxRTO, 20*time.Second)
	defDur(&c.AckDelay, 20*time.Millisecond)
	def(&c.AckThreshold, 5)
	def(&c.MaxRetransmit, 8)
	return c
}

// ErrRetransmitLimit is returned when the peer stops acknowledging MLR data.
var ErrRetransmitLimit = errors.New("multilink: peer stopped acknowledging (retransmission limit)")

// staleRNLimit bounds how far our receive side may have moved on before a packet stops carrying
// its fragment's original RN.
//
// Gadgetbridge retransmits with the original RN (commit f4556e81cc, tested against watches with
// dropped packets). garmin-proto-lab says Garmin Connect uses the current one; our capture has
// no retransmission to tell. An RN is only 6 bits, though: once we have received 32 or more
// packets since the first send, the peer can read the old RN as a fresh acknowledgement of data
// it never got through (found by TestReliablePairLossy). So the original RN is kept while it is
// unambiguous, and replaced by the current one after that.
const staleRNLimit = 16

// reliable implements one MLR stream for one handle: it fragments and sends data with sequence
// numbers, retransmits unacknowledged fragments go-back-N style, and delivers in-order payloads
// while acknowledging them.
//
// It is driven by three inputs: write (data to send), onPacket (a received MLR packet) and its
// timers. Output goes to emit as builders, not bytes. The writer calls a builder when the packet
// actually goes on the wire, so:
//   - the header carries the receive state of that moment;
//   - a fragment acknowledged in the meantime is not sent at all;
//   - each fragment and the pending ACK sit in the writer's queue at most once.
//
// Queued bytes went stale: on a slow link, a backlog of retransmissions carried RNs 40 packets
// old, which the peer read as a fresh acknowledgement after the wrap. TestMuxLossyMLR caught it
// under the race detector.
//
// All state is guarded by mu. emit is called with mu held and must not call back into the
// stream; builders lock mu themselves and must be called without it.
type reliable struct {
	handle  byte
	maxData int // payload bytes per packet (write size minus the 2-byte header)
	cfg     MLRConfig
	clk     clock.Clock
	emit    func(build func() []byte, ackOnly bool)
	deliver func(payload []byte)
	fail    func(error)

	mu          sync.Mutex
	queue       [][]byte // fragments waiting for window space
	sent        map[int]*sentFragment
	nextSend    int // SN of the next new fragment
	lastAcked   int // oldest unacknowledged SN (cumulative RN received from the peer)
	window      int
	rto         time.Duration
	retx        *clockTimer
	timeouts    int
	nextRecv    int   // SN expected next from the peer
	recvTotal   int64 // in-order packets received so far (nextRecv without the wrap)
	lastSentAck int   // RN carried by the last packet that went out
	ackQueued   bool  // an ACK-only packet waits in the writer's queue
	ackTimer    *clockTimer
	closed      bool
	idle        *sync.Cond
	stats       StreamStats
}

// StreamStats counts MLR events on one stream.
type StreamStats struct {
	Sent, Retransmitted, Received, Duplicates, AcksSent, StaleAcks int
}

type sentFragment struct {
	data   []byte
	rn     int   // request number when first queued
	recvAt int64 // recvTotal when first queued
	queued bool  // waits in the writer's queue
	sends  int   // times it went on the wire
}

// clockTimer wraps a clock.Timer created with AfterFunc so a callback can tell whether it is
// still the current timer.
type clockTimer struct{ t clock.Timer }

func newReliable(handle byte, maxWrite int, cfg MLRConfig, clk clock.Clock, emit func(func() []byte, bool), deliver func([]byte), fail func(error)) *reliable {
	cfg = cfg.withDefaults()
	r := &reliable{
		handle: handle, maxData: max(1, maxWrite-2), cfg: cfg, clk: clk, emit: emit, deliver: deliver, fail: fail,
		sent: map[int]*sentFragment{}, window: cfg.Window, rto: cfg.InitialRTO,
	}
	r.idle = sync.NewCond(&r.mu)
	return r
}

func seqDiff(a, b int) int { return (a - b + SeqModulus) % SeqModulus }

// write queues data for sending, fragmenting it to the packet size.
func (r *reliable) write(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrClosed
	}
	for off := 0; off < len(data); off += r.maxData {
		end := min(off+r.maxData, len(data))
		r.queue = append(r.queue, append([]byte(nil), data[off:end]...))
	}
	r.pumpLocked()
	return nil
}

// pumpLocked sends queued fragments while the window allows.
func (r *reliable) pumpLocked() {
	for len(r.queue) > 0 && seqDiff(r.nextSend, r.lastAcked) < r.window {
		frag := r.queue[0]
		r.queue = r.queue[1:]
		sn := r.nextSend
		f := &sentFragment{data: frag, rn: r.nextRecv, recvAt: r.recvTotal}
		r.sent[sn] = f
		r.nextSend = (r.nextSend + 1) % SeqModulus
		r.emitData(sn, f)
		if r.retx == nil {
			r.armRetransmitLocked()
		}
	}
	if len(r.queue) == 0 && len(r.sent) == 0 {
		r.idle.Broadcast()
	}
}

// emitData queues fragment sn unless it already waits in the writer's queue.
func (r *reliable) emitData(sn int, f *sentFragment) {
	if f.queued {
		return
	}
	f.queued = true
	r.emit(func() []byte { return r.buildData(sn, f) }, false)
}

// buildData makes the wire packet for fragment sn, or returns nil if it was acknowledged (or
// the stream closed) since it was queued.
func (r *reliable) buildData(sn int, f *sentFragment) []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	f.queued = false
	if r.closed || r.sent[sn] != f {
		return nil
	}
	rn := f.rn
	if r.recvTotal-f.recvAt >= staleRNLimit {
		rn = r.nextRecv
	}
	if rn == r.nextRecv {
		// The packet acknowledges everything received, as an ACK-only packet would.
		r.lastSentAck = rn
		r.stopAckTimerLocked()
	}
	if f.sends == 0 {
		r.stats.Sent++
	} else {
		r.stats.Retransmitted++
	}
	f.sends++
	h := Header{Handle: r.handle, RN: rn, SN: sn}.Bytes()
	pkt := make([]byte, 0, 2+len(f.data))
	return append(append(pkt, h[:]...), f.data...)
}

func (r *reliable) armRetransmitLocked() {
	ct := &clockTimer{}
	r.retx = ct
	ct.t = r.clk.AfterFunc(r.rto, func() { r.onRetransmitTimeout(ct) })
}

func (r *reliable) stopRetransmitLocked() {
	if r.retx != nil {
		r.retx.t.Stop()
		r.retx = nil
	}
}

func (r *reliable) onRetransmitTimeout(ct *clockTimer) {
	r.mu.Lock()
	if r.closed || r.retx != ct {
		r.mu.Unlock()
		return
	}
	r.retx = nil
	if len(r.sent) == 0 {
		r.mu.Unlock()
		return
	}
	r.timeouts++
	if r.timeouts > r.cfg.MaxRetransmit {
		r.closeLocked()
		r.mu.Unlock()
		r.fail(ErrRetransmitLimit)
		return
	}
	r.rto = min(2*r.rto, r.cfg.MaxRTO)
	r.window = max(1, r.window/2)
	for sn := r.lastAcked; sn != r.nextSend; sn = (sn + 1) % SeqModulus {
		if f, ok := r.sent[sn]; ok {
			r.emitData(sn, f)
		}
	}
	r.armRetransmitLocked()
	r.mu.Unlock()
}

// onPacket processes one received MLR packet for this handle.
func (r *reliable) onPacket(pkt []byte) {
	if len(pkt) < 2 {
		return
	}
	h := ParseHeader(pkt[0], pkt[1])
	var payload []byte
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.handleAckLocked(h.RN)
	if len(pkt) > 2 {
		if h.SN == r.nextRecv {
			r.nextRecv = (r.nextRecv + 1) % SeqModulus
			r.recvTotal++
			r.stats.Received++
			payload = pkt[2:]
			if seqDiff(r.nextRecv, r.lastSentAck) >= r.cfg.AckThreshold {
				r.sendAckLocked()
			} else {
				r.armAckTimerLocked()
			}
		} else {
			// Duplicate or out of order: drop it and tell the peer what we expect.
			r.stats.Duplicates++
			r.sendAckLocked()
		}
	}
	r.mu.Unlock()
	if payload != nil {
		r.deliver(payload)
	}
}

// handleAckLocked applies a cumulative acknowledgement. RN values outside the outstanding
// range are stale and ignored (Gadgetbridge does not check, see docs/protocol/01 §3.4).
func (r *reliable) handleAckLocked(rn int) {
	if rn == r.lastAcked {
		return
	}
	acked := seqDiff(rn, r.lastAcked)
	if acked > seqDiff(r.nextSend, r.lastAcked) {
		r.stats.StaleAcks++
		return
	}
	for sn := r.lastAcked; sn != rn; sn = (sn + 1) % SeqModulus {
		delete(r.sent, sn)
	}
	r.lastAcked = rn
	r.timeouts = 0
	r.rto = r.cfg.InitialRTO
	r.window = min(r.window+acked, r.cfg.Window)
	r.stopRetransmitLocked()
	if len(r.sent) > 0 {
		r.armRetransmitLocked()
	}
	r.pumpLocked()
}

func (r *reliable) armAckTimerLocked() {
	r.stopAckTimerLocked()
	ct := &clockTimer{}
	r.ackTimer = ct
	ct.t = r.clk.AfterFunc(r.cfg.AckDelay, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.closed || r.ackTimer != ct {
			return
		}
		r.ackTimer = nil
		if r.lastSentAck != r.nextRecv {
			r.sendAckLocked()
		}
	})
}

func (r *reliable) stopAckTimerLocked() {
	if r.ackTimer != nil {
		r.ackTimer.t.Stop()
		r.ackTimer = nil
	}
}

// sendAckLocked queues an ACK-only packet. It is built when it goes out, with the RN of that
// moment, so one queued ACK covers everything received until then.
func (r *reliable) sendAckLocked() {
	r.stopAckTimerLocked()
	if r.ackQueued {
		return
	}
	r.ackQueued = true
	r.emit(r.buildAck, true)
}

func (r *reliable) buildAck() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ackQueued = false
	if r.closed {
		return nil
	}
	r.stopAckTimerLocked()
	h := Header{Handle: r.handle, RN: r.nextRecv, SN: 0}.Bytes()
	r.lastSentAck = r.nextRecv
	r.stats.AcksSent++
	return h[:]
}

// flush blocks until every queued and sent fragment has been acknowledged, the stream closes,
// or done is closed.
func (r *reliable) flush(done <-chan struct{}) error {
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-done:
		case <-stop:
			return
		}
		r.mu.Lock()
		r.idle.Broadcast()
		r.mu.Unlock()
	}()
	r.mu.Lock()
	defer r.mu.Unlock()
	for !r.closed && (len(r.queue) > 0 || len(r.sent) > 0) {
		select {
		case <-done:
			return errors.New("multilink: flush interrupted")
		default:
		}
		r.idle.Wait()
	}
	if len(r.queue) > 0 || len(r.sent) > 0 {
		return ErrClosed
	}
	return nil
}

func (r *reliable) snapshot() StreamStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stats
}

func (r *reliable) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeLocked()
}

func (r *reliable) closeLocked() {
	r.closed = true
	r.stopRetransmitLocked()
	r.stopAckTimerLocked()
	r.idle.Broadcast()
}
