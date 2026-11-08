// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package wellness extracts health data from Garmin FIT files: heart rate, stress, Body
// Battery, respiration, steps and the other running totals, sleep sessions and stages, HRV and
// daily scores. It reads messages, not file types, so any file that carries a known message
// contributes; on the Fenix 8 that is mainly monitoring (FIT 32), sleep (49), HRV (68), metrics
// (44) and the daily summary file (72).
//
// Field meanings of messages the public FIT profile lacks come from Gadgetbridge's FIT profile
// and were checked against our Fenix 8's files (docs/protocol/06).
package wellness

import (
	"bytes"
	"cmp"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/proto"
)

// GarminEpoch is the FIT time origin, 1989-12-31 00:00 UTC, in Unix seconds.
const GarminEpoch = 631065600

// FITTime converts a FIT timestamp to time.
func FITTime(v int64) time.Time { return time.Unix(v+GarminEpoch, 0).UTC() }

// Sample is one value of a time series.
type Sample struct {
	Kind  Kind
	At    time.Time
	Sub   uint8 // activity type for the per-activity-type totals and Intensity, else 0
	Value int64
}

// Offset says that from At on, local time is UTC + Offset (the watch's time zone and DST).
type Offset struct {
	At     time.Time
	Offset time.Duration
}

// Daily is a once-a-day value.
type Daily struct {
	Kind Kind
	At   time.Time // UTC
	// Day is the local date ("2006-01-02") when the record states its local time, else "".
	Day   string
	Value int64
}

// Unknown marks an unknown number in Sleep and HRVNight.
const Unknown = -1

// Sleep is one night (or nap) as the watch summarised it. Durations and scores are Unknown
// when the file does not give them.
type Sleep struct {
	Start, End                                        time.Time
	Score, QualityScore, DurationScore, RecoveryScore int
	MinHR                                             int
	BodyBatteryStart, BodyBatteryEnd                  int
	// Seconds per stage.
	Deep, Light, REM, Awake, Unmeasurable int
	// Stages: built from the night's stage records (a sleep file), so the stage samples of the
	// night are there too; else a summary (the daily-health or metrics file, an export).
	Stages bool
}

// Nap is a nap the watch recorded (FIT nap_event).
type Nap struct {
	Start, End time.Time
	Feedback   int // nap_period_feedback, Unknown if absent
	Source     int // nap_source: 0 automatic, 1 manual, … (Unknown if absent)
}

// HRVNight is the watch's HRV status after a night, in ms (Unknown when absent).
type HRVNight struct {
	At                                             time.Time
	WeeklyAverage, LastNight, LastNightFiveMinHigh float64
	BaselineLowUpper, BalancedLower, BalancedUpper float64
	Status                                         int
}

// Suggestion is one of Garmin Coach's daily suggested workouts (FIT 77). A rest day has no name,
// no duration and Benefit Unknown.
type Suggestion struct {
	Day                    string // local date
	Index                  int    // workout_index
	Name, Description      string // e.g. "Base", "136bpm"
	Sport, SubSport        int
	Duration               time.Duration
	Benefit                int     // est_benefit: 1 recovery, 2 base, … (Garmin's enum)
	AerobicTE, AnaerobicTE float64 // estimated training effect
}

// WeighIn is one weighing (a scale's weight file). Unknown marks what the scale did not measure.
type WeighIn struct {
	At                                  time.Time
	Weight                              float64 // kg
	Fat, Hydration                      float64 // %
	Bone, Muscle, VisceralFat           float64 // kg
	BMI                                 float64 // kg/m²
	BMR, ActiveMR                       int     // kcal/day
	MetabolicAge, VisceralRating, Build int     // years; the scale's ratings
}

// SkinTempNight is the watch's overnight skin temperature (°C, Unknown when absent).
type SkinTempNight struct {
	At                              time.Time
	Day                             string // local date
	Nightly, Deviation, Deviation7d float64
	CalibratedDays                  int
}

// ECG is one ECG recording: its summary and its samples (µV, as the watch gives them).
type ECG struct {
	Start               time.Time
	Class               int     // ecg_rhythm_classification
	Side                int     // the wrist it was taken on (FIT side), Unknown if absent
	AvgHR               float64 // bpm
	RMSSD               int     // ms, Unknown if absent
	Seconds             int
	RawRate, SmoothRate float64 // samples per second
	Raw, Smooth         []float32
}

// Physio is the watch's physiological metrics as of a moment (after an activity, or from its
// backup of them): Unknown where it gives none.
type Physio struct {
	At                                      time.Time
	Sport, SubSport                         int
	VO2Max                                  float64 // ml/kg/min
	LTHR, LTPower, MaxHR, AvgHR             int     // bpm, W
	LTSpeed                                 float64 // km/h
	RecoveryMin                             int
	AerobicTE, AnaerobicTE                  float64
	Duration, Distance                      float64 // s, m
	Race5K, Race10K, RaceHalf, RaceMarathon int     // s
}

