// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// Import kinds and statuses.
const (
	ImportUpload = "upload" // files sent from the browser
	ImportUSB    = "usb"    // a watch plugged in by USB

	ImportUploading = "uploading" // the browser is still sending the files
	ImportQueued    = "queued"
	ImportRunning   = "running"
	ImportDone      = "done"
	ImportFailed    = "failed"
	ImportCanceled  = "canceled"
)

// Import is one import run: what was imported, how far it got and what it found.
type Import struct {
	ID       int64
	UserID   int64
	Kind     string
	Source   string // the raw files' source: SourceUpload, SourceUSB, SourceGCExport
	Name     string
	DeviceID int64 // a USB import's watch, 0 if none
	// StartedBy is "manual" or "plugged" (a watch plugged in started it).
	StartedBy  string
	Status     string
	CreatedAt  int64 // Unix seconds; StartedAt and FinishedAt are 0 until then
	StartedAt  int64
	FinishedAt int64
	Report     json.RawMessage
	Error      string
}

// Finished reports whether the import has ended.
func (i Import) Finished() bool {
	return i.Status == ImportDone || i.Status == ImportFailed || i.Status == ImportCanceled
}

const importCols = `id, user_id, kind, source, name, COALESCE(device_id, 0), started_by, status, created_at,
	COALESCE(started_at, 0), COALESCE(finished_at, 0), report, error`

func scanImport(row interface{ Scan(...any) error }) (Import, error) {
	var i Import
	var report string
	err := row.Scan(&i.ID, &i.UserID, &i.Kind, &i.Source, &i.Name, &i.DeviceID, &i.StartedBy, &i.Status, &i.CreatedAt,
		&i.StartedAt, &i.FinishedAt, &report, &i.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return Import{}, ErrNotFound
	}
	i.Report = json.RawMessage(report)
	return i, err
}

// CreateImport records a new import (its ID, CreatedAt and Report are filled in).
func (s *Store) CreateImport(ctx context.Context, i Import) (Import, error) {
	if i.StartedBy == "" {
		i.StartedBy = "manual"
	}
	i.CreatedAt = s.now().Unix()
	i.Report = json.RawMessage(`{}`)
	var dev any
	if i.DeviceID != 0 {
		dev = i.DeviceID
	}
	res, err := s.db.W.ExecContext(ctx, `INSERT INTO imports (user_id, kind, source, name, device_id, started_by, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, i.UserID, i.Kind, i.Source, i.Name, dev, i.StartedBy, i.Status, i.CreatedAt)
	if err != nil {
		return Import{}, err
	}
	i.ID, err = res.LastInsertId()
	return i, err
}

// SetImportStatus moves an unfinished import on (queued, running: running sets its start).
func (s *Store) SetImportStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE imports SET status = ?,
		started_at = CASE WHEN ? = 'running' THEN ? ELSE started_at END
		WHERE id = ? AND status NOT IN ('done', 'failed', 'canceled')`, status, status, s.now().Unix(), id)
	return err
}

// SetImportSource records what an import turned out to be (an upload that holds a Garmin
// Connect export becomes gc_export) and its name.
func (s *Store) SetImportSource(ctx context.Context, id int64, source, name string) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE imports SET source = ?, name = ? WHERE id = ?`, source, name, id)
	return err
}

// SaveImportReport stores a running import's report so far.
func (s *Store) SaveImportReport(ctx context.Context, id int64, report []byte) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE imports SET report = ? WHERE id = ?`, string(report), id)
	return err
}

// FinishImport ends an import with its status (done, failed, canceled), report and error.
func (s *Store) FinishImport(ctx context.Context, id int64, status string, report []byte, errText string) error {
	if report == nil {
		report = []byte(`{}`)
	}
	_, err := s.db.W.ExecContext(ctx, `UPDATE imports SET status = ?, report = ?, error = ?, finished_at = ?,
		started_at = COALESCE(started_at, ?) WHERE id = ?`, status, string(report), errText, s.now().Unix(), s.now().Unix(), id)
	return err
}

// Import returns one of user's imports (ErrNotFound for another account's).
func (s *Store) Import(ctx context.Context, user, id int64) (Import, error) {
	return scanImport(s.db.W.QueryRowContext(ctx, `SELECT `+importCols+` FROM imports WHERE id = ? AND user_id = ?`, id, user))
}

