// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wellness

import (
	"testing"
	"time"

	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"

	"github.com/karamble/goarmin/pkg/fitid/fittest"
)

type F = fittest.F

func ft(t time.Time) proto.Value { return proto.Uint32(fittest.FITTime(t)) }

func byKind(d *Data, k Kind) []Sample {
	var out []Sample
	for _, s := range d.Samples {
		if s.Kind == k {
			out = append(out, s)
		}
	}
	return out
}

// TestTimestamp16Wrap: a timestamp_16 past the 16-bit wrap of the last full timestamp.
func TestTimestamp16Wrap(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	// A full timestamp just below a multiple of 65536 FIT seconds, then one 100 s later.
	full := int64(fittest.FITTime(now))
	full -= full%65536 - 65500
	at := FITTime(full)
	later := at.Add(100 * time.Second)
	b := fittest.Build(typedef.FileMonitoringB, 1, now,
		fittest.Message(55, F{Num: 253, Value: proto.Uint32(uint32(full))}, F{Num: 27, Value: proto.Uint8(70)}),
		fittest.Message(55, F{Num: 26, Value: proto.Uint16(uint16(fittest.FITTime(later)))}, F{Num: 27, Value: proto.Uint8(72)}),
	)
	d, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	hr := byKind(d, HeartRate)
	if len(hr) != 2 || !hr[0].At.Equal(at) || !hr[1].At.Equal(later) || hr[1].Value != 72 {
		t.Fatalf("heart rate %+v, want %v and %v", hr, at, later)
	}
}

// TestTimestamp16BeforeBase: another message's full timestamp lies a little after the next
// monitoring record. The record's timestamp_16 belongs a few seconds before it, not 18 hours
// later (the real monitoring files of 2026-11-07 had such records).
func TestTimestamp16BeforeBase(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	first := now.Add(-2 * time.Hour)
	other := first.Add(60 * time.Second)  // e.g. a stress record, rounded up
	record := first.Add(45 * time.Second) // the next monitoring record, before it
	b := fittest.Build(typedef.FileMonitoringB, 1, now,
		fittest.Message(55, F{Num: 253, Value: proto.Uint32(fittest.FITTime(first))}, F{Num: 27, Value: proto.Uint8(70)}),
		fittest.Message(227, F{Num: 253, Value: proto.Uint32(fittest.FITTime(other))}, F{Num: 0, Value: proto.Int16(30)}),
		fittest.Message(55, F{Num: 26, Value: proto.Uint16(uint16(fittest.FITTime(record)))}, F{Num: 27, Value: proto.Uint8(74)}),
	)
	d, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	hr := byKind(d, HeartRate)
	if len(hr) != 2 || !hr[1].At.Equal(record) {
		t.Fatalf("heart rate %+v, want the second at %v", hr, record)
	}
}

