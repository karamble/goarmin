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

// TestSleepFile: stages and an assessment make one night; HRV values and the summary.
func TestSleepFile(t *testing.T) {
	now := time.Now().Truncate(time.Minute)
	start := now.Add(-8 * time.Hour)
	stages := []struct {
		after time.Duration
		level uint8
	}{{0, SleepLight}, {30 * time.Minute, SleepDeep}, {90 * time.Minute, SleepLight}, {5 * time.Hour, SleepREM}, {7*time.Hour + 50*time.Minute, SleepAwake}}
	var msgs []proto.Message
	for _, s := range stages {
		msgs = append(msgs, fittest.Message(275, F{Num: 253, Value: ft(start.Add(s.after))}, F{Num: 0, Value: proto.Uint8(s.level)}))
	}
	msgs = append(msgs,
		fittest.Message(346, F{Num: 6, Value: proto.Uint8(84)}, F{Num: 7, Value: proto.Uint8(80)}),
		fittest.Message(276, F{Num: 253, Value: ft(start.Add(8 * time.Hour))}),
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
	if !s.Start.Equal(start) || !s.End.Equal(start.Add(8*time.Hour)) || s.Score != 84 || s.QualityScore != 80 {
		t.Fatalf("sleep %+v", s)
	}
	if s.Deep != 3600 || s.Light != (30*60+210*60) || s.REM != 170*60 || s.Awake != 10*60 {
		t.Fatalf("stages deep %d light %d rem %d awake %d", s.Deep, s.Light, s.REM, s.Awake)
	}
	if len(byKind(d, SleepStage)) != 5 || len(byKind(d, HRV)) != 1 {
		t.Fatalf("samples %+v", d.Samples)
	}
	if len(d.HRV) != 1 || d.HRV[0].WeeklyAverage != 48 || d.HRV[0].LastNight != 46 || d.HRV[0].Status != 2 {
		t.Fatalf("hrv %+v", d.HRV)
	}
	if d.HRV[0].BalancedLower != Unknown {
		t.Fatalf("absent field not Unknown: %+v", d.HRV[0])
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