// ImportByID returns any import (for the import runner).
func (s *Store) ImportByID(ctx context.Context, id int64) (Import, error) {
	return scanImport(s.db.W.QueryRowContext(ctx, `SELECT `+importCols+` FROM imports WHERE id = ?`, id))
}

// Imports lists user's imports, newest first, before the id before (0: from the newest).
func (s *Store) Imports(ctx context.Context, user, before int64, limit int) ([]Import, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if before <= 0 {
		before = 1<<62 - 1
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT `+importCols+` FROM imports WHERE user_id = ? AND id < ?
		ORDER BY id DESC LIMIT ?`, user, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Import
	for rows.Next() {
		i, err := scanImport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// LastDeviceImport is the newest import of a watch for user (ErrNotFound if none).
func (s *Store) LastDeviceImport(ctx context.Context, user, device int64) (Import, error) {
	return scanImport(s.db.R.QueryRowContext(ctx, `SELECT `+importCols+` FROM imports WHERE user_id = ? AND device_id = ?
		ORDER BY id DESC LIMIT 1`, user, device))
}

// UnfinishedImports lists the imports that have not ended, oldest first (after a restart).
func (s *Store) UnfinishedImports(ctx context.Context) ([]Import, error) {
	rows, err := s.db.W.QueryContext(ctx, `SELECT `+importCols+` FROM imports
		WHERE status NOT IN ('done', 'failed', 'canceled') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Import
	for rows.Next() {
		i, err := scanImport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// SetImportDevice records the watch a USB import turned out to read.
func (s *Store) SetImportDevice(ctx context.Context, id, device int64) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE imports SET device_id = ? WHERE id = ?`, device, id)
	return err
}

// FailUnfinishedImports ends every import that has not finished with reason, as after a
// restart, and returns their ids.
func (s *Store) FailUnfinishedImports(ctx context.Context, reason string) ([]int64, error) {
	rows, err := s.db.W.QueryContext(ctx, `UPDATE imports SET status = 'failed', error = ?, finished_at = ?
		WHERE status NOT IN ('done', 'failed', 'canceled') RETURNING id`, reason, s.now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// USBSeen is what a USB import last read at path on the watch with this serial.
type USBSeen struct {
	Size     int64
	Modified int64
	SHA256   string
}

// USBSeenFile returns what was read from path on the watch (ErrNotFound if nothing).
func (s *Store) USBSeenFile(ctx context.Context, serial, path string) (USBSeen, error) {
	var u USBSeen
	err := s.db.R.QueryRowContext(ctx, `SELECT size, modified, sha256 FROM usb_seen WHERE serial = ? AND path = ?`, serial, path).
		Scan(&u.Size, &u.Modified, &u.SHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return USBSeen{}, ErrNotFound
	}
	return u, err
}

// SetUSBSeen records a file a USB import read.
func (s *Store) SetUSBSeen(ctx context.Context, serial, path string, u USBSeen) error {
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO usb_seen (serial, path, size, modified, sha256) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (serial, path) DO UPDATE SET size = excluded.size, modified = excluded.modified, sha256 = excluded.sha256`,
		serial, path, u.Size, u.Modified, u.SHA256)
	return err
}

// SetUSBAuto turns importing a watch when it is plugged in on or off.
func (s *Store) SetUSBAuto(ctx context.Context, device int64, on bool) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE devices SET usb_auto = ? WHERE id = ?`, on, device)
	return err
}

// SetUSBSerial records the serial a watch reports on USB. Another watch that had it loses it.
func (s *Store) SetUSBSerial(ctx context.Context, device int64, serial string) error {
	tx, err := s.db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET usb_serial = '' WHERE usb_serial = ? AND id != ?`, serial, device); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET usb_serial = ? WHERE id = ?`, serial, device); err != nil {
		return err
	}
	return tx.Commit()
}

// DeviceByUSBSerial finds the watch that reports serial on USB.
func (s *Store) DeviceByUSBSerial(ctx context.Context, serial string) (Device, error) {
	if serial == "" {
		return Device{}, ErrNotFound
	}
	d, err := scanDevice(s.db.R.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices d WHERE d.usb_serial = ?`, serial))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}
