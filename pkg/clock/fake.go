// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package clock

import (
	"context"
	"sort"
	"sync"
	"time"
)

// Fake is a manually driven clock. Time stands still until Advance or Set moves it; timers
// whose deadline is reached fire in deadline order, each one seeing Now() equal to its own
// deadline. AfterFunc callbacks run synchronously in the goroutine calling Advance, after the
// clock's lock is released, so they may use the clock themselves.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	timers  []*fakeTimer
	seq     uint64
	changed chan struct{} // closed and replaced whenever the set of timers changes
}

// NewFake returns a Fake clock starting at start. Tests should pass a value derived from
// time.Now(), never a literal date.
func NewFake(start time.Time) *Fake {
	return &Fake{now: start, changed: make(chan struct{})}
}

type fakeTimer struct {
	f        *Fake
	deadline time.Time
	seq      uint64 // tie-breaker: timers with equal deadlines fire in creation order
	c        chan time.Time
	fn       func()
	active   bool
}

// Now returns the fake current time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// NewTimer implements Clock.
func (f *Fake) NewTimer(d time.Duration) Timer {
	return f.add(d, make(chan time.Time, 1), nil)
}

// AfterFunc implements Clock.
func (f *Fake) AfterFunc(d time.Duration, fn func()) Timer {
	return f.add(d, nil, fn)
}

func (f *Fake) add(d time.Duration, c chan time.Time, fn func()) *fakeTimer {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	t := &fakeTimer{f: f, deadline: f.now.Add(d), seq: f.seq, c: c, fn: fn, active: true}
	f.timers = append(f.timers, t)
	f.notifyLocked()
	return t
}

func (f *Fake) notifyLocked() {
	close(f.changed)
	f.changed = make(chan struct{})
}

// Advance moves the clock forward by d and fires every timer that falls due.
func (f *Fake) Advance(d time.Duration) { f.Set(f.Now().Add(d)) }

// Set moves the clock to t (never backwards) and fires every timer that falls due.
func (f *Fake) Set(t time.Time) {
	for {
		f.mu.Lock()
		if t.Before(f.now) {
			t = f.now
		}
		next := f.nextDueLocked(t)
		if next == nil {
			f.now = t
			f.mu.Unlock()
			return
		}
		f.now = next.deadline
		next.active = false
		f.removeLocked(next)
		fn, c, at := next.fn, next.c, f.now
		f.mu.Unlock()
		if fn != nil {
			fn()
		} else {
			select {
			case c <- at:
			default: // a pending, unread tick stays; like time.Timer, it does not queue up
			}
		}
	}
}

func (f *Fake) nextDueLocked(until time.Time) *fakeTimer {
	var due []*fakeTimer
	for _, t := range f.timers {
		if t.active && !t.deadline.After(until) {
			due = append(due, t)
		}
	}
	if len(due) == 0 {
		return nil
	}
	sort.Slice(due, func(i, j int) bool {
		if !due[i].deadline.Equal(due[j].deadline) {
			return due[i].deadline.Before(due[j].deadline)
		}
		return due[i].seq < due[j].seq
	})
	return due[0]
}

func (f *Fake) removeLocked(t *fakeTimer) {
	for i, x := range f.timers {
		if x == t {
			f.timers = append(f.timers[:i], f.timers[i+1:]...)
			break
		}
	}
	f.notifyLocked()
}

// Pending returns the number of active timers.
func (f *Fake) Pending() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.timers)
}

// WaitForTimers blocks until at least n timers are active or ctx ends. Tests use it to wait
// until goroutines under test have armed their timers before calling Advance.
func (f *Fake) WaitForTimers(ctx context.Context, n int) error {
	for {
		f.mu.Lock()
		have, changed := len(f.timers), f.changed
		f.mu.Unlock()
		if have >= n {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (t *fakeTimer) C() <-chan time.Time { return t.c }

func (t *fakeTimer) Stop() bool {
	f := t.f
	f.mu.Lock()
	defer f.mu.Unlock()
	if !t.active {
		return false
	}
	t.active = false
	f.removeLocked(t)
	return true
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	f := t.f
	f.mu.Lock()
	defer f.mu.Unlock()
	wasActive := t.active
	if wasActive {
		f.removeLocked(t)
	}
	f.seq++
	t.deadline, t.seq, t.active = f.now.Add(d), f.seq, true
	f.timers = append(f.timers, t)
	f.notifyLocked()
	return wasActive
}
