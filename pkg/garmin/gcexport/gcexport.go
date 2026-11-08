// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package gcexport reads the files of a Garmin Connect account export (the zip Garmin sends on
// request): the daily metrics, summaries, sleep, health status, biometrics, hydration, ECG,
// gear, personal records and activities as typed records, and every other file (the account's
// records: profile, settings, orders, consents, inReach, …) as a document of plain values.
//
// The formats are Garmin's and change between exports: readers take what they find, skip what
// is missing, and keep a file they cannot read as a document of its raw contents.
package gcexport

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

// Result is what one export file holds. Kind names the file's sort ("sleep", "uds", …);
// Document is set for every file, typed parts only where a reader knows them.
type Result struct {
	Kind         string
	Daily        []wellness.Daily // each with Day set
	Sleep        []wellness.Sleep
	Naps         []wellness.Nap
	WeighIns     []wellness.WeighIn
	ECG          []wellness.ECG
	HealthStatus []HealthStatus
	Hydration    []Hydration
	Snapshots    []Snapshot
	Activities   []Activity
	Gear         []Gear
	Records      []Record
	// Document is the file as plain values (JSON objects, a CSV's rows, a GPX's text) for
	// files kept as documents; nil for the large metric files the tables hold.
	Document any
}

// HealthStatus is one day's health status for one metric (HRV, resting HR, respiration, skin
// temperature, pulse ox) against the personal baseline.
type HealthStatus struct {
	Day                   string
	Metric                string
	Value, Low, High, Pct float64 // NaN when absent
	Status                string
}

// Hydration is one logged drink (or an activity's sweat loss estimate).
type Hydration struct {
	At       time.Time
	Day      string
	ML       float64
	SweatML  float64
	Source   string
	Activity int64
}

// Snapshot is a health snapshot (a two-minute measurement the watch takes on request).
type Snapshot struct {
	Start, End time.Time
	Day        string
	Name       string
	Data       map[string]any // its summaries and time in zones, as the export has them
}

// Activity is an activity as Garmin Connect summarises it.
type Activity struct {
	ID                 int64
	Start              time.Time
	Name, Type, Sport  string
	Duration, Elapsed  float64 // s
	Distance           float64 // m
	Kcal               int
	AvgHR, MaxHR       int
	AvgSpeed, MaxSpeed float64 // m/s
	Ascent, Descent    float64 // m
	AerobicTE          float64
	AnaerobicTE        float64
	TrainingLoad       float64
	Extra              map[string]any // what only Garmin Connect knows: labels, location, …
}

// Gear is gear as Garmin Connect keeps it, with the activities it was used in.
type Gear struct {
	UUID, Type, Status, Name, Model string
	Begin                           string // date
	MaxMeters                       float64
	Activities                      []int64
	DefaultFor                      []string // activity types it is the default gear for
}

// Record is a personal record as Garmin Connect lists it.
type Record struct {
	Type     string // "Best 5km Run", "Most Steps in a Week", …
	At       time.Time
	Value    float64
	Activity int64
	Current  bool
}

