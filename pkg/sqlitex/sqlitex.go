// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package sqlitex opens a SQLite database (modernc.org/sqlite, no cgo) the way a single-process
// server wants it:
//   - one writer connection, so SQLite never reports "database is locked";
//   - a separate pool of read-only connections for queries;
//   - WAL, a busy timeout, foreign keys and synchronous=NORMAL;
//   - a large page cache for the writer (big imports insert millions of rows at scattered
//     places of large indexes), temporary tables in memory;
//   - append-only migrations recorded in schema_version.
package sqlitex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	_ "modernc.org/sqlite" // the "sqlite" driver
)

// DB holds the two handles. W is the only connection that writes; R never writes.
type DB struct {
	W *sql.DB
	R *sql.DB
}

// Options configures Open.
type Options struct {
	// Readers is the size of the read pool (8).
	Readers int
	// BusyTimeoutMS is how long a connection waits for a lock (10000).
	BusyTimeoutMS int
	// WriterCacheMB is the writer's page cache (128 MiB); readers keep SQLite's default.
	WriterCacheMB int
}

// Open opens or creates the database file at path.
func Open(path string, opt Options) (*DB, error) {
	if opt.Readers <= 0 {
		opt.Readers = 8
	}
	if opt.BusyTimeoutMS <= 0 {
		opt.BusyTimeoutMS = 10000
	}
	if opt.WriterCacheMB <= 0 {
		opt.WriterCacheMB = 128
	}
	if strings.ContainsAny(path, "?#") {
		return nil, errors.New("sqlitex: the path must not contain ? or #")
	}
	// SQLite creates files with the process umask; a database is private data. Its -wal and
	// -shm files inherit the main file's mode.
	if f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600); err != nil {
		return nil, err
	} else {
		f.Close()
	}
	q := url.Values{}
	for _, p := range []string{
		"journal_mode(WAL)", fmt.Sprintf("busy_timeout(%d)", opt.BusyTimeoutMS),
		"foreign_keys(1)", "synchronous(NORMAL)",
	} {
		q.Add("_pragma", p)
	}
	wq := url.Values{}
	for k, v := range q {
		wq[k] = append([]string(nil), v...)
	}
	wq.Add("_pragma", fmt.Sprintf("cache_size(%d)", -opt.WriterCacheMB*1024)) // negative: KiB
	wq.Add("_pragma", "temp_store(MEMORY)")
	w, err := sql.Open("sqlite", "file:"+path+"?"+wq.Encode())
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	// Opening the writer first creates the file and switches it to WAL before readers attach.
	if err := w.Ping(); err != nil {
		w.Close()
		return nil, fmt.Errorf("sqlitex: %s: %w", path, err)
	}
	q.Add("_pragma", "query_only(1)")
	r, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		w.Close()
		return nil, err
	}
	r.SetMaxOpenConns(opt.Readers)
	return &DB{W: w, R: r}, nil
}

// Snapshot writes a consistent copy of the database file src to dst with VACUUM INTO. It is
// safe while another process writes: the copy is one transaction's view, compacted, and src is
// opened read-only (no pragmas, no migrations, so a newer or older program can take it). dst
// must not exist; it is made private (0600) and checked with integrity_check.
func Snapshot(ctx context.Context, src, dst string) error {
	if strings.ContainsAny(src, "?#") {
		return errors.New("sqlitex: the path must not contain ? or #")
	}
	if _, err := os.Stat(src); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("sqlitex: %s exists", dst)
	}
	db, err := sql.Open("sqlite", "file:"+src+"?mode=ro&_pragma=busy_timeout(10000)")
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `VACUUM INTO ?`, dst); err != nil {
		os.Remove(dst)
		return fmt.Errorf("sqlitex: snapshot of %s: %w", src, err)
	}
	if err := os.Chmod(dst, 0o600); err != nil {
		return err
	}
	return Check(ctx, dst)
}

// Check runs integrity_check on the database file at path, read-only.
func Check(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var res string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil {
		return fmt.Errorf("sqlitex: %s: %w", path, err)
	}
	if res != "ok" {
		return fmt.Errorf("sqlitex: %s fails its integrity check: %s", path, res)
	}
	return nil
}

// Close closes both handles.
func (db *DB) Close() error {
	return errors.Join(db.R.Close(), db.W.Close())
}

// Migrate applies the migrations not applied yet, each in its own transaction. Migrations are
// append-only: entry i is schema version i+1, and a shipped entry is never edited.
func (db *DB) Migrate(ctx context.Context, migrations []string) error {
	if _, err := db.W.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (v INTEGER NOT NULL)`); err != nil {
		return err
	}
	v, err := db.Version(ctx)
	if err != nil {
		return err
	}
	if v > len(migrations) {
		return fmt.Errorf("sqlitex: the database is at schema version %d, newer than this program (%d)", v, len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		err := db.Tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
				return fmt.Errorf("sqlitex: migration %d: %w", i+1, err)
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_version (v) VALUES (?)`, i+1)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Version returns the schema version (0 for a new database).
func (db *DB) Version(ctx context.Context) (int, error) {
	var v int
	err := db.W.QueryRowContext(ctx, `SELECT COALESCE(MAX(v), 0) FROM schema_version`).Scan(&v)
	return v, err
}

// Tx runs fn in a write transaction, committing if it returns nil.
func (db *DB) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}