// TestSleepFile: a stage record closes the interval since the record before (the first one the
// interval since the night's start, which sleep_insight states); an assessment
// gives the scores. HRV values and the summary.
func TestSleepFile(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	start := now.Add(-8 * time.Hour)
	ends := []struct {
		after time.Duration
		level uint8
	}{{30 * time.Minute, SleepLight}, {90 * time.Minute, SleepDeep}, {5 * time.Hour, SleepLight}, {7*time.Hour + 50*time.Minute, SleepREM}, {8 * time.Hour, SleepAwake}}
	var msgs []proto.Message
	for _, s := range ends {
		msgs = append(msgs, fittest.Message(275, F{Num: 253, Value: ft(start.Add(s.after))}, F{Num: 0, Value: proto.Uint8(s.level)}))
	}
	msgs = append(msgs,
		fittest.Message(346, F{Num: 6, Value: proto.Uint8(84)}, F{Num: 7, Value: proto.Uint8(80)}),
		fittest.Message(521, F{Num: 253, Value: ft(now)}, F{Num: 3, Value: ft(start)}, F{Num: 4, Value: proto.Int16(60)},
			F{Num: 5, Value: ft(start.Add(8 * time.Hour))}, F{Num: 6, Value: proto.Int16(60)}),
		fittest.Message(371, F{Num: 253, Value: ft(start.Add(time.Hour))}, F{Num: 0, Value: proto.Uint16(45 * 128)}),
		fittest.Message(370, F{Num: 253, Value: ft(start.Add(8 * time.Hour))}, F{Num: 0, Value: proto.Uint16(48 * 128)},
			F{Num: 1, Value: proto.Uint16(46 * 128)}, F{Num: 6, Value: proto.Uint8(2)}),
	)
	d, err := Decode(fittest.Build(typedef.File(49), 1, now, msgs...))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Sleep) != 1 {
		t.Fatalf("%d sleeps", len(d.Sleep))
	}
	s := d.Sleep[0]
	if !s.Start.Equal(start) || !s.End.Equal(start.Add(8*time.Hour)) || s.Score != 84 || s.QualityScore != 80 || !s.Stages {
		t.Fatalf("sleep %+v", s)
	}
	if s.Deep != 3600 || s.Light != (30*60+210*60) || s.REM != 170*60 || s.Awake != 10*60 {
		t.Fatalf("stages deep %d light %d rem %d awake %d", s.Deep, s.Light, s.REM, s.Awake)
	}
	// One stage sample per interval, at its start, with the level of the record closing it.
	st := byKind(d, SleepStage)
	want := []struct {
		after time.Duration
		level int64
	}{{0, SleepLight}, {30 * time.Minute, SleepDeep}, {90 * time.Minute, SleepLight}, {5 * time.Hour, SleepREM}, {7*time.Hour + 50*time.Minute, SleepAwake}}
	if len(st) != len(want) {
		t.Fatalf("stage samples %+v", st)
	}
	for i, w := range want {
		if !st[i].At.Equal(start.Add(w.after)) || st[i].Value != w.level {
			t.Fatalf("stage sample %d: %+v, want %v at +%v", i, st[i], w.level, w.after)
		}
	}
	if len(byKind(d, HRV)) != 1 {
		t.Fatalf("samples %+v", d.Samples)
	}
	if len(d.HRV) != 1 || d.HRV[0].WeeklyAverage != 48 || d.HRV[0].LastNight != 46 || d.HRV[0].Status != 2 {
		t.Fatalf("hrv %+v", d.HRV)
	}
	if d.HRV[0].BalancedLower != Unknown {
		t.Fatalf("absent field not Unknown: %+v", d.HRV[0])
	}
	if len(d.Offsets) == 0 || d.Offsets[0].Offset != time.Hour {
		t.Fatalf("offsets %+v", d.Offsets)
	}
}

// TestSleepEvent: an older sleep file states the start only by its sleep event.
func TestSleepEvent(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	start := now.Add(-6 * time.Hour)
	b := fittest.Build(typedef.File(49), 1, now,
		fittest.Message(21, F{Num: 253, Value: ft(start)}, F{Num: 0, Value: proto.Uint8(74)}, F{Num: 1, Value: proto.Uint8(0)}),
		fittest.Message(275, F{Num: 253, Value: ft(start.Add(10 * time.Minute))}, F{Num: 0, Value: proto.Uint8(SleepLight)}),
		fittest.Message(275, F{Num: 253, Value: ft(start.Add(time.Hour))}, F{Num: 0, Value: proto.Uint8(SleepDeep)}),
		fittest.Message(21, F{Num: 253, Value: ft(start.Add(time.Hour))}, F{Num: 0, Value: proto.Uint8(74)}, F{Num: 1, Value: proto.Uint8(1)}),
	)
	d, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Sleep) != 1 || !d.Sleep[0].Start.Equal(start) || d.Sleep[0].Light != 600 || d.Sleep[0].Deep != 3000 {
		t.Fatalf("%+v", d.Sleep)
	}
}

