// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fitwatch

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"

	"github.com/karamble/goarmin/pkg/fitid/fittest"
)

const serial = 3_500_000_001

func hrZones(limits ...uint8) []proto.Message {
	var out []proto.Message
	for i, v := range limits {
		z := mesgdef.NewHrZone(nil)
		z.MessageIndex, z.HighBpm, z.Name = typedef.MessageIndex(i), v, "HR Zone"
		out = append(out, z.ToMesg(nil))
	}
	return out
}

func TestSettings(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	dev := mesgdef.NewDeviceSettings(nil)
	dev.TimeMode, dev.DateMode = []typedef.TimeMode{typedef.TimeModeHour24}, typedef.DateModeDayMonth
	u := mesgdef.NewUserProfile(nil)
	u.DistSetting, u.SpeedSetting, u.ElevSetting = typedef.DisplayMeasureMetric, typedef.DisplayMeasureMetric, typedef.DisplayMeasureStatute
	u.RestingHeartRate, u.HrSetting = 52, typedef.DisplayHeartBpm
	u.WakeTime, u.SleepTime = typedef.LocaltimeIntoDay(7*3600), typedef.LocaltimeIntoDay(23*3600)
	u.UserRunningStepLength, u.ActivityClass = 1150, typedef.ActivityClass(0x80|60)
	zt := mesgdef.NewZonesTarget(nil)
	zt.MaxHeartRate, zt.ThresholdHeartRate, zt.FunctionalThresholdPower, zt.HrCalcType = 183, 161, 255, typedef.HrZoneCalcPercentMaxHr
	zt2 := mesgdef.NewZonesTarget(nil)
	zt2.FunctionalThresholdPower = 329
	bike := mesgdef.NewBikeProfile(nil)
	bike.Name, bike.Odometer, bike.Enabled, bike.PowerEnabled = "Road", 1234567, typedef.BoolTrue, typedef.BoolTrue
	unused := mesgdef.NewBikeProfile(nil) // an empty slot
	unused.MessageIndex = 1
	msgs := []proto.Message{dev.ToMesg(nil), u.ToMesg(nil), zt.ToMesg(nil), zt2.ToMesg(nil), bike.ToMesg(nil), unused.ToMesg(nil)}
	msgs = append(msgs, hrZones(92, 110, 128, 146, 165, 183, 255, 255, 255, 255, 255, 255)...)
	f, err := Decode(fittest.Build(typedef.FileSettings, serial, now, msgs...))
	if err != nil {
		t.Fatal(err)
	}
	s := f.Settings
	if s == nil || s.Clock != "24h" || s.DateMode != "day_month" || s.Units.Distance != "metric" || s.Units.Elevation != "statute" ||
		s.RestingHR != 52 || s.WakeS != 7*3600 || s.SleepS != 23*3600 || s.RunStepM != 1.15 || s.ActivityLvl != 60 || !s.Athlete {
		t.Fatalf("settings %+v", s)
	}
	if len(s.Zones) != 2 || !reflect.DeepEqual(s.Zones[0].HR, []int{92, 110, 128, 146, 165, 183}) || s.Zones[0].MaxHR != 183 ||
		s.Zones[0].ThresholdHR != 161 || s.Zones[0].FTP != 255 || s.Zones[0].HRCalc != "percent_max_hr" || s.Zones[1].Valid() || s.Zones[1].FTP != 329 {
		t.Fatalf("zones %+v", s.Zones)
	}
	if len(s.Bikes) != 1 || s.Bikes[0].Name != "Road" || s.Bikes[0].OdometerM != 12345.67 || !s.Bikes[0].Enabled ||
		!reflect.DeepEqual(s.Bikes[0].Sensors, []string{"power"}) {
		t.Fatalf("bikes %+v", s.Bikes)
	}
	if !f.Created.Equal(now) || f.Type != typedef.FileSettings {
		t.Fatalf("file %v %v", f.Type, f.Created)
	}
}

