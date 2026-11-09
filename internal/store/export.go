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
	"time"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/pkg/garmin/gcexport"
	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

// Daily values rank by where they come from: the watch's own file over the export's record of
// the same day. Equal ranks: the newer value wins.
const (
	rankJSON = 1
	rankFIT  = 2
)

// writeDaily stores one daily value unless the day already holds a better one.
func writeDaily(ctx context.Context, tx execer, user, file int64, rank int, k wellness.Kind, day string, ts, v int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO daily_values (user_id, kind, day, ts, value, raw_file_id, rank) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, kind, day) DO UPDATE SET value = excluded.value, ts = excluded.ts, raw_file_id = excluded.raw_file_id,
			rank = excluded.rank
		WHERE excluded.rank > daily_values.rank OR (excluded.rank = daily_values.rank AND excluded.ts >= daily_values.ts)`,
		user, k, day, ts, v, file, rank)
	return err
}

// exportTables are what an export file writes, deleted by file before it is written again.
var exportTables = []string{"daily_values", "sleep_parts", "naps", "weigh_ins", "ecg_recordings", "health_status", "hydration_logs",
	"health_snapshots", "gear", "personal_records", "account_documents", "workouts"}

// WriteExportFile replaces what an export file (f, a kept JSON or other file) contributed with
// what gcexport read from it, and records the ingest under handler and version. It returns the
// local days the data touches, for RebuildDays.
func (s *Store) WriteExportFile(ctx context.Context, f RawFile, r *gcexport.Result, handler string, version int) ([]string, error) {
	days := map[string]bool{}
	z, err := s.Zone(ctx, f.UserID)
	if err != nil {
		return nil, err
	}
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		var prior int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ingest_state WHERE raw_file_id = ?`, f.ID).Scan(&prior); err != nil {
			return err
		}
		fresh := prior == 0
		if !fresh {
			rows, err := tx.QueryContext(ctx, `SELECT DISTINCT day FROM daily_values WHERE raw_file_id = ?`, f.ID)
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
			for _, t := range exportTables {
				if t == "sleep_parts" || t == "naps" {
					continue // writeSleep's own
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM `+t+` WHERE raw_file_id = ?`, f.ID); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM activities WHERE raw_file_id = ?`, f.ID); err != nil {
				return err
			}
		}
		var from, to time.Time
		span := func(t time.Time) {
			if t.IsZero() {
				return
			}
			if from.IsZero() || t.Before(from) {
				from = t
			}
			if t.After(to) {
				to = t
			}
		}
		for _, v := range r.Daily {
			days[v.Day] = true
			span(v.At)
			if err := writeDaily(ctx, tx, f.UserID, f.ID, rankJSON, v.Kind, v.Day, v.At.Unix(), v.Value); err != nil {
				return err
			}
		}
		parts := make([]SleepPart, 0, len(r.Sleep))
		for _, sl := range r.Sleep {
			parts = append(parts, SleepPart{Source: SleepJSON, Sleep: sl})
			span(sl.Start)
			span(sl.End)
		}
		if err := writeSleep(ctx, tx, f.UserID, f.ID, parts, r.Naps, z, days, fresh); err != nil {
			return err
		}
		for _, w := range r.WeighIns {
			if err := WriteWeighIn(ctx, tx, f.UserID, f.ID, FromJSON, w); err != nil {
				return err
			}
			days[z.Day(w.At.Unix())] = true
			span(w.At)
		}
		for _, e := range r.ECG {
			samples, err := gzipJSON(map[string][]float32{"raw": e.Raw, "smooth": e.Smooth})
			if err != nil {
				return err
			}
			// The watch's own recording keeps its samples (512 per second); the export only
			// fills in a recording the watch's files lack.
			if _, err := tx.ExecContext(ctx, `INSERT INTO ecg_recordings (user_id, start_ts, day, class, side, avg_hr, rmssd_ms, seconds,
				raw_rate, smooth_rate, samples, source, raw_file_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, start_ts) DO UPDATE SET class = excluded.class, avg_hr = excluded.avg_hr, rmssd_ms = excluded.rmssd_ms,
					samples = excluded.samples, raw_file_id = excluded.raw_file_id WHERE ecg_recordings.source = 'json'`,
				f.UserID, e.Start.Unix(), z.Day(e.Start.Unix()), nullUnknown(e.Class), nullUnknown(e.Side), unknownF(e.AvgHR), nullUnknown(e.RMSSD),
				nullUnknown(e.Seconds), unknownF(e.RawRate), unknownF(e.SmoothRate), samples, FromJSON, f.ID); err != nil {
				return err
			}
			span(e.Start)
		}
		for _, h := range r.HealthStatus {
			if _, err := tx.ExecContext(ctx, `INSERT INTO health_status (user_id, day, metric, value, low, high, pct, status, raw_file_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, day, metric) DO UPDATE SET value = excluded.value, low = excluded.low, high = excluded.high,
					pct = excluded.pct, status = excluded.status, raw_file_id = excluded.raw_file_id`,
				f.UserID, h.Day, h.Metric, nanNull(h.Value), nanNull(h.Low), nanNull(h.High), nanNull(h.Pct), h.Status, f.ID); err != nil {
				return err
			}
		}
		for _, h := range r.Hydration {
			if _, err := tx.ExecContext(ctx, `INSERT INTO hydration_logs (user_id, ts, day, ml, sweat_ml, source, activity_id, raw_file_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, f.UserID, h.At.Unix(), h.Day, h.ML, h.SweatML, h.Source, nullIf0(h.Activity), f.ID); err != nil {
				return err
			}
		}
		for _, sn := range r.Snapshots {
			data, err := json.Marshal(sn.Data)
			if err != nil {
				return err
			}
			day := sn.Day
			if day == "" {
				day = z.Day(sn.Start.Unix())
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO health_snapshots (user_id, start_ts, end_ts, day, name, data, raw_file_id)
				VALUES (?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, start_ts) DO UPDATE SET end_ts = excluded.end_ts, day = excluded.day, name = excluded.name,
					data = excluded.data, raw_file_id = excluded.raw_file_id`,
				f.UserID, sn.Start.Unix(), sn.End.Unix(), day, sn.Name, string(data), f.ID); err != nil {
				return err
			}
		}
		for _, a := range r.Activities {
			if err := writeExportActivity(ctx, tx, f, a, z, days); err != nil {
				return err
			}
			span(a.Start)
		}
		for _, g := range r.Gear {
			data, err := json.Marshal(map[string]any{"status": g.Status, "begin": g.Begin, "max_meters": g.MaxMeters, "activities": g.Activities,
				"default_for": g.DefaultFor})
			if err != nil {
				return err
			}
			id := g.UUID
			if id == "" {
				id = "name:" + g.Name
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO gear (user_id, uuid, source, type, name, brand, model, data, raw_file_id)
				VALUES (?, ?, ?, ?, ?, '', ?, ?, ?)
				ON CONFLICT (user_id, uuid, source) DO UPDATE SET type = excluded.type, name = excluded.name, model = excluded.model,
					data = excluded.data, raw_file_id = excluded.raw_file_id`,
				f.UserID, id, FromJSON, g.Type, g.Name, g.Model, string(data), f.ID); err != nil {
				return err
			}
		}
		for _, p := range r.Records {
			data, err := json.Marshal(map[string]any{"current": p.Current})
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO personal_records (user_id, source, metric, sport, ts, value, activity_id, data, raw_file_id)
				VALUES (?, ?, ?, '', ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, source, metric, sport) DO UPDATE SET ts = excluded.ts, value = excluded.value,
					activity_id = excluded.activity_id, data = excluded.data, raw_file_id = excluded.raw_file_id
				WHERE excluded.ts >= personal_records.ts`,
				f.UserID, FromJSON, p.Type, p.At.Unix(), nanNull(p.Value), nullIf0(p.Activity), string(data), f.ID); err != nil {
				return err
			}
		}
		for _, w := range r.Workouts {
			steps, err := json.Marshal(w.Steps)
			if err != nil {
				return err
			}
			at := w.Updated
			if at.IsZero() {
				at = f.ReceivedAt
			}
			entry, err := entryKeyOf(ctx, tx, f.UserID, w.Name, w.Sport)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO workouts (user_id, origin, device_id, entry_key, name, sport, sub_sport, steps, estimated_s,
				description, recorded_at, raw_file_id) VALUES (?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, origin, device_id, name, sport) DO UPDATE SET sub_sport = excluded.sub_sport, steps = excluded.steps,
					estimated_s = excluded.estimated_s, description = excluded.description, recorded_at = excluded.recorded_at,
					raw_file_id = excluded.raw_file_id,
					entry_key = CASE WHEN excluded.entry_key <> '' THEN excluded.entry_key ELSE workouts.entry_key END
				WHERE excluded.recorded_at >= workouts.recorded_at`,
				f.UserID, OriginGarminConnect, entry, w.Name, w.Sport, w.SubSport, string(steps), w.EstimatedS, w.Description, at.Unix(), f.ID); err != nil {
				return err
			}
		}
		if r.Document != nil {
			data, err := json.Marshal(r.Document)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO account_documents (raw_file_id, user_id, kind, name, data, recorded_at)
				VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (raw_file_id) DO UPDATE SET kind = excluded.kind, name = excluded.name, data = excluded.data,
					recorded_at = excluded.recorded_at`, f.ID, f.UserID, r.Kind, f.SourceName, string(data), f.ReceivedAt.Unix()); err != nil {
				return err
			}
		}
		return recordIngest(ctx, tx, f.ID, handler, version, "ok", "", from, to, s.now())
	})
	return sortedDays(days), err
}

