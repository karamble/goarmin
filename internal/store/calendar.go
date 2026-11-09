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

// Counted kinds of a calendar day besides its summary: one row each in their tables.
const (
	CountNights   = "sleep" // nights that ended that day
	CountNaps     = "nap"
	CountHRV      = "hrv" // a night with HRV
	CountWeighIns = "weigh_in"
	CountECG      = "ecg"
	CountDrinks   = "drink"
)

// CalendarCounts returns, per local day in [from, to], how many nights, naps, HRV nights,
// weigh-ins, ECG recordings and drinks user has.
func (s *Store) CalendarCounts(ctx context.Context, user int64, from, to string) (map[string]map[string]int, error) {
	out := map[string]map[string]int{}
	add := func(day, kind string, n int) {
		if n <= 0 {
			return
		}
		if out[day] == nil {
			out[day] = map[string]int{}
		}
		out[day][kind] += n
	}
	for kind, q := range map[string]string{
		CountNights: `SELECT day, COUNT(*) FROM sleep_sessions WHERE user_id = ? AND day >= ? AND day <= ? GROUP BY day`,
		CountNaps:   `SELECT day, COUNT(*) FROM naps WHERE user_id = ? AND day >= ? AND day <= ? GROUP BY day`,
		CountHRV:    `SELECT day, COUNT(*) FROM hrv_nightly WHERE user_id = ? AND day >= ? AND day <= ? GROUP BY day`,
		CountECG:    `SELECT day, COUNT(*) FROM ecg_recordings WHERE user_id = ? AND day >= ? AND day <= ? GROUP BY day`,
		CountDrinks: `SELECT day, COUNT(*) FROM hydration_logs WHERE user_id = ? AND day >= ? AND day <= ? AND ml > 0 GROUP BY day`,
	} {
		rows, err := s.db.R.QueryContext(ctx, q, user, from, to)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var day string
			var n int
			if err := rows.Scan(&day, &n); err != nil {
				rows.Close()
				return nil, err
			}
			add(day, kind, n)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	// Weigh-ins carry only their time: their day is the account's.
	z, err := s.Zone(ctx, user)
	if err != nil {
		return nil, err
	}
	f, err1 := time.Parse(time.DateOnly, from)
	t, err2 := time.Parse(time.DateOnly, to)
	if err := errors.Join(err1, err2); err != nil {
		return nil, err
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT ts FROM weigh_ins WHERE user_id = ? AND ts >= ? AND ts < ?`,
		user, f.Add(-14*time.Hour).Unix(), t.Add(38*time.Hour).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ts int64
		if err := rows.Scan(&ts); err != nil {
			return nil, err
		}
		if d := z.Day(ts); d >= from && d <= to {
			add(d, CountWeighIns, 1)
		}
	}
	return out, rows.Err()
}

// WorkoutPlan is a library workout put on a day. Name and sport are the workout's when it was
// planned, so the plan finds the workout again if its stored version is replaced.
type WorkoutPlan struct {
	ID        int64
	Day       string
	WorkoutID int64
	Name      string
	Sport     string
	Note      string
	Created   time.Time
	Updated   time.Time
}

const planCols = `id, day, workout_id, name, sport, note, created_at, updated_at`

func scanPlan(row interface{ Scan(...any) error }) (WorkoutPlan, error) {
	var p WorkoutPlan
	var c, u int64
	err := row.Scan(&p.ID, &p.Day, &p.WorkoutID, &p.Name, &p.Sport, &p.Note, &c, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.Created, p.Updated = time.Unix(c, 0), time.Unix(u, 0)
	return p, err
}

// AddPlan puts a workout on a day for user.
func (s *Store) AddPlan(ctx context.Context, user int64, p WorkoutPlan) (WorkoutPlan, error) {
	now := s.now().Unix()
	res, err := s.db.W.ExecContext(ctx, `INSERT INTO workout_plans (user_id, day, workout_id, name, sport, note, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, user, p.Day, p.WorkoutID, p.Name, p.Sport, p.Note, now, now)
	if err != nil {
		return p, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return p, err
	}
	return s.Plan(ctx, user, id)
}

// Plan returns one of user's plans, or ErrNotFound.
func (s *Store) Plan(ctx context.Context, user, id int64) (WorkoutPlan, error) {
	return scanPlan(s.db.R.QueryRowContext(ctx, `SELECT `+planCols+` FROM workout_plans WHERE user_id = ? AND id = ?`, user, id))
}

// Plans returns user's plans for days in [from, to], by day and then as they were made.
func (s *Store) Plans(ctx context.Context, user int64, from, to string) ([]WorkoutPlan, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT `+planCols+` FROM workout_plans WHERE user_id = ? AND day >= ? AND day <= ?
		ORDER BY day, id`, user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkoutPlan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdatePlan moves one of user's plans to another day and/or changes its note (nil: as it is).
func (s *Store) UpdatePlan(ctx context.Context, user, id int64, day, note *string) (WorkoutPlan, error) {
	p, err := s.Plan(ctx, user, id)
	if err != nil {
		return p, err
	}
	if day != nil {
		p.Day = *day
	}
	if note != nil {
		p.Note = *note
	}
	if _, err := s.db.W.ExecContext(ctx, `UPDATE workout_plans SET day = ?, note = ?, updated_at = ? WHERE user_id = ? AND id = ?`,
		p.Day, p.Note, s.now().Unix(), user, id); err != nil {
		return p, err
	}
	return s.Plan(ctx, user, id)
}

// DeletePlan removes one of user's plans; another's is ErrNotFound.
func (s *Store) DeletePlan(ctx context.Context, user, id int64) error {
	res, err := s.db.W.ExecContext(ctx, `DELETE FROM workout_plans WHERE user_id = ? AND id = ?`, user, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