// TestSleepFileWithoutStart: without a stated start the first interval is unknown; the night
// starts at the first record.
func TestSleepFileWithoutStart(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	start := now.Add(-6 * time.Hour)
	b := fittest.Build(typedef.File(49), 1, now,
		fittest.Message(275, F{Num: 253, Value: ft(start)}, F{Num: 0, Value: proto.Uint8(SleepLight)}),
		fittest.Message(275, F{Num: 253, Value: ft(start.Add(time.Hour))}, F{Num: 0, Value: proto.Uint8(SleepDeep)}),
		fittest.Message(275, F{Num: 253, Value: ft(start.Add(3 * time.Hour))}, F{Num: 0, Value: proto.Uint8(SleepREM)}),
	)
	d, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Sleep) != 1 {
		t.Fatalf("%+v", d.Sleep)
	}
	s := d.Sleep[0]
	if !s.Start.Equal(start) || !s.End.Equal(start.Add(3*time.Hour)) || s.Deep != 3600 || s.REM != 7200 || s.Light != 0 {
		t.Fatalf("sleep %+v", s)
	}
	if st := byKind(d, SleepStage); len(st) != 2 || !st[0].At.Equal(start) || st[0].Value != SleepDeep {
		t.Fatalf("stage samples %+v", st)
	}
}

// TestNaps: nap events of the daily-health file; deleted ones are left out.
func TestNaps(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	a, b := now.Add(-5*time.Hour), now.Add(-4*time.Hour)
	f := fittest.Build(typedef.File(72), 1, now,
		fittest.Message(412, F{Num: 0, Value: ft(a)}, F{Num: 1, Value: proto.Int16(60)}, F{Num: 2, Value: ft(a.Add(25 * time.Minute))},
			F{Num: 4, Value: proto.Uint8(2)}, F{Num: 6, Value: proto.Uint8(0)}),
		fittest.Message(412, F{Num: 0, Value: ft(b)}, F{Num: 2, Value: ft(b.Add(20 * time.Minute))}, F{Num: 5, Value: proto.Uint8(1)}),
	)
	d, err := Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Naps) != 1 || !d.Naps[0].Start.Equal(a) || d.Naps[0].End.Sub(d.Naps[0].Start) != 25*time.Minute ||
		d.Naps[0].Feedback != 2 || d.Naps[0].Source != 0 {
		t.Fatalf("naps %+v", d.Naps)
	}
	if from, to := d.Span(); !from.Equal(a) || !to.Equal(a.Add(25*time.Minute)) {
		t.Fatalf("span %v %v", from, to)
	}
}

func TestNotFIT(t *testing.T) {
	if _, err := Decode([]byte("<xml>not fit</xml>")); err != ErrNotFIT {
		t.Fatalf("err %v", err)
	}
	if k, ok := KindByName("body_battery"); !ok || k != BodyBattery || BodyBattery.Info().Unit != "%" {
		t.Fatal("kind lookup")
	}
}

// TestDailyArrays: the step goal (an array per activity type), the sleep coach's need and the
// nights of the HRV arrays.
func TestDailyArrays(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	local := now.Add(time.Hour)
	inv := uint16(0xFFFF)
	b := fittest.Build(typedef.FileMonitoringB, 1, now,
		fittest.Message(103, F{Num: 253, Value: ft(now)}, F{Num: 0, Value: ft(local)},
			F{Num: 7, Value: proto.SliceUint16([]uint16{3990, 3990})}),
		fittest.Message(410, F{Num: 253, Value: ft(now)}, F{Num: 0, Value: proto.Uint16(420)}, F{Num: 1, Value: proto.Uint16(430)}),
		fittest.Message(401, F{Num: 0, Value: ft(local)},
			F{Num: 2, Value: proto.SliceUint16([]uint16{45, inv, 50, 57, inv, inv})},
			F{Num: 3, Value: proto.SliceUint16([]uint16{46, 46, 46, 47, inv, 44})},
			F{Num: 4, Value: proto.SliceUint16([]uint16{60, 60, 60, 61, inv, 62})}),
	)
	d, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	day := func(back int) string { return local.UTC().AddDate(0, 0, -back).Format(time.DateOnly) }
	got := map[string]int64{}
	for _, v := range d.Daily {
		if v.Kind == SleepNeed {
			got[v.Kind.String()] = v.Value
			continue
		}
		got[v.Kind.String()+" "+v.Day] = v.Value
	}
	// Last night is the last valid HRV slot; each slot before it is a night earlier, an invalid
	// slot is a night without a value, and the range after last night is ignored.
	want := map[string]int64{
		"step_goal " + day(0): 3990, "sleep_need": 430,
		"hrv_last_night " + day(0): 57, "hrv_last_night " + day(1): 50, "hrv_last_night " + day(3): 45,
		"hrv_target_low " + day(0): 47, "hrv_target_low " + day(3): 46, "hrv_target_high " + day(0): 61,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %d, want %d", k, got[k], w)
		}
	}
	if _, ok := got["hrv_last_night "+day(2)]; ok {
		t.Error("a night without a value got one")
	}
	n := 0
	for _, v := range d.Daily {
		if v.Kind == HRVTargetLow {
			n++
		}
	}
	if n != 4 { // slots 0–3; the lingering 44 after last night is ignored
		t.Errorf("%d target-low values, want 4", n)
	}
}

