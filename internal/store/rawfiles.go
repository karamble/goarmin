// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Sources of raw files, best first: when two files are the same recording, the better source
// is kept (ingested) and the other marked as its duplicate.
const (
	SourceBLE      = "ble"
	SourceUSB      = "usb"
	SourceUpload   = "upload"
	SourceGCExport = "gc_export"
)

func sourceRank(s string) int {
	switch s {
	case SourceBLE, SourceUSB:
		return 0
	case SourceUpload:
		return 1
	}
	return 2
}

// FITIdentity is a FIT file's file_id: what makes two copies the same recording.
type FITIdentity struct {
	Type         uint8
	Manufacturer uint16
	Product      uint16
	Serial       uint32
	TimeCreated  int64 // Unix seconds; 0 if absent
	Number       uint16
}

// RawFile is a received file.
type RawFile struct {
	ID          int64
	SHA256      string
	Size        int64
	UserID      int64
	DeviceID    int64 // 0 if none
	Source      string
	SourceName  string
	FIT         *FITIdentity // nil for non-FIT files
	DuplicateOf int64        // 0 if none
	ReceivedAt  time.Time
}

// identityTypes are the FIT file types whose file_id identifies one recording, so that a
// second copy with other bytes is a duplicate (an export, a re-encoded copy). Settings,
// sports and device files are rewritten in place under the same identity and are not listed.
var identityTypes = map[uint8]bool{4: true, 15: true, 28: true, 32: true, 41: true, 44: true, 49: true, 68: true, 72: true, 73: true, 74: true, 77: true}

// AddRawFile records f (already in the blob store) unless a file with the same digest exists.
// It returns the stored record and whether it is new. A new FIT recording identical in
// file_id to an existing one becomes the duplicate of the better-sourced copy.
func (s *Store) AddRawFile(ctx context.Context, f RawFile) (RawFile, bool, error) {
	if old, err := s.RawFileBySHA(ctx, f.SHA256); err == nil {
		return old, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return RawFile{}, false, err
	}
	f.ReceivedAt = s.now()
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var twin int64
		var twinSource string
		if f.FIT != nil && f.FIT.TimeCreated != 0 && identityTypes[f.FIT.Type] {
			// Only the same account's copies count: another account's file never loses its data
			// to this one.
			err := tx.QueryRowContext(ctx, `SELECT id, source FROM raw_files
				WHERE user_id = ? AND fit_serial = ? AND fit_type = ? AND fit_time_created = ? AND fit_number = ?
					AND duplicate_of IS NULL
				LIMIT 1`, f.UserID, f.FIT.Serial, f.FIT.Type, f.FIT.TimeCreated, f.FIT.Number).Scan(&twin, &twinSource)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if twin != 0 && sourceRank(f.Source) >= sourceRank(twinSource) {
				f.DuplicateOf = twin
			}
		}
		var fit [6]any
		if f.FIT != nil {
			fit = [6]any{f.FIT.Type, f.FIT.Manufacturer, f.FIT.Product, f.FIT.Serial, nullIf0(f.FIT.TimeCreated), f.FIT.Number}
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO raw_files (sha256, size, user_id, device_id, source, source_name, is_fit,
			fit_type, fit_manufacturer, fit_product, fit_serial, fit_time_created, fit_number, duplicate_of, received_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			f.SHA256, f.Size, f.UserID, nullIf0(f.DeviceID), f.Source, f.SourceName, f.FIT != nil,
			fit[0], fit[1], fit[2], fit[3], fit[4], fit[5], nullIf0(f.DuplicateOf), f.ReceivedAt.Unix())
		if err != nil {
			return err
		}
		if f.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		if twin != 0 && f.DuplicateOf == 0 {
			// The new copy has the better source: the old one steps aside and loses its data.
			if _, err := tx.ExecContext(ctx, `UPDATE raw_files SET duplicate_of = ? WHERE id = ?`, f.ID, twin); err != nil {
				return err
			}
			return deleteFileData(ctx, tx, twin)
		}
		return nil
	})
	if err != nil {
		return RawFile{}, false, err
	}
	return f, true, nil
}

func nullIf0[T int64 | uint32 | uint16](v T) any {
	if v == 0 {
		return nil
	}
	return v
}

const rawFileCols = `id, sha256, size, user_id, device_id, source, source_name, is_fit, fit_type, fit_manufacturer,
	fit_product, fit_serial, fit_time_created, fit_number, duplicate_of, received_at`

