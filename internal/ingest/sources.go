// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ingest

import (
	"archive/zip"
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/garmin/gcexport"
	"github.com/karamble/goarmin/pkg/zipwalk"
)

// Entry is one file an import reads.
type Entry struct {
	// Path is slash-separated within the source: "GARMIN/Activity/x.fit",
	// "export.zip/DI_CONNECT/DI-Connect-Fitness/x.json".
	Path     string
	Size     int64     // -1 if unknown
	Modified time.Time // zero if unknown
	// Read reads the file. Err, if set, says why the source could not hand it over (an archive
	// beyond the limits): the import reports it and goes on.
	Read func() ([]byte, error)
	Err  error
	// InExport says the file came from an archive that is a Garmin Connect export as a whole
	// (it holds DI_CONNECT/), even if its own path lies outside DI_CONNECT (INREACH/, …).
	InExport bool
}

// Source hands an import its files, one at a time. fn's error ends the walk.
type Source func(ctx context.Context, fn func(Entry) error) error

// ImportOptions shape ImportFrom.
type ImportOptions struct {
	// Source is the raw files' source (store.SourceUSB, store.SourceUpload, …). FIT files from a
	// Garmin Connect export are stored as store.SourceGCExport whatever it says.
	Source string
	// Skip, if set, is asked before a FIT file is read: true leaves it unread (Unchanged).
	Skip func(Entry) bool
	// Stored, if set, hears of each FIT file stored or found stored, with its digest.
	Stored func(e Entry, sha string)
	// Progress, if set, gets the report after each file.
	Progress func(Report)
}

// ImportFrom stores and ingests a source's files for user. FIT files go through the same rules
// as every import: by content once, into the account that owned the watch when each was
// recorded. A Garmin Connect export's other files (JSON mostly) are kept as raw files, not
// ingested, for readers to come. Anything else is counted and left out.
func (in *Ingester) ImportFrom(ctx context.Context, user store.User, src Source, opt ImportOptions) (Report, error) {
	var rep Report
	// The walk reads the files in order and they are decoded ahead on every core; the writer
	// stores them one at a time, in the walk's order.
	type job struct {
		e      Entry
		export bool
		kind   int // what to do: one of the constants below
		p      *prepared
		b      []byte
		exp    *gcexport.Result // an export file read ahead (nil if it could not be: expErr)
		expErr error
		err    error
	}
	const (
		jobFIT = iota
		jobUnchanged
		jobKeep
		jobOther
		jobFailed
	)
	err := ordered(ctx, workers(), func(submit func(func() job) error) error {
		return src(ctx, func(e Entry) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			j := job{e: e, export: e.InExport || IsExportPath(e.Path)}
			now := func(kind int) error { j.kind = kind; return submit(func() job { return j }) }
			switch {
			case e.Err != nil:
				j.err = e.Err
				return now(jobFailed)
			case strings.EqualFold(path.Ext(e.Path), ".fit"):
				if opt.Skip != nil && opt.Skip(e) {
					return now(jobUnchanged)
				}
				b, err := e.Read()
				if err != nil {
					j.err = err
					return now(jobFailed)
				}
				return submit(func() job { j.p = prepare(b); return j })
			case j.export:
				b, err := e.Read()
				if err != nil {
					j.err = err
					return now(jobFailed)
				}
				j.b, j.kind = b, jobKeep
				return submit(func() job { j.exp, j.expErr = gcexport.Read(e.Path, b); return j })
			}
			return now(jobOther)
		})
	}, func(j job) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		rep.Read++
		rep.Current = j.e.Path
		if j.export {
			rep.Export = true
		}
		fail := func(err error) {
			rep.Failed++
			rep.addError(j.e.Path + ": " + err.Error())
		}
		switch j.kind {
		case jobFailed:
			if strings.EqualFold(path.Ext(j.e.Path), ".fit") && j.e.Err == nil {
				rep.Seen++
			}
			fail(j.err)
		case jobUnchanged:
			rep.Seen++
			rep.Unchanged++
		case jobFIT:
			rep.Seen++
			rep.Bytes += int64(len(j.p.b))
			source := opt.Source
			if j.export {
				source = store.SourceGCExport
			}
			sum, err := in.importPrepared(ctx, user, source, path.Base(j.e.Path), j.p, &rep)
			if err != nil {
				fail(err)
			}
			if sum != "" && opt.Stored != nil {
				opt.Stored(j.e, sum)
			}
		case jobKeep:
			rep.Bytes += int64(len(j.b))
			if err := in.keep(ctx, user, j.e.Path, j.b, j.exp, j.expErr, &rep); err != nil {
				fail(err)
			}
		case jobOther:
			rep.Other++
		}
		if opt.Progress != nil {
			opt.Progress(rep)
		}
		return nil
	})
	rep.Current = ""
	return rep, errors.Join(err, in.finish(ctx, &rep))
}

