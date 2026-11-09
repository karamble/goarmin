// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/karamble/goarmin/pkg/fitwatch"
)

// recordedAt is when the file was made on the watch, else when goarmin received it.
func recordedAt(f RawFile, w *fitwatch.File) int64 {
	if f.FIT != nil && f.FIT.TimeCreated > 0 {
		return f.FIT.TimeCreated
	}
	if w != nil && !w.Created.IsZero() {
		return w.Created.Unix()
	}
	return f.ReceivedAt.Unix()
}

var watchTables = []string{"watch_settings", "sport_profiles", "watch_goals", "lifetime_totals", "workouts", "workout_files", "courses",
	"gear", "golf_clubs", "personal_records", "settings_changes", "device_series", "device_documents"}

// WriteWatchFile replaces what a settings, sport, workout, course, totals or goals file
// contributed. Per key (the watch's settings, a sport profile, a goal type, a sport's totals,
// a workout or course by name) the newest recording wins, whichever file it came from.
func (s *Store) WriteWatchFile(ctx context.Context, f RawFile, w *fitwatch.File) error {
	at := recordedAt(f, w)
	js := func(v any) (string, error) {
		b, err := json.Marshal(v)
		return string(b), err
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		for _, t := range watchTables {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+t+` WHERE raw_file_id = ?`, f.ID); err != nil {
				return err
			}
		}
		if err := writeDeviceData(ctx, tx, f, w, at); err != nil {
			return err
		}
		if w.Settings != nil {
			data, err := js(w.Settings)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO watch_settings (user_id, device_id, data, recorded_at, raw_file_id) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT (user_id, device_id) DO UPDATE SET data = excluded.data, recorded_at = excluded.recorded_at, raw_file_id = excluded.raw_file_id
				WHERE excluded.recorded_at >= watch_settings.recorded_at`, f.UserID, f.DeviceID, data, at, f.ID); err != nil {
				return err
			}
		}
		if p := w.Sport; p != nil {
			zones, err := js(p.Zones)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO sport_profiles (user_id, device_id, sport, sub_sport, name, zones, recorded_at, raw_file_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, device_id, sport, sub_sport, name) DO UPDATE SET zones = excluded.zones, recorded_at = excluded.recorded_at,
					raw_file_id = excluded.raw_file_id WHERE excluded.recorded_at >= sport_profiles.recorded_at`,
				f.UserID, f.DeviceID, p.Sport, p.SubSport, p.Name, zones, at, f.ID); err != nil {
				return err
			}
		}
		for _, g := range w.Goals {
			if _, err := tx.ExecContext(ctx, `INSERT INTO watch_goals (user_id, device_id, type, sport, target, unit, source, recurrence, recorded_at, raw_file_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, device_id, type, sport) DO UPDATE SET target = excluded.target, unit = excluded.unit, source = excluded.source,
					recurrence = excluded.recurrence, recorded_at = excluded.recorded_at, raw_file_id = excluded.raw_file_id
				WHERE excluded.recorded_at >= watch_goals.recorded_at`,
				f.UserID, f.DeviceID, g.Type, g.Sport, g.Target, g.Unit, g.Source, g.Recurrence, at, f.ID); err != nil {
				return err
			}
		}
		for _, t := range w.Totals {
			if _, err := tx.ExecContext(ctx, `INSERT INTO lifetime_totals (user_id, device_id, sport, timer_s, elapsed_s, active_s, distance_m, kcal, sessions, recorded_at, raw_file_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, device_id, sport) DO UPDATE SET timer_s = excluded.timer_s, elapsed_s = excluded.elapsed_s, active_s = excluded.active_s,
					distance_m = excluded.distance_m, kcal = excluded.kcal, sessions = excluded.sessions, recorded_at = excluded.recorded_at,
					raw_file_id = excluded.raw_file_id WHERE excluded.recorded_at >= lifetime_totals.recorded_at`,
				f.UserID, f.DeviceID, t.Sport, t.TimerS, nullNeg64(t.ElapsedS), nullNeg64(t.ActiveS), t.DistanceM, t.Calories, t.Sessions, at, f.ID); err != nil {
				return err
			}
		}
		if wk := w.Workout; wk != nil && wk.Name != "" {
			steps, err := js(wk.Steps)
			if err != nil {
				return err
			}
			entry, err := entryKeyOf(ctx, tx, f.UserID, wk.Name, wk.Sport)
			if err != nil {
				return err
			}
			extra, err := extraJSON(wk.Extra)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO workouts (user_id, origin, device_id, entry_key, name, sport, sub_sport, steps, estimated_s,
				description, extra, recorded_at, raw_file_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, origin, device_id, name, sport) DO UPDATE SET sub_sport = excluded.sub_sport, steps = excluded.steps,
					estimated_s = excluded.estimated_s, description = excluded.description, extra = excluded.extra,
					recorded_at = excluded.recorded_at, raw_file_id = excluded.raw_file_id,
					entry_key = CASE WHEN excluded.entry_key <> '' THEN excluded.entry_key ELSE workouts.entry_key END
				WHERE excluded.recorded_at >= workouts.recorded_at`,
				f.UserID, OriginWatch, f.DeviceID, entry, wk.Name, wk.Sport, wk.SubSport, steps, wk.EstimatedS, wk.Description, extra, at, f.ID); err != nil {
				return err
			}
			// Every version is named, also one a newer file replaces above: the watch may keep it.
			if _, err := tx.ExecContext(ctx, `INSERT INTO workout_files (raw_file_id, user_id, name, sport) VALUES (?, ?, ?, ?)`,
				f.ID, f.UserID, wk.Name, wk.Sport); err != nil {
				return err
			}
		}
		if c := w.Course; c != nil && c.Name != "" {
			points, err := js(c.Points)
			if err != nil {
				return err
			}
			var b [4]any
			if c.Bounds != nil {
				b = [4]any{c.Bounds.MinLat, c.Bounds.MinLon, c.Bounds.MaxLat, c.Bounds.MaxLon}
			}
			res, err := tx.ExecContext(ctx, `INSERT INTO courses (user_id, device_id, name, sport, distance_m, ascent_m, descent_m,
				min_lat, min_lon, max_lat, max_lon, polyline, points, recorded_at, raw_file_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, device_id, name) DO UPDATE SET sport = excluded.sport, distance_m = excluded.distance_m,
					ascent_m = excluded.ascent_m, descent_m = excluded.descent_m, min_lat = excluded.min_lat, min_lon = excluded.min_lon,
					max_lat = excluded.max_lat, max_lon = excluded.max_lon, polyline = excluded.polyline, points = excluded.points,
					recorded_at = excluded.recorded_at, raw_file_id = excluded.raw_file_id
				WHERE excluded.recorded_at >= courses.recorded_at`,
				f.UserID, f.DeviceID, c.Name, c.Sport, c.DistanceM, c.AscentM, c.DescentM, b[0], b[1], b[2], b[3], c.Polyline, points, at, f.ID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return nil // an older version of a course already stored from a newer file
			}
			var id int64
			if err := tx.QueryRowContext(ctx, `SELECT id FROM courses WHERE user_id = ? AND device_id = ? AND name = ?`, f.UserID, f.DeviceID, c.Name).Scan(&id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM course_streams WHERE course_id = ?`, id); err != nil {
				return err
			}
			if c.Track.Len() > 0 {
				data, fields, err := c.Track.Columns()
				if err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO course_streams (course_id, points, fields, data) VALUES (?, ?, ?, ?)`,
					id, c.Track.Len(), strings.Join(fields, ","), data); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func nullNeg64(v int64) any {
	if v < 0 {
		return nil
	}
	return v
}

// WatchSettings is a watch's stored settings.
type WatchSettings struct {
	DeviceID   int64
	RecordedAt time.Time
	Settings   fitwatch.Settings
}

// LatestWatchSettings returns user's newest watch settings (from whichever watch recorded
// them last), or ErrNotFound.
func (s *Store) LatestWatchSettings(ctx context.Context, user int64) (WatchSettings, error) {
	var out WatchSettings
	var data string
	var at int64
	err := s.db.R.QueryRowContext(ctx, `SELECT device_id, data, recorded_at FROM watch_settings WHERE user_id = ?
		ORDER BY recorded_at DESC LIMIT 1`, user).Scan(&out.DeviceID, &data, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	out.RecordedAt = time.Unix(at, 0)
	return out, json.Unmarshal([]byte(data), &out.Settings)
}

// SportProfileRow is a stored sport profile.
type SportProfileRow struct {
	DeviceID   int64
	RecordedAt time.Time
	fitwatch.SportProfile
}

// SportProfiles returns user's sport profiles, newest first.
func (s *Store) SportProfiles(ctx context.Context, user int64) ([]SportProfileRow, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT device_id, sport, sub_sport, name, zones, recorded_at FROM sport_profiles
		WHERE user_id = ? ORDER BY recorded_at DESC, sport, sub_sport, name`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SportProfileRow
	for rows.Next() {
		var r SportProfileRow
		var zones string
		var at int64
		if err := rows.Scan(&r.DeviceID, &r.Sport, &r.SubSport, &r.Name, &zones, &at); err != nil {
			return nil, err
		}
		r.RecordedAt = time.Unix(at, 0)
		if err := json.Unmarshal([]byte(zones), &r.Zones); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// WatchGoals returns user's goals as the watches last set them: per type and sport the newest.
func (s *Store) WatchGoals(ctx context.Context, user int64) ([]fitwatch.Goal, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT type, sport, target, unit, source, recurrence FROM watch_goals g WHERE user_id = ?
		AND recorded_at = (SELECT MAX(recorded_at) FROM watch_goals x WHERE x.user_id = g.user_id AND x.type = g.type AND x.sport = g.sport)
		GROUP BY type, sport ORDER BY type, sport`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fitwatch.Goal
	for rows.Next() {
		var g fitwatch.Goal
		if err := rows.Scan(&g.Type, &g.Sport, &g.Target, &g.Unit, &g.Source, &g.Recurrence); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// TotalsRow is a sport's lifetime totals on one watch.
type TotalsRow struct {
	DeviceID   int64
	RecordedAt time.Time
	fitwatch.Totals
}

// LifetimeTotals returns user's lifetime totals per watch and sport.
func (s *Store) LifetimeTotals(ctx context.Context, user int64) ([]TotalsRow, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT device_id, sport, timer_s, COALESCE(elapsed_s, -1), COALESCE(active_s, -1), distance_m, kcal,
		sessions, recorded_at FROM lifetime_totals WHERE user_id = ? ORDER BY sessions DESC, sport`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TotalsRow
	for rows.Next() {
		var r TotalsRow
		var at int64
		if err := rows.Scan(&r.DeviceID, &r.Sport, &r.TimerS, &r.ElapsedS, &r.ActiveS, &r.DistanceM, &r.Calories, &r.Sessions, &at); err != nil {
			return nil, err
		}
		r.RecordedAt = time.Unix(at, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Where a stored workout came from.
const (
	OriginWatch         = "watch"          // a watch's workout file
	OriginGarminConnect = "garmin_connect" // a Garmin Connect export's workout list
	OriginGoarmin       = "goarmin"        // renamed or edited in goarmin (no file)
)

// WorkoutRow is a stored workout (its description is the Workout's).
type WorkoutRow struct {
	ID         int64
	Origin     string // OriginWatch, OriginGarminConnect, OriginGoarmin
	DeviceID   int64  // the watch it came from (0: unknown, Garmin Connect or goarmin)
	RecordedAt time.Time
	// EntryKey is the library entry the version belongs to: the key of its name and sport
	// (WorkoutKey), or of the entry a renamed workout began in.
	EntryKey string
	// RawFileID is the file a watch's or Garmin Connect's version was read from (0: goarmin's
	// own); FileSHA the sha of the file a watch gets of it: its file's, or the file goarmin's
	// own is written as ("" until it was).
	RawFileID int64
	FileSHA   string
	fitwatch.Workout
}

// extraJSON is a workout's kept file fields as stored ("" for none).
func extraJSON(x *fitwatch.WorkoutExtra) (string, error) {
	if x == nil {
		return "", nil
	}
	b, err := json.Marshal(x)
	return string(b), err
}

// entryKeyOf is the library entry a new version of a workout named name in sport joins: the
// entry of user's goarmin version of that name and sport (a renamed workout, handed back by a
// watch that goarmin sent it to), or "" (its own name's key).
func entryKeyOf(ctx context.Context, tx *sql.Tx, user int64, name, sport string) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT entry_key, name FROM workouts WHERE user_id = ? AND origin = ? AND sport = ?`,
		user, OriginGoarmin, sport)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	want := WorkoutKey(name, sport)
	for rows.Next() {
		var entry, n string
		if err := rows.Scan(&entry, &n); err != nil {
			return "", err
		}
		if WorkoutKey(n, sport) == want {
			return entry, nil
		}
	}
	return "", rows.Err()
}

// Workouts returns user's stored workouts by name, every version of each; with id > 0 only that
// one (or ErrNotFound).
func (s *Store) Workouts(ctx context.Context, user, id int64) ([]WorkoutRow, error) {
	q := `SELECT id, origin, device_id, description, entry_key, name, sport, sub_sport, steps, estimated_s, recorded_at, COALESCE(raw_file_id, 0),
		extra, CASE WHEN origin = 'watch' THEN COALESCE((SELECT r.sha256 FROM raw_files r WHERE r.id = raw_file_id), '') ELSE file_sha256 END
		FROM workouts WHERE user_id = ?`
	args := []any{user}
	if id > 0 {
		q += ` AND id = ?`
		args = append(args, id)
	}
	rows, err := s.db.R.QueryContext(ctx, q+` ORDER BY name COLLATE NOCASE, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkoutRow
	for rows.Next() {
		var r WorkoutRow
		var steps, extra string
		var at int64
		if err := rows.Scan(&r.ID, &r.Origin, &r.DeviceID, &r.Description, &r.EntryKey, &r.Name, &r.Sport, &r.SubSport, &steps, &r.EstimatedS, &at, &r.RawFileID,
			&extra, &r.FileSHA); err != nil {
			return nil, err
		}
		if extra != "" {
			r.Extra = &fitwatch.WorkoutExtra{}
			if err := json.Unmarshal([]byte(extra), r.Extra); err != nil {
				return nil, err
			}
		}
		r.RecordedAt = time.Unix(at, 0)
		if r.EntryKey == "" {
			r.EntryKey = WorkoutKey(r.Name, r.Sport)
		}
		if err := json.Unmarshal([]byte(steps), &r.Steps); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if id > 0 && len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

// CourseRow is a stored course.
type CourseRow struct {
	ID         int64
	DeviceID   int64
	RecordedAt time.Time
	RawFileID  int64
	Name       string
	Sport      string
	DistanceM  float64
	AscentM    float64
	DescentM   float64
	Bounds     *[4]float64
	Polyline   string
	Points     []fitwatch.CoursePoint
}

// Courses returns user's saved courses by name; with id > 0 only that one (or ErrNotFound).
func (s *Store) Courses(ctx context.Context, user, id int64) ([]CourseRow, error) {
	q := `SELECT id, device_id, name, sport, distance_m, ascent_m, descent_m, min_lat, min_lon, max_lat, max_lon, polyline, points,
		recorded_at, raw_file_id FROM courses WHERE user_id = ?`
	args := []any{user}
	if id > 0 {
		q += ` AND id = ?`
		args = append(args, id)
	}
	rows, err := s.db.R.QueryContext(ctx, q+` ORDER BY name COLLATE NOCASE`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CourseRow
	for rows.Next() {
		var r CourseRow
		var b [4]sql.NullFloat64
		var points string
		var at int64
		if err := rows.Scan(&r.ID, &r.DeviceID, &r.Name, &r.Sport, &r.DistanceM, &r.AscentM, &r.DescentM, &b[0], &b[1], &b[2], &b[3],
			&r.Polyline, &points, &at, &r.RawFileID); err != nil {
			return nil, err
		}
		if b[0].Valid {
			r.Bounds = &[4]float64{b[0].Float64, b[1].Float64, b[2].Float64, b[3].Float64}
		}
		r.RecordedAt = time.Unix(at, 0)
		if err := json.Unmarshal([]byte(points), &r.Points); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if id > 0 && len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

// CourseStream returns one of user's courses' track: its fields and the gzipped JSON columns.
func (s *Store) CourseStream(ctx context.Context, user, id int64) (fields []string, data []byte, err error) {
	var f string
	err = s.db.R.QueryRowContext(ctx, `SELECT st.fields, st.data FROM course_streams st JOIN courses c ON c.id = st.course_id
		WHERE c.user_id = ? AND c.id = ?`, user, id).Scan(&f, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	return strings.Split(f, ","), data, err
}

// ActivityZones returns the watch's own time in zones of one of user's activities: the session's
// under -1, each lap's under its index, as stored JSON (nil map if the file had none).
func (s *Store) ActivityZones(ctx context.Context, user, id int64) (map[int]json.RawMessage, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT z.lap, z.data FROM time_in_zone z JOIN activities a ON a.id = z.activity_id
		WHERE a.user_id = ? AND a.id = ?`, user, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out map[int]json.RawMessage
	for rows.Next() {
		var lap int
		var data string
		if err := rows.Scan(&lap, &data); err != nil {
			return nil, err
		}
		if out == nil {
			out = map[int]json.RawMessage{}
		}
		out[lap] = json.RawMessage(data)
	}
	return out, rows.Err()
}
