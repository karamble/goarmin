// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package clock

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestFakeNowOnlyMovesWhenAdvanced(t *testing.T) {
	start := time.Now()
	f := NewFake(start)
	if !f.Now().Equal(start) {
		t.Fatalf("Now = %v, want %v", f.Now(), start)
	}
	f.Advance(90 * time.Second)
	if got := f.Now().Sub(start); got != 90*time.Second {
		t.Fatalf("advanced by %v, want 90s", got)
	}
	f.Set(start) // never backwards
	if got := f.Now().Sub(start); got != 90*time.Second {
		t.Fatalf("Set moved the clock backwards to +%v", got)
	}
}

func TestFakeTimersFireInDeadlineOrder(t *testing.T) {
	start := time.Now()
	f := NewFake(start)
	var order []string
	var seenAt []time.Duration
	record := func(name string) func() {
		return func() {
			order = append(order, name)
			seenAt = append(seenAt, f.Now().Sub(start))
		}
	}
	f.AfterFunc(3*time.Second, record("c"))
	f.AfterFunc(time.Second, record("a"))
	f.AfterFunc(2*time.Second, record("b1"))
	f.AfterFunc(2*time.Second, record("b2")) // same deadline: creation order
	f.AfterFunc(10*time.Second, record("late"))

	f.Advance(5 * time.Second)
	want := []string{"a", "b1", "b2", "c"}
	if len(order) != len(want) {
		t.Fatalf("fired %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("fired %v, want %v", order, want)
		}
	}
	if seenAt[0] != time.Second || seenAt[3] != 3*time.Second {
		t.Fatalf("callbacks saw times %v, want each timer's own deadline", seenAt)
	}
	if f.Pending() != 1 {
		t.Fatalf("pending = %d, want 1 (the late timer)", f.Pending())
	}
}

func TestFakeTimerChannelStopReset(t *testing.T) {
	start := time.Now()
	f := NewFake(start)
	tm := f.NewTimer(time.Minute)
	f.Advance(59 * time.Second)
	select {
	case <-tm.C():
		t.Fatal("timer fired early")
	default:
	}
	if !tm.Reset(time.Minute) {
		t.Fatal("Reset of an active timer must return true")
	}
	f.Advance(59 * time.Second) // 118 s since start, 59 s since reset
	select {
	case <-tm.C():
		t.Fatal("timer fired before the reset deadline")
	default:
	}
	f.Advance(time.Second)
	select {
	case at := <-tm.C():
		if at.Sub(start) != 119*time.Second {
			t.Fatalf("fired at +%v, want +119s", at.Sub(start))
		}
	default:
		t.Fatal("timer did not fire at its reset deadline")
	}
	if tm.Stop() {
		t.Fatal("Stop after firing must return false")
	}

	stopped := f.NewTimer(time.Second)
	if !stopped.Stop() {
		t.Fatal("Stop of an active timer must return true")
	}
	f.Advance(time.Hour)
	select {
	case <-stopped.C():
		t.Fatal("stopped timer fired")
	default:
	}
}

// A callback may re-arm a timer; Advance keeps firing as long as deadlines fall in range.
func TestFakeCallbackRearms(t *testing.T) {
	f := NewFake(time.Now())
	n := 0
	var tm Timer
	tm = f.AfterFunc(time.Second, func() {
		n++
		tm.Reset(time.Second)
	})
	f.Advance(5 * time.Second)
	if n != 5 {
		t.Fatalf("periodic callback ran %d times in 5s, want 5", n)
	}
}

func TestFakeWaitForTimers(t *testing.T) {
	f := NewFake(time.Now())
	var wg sync.WaitGroup
	fired := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		tm := f.NewTimer(time.Second)
		<-tm.C()
		close(fired)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := f.WaitForTimers(ctx, 1); err != nil {
		t.Fatal(err)
	}
	f.Advance(time.Second)
	<-fired
	wg.Wait()
}

func TestRealClock(t *testing.T) {
	c := Real()
	before := time.Now()
	if c.Now().Before(before) {
		t.Fatal("Real().Now() is behind time.Now()")
	}
	tm := c.NewTimer(time.Millisecond)
	select {
	case <-tm.C():
	case <-time.After(5 * time.Second):
		t.Fatal("real timer did not fire")
	}
	done := make(chan struct{})
	c.AfterFunc(time.Millisecond, func() { close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("real AfterFunc did not run")
	}
}
