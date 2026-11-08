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
	"github.com/karamble/goarmin/pkg/garmin/product"
	"github.com/karamble/goarmin/pkg/zipwalk"
)

// MaxFileSize bounds a single imported file.
const MaxFileSize = 64 << 20

// Report summarises an import.
type Report struct {
	Seen       int `json:"seen"`       // FIT files found
	New        int `json:"new"`        // stored for the first time
	Known      int `json:"known"`      // already stored (same bytes)
	Duplicates int `json:"duplicates"` // a copy of a recording already stored with other bytes
	Invalid    int `json:"invalid"`    // a .fit name but not a valid FIT file (stored, not ingested)
	Ingested   int `json:"ingested"`
	Failed     int `json:"failed"`
	// Waiting are files recorded while nobody owned the watch (after a hand-over, before the
	// next person adds it); they stay in the spool until someone does.
	Waiting int `json:"waiting"`
	// Unchanged FIT files were not read again: a USB import found them as it left them.
	Unchanged int `json:"unchanged"`
	// Kept are a Garmin Connect export's other files (JSON mostly), stored as they are for
	// readers to come; KeptBy counts every such file by the export's folder.
	Kept   int            `json:"kept"`
	KeptBy map[string]int `json:"kept_by,omitempty"`
	// Export: the import held a Garmin Connect export.
	Export bool `json:"export"`
	// Other files were neither FIT nor part of an export, and were left out.
	Other    int   `json:"other"`
	Archives int   `json:"archives"` // zip archives opened (set by the caller)
	Bytes    int64 `json:"bytes"`    // read
	// Read counts every file the import has dealt with so far, of whatever kind; Found how many
	// it knows of (set by the caller, 0 if it cannot tell: a zip's nested archives add theirs
	// when they are opened); Current is the path being read while it runs.
	Read    int    `json:"read"`
	Found   int    `json:"found,omitempty"`
	Current string `json:"current,omitempty"`
	// Errors are the first maxErrors problems, ErrorsMore how many more there were.
	Errors     []string `json:"errors,omitempty"`
	ErrorsMore int      `json:"errors_more,omitempty"`
	// Watches counts the FIT files per watch (device id) that made them.
	Watches map[int64]int `json:"-"`

	t *touched // the days the import touched, rebuilt at its end
}

func (r *Report) touched() *touched {
	if r.t == nil {
		r.t = newTouched()
	}
	return r.t
}

// finish rebuilds the days the import touched and classifies the devices its files told
// apart (a device first seen with this import's files).
func (in *Ingester) finish(ctx context.Context, rep *Report) error {
	ctx = context.WithoutCancel(ctx)
	err := in.Store.ClassifyDevices(ctx)
	if rep.t == nil {
		return err
	}
	return errors.Join(err, in.rebuild(ctx, rep.t))
}

const maxErrors = 200

func (r *Report) addError(msg string) {
	if len(r.Errors) < maxErrors {
		r.Errors = append(r.Errors, msg)
		return
	}
	r.ErrorsMore++
}

// Import stores and ingests what is at paths (files or folders, walked) for user: FIT files,
// zip archives with what they hold (a USB copy, a Garmin Connect export), and an export's
// other files, kept. source is store.SourceUSB, store.SourceBLE, …. A file made by a watch is
// linked to that watch; a watch nobody owns yet becomes user's.
func (in *Ingester) Import(ctx context.Context, user store.User, source string, paths []string) (Report, error) {
	zw := &zipwalk.Walker{}
	rep, err := in.ImportFrom(ctx, user, Paths(paths, zw), ImportOptions{Source: source})
	rep.Archives = zw.Archives()
	return rep, err
}

// ErrHandedOver is an import's error for a device that was handed over and not added again.
var ErrHandedOver = errors.New("handed over")

// handedOver says so for d in words: a watch is added again under Devices, anything else
// simply by its next owner's import.
func handedOver(d store.Device) error {
	if d.Kind == product.Watch || d.Kind == "" {
		return fmt.Errorf("this watch was %w; add it under Devices › Add a watch first", ErrHandedOver)
	}
	return fmt.Errorf("this %s was %w to another account", d.Kind.Words(), ErrHandedOver)
}

func (in *Ingester) importFile(ctx context.Context, user store.User, source, path string, rep *Report) error {
	b, err := readFile(path)
	if err != nil {
		return err
	}
	_, err = in.importBytes(ctx, user, source, filepath.Base(path), b, rep)
	return err
}

// importBytes stores and ingests one FIT file for user, into the account that owned its watch
// when it was recorded. It returns the file's digest.
func (in *Ingester) importBytes(ctx context.Context, user store.User, source, name string, b []byte, rep *Report) (string, error) {
	return in.importPrepared(ctx, user, source, name, prepare(b), rep)
}

// importPrepared is importBytes with the file read and decoded already.
func (in *Ingester) importPrepared(ctx context.Context, user store.User, source, name string, p *prepared, rep *Report) (string, error) {
	info := p.info
	var dev int64
	if p.ierr == nil && isWatchFile(info) {
		d, err := in.Store.EnsureDevice(ctx, store.Device{UnitID: info.SerialNumber, Product: info.Product})
		if err != nil {
			return "", err
		}
		// The file belongs to whoever owned the watch when it was recorded; a watch nobody
		// ever owned becomes the importer's.
		owner, err := in.Store.Owner(ctx, d.ID, recorded(info, time.Now()))
		if errors.Is(err, store.ErrNotFound) {
			owner, err = in.Store.AssignIfUnowned(ctx, d.ID, user.ID)
			if errors.Is(err, store.ErrNotFound) {
				return "", handedOver(d)
			}
		}
		if err != nil {
			return "", err
		}
		if owner != user.ID {
			return "", fmt.Errorf("the %s %d belonged to another account when this was recorded", d.Kind.Words(), info.SerialNumber)
		}
		dev = d.ID
		if rep.Watches == nil {
			rep.Watches = map[int64]int{}
		}
		rep.Watches[dev]++
	}
	return in.store(ctx, user, dev, source, name, p, rep)
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
			rep.addError(p + ": " + err.Error())
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
		if _, err := in.store(ctx, u, dev.ID, source, filepath.Base(p), &prepared{b: b, info: info, ierr: ierr}, &rep); err != nil {
			return fail(err)
		}
		if err := os.Remove(p); err != nil {
			in.log().Warn("ingest: removing an imported file from the spool", "file", p, "err", err)
		}
		return nil
	})
	return rep, errors.Join(err, in.finish(ctx, &rep))
}

// store records one file's bytes for user (linked to device, if not 0) and ingests it; p.dec,
// if set, is what it holds already.
func (in *Ingester) store(ctx context.Context, user store.User, device int64, source, name string, p *prepared, rep *Report) (string, error) {
	info := p.info
	sum, err := in.Blobs.PutBytes(p.b)
	if err != nil {
		return "", err
	}
	f := store.RawFile{SHA256: sum, Size: int64(len(p.b)), UserID: user.ID, DeviceID: device, Source: source, SourceName: name}
	if p.ierr != nil {
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
		return "", err
	}
	switch {
	case !isNew:
		rep.Known++
		return sum, nil
	case rf.DuplicateOf != 0:
		rep.Duplicates++
		return sum, nil
	}
	rep.New++
	if rf.FIT == nil {
		return sum, nil
	}
	if err := in.ingest(ctx, rf, p.dec, rep.touched()); err != nil {
		return sum, err
	}
	rep.Ingested++
	if err := in.rebuildDue(ctx, rep.touched()); err != nil {
		return sum, err
	}
	return sum, nil
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