func scanRawFile(row interface{ Scan(...any) error }) (RawFile, error) {
	var f RawFile
	var dev, dup, typ, man, prod, serial, created, num sql.NullInt64
	var isFIT bool
	var received int64
	err := row.Scan(&f.ID, &f.SHA256, &f.Size, &f.UserID, &dev, &f.Source, &f.SourceName, &isFIT,
		&typ, &man, &prod, &serial, &created, &num, &dup, &received)
	if errors.Is(err, sql.ErrNoRows) {
		return RawFile{}, ErrNotFound
	}
	if err != nil {
		return RawFile{}, err
	}
	f.DeviceID, f.DuplicateOf = dev.Int64, dup.Int64
	f.ReceivedAt = time.Unix(received, 0)
	if isFIT {
		f.FIT = &FITIdentity{Type: uint8(typ.Int64), Manufacturer: uint16(man.Int64), Product: uint16(prod.Int64),
			Serial: uint32(serial.Int64), TimeCreated: created.Int64, Number: uint16(num.Int64)}
	}
	return f, nil
}

// RawFile returns the raw file with this id.
func (s *Store) RawFile(ctx context.Context, id int64) (RawFile, error) {
	return scanRawFile(s.db.W.QueryRowContext(ctx, `SELECT `+rawFileCols+` FROM raw_files WHERE id = ?`, id))
}

// RawFileBySHA returns the raw file with this digest.
func (s *Store) RawFileBySHA(ctx context.Context, sum string) (RawFile, error) {
	return scanRawFile(s.db.W.QueryRowContext(ctx, `SELECT `+rawFileCols+` FROM raw_files WHERE sha256 = ?`, sum))
}

// DeviceSummary is what came from one watch into one account.
type DeviceSummary struct {
	Files      int
	Bytes      int64
	ByType     map[int]int // FIT file type → files
	Activities int
	Nights     int
	Days       int    // local days with daily values or a night from this watch
	FirstDay   string // "" when none
	LastDay    string
	FirstAt    int64 // the oldest and newest recording (Unix seconds), 0 when none
	LastAt     int64
}

// DeviceSummary counts what user has from device.
func (s *Store) DeviceSummary(ctx context.Context, user, device int64) (DeviceSummary, error) {
	sum := DeviceSummary{ByType: map[int]int{}}
	err := s.db.R.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(size), 0),
			COALESCE(MIN(COALESCE(fit_time_created, received_at)), 0), COALESCE(MAX(COALESCE(fit_time_created, received_at)), 0)
		FROM raw_files WHERE device_id = ? AND user_id = ? AND duplicate_of IS NULL`, device, user).
		Scan(&sum.Files, &sum.Bytes, &sum.FirstAt, &sum.LastAt)
	if err != nil {
		return sum, err
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT COALESCE(fit_type, -1), COUNT(*) FROM raw_files
		WHERE device_id = ? AND user_id = ? AND duplicate_of IS NULL GROUP BY fit_type`, device, user)
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var t, n int
		if err := rows.Scan(&t, &n); err != nil {
			rows.Close()
			return sum, err
		}
		sum.ByType[t] = n
	}
	rows.Close()
	files := `SELECT id FROM raw_files WHERE device_id = ? AND user_id = ?`
	if err := s.db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM activities WHERE user_id = ? AND raw_file_id IN (`+files+`)`,
		user, device, user).Scan(&sum.Activities); err != nil {
		return sum, err
	}
	if err := s.db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM sleep_sessions s WHERE s.user_id = ? AND EXISTS (
		SELECT 1 FROM sleep_parts p WHERE p.raw_file_id IN (`+files+`) AND p.user_id = s.user_id
			AND p.start_ts < s.end_ts AND p.end_ts > s.start_ts)`,
		user, device, user).Scan(&sum.Nights); err != nil {
		return sum, err
	}
	// Days: daily values and nights name their day; monitoring files (FIT 32) cover the day they
	// were recorded on, in the watch's own time zone.
	days := map[string]bool{}
	rows, err = s.db.R.QueryContext(ctx, `SELECT day FROM daily_values WHERE user_id = ? AND raw_file_id IN (`+files+`)
		UNION SELECT s.day FROM sleep_sessions s WHERE s.user_id = ? AND EXISTS (
			SELECT 1 FROM sleep_parts p WHERE p.raw_file_id IN (`+files+`) AND p.user_id = s.user_id
				AND p.start_ts < s.end_ts AND p.end_ts > s.start_ts)`,
		user, device, user, user, device, user)
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return sum, err
		}
		days[d] = true
	}
	rows.Close()
	zone, err := s.Zone(ctx, user)
	if err != nil {
		return sum, err
	}
	rows, err = s.db.R.QueryContext(ctx, `SELECT fit_time_created FROM raw_files WHERE device_id = ? AND user_id = ?
		AND fit_type = 32 AND fit_time_created IS NOT NULL AND duplicate_of IS NULL`, device, user)
	if err != nil {
		return sum, err
	}
	for rows.Next() {
		var ts int64
		if err := rows.Scan(&ts); err != nil {
			rows.Close()
			return sum, err
		}
		days[zone.Day(ts)] = true
	}
	rows.Close()
	sum.Days = len(days)
	for d := range days {
		if sum.FirstDay == "" || d < sum.FirstDay {
			sum.FirstDay = d
		}
		if d > sum.LastDay {
			sum.LastDay = d
		}
	}
	return sum, rows.Err()
}

