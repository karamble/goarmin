// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gcexport

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

// gmt is the export's layout of a UTC time without a zone.
const gmt = time.DateOnly + "T15:04:05.0"

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func dailyOf(r *Result) map[wellness.Kind]map[string]int64 {
	out := map[wellness.Kind]map[string]int64{}
	for _, d := range r.Daily {
		if out[d.Kind] == nil {
			out[d.Kind] = map[string]int64{}
		}
		out[d.Kind][d.Day] = d.Value
	}
	return out
}

func TestKind(t *testing.T) {
	for p, want := range map[string]string{
		"x.zip/DI_CONNECT/DI-Connect-Wellness/2025-06-27_2025-10-05_123_sleepData.json":           "sleep",
		"x.zip/DI_CONNECT/DI-Connect-Metrics/TrainingReadinessDTO_20230708_20231016_123.json":     "readiness",
		"x.zip/DI_CONNECT/DI-Connect-Metrics/RunningToleranceHistory_20250607_1_123.json":         "running_tolerance_history",
		"x.zip/DI_CONNECT/DI-Connect-Metrics/RunningTolerance_20250607_1_123.json":                "running_tolerance",
		"x.zip/DI_CONNECT/DI-Connect-Fitness/someone@example.invalid_0_summarizedActivities.json": "activities",
		"x.zip/DI_CONNECT/DI-Connect-User/user_profile.json":                                      "profile",
		"x.zip/DI_CONNECT/DI-GOLF/Golf-CLUB_TYPES.json":                                           "golf_club_types",
		"x.zip/DI_CONNECT/DI-GOLF/Golf-CLUB.json":                                                 "golf_clubs",
		"x.zip/INREACH/message-history.csv":                                                       "inreach_messages",
		"x.zip/IT_ORDERS/orders.json":                                                             "orders",
		"x.zip/DI_CONNECT_IQ/DI-CONNECT_IQ/user.json":                                             "connect_iq",
		"x.zip/somewhere/unknown.json":                                                            "other",
	} {
		if got := Kind(p); got != want {
			t.Errorf("Kind(%s) = %q, want %q", p, got, want)
		}
	}
}