// Kind names an export file's sort from its path. Names carry dates, account numbers and the
// account's e-mail address; the kind leaves them out.
func Kind(p string) string {
	base := path.Base(p)
	dir := path.Base(path.Dir(p))
	lower := strings.ToLower(base)
	ext := path.Ext(lower)
	for _, k := range []struct{ match, kind string }{
		{"sleepdata", "sleep"}, {"healthstatusdata", "health_status"}, {"nutritionlogs", "nutrition"},
		{"userbiometricprofiledata", "biometrics_profile"}, {"userbiometrics", "biometrics"}, {"biometrics_latest", "biometrics_latest"},
		{"heartratezones", "hr_zones"}, {"powerzones", "power_zones"}, {"fitnessagedata", "fitness_age"},
		{"wellnessactivities", "snapshots"}, {"udsfile", "uds"}, {"hydrationlogfile", "hydration"},
		{"trainingreadinessdto", "readiness"}, {"metricsacutetrainingload", "acute_load"}, {"traininghistory", "training_history"},
		{"metricsmaxmetdata", "max_met"}, {"runracepredictions", "race_predictions"}, {"endurancescore", "endurance"},
		{"hillscore", "hill"}, {"metricsheataltitudeacclimation", "heat_altitude"}, {"runningtolerancehistory", "running_tolerance_history"},
		{"runningtolerance", "running_tolerance"}, {"cyclingability", "cycling_ability"},
		{"summarizedactivities", "activities"}, {"personalrecord", "personal_records"}, {"trainingplan", "training_plan"},
		{"weekly_training_report", "weekly_report"}, {"workout", "workouts"}, {"gear", "gear"}, {"courses", "courses"},
		{"ecg_details", "ecg"}, {"golf-club_types", "golf_club_types"}, {"golf-club", "golf_clubs"},
		{"devicebackups", "device_backups"}, {"usergoal", "goals"}, {"user_profile_suica", "suica"}, {"user_profile", "profile"},
		{"user_settings", "settings"}, {"user_reminders", "reminders"}, {"social-profile", "social_profile"},
		{"comments", "comments"}, {"likes", "likes"}, {"profile-image", "profile_image"},
		{"livetrack", "livetrack"}, {"media-", "media"}, {"billinghistory", "inreach_billing"}, {"freetrialshistory", "inreach_trials"},
		{"message-history", "inreach_messages"}, {"addressbook", "inreach_addressbook"}, {"preset-messages", "inreach_presets"},
		{"mapdata-routes", "inreach_routes"}, {"mapdata-waypoints", "inreach_waypoints"}, {"consenthistory", "consents"},
		{"devicesandcontent", "devices"}, {"events", "events"}, {"orders", "orders"}, {"customer", "customer"},
	} {
		if strings.Contains(lower, k.match) {
			return k.kind
		}
	}
	switch {
	case dir == "DI-CONNECT_IQ" || strings.EqualFold(dir, "DI_CONNECT_IQ"):
		return "connect_iq"
	case ext == ".bak":
		return "device_backup_file"
	}
	return "other"
}

