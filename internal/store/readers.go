// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/pkg/fitwatch"
	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

// Sources of rows a FIT file and an export's JSON both fill: the watch's own file wins.
const (
	FromFIT  = "fit"
	FromJSON = "json"
)

// unknownF is NULL for wellness.Unknown (and NaN).
func unknownF(v float64) any {
	if v == wellness.Unknown || math.IsNaN(v) {
		return nil
	}
	return v
}

// writeHealth writes what the health readers found in a file: weigh-ins, nightly skin
// temperature, ECG recordings and physiological metrics. The caller deleted the file's rows.
func writeHealth(ctx context.Context, tx *sql.Tx, f RawFile, d *wellness.Data, z Zone, days map[string]bool) error {
	for _, w := range d.WeighIns {
		if err := WriteWeighIn(ctx, tx, f.UserID, f.ID, FromFIT, w); err != nil {
			return err
		}
		days[z.Day(w.At.Unix())] = true
	}
	for _, n := range d.SkinTemp {
		day := n.Day
		if day == "" {
			day = z.Day(n.At.Unix())
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO skin_temp_nightly (user_id, day, ts, nightly_c, deviation_c, deviation7d_c, calibrated_days, raw_file_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, day) DO UPDATE SET ts = excluded.ts, nightly_c = excluded.nightly_c, deviation_c = excluded.deviation_c,
				deviation7d_c = excluded.deviation7d_c, calibrated_days = excluded.calibrated_days, raw_file_id = excluded.raw_file_id
			WHERE excluded.ts >= skin_temp_nightly.ts`,
			f.UserID, day, n.At.Unix(), unknownF(n.Nightly), unknownF(n.Deviation), unknownF(n.Deviation7d), nullUnknown(n.CalibratedDays), f.ID); err != nil {
			return err
		}
	}
	for _, e := range d.ECG {
		samples, err := gzipJSON(map[string][]float32{"raw": e.Raw, "smooth": e.Smooth})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO ecg_recordings (user_id, start_ts, day, class, side, avg_hr, rmssd_ms, seconds, raw_rate,
			smooth_rate, samples, source, raw_file_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, start_ts) DO UPDATE SET class = excluded.class, side = excluded.side, avg_hr = excluded.avg_hr,
				rmssd_ms = excluded.rmssd_ms, seconds = excluded.seconds, raw_rate = excluded.raw_rate, smooth_rate = excluded.smooth_rate,
				samples = excluded.samples, source = excluded.source, raw_file_id = excluded.raw_file_id`,
			f.UserID, e.Start.Unix(), z.Day(e.Start.Unix()), nullUnknown(e.Class), nullUnknown(e.Side), unknownF(e.AvgHR), nullUnknown(e.RMSSD),
			nullUnknown(e.Seconds), unknownF(e.RawRate), unknownF(e.SmoothRate), samples, FromFIT, f.ID); err != nil {
			return err
		}
		days[z.Day(e.Start.Unix())] = true
	}
	for _, p := range d.Physio {
		if _, err := tx.ExecContext(ctx, `INSERT INTO physiology (user_id, ts, sport, vo2max, lthr, lt_power, lt_speed_kmh, max_hr, avg_hr,
			recovery_min, aerobic_te, anaerobic_te, duration_s, distance_m, race_5k_s, race_10k_s, race_half_s, race_marathon_s, raw_file_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, ts) DO UPDATE SET sport = excluded.sport, vo2max = excluded.vo2max, lthr = excluded.lthr,
				lt_power = excluded.lt_power, lt_speed_kmh = excluded.lt_speed_kmh, max_hr = excluded.max_hr, avg_hr = excluded.avg_hr,
				recovery_min = excluded.recovery_min, aerobic_te = excluded.aerobic_te, anaerobic_te = excluded.anaerobic_te,
				duration_s = excluded.duration_s, distance_m = excluded.distance_m, race_5k_s = excluded.race_5k_s,
				race_10k_s = excluded.race_10k_s, race_half_s = excluded.race_half_s, race_marathon_s = excluded.race_marathon_s,
				raw_file_id = excluded.raw_file_id`,
			f.UserID, p.At.Unix(), sportWord(p.Sport), positive(p.VO2Max), positiveI(p.LTHR), positiveI(p.LTPower), positive(p.LTSpeed),
			positiveI(p.MaxHR), positiveI(p.AvgHR), nullUnknown(p.RecoveryMin), unknownF(p.AerobicTE), unknownF(p.AnaerobicTE),
			positive(p.Duration), positive(p.Distance), positiveI(p.Race5K), positiveI(p.Race10K), positiveI(p.RaceHalf),
			positiveI(p.RaceMarathon), f.ID); err != nil {
			return err
		}
	}
	return nil
}