func nanNull(v float64) any {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return v
}

// connectSports maps Garmin Connect's activity types to FIT sport and sub sport.
var connectSports = map[string][2]int{
	"running": {int(typedef.SportRunning), 0}, "trail_running": {int(typedef.SportRunning), int(typedef.SubSportTrail)},
	"treadmill_running": {int(typedef.SportRunning), int(typedef.SubSportTreadmill)}, "walking": {int(typedef.SportWalking), 0},
	"hiking": {int(typedef.SportHiking), 0}, "cycling": {int(typedef.SportCycling), 0}, "road_biking": {int(typedef.SportCycling), int(typedef.SubSportRoad)},
	"mountain_biking": {int(typedef.SportCycling), int(typedef.SubSportMountain)}, "indoor_cycling": {int(typedef.SportCycling), int(typedef.SubSportIndoorCycling)},
	"lap_swimming": {int(typedef.SportSwimming), int(typedef.SubSportLapSwimming)}, "open_water_swimming": {int(typedef.SportSwimming), int(typedef.SubSportOpenWater)},
	"yoga": {int(typedef.SportTraining), int(typedef.SubSportYoga)}, "pilates": {int(typedef.SportTraining), int(typedef.SubSportPilates)},
	"indoor_cardio": {int(typedef.SportTraining), int(typedef.SubSportCardioTraining)}, "strength_training": {int(typedef.SportTraining), int(typedef.SubSportStrengthTraining)},
	"breathwork": {int(typedef.SportTraining), int(typedef.SubSportBreathing)},
}