// Read reads one export file (its path in the export and its bytes).
func Read(p string, b []byte) (*Result, error) {
	r := &Result{Kind: Kind(p)}
	ext := strings.ToLower(path.Ext(p))
	switch ext {
	case ".json":
	case ".csv":
		rows, err := readCSV(b)
		if err != nil {
			return r, err
		}
		r.Document = rows
		return r, nil
	case ".gpx", ".kml", ".txt", ".xml":
		r.Document = map[string]any{"text": string(b)}
		return r, nil
	default:
		r.Document = map[string]any{"file": path.Base(p), "bytes": len(b)} // binary (an image, a device backup, a kmz)
		return r, nil
	}
	var v any
	if err := json.Unmarshal(bytes.TrimPrefix(b, []byte("\xEF\xBB\xBF")), &v); err != nil {
		return r, err
	}
	switch r.Kind {
	case "sleep":
		r.sleep(v)
	case "health_status":
		r.healthStatus(v)
	case "nutrition":
		r.each(v, func(m obj) { r.daily(wellness.CalorieGoal, m.day("calendarDate"), m.num("mfpCalorie.calorieGoal")) })
	case "biometrics":
		r.biometrics(v)
	case "fitness_age":
		r.each(v, func(m obj) {
			day := m.day("asOfDateGmt")
			r.daily(wellness.FitnessAge, day, math.Round(m.num("currentBioAge")))
			r.daily(wellness.BiometricVO2, day, m.num("biometricVo2Max")*100)
		})
	case "snapshots":
		r.snapshots(v)
	case "uds":
		r.uds(v)
	case "hydration":
		r.hydration(v)
	case "readiness":
		r.each(v, func(m obj) {
			day := m.day("calendarDate")
			r.daily(wellness.TrainingReadiness, day, m.num("score"))
			r.daily(wellness.ReadinessLevel, day, float64(readinessLevels[m.str("level")]))
			r.daily(wellness.RecoveryTime, day, m.num("recoveryTime"))
		})
	case "acute_load":
		r.each(v, func(m obj) {
			day := m.day("calendarDate")
			r.daily(wellness.AcuteLoad, day, m.num("dailyTrainingLoadAcute"))
			r.daily(wellness.ChronicLoad, day, m.num("dailyTrainingLoadChronic"))
			r.daily(wellness.ACWR, day, m.num("dailyAcuteChronicWorkloadRatio")*100)
			if s, ok := acwrStatuses[m.str("acwrStatus")]; ok {
				r.daily(wellness.ACWRStatus, day, float64(s))
			}
		})
	case "training_history":
		r.each(v, func(m obj) {
			if s, ok := trainingStatuses[m.str("trainingStatus")]; ok {
				r.daily(wellness.TrainingStatus, m.day("calendarDate"), float64(s))
			}
		})
	case "max_met":
		r.each(v, func(m obj) { r.daily(wellness.VO2Max, m.day("calendarDate"), m.num("vo2MaxValue")*100) })
	case "race_predictions":
		r.each(v, func(m obj) {
			day := m.day("calendarDate")
			r.daily(wellness.Race5K, day, m.num("raceTime5K"))
			r.daily(wellness.Race10K, day, m.num("raceTime10K"))
			r.daily(wellness.RaceHalf, day, m.num("raceTimeHalf"))
			r.daily(wellness.RaceMarathon, day, m.num("raceTimeMarathon"))
		})
	case "endurance":
		r.each(v, func(m obj) { r.daily(wellness.EnduranceScore, m.day("calendarDate"), m.num("overallScore")) })
	case "hill":
		r.each(v, func(m obj) {
			day := m.day("calendarDate")
			r.daily(wellness.HillScore, day, m.num("overallScore"))
			r.daily(wellness.HillStrength, day, m.num("strengthScore"))
			r.daily(wellness.HillEndurance, day, m.num("enduranceScore"))
		})
	case "heat_altitude":
		r.each(v, func(m obj) {
			day := m.day("calendarDate")
			r.daily(wellness.HeatAcclim, day, m.num("heatAcclimationPercentage"))
			r.daily(wellness.AltitudeAcclim, day, m.num("altitudeAcclimation"))
			r.daily(wellness.DailyAltitude, day, m.num("currentAltitude"))
		})
	case "running_tolerance":
		r.each(v, func(m obj) {
			day := m.day("calendarDate")
			r.daily(wellness.ImpactLoad, day, m.num("acuteImpactLoad"))
			r.daily(wellness.RunTolerance, day, m.num("acuteTolerance"))
		})
	case "running_tolerance_history":
		r.each(v, func(m obj) {
			day := m.day("calendarDate")
			r.daily(wellness.WeekImpact, day, m.num("totalImpactLoad"))
			r.daily(wellness.WeekTolerance, day, m.num("tolerance"))
		})
	case "cycling_ability":
		r.each(v, func(m obj) {
			day := m.day("calendarDate")
			r.daily(wellness.CyclingAerEnd, day, m.num("aerobicEndurance"))
			r.daily(wellness.CyclingAerCap, day, m.num("aerobicCapacity"))
			r.daily(wellness.CyclingAnaCap, day, m.num("anaerobicCapacity"))
		})
	case "activities":
		r.activities(v)
	case "gear":
		r.gear(v)
		r.Document = v
	case "personal_records":
		r.records(v)
		r.Document = v
	case "ecg":
		r.ecg(v)
	case "biometrics_latest", "biometrics_profile":
		r.each(v, func(m obj) {
			day := time.Now().UTC().Format(time.DateOnly) // "latest": as of the export
			r.daily(wellness.LactateThreshold, day, m.num("lactateThresholdHeartRate"))
			r.daily(wellness.FTP, day, m.num("functionalThresholdPower"))
		})
		r.Document = v
	default:
		r.Document = v
	}
	return r, nil
}

// readinessLevels, acwrStatuses and trainingStatuses turn Garmin Connect's words into the
// numbers the watch uses (training status as Gadgetbridge's TrainingStatus).
var (
	readinessLevels  = map[string]int{"POOR": 1, "LOW": 2, "MODERATE": 3, "HIGH": 4, "PRIME": 5}
	acwrStatuses     = map[string]int{"NONE": 0, "LOW": 1, "OPTIMAL": 2, "HIGH": 3, "VERY_HIGH": 4}
	trainingStatuses = map[string]int{"NO_STATUS": 0, "DETRAINING": 1, "UNPRODUCTIVE": 2, "OVERREACHING": 3, "MAINTAINING": 4,
		"RECOVERY": 5, "PEAKING": 6, "PRODUCTIVE": 7, "STRAINED": 8, "PAUSED": -1}
)