func positive(v float64) any {
	if v <= 0 || v == wellness.Unknown || math.IsNaN(v) {
		return nil
	}
	return v
}

func positiveI(v int) any {
	if v <= 0 {
		return nil
	}
	return v
}

// sportWord is a FIT sport number as a word ("running"; "" if unknown).
func sportWord(n int) string {
	if n < 0 || n >= 255 {
		return ""
	}
	if w := typedef.Sport(n).String(); !strings.Contains(w, "Invalid") && !strings.HasPrefix(w, "Sport") {
		return w
	}
	return strconv.Itoa(n)
}

// execer is a transaction or a connection.
type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// WriteWeighIn stores one weighing for user from file (source FromFIT or FromJSON). At the same
// second a scale's own file wins over the export's record of it.
func WriteWeighIn(ctx context.Context, tx execer, user, file int64, source string, w wellness.WeighIn) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO weigh_ins (user_id, ts, weight_kg, fat_pct, hydration_pct, bone_kg, muscle_kg, visceral_fat_kg,
		bmi, bmr_kcal, active_mr_kcal, metabolic_age, visceral_rating, build, source, raw_file_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, ts) DO UPDATE SET weight_kg = excluded.weight_kg, fat_pct = COALESCE(excluded.fat_pct, fat_pct),
			hydration_pct = COALESCE(excluded.hydration_pct, hydration_pct), bone_kg = COALESCE(excluded.bone_kg, bone_kg),
			muscle_kg = COALESCE(excluded.muscle_kg, muscle_kg), visceral_fat_kg = COALESCE(excluded.visceral_fat_kg, visceral_fat_kg),
			bmi = COALESCE(excluded.bmi, bmi), bmr_kcal = COALESCE(excluded.bmr_kcal, bmr_kcal),
			active_mr_kcal = COALESCE(excluded.active_mr_kcal, active_mr_kcal), metabolic_age = COALESCE(excluded.metabolic_age, metabolic_age),
			visceral_rating = COALESCE(excluded.visceral_rating, visceral_rating), build = COALESCE(excluded.build, build),
			source = excluded.source, raw_file_id = excluded.raw_file_id
		WHERE excluded.source = 'fit' OR weigh_ins.source = 'json'`,
		user, w.At.Unix(), w.Weight, unknownF(w.Fat), unknownF(w.Hydration), unknownF(w.Bone), unknownF(w.Muscle), unknownF(w.VisceralFat),
		unknownF(w.BMI), nullUnknown(w.BMR), nullUnknown(w.ActiveMR), nullUnknown(w.MetabolicAge), nullUnknown(w.VisceralRating),
		nullUnknown(w.Build), source, file)
	return err
}

func gzipJSON(v any) ([]byte, error) {
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if err := json.NewEncoder(zw).Encode(v); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// solarBucket is how long a device_series bucket of solar charge lasts: the watch logs every
// minute; an hour shows the day's sun and keeps the table small.
const solarBucket = 3600

// writeDeviceData writes what a device-data file held: gear, golf clubs, personal records, the
// settings change log, solar charge (hourly averages) and documents. The caller deleted the
// file's rows.
func writeDeviceData(ctx context.Context, tx *sql.Tx, f RawFile, w *fitwatch.File, at int64) error {
	for _, g := range w.Gear {
		id := g.UUID
		if id == "" {
			id = "name:" + g.Name
		}
		data, err := json.Marshal(g)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO gear (user_id, uuid, source, type, name, brand, model, data, raw_file_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, uuid, source) DO UPDATE SET type = excluded.type, name = excluded.name, brand = excluded.brand,
				model = excluded.model, data = excluded.data, raw_file_id = excluded.raw_file_id`,
			f.UserID, id, FromFIT, strconv.FormatInt(g.Type, 10), g.Name, g.Brand, g.Model, string(data), f.ID); err != nil {
			return err
		}
	}
	for _, c := range w.Clubs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO golf_clubs (user_id, device_id, idx, type, name, avg_distance_m, max_distance_m, raw_file_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, device_id, idx) DO UPDATE SET type = excluded.type, name = excluded.name,
				avg_distance_m = excluded.avg_distance_m, max_distance_m = excluded.max_distance_m, raw_file_id = excluded.raw_file_id`,
			f.UserID, f.DeviceID, c.Index, c.Type, c.Name, positive(c.AvgDistance), positive(c.MaxDistance), f.ID); err != nil {
			return err
		}
	}
	for _, r := range w.Records {
		// A record kind can repeat over distances (the fastest 1 km, 5 km, …): one each.
		metric := strconv.FormatInt(r.Metric, 10)
		if r.Distance > 0 {
			metric += "@" + strconv.FormatFloat(r.Distance, 'f', -1, 64)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO personal_records (user_id, source, metric, sport, ts, value, distance_m, raw_file_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, source, metric, sport) DO UPDATE SET ts = excluded.ts, value = excluded.value,
				distance_m = excluded.distance_m, raw_file_id = excluded.raw_file_id WHERE excluded.ts >= personal_records.ts`,
			f.UserID, FromFIT, metric, r.Sport, r.At.Unix(), r.Value, positive(r.Distance), f.ID); err != nil {
			return err
		}
	}
	for _, c := range w.Changes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings_changes (user_id, device_id, ts, message, field, value, kind, sport, raw_file_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, f.UserID, f.DeviceID, c.At.Unix(), c.Message, c.Field, c.Value, c.Kind, c.Sport, f.ID); err != nil {
			return err
		}
	}
	if len(w.Solar) > 0 {
		type acc struct {
			sum  float64
			n    int
			gain int64
		}
		buckets := map[int64]*acc{}
		for _, p := range w.Solar {
			b := p.At.Unix() / solarBucket * solarBucket
			a := buckets[b]
			if a == nil {
				a = &acc{}
				buckets[b] = a
			}
			a.sum += p.Percent
			a.n++
			a.gain += p.GainMS
		}
		for b, a := range buckets {
			for kind, v := range map[string]float64{"solar": a.sum / float64(a.n), "solar_gain": float64(a.gain)} {
				if _, err := tx.ExecContext(ctx, `INSERT INTO device_series (user_id, device_id, kind, ts, value, raw_file_id) VALUES (?, ?, ?, ?, ?, ?)
					ON CONFLICT (user_id, device_id, kind, ts) DO UPDATE SET value = excluded.value, raw_file_id = excluded.raw_file_id`,
					f.UserID, f.DeviceID, kind, b, math.Round(v*100)/100, f.ID); err != nil {
					return err
				}
			}
		}
	}
	if len(w.Doc) > 0 {
		kind := map[uint8]string{1: "device", 2: "settings", 33: "multisport"}[f.FIT.Type]
		if kind == "" {
			kind = "file_" + strconv.Itoa(int(f.FIT.Type))
		}
		data, err := json.Marshal(w.Doc)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO device_documents (user_id, device_id, kind, data, recorded_at, raw_file_id) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (user_id, device_id, kind) DO UPDATE SET data = excluded.data, recorded_at = excluded.recorded_at,
				raw_file_id = excluded.raw_file_id WHERE excluded.recorded_at >= device_documents.recorded_at`,
			f.UserID, f.DeviceID, kind, string(data), at, f.ID); err != nil {
			return err
		}
	}
	return nil
}

// WeighIn is a stored weighing; nil fields were not measured.
type WeighIn struct {
	TS                                          int64    `json:"ts"`
	Day                                         string   `json:"day"`
	WeightKg                                    float64  `json:"weight_kg"`
	FatPct, HydrationPct, BoneKg, MuscleKg      *float64 `json:"-"`
	VisceralFatKg, BMI                          *float64 `json:"-"`
	BMR, ActiveMR, MetabolicAge, VisceralRating *int64   `json:"-"`
	Source                                      string   `json:"source"`
}

// WeighIns returns user's weighings in [from, to) (Unix seconds), oldest first, with their
// local days.
func (s *Store) WeighIns(ctx context.Context, user, from, to int64) ([]WeighIn, error) {
	z, err := s.Zone(ctx, user)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT ts, weight_kg, fat_pct, hydration_pct, bone_kg, muscle_kg, visceral_fat_kg, bmi, bmr_kcal,
		active_mr_kcal, metabolic_age, visceral_rating, source FROM weigh_ins WHERE user_id = ? AND ts >= ? AND ts < ? ORDER BY ts`, user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WeighIn
	for rows.Next() {
		var w WeighIn
		if err := rows.Scan(&w.TS, &w.WeightKg, &w.FatPct, &w.HydrationPct, &w.BoneKg, &w.MuscleKg, &w.VisceralFatKg, &w.BMI, &w.BMR,
			&w.ActiveMR, &w.MetabolicAge, &w.VisceralRating, &w.Source); err != nil {
			return nil, err
		}
		w.Day = z.Day(w.TS)
		out = append(out, w)
	}
	return out, rows.Err()
}

