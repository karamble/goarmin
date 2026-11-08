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
	SkinTemp        Kind = 18 // °C ×100, the deviation from the personal baseline (overnight)
	Disruption      Kind = 19 // sleep disruption severity from this moment on (0 none … 3 high)
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
	Weight            Kind = 118 // g, the day's last weigh-in
	BodyFat           Kind = 119 // % ×100
	BMI               Kind = 120 // ×10
	SkinTempDeviation Kind = 121 // °C ×100, the night's average deviation from the baseline
	DisruptionNight   Kind = 122 // the night's sleep disruption severity (0 none … 3 high)
	LactateThreshold  Kind = 123 // bpm, the lactate threshold heart rate
	FTP               Kind = 124 // W, functional threshold power
	MaxHR             Kind = 125 // bpm, the maximum heart rate the watch works with
	RecoveryTime      Kind = 126 // min, the recovery time the watch gave that day
	DailyAltitude     Kind = 127 // m, the altitude the watch spent the day at
	FitnessAge        Kind = 128 // years
	AltitudeAcclim    Kind = 129 // m, the altitude the body is acclimatised to
	RunningLTPower    Kind = 130 // W, the running lactate threshold power

	// From a Garmin Connect export's daily summaries (UDS): a day without monitoring files
	// takes its summary from these.
	TotalKcal      Kind = 131 // kcal
	BMRKcal        Kind = 132 // kcal
	FloorsUp       Kind = 133 // cm climbed
	FloorsDown     Kind = 134 // cm descended
	IntensityGoal  Kind = 135 // min a week
	FloorsGoal     Kind = 136 // floors
	DaySteps       Kind = 137
	DayDistance    Kind = 138 // m
	DayActiveKcal  Kind = 139 // kcal
	DayMaxHR       Kind = 140 // bpm
	DayModerate    Kind = 141 // min
	DayVigorous    Kind = 142 // min
	DayStress      Kind = 143 // average stress
	DayBBHigh      Kind = 144
	DayBBLow       Kind = 145
	Hydration      Kind = 146 // ml drunk
	BiometricVO2   Kind = 147 // ml/kg/min ×100, the fitness age's VO2 max
	CalorieGoal    Kind = 148 // kcal
	ACWR           Kind = 149 // acute:chronic workload ratio ×100
	ACWRStatus     Kind = 150 // 0 none, 1 low, 2 optimal, 3 high, 4 very high
	HillStrength   Kind = 151
	HillEndurance  Kind = 152
	HeatAcclim     Kind = 153 // %
	ImpactLoad     Kind = 154 // running: acute impact load
	RunTolerance   Kind = 155 // running: acute tolerance
	WeekImpact     Kind = 156 // running: the week's impact load
	WeekTolerance  Kind = 157 // running: the week's tolerance
	CyclingAerEnd  Kind = 158 // cycling ability: aerobic endurance
	CyclingAerCap  Kind = 159 // aerobic capacity
	CyclingAnaCap  Kind = 160 // anaerobic capacity
	BBCharged      Kind = 161
	BBDrained      Kind = 162
	DayRespiration Kind = 163 // waking average, breaths per minute ×100
	ReadinessLevel Kind = 164 // 1 poor, 2 low, 3 moderate, 4 high, 5 prime
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
	SkinTemp:        {Name: "skin_temp", Unit: "°C", Scale: 0.01},
	Disruption:      {Name: "sleep_disruption", Unit: "", Scale: 1},

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
	Weight:            {Name: "weight", Unit: "kg", Scale: 0.001, Daily: true},
	BodyFat:           {Name: "body_fat", Unit: "%", Scale: 0.01, Daily: true},
	BMI:               {Name: "bmi", Unit: "kg/m²", Scale: 0.1, Daily: true},
	SkinTempDeviation: {Name: "skin_temp_deviation", Unit: "°C", Scale: 0.01, Daily: true},
	DisruptionNight:   {Name: "sleep_disruption_night", Unit: "", Scale: 1, Daily: true},
	LactateThreshold:  {Name: "lactate_threshold_hr", Unit: "bpm", Scale: 1, Daily: true},
	FTP:               {Name: "ftp", Unit: "W", Scale: 1, Daily: true},
	MaxHR:             {Name: "max_hr", Unit: "bpm", Scale: 1, Daily: true},
	RecoveryTime:      {Name: "recovery_time", Unit: "min", Scale: 1, Daily: true},
	DailyAltitude:     {Name: "daily_altitude", Unit: "m", Scale: 1, Daily: true},
	FitnessAge:        {Name: "fitness_age", Unit: "years", Scale: 1, Daily: true},
	AltitudeAcclim:    {Name: "altitude_acclimation", Unit: "m", Scale: 1, Daily: true},
	RunningLTPower:    {Name: "running_lt_power", Unit: "W", Scale: 1, Daily: true},
	TotalKcal:         {Name: "total_kcal", Unit: "kcal", Scale: 1, Daily: true},
	BMRKcal:           {Name: "bmr_kcal", Unit: "kcal", Scale: 1, Daily: true},
	FloorsUp:          {Name: "floors_up", Unit: "m", Scale: 0.01, Daily: true},
	FloorsDown:        {Name: "floors_down", Unit: "m", Scale: 0.01, Daily: true},
	IntensityGoal:     {Name: "intensity_goal", Unit: "min", Scale: 1, Daily: true},
	FloorsGoal:        {Name: "floors_goal", Unit: "floors", Scale: 1, Daily: true},
	DaySteps:          {Name: "day_steps", Unit: "steps", Scale: 1, Daily: true},
	DayDistance:       {Name: "day_distance", Unit: "m", Scale: 1, Daily: true},
	DayActiveKcal:     {Name: "day_active_kcal", Unit: "kcal", Scale: 1, Daily: true},
	DayMaxHR:          {Name: "day_max_hr", Unit: "bpm", Scale: 1, Daily: true},
	DayModerate:       {Name: "day_moderate_min", Unit: "min", Scale: 1, Daily: true},
	DayVigorous:       {Name: "day_vigorous_min", Unit: "min", Scale: 1, Daily: true},
	DayStress:         {Name: "day_stress", Unit: "", Scale: 1, Daily: true},
	DayBBHigh:         {Name: "day_bb_high", Unit: "%", Scale: 1, Daily: true},
	DayBBLow:          {Name: "day_bb_low", Unit: "%", Scale: 1, Daily: true},
	Hydration:         {Name: "hydration", Unit: "ml", Scale: 1, Daily: true},
	BiometricVO2:      {Name: "biometric_vo2max", Unit: "ml/kg/min", Scale: 0.01, Daily: true},
	CalorieGoal:       {Name: "calorie_goal", Unit: "kcal", Scale: 1, Daily: true},
	ACWR:              {Name: "acwr", Unit: "", Scale: 0.01, Daily: true},
	ACWRStatus:        {Name: "acwr_status", Unit: "", Scale: 1, Daily: true},
	HillStrength:      {Name: "hill_strength", Unit: "", Scale: 1, Daily: true},
	HillEndurance:     {Name: "hill_endurance", Unit: "", Scale: 1, Daily: true},
	HeatAcclim:        {Name: "heat_acclimation", Unit: "%", Scale: 1, Daily: true},
	ImpactLoad:        {Name: "impact_load", Unit: "", Scale: 1, Daily: true},
	RunTolerance:      {Name: "running_tolerance", Unit: "", Scale: 1, Daily: true},
	WeekImpact:        {Name: "week_impact_load", Unit: "", Scale: 1, Daily: true},
	WeekTolerance:     {Name: "week_running_tolerance", Unit: "", Scale: 1, Daily: true},
	CyclingAerEnd:     {Name: "cycling_aerobic_endurance", Unit: "", Scale: 1, Daily: true},
	CyclingAerCap:     {Name: "cycling_aerobic_capacity", Unit: "", Scale: 1, Daily: true},
	CyclingAnaCap:     {Name: "cycling_anaerobic_capacity", Unit: "", Scale: 1, Daily: true},
	BBCharged:         {Name: "bb_charged", Unit: "", Scale: 1, Daily: true},
	BBDrained:         {Name: "bb_drained", Unit: "", Scale: 1, Daily: true},
	DayRespiration:    {Name: "day_respiration", Unit: "brpm", Scale: 0.01, Daily: true},
	ReadinessLevel:    {Name: "readiness_level", Unit: "", Scale: 1, Daily: true},
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
