// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package multilink

import "fmt"

// DebugState describes an MLR channel's internal state for test failure messages.
func (c *Channel) DebugState() string {
	if c.rel == nil {
		return "plain"
	}
	r := c.rel
	r.mu.Lock()
	defer r.mu.Unlock()
	c.qmu.Lock()
	q := len(c.q)
	c.qmu.Unlock()
	return fmt.Sprintf("closed=%v lastAcked=%d nextSend=%d queue=%d sent=%d window=%d rto=%s retxArmed=%v timeouts=%d nextRecv=%d lastSentAck=%d ackArmed=%v recvQ=%d stats=%+v",
		r.closed, r.lastAcked, r.nextSend, len(r.queue), len(r.sent), r.window, r.rto, r.retx != nil, r.timeouts,
		r.nextRecv, r.lastSentAck, r.ackTimer != nil, q, r.stats)
}