// SkinTempNight is a stored night of skin temperature.
type SkinTempNight struct {
	Day                                string
	TS                                 int64
	NightlyC, DeviationC, Deviation7dC *float64
	CalibratedDays                     *int64
}

// SkinTempNights returns user's nights of skin temperature on days in [from, to].
func (s *Store) SkinTempNights(ctx context.Context, user int64, from, to string) ([]SkinTempNight, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT day, ts, nightly_c, deviation_c, deviation7d_c, calibrated_days FROM skin_temp_nightly
		WHERE user_id = ? AND day >= ? AND day <= ? ORDER BY day`, user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SkinTempNight
	for rows.Next() {
		var n SkinTempNight
		if err := rows.Scan(&n.Day, &n.TS, &n.NightlyC, &n.DeviationC, &n.Deviation7dC, &n.CalibratedDays); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ECGRecording is a stored ECG; Samples only from ECG.
type ECGRecording struct {
	ID                  int64
	Start               int64
	Day                 string
	Class, Side, RMSSD  *int64
	Seconds             *int64
	AvgHR               *float64
	RawRate, SmoothRate *float64
	Source              string
	Samples             map[string][]float32
}

const ecgCols = `id, start_ts, day, class, side, rmssd_ms, seconds, avg_hr, raw_rate, smooth_rate, source`

func scanECG(row interface{ Scan(...any) error }, extra ...any) (ECGRecording, error) {
	var e ECGRecording
	err := row.Scan(append([]any{&e.ID, &e.Start, &e.Day, &e.Class, &e.Side, &e.RMSSD, &e.Seconds, &e.AvgHR, &e.RawRate, &e.SmoothRate, &e.Source}, extra...)...)
	return e, err
}

// ECGs returns user's ECG recordings, newest first, without their samples.
func (s *Store) ECGs(ctx context.Context, user int64) ([]ECGRecording, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT `+ecgCols+` FROM ecg_recordings WHERE user_id = ? ORDER BY start_ts DESC`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ECGRecording
	for rows.Next() {
		e, err := scanECG(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ECG returns one of user's ECG recordings with its samples.
func (s *Store) ECG(ctx context.Context, user, id int64) (ECGRecording, error) {
	var blob []byte
	e, err := scanECG(s.db.R.QueryRowContext(ctx, `SELECT `+ecgCols+`, samples FROM ecg_recordings WHERE user_id = ? AND id = ?`, user, id), &blob)
	if err == sql.ErrNoRows {
		return e, ErrNotFound
	}
	if err != nil || len(blob) == 0 {
		return e, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return e, err
	}
	defer zr.Close()
	err = json.NewDecoder(zr).Decode(&e.Samples)
	return e, err
}

// Physiology is a stored set of physiological metrics.
type Physiology struct {
	TS                                      int64
	Sport                                   string
	VO2Max, LTSpeedKmh, AerobicTE           *float64
	AnaerobicTE, DurationS, DistanceM       *float64
	LTHR, LTPower, MaxHR, AvgHR, Recovery   *int64
	Race5K, Race10K, RaceHalf, RaceMarathon *int64
}

// Physiologies returns user's physiological metrics in [from, to) (Unix seconds), oldest first.
func (s *Store) Physiologies(ctx context.Context, user, from, to int64) ([]Physiology, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT ts, sport, vo2max, lt_speed_kmh, aerobic_te, anaerobic_te, duration_s, distance_m, lthr,
		lt_power, max_hr, avg_hr, recovery_min, race_5k_s, race_10k_s, race_half_s, race_marathon_s FROM physiology
		WHERE user_id = ? AND ts >= ? AND ts < ? ORDER BY ts`, user, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Physiology
	for rows.Next() {
		var p Physiology
		if err := rows.Scan(&p.TS, &p.Sport, &p.VO2Max, &p.LTSpeedKmh, &p.AerobicTE, &p.AnaerobicTE, &p.DurationS, &p.DistanceM, &p.LTHR,
			&p.LTPower, &p.MaxHR, &p.AvgHR, &p.Recovery, &p.Race5K, &p.Race10K, &p.RaceHalf, &p.RaceMarathon); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GearItem is stored gear (from the watch or the export).
type GearItem struct {
	UUID, Source, Type, Name, Brand, Model string
	Data                                   json.RawMessage
}

// Gear returns user's gear.
func (s *Store) Gear(ctx context.Context, user int64) ([]GearItem, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT uuid, source, type, name, brand, model, data FROM gear WHERE user_id = ? ORDER BY name, source`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GearItem
	for rows.Next() {
		var g GearItem
		var data string
		if err := rows.Scan(&g.UUID, &g.Source, &g.Type, &g.Name, &g.Brand, &g.Model, &data); err != nil {
			return nil, err
		}
		g.Data = json.RawMessage(data)
		out = append(out, g)
	}
	return out, rows.Err()
}

// PersonalRecord is a stored record.
type PersonalRecord struct {
	Source, Metric, Sport string
	TS                    int64
	Value, DistanceM      *float64
	ActivityID            *int64
	Data                  json.RawMessage
}

// PersonalRecords returns user's personal records, newest first.
func (s *Store) PersonalRecords(ctx context.Context, user int64) ([]PersonalRecord, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT source, metric, sport, ts, value, distance_m, activity_id, data FROM personal_records
		WHERE user_id = ? ORDER BY ts DESC`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonalRecord
	for rows.Next() {
		var r PersonalRecord
		var data string
		if err := rows.Scan(&r.Source, &r.Metric, &r.Sport, &r.TS, &r.Value, &r.DistanceM, &r.ActivityID, &data); err != nil {
			return nil, err
		}
		r.Data = json.RawMessage(data)
		out = append(out, r)
	}
	return out, rows.Err()
}

// DevicePoint is one bucket of a device series.
type DevicePoint struct {
	TS    int64
	Value float64
}

// DeviceSeries returns device's series of kind for user in [from, to) (Unix seconds).
func (s *Store) DeviceSeries(ctx context.Context, user, device int64, kind string, from, to int64) ([]DevicePoint, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT ts, value FROM device_series WHERE user_id = ? AND device_id = ? AND kind = ? AND ts >= ? AND ts < ?
		ORDER BY ts`, user, device, kind, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DevicePoint
	for rows.Next() {
		var p DevicePoint
		if err := rows.Scan(&p.TS, &p.Value); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SettingsChange is one entry of a watch's settings change log.
type SettingsChange struct {
	TS                                 int64
	Message, Field, Value, Kind, Sport string
}

// SettingsChanges returns device's settings change log for user, newest first, at most limit
// entries before before (Unix seconds; 0 for the newest).
func (s *Store) SettingsChanges(ctx context.Context, user, device, before int64, limit int) ([]SettingsChange, error) {
	if before <= 0 {
		before = math.MaxInt64
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT ts, message, field, value, kind, sport FROM settings_changes
		WHERE user_id = ? AND device_id = ? AND ts < ? ORDER BY ts DESC LIMIT ?`, user, device, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SettingsChange
	for rows.Next() {
		var c SettingsChange
		if err := rows.Scan(&c.TS, &c.Message, &c.Field, &c.Value, &c.Kind, &c.Sport); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeviceDocument is what a settings, device or multisport file held beyond the tables.
type DeviceDocument struct {
	Kind       string
	RecordedAt time.Time
	Data       json.RawMessage
}

// DeviceDocuments returns device's documents for user.
func (s *Store) DeviceDocuments(ctx context.Context, user, device int64) ([]DeviceDocument, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT kind, recorded_at, data FROM device_documents WHERE user_id = ? AND device_id = ? ORDER BY kind`, user, device)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceDocument
	for rows.Next() {
		var d DeviceDocument
		var at int64
		var data string
		if err := rows.Scan(&d.Kind, &at, &data); err != nil {
			return nil, err
		}
		d.RecordedAt, d.Data = time.Unix(at, 0), json.RawMessage(data)
		out = append(out, d)
	}
	return out, rows.Err()
}

// GolfClub is a stored golf club.
type GolfClub struct {
	Index                      int
	Type                       *int64
	Name                       string
	AvgDistanceM, MaxDistanceM *float64
}

// GolfClubs returns user's golf clubs (from every device).
func (s *Store) GolfClubs(ctx context.Context, user int64) ([]GolfClub, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT idx, type, name, avg_distance_m, max_distance_m FROM golf_clubs WHERE user_id = ? ORDER BY device_id, idx`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GolfClub
	for rows.Next() {
		var c GolfClub
		if err := rows.Scan(&c.Index, &c.Type, &c.Name, &c.AvgDistanceM, &c.MaxDistanceM); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeviceSeriesLast returns the time of device's newest bucket of kind for user (0 if none).
func (s *Store) DeviceSeriesLast(ctx context.Context, user, device int64, kind string) (int64, error) {
	var ts sql.NullInt64
	err := s.db.R.QueryRowContext(ctx, `SELECT MAX(ts) FROM device_series WHERE user_id = ? AND device_id = ? AND kind = ?`, user, device, kind).Scan(&ts)
	return ts.Int64, err
}
