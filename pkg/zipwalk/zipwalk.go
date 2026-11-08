// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package zipwalk walks the files in a zip archive, and in the zip archives inside it, within
// limits that keep a hostile archive from exhausting memory or disk: too many entries, too many
// bytes, compression ratios no real file has (zip bombs), nesting without end. Nothing is
// extracted by name. A file is read into memory when the caller asks for it; a nested archive
// is read into memory, or into an anonymous temporary file when it is large.
package zipwalk

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
)

// Limits bound a walk. Zero values take the defaults.
type Limits struct {
	MaxEntries int   // entries in all archives together (200,000)
	MaxTotal   int64 // uncompressed bytes in all (20 GiB)
	MaxFile    int64 // bytes of one file handed to the caller (64 MiB)
	MaxArchive int64 // bytes of one nested archive (8 GiB)
	MemArchive int64 // nested archives up to this size are read into memory (64 MiB)
	MaxRatio   int64 // uncompressed per compressed byte, for entries over 1 MiB (200)
	MaxDepth   int   // archives inside archives (3)
	TempDir    string
}

func (l Limits) withDefaults() Limits {
	def := func(v *int64, d int64) {
		if *v <= 0 {
			*v = d
		}
	}
	if l.MaxEntries <= 0 {
		l.MaxEntries = 200_000
	}
	def(&l.MaxTotal, 20<<30)
	def(&l.MaxFile, 64<<20)
	def(&l.MaxArchive, 8<<30)
	def(&l.MemArchive, 64<<20)
	def(&l.MaxRatio, 200)
	if l.MaxDepth <= 0 {
		l.MaxDepth = 3
	}
	return l
}

var (
	// ErrTooManyEntries and ErrTooLarge end a walk: the archive is beyond the limits.
	ErrTooManyEntries = errors.New("zipwalk: too many entries")
	ErrTooLarge       = errors.New("zipwalk: too much data")
	// The rest leave one entry out (Walker.Skipped hears of it).
	ErrFileTooLarge = errors.New("zipwalk: file too large")
	ErrRatio        = errors.New("zipwalk: compressed too well to be a real file")
	ErrDepth        = errors.New("zipwalk: archives nested too deep")
	ErrNotZip       = errors.New("zipwalk: not a zip archive")
)

// File is one file found in a walk.
type File struct {
	// Path is the file's path from the top archive, cleaned and slash-separated, with each
	// nested archive's name as a folder: "DI_CONNECT/Part1.zip/activity.fit".
	Path     string
	Size     int64
	Modified time.Time
	f        *zip.File
	max      int64
}

// Read reads the file. It works only during the call that handed the file over.
func (f File) Read() ([]byte, error) {
	if int64(f.f.UncompressedSize64) > f.max {
		return nil, fmt.Errorf("%w (%d bytes)", ErrFileTooLarge, f.f.UncompressedSize64)
	}
	rc, err := f.f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, int64(f.f.UncompressedSize64)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(b)) != f.f.UncompressedSize64 {
		return nil, fmt.Errorf("zipwalk: %s holds %d bytes, its header says %d", f.Path, len(b), f.f.UncompressedSize64)
	}
	return b, nil
}

// Walker walks archives within its Limits. Use one Walker per import: the entry and byte
// counts add up across Walk calls.
type Walker struct {
	Limits
	// Skipped hears of each entry left out, and why.
	Skipped func(path string, err error)
	// Archives counts the archives opened, the top ones included.
	Archives int

	entries int
	total   int64
}

// WalkFile walks the archive at name.
func (w *Walker) WalkFile(ctx context.Context, name, prefix string, fn func(File) error) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	return w.Walk(ctx, f, st.Size(), prefix, fn)
}

// Walk walks the archive in r. prefix is put before every path ("" for none). fn's error
// ends the walk and is returned.
func (w *Walker) Walk(ctx context.Context, r io.ReaderAt, size int64, prefix string, fn func(File) error) error {
	w.Limits = w.Limits.withDefaults()
	return unwrapCaller(w.walk(ctx, r, size, prefix, 0, fn))
}