func TestSportGoalsTotals(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	sp := mesgdef.NewSport(nil)
	sp.Sport, sp.SubSport, sp.Name = typedef.SportRunning, typedef.SubSportTrail, "Trail Run"
	zt := mesgdef.NewZonesTarget(nil)
	zt.MaxHeartRate, zt.HrCalcType = 190, typedef.HrZoneCalcPercentHrr
	f, err := Decode(fittest.Build(typedef.FileSport, serial, now, append([]proto.Message{sp.ToMesg(nil), zt.ToMesg(nil)}, hrZones(95, 114, 133, 152, 171, 190)...)...))
	if err != nil {
		t.Fatal(err)
	}
	if p := f.Sport; p == nil || p.Sport != "running" || p.SubSport != "trail" || p.Name != "Trail Run" || p.Zones.HRCalc != "percent_hrr" || p.Zones.HR[5] != 190 {
		t.Fatalf("sport %+v", f.Sport)
	}

	var goals []proto.Message
	for _, g := range []struct {
		typ typedef.Goal
		v   uint32
		src typedef.GoalSource
	}{{typedef.GoalAscent, 30479, typedef.GoalSourceUser}, {typedef.GoalSteps, 5890, typedef.GoalSourceAuto}, {typedef.GoalActiveMinutes, 150, typedef.GoalSourceUser}} {
		m := mesgdef.NewGoal(nil)
		m.Type, m.TargetValue, m.Source = g.typ, g.v, g.src
		goals = append(goals, m.ToMesg(nil))
	}
	f, err = Decode(fittest.Build(typedef.FileGoals, serial, now, goals...))
	if err != nil {
		t.Fatal(err)
	}
	want := []Goal{{Type: "ascent", Target: 30, Unit: "m", Source: "user"}, {Type: "steps", Target: 5890, Unit: "steps", Source: "auto"},
		{Type: "active_minutes", Target: 150, Unit: "min", Source: "user"}}
	if !reflect.DeepEqual(f.Goals, want) {
		t.Fatalf("goals %+v", f.Goals)
	}

	run := mesgdef.NewTotals(nil)
	run.Sport, run.TimerTime, run.Distance, run.Calories, run.Sessions = typedef.SportRunning, 6155, 11944, 1149, 3
	empty := mesgdef.NewTotals(nil) // the Fenix 8 sends one with no sport and no sessions
	empty.MessageIndex, empty.Sessions = 3, 0
	f, err = Decode(fittest.Build(typedef.FileTotals, serial, now, run.ToMesg(nil), empty.ToMesg(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Totals) != 1 || f.Totals[0] != (Totals{Sport: "running", TimerS: 6155, ElapsedS: -1, ActiveS: -1, DistanceM: 11944, Calories: 1149, Sessions: 3}) {
		t.Fatalf("totals %+v", f.Totals)
	}
}

func TestWorkout(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	w := mesgdef.NewWorkout(nil)
	w.WktName, w.Sport, w.NumValidSteps = "Intervals", typedef.SportRunning, 5
	step := func(i int, f func(s *mesgdef.WorkoutStep)) proto.Message {
		s := mesgdef.NewWorkoutStep(nil)
		s.MessageIndex = typedef.MessageIndex(i)
		f(s)
		return s.ToMesg(nil)
	}
	title := mesgdef.NewExerciseTitle(nil)
	title.ExerciseCategory, title.ExerciseName, title.WktStepName = typedef.ExerciseCategoryCore, 72, []string{"The Hundred"}
	b := fittest.Build(typedef.FileWorkout, serial, now, w.ToMesg(nil),
		step(0, func(s *mesgdef.WorkoutStep) {
			s.Intensity, s.DurationType, s.DurationValue = typedef.IntensityWarmup, typedef.WktStepDurationTime, 600000
		}),
		step(1, func(s *mesgdef.WorkoutStep) { // 400 m at 150–165 bpm
			s.Intensity, s.DurationType, s.DurationValue = typedef.IntensityActive, typedef.WktStepDurationDistance, 40000
			s.TargetType, s.TargetValue, s.CustomTargetValueLow, s.CustomTargetValueHigh = typedef.WktStepTargetHeartRate, 0, 250, 265
		}),
		step(2, func(s *mesgdef.WorkoutStep) { // 90 s recovery in zone 2
			s.Intensity, s.DurationType, s.DurationValue = typedef.IntensityRecovery, typedef.WktStepDurationTime, 90000
			s.TargetType, s.TargetValue = typedef.WktStepTargetHeartRate, 2
		}),
		step(3, func(s *mesgdef.WorkoutStep) { // 6 times from step 1
			s.DurationType, s.DurationValue, s.TargetValue = typedef.WktStepDurationRepeatUntilStepsCmplt, 1, 6
		}),
		step(4, func(s *mesgdef.WorkoutStep) {
			s.Intensity, s.DurationType, s.DurationValue = typedef.IntensityActive, typedef.WktStepDurationTime, 60000
			s.ExerciseCategory, s.ExerciseName = typedef.ExerciseCategoryCore, 72
		}),
		step(5, func(s *mesgdef.WorkoutStep) {
			s.Intensity, s.DurationType, s.DurationValue = typedef.IntensityActive, typedef.WktStepDurationReps, 12
			s.ExerciseCategory, s.ExerciseName, s.ExerciseWeight = typedef.ExerciseCategoryPlank, 0, 1000
		}),
		title.ToMesg(nil),
	)
	f, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	wk := f.Workout
	if wk == nil || wk.Name != "Intervals" || wk.Sport != "running" || len(wk.Steps) != 6 {
		t.Fatalf("workout %+v", wk)
	}
	s := wk.Steps
	if s[0].Intensity != "warmup" || s[0].Duration != "time" || s[0].DurationValue != 600 {
		t.Fatalf("warm-up %+v", s[0])
	}
	if s[1].Duration != "distance" || s[1].DurationValue != 400 || s[1].Target != "heart_rate" || s[1].TargetLow != 150 || s[1].TargetHigh != 165 || s[1].TargetPct {
		t.Fatalf("interval %+v", s[1])
	}
	if s[2].TargetZone != 2 || s[3].Duration != "repeat" || s[3].From != 1 || s[3].Repeat != 6 {
		t.Fatalf("recovery %+v, repeat %+v", s[2], s[3])
	}
	if s[4].Exercise == nil || s[4].Exercise.Name != "The Hundred" || s[4].Exercise.Category != "core" {
		t.Fatalf("exercise with a title %+v", s[4].Exercise)
	}
	if s[5].Exercise == nil || s[5].Exercise.Name == "" || s[5].Exercise.Title != "" || s[5].WeightKg != 10 || s[5].Duration != "reps" {
		t.Fatalf("exercise by the profile's name %+v", s[5])
	}
	// 600 + 90 + (90 × 5 more rounds) + 60: distance and reps steps are not timed.
	if wk.EstimatedS != 600+90+450+60 {
		t.Fatalf("estimate %d", wk.EstimatedS)
	}
}

func TestCourse(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	c := mesgdef.NewCourse(nil)
	c.Name, c.Sport = "Lakeside loop", typedef.SportRunning
	var msgs []proto.Message
	msgs = append(msgs, c.ToMesg(nil))
	semi := func(d float64) int32 { return int32(math.Round(d / (180.0 / (1 << 31)))) }
	for i := 0; i < 50; i++ {
		r := mesgdef.NewRecord(nil)
		r.Timestamp = now.Add(time.Duration(i) * time.Second)
		r.PositionLat, r.PositionLong = semi(45+float64(i)*0.0001), semi(7+float64(i%10)*0.0001)
		r.Distance = uint32(i * 1000) // cm
		r.EnhancedAltitude = uint32((400 + i%5 + 500) * 5)
		msgs = append(msgs, r.ToMesg(nil))
	}
	p := mesgdef.NewCoursePoint(nil)
	p.Type, p.Name, p.Distance = typedef.CoursePointWater, "Fountain", 25000
	p.PositionLat, p.PositionLong = semi(45.0025), semi(7.0005)
	msgs = append(msgs, p.ToMesg(nil))
	f, err := Decode(fittest.Build(typedef.FileCourse, serial, now, msgs...))
	if err != nil {
		t.Fatal(err)
	}
	co := f.Course
	if co == nil || co.Name != "Lakeside loop" || co.Sport != "running" || co.Track.Len() != 50 || co.Polyline == "" || co.Bounds == nil {
		t.Fatalf("course %+v", co)
	}
	if co.DistanceM != 490 || co.AscentM <= 0 {
		t.Fatalf("length %.1f m, climb %.1f m", co.DistanceM, co.AscentM)
	}
	if len(co.Points) != 1 || co.Points[0].Type != "water" || co.Points[0].Name != "Fountain" || co.Points[0].DistanceM != 250 ||
		math.Abs(co.Points[0].Lat-45.0025) > 1e-6 {
		t.Fatalf("points %+v", co.Points)
	}
}

func TestNotAWatchFile(t *testing.T) {
	if _, err := Decode(fittest.File(typedef.FileActivity, serial, time.Now(), 3)); err != ErrNotWatchFile {
		t.Fatalf("activity file: %v", err)
	}
}
