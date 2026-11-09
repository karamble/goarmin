// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package ingest turns raw files into health data: it stores files in the blob store and the
// database (Import) and extracts their contents into the database tables (File, Pending).
// Every step is idempotent: importing the same file again, or ingesting a file again, changes
// nothing.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/blobstore"
)

// Handler and Version name the extraction. Bump Version when the extraction changes: every
// file is then ingested again on the next Pending run (goarmin reingest).
const (
	Handler = "fit"
	// 2: step goal, sleep need, nightly HRV; 3: activities, Garmin Coach; 4: four weeks of
	// nightly HRV; 5: timestamp_16 from the last monitoring record; 6: the watch's own time in
	// zones, its settings, sport profiles, goals, lifetime totals, workouts and courses; 7: sleep
	// stages read as the end of each interval, nights merged from parts, naps; 8: the workout an
	// activity followed; 9: workouts whole, as their files say (descriptions and notes from
	// their memos, the stated total, the fields the profile does not name).
	Version = 9
)

// fitTypeActivity is FIT file type 4: its sessions become activities; an activity summary (20)
// adds the sessions no activity file has.
const (
	fitTypeActivity        = 4
	fitTypeActivitySummary = 20
)

// Ingester ingests raw files.
type Ingester struct {
	Store *store.Store
	Blobs *blobstore.Store
	Log   *slog.Logger
}

func (in *Ingester) log() *slog.Logger {
	if in.Log == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return in.Log
}

// File ingests one raw file (again), then rebuilds the daily summaries of the days it touches.
func (in *Ingester) File(ctx context.Context, id int64) error {
	t := newTouched()
	err := in.file(ctx, id, t)
	return errors.Join(err, in.rebuild(ctx, t))
}

// touched collects the days that ingesting files touched, per account, so that a run of files
// (an import, a reingest) rebuilds each day once instead of after every file.
type touched struct {
	days  map[int64]map[string]bool
	since time.Time // of the oldest day not rebuilt yet
}

func newTouched() *touched { return &touched{days: map[int64]map[string]bool{}} }

func (t *touched) add(user int64, days []string) {
	if len(days) == 0 {
		return
	}
	m := t.days[user]
	if m == nil {
		m = map[string]bool{}
		t.days[user] = m
	}
	if len(t.days) == 1 && len(m) == 0 {
		t.since = time.Now()
	}
	for _, d := range days {
		m[d] = true
	}
}

// rebuildEvery is how often a long run of files rebuilds the days it touched so far, so the
// dashboard fills while it runs (each rebuild costs: an export's files touch the same days
// many times over).
const rebuildEvery = time.Minute

// rebuild rebuilds the touched days and forgets them.
func (in *Ingester) rebuild(ctx context.Context, t *touched) error {
	var errs []error
	for user, m := range t.days {
		days := make([]string, 0, len(m))
		for d := range m {
			days = append(days, d)
		}
		slices.Sort(days)
		if err := in.Store.RebuildDays(ctx, user, days); err != nil {
			errs = append(errs, err)
		}
	}
	clear(t.days)
	return errors.Join(errs...)
}

// rebuildDue rebuilds the touched days if the oldest waited rebuildEvery.
func (in *Ingester) rebuildDue(ctx context.Context, t *touched) error {
	if len(t.days) == 0 || time.Since(t.since) < rebuildEvery {
		return nil
	}
	return in.rebuild(ctx, t)
}

// file ingests one raw file (again), read from the blob store. The days it touches go into t.
func (in *Ingester) file(ctx context.Context, id int64, t *touched) error {
	f, err := in.Store.RawFile(ctx, id)
	if err != nil {
		return err
	}
	return in.ingest(ctx, f, nil, t)
}

// read reads f's bytes from the blob store and decodes them (for a run of files, ahead of the
// writer).
func (in *Ingester) read(f store.RawFile) (*decoded, error) {
	b, err := in.Blobs.ReadFile(f.SHA256)
	if err != nil {
		return nil, fmt.Errorf("ingest: file %d: %w", f.ID, err)
	}
	return decode(b, f.FIT.Type), nil
}

// ingest writes what dec (f decoded) holds; with dec nil it reads f first. The days it touches
// go into t.
func (in *Ingester) ingest(ctx context.Context, f store.RawFile, dec *decoded, t *touched) error {
	if f.DuplicateOf != 0 || f.FIT == nil {
		return nil
	}
	if dec == nil {
		var err error
		if dec, err = in.read(f); err != nil {
			return err
		}
	}
	failed := func(err error) error {
		if rerr := in.Store.IngestFailed(ctx, f.ID, Handler, Version, err); rerr != nil {
			return errors.Join(err, rerr)
		}
		return fmt.Errorf("ingest: file %d: %w", f.ID, err)
	}
	for _, err := range []error{dec.derr, dec.aerr, dec.werr} {
		if err != nil {
			return failed(err)
		}
	}
	if dec.w != nil {
		if err := in.Store.WriteWatchFile(ctx, f, dec.w); err != nil {
			return failed(err)
		}
	}
	days, err := in.Store.WriteFileData(ctx, f, dec.d, dec.acts, Handler, Version)
	if err != nil {
		return fmt.Errorf("ingest: file %d: %w", f.ID, err)
	}
	t.add(f.UserID, days)
	return nil
}

// Pending ingests every FIT file not yet ingested at the current Version and every kept export
// file not read at ExportVersion, and returns how many it ingested and how many failed. The days they touch are rebuilt once, at the end (and every
// rebuildEvery on the way).
func (in *Ingester) Pending(ctx context.Context) (done, failed int, err error) {
	ids, err := in.Store.PendingIngest(ctx, Handler, Version)
	if err != nil {
		return 0, 0, err
	}
	t := newTouched()
	defer func() { err = errors.Join(err, in.rebuild(context.WithoutCancel(ctx), t)) }()
	type job struct {
		f   store.RawFile
		dec *decoded
		err error
	}
	err = ordered(ctx, workers(), func(submit func(func() job) error) error {
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return err
			}
			f, err := in.Store.RawFile(ctx, id)
			if err != nil {
				return err
			}
			if err := submit(func() job {
				if f.DuplicateOf != 0 || f.FIT == nil {
					return job{f: f}
				}
				dec, err := in.read(f)
				return job{f: f, dec: dec, err: err}
			}); err != nil {
				return err
			}
		}
		return nil
	}, func(j job) error {
		err := j.err
		if err == nil && j.dec != nil {
			err = in.ingest(ctx, j.f, j.dec, t)
		}
		if err != nil {
			failed++
			in.log().Warn("ingest failed", "file", j.f.ID, "err", err)
			return nil
		}
		done++
		return in.rebuildDue(ctx, t)
	})
	if err != nil {
		return done, failed, err
	}
	// The kept files of Garmin Connect exports.
	d, f, err := in.pendingExport(ctx, t)
	return done + d, failed + f, err
}