// Data is everything extracted from one file.
type Data struct {
	Suggestions []Suggestion
	Samples     []Sample
	Offsets     []Offset
	Daily       []Daily
	Sleep       []Sleep
	Naps        []Nap
	HRV         []HRVNight
	WeighIns    []WeighIn
	SkinTemp    []SkinTempNight
	ECG         []ECG
	Physio      []Physio
	// Messages counts every message number in the file, known or not.
	Messages map[uint16]int
}

// Span returns the earliest and latest time the data covers (zero times if none).
func (d *Data) Span() (from, to time.Time) {
	add := func(t time.Time) {
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
	for _, s := range d.Samples {
		add(s.At)
	}
	for _, s := range d.Daily {
		add(s.At)
	}
	for _, s := range d.Sleep {
		add(s.Start)
		add(s.End)
	}
	for _, n := range d.Naps {
		add(n.Start)
		add(n.End)
	}
	for _, s := range d.HRV {
		add(s.At)
	}
	for _, w := range d.WeighIns {
		add(w.At)
	}
	for _, n := range d.SkinTemp {
		add(n.At)
	}
	for _, e := range d.ECG {
		add(e.Start)
	}
	for _, p := range d.Physio {
		add(p.At)
	}
	return from, to
}

// Message numbers.
const (
	mMonitoring        = 55
	mMonitoringInfo    = 103
	mMonitoringHRData  = 211
	mStressLevel       = 227
	mLoadHistory       = 232
	mMetricsTime       = 241
	mSpO2              = 269
	mSleepLevel        = 275
	mMonitoringAlt     = 279
	mRespiration       = 297
	mRacePrediction    = 339
	mSleepAssessment   = 346
	mTrainingReadiness = 369
	mHRVSummary        = 370
	mHRVValue          = 371
	mTrainingLoad      = 378
	mDailySleep        = 384
	mHillScore         = 402
	mEnduranceScore    = 403
	mSleepSummary      = 411
	mHRMinimum         = 431
	mUserMetrics       = 79
	mSleepDemand       = 410
	mEvent             = 21
	mWorkout           = 26
	mWorkoutSchedule   = 428
	mHRVStats          = 401
	mNapEvent          = 412
	mWeightScale       = 30
	mPhysiological     = 140
	mMaxMetData        = 229
	mAcclimatization   = 281
	mDailyAltitude     = 282
	mMetricRecovery    = 284
	mECGSummary        = 336
	mECGRaw            = 337
	mECGSmooth         = 338
	mFunctionalMetrics = 356
	mTrueUpRecovery    = 357
	mSkinTempRaw       = 397
	mSkinTempOvernight = 398
	mMaxHR             = 430
	mDisruptionPeriod  = 470
	mDisruptionNight   = 471
	mSleepInsight      = 521
	fTimestamp         = 253
)

// eventSleep is FIT event 74 (sleep): type 0 when the watch saw sleep begin, 1 when it ended.
const eventSleep = 74

// ErrNotFIT is returned for bytes that are not a FIT file.
var ErrNotFIT = errors.New("wellness: not a FIT file")

// Decode extracts what it knows from a FIT file (all chained parts of it).
func Decode(b []byte) (*Data, error) {
	if len(b) < 12 || string(b[8:12]) != ".FIT" {
		return nil, ErrNotFIT
	}
	x := &extractor{d: &Data{Messages: map[uint16]int{}}}
	dec := decoder.New(bytes.NewReader(b), decoder.WithNoComponentExpansion())
	for dec.Next() {
		f, err := dec.Decode()
		if err != nil {
			return nil, err
		}
		x.last = 0
		for i := range f.Messages {
			x.message(&f.Messages[i])
		}
	}
	x.finish()
	return x.d, nil
}

type extractor struct {
	d    *Data
	last int64 // the last full timestamp of any message
	// lastMon is the last monitoring record's time (full or reconstructed): the base for the
	// next record's timestamp_16, as Gadgetbridge does. Other messages carry full timestamps
	// that can lie a little after the next monitoring record; based on those, a timestamp_16
	// just before them wrapped 18 hours into the future.
	lastMon int64

	stages []Sample // sleep_level records: each closes the stage interval since the record before
	// sleepStart is the night's start a sleep file states (UTC FIT seconds, 0 if it states
	// none): the first stage interval runs from it to the first record. sleep_insight states it
	// on newer watches; the sleep event's start (event 74, type start) on all, the same second.
	sleepStart, sleepEvent int64
	assessment             *Sleep
	ecg                    *ECG                 // the recording being read: its samples follow its summary
	summaries              []Sleep              // SLEEP_SUMMARY records, merged into DAILY_SLEEP in finish
	workouts               map[int64]Suggestion // WORKOUT by workout_index, for the schedule
}

// text returns string field n of m ("" if absent).
func text(m *proto.Message, n byte) string {
	for i := range m.Fields {
		f := &m.Fields[i]
		if f.Num == n && f.Value.Type() == proto.TypeString {
			return f.Value.String()
		}
	}
	return ""
}

// field returns field n of m as an integer, if present and valid.
func field(m *proto.Message, n byte) (int64, bool) {
	for i := range m.Fields {
		f := &m.Fields[i]
		if f.Num != n || f.IsExpandedField {
			continue
		}
		if !f.Value.Valid(f.BaseType) {
			return 0, false
		}
		return intValue(f.Value)
	}
	return 0, false
}

// slice returns the valid elements of array field n of m, with their positions (nil if the
// field is absent or not an array). Invalid elements are left out.
func slice(m *proto.Message, n byte) (vals []int64, idx []int) {
	for i := range m.Fields {
		f := &m.Fields[i]
		if f.Num != n || f.IsExpandedField {
			continue
		}
		add := func(j int, v, invalid uint64) {
			if v != invalid {
				vals, idx = append(vals, int64(v)), append(idx, j)
			}
		}
		switch f.Value.Type() {
		case proto.TypeSliceUint8:
			for j, v := range f.Value.SliceUint8() {
				add(j, uint64(v), 0xFF)
			}
		case proto.TypeSliceUint16:
			for j, v := range f.Value.SliceUint16() {
				add(j, uint64(v), 0xFFFF)
			}
		case proto.TypeSliceUint32:
			for j, v := range f.Value.SliceUint32() {
				add(j, uint64(v), 0xFFFFFFFF)
			}
		default:
			if v, ok := field(m, n); ok { // a one-element array may be encoded as a scalar
				return []int64{v}, []int{0}
			}
		}
		return vals, idx
	}
	return nil, nil
}

func intValue(v proto.Value) (int64, bool) {
	switch v.Type() {
	case proto.TypeBool: // a profile field of type bool (typedef.Bool: 0, 1, 0xFF invalid)
		return int64(v.Bool()), true
	case proto.TypeInt8:
		return int64(v.Int8()), true
	case proto.TypeUint8:
		return int64(v.Uint8()), true
	case proto.TypeInt16:
		return int64(v.Int16()), true
	case proto.TypeUint16:
		return int64(v.Uint16()), true
	case proto.TypeInt32:
		return int64(v.Int32()), true
	case proto.TypeUint32:
		return int64(v.Uint32()), true
	case proto.TypeInt64:
		return v.Int64(), true
	case proto.TypeUint64:
		return int64(v.Uint64()), true
	}
	return 0, false
}

// float returns field n of m as a number (float fields included), if present and valid.
func float(m *proto.Message, n byte) (float64, bool) {
	for i := range m.Fields {
		f := &m.Fields[i]
		if f.Num != n || f.IsExpandedField {
			continue
		}
		if !f.Value.Valid(f.BaseType) {
			return 0, false
		}
		switch f.Value.Type() {
		case proto.TypeFloat32:
			return float64(f.Value.Float32()), true
		case proto.TypeFloat64:
			return f.Value.Float64(), true
		}
		v, ok := intValue(f.Value)
		return float64(v), ok
	}
	return 0, false
}

// floats returns the valid float32 values of field n (one value or an array).
func floats(m *proto.Message, n byte) []float32 {
	for i := range m.Fields {
		f := &m.Fields[i]
		if f.Num != n || f.IsExpandedField {
			continue
		}
		switch f.Value.Type() {
		case proto.TypeFloat32:
			if f.Value.Valid(f.BaseType) {
				return []float32{f.Value.Float32()}
			}
		case proto.TypeSliceFloat32:
			var out []float32
			for _, v := range f.Value.SliceFloat32() {
				if !math.IsNaN(float64(v)) && math.Float32bits(v) != 0xFFFFFFFF {
					out = append(out, v)
				}
			}
			return out
		}
	}
	return nil
}

// scaled returns field n divided by scale, or Unknown.
func scaled(m *proto.Message, n byte, scale float64) float64 {
	if v, ok := float(m, n); ok {
		return v / scale
	}
	return Unknown
}

// orUnknown returns field n as an int, or Unknown.
func orUnknown(m *proto.Message, n byte) int {
	if v, ok := field(m, n); ok {
		return int(v)
	}
	return Unknown
}

func (x *extractor) sample(k Kind, at int64, sub uint8, v int64) {
	x.d.Samples = append(x.d.Samples, Sample{Kind: k, At: FITTime(at), Sub: sub, Value: v})
}

func (x *extractor) offset(at, local int64) {
	off := time.Duration(local-at) * time.Second
	// Real zones lie within ±14 h; anything else is a bad record.
	if at <= 0 || off < -14*time.Hour || off > 14*time.Hour {
		return
	}
	x.d.Offsets = append(x.d.Offsets, Offset{At: FITTime(at), Offset: off})
}

func (x *extractor) daily(k Kind, at int64, local int64, v int64) {
	d := Daily{Kind: k, At: FITTime(at), Value: v}
	if local > 0 {
		d.Day = FITTime(local).Format(time.DateOnly)
	}
	x.d.Daily = append(x.d.Daily, d)
}

func (x *extractor) message(m *proto.Message) {
	x.d.Messages[uint16(m.Num)]++
	ts, hasTS := field(m, fTimestamp)
	if hasTS {
		x.last = ts
	}
	switch m.Num {
	case mMonitoring:
		x.monitoring(m, ts, hasTS)
	case mMonitoringInfo:
		local, ok := field(m, 0)
		if ok && hasTS {
			x.offset(ts, local)
		}
		// cycles_goal per activity type; the watch repeats the daily step goal in each.
		if goals, _ := slice(m, 7); len(goals) > 0 && hasTS && goals[0] > 0 {
			x.daily(StepGoal, ts, local, goals[0])
		}
	case mMetricsTime:
		if local, ok := field(m, 0); ok && hasTS {
			x.offset(ts, local)
		}
	case mMonitoringHRData:
		if v, ok := field(m, 1); ok && hasTS && v > 0 {
			x.daily(RestingHR, ts, 0, v)
		}
	case mStressLevel:
		at, ok := field(m, 1)
		if !ok {
			return
		}
		if v, ok := field(m, 0); ok && v >= 0 && v <= 100 {
			x.sample(Stress, at, 0, v)
		}
		if v, ok := field(m, 3); ok && v >= 0 && v <= 100 {
			x.sample(BodyBattery, at, 0, v)
		}
	case mRespiration:
		if v, ok := field(m, 0); ok && hasTS && v > 0 {
			x.sample(Respiration, ts, 0, v)
		}
	case mSpO2:
		if v, ok := field(m, 0); ok && hasTS && v > 0 && v <= 100 {
			x.sample(SpO2, ts, 0, v)
		}
	case mMonitoringAlt:
		if v, ok := field(m, 0); ok && hasTS {
			x.sample(Altitude, ts, 0, v*20) // raw is 1/5 m
		}
	case mSleepLevel:
		// A record marks the END of a stage: the level since the previous record (checked on
		// the Fenix 8: read so, a night's records sum exactly to the daily-health file's stage
		// minutes). The samples are written in sleepFromStages, at each interval's start.
		if v, ok := field(m, 0); ok && hasTS {
			x.stages = append(x.stages, Sample{Kind: SleepStage, At: FITTime(ts), Value: v})
		}
	case mSleepInsight:
		// The night's bounds. The community profile calls them local times, but they are UTC:
		// on the Fenix 8 the end is the last stage record to the second. The offsets (minutes)
		// give the local time.
		at := func(t, o byte) int64 {
			v, ok := field(m, t)
			if !ok || v <= 0 {
				return 0
			}
			if off, ok := field(m, o); ok {
				x.offset(v, v+off*60)
			}
			return v
		}
		x.sleepStart = at(3, 4)
		at(5, 6) // the end: the last stage record; only its offset is kept
	case mEvent:
		if ev, _ := field(m, 0); ev == eventSleep && hasTS {
			if typ, ok := field(m, 1); ok && typ == 0 {
				x.sleepEvent = ts
			}
		}
	case mWeightScale:
		x.weighIn(m, ts, hasTS)
	case mSkinTempRaw:
		if v, ok := float(m, 1); ok && hasTS && math.Abs(v) < 20 {
			x.sample(SkinTemp, ts, 0, int64(math.Round(v*100)))
		}
	case mSkinTempOvernight:
		if !hasTS {
			return
		}
		n := SkinTempNight{At: FITTime(ts), Nightly: scaled(m, 4, 1), Deviation: scaled(m, 1, 1), Deviation7d: scaled(m, 2, 1),
			CalibratedDays: orUnknown(m, 3)}
		local, _ := field(m, 0)
		if local > 0 {
			n.Day = FITTime(local).Format(time.DateOnly)
		}
		x.d.SkinTemp = append(x.d.SkinTemp, n)
		if n.Deviation != Unknown {
			x.daily(SkinTempDeviation, ts, local, int64(math.Round(n.Deviation*100)))
		}
	case mDisruptionPeriod:
		if v, ok := field(m, 0); ok && hasTS {
			x.sample(Disruption, ts, 0, v)
		}
	case mDisruptionNight:
		if v, ok := field(m, 0); ok && hasTS {
			x.daily(DisruptionNight, ts, 0, v)
		}
	case mECGSummary:
		x.ecgSummary(m)
	case mECGRaw:
		if x.ecg != nil {
			x.ecg.Raw = append(x.ecg.Raw, floats(m, 0)...)
		}
	case mECGSmooth:
		if x.ecg != nil {
			x.ecg.Smooth = append(x.ecg.Smooth, floats(m, 0)...)
		}
	case mPhysiological:
		x.physio(m, ts, hasTS)
	case mMaxMetData:
		at, ok := field(m, 0)
		if !ok {
			at, ok = ts, hasTS
		}
		if !ok {
			return
		}
		if v, ok := field(m, 2); ok && v > 0 {
			x.daily(VO2Max, at, 0, v*10) // the field is ×10, the kind ×100
		}
		if v, ok := field(m, 3); ok && v > 0 {
			x.daily(FitnessAge, at, 0, v)
		}
	case mAcclimatization:
		if v, ok := field(m, 4); ok && hasTS && v > 0 {
			x.daily(AltitudeAcclim, ts, 0, v)
		}
	case mDailyAltitude:
		if v, ok := float(m, 1); ok && hasTS {
			x.daily(DailyAltitude, ts, 0, int64(math.Round(v/65536)))
		}
	case mMetricRecovery:
		if v, ok := field(m, 0); ok {
			at, ok2 := field(m, 1)
			if !ok2 {
				at, ok2 = ts, hasTS
			}
			if ok2 {
				x.daily(RecoveryTime, at, 0, v)
			}
		}
	case mTrueUpRecovery:
		if v, ok := field(m, 2); ok && hasTS {
			x.daily(RecoveryTime, ts, 0, v)
		}
	case mFunctionalMetrics:
		if !hasTS {
			return
		}
		if v, ok := field(m, 4); ok && v > 0 {
			x.daily(FTP, ts, 0, v)
		}
		if v, ok := field(m, 7); ok && v > 0 {
			x.daily(RunningLTPower, ts, 0, v)
		}
		if v, ok := field(m, 8); ok && v > 0 {
			x.daily(LactateThreshold, ts, 0, v)
		}
	case mMaxHR:
		if v, ok := field(m, 7); ok && hasTS && v > 0 {
			x.daily(MaxHR, ts, 0, v)
		}
	case mNapEvent:
		start, ok1 := field(m, 0)
		end, ok2 := field(m, 2)
		if del, _ := field(m, 5); !ok1 || !ok2 || end <= start || del == 1 {
			return
		}
		if v, ok := field(m, 1); ok {
			x.offset(start, start+v*60)
		}
		x.d.Naps = append(x.d.Naps, Nap{Start: FITTime(start), End: FITTime(end), Feedback: orUnknown(m, 4), Source: orUnknown(m, 6)})
	case mSleepAssessment:
		x.assessment = &Sleep{
			Score: orUnknown(m, 6), QualityScore: orUnknown(m, 7), DurationScore: orUnknown(m, 4),
			RecoveryScore: orUnknown(m, 8), MinHR: Unknown, BodyBatteryStart: Unknown, BodyBatteryEnd: Unknown,
		}
	case mHRVValue:
		if v, ok := field(m, 0); ok && hasTS && v > 0 {
			x.sample(HRV, ts, 0, v)
		}
	case mHRVSummary:
		if hasTS {
			ms := func(n byte) float64 {
				if v, ok := field(m, n); ok {
					return float64(v) / 128
				}
				return Unknown
			}
			x.d.HRV = append(x.d.HRV, HRVNight{At: FITTime(ts), WeeklyAverage: ms(0), LastNight: ms(1),
				LastNightFiveMinHigh: ms(2), BaselineLowUpper: ms(3), BalancedLower: ms(4), BalancedUpper: ms(5),
				Status: orUnknown(m, 6)})
		}
	case mDailySleep:
		x.dailySleep(m)
	case mSleepSummary:
		x.sleepSummary(m)
	case mHRMinimum:
		local, _ := field(m, 2)
		if v, ok := field(m, 3); ok && v > 0 && local > 0 {
			at := ts
			if !hasTS {
				at = local
			}
			x.daily(MinHR, at, local, v)
		}
	case mUserMetrics:
		if hasTS {
			if tz, ok := field(m, 23); ok {
				x.offset(ts, ts+tz*60)
			}
			if v, ok := field(m, 0); ok && v > 0 {
				x.daily(VO2Max, ts, 0, int64(float64(v)/292.57142857142856*100+0.5))
			}
		}
	case mWorkout:
		if idx, ok := field(m, 20); ok {
			if x.workouts == nil {
				x.workouts = map[int64]Suggestion{}
			}
			x.workouts[idx] = Suggestion{Name: text(m, 8), Description: text(m, 17), Sport: orUnknown(m, 4), SubSport: orUnknown(m, 11)}
		}
	case mWorkoutSchedule:
		idx, ok1 := field(m, 1)
		local, ok2 := field(m, 10)
		if !ok1 || !ok2 {
			return
		}
		s := Suggestion{Day: FITTime(local).Format(time.DateOnly), Index: int(idx), Sport: orUnknown(m, 7),
			SubSport: orUnknown(m, 8), Benefit: orUnknown(m, 3), AerobicTE: Unknown, AnaerobicTE: Unknown}
		if v, ok := field(m, 9); ok {
			s.Duration = time.Duration(v) * time.Millisecond
		}
		if v, ok := field(m, 5); ok {
			s.AerobicTE = float64(v) / 10
		}
		if v, ok := field(m, 6); ok {
			s.AnaerobicTE = float64(v) / 10
		}
		x.d.Suggestions = append(x.d.Suggestions, s)
	case mSleepDemand:
		if v, ok := field(m, 1); ok && hasTS && v > 0 {
			x.daily(SleepNeed, ts, 0, v)
		}
	case mHRVStats:
		// Arrays of nightly HRV (2) and the personal range (3, 4): 28 slots, one per night,
		// oldest first, as of the local time in field 0. The last valid night is last night;
		// each slot before it is one night earlier, and invalid slots are nights without a
		// value. Slots after last night are unused (a range may linger there and is ignored).
		// So one file carries up to four weeks of nights (docs/protocol/06, HRV history).
		local, ok := field(m, 0)
		if !ok {
			return
		}
		nights, idx := slice(m, 2)
		if len(nights) == 0 {
			return
		}
		last := idx[len(idx)-1]
		at := local // the message has no UTC timestamp; local time is close enough for At
		back := func(slot int) int64 { return int64(last-slot) * 86400 }
		for j, v := range nights {
			x.daily(HRVLastNight, at-back(idx[j]), local-back(idx[j]), v)
		}
		for n, k := range map[byte]Kind{3: HRVTargetLow, 4: HRVTargetHigh} {
			vals, vidx := slice(m, n)
			for j := range vals {
				if vidx[j] <= last {
					x.daily(k, at-back(vidx[j]), local-back(vidx[j]), vals[j])
				}
			}
		}
	case mTrainingReadiness:
		local, _ := field(m, 20)
		if v, ok := field(m, 0); ok && hasTS {
			x.daily(TrainingReadiness, ts, local, v)
		}
	case mEnduranceScore:
		if v, ok := field(m, 0); ok && hasTS && v > 0 {
			x.daily(EnduranceScore, ts, 0, v)
		}
	case mHillScore:
		if v, ok := field(m, 0); ok && hasTS && v > 0 {
			x.daily(HillScore, ts, 0, v)
		}
	case mTrainingLoad:
		if hasTS {
			if v, ok := field(m, 3); ok {
				x.daily(AcuteLoad, ts, 0, v)
			}
			if v, ok := field(m, 4); ok {
				x.daily(ChronicLoad, ts, 0, v)
			}
		}
	case mLoadHistory:
		if v, ok := field(m, 2); ok && hasTS {
			x.daily(TrainingStatus, ts, 0, v)
		}
	case mRacePrediction:
		local, _ := field(m, 0)
		if hasTS {
			for n, k := range map[byte]Kind{1: Race5K, 2: Race10K, 3: RaceHalf, 4: RaceMarathon} {
				if v, ok := field(m, n); ok && v > 0 {
					x.daily(k, ts, local, v)
				}
			}
		}
	}
}

// weighIn handles WEIGHT_SCALE: one weighing.
func (x *extractor) weighIn(m *proto.Message, ts int64, hasTS bool) {
	kg, ok := float(m, 0)
	if !hasTS || !ok || kg <= 0 || kg >= 0xFFFE {
		return
	}
	w := WeighIn{At: FITTime(ts), Weight: kg / 100, Fat: scaled(m, 1, 100), Hydration: scaled(m, 2, 100),
		VisceralFat: scaled(m, 3, 100), Bone: scaled(m, 4, 100), Muscle: scaled(m, 5, 100), BMI: scaled(m, 13, 10),
		BMR: Unknown, ActiveMR: Unknown, MetabolicAge: orUnknown(m, 10), VisceralRating: orUnknown(m, 11), Build: orUnknown(m, 8)}
	if v, ok := float(m, 7); ok {
		w.BMR = int(math.Round(v / 4))
	}
	if v, ok := float(m, 9); ok {
		w.ActiveMR = int(math.Round(v / 4))
	}
	x.d.WeighIns = append(x.d.WeighIns, w)
	x.daily(Weight, ts, 0, int64(math.Round(w.Weight*1000)))
	if w.Fat != Unknown {
		x.daily(BodyFat, ts, 0, int64(math.Round(w.Fat*100)))
	}
	if w.BMI != Unknown {
		x.daily(BMI, ts, 0, int64(math.Round(w.BMI*10)))
	}
}

// ecgSummary handles ECG_SUMMARY: a recording starts; its samples follow.
func (x *extractor) ecgSummary(m *proto.Message) {
	at, ok := field(m, 4)
	if !ok {
		at, ok = field(m, fTimestamp)
	}
	if !ok {
		x.ecg = nil
		return
	}
	e := ECG{Start: FITTime(at), Class: orUnknown(m, 1), Side: orUnknown(m, 6), AvgHR: scaled(m, 7, 1), RMSSD: orUnknown(m, 12),
		Seconds: orUnknown(m, 11), RawRate: scaled(m, 2, 1), SmoothRate: scaled(m, 3, 1)}
	x.d.ECG = append(x.d.ECG, e)
	x.ecg = &x.d.ECG[len(x.d.ECG)-1]
}

// physio handles PHYSIOLOGICAL_METRICS: the watch's view after an activity (or its backup).
func (x *extractor) physio(m *proto.Message, ts int64, hasTS bool) {
	if !hasTS {
		return
	}
	p := Physio{At: FITTime(ts), Sport: orUnknown(m, 11), SubSport: orUnknown(m, 12), VO2Max: scaled(m, 7, 18724.571428571428),
		LTHR: orUnknown(m, 14), LTPower: orUnknown(m, 15), LTSpeed: scaled(m, 16, 10), MaxHR: orUnknown(m, 53), AvgHR: orUnknown(m, 63),
		RecoveryMin: orUnknown(m, 9), AerobicTE: scaled(m, 4, 10), AnaerobicTE: scaled(m, 20, 10), Duration: scaled(m, 35, 1000),
		Distance: scaled(m, 36, 100), Race5K: orUnknown(m, 69), Race10K: orUnknown(m, 70), RaceHalf: orUnknown(m, 71), RaceMarathon: orUnknown(m, 72)}
	if p.MaxHR == Unknown {
		p.MaxHR = orUnknown(m, 1) // new_hr_max
	}
	x.d.Physio = append(x.d.Physio, p)
	if p.LTHR > 0 {
		x.daily(LactateThreshold, ts, 0, int64(p.LTHR))
	}
	if p.MaxHR > 0 {
		x.daily(MaxHR, ts, 0, int64(p.MaxHR))
	}
	if p.RecoveryMin > 0 {
		x.daily(RecoveryTime, ts, 0, int64(p.RecoveryMin))
	}
}

// monitoring handles one MONITORING record. Its time is a full timestamp or a timestamp_16
// relative to the last full one.
func (x *extractor) monitoring(m *proto.Message, ts int64, hasTS bool) {
	at := ts
	if !hasTS {
		t16, ok := field(m, 26)
		base := x.lastMon
		if base == 0 {
			base = x.last
		}
		if !ok || base == 0 {
			return
		}
		// The difference is signed: a record may lie a little before its base.
		d := (t16 - base) & 0xFFFF
		if d >= 0x8000 {
			d -= 0x10000
		}
		at = base + d
	}
	x.lastMon = at
	activity := int64(-1)
	if v, ok := field(m, 5); ok {
		activity = v
	} else if v, ok := field(m, 24); ok {
		activity = v & 0x1F
	}
	sub := uint8(255) // activity type unknown
	if activity >= 0 && activity < 255 {
		sub = uint8(activity)
	}
	if v, ok := field(m, 27); ok && v > 0 {
		x.sample(HeartRate, at, 0, v)
	}
	if v, ok := field(m, 24); ok {
		x.sample(Intensity, at, uint8(v&0x1F), (v>>5)&0x7)
	}
	for n, k := range map[byte]Kind{3: Steps, 2: Distance, 19: ActiveCalories} {
		if v, ok := field(m, n); ok {
			x.sample(k, at, sub, v)
		}
	}
	if v, ok := field(m, 4); ok {
		x.sample(ActiveTime, at, sub, v/1000)
	}
	if v, ok := field(m, 33); ok && v > 0 {
		x.sample(ModerateMinutes, at, 0, v)
	}
	if v, ok := field(m, 34); ok && v > 0 {
		x.sample(VigorousMinutes, at, 0, v)
	}
	if v, ok := field(m, 35); ok {
		x.sample(Ascent, at, 0, v)
	}
	if v, ok := field(m, 36); ok {
		x.sample(Descent, at, 0, v)
	}
}

// dailySleep handles DAILY_SLEEP: one record per night, many nights per file.
func (x *extractor) dailySleep(m *proto.Message) {
	start, ok1 := field(m, 9)
	end, ok2 := field(m, 11)
	if !ok1 || !ok2 || end <= start {
		return
	}
	if v, ok := field(m, 10); ok {
		x.offset(start, start+v*60)
	}
	if v, ok := field(m, 12); ok {
		x.offset(end, end+v*60)
	}
	x.d.Sleep = append(x.d.Sleep, Sleep{
		Start: FITTime(start), End: FITTime(end),
		Score: orUnknown(m, 2), MinHR: orUnknown(m, 0),
		BodyBatteryStart: orUnknown(m, 14), BodyBatteryEnd: orUnknown(m, 16),
		DurationScore: orUnknown(m, 20), RecoveryScore: orUnknown(m, 23), QualityScore: orUnknown(m, 25),
		Deep: Unknown, Light: Unknown, REM: Unknown, Awake: Unknown, Unmeasurable: Unknown,
	})
}

// sleepSummary handles SLEEP_SUMMARY: stage minutes of a night, kept for merging.
func (x *extractor) sleepSummary(m *proto.Message) {
	start, ok1 := field(m, 10)
	end, ok2 := field(m, 8)
	if !ok1 || !ok2 || end <= start {
		return
	}
	if local, ok := field(m, 9); ok {
		x.offset(start, local)
	}
	if local, ok := field(m, 7); ok {
		x.offset(end, local)
	}
	minutes := func(n byte) int {
		if v, ok := field(m, n); ok {
			return int(v) * 60
		}
		return Unknown
	}
	x.summaries = append(x.summaries, Sleep{
		Start: FITTime(start), End: FITTime(end), Score: orUnknown(m, 0),
		QualityScore: Unknown, DurationScore: Unknown, RecoveryScore: Unknown,
		MinHR: Unknown, BodyBatteryStart: Unknown, BodyBatteryEnd: Unknown,
		Deep: minutes(1), Light: minutes(2), REM: minutes(3), Awake: minutes(4), Unmeasurable: minutes(5),
	})
}

// sameNight reports whether two records describe the same sleep.
func sameNight(a, b Sleep) bool {
	d := a.Start.Sub(b.Start)
	return d > -2*time.Minute && d < 2*time.Minute
}

func (x *extractor) finish() {
	for i := range x.d.Suggestions { // the schedule may come before or after its workouts
		s := &x.d.Suggestions[i]
		if w, ok := x.workouts[int64(s.Index)]; ok {
			s.Name, s.Description = w.Name, w.Description
		}
	}
	// Stage minutes from SLEEP_SUMMARY complete the matching DAILY_SLEEP night.
	for _, s := range x.summaries {
		i := slices.IndexFunc(x.d.Sleep, func(d Sleep) bool { return sameNight(d, s) })
		if i < 0 {
			x.d.Sleep = append(x.d.Sleep, s)
			continue
		}
		d := &x.d.Sleep[i]
		d.Deep, d.Light, d.REM, d.Awake, d.Unmeasurable = s.Deep, s.Light, s.REM, s.Awake, s.Unmeasurable
		if d.Score == Unknown {
			d.Score = s.Score
		}
	}
	// A sleep file (stages, maybe an assessment) makes one session of its own.
	if len(x.stages) > 0 {
		x.sleepFromStages()
	}
	slices.SortStableFunc(x.d.Sleep, func(a, b Sleep) int { return a.Start.Compare(b.Start) })
	slices.SortStableFunc(x.d.Naps, func(a, b Nap) int { return a.Start.Compare(b.Start) })
	slices.SortStableFunc(x.d.Offsets, func(a, b Offset) int { return a.At.Compare(b.At) })
	x.d.Offsets = slices.CompactFunc(x.d.Offsets, func(a, b Offset) bool { return a.At.Equal(b.At) && a.Offset == b.Offset })
}

// sleepFromStages makes the night of a sleep file from its stage records. Each record closes an
// interval: the first one the interval from the night's start (the file states it), each later
// one the interval since the record before. The night ends at the last record. A file that
// states no start loses its first interval (its length is unknown); the night then starts at
// the first record, and a summary of the night from another file supplies its true start.
func (x *extractor) sleepFromStages() {
	st := slices.Clone(x.stages)
	slices.SortStableFunc(st, func(a, b Sample) int { return cmp.Compare(a.At.Unix(), b.At.Unix()) })
	start := st[0].At
	for _, v := range []int64{x.sleepStart, x.sleepEvent} {
		// A stated start, if it lies before the first record and within a night of it.
		if t := FITTime(v); v > 0 && !t.After(start) && start.Sub(t) < 16*time.Hour {
			start = t
			break
		}
	}
	s := Sleep{Start: start, End: st[len(st)-1].At, Score: Unknown, QualityScore: Unknown, DurationScore: Unknown,
		RecoveryScore: Unknown, MinHR: Unknown, BodyBatteryStart: Unknown, BodyBatteryEnd: Unknown, Stages: true}
	if a := x.assessment; a != nil {
		s.Score, s.QualityScore, s.DurationScore, s.RecoveryScore = a.Score, a.QualityScore, a.DurationScore, a.RecoveryScore
	}
	prev := start
	for _, p := range st {
		if !p.At.After(prev) {
			continue // a record at the start (or a repeat) closes nothing
		}
		x.d.Samples = append(x.d.Samples, Sample{Kind: SleepStage, At: prev, Value: p.Value})
		secs := int(p.At.Sub(prev) / time.Second)
		switch p.Value {
		case SleepDeep:
			s.Deep += secs
		case SleepLight:
			s.Light += secs
		case SleepREM:
			s.REM += secs
		case SleepAwake:
			s.Awake += secs
		default:
			s.Unmeasurable += secs
		}
		prev = p.At
	}
	if !s.End.After(s.Start) {
		return // a single record: no interval, no night
	}
	if i := slices.IndexFunc(x.d.Sleep, func(d Sleep) bool { return sameNight(d, s) }); i >= 0 {
		// The daily record already describes this night; add the stage totals.
		d := &x.d.Sleep[i]
		d.Deep, d.Light, d.REM, d.Awake, d.Unmeasurable, d.Stages = s.Deep, s.Light, s.REM, s.Awake, s.Unmeasurable, true
		return
	}
	x.d.Sleep = append(x.d.Sleep, s)
}