// TestSuggestions: Garmin Coach's schedule refers to workouts by workout_index, in any order.
func TestSuggestions(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	tomorrow := now.AddDate(0, 0, 1)
	b := fittest.Build(typedef.File(77), 1, now,
		fittest.Message(428, F{Num: 1, Value: proto.Uint16(1)}, F{Num: 3, Value: proto.Uint8(2)}, F{Num: 5, Value: proto.Uint8(27)},
			F{Num: 7, Value: proto.Uint8(1)}, F{Num: 9, Value: proto.Uint32(1200000)}, F{Num: 10, Value: ft(now)}),
		fittest.Message(428, F{Num: 1, Value: proto.Uint16(2)}, F{Num: 3, Value: proto.Uint8(0xFF)}, F{Num: 10, Value: ft(tomorrow)}),
		fittest.Message(26, F{Num: 20, Value: proto.Uint16(1)}, F{Num: 8, Value: proto.String("Base")},
			F{Num: 17, Value: proto.String("136bpm")}, F{Num: 4, Value: proto.Uint8(1)}),
	)
	d, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Suggestions) != 2 {
		t.Fatalf("%+v", d.Suggestions)
	}
	s := d.Suggestions[0]
	if s.Name != "Base" || s.Description != "136bpm" || s.Duration != 20*time.Minute || s.Benefit != 2 || s.AerobicTE != 2.7 ||
		s.Day != now.UTC().Format(time.DateOnly) {
		t.Fatalf("suggestion %+v", s)
	}
	if rest := d.Suggestions[1]; rest.Name != "" || rest.Duration != 0 || rest.Benefit != Unknown {
		t.Fatalf("rest day %+v", rest)
	}
}