// daily adds a value for a day; a missing day or value (NaN) adds nothing.
func (r *Result) daily(k wellness.Kind, day string, v float64) {
	if day == "" || math.IsNaN(v) {
		return
	}
	at, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return
	}
	r.Daily = append(r.Daily, wellness.Daily{Kind: k, At: at.Add(12 * time.Hour), Day: day, Value: int64(math.Round(v))})
}

// each calls fn for every object of a list (or the object itself).
func (r *Result) each(v any, fn func(obj)) {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				fn(obj(m))
			}
		}
	case map[string]any:
		fn(obj(x))
	}
}

func (r *Result) sleep(v any) {
	r.each(v, func(m obj) {
		start, ok1 := m.time("sleepStartTimestampGMT")
		end, ok2 := m.time("sleepEndTimestampGMT")
		if ok1 && ok2 && end.After(start) {
			s := wellness.Sleep{Start: start, End: end, Score: m.int("sleepScores.overallScore"), QualityScore: m.int("sleepScores.qualityScore"),
				DurationScore: m.int("sleepScores.durationScore"), RecoveryScore: m.int("sleepScores.recoveryScore"),
				MinHR: wellness.Unknown, BodyBatteryStart: wellness.Unknown, BodyBatteryEnd: wellness.Unknown,
				Deep: m.int("deepSleepSeconds"), Light: m.int("lightSleepSeconds"), REM: m.int("remSleepSeconds"), Awake: m.int("awakeSleepSeconds"),
				Unmeasurable: m.int("unmeasurableSeconds")}
			r.Sleep = append(r.Sleep, s)
		}
		for _, n := range m.list("napList") {
			ns, ok1 := n.time("napStartTimestampGMT")
			ne, ok2 := n.time("napEndTimestampGMT")
			if ok1 && ok2 && ne.After(ns) {
				r.Naps = append(r.Naps, wellness.Nap{Start: ns, End: ne, Feedback: wellness.Unknown, Source: wellness.Unknown})
			}
		}
	})
}

func (r *Result) healthStatus(v any) {
	r.each(v, func(m obj) {
		day := m.day("calendarDate")
		for _, x := range m.list("metrics") {
			if day == "" || x.str("type") == "" {
				continue
			}
			r.HealthStatus = append(r.HealthStatus, HealthStatus{Day: day, Metric: strings.ToLower(x.str("type")), Value: x.num("value"),
				Low: x.num("baselineLowerLimit"), High: x.num("baselineUpperLimit"), Pct: x.num("percentage"), Status: strings.ToLower(x.str("status"))})
		}
	})
}

func (r *Result) biometrics(v any) {
	r.each(v, func(m obj) {
		day := m.day("metaData.calendarDate")
		if g := m.num("weight.weight"); !math.IsNaN(g) && g > 0 {
			at, ok := m.time("weight.timestampGMT")
			if !ok {
				at, ok = m.time("metaData.calendarDate")
			}
			if ok {
				const u = wellness.Unknown
				r.WeighIns = append(r.WeighIns, wellness.WeighIn{At: at, Weight: g / 1000, Fat: u, Hydration: u, Bone: u, Muscle: u,
					VisceralFat: u, BMI: u, BMR: u, ActiveMR: u, MetabolicAge: u, VisceralRating: u, Build: u})
			}
		}
		r.daily(wellness.VO2Max, day, m.num("vo2MaxRunning")*100)
		r.daily(wellness.LactateThreshold, day, m.num("lactateThresholdHearRate"))
		r.daily(wellness.FTP, day, m.num("functionalThresholdPower"))
	})
}

func (r *Result) snapshots(v any) {
	r.each(v, func(m obj) {
		start, ok1 := m.time("startTimestampGMT")
		end, ok2 := m.time("endTimestampGMT")
		if !ok1 || !ok2 {
			return
		}
		data := map[string]any{"type": m.str("wellnessActivityType"), "time_of_day": m.str("snapshotTimeOfDayType")}
		if x, ok := m["summaryTypeDataList"]; ok {
			data["summaries"] = x
		}
		if x, ok := m["timeInZoneList"]; ok {
			data["zones"] = x
		}
		r.Snapshots = append(r.Snapshots, Snapshot{Start: start, End: end, Day: m.day("calendarDate"), Name: m.str("activityName"), Data: data})
	})
}

