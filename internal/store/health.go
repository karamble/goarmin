// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/karamble/goarmin/pkg/fitactivity"
	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

// Zone turns times into a user's local days: from the watch's own UTC offset history where it
// has one, else from the user's time zone.
type Zone struct {
	ts, off []int64 // ascending ts; offset in seconds from each ts on
	loc     *time.Location
}

// NewZone builds a zone from offset history (any order) and a fallback location.
func NewZone(offsets map[int64]int64, loc *time.Location) Zone {
	if loc == nil {
		loc = time.Local
	}
	z := Zone{loc: loc}
	keys := make([]int64, 0, len(offsets))
	for t := range offsets {
		keys = append(keys, t)
	}
	slices.Sort(keys)
	for _, t := range keys {
		if n := len(z.off); n > 0 && z.off[n-1] == offsets[t] {
			continue // same offset as before: no change
		}
		z.ts, z.off = append(z.ts, t), append(z.off, offsets[t])
	}
	return z
}

// Offset is the UTC offset in seconds at ts.
func (z Zone) Offset(ts int64) int64 {
	i := sort.Search(len(z.ts), func(i int) bool { return z.ts[i] > ts })
	if i > 0 {
		return z.off[i-1]
	}
	if len(z.ts) > 0 && z.ts[0]-ts < 2*86400 {
		return z.off[0] // just before the history starts: the zone it starts with
	}
	_, off := time.Unix(ts, 0).In(z.loc).Zone()
	return int64(off)
}

// Day is the local date at ts.
func (z Zone) Day(ts int64) string {
	return time.Unix(ts+z.Offset(ts), 0).UTC().Format(time.DateOnly)
}

// DayRange returns the UTC span [from, to) of a local date.
func (z Zone) DayRange(day string) (from, to int64, err error) {
	d, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return 0, 0, err
	}
	local := func(m int64) int64 { // UTC instant of local wall time m
		return m - z.Offset(m-z.Offset(m))
	}
	m := d.Unix()
	return local(m), local(m + 86400), nil
}

// Zone loads user's offset history.
func (s *Store) Zone(ctx context.Context, user int64) (Zone, error) {
	return s.zone(ctx, s.db.W, user)
}

type querier interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

func (s *Store) zone(ctx context.Context, q querier, user int64) (Zone, error) {
	var tz string
	if err := q.QueryRowContext(ctx, `SELECT timezone FROM users WHERE id = ?`, user).Scan(&tz); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Zone{}, err
	}
	loc := time.Local
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	rows, err := q.QueryContext(ctx, `SELECT ts, offset_s FROM tz_offsets WHERE user_id = ?`, user)
	if err != nil {
		return Zone{}, err
	}
	defer rows.Close()
	offs := map[int64]int64{}
	for rows.Next() {
		var t, o int64
		if err := rows.Scan(&t, &o); err != nil {
			return Zone{}, err
		}
		offs[t] = o
	}
	return NewZone(offs, loc), rows.Err()
}

// deleteFileData removes what one raw file contributed (its sleep sessions included).
func deleteFileData(ctx context.Context, tx *sql.Tx, file int64) error {
	for _, t := range []string{"samples", "tz_offsets", "daily_values", "hrv_nightly", "sleep_sessions", "activities", "suggested_workouts",
		"watch_settings", "sport_profiles", "watch_goals", "lifetime_totals", "workouts", "courses"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+t+` WHERE raw_file_id = ?`, file); err != nil {
			return err
		}
	}
	return nil
}

func nullUnknown(v int) any {
	if v == wellness.Unknown {
		return nil
	}
	return v
}

func nullUnknownF(v float64) any {
	if v == wellness.Unknown {
		return nil
	}
	return v
}

