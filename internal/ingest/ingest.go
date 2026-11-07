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

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/blobstore"
	"github.com/karamble/goarmin/pkg/fitactivity"
	"github.com/karamble/goarmin/pkg/fitwatch"
	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

// Handler and Version name the extraction. Bump Version when the extraction changes: every
// file is then ingested again on the next Pending run (goarmin reingest).
const (
	Handler = "fit"
	// 2: step goal, sleep need, nightly HRV; 3: activities, Garmin Coach; 4: four weeks of
	// nightly HRV; 5: timestamp_16 from the last monitoring record; 6: the watch's own time in
	// zones, its settings, sport profiles, goals, lifetime totals, workouts and courses.
	Version = 6
)

// fitTypeActivity is FIT file type 4: its sessions become activities.
const fitTypeActivity = 4

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
	f, err := in.Store.RawFile(ctx, id)
	if err != nil {
		return err
	}
	if f.DuplicateOf != 0 || f.FIT == nil {
		return nil
	}
	b, err := in.Blobs.ReadFile(f.SHA256)
	if err != nil {
		return fmt.Errorf("ingest: file %d: %w", id, err)
	}
	d, err := wellness.Decode(b)
	if err != nil {
		if rerr := in.Store.IngestFailed(ctx, id, Handler, Version, err); rerr != nil {
			return errors.Join(err, rerr)
		}
		return fmt.Errorf("ingest: file %d: %w", id, err)
	}
	var acts []fitactivity.Activity
	if f.FIT.Type == fitTypeActivity {
		if acts, err = fitactivity.Decode(b); err != nil && !errors.Is(err, fitactivity.ErrNoSession) {
			if rerr := in.Store.IngestFailed(ctx, id, Handler, Version, err); rerr != nil {
				return errors.Join(err, rerr)
			}
			return fmt.Errorf("ingest: file %d: %w", id, err)
		}
	}
	if slices.Contains(fitwatch.Types, typedef.File(f.FIT.Type)) {
		w, err := fitwatch.Decode(b)
		if err == nil {
			err = in.Store.WriteWatchFile(ctx, f, w)
		}
		if err != nil {
			if rerr := in.Store.IngestFailed(ctx, id, Handler, Version, err); rerr != nil {
				return errors.Join(err, rerr)
			}
			return fmt.Errorf("ingest: file %d: %w", id, err)
		}
	}
	days, err := in.Store.WriteFileData(ctx, f, d, acts, Handler, Version)
	if err != nil {
		return fmt.Errorf("ingest: file %d: %w", id, err)
	}
	return in.Store.RebuildDays(ctx, f.UserID, days)
}

// Pending ingests every file not yet ingested at the current Version and returns how many it
// ingested and how many failed.
func (in *Ingester) Pending(ctx context.Context) (done, failed int, err error) {
	ids, err := in.Store.PendingIngest(ctx, Handler, Version)
	if err != nil {
		return 0, 0, err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return done, failed, err
		}
		if err := in.File(ctx, id); err != nil {
			failed++
			in.log().Warn("ingest failed", "file", id, "err", err)
			continue
		}
		done++
	}
	return done, failed, nil
}