func (r *Result) uds(v any) {
	r.each(v, func(m obj) {
		day := m.day("calendarDate")
		if day == "" {
			return
		}
		for k, key := range map[wellness.Kind]string{
			wellness.TotalKcal: "totalKilocalories", wellness.BMRKcal: "bmrKilocalories", wellness.DayActiveKcal: "activeKilocalories",
			wellness.DaySteps: "totalSteps", wellness.StepGoal: "dailyStepGoal", wellness.DayDistance: "totalDistanceMeters",
			wellness.DayModerate: "moderateIntensityMinutes", wellness.DayVigorous: "vigorousIntensityMinutes",
			wellness.IntensityGoal: "userIntensityMinutesGoal", wellness.FloorsGoal: "userFloorsAscendedGoal",
			wellness.MinHR: "minHeartRate", wellness.DayMaxHR: "maxHeartRate", wellness.RestingHR: "restingHeartRate",
			wellness.BBCharged: "bodyBattery.chargedValue", wellness.BBDrained: "bodyBattery.drainedValue",
		} {
			if x := m.num(key); x > 0 {
				r.daily(k, day, x)
			}
		}
		r.daily(wellness.FloorsUp, day, m.num("floorsAscendedInMeters")*100)
		r.daily(wellness.FloorsDown, day, m.num("floorsDescendedInMeters")*100)
		if x := m.num("respiration.avgWakingRespirationValue"); x > 0 {
			r.daily(wellness.DayRespiration, day, x*100)
		}
		for _, s := range m.list("allDayStress.aggregatorList") {
			if s.str("type") == "TOTAL" && s.num("averageStressLevel") >= 0 {
				r.daily(wellness.DayStress, day, s.num("averageStressLevel"))
			}
		}
		for _, s := range m.list("bodyBattery.bodyBatteryStatList") {
			switch s.str("bodyBatteryStatType") {
			case "HIGHEST":
				r.daily(wellness.DayBBHigh, day, s.num("statsValue"))
			case "LOWEST":
				r.daily(wellness.DayBBLow, day, s.num("statsValue"))
			}
		}
	})
}

func (r *Result) hydration(v any) {
	sums := map[string]float64{}
	r.each(v, func(m obj) {
		at, ok := m.time("persistedTimestampGMT")
		day := m.day("calendarDate")
		if !ok || day == "" {
			return
		}
		h := Hydration{At: at, Day: day, ML: zero(m.num("valueInML")), SweatML: zero(m.num("estimatedSweatLossInML")), Source: m.str("hydrationSource"),
			Activity: int64(zero(m.num("activityId")))}
		r.Hydration = append(r.Hydration, h)
		sums[day] += h.ML
	})
	for day, ml := range sums {
		r.daily(wellness.Hydration, day, ml)
	}
}

func (r *Result) activities(v any) {
	r.each(v, func(m obj) {
		for _, a := range m.list("summarizedActivitiesExport") {
			ms := a.num("beginTimestamp")
			if math.IsNaN(ms) {
				ms = a.num("startTimeGmt")
			}
			if math.IsNaN(ms) {
				continue
			}
			x := Activity{ID: int64(zero(a.num("activityId"))), Start: time.UnixMilli(int64(ms)).UTC(), Name: a.str("name"), Type: a.str("activityType"),
				Sport: a.str("sportType"), Duration: zero(a.num("duration")) / 1000, Elapsed: zero(a.num("elapsedDuration")) / 1000,
				Distance: zero(a.num("distance")) / 100, Kcal: int(math.Round(zero(a.num("calories")) / 4.184)), AvgHR: int(zero(a.num("avgHr"))),
				MaxHR: int(zero(a.num("maxHr"))), AvgSpeed: zero(a.num("avgSpeed")) * 10, MaxSpeed: zero(a.num("maxSpeed")) * 10,
				Ascent: zero(a.num("elevationGain")) / 100, Descent: zero(a.num("elevationLoss")) / 100, AerobicTE: zero(a.num("aerobicTrainingEffect")),
				AnaerobicTE: zero(a.num("anaerobicTrainingEffect")), TrainingLoad: zero(a.num("activityTrainingLoad")), Extra: map[string]any{}}
			for _, k := range []string{"trainingEffectLabel", "aerobicTrainingEffectMessage", "anaerobicTrainingEffectMessage", "locationName",
				"description", "moderateIntensityMinutes", "vigorousIntensityMinutes", "differenceBodyBattery", "waterEstimated", "lapCount",
				"steps", "vO2MaxValue", "favorite", "pr", "activityId"} {
				if y, ok := a[k]; ok && y != nil && y != "" {
					x.Extra[k] = y
				}
			}
			r.Activities = append(r.Activities, x)
		}
	})
}

