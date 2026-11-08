// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"cmp"
	"context"
	"database/sql"
	"slices"

	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

// Each file's account of a sleep is a part (sleep_parts); a night (sleep_sessions) is merged
// from the parts that overlap. A sleep file (FIT 49) and the daily-health file (FIT 72) both
// describe last night, starting a few minutes apart: one night, with the sleep file's stages
// and the daily-health file's Body Battery.

// Sleep part sources and their priority: the part with the highest priority leads a night (its
// start and end), and each value comes from the highest-priority part that has it.
const (
	SleepStages  = "stages"  // a sleep file: the stage records themselves
	SleepSummary = "summary" // the daily-health or metrics file's summary of a night
	SleepJSON    = "json"    // a Garmin Connect export's record
	sleepLegacy  = "legacy"  // a night stored before parts existed
)

func sleepPrio(source string) int {
	switch source {
	case SleepStages:
		return 3
	case SleepJSON:
		return 1
	}
	return 2
}

// sleepCols are the values a night and its parts share; the stage seconds are the last five.
const sleepCols = `score, quality_score, duration_score, recovery_score, min_hr, bb_start, bb_end,
	deep_s, light_s, rem_s, awake_s, unmeasurable_s`

const (
	nSleepVals = 12
	firstStage = 7
)

// SleepPart is one file's account of a sleep.
type SleepPart struct {
	Source string // SleepStages, SleepSummary, SleepJSON
	Sleep  wellness.Sleep
}

func sleepValues(sl wellness.Sleep) []any {
	return []any{nullUnknown(sl.Score), nullUnknown(sl.QualityScore), nullUnknown(sl.DurationScore),
		nullUnknown(sl.RecoveryScore), nullUnknown(sl.MinHR), nullUnknown(sl.BodyBatteryStart), nullUnknown(sl.BodyBatteryEnd),
		nullUnknown(sl.Deep), nullUnknown(sl.Light), nullUnknown(sl.REM), nullUnknown(sl.Awake), nullUnknown(sl.Unmeasurable)}
}

// fitSleepParts are the parts of a decoded FIT file.
func fitSleepParts(sleeps []wellness.Sleep) []SleepPart {
	out := make([]SleepPart, 0, len(sleeps))
	for _, sl := range sleeps {
		src := SleepSummary
		if sl.Stages {
			src = SleepStages
		}
		out = append(out, SleepPart{Source: src, Sleep: sl})
	}
	return out
}

// writeSleep replaces what file said about sleeps and naps, then merges the nights the old and
// the new parts touch. The days of the nights it removed or wrote go into days.
// fresh says the file wrote nothing before (its first ingest).
func writeSleep(ctx context.Context, tx *sql.Tx, user, file int64, parts []SleepPart, naps []wellness.Nap, z Zone, days map[string]bool, fresh bool) error {
	var lo, hi sql.NullInt64
	if !fresh {
		if err := tx.QueryRowContext(ctx, `SELECT MIN(start_ts), MAX(end_ts) FROM sleep_parts WHERE raw_file_id = ?`, file).Scan(&lo, &hi); err != nil {
			return err
		}
		for _, q := range []string{`DELETE FROM sleep_parts WHERE raw_file_id = ?`, `DELETE FROM naps WHERE raw_file_id = ?`} {
			if _, err := tx.ExecContext(ctx, q, file); err != nil {
				return err
			}
		}
	}
	if fresh && len(parts) == 0 && len(naps) == 0 {
		return nil
	}
	span := func(a, b int64) {
		if !lo.Valid || a < lo.Int64 {
			lo = sql.NullInt64{Int64: a, Valid: true}
		}
		if !hi.Valid || b > hi.Int64 {
			hi = sql.NullInt64{Int64: b, Valid: true}
		}
	}
	if len(parts) > 0 {
		ins, err := tx.PrepareContext(ctx, `INSERT INTO sleep_parts (user_id, raw_file_id, source, prio, start_ts, end_ts, `+sleepCols+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (raw_file_id, start_ts) DO UPDATE SET source = excluded.source, prio = excluded.prio, end_ts = excluded.end_ts,
				score = excluded.score, quality_score = excluded.quality_score, duration_score = excluded.duration_score,
				recovery_score = excluded.recovery_score, min_hr = excluded.min_hr, bb_start = excluded.bb_start, bb_end = excluded.bb_end,
				deep_s = excluded.deep_s, light_s = excluded.light_s, rem_s = excluded.rem_s, awake_s = excluded.awake_s,
				unmeasurable_s = excluded.unmeasurable_s`)
		if err != nil {
			return err
		}
		defer ins.Close()
		for _, p := range parts {
			a, b := p.Sleep.Start.Unix(), p.Sleep.End.Unix()
			if b <= a {
				continue
			}
			args := append([]any{user, file, p.Source, sleepPrio(p.Source), a, b}, sleepValues(p.Sleep)...)
			if _, err := ins.ExecContext(ctx, args...); err != nil {
				return err
			}
			span(a, b)
		}
	}
	for _, n := range naps {
		a, b := n.Start.Unix(), n.End.Unix()
		if b <= a {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO naps (user_id, start_ts, end_ts, day, feedback, how, raw_file_id) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, start_ts) DO UPDATE SET end_ts = excluded.end_ts, day = excluded.day, feedback = excluded.feedback,
				how = excluded.how, raw_file_id = excluded.raw_file_id`,
			user, a, b, z.Day(a), nullUnknown(n.Feedback), nullUnknown(n.Source), file); err != nil {
			return err
		}
	}
	if !lo.Valid {
		return nil
	}
	return rebuildNights(ctx, tx, user, lo.Int64, hi.Int64, z, days)
}

