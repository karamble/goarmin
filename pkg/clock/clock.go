// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package clock abstracts time so that code with timers (MLR retransmission, sync timeouts,
// schedulers) can be tested deterministically. Production code uses Real(); tests use a Fake
// that only moves when told to.
package clock

import "time"

// Clock is the subset of the time package goarmin's code depends on.
type Clock interface {
	Now() time.Time
	// NewTimer returns a timer that sends the current time on C() after d.
	NewTimer(d time.Duration) Timer
	// AfterFunc calls f after d. With a Fake clock, f runs synchronously inside Advance.
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer mirrors *time.Timer.
type Timer interface {
	// C returns the channel the timer fires on; it is nil for timers made with AfterFunc.
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

// Real returns the system clock.
func Real() Clock { return realClock{} }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) NewTimer(d time.Duration) Timer {
	t := time.NewTimer(d)
	return realTimer{t: t, c: t.C}
}

func (realClock) AfterFunc(d time.Duration, f func()) Timer {
	return realTimer{t: time.AfterFunc(d, f)}
}

type realTimer struct {
	t *time.Timer
	c <-chan time.Time
}

func (r realTimer) C() <-chan time.Time        { return r.c }
func (r realTimer) Stop() bool                 { return r.t.Stop() }
func (r realTimer) Reset(d time.Duration) bool { return r.t.Reset(d) }