func (r *Result) gear(v any) {
	r.each(v, func(m obj) {
		uses := map[string][]int64{}
		if acts, ok := m["gearActivityDTOs"].(map[string]any); ok {
			for _, list := range acts {
				if l, ok := list.([]any); ok {
					for _, e := range l {
						if a, ok := e.(map[string]any); ok {
							pk := strconv.FormatInt(int64(zero(obj(a).num("gearPk"))), 10)
							uses[pk] = append(uses[pk], int64(zero(obj(a).num("activityId"))))
						}
					}
				}
			}
		}
		defaults := map[string][]string{}
		for _, d := range m.list("gearActivityTypeDTOS") {
			if d.num("defaultFlag") == 1 {
				pk := strconv.FormatInt(int64(zero(d.num("gearPk"))), 10)
				defaults[pk] = append(defaults[pk], d.str("activityType"))
			}
		}
		for _, g := range m.list("gearDTOS") {
			pk := strconv.FormatInt(int64(zero(g.num("gearPk"))), 10)
			r.Gear = append(r.Gear, Gear{UUID: g.str("uuid"), Type: strings.ToLower(g.str("gearTypeName")), Status: strings.ToLower(g.str("gearStatusName")),
				Name: g.str("displayName"), Model: g.str("customMakeModel"), Begin: g.day("dateBegin"), MaxMeters: zero(g.num("maximumMeters")),
				Activities: uses[pk], DefaultFor: defaults[pk]})
		}
	})
}

func (r *Result) records(v any) {
	r.each(v, func(m obj) {
		for _, p := range m.list("personalRecords") {
			at, ok := p.time("prStartTimeGMT")
			if !ok {
				at, ok = p.time("createdDate")
			}
			if !ok || p.str("personalRecordType") == "" {
				continue
			}
			cur, _ := p["current"].(bool)
			r.Records = append(r.Records, Record{Type: p.str("personalRecordType"), At: at, Value: p.num("value"), Activity: int64(zero(p.num("activityId"))),
				Current: cur})
		}
	})
}

func (r *Result) ecg(v any) {
	r.each(v, func(m obj) {
		ms := m.num("summary.startTime")
		if math.IsNaN(ms) {
			return
		}
		e := wellness.ECG{Start: time.UnixMilli(int64(ms)).UTC(), Class: ecgClasses[m.str("summary.rhythmClassification")], Side: wellness.Unknown,
			AvgHR: m.num("summary.heartRateAverage"), RMSSD: m.int("summary.rmssdHrv"), Seconds: m.int("reading.durationInSeconds"),
			RawRate: m.num("reading.rawSampleRate"), SmoothRate: m.num("reading.sampleRate")}
		if strings.EqualFold(m.str("summary.mountingSide"), "LEFT") {
			e.Side = 0
		} else if strings.EqualFold(m.str("summary.mountingSide"), "RIGHT") {
			e.Side = 1
		}
		if s, ok := m.get("reading.samples").([]any); ok {
			for _, x := range s {
				if f, ok := x.(float64); ok {
					e.Smooth = append(e.Smooth, float32(f))
				}
			}
		}
		r.ECG = append(r.ECG, e)
	})
}

