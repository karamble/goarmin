// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"runtime"
	"slices"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/pkg/fitactivity"
	"github.com/karamble/goarmin/pkg/fitid"
	"github.com/karamble/goarmin/pkg/fitwatch"
	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

// Ingesting a file is reading it (hashing, decoding: any number at once) and writing what it
// holds (one at a time: SQLite has one writer). A run of files decodes ahead on every core
// while the writer applies them one by one, in their order.

// workers is how many files decode at once.
func workers() int { return max(1, runtime.GOMAXPROCS(0)-1) }

// ordered runs the work that produce submits on up to n goroutines at once, and hands the
// results to consume one at a time, in the order submitted. consume's error stops the run:
// submit returns it, and ordered returns it once the submitted work has finished.
func ordered[T any](ctx context.Context, n int, produce func(submit func(work func() T) error) error, consume func(T) error) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	queue := make(chan chan T, 4*n)
	sem := make(chan struct{}, n)
	done := make(chan error, 1)
	go func() {
		var err error
		for c := range queue {
			v := <-c
			if err == nil {
				if err = consume(v); err != nil {
					cancel(err)
				}
			}
		}
		done <- err
	}()
	submit := func(work func() T) error {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
		c := make(chan T, 1)
		go func() {
			defer func() { <-sem }()
			c <- work()
		}()
		select {
		case queue <- c:
			return nil
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
	perr := produce(submit)
	close(queue)
	cerr := <-done
	if cerr != nil {
		return cerr // the producer's error is then the same one, or a consequence
	}
	return perr
}

// prepared is a FIT file read and decoded, ready for the writer.
type prepared struct {
	b    []byte
	sum  string // sha256, hex
	info fitid.Info
	ierr error    // not a valid FIT file: stored as it is, nothing to ingest
	dec  *decoded // nil with ierr
}

func prepare(b []byte) *prepared {
	h := sha256.Sum256(b)
	p := &prepared{b: b, sum: hex.EncodeToString(h[:])}
	p.info, p.ierr = fitid.Inspect(b)
	if p.ierr == nil {
		p.dec = decode(b, uint8(p.info.Type))
	}
	return p
}

// decoded is what the readers made of a FIT file of a type.
type decoded struct {
	typ  uint8
	d    *wellness.Data
	derr error
	acts []fitactivity.Activity
	aerr error
	w    *fitwatch.File // nil unless the type is one of fitwatch.Types
	werr error
}

func decode(b []byte, typ uint8) *decoded {
	x := &decoded{typ: typ}
	x.d, x.derr = wellness.Decode(b)
	if x.derr != nil {
		return x
	}
	if typ == fitTypeActivity || typ == fitTypeActivitySummary {
		if x.acts, x.aerr = fitactivity.Decode(b); errors.Is(x.aerr, fitactivity.ErrNoSession) {
			x.aerr = nil
		}
	}
	if slices.Contains(fitwatch.Types, typedef.File(typ)) {
		x.w, x.werr = fitwatch.Decode(b)
	}
	return x
}