type sleepPartRow struct {
	file, prio, start, end int64
	vals                   [nSleepVals]sql.NullInt64
}

// rebuildNights merges user's nights again from the parts in [from, to], widened until no part
// or night crosses its ends. The days of the nights removed and written go into days.
func rebuildNights(ctx context.Context, tx *sql.Tx, user, from, to int64, z Zone, days map[string]bool) error {
	for range 16 {
		var a, b sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT MIN(s), MAX(e) FROM (
			SELECT start_ts AS s, end_ts AS e FROM sleep_parts WHERE user_id = ? AND start_ts <= ? AND end_ts >= ?
			UNION ALL SELECT start_ts, end_ts FROM sleep_sessions WHERE user_id = ? AND start_ts <= ? AND end_ts >= ?)`,
			user, to, from, user, to, from).Scan(&a, &b)
		if err != nil {
			return err
		}
		nf, nt := from, to
		if a.Valid && a.Int64 < nf {
			nf = a.Int64
		}
		if b.Valid && b.Int64 > nt {
			nt = b.Int64
		}
		if nf == from && nt == to {
			break
		}
		from, to = nf, nt
	}
	rows, err := tx.QueryContext(ctx, `SELECT day FROM sleep_sessions WHERE user_id = ? AND start_ts <= ? AND end_ts >= ?`, user, to, from)
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
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sleep_sessions WHERE user_id = ? AND start_ts <= ? AND end_ts >= ?`, user, to, from); err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT raw_file_id, prio, start_ts, end_ts, `+sleepCols+` FROM sleep_parts
		WHERE user_id = ? AND start_ts <= ? AND end_ts >= ? ORDER BY start_ts`, user, to, from)
	if err != nil {
		return err
	}
	var parts []sleepPartRow
	for rows.Next() {
		var p sleepPartRow
		dst := []any{&p.file, &p.prio, &p.start, &p.end}
		for i := range p.vals {
			dst = append(dst, &p.vals[i])
		}
		if err := rows.Scan(dst...); err != nil {
			rows.Close()
			return err
		}
		parts = append(parts, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, night := range clusterNights(parts) {
		day := z.Day(night.end)
		days[day] = true
		args := []any{user, night.start, night.end, day}
		for _, v := range night.vals {
			args = append(args, nullInt(v))
		}
		args = append(args, night.file)
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO sleep_sessions (user_id, start_ts, end_ts, day, `+sleepCols+`, raw_file_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...); err != nil {
			return err
		}
	}
	return nil
}

// clusterNights merges parts (sorted by start) that overlap into nights. A night takes its start
// and end from its leading part (highest priority, then the newest file), each score from the
// first part by that order that has it, and the stage seconds together from the first part that
// has stages. raw_file_id names the leading part's file.
func clusterNights(parts []sleepPartRow) []sleepPartRow {
	var out []sleepPartRow
	for i := 0; i < len(parts); {
		j, end := i+1, parts[i].end
		for j < len(parts) && parts[j].start < end {
			end = max(end, parts[j].end)
			j++
		}
		c := slices.Clone(parts[i:j])
		slices.SortStableFunc(c, func(a, b sleepPartRow) int {
			if a.prio != b.prio {
				return cmp.Compare(b.prio, a.prio)
			}
			return cmp.Compare(b.file, a.file)
		})
		night := sleepPartRow{file: c[0].file, prio: c[0].prio, start: c[0].start, end: c[0].end}
		for k := range firstStage {
			for _, p := range c {
				if p.vals[k].Valid {
					night.vals[k] = p.vals[k]
					break
				}
			}
		}
		for _, p := range c {
			if p.vals[firstStage].Valid {
				copy(night.vals[firstStage:], p.vals[firstStage:])
				break
			}
		}
		out = append(out, night)
		i = j
	}
	return out
}

// RebuildNights merges user's nights in [from, to] (Unix seconds) again from their parts and
// returns the local days whose nights changed, for RebuildDays.
func (s *Store) RebuildNights(ctx context.Context, user, from, to int64) ([]string, error) {
	days := map[string]bool{}
	z, err := s.Zone(ctx, user)
	if err != nil {
		return nil, err
	}
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		return rebuildNights(ctx, tx, user, from, to, z, days)
	})
	return sortedDays(days), err
}

func sortedDays(days map[string]bool) []string {
	out := make([]string, 0, len(days))
	for d := range days {
		out = append(out, d)
	}
	slices.Sort(out)
	return out
}

// Nap is a stored nap.
type Nap struct {
	Start, End int64
	Day        string
	Feedback   *int64
	How        *int64 // FIT nap_source: 0 automatic, 1 manual
}

// Naps returns user's naps on days in [from, to], oldest first.
func (s *Store) Naps(ctx context.Context, user int64, from, to string) ([]Nap, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT start_ts, end_ts, day, feedback, how FROM naps
		WHERE user_id = ? AND day >= ? AND day <= ? ORDER BY start_ts`, user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Nap
	for rows.Next() {
		var n Nap
		if err := rows.Scan(&n.Start, &n.End, &n.Day, &n.Feedback, &n.How); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
