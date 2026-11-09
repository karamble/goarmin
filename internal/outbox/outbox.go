// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package outbox turns what a watch should get from its owner's workout library into files to
// push at its next sync, and records what became of them. The server builds it (for its own
// hub, or for a sync node over the node API); the sync pushes the files.
package outbox

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/blobstore"
	"github.com/karamble/goarmin/pkg/fitwatch"
)

// WorkoutType is the GDXML type name a workout is pushed as.
const WorkoutType = "FIT_TYPE_5"

// MaxFiles is the most files one sync pushes; the rest follow at the next.
const MaxFiles = 50

// File is one file for the watch.
type File struct {
	// Key names what it is: "<reason>:<library entry key>".
	Key      string
	Type     string
	Data     []byte
	Announce bool
}

// Result is what became of one pushed file.
type Result struct {
	Key    string
	SHA256 string
	UID    string // the watch's id for it ("" if it named none)
	Err    string // why the push failed ("" if it did not)
	// Refused says the watch said no; Unsupported that it takes no pushes of that kind.
	Refused, Unsupported bool
}

// Key is the name of a file sent for a reason (store.SendReason*) and a library entry.
func Key(reason, entryKey string) string { return reason + ":" + entryKey }

// ParseKey splits a file's Key into its reason and library entry.
func ParseKey(k string) (reason, entryKey string, ok bool) {
	return strings.Cut(k, ":")
}

// Build returns the files device should get from user's library at its next sync: a watch's
// original bytes where the version shown is a watch's file, else the version written as a
// workout file (a rename, Garmin Connect's). A version that cannot be written is left out and
// returned in the error, joined.
func Build(ctx context.Context, st *store.Store, blobs *blobstore.Store, user, device int64) ([]File, error) {
	entries, err := st.WatchOutbox(ctx, user, device)
	if err != nil {
		return nil, err
	}
	var out []File
	var errs []error
	for _, e := range entries {
		if len(out) == MaxFiles {
			break
		}
		data, err := WorkoutFile(ctx, st, blobs, e)
		if err != nil {
			errs = append(errs, fmt.Errorf("outbox: %s: %w", e.Version.Name, err))
			continue
		}
		out = append(out, File{Key: Key(store.SendReasonLibrary, e.EntryKey), Type: WorkoutType, Data: data, Announce: true})
	}
	return out, errors.Join(errs...)
}

// WorkoutFile is the workout file for an entry's version: a watch's file as it is, else the
// version written as Garmin Connect writes workouts (the same bytes for the same version).
func WorkoutFile(ctx context.Context, st *store.Store, blobs *blobstore.Store, e store.OutboxEntry) ([]byte, error) {
	v := e.Version
	if v.Origin == store.OriginWatch && v.RawFileID != 0 {
		f, err := st.RawFile(ctx, v.RawFileID)
		if err != nil {
			return nil, err
		}
		return blobs.ReadFile(f.SHA256)
	}
	// A file of goarmin's own: the same bytes for the same version, so a resend is the same
	// workout to the watch.
	h := fnv.New32a()
	h.Write([]byte(e.EntryKey))
	fmt.Fprint(h, v.ID)
	return fitwatch.EncodeWorkout(v.Workout, e.Description, fitwatch.EncodeOptions{Serial: h.Sum32() | 1, Created: v.RecordedAt})
}

// Record keeps what became of the files pushed to device for user at at.
func Record(ctx context.Context, st *store.Store, user, device int64, results []Result, at time.Time) error {
	var recs []store.SendRecord
	for _, r := range results {
		reason, entry, ok := ParseKey(r.Key)
		if !ok || reason != store.SendReasonLibrary || entry == "" {
			continue
		}
		recs = append(recs, store.SendRecord{EntryKey: entry, Reason: reason, SHA256: r.SHA256, UID: r.UID, Err: r.Err,
			Refused: r.Refused, Unsupported: r.Unsupported})
	}
	return st.RecordSends(ctx, user, device, recs, at)
}
