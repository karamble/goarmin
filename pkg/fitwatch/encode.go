// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fitwatch

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
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

// EncodeWorkout writes w as a FIT workout file, message for message as Garmin Connect writes
// one (docs/protocol/08): the description in a 160-byte field and whole in a memo, notes in
// memos, repeat steps with field 18, the workout's unnamed fields from w.Extra (else Connect's
// values for a user's workout, the total from Total). Repeat steps refer to the Index of their
// first step; they are renumbered in order.
func EncodeWorkout(w Workout, opt EncodeOptions) ([]byte, error) {
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
	software := uint16(garminConnectSoftware)
	if w.Extra != nil && w.Extra.Software != nil {
		software = *w.Extra.Software
	}
	msgs := []proto.Message{fileID(typedef.FileWorkout, opt), fileCreator(software)}
	wk := mesgdef.NewWorkout(nil)
	wk.WktName, wk.Sport, wk.SubSport = w.Name, sport, sub
	desc := w.Description // as given: an original keeps its text to the byte
	wk.WktDescription = descriptionField(desc)
	wk.Capabilities = typedef.WorkoutCapabilitiesTcx
	wk.NumValidSteps, wk.MessageIndex = uint16(len(w.Steps)), 0
	wk.UnknownFields = workoutFields(w)
	msgs = append(msgs, wk.ToMesg(nil))
	if desc != "" && (w.Extra == nil || !w.Extra.NoDescriptionMemo) {
		msgs = append(msgs, memos(typedef.MesgNumWorkout, 0, 17, desc)...)
	}
	secondary := w.Extra == nil || !w.Extra.NoSecondary

	type titleKey struct {
		cat typedef.ExerciseCategory
		num uint16
	}
	var titles []mesgdef.ExerciseTitle
	seen := map[titleKey]bool{}
	for i, s := range w.Steps {
		st, err := encodeStep(s, i, pos, secondary)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, st.ToMesg(nil))
		if notes := strings.TrimSpace(s.Notes); notes != "" && !IsRepeat(s) {
			msgs = append(msgs, memos(typedef.MesgNumWorkoutStep, i, 8, notes)...)
		}
		if s.Exercise != nil && st.ExerciseCategory != typedef.ExerciseCategoryInvalid && (w.Extra == nil || !w.Extra.NoTitles) {
			k := titleKey{st.ExerciseCategory, st.ExerciseName}
			t := strings.TrimSpace(s.Exercise.Title)
			if t == "" {
				t = ExerciseTitle(exerciseEnum(st.ExerciseCategory, st.ExerciseName))
			}
			if t != "" && !seen[k] {
				seen[k] = true
				et := mesgdef.NewExerciseTitle(nil)
				et.ExerciseCategory, et.ExerciseName, et.WktStepName = k.cat, k.num, []string{t}
				et.MessageIndex = typedef.MessageIndex(len(titles))
				titles = append(titles, *et)
			}
		}
	}
	if w.Extra != nil && len(w.Extra.Titles) > 0 {
		// The original's order where it has the exercise; the rest after, as they come.
		rank := map[titleKey]int{}
		for i, k := range w.Extra.Titles {
			rank[titleKey{typedef.ExerciseCategory(k[0]), k[1]}] = i + 1
		}
		at := func(t mesgdef.ExerciseTitle) int {
			if r := rank[titleKey{t.ExerciseCategory, t.ExerciseName}]; r > 0 {
				return r
			}
			return len(rank) + 1
		}
		slices.SortStableFunc(titles, func(a, b mesgdef.ExerciseTitle) int { return cmp.Compare(at(a), at(b)) })
	}
	for i := range titles {
		titles[i].MessageIndex = typedef.MessageIndex(i)
		msgs = append(msgs, titles[i].ToMesg(nil))
	}
	var buf bytes.Buffer
	// Big-endian records, as Garmin Connect writes them.
	if err := encoder.New(&buf, encoder.WithBigEndian()).Encode(&proto.FIT{Messages: msgs}); err != nil {
		return nil, fmt.Errorf("fitwatch: writing the workout: %w", err)
	}
	return buf.Bytes(), nil
}

// maxDescriptionField is the most bytes Garmin Connect puts in a workout's description field;
// a longer description is cut to 157 bytes and "...", and kept whole in a memo.
const maxDescriptionField = 160

