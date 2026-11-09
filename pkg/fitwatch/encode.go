// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fitwatch

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"
)

// Writing workout and schedule files as Garmin Connect does when it sends them to a watch
// (docs/protocol/08): the file_id names Garmin Connect (manufacturer Garmin, product 65534),
// a workout carries its steps, notes as memo_glob and exercise titles, and a schedule names
// each workout by that workout file's own file_id.

// ConnectProduct is Garmin Connect's product number in the file_id of the files it writes.
const ConnectProduct = 65534

// FileID is a FIT file's identity: a schedule names its workout by it.
type FileID struct {
	Manufacturer uint16
	Product      uint16
	Serial       uint32
	Created      time.Time
}

// EncodeOptions sets a written file's identity. Serial should differ from file to file: the
// watch tells a schedule's workouts apart by serial and time.
type EncodeOptions struct {
	Serial  uint32
	Created time.Time
}

// ID is the FileID a file written with these options carries.
func (o EncodeOptions) ID() FileID {
	return FileID{Manufacturer: uint16(typedef.ManufacturerGarmin), Product: ConnectProduct, Serial: o.Serial, Created: o.Created.Truncate(time.Second)}
}

// Limits a watch takes (Garmin Connect's and Gadgetbridge's).
const (
	MaxWorkoutName  = 40
	MaxStepNotes    = 200
	MaxWorkoutSteps = 100
)

// ErrInvalidWorkout wraps every reason a workout cannot be written.
var ErrInvalidWorkout = errors.New("fitwatch: invalid workout")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidWorkout, fmt.Sprintf(format, a...))
}

// garminConnectSoftware is the file_creator version in Garmin Connect's workout files.
const garminConnectSoftware = 2621

func fileID(typ typedef.File, opt EncodeOptions) proto.Message {
	m := mesgdef.NewFileId(nil)
	m.Type, m.Manufacturer, m.Product, m.SerialNumber, m.TimeCreated = typ, typedef.ManufacturerGarmin, ConnectProduct, opt.Serial, opt.Created
	return m.ToMesg(nil)
}

func fileCreator(software uint16) proto.Message {
	m := mesgdef.NewFileCreator(nil)
	m.SoftwareVersion, m.HardwareVersion = software, 0
	return m.ToMesg(nil)
}

// rawField is a field the FIT library's profile does not name, written by number.
func rawField(num byte, bt basetype.BaseType, v proto.Value) proto.Field {
	return proto.Field{FieldBase: &proto.FieldBase{Num: num, Name: fmt.Sprintf("field_%d", num), BaseType: bt, Scale: 1}, Value: v}
}