// WriteFileData replaces what file contributed with d and records the ingest. Sleep sessions
// are merged with what other files said about the same night. It returns the local days the
// data touches, for RebuildDays.
func (s *Store) WriteFileData(ctx context.Context, f RawFile, d *wellness.Data, acts []fitactivity.Activity, handler string, version int) ([]string, error) {
	days := map[string]bool{}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		// The days this file wrote before are rebuilt too: a file ingested again (a decoder
		// fix) may no longer reach them, and a day with nothing left must go.
		if err := s.fileDays(ctx, tx, f, days); err != nil {
			return err
		}
		for _, t := range []string{"samples", "tz_offsets", "daily_values", "hrv_nightly"} {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+t+` WHERE raw_file_id = ?`, f.ID); err != nil {
				return err
			}
		}
		for _, o := range d.Offsets {
			if _, err := tx.ExecContext(ctx, `INSERT INTO tz_offsets (user_id, ts, offset_s, raw_file_id) VALUES (?, ?, ?, ?)
				ON CONFLICT (user_id, ts) DO UPDATE SET offset_s = excluded.offset_s, raw_file_id = excluded.raw_file_id`,
				f.UserID, o.At.Unix(), int64(o.Offset/time.Second), f.ID); err != nil {
				return err
			}
		}
		z, err := s.zone(ctx, tx, f.UserID)
		if err != nil {
			return err
		}
		ins, err := tx.PrepareContext(ctx, `INSERT INTO samples (user_id, kind, ts, sub, value, raw_file_id) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, kind, ts, sub) DO UPDATE SET value = excluded.value, raw_file_id = excluded.raw_file_id`)
		if err != nil {
			return err
		}
		defer ins.Close()
		seen := map[int64]bool{} // 15-minute slots: local midnight always falls on a slot boundary
		for _, smp := range d.Samples {
			t := smp.At.Unix()
			if _, err := ins.ExecContext(ctx, f.UserID, smp.Kind, t, smp.Sub, smp.Value, f.ID); err != nil {
				return err
			}
			if slot := t / 900; !seen[slot] {
				seen[slot] = true
				days[z.Day(t)] = true
			}
		}
		for _, v := range d.Daily {
			day := v.Day
			if day == "" {
				day = z.Day(v.At.Unix())
			}
			days[day] = true
			if _, err := tx.ExecContext(ctx, `INSERT INTO daily_values (user_id, kind, day, ts, value, raw_file_id) VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, kind, day) DO UPDATE SET value = excluded.value, ts = excluded.ts, raw_file_id = excluded.raw_file_id
				WHERE excluded.ts >= daily_values.ts`, f.UserID, v.Kind, day, v.At.Unix(), v.Value, f.ID); err != nil {
				return err
			}
		}
		for _, sl := range d.Sleep {
			day := z.Day(sl.End.Unix())
			days[day] = true
			_, err := tx.ExecContext(ctx, `INSERT INTO sleep_sessions (user_id, start_ts, end_ts, day, score, quality_score,
				duration_score, recovery_score, min_hr, bb_start, bb_end, deep_s, light_s, rem_s, awake_s, unmeasurable_s, raw_file_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, start_ts) DO UPDATE SET
					end_ts = excluded.end_ts, day = excluded.day,
					score = COALESCE(excluded.score, score), quality_score = COALESCE(excluded.quality_score, quality_score),
					duration_score = COALESCE(excluded.duration_score, duration_score),
					recovery_score = COALESCE(excluded.recovery_score, recovery_score),
					min_hr = COALESCE(excluded.min_hr, min_hr), bb_start = COALESCE(excluded.bb_start, bb_start),
					bb_end = COALESCE(excluded.bb_end, bb_end), deep_s = COALESCE(excluded.deep_s, deep_s),
					light_s = COALESCE(excluded.light_s, light_s), rem_s = COALESCE(excluded.rem_s, rem_s),
					awake_s = COALESCE(excluded.awake_s, awake_s), unmeasurable_s = COALESCE(excluded.unmeasurable_s, unmeasurable_s),
					raw_file_id = excluded.raw_file_id`,
				f.UserID, sl.Start.Unix(), sl.End.Unix(), day, nullUnknown(sl.Score), nullUnknown(sl.QualityScore),
				nullUnknown(sl.DurationScore), nullUnknown(sl.RecoveryScore), nullUnknown(sl.MinHR),
				nullUnknown(sl.BodyBatteryStart), nullUnknown(sl.BodyBatteryEnd), nullUnknown(sl.Deep), nullUnknown(sl.Light),
				nullUnknown(sl.REM), nullUnknown(sl.Awake), nullUnknown(sl.Unmeasurable), f.ID)
			if err != nil {
				return err
			}
		}
		for _, h := range d.HRV {
			day := z.Day(h.At.Unix())
			days[day] = true
			_, err := tx.ExecContext(ctx, `INSERT INTO hrv_nightly (user_id, day, ts, weekly_avg_ms, last_night_ms, last_night_high_ms,
				baseline_low_upper_ms, balanced_lower_ms, balanced_upper_ms, status, raw_file_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, day) DO UPDATE SET ts = excluded.ts, weekly_avg_ms = excluded.weekly_avg_ms,
					last_night_ms = excluded.last_night_ms, last_night_high_ms = excluded.last_night_high_ms,
					baseline_low_upper_ms = excluded.baseline_low_upper_ms, balanced_lower_ms = excluded.balanced_lower_ms,
					balanced_upper_ms = excluded.balanced_upper_ms, status = excluded.status, raw_file_id = excluded.raw_file_id`,
				f.UserID, day, h.At.Unix(), nullUnknownF(h.WeeklyAverage), nullUnknownF(h.LastNight), nullUnknownF(h.LastNightFiveMinHigh),
				nullUnknownF(h.BaselineLowUpper), nullUnknownF(h.BalancedLower), nullUnknownF(h.BalancedUpper), nullUnknown(h.Status), f.ID)
			if err != nil {
				return err
			}
		}
		if err := writeActivities(ctx, tx, f, acts, z, days); err != nil {
			return err
		}
		if err := writeSuggestions(ctx, tx, f, d.Suggestions); err != nil {
			return err
		}
		from, to := d.Span()
		for _, a := range acts {
			if from.IsZero() || a.Start.Before(from) {
				from = a.Start
			}
			if a.End.After(to) {
				to = a.End
			}
		}
		return recordIngest(ctx, tx, f.ID, handler, version, "ok", "", from, to, s.now())
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(days))
	for day := range days {
		out = append(out, day)
	}
	slices.Sort(out)
	return out, nil
}

