// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package blobstore keeps immutable files under their SHA-256, in a directory:
//
//	<dir>/ab/cd/abcd…  (the full lowercase hex digest)
//
// A blob is written to a temporary file in the same directory, synced, checked and renamed into
// place, so it is either complete or absent. Storing the same bytes twice is a no-op.
package blobstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Store is a blob directory. It is safe for concurrent use.
type Store struct {
	dir string
}

// ErrNotFound is returned for a digest the store does not hold.
var ErrNotFound = errors.New("blobstore: not found")

// Open opens or creates the store in dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Valid reports whether sum is a lowercase hex SHA-256 digest.
func Valid(sum string) bool {
	if len(sum) != 64 {
		return false
	}
	for _, c := range sum {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Path returns where the blob sum lives (whether or not it exists).
func (s *Store) Path(sum string) (string, error) {
	if !Valid(sum) {
		return "", fmt.Errorf("blobstore: bad digest %q", sum)
	}
	return filepath.Join(s.dir, sum[:2], sum[2:4], sum), nil
}

// Has reports whether the store holds sum.
func (s *Store) Has(sum string) bool {
	p, err := s.Path(sum)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Put stores everything r yields and returns its digest and size. If want is not empty, the
// content must hash to it or nothing is stored.
func (s *Store) Put(r io.Reader, want string) (sum string, size int64, err error) {
	tmpDir := filepath.Join(s.dir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return "", 0, err
	}
	f, err := os.CreateTemp(tmpDir, "put-*")
	if err != nil {
		return "", 0, err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // a no-op after the rename
	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(f, h), r)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", 0, err
	}
	sum = hex.EncodeToString(h.Sum(nil))
	if want != "" && want != sum {
		return "", 0, fmt.Errorf("blobstore: content hashes to %s, not %s", sum, want)
	}
	p, _ := s.Path(sum)
	if _, err := os.Stat(p); err == nil {
		return sum, size, nil // already there
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmp, p); err != nil {
		return "", 0, err
	}
	if d, err := os.Open(filepath.Dir(p)); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return sum, size, nil
}

// PutBytes stores b.
func (s *Store) PutBytes(b []byte) (string, error) {
	sum, _, err := s.Put(bytes.NewReader(b), "")
	return sum, err
}

// ReadFile returns the whole blob, checking it against its digest.
func (s *Store) ReadFile(sum string) ([]byte, error) {
	p, err := s.Path(sum)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(b)
	if hex.EncodeToString(got[:]) != sum {
		return nil, fmt.Errorf("blobstore: %s is corrupt on disk", sum)
	}
	return b, nil
}

// Open opens the blob for reading (without checking it).
func (s *Store) Open(sum string) (*os.File, error) {
	p, err := s.Path(sum)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

// Remove deletes the blob with this digest; one that is not there is no error.
func (s *Store) Remove(sum string) error {
	p, err := s.Path(sum)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