// EncodeWorkout writes w as a FIT workout file. description is the workout's text (empty for
// none). Repeat steps refer to the Index of their first step; they are renumbered in order.
func EncodeWorkout(w Workout, description string, opt EncodeOptions) ([]byte, error) {
	if err := ValidateWorkout(w); err != nil {
		return nil, err
	}
	sport := typedef.SportFromString(w.Sport)
	sub := typedef.SubSportGeneric
	if w.SubSport != "" {
		sub = typedef.SubSportFromString(w.SubSport)
	}
	pos := map[int]int{} // step Index → message index
	for i, s := range w.Steps {
		pos[s.Index] = i
	}
	msgs := []proto.Message{fileID(typedef.FileWorkout, opt), fileCreator(garminConnectSoftware)}
	wk := mesgdef.NewWorkout(nil)
	wk.WktName, wk.Sport, wk.SubSport = w.Name, sport, sub
	wk.WktDescription = strings.TrimSpace(description)
	wk.Capabilities = typedef.WorkoutCapabilitiesTcx
	wk.NumValidSteps, wk.MessageIndex = uint16(len(w.Steps)), 0
	if est := Estimate(w.Steps); est > 0 {
		// Garmin Connect's total: duration type time (field 9), value and time in ms (10, 21),
		// time_flag 0 (23).
		ms := uint32(est) * 1000
		wk.UnknownFields = []proto.Field{
			rawField(9, basetype.Enum, proto.Uint8(uint8(typedef.WktStepDurationTime))),
			rawField(10, basetype.Uint32, proto.Uint32(ms)),
			rawField(21, basetype.Uint32, proto.Uint32(ms)),
			rawField(23, basetype.Enum, proto.Uint8(0)),
		}
	}
	// Field 16: 16 bytes in every workout Garmin Connect writes, zeros (once ending in 1).
	wk.UnknownFields = append(wk.UnknownFields, proto.Field{FieldBase: &proto.FieldBase{Num: 16, Name: "field_16", BaseType: basetype.Byte,
		Array: true, Scale: 1}, Value: proto.SliceUint8(make([]byte, 16))})
	msgs = append(msgs, wk.ToMesg(nil))

	type titleKey struct {
		cat typedef.ExerciseCategory
		num uint16
	}
	var titles []mesgdef.ExerciseTitle
	seen := map[titleKey]bool{}
	for i, s := range w.Steps {
		st, err := encodeStep(s, i, pos)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, st.ToMesg(nil))
		if notes := strings.TrimSpace(s.Notes); notes != "" && !IsRepeat(s) {
			msgs = append(msgs, memo(notes, i))
		}
		if s.Exercise != nil && st.ExerciseCategory != typedef.ExerciseCategoryInvalid {
			k := titleKey{st.ExerciseCategory, st.ExerciseName}
			if t := strings.TrimSpace(s.Exercise.Title); t != "" && !seen[k] {
				seen[k] = true
				et := mesgdef.NewExerciseTitle(nil)
				et.ExerciseCategory, et.ExerciseName, et.WktStepName = k.cat, k.num, []string{t}
				et.MessageIndex = typedef.MessageIndex(len(titles))
				titles = append(titles, *et)
			}
		}
	}
	for i := range titles {
		msgs = append(msgs, titles[i].ToMesg(nil))
	}
	var buf bytes.Buffer
	// Big-endian records, as Garmin Connect writes them.
	if err := encoder.New(&buf, encoder.WithBigEndian()).Encode(&proto.FIT{Messages: msgs}); err != nil {
		return nil, fmt.Errorf("fitwatch: writing the workout: %w", err)
	}
	return buf.Bytes(), nil
}

// memo is a step's notes as a memo_glob, the whole text (Garmin Connect writes it beside the
// step's notes field).
func memo(notes string, step int) proto.Message {
	b := []byte(notes)
	if len(b) > 254 {
		b = b[:254]
		for len(b) > 0 && !utf8.Valid(b) {
			b = b[:len(b)-1]
		}
	}
	m := mesgdef.NewMemoGlob(nil)
	m.PartIndex, m.MesgNum, m.ParentIndex, m.FieldNum = 0, typedef.MesgNumWorkoutStep, typedef.MessageIndex(step), 8
	m.Data = append(b, 0)
	return m.ToMesg(nil)
}

// hr encodes a heart-rate value of a step: a % of max as is, bpm + 100.
func hr(v float64, pct bool) uint32 {
	if pct {
		return uint32(math.Round(v))
	}
	return uint32(math.Round(v)) + 100
}

// power encodes a power value of a step: a % of FTP as is, W + 1000.
func power(v float64, pct bool) uint32 {
	if pct {
		return uint32(math.Round(v))
	}
	return uint32(math.Round(v)) + 1000
}