// ecgClasses maps Garmin Connect's rhythm words to the watch's ecg_rhythm_classification.
var ecgClasses = map[string]int{"SINUS_NORMAL": 0, "ATRIAL_FIBRILLATION": 1, "INCONCLUSIVE": 2, "INCONCLUSIVE_HIGH_HR": 3,
	"INCONCLUSIVE_LOW_HR": 4, "INCONCLUSIVE_POOR_RECORDING": 5}

// zero is v, or 0 for NaN.
func zero(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return v
}

// readCSV reads a CSV file (UTF-8, or UTF-16 with a byte order mark) into rows of named values.
func readCSV(b []byte) ([]map[string]string, error) {
	switch {
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}), bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		big := b[0] == 0xFE
		u := make([]uint16, 0, len(b)/2)
		for i := 2; i+1 < len(b); i += 2 {
			if big {
				u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
			} else {
				u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
			}
		}
		b = []byte(string(utf16.Decode(u)))
	case bytes.HasPrefix(b, []byte("\xEF\xBB\xBF")):
		b = b[3:]
	}
	cr := csv.NewReader(bytes.NewReader(b))
	cr.FieldsPerRecord = -1
	all, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return []map[string]string{}, nil
	}
	head := all[0]
	out := make([]map[string]string, 0, len(all)-1)
	for _, row := range all[1:] {
		m := map[string]string{}
		for i, v := range row {
			k := "column_" + strconv.Itoa(i+1)
			if i < len(head) && head[i] != "" {
				k = head[i]
			}
			m[k] = v
		}
		out = append(out, m)
	}
	return out, nil
}

// obj is a JSON object with dotted-path access.
type obj map[string]any

func (m obj) get(p string) any {
	var cur any = map[string]any(m)
	for _, part := range strings.Split(p, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[part]
	}
	return cur
}

// num is a number at p (NaN if absent or not a number).
func (m obj) num(p string) float64 {
	switch x := m.get(p).(type) {
	case float64:
		return x
	case string:
		if f, err := strconv.ParseFloat(x, 64); err == nil {
			return f
		}
	}
	return math.NaN()
}

// int is a whole number at p, or wellness.Unknown.
func (m obj) int(p string) int {
	if f := m.num(p); !math.IsNaN(f) {
		return int(math.Round(f))
	}
	return wellness.Unknown
}

func (m obj) str(p string) string {
	if s, ok := m.get(p).(string); ok {
		return s
	}
	return ""
}

func (m obj) list(p string) []obj {
	l, _ := m.get(p).([]any)
	out := make([]obj, 0, len(l))
	for _, e := range l {
		if mm, ok := e.(map[string]any); ok {
			out = append(out, obj(mm))
		}
	}
	return out
}

// time reads a time in any of the export's forms: Unix milliseconds, "2026-01-11T10:59:00.0"
// (UTC), RFC 3339, "2006-01-02", {"date": "…"}, or Java's "Mon Jul 03 22:00:00 GMT 2023".
func (m obj) time(p string) (time.Time, bool) {
	return parseTime(m.get(p))
}

// day is the date at p ("2006-01-02"; a timestamp's UTC date), "" if none.
func (m obj) day(p string) string {
	v := m.get(p)
	if s, ok := v.(string); ok && dateOnly.MatchString(s) {
		return s[:10]
	}
	if t, ok := parseTime(v); ok {
		return t.UTC().Format(time.DateOnly)
	}
	return ""
}

var dateOnly = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

var layouts = []string{"2006-01-02T15:04:05.0", "2006-01-02T15:04:05", "2006-01-02T15:04:05.000", "2006-01-02 15:04:05", time.RFC3339,
	"2006-01-02T15:04:05.0Z", time.DateOnly, "Mon Jan 02 15:04:05 MST 2006", "Jan 2, 2006", "Jan 2, 2006 3:04:05 PM"}

func parseTime(v any) (time.Time, bool) {
	switch x := v.(type) {
	case float64:
		if x > 1e11 {
			return time.UnixMilli(int64(x)).UTC(), true
		}
		if x > 1e8 {
			return time.Unix(int64(x), 0).UTC(), true
		}
	case string:
		for _, l := range layouts {
			if t, err := time.Parse(l, x); err == nil {
				return t.UTC(), true
			}
		}
	case map[string]any:
		return parseTime(x["date"])
	}
	return time.Time{}, false
}
