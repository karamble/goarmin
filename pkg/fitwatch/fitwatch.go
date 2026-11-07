// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package fitwatch reads what a Garmin watch keeps about its wearer and their plans from its
// FIT files: settings (type 2), sport profiles (3), workouts (5), courses (6), lifetime totals
// (10) and goals (11). Values are in SI units; unknown numbers are 0 (or -1 where 0 is a real
// value), unknown words "".
package fitwatch

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/pkg/fitactivity"
	"github.com/karamble/goarmin/pkg/geo"
)

// File is what one file held; only the parts of its type are set.
type File struct {
	Type     typedef.File
	Created  time.Time
	Settings *Settings
	Sport    *SportProfile
	Goals    []Goal
	Totals   []Totals
	Workout  *Workout
	Course   *Course
}

// Zones are heart-rate and power zones with what they derive from.
type Zones struct {
	MaxHR       int    `json:"max_hr,omitempty"`
	ThresholdHR int    `json:"threshold_hr,omitempty"`
	FTP         int    `json:"ftp,omitempty"`
	HRCalc      string `json:"hr_calc,omitempty"`    // percent_max_hr, percent_hrr, percent_lthr, custom
	PowerCalc   string `json:"power_calc,omitempty"` // percent_ftp, custom
	// HR are six limits in bpm: the bottom of zone 1, then the top of zones 1–5 (the last is
	// the maximum), as the watch's hr_zone records and an activity's time_in_zone give them.
	// Power zones are not kept: the Fenix 8's power_zone records are exactly 55, 75, 90, 105,
	// 120 and 150 % of each FTP, and do not pair with the zone targets in order.
	HR []int `json:"hr,omitempty"`
}

// Valid says whether the zones have heart-rate limits.
func (z Zones) Valid() bool { return len(z.HR) == 6 }

// Units are the watch's display units: "metric" or "statute" each.
type Units struct {
	Distance    string `json:"distance,omitempty"`
	Speed       string `json:"speed,omitempty"`
	Elevation   string `json:"elevation,omitempty"`
	Temperature string `json:"temperature,omitempty"`
	Weight      string `json:"weight,omitempty"`
	Height      string `json:"height,omitempty"`
}

// Bike is a bike profile with its odometer.
type Bike struct {
	Name      string   `json:"name"`
	Sport     string   `json:"sport,omitempty"`
	SubSport  string   `json:"sub_sport,omitempty"`
	OdometerM float64  `json:"odometer_m,omitempty"`
	WeightKg  float64  `json:"weight_kg,omitempty"`
	WheelMM   int      `json:"wheel_mm,omitempty"`
	CrankMM   float64  `json:"crank_mm,omitempty"`
	Enabled   bool     `json:"enabled"`
	Sensors   []string `json:"sensors,omitempty"` // speed, cadence, speed_cadence, power
}

// Settings are the watch's settings and what it knows about its wearer.
type Settings struct {
	Clock        string  `json:"clock,omitempty"`     // 24h or 12h
	DateMode     string  `json:"date_mode,omitempty"` // day_month or month_day
	Units        Units   `json:"units"`
	RestingHR    int     `json:"resting_hr,omitempty"`
	MaxHR        int     `json:"max_hr,omitempty"`     // default_max_heart_rate
	HRSetting    string  `json:"hr_setting,omitempty"` // max, reserve
	WakeS        int     `json:"wake_s"`               // seconds after midnight, -1 unknown
	SleepS       int     `json:"sleep_s"`
	RunStepM     float64 `json:"run_step_m,omitempty"`
	WalkStepM    float64 `json:"walk_step_m,omitempty"`
	HeightM      float64 `json:"height_m,omitempty"`
	WeightKg     float64 `json:"weight_kg,omitempty"`
	ActivityLvl  int     `json:"activity_level,omitempty"` // 0–100
	Athlete      bool    `json:"athlete,omitempty"`
	Zones        []Zones `json:"zones,omitempty"` // per sport group, the first is the default
	Bikes        []Bike  `json:"bikes,omitempty"`
	FriendlyName string  `json:"-"` // not kept: the watch's owner name
}