// writeExportActivity stores an activity from Garmin Connect's summaries: on an activity a watch's
// file already gave, only what Garmin Connect knows beyond it (extra); else as an activity of its
// own, without a track, which a watch's file arriving later replaces.
func writeExportActivity(ctx context.Context, tx *sql.Tx, f RawFile, a gcexport.Activity, z Zone, days map[string]bool) error {
	extra, err := json.Marshal(a.Extra)
	if err != nil {
		return err
	}
	start := a.Start.Unix()
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM activities WHERE user_id = ? AND start_ts BETWEEN ? AND ? ORDER BY abs(start_ts - ?) LIMIT 1`,
		f.UserID, start-60, start+60, start).Scan(&id)
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE activities SET extra = ? WHERE id = ?`, string(extra), id)
		return err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	sport := connectSports[a.Type]
	if _, ok := connectSports[a.Type]; !ok {
		sport = [2]int{int(typedef.SportGeneric), 0}
	}
	day := z.Day(start)
	days[day] = true
	pos := func(v float64) any {
		if v <= 0 {
			return nil
		}
		return v
	}
	posI := func(v int) any {
		if v <= 0 {
			return nil
		}
		return v
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO activities (user_id, raw_file_id, session_index, start_ts, end_ts, day, sport, sub_sport, name,
		elapsed_s, timer_s, distance_m, kcal, avg_hr, max_hr, avg_speed, max_speed, ascent_m, descent_m, training_effect, anaerobic_effect,
		training_load, polyline, extra) VALUES (?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?)
		ON CONFLICT (user_id, start_ts, session_index) DO NOTHING`,
		f.UserID, f.ID, start, start+int64(math.Max(a.Elapsed, a.Duration)), day, sport[0], sport[1], a.Name, pos(a.Elapsed), pos(a.Duration),
		pos(a.Distance), posI(a.Kcal), posI(a.AvgHR), posI(a.MaxHR), pos(a.AvgSpeed), pos(a.MaxSpeed), pos(a.Ascent), pos(a.Descent),
		pos(a.AerobicTE), pos(a.AnaerobicTE), pos(a.TrainingLoad), string(extra))
	return err
}

// AccountDocument is one of the account's records from an export.
type AccountDocument struct {
	FileID     int64
	Kind, Name string
	RecordedAt int64
	Data       json.RawMessage
}

// AccountDocuments lists user's account documents (without their data, which AccountDocument
// gives).
func (s *Store) AccountDocuments(ctx context.Context, user int64) ([]AccountDocument, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT raw_file_id, kind, name, recorded_at, length(data) FROM account_documents
		WHERE user_id = ? ORDER BY kind, name`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountDocument
	for rows.Next() {
		var d AccountDocument
		var n int
		if err := rows.Scan(&d.FileID, &d.Kind, &d.Name, &d.RecordedAt, &n); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AccountDocument returns one of user's account documents with its data.
func (s *Store) AccountDocument(ctx context.Context, user, file int64) (AccountDocument, error) {
	var d AccountDocument
	var data string
	err := s.db.R.QueryRowContext(ctx, `SELECT raw_file_id, kind, name, recorded_at, data FROM account_documents WHERE user_id = ? AND raw_file_id = ?`,
		user, file).Scan(&d.FileID, &d.Kind, &d.Name, &d.RecordedAt, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	d.Data = json.RawMessage(data)
	return d, err
}

// HealthStatusDay is a day's health status of one metric.
type HealthStatusDay struct {
	Day, Metric, Status   string
	Value, Low, High, Pct *float64
}

// HealthStatus returns user's health status on days in [from, to].
func (s *Store) HealthStatus(ctx context.Context, user int64, from, to string) ([]HealthStatusDay, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT day, metric, status, value, low, high, pct FROM health_status
		WHERE user_id = ? AND day >= ? AND day <= ? ORDER BY day, metric`, user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HealthStatusDay
	for rows.Next() {
		var h HealthStatusDay
		if err := rows.Scan(&h.Day, &h.Metric, &h.Status, &h.Value, &h.Low, &h.High, &h.Pct); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// HydrationLog is one logged drink.
type HydrationLog struct {
	TS          int64
	Day, Source string
	ML, SweatML float64
}

// HydrationLogs returns user's drinks on days in [from, to].
func (s *Store) HydrationLogs(ctx context.Context, user int64, from, to string) ([]HydrationLog, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT ts, day, source, ml, sweat_ml FROM hydration_logs WHERE user_id = ? AND day >= ? AND day <= ?
		ORDER BY ts`, user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HydrationLog
	for rows.Next() {
		var h HydrationLog
		if err := rows.Scan(&h.TS, &h.Day, &h.Source, &h.ML, &h.SweatML); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// HealthSnapshot is a stored health snapshot.
type HealthSnapshot struct {
	Start, End int64
	Day, Name  string
	Data       json.RawMessage
}

// HealthSnapshots returns user's health snapshots on days in [from, to].
func (s *Store) HealthSnapshots(ctx context.Context, user int64, from, to string) ([]HealthSnapshot, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT start_ts, end_ts, day, name, data FROM health_snapshots WHERE user_id = ? AND day >= ? AND day <= ?
		ORDER BY start_ts`, user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HealthSnapshot
	for rows.Next() {
		var h HealthSnapshot
		var data string
		if err := rows.Scan(&h.Start, &h.End, &h.Day, &h.Name, &data); err != nil {
			return nil, err
		}
		h.Data = json.RawMessage(data)
		out = append(out, h)
	}
	return out, rows.Err()
}

// PendingExportFiles lists user-independent: every kept export file (source gc_export, not FIT)
// not yet read by handler at version (or later).
func (s *Store) PendingExportFiles(ctx context.Context, handler string, version int) ([]int64, error) {
	rows, err := s.db.W.QueryContext(ctx, `SELECT f.id FROM raw_files f
		LEFT JOIN ingest_state i ON i.raw_file_id = f.id AND i.handler = ?
		WHERE f.source = 'gc_export' AND f.fit_type IS NULL AND (i.version IS NULL OR i.version < ?) ORDER BY f.id`, handler, version)
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

// ConnectActivity is one of user's activities known to Garmin Connect (from an export).
type ConnectActivity struct {
	ID       int64 // goarmin's
	Distance float64
}

// ConnectActivities maps the Garmin Connect ids of user's activities (from an export) to them:
// gear and personal records name activities by those ids.
func (s *Store) ConnectActivities(ctx context.Context, user int64) (map[int64]ConnectActivity, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT CAST(json_extract(extra, '$.activityId') AS INTEGER), id, COALESCE(distance_m, 0) FROM activities
		WHERE user_id = ? AND json_extract(extra, '$.activityId') IS NOT NULL`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]ConnectActivity{}
	for rows.Next() {
		var cid int64
		var a ConnectActivity
		if err := rows.Scan(&cid, &a.ID, &a.Distance); err != nil {
			return nil, err
		}
		out[cid] = a
	}
	return out, rows.Err()
}