func recordIngest(ctx context.Context, tx *sql.Tx, file int64, handler string, version int, status, msg string, from, to, now time.Time) error {
	span := func(t time.Time) any {
		if t.IsZero() {
			return nil
		}
		return t.Unix()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO ingest_state (raw_file_id, handler, version, status, error, span_from, span_to, ingested_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (raw_file_id, handler) DO UPDATE SET version = excluded.version, status = excluded.status,
			error = excluded.error, span_from = excluded.span_from, span_to = excluded.span_to, ingested_at = excluded.ingested_at`,
		file, handler, version, status, msg, span(from), span(to), now.Unix())
	return err
}

// IngestFailed records that handler could not ingest file.
func (s *Store) IngestFailed(ctx context.Context, file int64, handler string, version int, cause error) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		return recordIngest(ctx, tx, file, handler, version, "error", cause.Error(), time.Time{}, time.Time{}, s.now())
	})
}

// DaySummary is a row of daily_summary; nil fields have no data.
type DaySummary struct {
	Day                                        string
	Steps, DistanceCM, ActiveKcal              *int64
	AscentMM, DescentMM                        *int64
	ModerateMin, VigorousMin                   *int64
	HRMin, HRMax, HRAvg, RestingHR             *int64
	StressAvg, StressMax, BBMin, BBMax         *int64
	RespirationAvg, SpO2Avg, SleepScore, Sleep *int64
}

// fileDays adds the local days the file's stored samples, daily values and HRV nights cover.
func (s *Store) fileDays(ctx context.Context, tx *sql.Tx, f RawFile, days map[string]bool) error {
	z, err := s.zone(ctx, tx, f.UserID)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT ts / 900 FROM samples WHERE raw_file_id = ?`, f.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var slot int64
		if err := rows.Scan(&slot); err != nil {
			rows.Close()
			return err
		}
		days[z.Day(slot*900)] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, q := range []string{`SELECT DISTINCT day FROM daily_values WHERE raw_file_id = ?`, `SELECT DISTINCT day FROM hrv_nightly WHERE raw_file_id = ?`} {
		rows, err := tx.QueryContext(ctx, q, f.ID)
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
	}
	return nil
}

// RebuildAllDays rebuilds every stored daily summary of every account from its data, dropping
// days with nothing left, and returns how many days it rebuilt.
func (s *Store) RebuildAllDays(ctx context.Context) (int, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT user_id, day FROM daily_summary ORDER BY user_id, day`)
	if err != nil {
		return 0, err
	}
	byUser := map[int64][]string{}
	n := 0
	for rows.Next() {
		var u int64
		var d string
		if err := rows.Scan(&u, &d); err != nil {
			rows.Close()
			return 0, err
		}
		byUser[u] = append(byUser[u], d)
		n++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for u, days := range byUser {
		if err := s.RebuildDays(ctx, u, days); err != nil {
			return 0, err
		}
	}
	return n, nil
}

// RebuildDays recomputes daily_summary for user's days from the stored data.
func (s *Store) RebuildDays(ctx context.Context, user int64, days []string) error {
	if len(days) == 0 {
		return nil
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		z, err := s.zone(ctx, tx, user)
		if err != nil {
			return err
		}
		for _, day := range days {
			if err := s.rebuildDay(ctx, tx, user, day, z); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) rebuildDay(ctx context.Context, tx *sql.Tx, user int64, day string, z Zone) error {
	from, to, err := z.DayRange(day)
	if err != nil {
		return err
	}
	// Running totals restart at local midnight: the day's amount is each series' largest value
	// (per activity type, summed).
	total := func(k wellness.Kind) (any, error) {
		var v sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT SUM(m) FROM (SELECT MAX(value) AS m FROM samples
			WHERE user_id = ? AND kind = ? AND ts >= ? AND ts < ? GROUP BY sub)`, user, k, from, to).Scan(&v)
		if !v.Valid {
			return nil, err
		}
		return v.Int64, err
	}
	agg := func(fn string, k wellness.Kind) (any, error) {
		var v sql.NullFloat64
		err := tx.QueryRowContext(ctx, `SELECT `+fn+`(value) FROM samples WHERE user_id = ? AND kind = ? AND ts >= ? AND ts < ?`,
			user, k, from, to).Scan(&v)
		if !v.Valid {
			return nil, err
		}
		return int64(v.Float64 + 0.5), err
	}
	cols := []struct {
		name string
		get  func() (any, error)
	}{
		{"steps", func() (any, error) { return total(wellness.Steps) }},
		{"distance_cm", func() (any, error) { return total(wellness.Distance) }},
		{"active_kcal", func() (any, error) { return total(wellness.ActiveCalories) }},
		{"ascent_mm", func() (any, error) { return agg("MAX", wellness.Ascent) }},
		{"descent_mm", func() (any, error) { return agg("MAX", wellness.Descent) }},
		{"moderate_min", func() (any, error) { return agg("SUM", wellness.ModerateMinutes) }},
		{"vigorous_min", func() (any, error) { return agg("SUM", wellness.VigorousMinutes) }},
		{"hr_min", func() (any, error) { return agg("MIN", wellness.HeartRate) }},
		{"hr_max", func() (any, error) { return agg("MAX", wellness.HeartRate) }},
		{"hr_avg", func() (any, error) { return agg("AVG", wellness.HeartRate) }},
		{"resting_hr", func() (any, error) {
			var v sql.NullInt64
			err := tx.QueryRowContext(ctx, `SELECT value FROM daily_values WHERE user_id = ? AND kind = ? AND day = ?`,
				user, wellness.RestingHR, day).Scan(&v)
			if errors.Is(err, sql.ErrNoRows) || !v.Valid {
				return nil, nil
			}
			return v.Int64, err
		}},
		{"stress_avg", func() (any, error) { return agg("AVG", wellness.Stress) }},
		{"stress_max", func() (any, error) { return agg("MAX", wellness.Stress) }},
		{"bb_min", func() (any, error) { return agg("MIN", wellness.BodyBattery) }},
		{"bb_max", func() (any, error) { return agg("MAX", wellness.BodyBattery) }},
		{"respiration_avg", func() (any, error) { return agg("AVG", wellness.Respiration) }},
		{"spo2_avg", func() (any, error) { return agg("AVG", wellness.SpO2) }},
	}
	vals := make([]any, 0, len(cols)+5)
	vals = append(vals, user, day)
	names := "user_id, day"
	marks := "?, ?"
	for _, c := range cols {
		v, err := c.get()
		if err != nil {
			return err
		}
		vals = append(vals, v)
		names += ", " + c.name
		marks += ", ?"
	}
	// The night that ended this day; the longest one if there were several (naps).
	var score, secs sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT score, end_ts - start_ts FROM sleep_sessions WHERE user_id = ? AND day = ?
		ORDER BY end_ts - start_ts DESC LIMIT 1`, user, day).Scan(&score, &secs)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	empty := !score.Valid && !secs.Valid
	for _, v := range vals[2:] {
		if v != nil {
			empty = false
		}
	}
	if empty {
		// Nothing left for the day (its data was deleted): no row, rather than a row of blanks.
		_, err = tx.ExecContext(ctx, `DELETE FROM daily_summary WHERE user_id = ? AND day = ?`, user, day)
		return err
	}
	vals = append(vals, nullInt(score), nullInt(secs), s.now().Unix())
	names += ", sleep_score, sleep_s, updated_at"
	marks += ", ?, ?, ?"
	_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO daily_summary (`+names+`) VALUES (`+marks+`)`, vals...)
	return err
}

