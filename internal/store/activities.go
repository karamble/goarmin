// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/karamble/goarmin/pkg/fitactivity"
	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

func nullNaN(v float64) any {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return v
}

func nullNeg(v int) any {
	if v < 0 {
		return nil
	}
	return v
}

// fitActivitySummary is FIT file type 20: an activity's summary without its track (another
// watch's activity, or one the watch summarises).
const fitActivitySummary = 20

// writeActivities replaces the activities file contributed. An activity keeps its id when its
// file is ingested again, so links to it stay valid. An activity summary (FIT 20) adds a session
// only where no activity file has it: the activity file, with its track, always wins.
func writeActivities(ctx context.Context, tx *sql.Tx, f RawFile, acts []fitactivity.Activity, z Zone, days map[string]bool, fresh bool) error {
	summary := f.FIT != nil && f.FIT.Type == fitActivitySummary
	guard := ""
	if summary {
		guard = ` WHERE (SELECT fit_type FROM raw_files WHERE id = activities.raw_file_id) = 20`
	}
	keep := []any{f.ID}
	marks := ""
	for _, a := range acts {
		start := a.Start.Unix()
		day := z.Day(start)
		days[day] = true
		var b [4]any
		if a.Bounds != nil {
			b = [4]any{a.Bounds.MinLat, a.Bounds.MinLon, a.Bounds.MaxLat, a.Bounds.MaxLon}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO activities (user_id, raw_file_id, session_index, start_ts, end_ts, day,
			sport, sub_sport, name, elapsed_s, timer_s, distance_m, kcal, avg_hr, max_hr, avg_speed, max_speed, avg_cadence,
			avg_power, ascent_m, descent_m, training_effect, anaerobic_effect, training_load, min_lat, min_lon, max_lat, max_lon, polyline,
			workout_name)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, start_ts, session_index) DO UPDATE SET raw_file_id = excluded.raw_file_id,
				end_ts = excluded.end_ts, day = excluded.day, sport = excluded.sport, sub_sport = excluded.sub_sport,
				name = excluded.name, elapsed_s = excluded.elapsed_s, timer_s = excluded.timer_s, distance_m = excluded.distance_m,
				kcal = excluded.kcal, avg_hr = excluded.avg_hr, max_hr = excluded.max_hr, avg_speed = excluded.avg_speed,
				max_speed = excluded.max_speed, avg_cadence = excluded.avg_cadence, avg_power = excluded.avg_power,
				ascent_m = excluded.ascent_m, descent_m = excluded.descent_m, training_effect = excluded.training_effect,
				anaerobic_effect = excluded.anaerobic_effect, training_load = excluded.training_load,
				min_lat = excluded.min_lat, min_lon = excluded.min_lon, max_lat = excluded.max_lat, max_lon = excluded.max_lon,
				polyline = excluded.polyline, workout_name = excluded.workout_name`+guard,
			f.UserID, f.ID, a.Index, start, a.End.Unix(), day, a.Sport, a.SubSport, a.Name, nullNaN(a.Elapsed), nullNaN(a.Timer),
			nullNaN(a.Distance), nullNeg(a.Calories), nullNeg(a.AvgHR), nullNeg(a.MaxHR), nullNaN(a.AvgSpeed), nullNaN(a.MaxSpeed),
			nullNeg(a.AvgCadence), nullNeg(a.AvgPower), nullNeg(a.Ascent), nullNeg(a.Descent), nullNaN(a.TrainingEffect),
			nullNaN(a.AnaerobicEffect), nullNaN(a.TrainingLoad), b[0], b[1], b[2], b[3], a.Polyline, a.Workout)
		if err != nil {
			return err
		}
		var id, owner int64
		if err := tx.QueryRowContext(ctx, `SELECT id, raw_file_id FROM activities WHERE user_id = ? AND start_ts = ? AND session_index = ?`,
			f.UserID, start, a.Index).Scan(&id, &owner); err != nil {
			return err
		}
		if owner != f.ID {
			continue // a summary of an activity stored from its own file
		}
		keep = append(keep, id)
		if !summary {
			// An activity the export made of this session (its start a second off) steps
			// aside; what only Garmin Connect knows about it stays, on this one.
			if _, err := tx.ExecContext(ctx, `UPDATE activities SET extra = COALESCE((SELECT j.extra FROM activities j
				JOIN raw_files r ON r.id = j.raw_file_id WHERE j.user_id = ? AND j.id != ? AND r.fit_type IS NULL
				AND j.start_ts BETWEEN ? AND ?), extra) WHERE id = ? AND extra = '{}'`, f.UserID, id, start-60, start+60, id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM activities WHERE user_id = ? AND id != ? AND start_ts BETWEEN ? AND ?
				AND raw_file_id IN (SELECT id FROM raw_files WHERE user_id = ? AND fit_type IS NULL)`, f.UserID, id, start-60, start+60, f.UserID); err != nil {
				return err
			}
		}
		marks += ", ?"
		for _, q := range []string{`DELETE FROM laps WHERE activity_id = ?`, `DELETE FROM activity_streams WHERE activity_id = ?`,
			`DELETE FROM time_in_zone WHERE activity_id = ?`} {
			if _, err := tx.ExecContext(ctx, q, id); err != nil {
				return err
			}
		}
		for i, l := range a.Laps {
			_, err := tx.ExecContext(ctx, `INSERT INTO laps (activity_id, idx, start_ts, elapsed_s, timer_s, distance_m, kcal,
				avg_hr, max_hr, avg_speed, max_speed, avg_cadence, avg_power, ascent_m, descent_m)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, i, l.Start.Unix(), nullNaN(l.Elapsed), nullNaN(l.Timer), nullNaN(l.Distance), nullNeg(l.Calories),
				nullNeg(l.AvgHR), nullNeg(l.MaxHR), nullNaN(l.AvgSpeed), nullNaN(l.MaxSpeed), nullNeg(l.AvgCadence),
				nullNeg(l.AvgPower), nullNeg(l.Ascent), nullNeg(l.Descent))
			if err != nil {
				return err
			}
		}
		// The watch's own time in zones: the session's as lap -1, then each lap's.
		zones := map[int]*fitactivity.Zones{-1: a.Zones}
		for i, l := range a.Laps {
			zones[i] = l.Zones
		}
		for lap, zs := range zones {
			if zs == nil {
				continue
			}
			b, err := json.Marshal(zs)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO time_in_zone (activity_id, lap, data) VALUES (?, ?, ?)`, id, lap, string(b)); err != nil {
				return err
			}
		}
		if a.Track.Len() > 0 {
			data, fields, err := a.Track.Columns()
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO activity_streams (activity_id, points, fields, data) VALUES (?, ?, ?, ?)`,
				id, a.Track.Len(), strings.Join(fields, ","), data); err != nil {
				return err
			}
		}
	}
	if fresh {
		return nil // it contributed nothing before
	}
	// Whatever this file contributed before and no longer does.
	_, err := tx.ExecContext(ctx, `DELETE FROM activities WHERE raw_file_id = ? AND id NOT IN (0`+marks+`)`, keep...)
	return err
}

// writeSuggestions replaces Garmin Coach's plan for the days the file covers: a newer plan
// wins, whichever file the old one came from.
func writeSuggestions(ctx context.Context, tx *sql.Tx, f RawFile, sug []wellness.Suggestion) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM suggested_workouts WHERE raw_file_id = ?`, f.ID); err != nil {
		return err
	}
	covered := map[string]bool{}
	for _, s := range sug {
		if !covered[s.Day] {
			covered[s.Day] = true
			if _, err := tx.ExecContext(ctx, `DELETE FROM suggested_workouts WHERE user_id = ? AND day = ?`, f.UserID, s.Day); err != nil {
				return err
			}
		}
	}
	for _, s := range sug {
		_, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO suggested_workouts (user_id, day, idx, name, description, sport,
			sub_sport, duration_s, benefit, aerobic_te, anaerobic_te, raw_file_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			f.UserID, s.Day, s.Index, s.Name, s.Description, nullNeg(s.Sport), nullNeg(s.SubSport), int64(s.Duration/time.Second),
			nullNeg(s.Benefit), nullUnknownF(s.AerobicTE), nullUnknownF(s.AnaerobicTE), f.ID)
		if err != nil {
			return err
		}
	}
	return nil
}