// SportProfile is one of the watch's sport profiles with its zones.
type SportProfile struct {
	Sport    string `json:"sport"`
	SubSport string `json:"sub_sport"`
	Name     string `json:"name"`
	Zones    Zones  `json:"zones"`
}

// Goal is one of the watch's goals.
type Goal struct {
	Type       string `json:"type"`  // steps, active_minutes, ascent, distance, calories, time, frequency, …
	Sport      string `json:"sport"` // "" for all
	Target     int64  `json:"target"`
	Unit       string `json:"unit"`                 // steps, min, m, kcal, s, times
	Source     string `json:"source,omitempty"`     // auto, user, community
	Recurrence string `json:"recurrence,omitempty"` // daily, weekly, …
}

// Totals are the watch's lifetime totals for a sport.
type Totals struct {
	Sport     string  `json:"sport"`
	TimerS    int64   `json:"timer_s"`
	ElapsedS  int64   `json:"elapsed_s"` // -1 unknown
	ActiveS   int64   `json:"active_s"`  // -1 unknown
	DistanceM float64 `json:"distance_m"`
	Calories  int64   `json:"kcal"`
	Sessions  int     `json:"sessions"`
}

// Exercise is a strength, yoga or mobility exercise of a workout step.
type Exercise struct {
	Category string `json:"category"`
	Name     string `json:"name"` // as the watch titles it, or the FIT profile's name
	Number   int    `json:"number"`
	Title    string `json:"title,omitempty"` // the workout's own title for it
}

// Step is one workout step.
type Step struct {
	Index     int    `json:"index"`
	Name      string `json:"name,omitempty"`
	Notes     string `json:"notes,omitempty"`
	Intensity string `json:"intensity,omitempty"` // active, rest, warmup, cooldown, recovery, interval
	// Duration: time (s), distance (m), reps, calories (kcal), open (lap button), hr_less_than
	// / hr_greater_than (bpm or % max), power_less_than / power_greater_than, or repeat (Repeat
	// times from the step From).
	Duration      string  `json:"duration"`
	DurationValue float64 `json:"duration_value,omitempty"`
	From          int     `json:"from,omitempty"`
	Repeat        int     `json:"repeat,omitempty"`
	// Target: speed (m/s), heart_rate (bpm), cadence (rpm), power (W), open; a zone or a range.
	Target     string    `json:"target,omitempty"`
	TargetZone int       `json:"target_zone,omitempty"`
	TargetLow  float64   `json:"target_low,omitempty"`
	TargetHigh float64   `json:"target_high,omitempty"`
	TargetPct  bool      `json:"target_pct,omitempty"` // low/high are % of max HR or FTP
	Exercise   *Exercise `json:"exercise,omitempty"`
	WeightKg   float64   `json:"weight_kg,omitempty"`
}

// Workout is a saved workout.
type Workout struct {
	Name       string `json:"name"`
	Sport      string `json:"sport"`
	SubSport   string `json:"sub_sport"`
	Steps      []Step `json:"steps"`
	EstimatedS int    `json:"estimated_s"` // timed steps with their repeats; 0 if none are timed
}

