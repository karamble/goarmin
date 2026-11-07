// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ingest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/fitid"
)

// MaxFileSize bounds a single imported file.
const MaxFileSize = 64 << 20

// Report summarises an import.
type Report struct {
	Seen       int // FIT files found
	New        int // stored for the first time
	Known      int // already stored (same bytes)
	Duplicates int // a copy of a recording already stored with other bytes
	Invalid    int // a .fit name but not a valid FIT file (stored, not ingested)
	Ingested   int
	Failed     int
	// Waiting are files recorded while nobody owned the watch (after a hand-over, before the
	// next person adds it); they stay in the spool until someone does.
	Waiting int
	Errors  []string
}

// Import stores and ingests the FIT files at paths (files or directories, walked) for user.
// source is store.SourceUSB, store.SourceBLE, …. A file made by a watch is linked to that
// watch; a watch nobody owns yet becomes user's.
func (in *Ingester) Import(ctx context.Context, user store.User, source string, paths []string) (Report, error) {
	var rep Report
	for _, root := range paths {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".fit") || !d.Type().IsRegular() {
				return nil
			}
			rep.Seen++
			if err := in.importFile(ctx, user, source, p, &rep); err != nil {
				rep.Failed++
				rep.Errors = append(rep.Errors, p+": "+err.Error())
			}
			return nil
		})
		if err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// ErrHandedOver is an import's error for a watch that was handed over and not added again.
var ErrHandedOver = errors.New("this watch was handed over; add it under Devices › Add a watch first")

func (in *Ingester) importFile(ctx context.Context, user store.User, source, path string, rep *Report) error {
	b, err := readFile(path)
	if err != nil {
		return err
	}
	info, ierr := fitid.Inspect(b)
	var dev int64
	if ierr == nil && isWatchFile(info) {
		d, err := in.Store.EnsureDevice(ctx, store.Device{UnitID: info.SerialNumber, Product: info.Product})
		if err != nil {
			return err
		}
		// The file belongs to whoever owned the watch when it was recorded; a watch nobody
		// ever owned becomes the importer's.
		owner, err := in.Store.Owner(ctx, d.ID, recorded(info, time.Now()))
		if errors.Is(err, store.ErrNotFound) {
			owner, err = in.Store.AssignIfUnowned(ctx, d.ID, user.ID)
			if errors.Is(err, store.ErrNotFound) {
				return ErrHandedOver
			}
		}
		if err != nil {
			return err
		}
		if owner != user.ID {
			return fmt.Errorf("the watch %d belonged to another account when this was recorded", info.SerialNumber)
		}
		dev = d.ID
	}
	return in.store(ctx, user, dev, source, filepath.Base(path), b, info, ierr, rep)
}

// ImportWatch stores and ingests the files a sync left in dir (a watch's spool), each into the
// account that owned dev when the file was recorded. A stored file leaves the spool: the raw
// store has it, and the spool's record keeps the watch from offering it again. So a sync never
// re-reads old files, and data deleted later cannot come back from the spool. Files from a time
// nobody owned the watch stay (Waiting) until someone does.
func (in *Ingester) ImportWatch(ctx context.Context, dev store.Device, source, dir string) (Report, error) {
	var rep Report
	users := map[int64]store.User{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".fit") || !d.Type().IsRegular() {
			return nil
		}
		rep.Seen++
		fail := func(err error) error {
			rep.Failed++
			rep.Errors = append(rep.Errors, p+": "+err.Error())
			return nil
		}
		b, err := readFile(p)
		if err != nil {
			return fail(err)
		}
		info, ierr := fitid.Inspect(b)
		at := time.Now()
		if fi, err := d.Info(); err == nil {
			at = fi.ModTime()
		}
		if ierr == nil {
			at = time.Unix(recorded(info, at), 0)
		}
		owner, err := in.Store.Owner(ctx, dev.ID, at.Unix())
		if errors.Is(err, store.ErrNotFound) {
			rep.Waiting++
			return nil
		}
		if err != nil {
			return fail(err)
		}
		u, ok := users[owner]
		if !ok {
			if u, err = in.Store.User(ctx, owner); err != nil {
				return fail(err)
			}
			users[owner] = u
		}
		if err := in.store(ctx, u, dev.ID, source, filepath.Base(p), b, info, ierr, &rep); err != nil {
			return fail(err)
		}
		if err := os.Remove(p); err != nil {
			in.log().Warn("ingest: removing an imported file from the spool", "file", p, "err", err)
		}
		return nil
	})
	return rep, err
}

// store records one file's bytes for user (linked to device, if not 0) and ingests it.
func (in *Ingester) store(ctx context.Context, user store.User, device int64, source, name string, b []byte,
	info fitid.Info, infoErr error, rep *Report) error {
	sum, err := in.Blobs.PutBytes(b)
	if err != nil {
		return err
	}
	f := store.RawFile{SHA256: sum, Size: int64(len(b)), UserID: user.ID, DeviceID: device, Source: source, SourceName: name}
	if infoErr != nil {
		rep.Invalid++ // stored as it is, but nothing to ingest
	} else {
		f.FIT = &store.FITIdentity{Type: uint8(info.Type), Manufacturer: uint16(info.Manufacturer), Product: info.Product,
			Serial: info.SerialNumber, Number: info.Number}
		if !info.TimeCreated.IsZero() {
			f.FIT.TimeCreated = info.TimeCreated.Unix()
		}
	}
	rf, isNew, err := in.Store.AddRawFile(ctx, f)
	if err != nil {
		return err
	}
	switch {
	case !isNew:
		rep.Known++
		return nil
	case rf.DuplicateOf != 0:
		rep.Duplicates++
		return nil
	}
	rep.New++
	if rf.FIT == nil {
		return nil
	}
	if err := in.File(ctx, rf.ID); err != nil {
		return err
	}
	rep.Ingested++
	return nil
}

func readFile(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() > MaxFileSize {
		return nil, fmt.Errorf("larger than %d MB", MaxFileSize>>20)
	}
	return os.ReadFile(path)
}

// isWatchFile reports whether a FIT file names the Garmin watch that made it.
func isWatchFile(info fitid.Info) bool {
	return info.Manufacturer == typedef.ManufacturerGarmin && info.SerialNumber != 0 && info.Product != 0xFFFE
}

// recorded is when a file was made (Unix seconds): its time_created, else fallback.
func recorded(info fitid.Info, fallback time.Time) int64 {
	if !info.TimeCreated.IsZero() {
		return info.TimeCreated.Unix()
	}
	return fallback.Unix()
}
