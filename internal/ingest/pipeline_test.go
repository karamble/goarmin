// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ingest

import (
	"context"
	"errors"
	"math/rand/v2"
	"testing"
	"time"
)

// TestOrdered: work runs concurrently, results arrive in the order submitted, and the
// consumer's error stops the producer.
func TestOrdered(t *testing.T) {
	var got []int
	err := ordered(context.Background(), 4, func(submit func(func() int) error) error {
		for i := range 200 {
			if err := submit(func() int {
				time.Sleep(time.Duration(rand.IntN(300)) * time.Microsecond)
				return i
			}); err != nil {
				return err
			}
		}
		return nil
	}, func(v int) error {
		got = append(got, v)
		return nil
	})
	if err != nil || len(got) != 200 {
		t.Fatalf("%d results, %v", len(got), err)
	}
	for i, v := range got {
		if v != i {
			t.Fatalf("result %d is %d: out of order", i, v)
		}
	}

	stop := errors.New("stop")
	n, submitted := 0, 0
	err = ordered(context.Background(), 2, func(submit func(func() int) error) error {
		for i := range 1000 {
			if err := submit(func() int { return i }); err != nil {
				return err
			}
			submitted++
		}
		return nil
	}, func(v int) error {
		if n++; v == 10 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) || n != 11 || submitted == 1000 {
		t.Fatalf("err %v after %d consumed, %d submitted", err, n, submitted)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = ordered(ctx, 2, func(submit func(func() int) error) error {
		return submit(func() int { return 1 })
	}, func(int) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
}