// ActivitySummary is a row of activities; nil fields are unknown.
type ActivitySummary struct {
	ID                              int64
	RawFileID                       int64
	Start, End                      int64
	Day                             string
	Sport, SubSport                 int
	Name                            string
	Elapsed, Timer, Distance        *float64
	Kcal, AvgHR, MaxHR              *int64
	AvgSpeed, MaxSpeed              *float64
	AvgCadence, AvgPower            *int64
	Ascent, Descent                 *int64
	TrainingEffect, AnaerobicEffect *float64
	TrainingLoad                    *float64
	MinLat, MinLon, MaxLat, MaxLon  *float64
	Polyline                        string
	// Extra is what only Garmin Connect knows about it (from an export): JSON.
	Extra json.RawMessage
	// WorkoutName is the workout the activity followed ("" when none or unknown).
	WorkoutName string
}

const activityCols = `id, raw_file_id, start_ts, end_ts, day, sport, sub_sport, name, elapsed_s, timer_s, distance_m, kcal,
	avg_hr, max_hr, avg_speed, max_speed, avg_cadence, avg_power, ascent_m, descent_m, training_effect, anaerobic_effect,
	training_load, min_lat, min_lon, max_lat, max_lon, polyline, extra, workout_name`

func scanActivity(row interface{ Scan(...any) error }) (ActivitySummary, error) {
	var a ActivitySummary
	var extra string
	err := row.Scan(&a.ID, &a.RawFileID, &a.Start, &a.End, &a.Day, &a.Sport, &a.SubSport, &a.Name, &a.Elapsed, &a.Timer,
		&a.Distance, &a.Kcal, &a.AvgHR, &a.MaxHR, &a.AvgSpeed, &a.MaxSpeed, &a.AvgCadence, &a.AvgPower, &a.Ascent,
		&a.Descent, &a.TrainingEffect, &a.AnaerobicEffect, &a.TrainingLoad, &a.MinLat, &a.MinLon, &a.MaxLat, &a.MaxLon,
		&a.Polyline, &extra, &a.WorkoutName)
	a.Extra = json.RawMessage(extra)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// Activities returns user's activities that started before before (Unix seconds; 0 = now),
// newest first, at most limit: a keyset page.
func (s *Store) Activities(ctx context.Context, user, before int64, limit int) ([]ActivitySummary, error) {
	if before <= 0 {
		before = math.MaxInt64
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT `+activityCols+` FROM activities WHERE user_id = ? AND start_ts < ?
		ORDER BY start_ts DESC, session_index LIMIT ?`, user, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActivitySummary
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ActivitiesIn returns user's activities recorded on the local days from..to, newest first (at
// most 2000).
func (s *Store) ActivitiesIn(ctx context.Context, user int64, from, to string) ([]ActivitySummary, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT `+activityCols+` FROM activities WHERE user_id = ? AND day >= ? AND day <= ?
		ORDER BY start_ts DESC, session_index LIMIT 2000`, user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActivitySummary
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ActivityAfter returns user's first activity that started after start (ErrNotFound if none).
func (s *Store) ActivityAfter(ctx context.Context, user, start int64) (ActivitySummary, error) {
	return scanActivity(s.db.R.QueryRowContext(ctx, `SELECT `+activityCols+` FROM activities WHERE user_id = ? AND start_ts > ?
		ORDER BY start_ts, session_index LIMIT 1`, user, start))
}

// Activity returns one of user's activities; another user's id is ErrNotFound.
func (s *Store) Activity(ctx context.Context, user, id int64) (ActivitySummary, error) {
	return scanActivity(s.db.R.QueryRowContext(ctx, `SELECT `+activityCols+` FROM activities WHERE user_id = ? AND id = ?`, user, id))
}

// LapRow is a row of laps.
type LapRow struct {
	Index                    int
	Start                    int64
	Elapsed, Timer, Distance *float64
	Kcal, AvgHR, MaxHR       *int64
	AvgSpeed, MaxSpeed       *float64
	AvgCadence, AvgPower     *int64
	Ascent, Descent          *int64
}

// Laps returns the laps of one of user's activities.
func (s *Store) Laps(ctx context.Context, user, id int64) ([]LapRow, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT l.idx, l.start_ts, l.elapsed_s, l.timer_s, l.distance_m, l.kcal, l.avg_hr,
		l.max_hr, l.avg_speed, l.max_speed, l.avg_cadence, l.avg_power, l.ascent_m, l.descent_m
		FROM laps l JOIN activities a ON a.id = l.activity_id WHERE a.user_id = ? AND a.id = ? ORDER BY l.idx`, user, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LapRow
	for rows.Next() {
		var l LapRow
		if err := rows.Scan(&l.Index, &l.Start, &l.Elapsed, &l.Timer, &l.Distance, &l.Kcal, &l.AvgHR, &l.MaxHR, &l.AvgSpeed,
			&l.MaxSpeed, &l.AvgCadence, &l.AvgPower, &l.Ascent, &l.Descent); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Stream returns the stored track of one of user's activities: its fields and the gzipped
// JSON columns.
func (s *Store) Stream(ctx context.Context, user, id int64) (fields []string, data []byte, err error) {
	var f string
	err = s.db.R.QueryRowContext(ctx, `SELECT st.fields, st.data FROM activity_streams st JOIN activities a ON a.id = st.activity_id
		WHERE a.user_id = ? AND a.id = ?`, user, id).Scan(&f, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if f != "" {
		fields = strings.Split(f, ",")
	}
	return fields, data, err
}

// SuggestedWorkout is a row of suggested_workouts.
type SuggestedWorkout struct {
	Day, Name, Description string
	Index                  int
	Sport, Benefit         *int64
	DurationS              int64
	AerobicTE              *float64
}

// SuggestedWorkouts returns Garmin Coach's suggestions for days in [from, to].
func (s *Store) SuggestedWorkouts(ctx context.Context, user int64, from, to string) ([]SuggestedWorkout, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT day, idx, name, description, sport, benefit, duration_s, aerobic_te
		FROM suggested_workouts WHERE user_id = ? AND day >= ? AND day <= ? ORDER BY day, idx`, user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SuggestedWorkout
	for rows.Next() {
		var w SuggestedWorkout
		if err := rows.Scan(&w.Day, &w.Index, &w.Name, &w.Description, &w.Sport, &w.Benefit, &w.DurationS, &w.AerobicTE); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
