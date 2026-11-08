// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ingest

import (
	"context"

	"github.com/karamble/goarmin/internal/store"
)

// DeleteDeviceData removes what came from device into user's account (store.DeleteDeviceData),
// then repairs what it may have shared with other files: rows keyed without the file (samples
// of two watches worn together, the time-zone history, merged nights) are re-ingested from the
// remaining files that overlap the deleted span, the touched days are rebuilt, and the deleted
// files leave the blob store.
func (in *Ingester) DeleteDeviceData(ctx context.Context, user, device int64) (store.DeviceDataDeleted, error) {
	gone, err := in.Store.DeleteDeviceData(ctx, user, device)
	if err != nil || gone.Files == 0 {
		return gone, err
	}
	if gone.From != 0 && gone.To != 0 {
		ids, err := in.Store.OverlappingFiles(ctx, user, gone.From, gone.To)
		if err != nil {
			return gone, err
		}
		t := newTouched()
		for _, id := range ids {
			if err := in.file(ctx, id, t); err != nil {
				in.log().Warn("ingest: re-ingesting an overlapping file after a delete", "file", id, "err", err)
			}
		}
		t.add(user, gone.Days)
		if err := in.rebuild(ctx, t); err != nil {
			return gone, err
		}
	} else if err := in.Store.RebuildDays(ctx, user, gone.Days); err != nil {
		return gone, err
	}
	for _, sum := range gone.Blobs {
		if err := in.Blobs.Remove(sum); err != nil {
			in.log().Warn("ingest: removing a deleted file's blob", "sha256", sum, "err", err)
		}
	}
	return gone, nil
}