// TestHealthReaders: a weighing, skin temperature, sleep disruption, an ECG with its samples,
// physiological metrics and the metrics file's VO2 max, fitness age and thresholds.
func TestHealthReaders(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	at := now.Add(-3 * time.Hour)
	local := proto.Uint32(fittest.FITTime(at.Add(time.Hour)))
	inv16 := proto.Uint16(0xFFFF)
	b := fittest.Build(typedef.File(9), 1, now,
		fittest.Message(30, F{Num: 253, Value: ft(at)}, F{Num: 0, Value: proto.Uint16(7520)}, F{Num: 1, Value: proto.Uint16(1850)},
			F{Num: 2, Value: inv16}, F{Num: 7, Value: proto.Uint16(1700 * 4)}, F{Num: 13, Value: proto.Uint16(231)}),
		fittest.Message(397, F{Num: 253, Value: ft(at)}, F{Num: 1, Value: proto.Float32(-0.37)}),
		fittest.Message(398, F{Num: 253, Value: ft(at)}, F{Num: 0, Value: local}, F{Num: 1, Value: proto.Float32(-0.2)},
			F{Num: 2, Value: proto.Float32(0.1)}, F{Num: 3, Value: proto.Uint8(30)}, F{Num: 4, Value: proto.Float32(33.4)}),
		fittest.Message(470, F{Num: 253, Value: ft(at)}, F{Num: 0, Value: proto.Uint8(2)}),
		fittest.Message(471, F{Num: 253, Value: ft(at)}, F{Num: 0, Value: proto.Uint8(1)}),
		fittest.Message(336, F{Num: 4, Value: ft(at)}, F{Num: 1, Value: proto.Uint8(1)}, F{Num: 2, Value: proto.Float32(512)},
			F{Num: 7, Value: proto.Float32(61.5)}, F{Num: 12, Value: proto.Uint8(44)}, F{Num: 11, Value: proto.Uint16(30)}),
		fittest.Message(337, F{Num: 0, Value: proto.Float32(12.5)}),
		fittest.Message(337, F{Num: 0, Value: proto.Float32(-3)}),
		fittest.Message(338, F{Num: 0, Value: proto.Float32(10)}),
		fittest.Message(140, F{Num: 253, Value: ft(at)}, F{Num: 7, Value: proto.Int32(979295)},
			F{Num: 14, Value: proto.Uint16(171)}, F{Num: 53, Value: proto.Uint8(190)}, F{Num: 9, Value: proto.Uint16(1440)},
			F{Num: 4, Value: proto.Uint8(31)}),
		fittest.Message(229, F{Num: 0, Value: ft(at)}, F{Num: 2, Value: proto.Uint16(523)}, F{Num: 3, Value: proto.Int8(35)}),
		fittest.Message(356, F{Num: 253, Value: ft(at)}, F{Num: 4, Value: proto.Uint16(250)}, F{Num: 8, Value: proto.Uint8(172)}),
	)
	d, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.WeighIns) != 1 {
		t.Fatalf("weigh-ins %+v", d.WeighIns)
	}
	w := d.WeighIns[0]
	if w.Weight != 75.2 || w.Fat != 18.5 || w.Hydration != Unknown || w.BMR != 1700 || w.BMI != 23.1 || !w.At.Equal(at) {
		t.Fatalf("weigh-in %+v", w)
	}
	if st := byKind(d, SkinTemp); len(st) != 1 || st[0].Value != -37 {
		t.Fatalf("skin temperature %+v", st)
	}
	if len(d.SkinTemp) != 1 || d.SkinTemp[0].Nightly < 33.39 || d.SkinTemp[0].CalibratedDays != 30 || d.SkinTemp[0].Day == "" {
		t.Fatalf("skin temperature night %+v", d.SkinTemp)
	}
	if dis := byKind(d, Disruption); len(dis) != 1 || dis[0].Value != 2 {
		t.Fatalf("disruption %+v", dis)
	}
	if len(d.ECG) != 1 || len(d.ECG[0].Raw) != 2 || d.ECG[0].Raw[0] != 12.5 || len(d.ECG[0].Smooth) != 1 || d.ECG[0].RMSSD != 44 ||
		d.ECG[0].AvgHR != 61.5 || d.ECG[0].RawRate != 512 || !d.ECG[0].Start.Equal(at) {
		t.Fatalf("ecg %+v", d.ECG)
	}
	if len(d.Physio) != 1 || d.Physio[0].LTHR != 171 || d.Physio[0].AerobicTE != 3.1 || d.Physio[0].VO2Max < 52.29 || d.Physio[0].VO2Max > 52.31 {
		t.Fatalf("physio %+v", d.Physio)
	}
	got := map[Kind]int64{}
	for _, v := range d.Daily {
		got[v.Kind] = v.Value
	}
	want := map[Kind]int64{Weight: 75200, BodyFat: 1850, BMI: 231, SkinTempDeviation: -20, DisruptionNight: 1, VO2Max: 5230,
		FitnessAge: 35, FTP: 250, LactateThreshold: 172, MaxHR: 190, RecoveryTime: 1440}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %d, want %d", k, got[k], v)
		}
	}
	if _, ok := got[MinHR]; ok {
		t.Error("an unrelated kind")
	}
}