// CoursePoint is a named point along a course.
type CoursePoint struct {
	Type      string  `json:"type"` // generic, summit, water, food, left, right, …
	Name      string  `json:"name,omitempty"`
	DistanceM float64 `json:"distance_m"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
}

// Course is a saved route.
type Course struct {
	Name      string            `json:"name"`
	Sport     string            `json:"sport"`
	DistanceM float64           `json:"distance_m"`
	AscentM   float64           `json:"ascent_m"`
	DescentM  float64           `json:"descent_m"`
	Bounds    *geo.Bounds       `json:"-"`
	Polyline  string            `json:"-"`
	Track     fitactivity.Track `json:"-"`
	Points    []CoursePoint     `json:"points"`
}

// ErrNotWatchFile is returned for a file of another type.
var ErrNotWatchFile = errors.New("fitwatch: not a settings, sport, workout, course, totals or goals file")

// Types are the file types Decode reads.
var Types = []typedef.File{typedef.FileSettings, typedef.FileSport, typedef.FileWorkout, typedef.FileCourse, typedef.FileTotals, typedef.FileGoals}

// Decode reads a settings, sport, workout, course, totals or goals file.
func Decode(b []byte) (*File, error) {
	f := &File{}
	var (
		zt        []*mesgdef.ZonesTarget
		hz        []*mesgdef.HrZone
		titles    = map[[2]uint16]string{}
		steps     []*mesgdef.WorkoutStep
		cpoints   []*mesgdef.CoursePoint
		laps      []*mesgdef.Lap
		dev       *mesgdef.DeviceSettings
		user      *mesgdef.UserProfile
		bikes     []*mesgdef.BikeProfile
		sport     *mesgdef.Sport
		workout   *mesgdef.Workout
		course    *mesgdef.Course
		track     fitactivity.Track
		hasFileID bool
	)
	dec := decoder.New(bytes.NewReader(b))
	for dec.Next() {
		ff, err := dec.Decode()
		if err != nil {
			return nil, err
		}
		for i := range ff.Messages {
			m := &ff.Messages[i]
			switch m.Num {
			case typedef.MesgNumFileId:
				if !hasFileID {
					id := mesgdef.NewFileId(m)
					f.Type, f.Created, hasFileID = id.Type, id.TimeCreated, true
				}
			case typedef.MesgNumDeviceSettings:
				dev = mesgdef.NewDeviceSettings(m)
			case typedef.MesgNumUserProfile:
				user = mesgdef.NewUserProfile(m)
			case typedef.MesgNumBikeProfile:
				bikes = append(bikes, mesgdef.NewBikeProfile(m))
			case typedef.MesgNumZonesTarget:
				zt = append(zt, mesgdef.NewZonesTarget(m))
			case typedef.MesgNumHrZone:
				hz = append(hz, mesgdef.NewHrZone(m))
			case typedef.MesgNumSport:
				sport = mesgdef.NewSport(m)
			case typedef.MesgNumGoal:
				if g, ok := goal(mesgdef.NewGoal(m)); ok {
					f.Goals = append(f.Goals, g)
				}
			case typedef.MesgNumTotals:
				if t, ok := totals(mesgdef.NewTotals(m)); ok {
					f.Totals = append(f.Totals, t)
				}
			case typedef.MesgNumWorkout:
				workout = mesgdef.NewWorkout(m)
			case typedef.MesgNumWorkoutStep:
				steps = append(steps, mesgdef.NewWorkoutStep(m))
			case typedef.MesgNumExerciseTitle:
				t := mesgdef.NewExerciseTitle(m)
				if name := first(t.WktStepName); name != "" {
					titles[[2]uint16{uint16(t.ExerciseCategory), t.ExerciseName}] = name
				}
			case typedef.MesgNumCourse:
				course = mesgdef.NewCourse(m)
			case typedef.MesgNumCoursePoint:
				cpoints = append(cpoints, mesgdef.NewCoursePoint(m))
			case typedef.MesgNumLap:
				laps = append(laps, mesgdef.NewLap(m))
			case typedef.MesgNumRecord:
				track.Add(mesgdef.NewRecord(m))
			}
		}
	}
	zones := groupZones(zt, hz)
	switch f.Type {
	case typedef.FileSettings:
		f.Settings = settings(dev, user, zones, bikes)
	case typedef.FileSport:
		p := &SportProfile{}
		if sport != nil {
			p.Sport, p.SubSport, p.Name = word(sport.Sport.String()), word(sport.SubSport.String()), strings.TrimSpace(sport.Name)
		}
		if len(zones) > 0 {
			p.Zones = zones[0]
		}
		f.Sport = p
	case typedef.FileWorkout:
		f.Workout = workoutOf(workout, steps, titles)
	case typedef.FileCourse:
		f.Course = courseOf(course, track, laps, cpoints)
	case typedef.FileTotals, typedef.FileGoals:
	default:
		return f, ErrNotWatchFile
	}
	return f, nil
}

// word turns an enum name into a word; invalid ones become "".
func word(s string) string {
	if strings.Contains(s, "Invalid") || strings.HasPrefix(s, "Sport") || strings.HasPrefix(s, "SubSport") {
		return ""
	}
	return s
}

func u8(v uint8) int {
	if v == basetype.Uint8Invalid {
		return 0
	}
	return int(v)
}

func u16(v uint16) int {
	if v == basetype.Uint16Invalid {
		return 0
	}
	return int(v)
}

// groupZones pairs each zones target with its six HR limits (hr_zone records in sets of six,
// in order). A set with an invalid limit has none.
func groupZones(zt []*mesgdef.ZonesTarget, hz []*mesgdef.HrZone) []Zones {
	n := len(zt)
	if n == 0 && len(hz) >= 6 {
		n = 1
	}
	out := make([]Zones, n)
	for i := range out {
		z := &out[i]
		if i < len(zt) {
			t := zt[i]
			if v := u8(t.MaxHeartRate); v > 0 && v < 255 {
				z.MaxHR = v
			}
			z.ThresholdHR = u8(t.ThresholdHeartRate)
			if v := u16(t.FunctionalThresholdPower); v > 0 && v < 0xFFFF {
				z.FTP = v
			}
			z.HRCalc, z.PowerCalc = word(t.HrCalcType.String()), word(t.PwrCalcType.String())
		}
		if (i+1)*6 <= len(hz) {
			var hr []int
			for _, h := range hz[i*6 : (i+1)*6] {
				v := u8(h.HighBpm)
				if v == 0 || v == 255 {
					hr = nil
					break
				}
				hr = append(hr, v)
			}
			z.HR = hr
		}
	}
	return out
}

func settings(dev *mesgdef.DeviceSettings, user *mesgdef.UserProfile, zones []Zones, bikes []*mesgdef.BikeProfile) *Settings {
	s := &Settings{WakeS: -1, SleepS: -1, Zones: zones}
	if dev != nil {
		if len(dev.TimeMode) > 0 {
			switch dev.TimeMode[0] {
			case typedef.TimeModeHour24:
				s.Clock = "24h"
			case typedef.TimeModeHour12:
				s.Clock = "12h"
			}
		}
		s.DateMode = word(dev.DateMode.String())
	}
	if user != nil {
		units := func(v typedef.DisplayMeasure) string { return word(v.String()) }
		s.Units = Units{Distance: units(user.DistSetting), Speed: units(user.SpeedSetting), Elevation: units(user.ElevSetting),
			Temperature: units(user.TemperatureSetting), Weight: units(user.WeightSetting), Height: units(user.HeightSetting)}
		s.RestingHR = u8(user.RestingHeartRate)
		if v := u8(user.DefaultMaxHeartRate); v > 0 && v < 255 {
			s.MaxHR = v
		}
		s.HRSetting = word(user.HrSetting.String())
		if uint32(user.WakeTime) != basetype.Uint32Invalid {
			s.WakeS = int(user.WakeTime)
		}
		if uint32(user.SleepTime) != basetype.Uint32Invalid {
			s.SleepS = int(user.SleepTime)
		}
		if v := u16(user.UserRunningStepLength); v > 0 {
			s.RunStepM = float64(v) / 1000
		}
		if v := u16(user.UserWalkingStepLength); v > 0 {
			s.WalkStepM = float64(v) / 1000
		}
		if v := u8(user.Height); v > 0 && v < 255 {
			s.HeightM = float64(v) / 100
		}
		if v := u16(user.Weight); v > 0 && v < 0xFFFF {
			s.WeightKg = float64(v) / 10
		}
		if c := uint8(user.ActivityClass); c != basetype.Uint8Invalid {
			s.ActivityLvl = int(c & 0x7F)
			s.Athlete = c&0x80 != 0
		}
		s.FriendlyName = strings.TrimSpace(user.FriendlyName)
	}
	for _, b := range bikes {
		bk := Bike{Name: strings.TrimSpace(b.Name), Sport: word(b.Sport.String()), SubSport: word(b.SubSport.String()),
			Enabled: b.Enabled == typedef.BoolTrue}
		if b.Odometer != basetype.Uint32Invalid {
			bk.OdometerM = float64(b.Odometer) / 100
		}
		if v := u16(b.BikeWeight); v > 0 {
			bk.WeightKg = float64(v) / 10
		}
		if v := u16(b.CustomWheelsize); v > 0 {
			bk.WheelMM = v
		} else if v := u16(b.AutoWheelsize); v > 0 {
			bk.WheelMM = v
		}
		if v := u8(b.CrankLength); v > 0 && v < 255 {
			bk.CrankMM = float64(v)/2 + 110
		}
		for name, on := range map[string]typedef.Bool{"speed": b.SpdEnabled, "cadence": b.CadEnabled, "speed_cadence": b.SpdcadEnabled, "power": b.PowerEnabled} {
			if on == typedef.BoolTrue {
				bk.Sensors = append(bk.Sensors, name)
			}
		}
		sortStrings(bk.Sensors)
		if bk.Name == "" && bk.OdometerM == 0 {
			continue // an unused slot
		}
		s.Bikes = append(s.Bikes, bk)
	}
	return s
}

// first is the first non-empty of a field's languages.
func first(ss []string) string {
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// goalUnits are the units of goal targets by type (FIT goal: ascent in mm, distance in cm).
var goalUnits = map[string]struct {
	unit  string
	scale float64
}{
	"steps": {"steps", 1}, "active_minutes": {"min", 1}, "ascent": {"m", 0.001}, "distance": {"m", 0.01},
	"calories": {"kcal", 1}, "time": {"s", 1}, "frequency": {"times", 1}, "wellness_minutes": {"min", 1},
}

func goal(g *mesgdef.Goal) (Goal, bool) {
	t := word(g.Type.String())
	if t == "" || g.TargetValue == basetype.Uint32Invalid {
		return Goal{}, false
	}
	u, ok := goalUnits[t]
	if !ok {
		u.scale = 1
	}
	return Goal{Type: t, Sport: word(g.Sport.String()), Target: int64(math.Round(float64(g.TargetValue) * u.scale)), Unit: u.unit,
		Source: word(g.Source.String()), Recurrence: word(g.Recurrence.String())}, true
}

func totals(t *mesgdef.Totals) (Totals, bool) {
	sp := word(t.Sport.String())
	if sp == "" || t.Sessions == basetype.Uint16Invalid || t.Sessions == 0 {
		return Totals{}, false
	}
	v := func(x uint32) int64 {
		if x == basetype.Uint32Invalid {
			return -1
		}
		return int64(x)
	}
	return Totals{Sport: sp, TimerS: max(0, v(t.TimerTime)), ElapsedS: v(t.ElapsedTime), ActiveS: v(t.ActiveTime),
		DistanceM: float64(max(0, v(t.Distance))), Calories: max(0, v(t.Calories)), Sessions: int(t.Sessions)}, true
}

func workoutOf(w *mesgdef.Workout, steps []*mesgdef.WorkoutStep, titles map[[2]uint16]string) *Workout {
	out := &Workout{}
	if w != nil {
		out.Name, out.Sport, out.SubSport = strings.TrimSpace(w.WktName), word(w.Sport.String()), word(w.SubSport.String())
	}
	for i, s := range steps {
		idx := int(s.MessageIndex)
		if s.MessageIndex == typedef.MessageIndexInvalid || idx >= 0xFFF {
			idx = i
		}
		st := Step{Index: idx, Name: strings.TrimSpace(s.WktStepName), Notes: strings.TrimSpace(s.Notes), Intensity: word(s.Intensity.String())}
		dv := s.DurationValue
		switch s.DurationType {
		case typedef.WktStepDurationTime, typedef.WktStepDurationRepetitionTime:
			st.Duration, st.DurationValue = "time", float64(dv)/1000
		case typedef.WktStepDurationDistance:
			st.Duration, st.DurationValue = "distance", float64(dv)/100
		case typedef.WktStepDurationReps:
			st.Duration, st.DurationValue = "reps", float64(dv)
		case typedef.WktStepDurationCalories:
			st.Duration, st.DurationValue = "calories", float64(dv)
		case typedef.WktStepDurationOpen:
			st.Duration = "open"
		case typedef.WktStepDurationHrLessThan, typedef.WktStepDurationHrGreaterThan:
			st.Duration = word(s.DurationType.String())
			st.DurationValue, st.TargetPct = offsetHR(dv)
		case typedef.WktStepDurationPowerLessThan, typedef.WktStepDurationPowerGreaterThan:
			st.Duration = word(s.DurationType.String())
			st.DurationValue, st.TargetPct = offsetPower(dv)
		case typedef.WktStepDurationRepeatUntilStepsCmplt:
			st.Duration, st.From, st.Repeat = "repeat", int(dv), int(s.TargetValue)
		default:
			st.Duration = word(s.DurationType.String())
			if dv != basetype.Uint32Invalid {
				st.DurationValue = float64(dv)
			}
		}
		if st.Duration != "repeat" {
			target(&st, s)
		}
		if s.ExerciseCategory != typedef.ExerciseCategoryInvalid && s.ExerciseName != basetype.Uint16Invalid {
			ex := &Exercise{Category: word(s.ExerciseCategory.String()), Number: int(s.ExerciseName),
				Title: titles[[2]uint16{uint16(s.ExerciseCategory), s.ExerciseName}]}
			ex.Name = ex.Title
			if ex.Name == "" {
				if e := exerciseEnum(s.ExerciseCategory, s.ExerciseName); e != "" && !strings.Contains(e, "Invalid") {
					ex.Name = e
				}
			}
			st.Exercise = ex
		}
		if s.ExerciseWeight != basetype.Uint16Invalid && s.ExerciseWeight > 0 {
			st.WeightKg = float64(s.ExerciseWeight) / 100
		}
		out.Steps = append(out.Steps, st)
	}
	out.EstimatedS = estimate(out.Steps)
	return out
}

// offsetHR reads a heart-rate value of a workout step: up to 100 a % of max, above it bpm + 100.
func offsetHR(v uint32) (float64, bool) {
	if v == basetype.Uint32Invalid || v == 0 {
		return 0, false
	}
	if v <= 100 {
		return float64(v), true
	}
	return float64(v - 100), false
}

// offsetPower reads a power value of a workout step: up to 1000 a % of FTP, above it W + 1000.
func offsetPower(v uint32) (float64, bool) {
	if v == basetype.Uint32Invalid || v == 0 {
		return 0, false
	}
	if v <= 1000 {
		return float64(v), true
	}
	return float64(v - 1000), false
}

func target(st *Step, s *mesgdef.WorkoutStep) {
	if s.TargetType == typedef.WktStepTargetInvalid {
		return
	}
	st.Target = word(s.TargetType.String())
	if st.Target == "open" || st.Target == "" {
		return
	}
	if s.TargetValue != basetype.Uint32Invalid && s.TargetValue > 0 {
		st.TargetZone = int(s.TargetValue)
		return
	}
	lo, hi := s.CustomTargetValueLow, s.CustomTargetValueHigh
	if lo == basetype.Uint32Invalid || hi == basetype.Uint32Invalid {
		return
	}
	switch s.TargetType {
	case typedef.WktStepTargetHeartRate:
		l, pct := offsetHR(lo)
		h, _ := offsetHR(hi)
		st.TargetLow, st.TargetHigh, st.TargetPct = l, h, pct
	case typedef.WktStepTargetPower:
		l, pct := offsetPower(lo)
		h, _ := offsetPower(hi)
		st.TargetLow, st.TargetHigh, st.TargetPct = l, h, pct
	case typedef.WktStepTargetSpeed:
		st.TargetLow, st.TargetHigh = float64(lo)/1000, float64(hi)/1000
	default:
		st.TargetLow, st.TargetHigh = float64(lo), float64(hi)
	}
}

// estimate adds up the timed steps, repeats included (a repeat step runs the steps from From up
// to itself Repeat times in all).
func estimate(steps []Step) int {
	total := 0.0
	pos := map[int]int{} // step index → position in steps
	for i, s := range steps {
		pos[s.Index] = i
	}
	sum := func(from, to int) float64 {
		t := 0.0
		for _, s := range steps[from:to] {
			if s.Duration == "time" {
				t += s.DurationValue
			}
		}
		return t
	}
	for i, s := range steps {
		switch {
		case s.Duration == "time":
			total += s.DurationValue
		case s.Duration == "repeat" && s.Repeat > 1:
			if from, ok := pos[s.From]; ok && from < i {
				total += sum(from, i) * float64(s.Repeat-1)
			}
		}
	}
	return int(math.Round(total))
}

func courseOf(c *mesgdef.Course, track fitactivity.Track, laps []*mesgdef.Lap, points []*mesgdef.CoursePoint) *Course {
	out := &Course{Track: track}
	if c != nil {
		out.Name, out.Sport = strings.TrimSpace(c.Name), word(c.Sport.String())
	}
	for _, l := range laps {
		if v := l.TotalDistanceScaled(); !math.IsNaN(v) {
			out.DistanceM += v
		}
		if v := l.TotalAscent; v != basetype.Uint16Invalid {
			out.AscentM += float64(v)
		}
		if v := l.TotalDescent; v != basetype.Uint16Invalid {
			out.DescentM += float64(v)
		}
	}
	var pts []geo.Point
	for i := range track.Lat {
		if !math.IsNaN(track.Lat[i]) && !math.IsNaN(track.Lon[i]) {
			pts = append(pts, geo.Point{Lat: track.Lat[i], Lon: track.Lon[i]})
		}
	}
	if b, ok := geo.BoundsOf(pts); ok {
		out.Bounds = &b
		out.Polyline = geo.EncodePolyline(geo.SimplifyTo(pts, 500))
	}
	// Without laps, the track tells the length and the climb.
	if out.DistanceM == 0 {
		for i := len(track.Distance) - 1; i >= 0; i-- {
			if !math.IsNaN(track.Distance[i]) {
				out.DistanceM = track.Distance[i]
				break
			}
		}
	}
	if out.AscentM == 0 && out.DescentM == 0 {
		prev := math.NaN()
		for _, a := range track.Altitude {
			if math.IsNaN(a) {
				continue
			}
			if !math.IsNaN(prev) {
				if a > prev {
					out.AscentM += a - prev
				} else {
					out.DescentM += prev - a
				}
			}
			prev = a
		}
	}
	for _, p := range points {
		cp := CoursePoint{Type: word(p.Type.String()), Name: strings.TrimSpace(p.Name), Lat: deg(p.PositionLat), Lon: deg(p.PositionLong)}
		if p.Distance != basetype.Uint32Invalid {
			cp.DistanceM = float64(p.Distance) / 100
		}
		out.Points = append(out.Points, cp)
	}
	return out
}

func deg(semi int32) float64 {
	if semi == basetype.Sint32Invalid {
		return 0
	}
	return float64(semi) * (180.0 / (1 << 31))
}