// TestMetrics: daily values from the metric files, words turned into the watch's numbers.
func TestMetrics(t *testing.T) {
	day := time.Now().AddDate(0, 0, -3).UTC().Format(time.DateOnly)
	dayMS := float64(time.Now().AddDate(0, 0, -2).UTC().Truncate(24 * time.Hour).UnixMilli())
	read := func(name string, v any) *Result {
		r, err := Read("x.zip/DI_CONNECT/DI-Connect-Metrics/"+name, mustJSON(t, v))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := read("TrainingReadinessDTO_1_2_3.json", []map[string]any{{"calendarDate": day, "score": 71, "level": "HIGH", "recoveryTime": 600}})
	d := dailyOf(r)
	if d[wellness.TrainingReadiness][day] != 71 || d[wellness.ReadinessLevel][day] != 4 || d[wellness.RecoveryTime][day] != 600 {
		t.Fatalf("readiness %v", d)
	}
	r = read("MetricsAcuteTrainingLoad_1_2_3.json", []map[string]any{{"calendarDate": dayMS, "dailyTrainingLoadAcute": 310,
		"dailyTrainingLoadChronic": 280, "dailyAcuteChronicWorkloadRatio": 1.1, "acwrStatus": "OPTIMAL"}})
	day2 := time.UnixMilli(int64(dayMS)).UTC().Format(time.DateOnly)
	if d := dailyOf(r); d[wellness.AcuteLoad][day2] != 310 || d[wellness.ACWR][day2] != 110 || d[wellness.ACWRStatus][day2] != 2 {
		t.Fatalf("acute load %v", d)
	}
	r = read("TrainingHistory_1_2_3.json", []map[string]any{{"calendarDate": day, "trainingStatus": "PRODUCTIVE"}, {"calendarDate": day, "trainingStatus": "SOMETHING_NEW"}})
	if d := dailyOf(r); d[wellness.TrainingStatus][day] != 7 || len(r.Daily) != 1 {
		t.Fatalf("training status %v", d)
	}
	r = read("MetricsMaxMetData_1_2_3.json", []map[string]any{{"calendarDate": day, "vo2MaxValue": 52.3}})
	if d := dailyOf(r); d[wellness.VO2Max][day] != 5230 {
		t.Fatalf("vo2max %v", d)
	}
}

// TestSleepAndSummaries: a night with its scores and a nap; a daily summary with its nested parts.
func TestSleepAndSummaries(t *testing.T) {
	end := time.Now().Add(-30 * time.Hour).UTC().Truncate(time.Second)
	start := end.Add(-7 * time.Hour)
	f := gmt
	r, err := Read("a/DI-Connect-Wellness/x_sleepData.json", mustJSON(t, []map[string]any{{
		"sleepStartTimestampGMT": start.Format(f), "sleepEndTimestampGMT": end.Format(f), "calendarDate": end.Format(time.DateOnly),
		"deepSleepSeconds": 3600, "lightSleepSeconds": 14400, "remSleepSeconds": 5400, "awakeSleepSeconds": 1800,
		"sleepScores": map[string]any{"overallScore": 81, "qualityScore": 77},
		"napList":     []map[string]any{{"napStartTimestampGMT": end.Add(6 * time.Hour).Format(f), "napEndTimestampGMT": end.Add(7 * time.Hour).Format(f)}},
	}}))
	if err != nil || len(r.Sleep) != 1 || len(r.Naps) != 1 {
		t.Fatalf("sleep %+v %v", r, err)
	}
	if s := r.Sleep[0]; !s.Start.Equal(start) || s.Deep != 3600 || s.Score != 81 || s.QualityScore != 77 || s.DurationScore != wellness.Unknown {
		t.Fatalf("night %+v", s)
	}
	day := end.Format(time.DateOnly)
	r, err = Read("a/DI-Connect-Aggregator/UDSFile_x.json", mustJSON(t, []map[string]any{{
		"calendarDate": day, "totalSteps": 8123, "totalDistanceMeters": 6100, "floorsAscendedInMeters": 30.5, "restingHeartRate": 52,
		"allDayStress": map[string]any{"aggregatorList": []map[string]any{{"type": "TOTAL", "averageStressLevel": 24}, {"type": "AWAKE", "averageStressLevel": 39}}},
		"bodyBattery":  map[string]any{"chargedValue": 71, "bodyBatteryStatList": []map[string]any{{"bodyBatteryStatType": "HIGHEST", "statsValue": 81}}},
		"respiration":  map[string]any{"avgWakingRespirationValue": 16.0},
	}}))
	d := dailyOf(r)
	if err != nil || d[wellness.DaySteps][day] != 8123 || d[wellness.FloorsUp][day] != 3050 || d[wellness.DayStress][day] != 24 ||
		d[wellness.DayBBHigh][day] != 81 || d[wellness.BBCharged][day] != 71 || d[wellness.DayRespiration][day] != 1600 || d[wellness.RestingHR][day] != 52 {
		t.Fatalf("uds %v %v", d, err)
	}
}

// TestUnits: weights in grams, calories in kilojoules, distances and heights in centimetres,
// speeds in tenths of m/s, times in Unix milliseconds or Java's text.
func TestUnits(t *testing.T) {
	at := time.Now().Add(-50 * time.Hour).UTC().Truncate(time.Second)
	r, err := Read("a/DI-Connect-Wellness/1_userBioMetrics.json", mustJSON(t, []map[string]any{{
		"metaData": map[string]any{"calendarDate": at.Format(gmt)},
		"weight":   map[string]any{"weight": 92500.0, "timestampGMT": at.Format(gmt)}, "vo2MaxRunning": 51.0,
	}}))
	if err != nil || len(r.WeighIns) != 1 || r.WeighIns[0].Weight != 92.5 || !r.WeighIns[0].At.Equal(at) || r.WeighIns[0].Fat != wellness.Unknown {
		t.Fatalf("biometrics %+v %v", r.WeighIns, err)
	}
	r, err = Read("a/DI-Connect-Fitness/x_summarizedActivities.json", mustJSON(t, []map[string]any{{"summarizedActivitiesExport": []map[string]any{{
		"activityId": 1, "beginTimestamp": float64(at.UnixMilli()), "activityType": "running", "duration": 1800000.0, "distance": 500000.0,
		"avgSpeed": 0.2778, "calories": 1673.6, "elevationGain": 4200.0, "trainingEffectLabel": "TEMPO", "locationName": "Somewhere",
	}}}}))
	if err != nil || len(r.Activities) != 1 {
		t.Fatalf("activities %+v %v", r, err)
	}
	a := r.Activities[0]
	if a.Duration != 1800 || a.Distance != 5000 || math.Abs(a.AvgSpeed-2.778) > 1e-9 || a.Kcal != 400 || a.Ascent != 42 ||
		a.Extra["trainingEffectLabel"] != "TEMPO" || !a.Start.Equal(at) {
		t.Fatalf("activity %+v", a)
	}
	java := at.Format("Mon Jan 02 15:04:05 MST 2006")
	r, err = Read("a/DI-Connect-Fitness/x_personalRecord.json", mustJSON(t, []map[string]any{{"personalRecords": []map[string]any{{
		"personalRecordType": "Best 5km Run", "value": 1520.4, "prStartTimeGMT": java, "activityId": 9, "current": true}}}}))
	if err != nil || len(r.Records) != 1 || !r.Records[0].At.Equal(at) || r.Records[0].Value != 1520.4 || !r.Records[0].Current {
		t.Fatalf("records %+v %v", r.Records, err)
	}
}

// TestDocuments: account records stay documents; a UTF-16 CSV with a byte order mark is read.
func TestDocuments(t *testing.T) {
	r, err := Read("x.zip/IT_ORDERS/orders.json", []byte(`[{"orderNumber": 1, "orderTotal": 9.5}]`))
	if err != nil || r.Kind != "orders" || r.Document == nil {
		t.Fatalf("orders %+v %v", r, err)
	}
	text := "Name,Message\r\nA,Hello\r\n"
	b := []byte{0xFF, 0xFE}
	for _, c := range text {
		b = append(b, byte(c), 0)
	}
	r, err = Read("x.zip/INREACH/message-history.csv", b)
	rows, _ := r.Document.([]map[string]string)
	if err != nil || len(rows) != 1 || rows[0]["Message"] != "Hello" {
		t.Fatalf("csv %+v %v", r.Document, err)
	}
	if _, err := Read("x.zip/IT_ORDERS/orders.json", []byte("{not json")); err == nil {
		t.Fatal("a broken file was read")
	}
}
