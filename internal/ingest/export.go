// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ingest

import (
	"context"
	"errors"
	"fmt"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/garmin/gcexport"
)

// ExportHandler and ExportVersion name the reading of a Garmin Connect export's kept files
// (its JSON and the rest). Bump ExportVersion when gcexport reads more: every kept file is read
// again on the next Pending run.
const (
	ExportHandler = "gcjson"
	ExportVersion = 2 // 2: Garmin Connect's workouts join the workout library
)

// exportFile reads one kept export file from b (nil: from the blob store) and writes what it
// holds. A file gcexport cannot read is marked failed; it stays stored as it is.
func (in *Ingester) exportFile(ctx context.Context, f store.RawFile, b []byte, t *touched) error {
	if b == nil {
		var err error
		if b, err = in.Blobs.ReadFile(f.SHA256); err != nil {
			return fmt.Errorf("ingest: export file %d: %w", f.ID, err)
		}
	}
	r, err := gcexport.Read(f.SourceName, b)
	return in.writeExport(ctx, f, r, err, t)
}

// writeExport writes what gcexport read from f (r), or marks it failed (readErr).
func (in *Ingester) writeExport(ctx context.Context, f store.RawFile, r *gcexport.Result, readErr error, t *touched) error {
	if readErr != nil {
		if rerr := in.Store.IngestFailed(ctx, f.ID, ExportHandler, ExportVersion, readErr); rerr != nil {
			return errors.Join(readErr, rerr)
		}
		return fmt.Errorf("ingest: export file %d (%s): %w", f.ID, r.Kind, readErr)
	}
	days, err := in.Store.WriteExportFile(ctx, f, r, ExportHandler, ExportVersion)
	if err != nil {
		return fmt.Errorf("ingest: export file %d (%s): %w", f.ID, r.Kind, err)
	}
	t.add(f.UserID, days)
	return nil
}

// pendingExport reads every kept export file not yet read at ExportVersion.
func (in *Ingester) pendingExport(ctx context.Context, t *touched) (done, failed int, err error) {
	ids, err := in.Store.PendingExportFiles(ctx, ExportHandler, ExportVersion)
	if err != nil {
		return 0, 0, err
	}
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return done, failed, err
		}
		f, err := in.Store.RawFile(ctx, id)
		if err == nil {
			err = in.exportFile(ctx, f, nil, t)
		}
		if err != nil {
			failed++
			in.log().Warn("ingest failed", "file", id, "err", err)
			continue
		}
		done++
	}
	return done, failed, nil
}
