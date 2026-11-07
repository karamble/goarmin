// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package blobstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPutReadDedupe(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("fit data "), 1000)
	want := sha256.Sum256(data)
	sum, size, err := s.Put(bytes.NewReader(data), "")
	if err != nil || sum != hex.EncodeToString(want[:]) || size != int64(len(data)) {
		t.Fatalf("put: %s %d %v", sum, size, err)
	}
	if again, err := s.PutBytes(data); err != nil || again != sum {
		t.Fatalf("second put: %s %v", again, err)
	}
	p, _ := s.Path(sum)
	if !strings.HasSuffix(filepath.ToSlash(p), sum[:2]+"/"+sum[2:4]+"/"+sum) || !s.Has(sum) {
		t.Fatalf("layout %s", p)
	}
	got, err := s.ReadFile(sum)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("read back: %v", err)
	}
	tmp, _ := os.ReadDir(filepath.Join(s.dir, "tmp"))
	if len(tmp) != 0 {
		t.Fatalf("%d temporary files left", len(tmp))
	}
}

func TestPutChecksWant(t *testing.T) {
	s, _ := Open(t.TempDir())
	other := strings.Repeat("0", 64)
	if _, _, err := s.Put(strings.NewReader("abc"), other); err == nil {
		t.Fatal("wrong digest accepted")
	}
	if s.Has(other) {
		t.Fatal("stored under the wrong digest")
	}
}

func TestCorruptAndMissing(t *testing.T) {
	s, _ := Open(t.TempDir())
	sum, err := s.PutBytes([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.Path(sum)
	if err := os.WriteFile(p, []byte("jello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadFile(sum); err == nil {
		t.Fatal("corruption not detected")
	}
	if _, err := s.ReadFile(strings.Repeat("a", 64)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing blob: %v", err)
	}
	if _, err := s.Path("../../etc/passwd"); err == nil || Valid("ABC") {
		t.Fatal("bad digest accepted")
	}
}