// keep stores an export's file as it is, with its path as its name, and writes what gcexport
// read from it (r, or readErr).
func (in *Ingester) keep(ctx context.Context, user store.User, name string, b []byte, r *gcexport.Result, readErr error, rep *Report) error {
	sum, err := in.Blobs.PutBytes(b)
	if err != nil {
		return err
	}
	rf, isNew, err := in.Store.AddRawFile(ctx, store.RawFile{SHA256: sum, Size: int64(len(b)), UserID: user.ID,
		Source: store.SourceGCExport, SourceName: name})
	if err != nil {
		return err
	}
	if isNew {
		rep.Kept++
		// Read at once: its daily values, nights, weigh-ins, … (a file it cannot read stays
		// stored as it is, and a later version may read it).
		if err := in.writeExport(ctx, rf, r, readErr, rep.touched()); err != nil {
			rep.addError(name + ": " + err.Error())
		}
	}
	if rep.KeptBy == nil {
		rep.KeptBy = map[string]int{}
	}
	rep.KeptBy[ExportFolder(name)]++
	return nil
}

// IsExportPath reports whether a path lies inside a Garmin Connect account export, whose
// files sit under DI_CONNECT/DI-Connect-….
func IsExportPath(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if part == "DI_CONNECT" || strings.HasPrefix(part, "DI-Connect-") {
			return true
		}
	}
	return false
}

// ExportFolder is the export folder a path belongs to ("DI-Connect-Fitness", or a top-level
// folder of the export such as "INREACH"), for reports.
func ExportFolder(p string) string {
	parts := strings.Split(p, "/")
	top := ""
	for i, part := range parts {
		if strings.HasPrefix(part, "DI-Connect-") {
			return part
		}
		if part == "DI_CONNECT" && i+2 < len(parts) {
			return parts[i+1]
		}
		if strings.EqualFold(path.Ext(part), ".zip") && top == "" && i+1 < len(parts) {
			top = parts[i+1] // the first folder inside the export's archive
			if i+2 == len(parts) {
				top = "Top level" // a file at the archive's root
			}
		}
	}
	if top != "" && top != "DI_CONNECT" {
		return top
	}
	return "DI_CONNECT"
}

// isExportArchive reports whether the zip archive at disk is a Garmin Connect export: one of
// its entries lies under DI_CONNECT/. Only the central directory is read.
func isExportArchive(disk string) bool {
	r, err := zip.OpenReader(disk)
	if err != nil {
		return false
	}
	defer r.Close()
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "DI_CONNECT/") || strings.Contains(f.Name, "/DI_CONNECT/") {
			return true
		}
	}
	return false
}

// Paths is a Source over files and folders on disk. A folder's files are named from the
// folder itself ("GARMIN/Activity/x.fit"); a zip archive is walked with zw (nested archives
// too), its files named from the archive ("export.zip/DI_CONNECT/…").
func Paths(paths []string, zw *zipwalk.Walker) Source {
	if zw == nil {
		zw = &zipwalk.Walker{}
	}
	return func(ctx context.Context, fn func(Entry) error) error {
		for _, root := range paths {
			root = filepath.Clean(root)
			st, err := os.Stat(root)
			if err != nil {
				return err
			}
			base := filepath.Dir(root)
			if !st.IsDir() {
				if err := diskEntry(ctx, root, filepath.Base(root), zw, fn); err != nil {
					return err
				}
				continue
			}
			err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if d.IsDir() || !d.Type().IsRegular() {
					return nil
				}
				rel, err := filepath.Rel(base, p)
				if err != nil {
					return err
				}
				return diskEntry(ctx, p, filepath.ToSlash(rel), zw, fn)
			})
			if err != nil {
				return err
			}
		}
		return nil
	}
}

// Staged is a Source over files saved under other names (an upload's staging folder): each
// file at its disk path, named by its path in the upload.
func Staged(files []StagedFile, zw *zipwalk.Walker) Source {
	if zw == nil {
		zw = &zipwalk.Walker{}
	}
	return func(ctx context.Context, fn func(Entry) error) error {
		for _, f := range files {
			if err := diskEntry(ctx, f.Disk, f.Name, zw, fn); err != nil {
				return err
			}
		}
		return nil
	}
}

// StagedFile is one uploaded file: where it was saved, and its path in the upload.
type StagedFile struct {
	Disk string `json:"disk"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func diskEntry(ctx context.Context, disk, name string, zw *zipwalk.Walker, fn func(Entry) error) error {
	if strings.EqualFold(path.Ext(name), ".zip") {
		var cbErr error
		zw.Skipped = func(p string, err error) {
			if cbErr == nil {
				cbErr = fn(Entry{Path: p, Size: -1, Err: err})
			}
		}
		defer func() { zw.Skipped = nil }()
		export := isExportArchive(disk)
		err := zw.WalkFile(ctx, disk, name, func(f zipwalk.File) error {
			if cbErr != nil {
				return cbErr
			}
			return fn(Entry{Path: f.Path, Size: f.Size, Modified: f.Modified, Read: f.Read, InExport: export})
		})
		if cbErr != nil {
			return cbErr
		}
		if err != nil && ctx.Err() == nil && isArchiveLimit(err) {
			// The archive is no zip or beyond the limits: report it and go on with the rest.
			return fn(Entry{Path: name, Size: -1, Err: err})
		}
		return err
	}
	st, err := os.Stat(disk)
	if err != nil {
		return fn(Entry{Path: name, Size: -1, Err: err})
	}
	return fn(Entry{Path: name, Size: st.Size(), Modified: st.ModTime(), Read: func() ([]byte, error) { return readFile(disk) }})
}

func isArchiveLimit(err error) bool {
	return errors.Is(err, zipwalk.ErrNotZip) || errors.Is(err, zipwalk.ErrTooLarge) || errors.Is(err, zipwalk.ErrTooManyEntries)
}