func nullInt(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

// DaySummaries returns user's daily summaries for days in [from, to] ("YYYY-MM-DD"), oldest first.
func (s *Store) DaySummaries(ctx context.Context, user int64, from, to string) ([]DaySummary, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT day, steps, distance_cm, active_kcal, ascent_mm, descent_mm, moderate_min,
		vigorous_min, hr_min, hr_max, hr_avg, resting_hr, stress_avg, stress_max, bb_min, bb_max, respiration_avg, spo2_avg,
		sleep_score, sleep_s FROM daily_summary WHERE user_id = ? AND day >= ? AND day <= ? ORDER BY day`, user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DaySummary
	for rows.Next() {
		var d DaySummary
		if err := rows.Scan(&d.Day, &d.Steps, &d.DistanceCM, &d.ActiveKcal, &d.AscentMM, &d.DescentMM, &d.ModerateMin,
			&d.VigorousMin, &d.HRMin, &d.HRMax, &d.HRAvg, &d.RestingHR, &d.StressAvg, &d.StressMax, &d.BBMin, &d.BBMax,
			&d.RespirationAvg, &d.SpO2Avg, &d.SleepScore, &d.Sleep); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Point is one stored sample.
type Point struct {
	TS    int64
	Sub   uint8
	Value int64
}

// Series returns user's samples of kind in [from, to), oldest first.
func (s *Store) Series(ctx context.Context, user int64, kind wellness.Kind, from, to int64) ([]Point, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT ts, sub, value FROM samples WHERE user_id = ? AND kind = ? AND ts >= ? AND ts < ?
		ORDER BY ts, sub`, user, kind, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.TS, &p.Sub, &p.Value); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SleepSession is a row of sleep_sessions; nil fields are unknown.
type SleepSession struct {
	Start, End                                        int64
	Day                                               string
	Score, QualityScore, DurationScore, RecoveryScore *int64
	MinHR, BBStart, BBEnd                             *int64
	Deep, Light, REM, Awake, Unmeasurable             *int64
}

// SleepSessions returns user's sleeps that ended on days in [from, to], oldest first.
func (s *Store) SleepSessions(ctx context.Context, user int64, from, to string) ([]SleepSession, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT start_ts, end_ts, day, score, quality_score, duration_score, recovery_score,
		min_hr, bb_start, bb_end, deep_s, light_s, rem_s, awake_s, unmeasurable_s FROM sleep_sessions
		WHERE user_id = ? AND day >= ? AND day <= ? ORDER BY start_ts`, user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SleepSession
	for rows.Next() {
		var x SleepSession
		if err := rows.Scan(&x.Start, &x.End, &x.Day, &x.Score, &x.QualityScore, &x.DurationScore, &x.RecoveryScore,
			&x.MinHR, &x.BBStart, &x.BBEnd, &x.Deep, &x.Light, &x.REM, &x.Awake, &x.Unmeasurable); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// DailyValue is a stored once-a-day value.
type DailyValue struct {
	Day   string
	Kind  wellness.Kind
	Value int64
}

// DailyValues returns user's daily values for days in [from, to].
func (s *Store) DailyValues(ctx context.Context, user int64, from, to string) ([]DailyValue, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT day, kind, value FROM daily_values WHERE user_id = ? AND day >= ? AND day <= ?
		ORDER BY day, kind`, user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DailyValue
	for rows.Next() {
		var v DailyValue
		if err := rows.Scan(&v.Day, &v.Kind, &v.Value); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// HRVNight is a row of hrv_nightly; nil fields are unknown.
type HRVNight struct {
	WeeklyAvgMs      *float64 `json:"weekly_avg_ms"`
	LastNightMs      *float64 `json:"last_night_ms"`
	LastNightHighMs  *float64 `json:"last_night_high_ms"`
	BaselineLowUpper *float64 `json:"baseline_low_upper_ms"`
	BalancedLower    *float64 `json:"balanced_lower_ms"`
	BalancedUpper    *float64 `json:"balanced_upper_ms"`
	Status           *int64   `json:"status"`
}

// HRVNights returns user's HRV status rows for days in [from, to], by day.
func (s *Store) HRVNights(ctx context.Context, user int64, from, to string) (map[string]HRVNight, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT day, weekly_avg_ms, last_night_ms, last_night_high_ms, baseline_low_upper_ms,
		balanced_lower_ms, balanced_upper_ms, status FROM hrv_nightly WHERE user_id = ? AND day >= ? AND day <= ?`, user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]HRVNight{}
	for rows.Next() {
		var day string
		var h HRVNight
		if err := rows.Scan(&day, &h.WeeklyAvgMs, &h.LastNightMs, &h.LastNightHighMs, &h.BaselineLowUpper, &h.BalancedLower,
			&h.BalancedUpper, &h.Status); err != nil {
			return nil, err
		}
		out[day] = h
	}
	return out, rows.Err()
}
