// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package sqlitex

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenMigrateReadOnly(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "x.db")
	db, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migs := []string{
		`CREATE TABLE a (id INTEGER PRIMARY KEY, v TEXT NOT NULL)`,
		`ALTER TABLE a ADD COLUMN w INTEGER NOT NULL DEFAULT 0`,
	}
	if err := db.Migrate(ctx, migs[:1]); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, migs); err != nil {
		t.Fatal(err)
	}
	if v, err := db.Version(ctx); err != nil || v != 2 {
		t.Fatalf("version %d, %v", v, err)
	}
	if err := db.Migrate(ctx, migs[:1]); err == nil {
		t.Fatal("an older program migrated a newer database")
	}
	if _, err := db.W.ExecContext(ctx, `INSERT INTO a (v, w) VALUES ('x', 1)`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM a`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("reader sees %d rows, %v", n, err)
	}
	if _, err := db.R.ExecContext(ctx, `INSERT INTO a (v) VALUES ('y')`); err == nil {
		t.Fatal("the read pool wrote")
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("database file mode %v, %v", fi.Mode(), err)
	}
	var mode string
	if err := db.W.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal mode %q, %v", mode, err)
	}
}

func TestTxRollsBack(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "x.db"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx, []string{`CREATE TABLE a (v INTEGER)`}); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	err = db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO a VALUES (1)`); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Tx error %v", err)
	}
	var n int
	if err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM a`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d rows after rollback, %v", n, err)
	}
	if _, err := Open("bad?path", Options{}); err == nil {
		t.Fatal("a path with ? was accepted")
	}
}

// TestSnapshotWhileWriting: a snapshot taken while another connection keeps committing is one
// transaction's view (rows 1..n, no gaps), private, sound, and never overwrites a file.
func TestSnapshotWhileWriting(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "x.db")
	db, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx, []string{`CREATE TABLE a (id INTEGER PRIMARY KEY, v TEXT NOT NULL)`}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if _, err := db.W.ExecContext(ctx, `INSERT INTO a (v) VALUES ('before')`); err != nil {
			t.Fatal(err)
		}
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		for {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			if _, err := db.W.ExecContext(ctx, `INSERT INTO a (v) VALUES ('during')`); err != nil {
				done <- err
				return
			}
		}
	}()
	dst := filepath.Join(dir, "snap.db")
	err = Snapshot(ctx, path, dst)
	close(stop)
	if werr := <-done; werr != nil {
		t.Fatal(werr)
	}
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dst)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode %v, %v", st.Mode(), err)
	}
	snap, err := sql.Open("sqlite", "file:"+dst+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	var n, max int
	if err := snap.QueryRow(`SELECT count(*), max(id) FROM a`).Scan(&n, &max); err != nil {
		t.Fatal(err)
	}
	if n < 100 || n != max {
		t.Fatalf("snapshot holds %d rows up to id %d, want a gapless prefix of at least 100", n, max)
	}
	if err := Snapshot(ctx, path, dst); err == nil {
		t.Fatal("Snapshot overwrote an existing file")
	}
	if err := Check(ctx, dst); err != nil {
		t.Fatal(err)
	}
}
