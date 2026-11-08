// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package backup writes goarmin's backup archives while the server runs: a consistent snapshot
// of the database plus the raw files, the watch spool and node.json, as one tar.gz.
//
// The layout is the one scripts/backup.sh restores (make restore):
//
//	goarmin_backup_YYYYMMDD_HHMMSS/goarmin.db
//	goarmin_backup_YYYYMMDD_HHMMSS/raw/…
//	goarmin_backup_YYYYMMDD_HHMMSS/watches/…
//	goarmin_backup_YYYYMMDD_HHMMSS/node.json
//
// The snapshot is taken first. Raw files are content-addressed, written before the database
// refers to them and never changed, so every file the snapshot names is already on disk when the
// raw store is read; files added meanwhile are harmless extras.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/karamble/goarmin/pkg/sqlitex"
)

// Prefix and Suffix frame every archive name.
const (
	Prefix = "goarmin_backup_"
	Suffix = ".tar.gz"
	stamp  = "20060102_150405"
)

// Result describes a written archive.
type Result struct {
	Path     string
	Size     int64
	Accounts int
	RawFiles int
}

// Write archives the data directory into a new archive in outDir, named after now (local time,
// as make backup names them). Nothing is left behind when it fails.
func Write(ctx context.Context, dataDir, outDir string, now time.Time) (Result, error) {
	db := filepath.Join(dataDir, "goarmin.db")
	if st, err := os.Stat(db); err != nil || st.Size() == 0 {
		return Result{}, fmt.Errorf("backup: no database at %s", db)
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return Result{}, err
	}
	name := Prefix + now.Format(stamp)
	out := filepath.Join(outDir, name+Suffix)
	if _, err := os.Stat(out); err == nil {
		return Result{}, fmt.Errorf("backup: %s exists", out)
	}
	stage, err := os.MkdirTemp(outDir, ".stage.")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(stage)
	snap := filepath.Join(stage, "goarmin.db")
	if err := sqlitex.Snapshot(ctx, db, snap); err != nil {
		return Result{}, err
	}
	res := Result{Path: out}
	if res.Accounts, err = accounts(ctx, snap); err != nil {
		return Result{}, err
	}

	part := filepath.Join(outDir, "."+name+Suffix+".part")
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(part) // a no-op after the rename
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	err = addFile(tw, snap, name+"/goarmin.db")
	for _, m := range []string{"raw", "watches", "node.json"} {
		if err != nil {
			break
		}
		var n int
		n, err = addTree(ctx, tw, dataDir, m, name)
		if m == "raw" {
			res.RawFiles = n
		}
	}
	if err == nil {
		err = tw.Close()
	}
	if err == nil {
		err = gz.Close()
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = verify(part, name+"/goarmin.db")
	}
	if err != nil {
		return Result{}, fmt.Errorf("backup: %w", err)
	}
	if err := os.Rename(part, out); err != nil {
		return Result{}, err
	}
	if st, err := os.Stat(out); err == nil {
		res.Size = st.Size()
	}
	return res, nil
}

func accounts(ctx context.Context, path string) (int, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("backup: the snapshot is no goarmin database: %w", err)
	}
	return n, nil
}

// addTree adds dataDir/rel (a file or a directory, if it exists) under prefix and returns the
// number of files. Unfinished files (*.tmp, *.part, the raw store's tmp/) are left out.
func addTree(ctx context.Context, tw *tar.Writer, dataDir, rel, prefix string) (int, error) {
	root := filepath.Join(dataDir, rel)
	if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	n := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil // removed while walking (a spool file that reached the raw store)
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		r, _ := filepath.Rel(dataDir, p)
		base := d.Name()
		if d.IsDir() && r == filepath.Join("raw", "tmp") {
			return filepath.SkipDir
		}
		if strings.HasSuffix(base, ".tmp") || strings.HasSuffix(base, ".part") || strings.HasPrefix(base, ".tmp-") {
			return nil
		}
		name := prefix + "/" + filepath.ToSlash(r)
		switch {
		case d.IsDir():
			info, err := d.Info()
			if err != nil {
				return err
			}
			hdr, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			hdr.Name = name + "/"
			clean(hdr)
			return tw.WriteHeader(hdr)
		case d.Type().IsRegular():
			if err := addFile(tw, p, name); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

func addFile(tw *tar.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = name
	clean(hdr)
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.CopyN(tw, f, hdr.Size)
	return err
}

// clean drops the owner's names and ids: a restore on another machine (or in a container) takes
// the files as the user who restores.
func clean(h *tar.Header) {
	h.Uid, h.Gid, h.Uname, h.Gname = 0, 0, "", ""
	h.Format = tar.FormatPAX
}

// verify reads the whole archive back and checks that it holds the database.
func verify(path, db string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("the archive does not read back: %w", err)
		}
		if h.Name == db {
			found = true
		}
		if _, err := io.Copy(io.Discard, tr); err != nil {
			return fmt.Errorf("the archive does not read back: %w", err)
		}
	}
	if !found {
		return errors.New("the archive lacks the database")
	}
	return nil
}

// Prune removes archives in dir older than keepDays, always keeping the newest keepMin, and
// returns what it removed. keepDays 0 removes nothing.
func Prune(dir string, keepDays, keepMin int, now time.Time) ([]string, error) {
	if keepDays <= 0 {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type arch struct {
		path string
		mod  time.Time
	}
	var all []arch
	for _, e := range entries {
		n := e.Name()
		if e.Type().IsRegular() && strings.HasPrefix(n, Prefix) && strings.HasSuffix(n, Suffix) {
			info, err := e.Info()
			if err != nil {
				continue
			}
			all = append(all, arch{filepath.Join(dir, n), info.ModTime()})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].mod.After(all[j].mod) })
	cut := now.Add(-time.Duration(keepDays) * 24 * time.Hour)
	var removed []string
	for i, a := range all {
		if i < keepMin || !a.mod.Before(cut) {
			continue
		}
		if err := os.Remove(a.path); err != nil {
			return removed, err
		}
		removed = append(removed, a.path)
	}
	return removed, nil
}