func (w *Walker) skip(p string, err error) {
	if w.Skipped != nil {
		w.Skipped(p, err)
	}
}

func (w *Walker) walk(ctx context.Context, r io.ReaderAt, size int64, prefix string, depth int, fn func(File) error) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotZip, err)
	}
	w.Archives++
	for _, zf := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name, ok := clean(zf.Name)
		if !ok {
			continue // a folder, or a name that is no file
		}
		p := path.Join(prefix, name)
		w.entries++
		if w.entries > w.MaxEntries {
			return fmt.Errorf("%w (more than %d)", ErrTooManyEntries, w.MaxEntries)
		}
		usize, csize := int64(zf.UncompressedSize64), int64(zf.CompressedSize64)
		if usize < 0 || csize < 0 {
			w.skip(p, ErrFileTooLarge)
			continue
		}
		if usize > 1<<20 && (csize == 0 || usize/max(csize, 1) > w.MaxRatio) {
			w.skip(p, ErrRatio)
			continue
		}
		w.total += usize
		if w.total > w.MaxTotal {
			return fmt.Errorf("%w (more than %d bytes uncompressed)", ErrTooLarge, w.MaxTotal)
		}
		if strings.EqualFold(path.Ext(name), ".zip") {
			if depth+1 > w.MaxDepth {
				w.skip(p, ErrDepth)
				continue
			}
			if err := w.nested(ctx, zf, p, depth+1, fn); err != nil {
				if errors.Is(err, ErrTooManyEntries) || errors.Is(err, ErrTooLarge) || ctx.Err() != nil || errors.Is(err, errCaller) {
					return err
				}
				w.skip(p, err)
			}
			continue
		}
		if err := fn(File{Path: p, Size: usize, Modified: zf.Modified, f: zf, max: w.MaxFile}); err != nil {
			return wrapCaller(err)
		}
	}
	return nil
}

// errCaller marks an error fn returned inside a nested archive, so it ends the walk instead
// of being taken for a broken nested archive.
var errCaller = errors.New("zipwalk: the caller stopped")

type callerError struct{ err error }

func (e callerError) Error() string   { return e.err.Error() }
func (e callerError) Unwrap() []error { return []error{errCaller, e.err} }
func wrapCaller(err error) error      { return callerError{err} }
func unwrapCaller(err error) error {
	var c callerError
	if errors.As(err, &c) {
		return c.err
	}
	return err
}

func (w *Walker) nested(ctx context.Context, zf *zip.File, p string, depth int, fn func(File) error) error {
	usize := int64(zf.UncompressedSize64)
	if usize > w.MaxArchive {
		return fmt.Errorf("%w (%d bytes)", ErrFileTooLarge, usize)
	}
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	src := io.LimitReader(rc, usize+1)
	if usize <= w.MemArchive {
		b, err := io.ReadAll(src)
		if err != nil {
			return err
		}
		if int64(len(b)) != usize {
			return fmt.Errorf("zipwalk: %s holds %d bytes, its header says %d", p, len(b), usize)
		}
		return w.walk(ctx, bytes.NewReader(b), usize, p, depth, fn)
	}
	tmp, err := os.CreateTemp(w.TempDir, "zipwalk-*.zip")
	if err != nil {
		return err
	}
	// The file is removed at once; it lives on, unnamed, until it is closed.
	os.Remove(tmp.Name())
	defer tmp.Close()
	n, err := io.Copy(tmp, src)
	if err != nil {
		return err
	}
	if n != usize {
		return fmt.Errorf("zipwalk: %s holds %d bytes, its header says %d", p, n, usize)
	}
	return w.walk(ctx, tmp, usize, p, depth, fn)
}

// clean makes an entry name a relative slash path; folders and names that cannot be a file
// ("", "..", with a NUL) are not.
func clean(name string) (string, bool) {
	if strings.HasSuffix(name, "/") || strings.HasSuffix(name, "\\") || strings.ContainsRune(name, 0) {
		return "", false
	}
	name = strings.ReplaceAll(name, "\\", "/")
	c := strings.TrimPrefix(path.Clean("/"+name), "/")
	if c == "" || c == "." {
		return "", false
	}
	return c, true
}