// DeviceFile is a raw file from a watch, by the name its sync stored it under.
type DeviceFile struct {
	ID          int64
	UserID      int64
	FITType     int // -1 when not a FIT file
	TimeCreated int64
	Size        int64
}

// DeviceFiles maps the source names of user's files from device to them.
func (s *Store) DeviceFiles(ctx context.Context, user, device int64) (map[string]DeviceFile, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT source_name, id, user_id, COALESCE(fit_type, -1), COALESCE(fit_time_created, 0), size
		FROM raw_files WHERE device_id = ? AND user_id = ?`, device, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]DeviceFile{}
	for rows.Next() {
		var name string
		var f DeviceFile
		if err := rows.Scan(&name, &f.ID, &f.UserID, &f.FITType, &f.TimeCreated, &f.Size); err != nil {
			return nil, err
		}
		out[name] = f
	}
	return out, rows.Err()
}

// HasDeviceFile reports whether the raw store holds the file a sync stored as name (the watch
// item's "<uid>.fit") from device.
func (s *Store) HasDeviceFile(ctx context.Context, device int64, name string) (bool, error) {
	var n int
	err := s.db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM raw_files WHERE device_id = ? AND source_name = ?`,
		device, name).Scan(&n)
	return n > 0, err
}

// RawFilesOf lists the raw files that came from device, whoever they belong to, oldest first.
func (s *Store) RawFilesOf(ctx context.Context, device int64) ([]RawFile, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT `+rawFileCols+` FROM raw_files WHERE device_id = ?
		ORDER BY COALESCE(fit_time_created, received_at), id`, device)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RawFile
	for rows.Next() {
		f, err := scanRawFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// PendingIngest lists the FIT files that are not duplicates and have not been ingested by
// handler at version (or later), oldest recording first.
func (s *Store) PendingIngest(ctx context.Context, handler string, version int) ([]int64, error) {
	rows, err := s.db.W.QueryContext(ctx, `SELECT f.id FROM raw_files f
		LEFT JOIN ingest_state i ON i.raw_file_id = f.id AND i.handler = ?
		WHERE f.is_fit = 1 AND f.duplicate_of IS NULL AND (i.version IS NULL OR i.version < ?)
		ORDER BY COALESCE(f.fit_time_created, f.received_at), f.id`, handler, version)
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

// ResetIngest forgets handler's state for every file, so the next run ingests them all again.
func (s *Store) ResetIngest(ctx context.Context, handler string) error {
	_, err := s.db.W.ExecContext(ctx, `DELETE FROM ingest_state WHERE handler = ?`, handler)
	return err
}

// RawFileCounts summarises the raw files of a user: total, duplicates, and per FIT type.
func (s *Store) RawFileCounts(ctx context.Context, user int64) (total, dups int, byType map[int]int, err error) {
	byType = map[int]int{}
	rows, err := s.db.R.QueryContext(ctx, `SELECT COALESCE(fit_type, -1), duplicate_of IS NOT NULL, COUNT(*)
		FROM raw_files WHERE user_id = ? GROUP BY 1, 2`, user)
	if err != nil {
		return 0, 0, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var typ, n int
		var dup bool
		if err := rows.Scan(&typ, &dup, &n); err != nil {
			return 0, 0, nil, err
		}
		total += n
		if dup {
			dups += n
		} else {
			byType[typ] += n
		}
	}
	return total, dups, byType, rows.Err()
}

// FileQuery narrows UserFiles.
type FileQuery struct {
	Device int64 // 0: any
	Type   int   // FIT type; -1 any, -2 files that are not FIT
	Before int64 // id keyset: files with a smaller id (0: from the newest)
	Limit  int
}

// UserFiles lists user's raw files (copies of others left out), newest first.
func (s *Store) UserFiles(ctx context.Context, user int64, q FileQuery) ([]RawFile, error) {
	where, args := `user_id = ? AND duplicate_of IS NULL`, []any{user}
	if q.Device != 0 {
		where, args = where+` AND device_id = ?`, append(args, q.Device)
	}
	switch {
	case q.Type == -2:
		where += ` AND fit_type IS NULL`
	case q.Type >= 0:
		where, args = where+` AND fit_type = ?`, append(args, q.Type)
	}
	if q.Before > 0 {
		where, args = where+` AND id < ?`, append(args, q.Before)
	}
	if q.Limit <= 0 {
		q.Limit = 100
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT `+rawFileCols+` FROM raw_files WHERE `+where+` ORDER BY id DESC LIMIT ?`, append(args, q.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RawFile
	for rows.Next() {
		f, err := scanRawFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// UserFile returns one of user's raw files (ErrNotFound for anyone else's).
func (s *Store) UserFile(ctx context.Context, user, id int64) (RawFile, error) {
	f, err := scanRawFile(s.db.R.QueryRowContext(ctx, `SELECT `+rawFileCols+` FROM raw_files WHERE id = ? AND user_id = ?`, id, user))
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}
