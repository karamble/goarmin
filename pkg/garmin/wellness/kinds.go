// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wellness

import "slices"

// Kind identifies a measured quantity. The numbers are a stored format: stores persist them,
// so a Kind is never renumbered or reused.
type Kind uint16

// Time series, from monitoring, sleep and HRV files.
const (
	HeartRate       Kind = 1  // bpm
	Stress          Kind = 2  // 0–100
	BodyBattery     Kind = 3  // 0–100
	Respiration     Kind = 4  // breaths per minute ×100
	SpO2            Kind = 5  // %
	Steps           Kind = 6  // running total since local midnight, per activity type (Sample.Sub)
	Distance        Kind = 7  // cm, running total per activity type
	ActiveCalories  Kind = 8  // kcal, running total per activity type
	ActiveTime      Kind = 9  // s, running total per activity type
	Intensity       Kind = 10 // intensity level 0–7 of the minute; Sub is the activity type
	ModerateMinutes Kind = 11 // moderate intensity minutes credited in the record
	VigorousMinutes Kind = 12 // vigorous intensity minutes credited in the record
	Ascent          Kind = 13 // mm, running total since local midnight
	Descent         Kind = 14 // mm, running total since local midnight
	Altitude        Kind = 15 // cm
	SleepStage      Kind = 16 // SleepLevel from this moment on
	HRV             Kind = 17 // ms ×128, a 5-minute value during sleep
)

// Daily values, one per local day.
const (
	RestingHR         Kind = 100 // bpm
	MinHR             Kind = 101 // bpm
	VO2Max            Kind = 102 // ml/kg/min ×100
	TrainingReadiness Kind = 103 // 0–100
	EnduranceScore    Kind = 104
	HillScore         Kind = 105
	AcuteLoad         Kind = 106
	ChronicLoad       Kind = 107
	Race5K            Kind = 108 // predicted time, s
	Race10K           Kind = 109
	RaceHalf          Kind = 110
	RaceMarathon      Kind = 111
	TrainingStatus    Kind = 112 // Garmin's enum
	StepGoal          Kind = 113 // steps
	SleepNeed         Kind = 114 // minutes, the sleep coach's recommendation for the coming night
	HRVLastNight      Kind = 115 // ms, the night's HRV as the HRV status shows it
	HRVTargetLow      Kind = 116 // ms, lower bound of the personal range
	HRVTargetHigh     Kind = 117 // ms, upper bound
)

// SleepLevel values of SleepStage samples (FIT sleep_level).
const (
	SleepUnmeasurable = 0
	SleepAwake        = 1
	SleepLight        = 2
	SleepDeep         = 3
	SleepREM          = 4
)

// KindInfo describes a Kind for display and aggregation.
type KindInfo struct {
	Name string // stable machine name, e.g. "heart_rate"
	Unit string
	// Scale turns a stored value into Unit: value × Scale.
	Scale float64
	// Total marks running totals that restart at local midnight: a day's amount is the
	// largest value of the day (summed over Sub for per-activity-type totals).
	Total bool
	// Daily marks one value per day.
	Daily bool
}

var kinds = map[Kind]KindInfo{
	HeartRate:       {Name: "heart_rate", Unit: "bpm", Scale: 1},
	Stress:          {Name: "stress", Unit: "", Scale: 1},
	BodyBattery:     {Name: "body_battery", Unit: "%", Scale: 1},
	Respiration:     {Name: "respiration", Unit: "brpm", Scale: 0.01},
	SpO2:            {Name: "spo2", Unit: "%", Scale: 1},
	Steps:           {Name: "steps", Unit: "steps", Scale: 1, Total: true},
	Distance:        {Name: "distance", Unit: "m", Scale: 0.01, Total: true},
	ActiveCalories:  {Name: "active_calories", Unit: "kcal", Scale: 1, Total: true},
	ActiveTime:      {Name: "active_time", Unit: "s", Scale: 1, Total: true},
	Intensity:       {Name: "intensity", Unit: "", Scale: 1},
	ModerateMinutes: {Name: "moderate_minutes", Unit: "min", Scale: 1},
	VigorousMinutes: {Name: "vigorous_minutes", Unit: "min", Scale: 1},
	Ascent:          {Name: "ascent", Unit: "m", Scale: 0.001, Total: true},
	Descent:         {Name: "descent", Unit: "m", Scale: 0.001, Total: true},
	Altitude:        {Name: "altitude", Unit: "m", Scale: 0.01},
	SleepStage:      {Name: "sleep_stage", Unit: "", Scale: 1},
	HRV:             {Name: "hrv", Unit: "ms", Scale: 1.0 / 128},

	RestingHR:         {Name: "resting_hr", Unit: "bpm", Scale: 1, Daily: true},
	MinHR:             {Name: "min_hr", Unit: "bpm", Scale: 1, Daily: true},
	VO2Max:            {Name: "vo2max", Unit: "ml/kg/min", Scale: 0.01, Daily: true},
	TrainingReadiness: {Name: "training_readiness", Unit: "", Scale: 1, Daily: true},
	EnduranceScore:    {Name: "endurance_score", Unit: "", Scale: 1, Daily: true},
	HillScore:         {Name: "hill_score", Unit: "", Scale: 1, Daily: true},
	AcuteLoad:         {Name: "acute_load", Unit: "", Scale: 1, Daily: true},
	ChronicLoad:       {Name: "chronic_load", Unit: "", Scale: 1, Daily: true},
	Race5K:            {Name: "race_5k", Unit: "s", Scale: 1, Daily: true},
	Race10K:           {Name: "race_10k", Unit: "s", Scale: 1, Daily: true},
	RaceHalf:          {Name: "race_half", Unit: "s", Scale: 1, Daily: true},
	RaceMarathon:      {Name: "race_marathon", Unit: "s", Scale: 1, Daily: true},
	TrainingStatus:    {Name: "training_status", Unit: "", Scale: 1, Daily: true},
	StepGoal:          {Name: "step_goal", Unit: "steps", Scale: 1, Daily: true},
	SleepNeed:         {Name: "sleep_need", Unit: "min", Scale: 1, Daily: true},
	HRVLastNight:      {Name: "hrv_last_night", Unit: "ms", Scale: 1, Daily: true},
	HRVTargetLow:      {Name: "hrv_target_low", Unit: "ms", Scale: 1, Daily: true},
	HRVTargetHigh:     {Name: "hrv_target_high", Unit: "ms", Scale: 1, Daily: true},
}

// Info describes k; an unknown Kind has an empty Name.
func (k Kind) Info() KindInfo { return kinds[k] }

func (k Kind) String() string {
	if i, ok := kinds[k]; ok {
		return i.Name
	}
	return "kind_unknown"
}

// Kinds returns every known Kind, ascending.
func Kinds() []Kind {
	out := make([]Kind, 0, len(kinds))
	for k := range kinds {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// KindByName finds a Kind by its Info().Name.
func KindByName(name string) (Kind, bool) {
	for k, i := range kinds {
		if i.Name == name {
			return k, true
		}
	}
	return 0, false
}