// descriptionField is what a workout's description field holds.
func descriptionField(d string) string {
	if len(d) <= maxDescriptionField {
		return d
	}
	return cutBytes(d, maxDescriptionField-3) + "..."
}

// memoPartSize is the size of each part of a memo that does not fit one.
const memoPartSize = 255

// memos is the whole text of field of message parent as memo_glob messages, as Garmin Connect
// writes them: one part, the text and its terminating zero; or, when that is over 255 bytes,
// parts of 255 bytes numbered from 0, the last padded with zeros.
func memos(mesg typedef.MesgNum, parent int, field uint8, text string) []proto.Message {
	data := append([]byte(text), 0)
	var parts [][]byte
	if len(data) <= memoPartSize {
		parts = [][]byte{data}
	} else {
		for i := 0; i < len(data); i += memoPartSize {
			p := make([]byte, memoPartSize)
			copy(p, data[i:min(i+memoPartSize, len(data))])
			parts = append(parts, p)
		}
	}
	var out []proto.Message
	for i, p := range parts {
		m := mesgdef.NewMemoGlob(nil)
		m.PartIndex, m.MesgNum, m.ParentIndex, m.FieldNum = uint32(i), mesg, typedef.MessageIndex(parent), field
		m.Data = p
		out = append(out, m.ToMesg(nil))
	}
	return out
}

// workoutFields are the workout message's unnamed fields: w.Extra's, else what Garmin Connect
// writes for a workout a user saved (9=0, the total time in ms in 10 and 21, 23=0, field 16
// zeros). A total Extra lacks comes from Total.
func workoutFields(w Workout) []proto.Field {
	x := WorkoutExtra{}
	if w.Extra != nil {
		x = *w.Extra
	} else {
		zero := uint8(0)
		x.F9, x.F23, x.F16 = &zero, &zero, make([]byte, 16)
	}
	ms := uint32(Total(w.Steps)) * 1000
	if x.F10 == nil {
		x.F10 = &ms
	}
	if x.F21 == nil {
		x.F21 = &ms
	}
	var out []proto.Field
	u8 := func(num byte, v *uint8) {
		if v != nil {
			out = append(out, rawField(num, basetype.Enum, proto.Uint8(*v)))
		}
	}
	u32 := func(num byte, v *uint32) {
		if v != nil {
			out = append(out, rawField(num, basetype.Uint32, proto.Uint32(*v)))
		}
	}
	u8(9, x.F9)
	u32(10, x.F10)
	if x.F16 != nil {
		out = append(out, proto.Field{FieldBase: &proto.FieldBase{Num: 16, Name: "field_16", BaseType: basetype.Byte, Array: true, Scale: 1},
			Value: proto.SliceUint8(append([]byte(nil), x.F16...))})
	}
	u32(21, x.F21)
	u32(22, x.F22)
	u8(23, x.F23)
	u8(24, x.F24)
	return out
}

// ExerciseTitle is an exercise's display name from its FIT profile name ("side_plank" → "Side
// Plank"), for a step without a title of its own.
func ExerciseTitle(name string) string {
	if name == "" || strings.Contains(name, "Invalid") {
		return ""
	}
	w := strings.Fields(strings.ReplaceAll(strings.ToLower(name), "_", " "))
	for i, x := range w {
		w[i] = strings.ToUpper(x[:1]) + x[1:]
	}
	return strings.Join(w, " ")
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

func encodeStep(s Step, i int, pos map[int]int, secondary bool) (*mesgdef.WorkoutStep, error) {
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
			// Field 18 = 0 on every such step Garmin Connect writes (likely Connect's
			// skipLastRestStep or smartRepeat, both false).
			st.UnknownFields = []proto.Field{rawField(18, basetype.Enum, proto.Uint8(0))}
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
	// Garmin Connect writes a secondary target value of 0 on every step, and the secondary
	// target type open on a rest (an older writer neither).
	if secondary {
		st.SecondaryTargetValue = 0
	}
	if secondary && st.Intensity == typedef.IntensityRest {
		st.SecondaryTargetType = typedef.WktStepTargetOpen
	}
	switch s.Target {
	case "":
		// No target: a zero value and no type, as Garmin Connect writes a step without one.
		st.TargetValue = 0
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

// maxText is the most bytes a FIT text field holds, its terminating zero aside.
const maxText = 254

// cutBytes cuts s to at most n bytes, at a character boundary.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	b := []byte(s[:n])
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
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