func encodeStep(s Step, i int, pos map[int]int) (*mesgdef.WorkoutStep, error) {
	st := mesgdef.NewWorkoutStep(nil)
	st.MessageIndex = typedef.MessageIndex(i)
	if IsRepeat(s) {
		from, ok := pos[s.From]
		if !ok || from >= i {
			return nil, invalid("step %d repeats from a step that does not come before it", i+1)
		}
		st.DurationValue = uint32(from)
		if s.Duration == "repeat" {
			st.DurationType, st.TargetValue = typedef.WktStepDurationRepeatUntilStepsCmplt, uint32(s.Repeat)
		} else {
			st.DurationType, st.TargetValue = typedef.WktStepDurationRepeatUntilTime, uint32(math.Round(s.DurationValue*1000))
		}
		return st, nil
	}
	st.WktStepName = strings.TrimSpace(s.Name)
	if n := strings.TrimSpace(s.Notes); n != "" {
		st.Notes = truncateRunes(n, 50)
	}
	st.Intensity = typedef.IntensityActive
	if s.Intensity != "" {
		if st.Intensity = typedef.IntensityFromString(s.Intensity); st.Intensity == typedef.IntensityInvalid {
			return nil, invalid("step %d: unknown intensity %q", i+1, s.Intensity)
		}
	}
	v := s.DurationValue
	switch s.Duration {
	case "time":
		st.DurationType, st.DurationValue = typedef.WktStepDurationTime, uint32(math.Round(v*1000))
	case "distance":
		st.DurationType, st.DurationValue = typedef.WktStepDurationDistance, uint32(math.Round(v*100))
	case "reps":
		st.DurationType, st.DurationValue = typedef.WktStepDurationReps, uint32(math.Round(v))
	case "calories":
		st.DurationType, st.DurationValue = typedef.WktStepDurationCalories, uint32(math.Round(v))
	case "open", "":
		st.DurationType = typedef.WktStepDurationOpen
	case "hr_less_than", "hr_greater_than":
		st.DurationType, st.DurationValue = typedef.WktStepDurationFromString(s.Duration), hr(v, s.TargetPct)
	case "power_less_than", "power_greater_than":
		st.DurationType, st.DurationValue = typedef.WktStepDurationFromString(s.Duration), power(v, s.TargetPct)
	default:
		if st.DurationType = typedef.WktStepDurationFromString(s.Duration); st.DurationType == typedef.WktStepDurationInvalid {
			return nil, invalid("step %d: unknown duration %q", i+1, s.Duration)
		}
		st.DurationValue = uint32(math.Round(v))
	}
	switch s.Target {
	case "":
		// No target: a zero value and no type, as Garmin Connect writes a step without one.
		st.TargetValue, st.SecondaryTargetValue = 0, 0
	case "open":
		st.TargetType, st.TargetValue = typedef.WktStepTargetOpen, 0
	default:
		if st.TargetType = typedef.WktStepTargetFromString(s.Target); st.TargetType == typedef.WktStepTargetInvalid {
			return nil, invalid("step %d: unknown target %q", i+1, s.Target)
		}
		if s.TargetZone > 0 {
			st.TargetValue = uint32(s.TargetZone)
			break
		}
		if s.TargetLow <= 0 && s.TargetHigh <= 0 {
			return nil, invalid("step %d: a %s target needs a zone or a range", i+1, s.Target)
		}
		st.TargetValue = 0
		lo, hi := s.TargetLow, s.TargetHigh
		switch st.TargetType {
		case typedef.WktStepTargetHeartRate:
			st.CustomTargetValueLow, st.CustomTargetValueHigh = hr(lo, s.TargetPct), hr(hi, s.TargetPct)
		case typedef.WktStepTargetPower, typedef.WktStepTargetPower3S, typedef.WktStepTargetPower10S,
			typedef.WktStepTargetPower30S, typedef.WktStepTargetPowerLap:
			st.CustomTargetValueLow, st.CustomTargetValueHigh = power(lo, s.TargetPct), power(hi, s.TargetPct)
		case typedef.WktStepTargetSpeed, typedef.WktStepTargetSpeedLap:
			st.CustomTargetValueLow, st.CustomTargetValueHigh = uint32(math.Round(lo*1000)), uint32(math.Round(hi*1000))
		default:
			st.CustomTargetValueLow, st.CustomTargetValueHigh = uint32(math.Round(lo)), uint32(math.Round(hi))
		}
	}
	if s.Exercise != nil {
		cat := typedef.ExerciseCategoryFromString(s.Exercise.Category)
		if cat == typedef.ExerciseCategoryInvalid {
			return nil, invalid("step %d: unknown exercise category %q", i+1, s.Exercise.Category)
		}
		st.ExerciseCategory, st.ExerciseName = cat, uint16(s.Exercise.Number)
	}
	st.WeightDisplayUnit = typedef.FitBaseUnitKilogram
	if s.WeightKg > 0 {
		st.ExerciseWeight = uint16(math.Round(s.WeightKg * 100))
	}
	return st, nil
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// ValidateWorkout checks a workout against what a watch takes: a name, a known sport, at most
// MaxWorkoutSteps steps, repeats of earlier steps and not nested, notes within MaxStepNotes.
func ValidateWorkout(w Workout) error {
	switch name := strings.TrimSpace(w.Name); {
	case name == "":
		return invalid("the workout needs a name")
	case utf8.RuneCountInString(name) > MaxWorkoutName:
		return invalid("the name is longer than %d characters", MaxWorkoutName)
	}
	if typedef.SportFromString(w.Sport) == typedef.SportInvalid {
		return invalid("unknown sport %q", w.Sport)
	}
	if w.SubSport != "" && typedef.SubSportFromString(w.SubSport) == typedef.SubSportInvalid {
		return invalid("unknown sub sport %q", w.SubSport)
	}
	if len(w.Steps) == 0 {
		return invalid("the workout has no steps")
	}
	if len(w.Steps) > MaxWorkoutSteps {
		return invalid("more than %d steps", MaxWorkoutSteps)
	}
	pos := map[int]int{}
	for i, s := range w.Steps {
		if _, dup := pos[s.Index]; dup {
			return invalid("two steps have index %d", s.Index)
		}
		pos[s.Index] = i
	}
	inRepeat := make([]bool, len(w.Steps))
	for i, s := range w.Steps {
		if utf8.RuneCountInString(s.Notes) > MaxStepNotes {
			return invalid("step %d: notes longer than %d characters", i+1, MaxStepNotes)
		}
		if !IsRepeat(s) {
			if s.Duration == "time" && s.DurationValue <= 0 || s.Duration == "distance" && s.DurationValue <= 0 ||
				s.Duration == "reps" && s.DurationValue < 1 {
				return invalid("step %d needs a %s greater than 0", i+1, s.Duration)
			}
			continue
		}
		from, ok := pos[s.From]
		switch {
		case !ok || from >= i:
			return invalid("step %d repeats from a step that does not come before it", i+1)
		case s.Duration == "repeat" && (s.Repeat < 2 || s.Repeat > 99):
			return invalid("step %d: repeat 2 to 99 times", i+1)
		case s.Duration == "repeat_until_time" && s.DurationValue <= 0:
			return invalid("step %d: repeat for a time greater than 0", i+1)
		}
		for j := from; j < i; j++ {
			if IsRepeat(w.Steps[j]) || inRepeat[j] {
				return invalid("step %d: repeats cannot hold repeats", i+1)
			}
			inRepeat[j] = true
		}
	}
	if IsRepeat(w.Steps[0]) {
		return invalid("the first step cannot be a repeat")
	}
	return nil
}

// ScheduleEntry is a workout on a day in a schedule file.
type ScheduleEntry struct {
	// Workout is the identity of the workout file on the watch the day names.
	Workout FileID
	// Day is the local date; only its year, month and day count.
	Day       time.Time
	Completed bool
}

// EncodeSchedule writes a FIT schedule file: one schedule message per entry, each naming its
// workout file by its file_id and the day as local midnight, as Garmin Connect does.
func EncodeSchedule(entries []ScheduleEntry, opt EncodeOptions) ([]byte, error) {
	msgs := []proto.Message{}
	id := mesgdef.NewFileId(nil)
	id.Type, id.Manufacturer, id.Product, id.SerialNumber, id.TimeCreated, id.Number = typedef.FileSchedules, typedef.ManufacturerGarmin,
		ConnectProduct, opt.Serial, opt.Created, 1
	msgs = append(msgs, id.ToMesg(nil), fileCreator(26))
	for _, e := range entries {
		if e.Workout.Serial == 0 || e.Workout.Created.IsZero() {
			return nil, errors.New("fitwatch: a schedule entry needs its workout file's serial and time")
		}
		y, m, d := e.Day.Date()
		s := mesgdef.NewSchedule(nil)
		s.Manufacturer, s.Product, s.SerialNumber, s.TimeCreated = typedef.Manufacturer(e.Workout.Manufacturer), e.Workout.Product,
			e.Workout.Serial, e.Workout.Created
		s.Type = typedef.ScheduleWorkout
		s.ScheduledTime = time.Date(y, m, d, 0, 0, 0, 0, time.UTC) // a local date-time: the day's midnight, written as is
		s.Completed = typedef.BoolFalse
		if e.Completed {
			s.Completed = typedef.BoolTrue
		}
		msgs = append(msgs, s.ToMesg(nil))
	}
	var buf bytes.Buffer
	if err := encoder.New(&buf, encoder.WithBigEndian()).Encode(&proto.FIT{Messages: msgs}); err != nil {
		return nil, fmt.Errorf("fitwatch: writing the schedule: %w", err)
	}
	return buf.Bytes(), nil
}
