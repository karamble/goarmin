// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"database/sql"
	"time"
)

// DeviceDataDeleted is what DeleteDeviceData removed.
type DeviceDataDeleted struct {
	Files, Activities, Nights, Runs int
	// Blobs are the deleted files' digests: the blob store may drop them now.
	Blobs []string
	// Days the deleted data touched, and its time span (Unix seconds; 0 when unknown): the
	// caller re-ingests the remaining files that overlap it, then rebuilds these days.
	Days     []string
	From, To int64
}

// DeleteDeviceData removes everything that came from device into user's account: the raw files
// (their copies from other sources included), every row derived from them, and user's sync runs
// of the device. Other accounts' data is untouched.
func (s *Store) DeleteDeviceData(ctx context.Context, user, device int64) (DeviceDataDeleted, error) {
	var out DeviceDataDeleted
	zone, err := s.Zone(ctx, user) // before the watch's time-zone history goes with its files
	if err != nil {
		return out, err
	}
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		ids, shas, err := deviceFileIDs(ctx, tx, user, device)
		if err != nil || len(ids) == 0 {
			return err
		}
		out.Files, out.Blobs = len(ids), shas
		// The set is frozen in a temporary table: no limit on its size, and it stays the same
		// while its rows are changed.
		for _, q := range []string{`CREATE TEMP TABLE IF NOT EXISTS gone_files (id INTEGER PRIMARY KEY)`, `DELETE FROM gone_files`} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `INSERT INTO gone_files (id) VALUES (?)`, id); err != nil {
				return err
			}
		}
		const in = `SELECT id FROM gone_files`
		var from, to sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MIN(span_from), MAX(span_to) FROM ingest_state WHERE raw_file_id IN (`+in+`)`).
			Scan(&from, &to); err != nil {
			return err
		}
		out.From, out.To = from.Int64, to.Int64
		days := map[string]bool{}
		rows, err := tx.QueryContext(ctx, `SELECT day FROM daily_values WHERE raw_file_id IN (`+in+`)
			UNION SELECT day FROM activities WHERE raw_file_id IN (`+in+`)`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				rows.Close()
				return err
			}
			days[d] = true
		}
		rows.Close()
		if from.Valid && to.Valid && to.Int64 >= from.Int64 {
			for d := zone.Day(from.Int64); d <= zone.Day(to.Int64); d = nextDay(d) {
				days[d] = true
			}
		}
		for d := range days {
			out.Days = append(out.Days, d)
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM activities WHERE raw_file_id IN (`+in+`)`).Scan(&out.Activities); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sleep_sessions s WHERE s.user_id = ? AND EXISTS (
			SELECT 1 FROM sleep_parts p WHERE p.raw_file_id IN (`+in+`) AND p.user_id = s.user_id
				AND p.start_ts < s.end_ts AND p.end_ts > s.start_ts)`, user).Scan(&out.Nights); err != nil {
			return err
		}
		var nightFrom, nightTo sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MIN(start_ts), MAX(end_ts) FROM sleep_parts WHERE raw_file_id IN (`+in+`)`).
			Scan(&nightFrom, &nightTo); err != nil {
			return err
		}
		for _, id := range ids {
			if err := deleteFileData(ctx, tx, id); err != nil {
				return err
			}
		}
		if nightFrom.Valid {
			// Nights are merged from the parts that are left (another watch's, an export's).
			if err := rebuildNights(ctx, tx, user, nightFrom.Int64, nightTo.Int64, zone, days); err != nil {
				return err
			}
		}
		for _, q := range []string{
			`DELETE FROM ingest_state WHERE raw_file_id IN (` + in + `)`,
			`UPDATE raw_files SET duplicate_of = NULL WHERE id IN (` + in + `)`, // copies among themselves
			`DELETE FROM raw_files WHERE id IN (` + in + `)`,
			`DELETE FROM gone_files`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM sync_runs WHERE device_id = ? AND EXISTS (
			SELECT 1 FROM device_assignments a WHERE a.device_id = sync_runs.device_id AND a.user_id = ?
				AND a.valid_from <= sync_runs.started_at AND (a.valid_to IS NULL OR a.valid_to > sync_runs.started_at))`, device, user)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		out.Runs = int(n)
		return nil
	})
	s.setZone(user, Zone{}, false) // the files' time-zone history went with them
	return out, err
}

// deviceFileIDs lists user's raw files from device and the copies of them from other sources.
func deviceFileIDs(ctx context.Context, tx *sql.Tx, user, device int64) ([]int64, []string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, sha256 FROM raw_files WHERE user_id = ? AND (device_id = ?
		OR duplicate_of IN (SELECT id FROM raw_files WHERE user_id = ? AND device_id = ?))`, user, device, user, device)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var ids []int64
	var shas []string
	for rows.Next() {
		var id int64
		var sha string
		if err := rows.Scan(&id, &sha); err != nil {
			return nil, nil, err
		}
		ids, shas = append(ids, id), append(shas, sha)
	}
	return ids, shas, rows.Err()
}

// OverlappingFiles lists user's remaining ingested files whose data overlaps [from, to]: what
// another file deleted may have shared rows with (samples of two watches worn together).
func (s *Store) OverlappingFiles(ctx context.Context, user, from, to int64) ([]int64, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT f.id FROM raw_files f JOIN ingest_state i ON i.raw_file_id = f.id
		WHERE f.user_id = ? AND f.duplicate_of IS NULL AND i.span_from <= ? AND i.span_to >= ? ORDER BY f.id`, user, to, from)
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

func nextDay(day string) string {
	t, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return "9999-12-31"
	}
	return t.AddDate(0, 0, 1).Format(time.DateOnly)
}
