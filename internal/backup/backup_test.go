// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/blobstore"
)

func TestWrite(t *testing.T) {
	ctx := context.Background()
	data := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(data, "goarmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.EnsureUser(ctx, "someone@example.invalid", "Someone"); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(filepath.Join(data, "raw"))
	if err != nil {
		t.Fatal(err)
	}
	sum, _, err := blobs.Put(strings.NewReader("a raw file"), "")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(data, "raw", "tmp", "put-123"), "unfinished")
	write(t, filepath.Join(data, "watches", "1", "files", "a.fit"), "spooled")
	write(t, filepath.Join(data, "watches", "1", "files", "b.fit.part"), "unfinished")
	write(t, filepath.Join(data, "node.json"), "{}")
	write(t, filepath.Join(data, "tiles", "x.png"), "cache, not backed up")

	out := filepath.Join(t.TempDir(), "backups")
	now := time.Now()
	res, err := Write(ctx, data, out, now)
	if err != nil {
		t.Fatal(err)
	}
	name := Prefix + now.Format(stamp)
	if res.Path != filepath.Join(out, name+Suffix) || res.Accounts != 1 || res.RawFiles != 1 || res.Size == 0 {
		t.Fatalf("result %+v", res)
	}
	if st, err := os.Stat(res.Path); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("archive mode %v, %v", st.Mode(), err)
	}
	files := list(t, res.Path)
	want := []string{
		name + "/goarmin.db",
		name + "/node.json",
		name + "/raw/" + sum[:2] + "/" + sum[2:4] + "/" + sum,
		name + "/watches/1/files/a.fit",
	}
	got := make([]string, 0, len(files))
	for n := range files {
		got = append(got, n)
	}
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("archive files:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// The database in the archive opens and holds the account.
	db := filepath.Join(t.TempDir(), "goarmin.db")
	if err := os.WriteFile(db, files[name+"/goarmin.db"], 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := accounts(ctx, db); err != nil || n != 1 {
		t.Fatalf("accounts in the archived database: %d, %v", n, err)
	}
	// Nothing is left behind next to the archive.
	entries, _ := os.ReadDir(out)
	if len(entries) != 1 {
		t.Fatalf("%d entries in the backup directory, want only the archive", len(entries))
	}
	if _, err := Write(ctx, data, out, now); err == nil {
		t.Fatal("a second archive with the same name overwrote the first")
	}
	if _, err := Write(ctx, t.TempDir(), out, now.Add(time.Second)); err == nil {
		t.Fatal("an empty data directory was backed up")
	}
}

func TestPrune(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	var names []string
	for i := 0; i < 6; i++ {
		at := now.AddDate(0, 0, -10*i) // today, 10, 20, … 50 days ago
		n := filepath.Join(dir, Prefix+at.Format(stamp)+Suffix)
		write(t, n, "x")
		if err := os.Chtimes(n, at, at); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	write(t, filepath.Join(dir, "unrelated.tar.gz"), "kept")
	if removed, err := Prune(dir, 0, 3, now); err != nil || len(removed) != 0 {
		t.Fatalf("keep-days 0 removed %v, %v", removed, err)
	}
	removed, err := Prune(dir, 15, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	// The newest three stay whatever their age; of the rest, those older than 15 days go.
	if len(removed) != 3 || removed[0] != names[3] || removed[1] != names[4] || removed[2] != names[5] {
		t.Fatalf("removed %v", removed)
	}
	removed, err = Prune(dir, 1, 1, now)
	if err != nil || len(removed) != 2 {
		t.Fatalf("second prune removed %v, %v", removed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "unrelated.tar.gz")); err != nil {
		t.Fatal("Prune removed a file that is no backup")
	}
}

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

// list returns the regular files of a tar.gz with their contents.
func list(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			b, _ := io.ReadAll(tr)
			out[h.Name] = b
		}
	}
}
