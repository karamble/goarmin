// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fitactivity

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"

	"github.com/karamble/goarmin/pkg/fitid/fittest"
	"github.com/karamble/goarmin/pkg/geo"
)

func semi(deg float64) int32 { return int32(deg * (1 << 31) / 180) }

// Multisport builds a two-session file (run then bike) starting at start, one record a
// second, a lap per session; the bike has no GPS for its first half.
func Multisport(start time.Time) []byte {
	var msgs []proto.Message
	add := func(m proto.Message) { msgs = append(msgs, m) }
	type part struct {
		sport typedef.Sport
		dur   int
		speed float64
	}
	parts := []part{{typedef.SportRunning, 600, 3}, {typedef.SportCycling, 900, 8}}
	t := start
	dist := 0.0
	for i, p := range parts {
		s0 := t
		for k := range p.dur {
			r := mesgdef.NewRecord(nil)
			r.Timestamp = t
			dist += p.speed
			r.Distance = uint32(dist * 100)
			r.EnhancedSpeed = uint32(p.speed * 1000)
			r.HeartRate = uint8(130 + i*10 + k%5)
			if !(i == 1 && k < p.dur/2) {
				r.PositionLat, r.PositionLong = semi(50+float64(k)*1e-4), semi(7+float64(i)*0.01)
			}
			add(r.ToMesg(nil))
			t = t.Add(time.Second)
		}
		l := mesgdef.NewLap(nil)
		l.StartTime, l.Timestamp = s0, t
		l.TotalElapsedTime, l.TotalTimerTime = uint32(p.dur*1000), uint32(p.dur*1000)
		l.TotalDistance = uint32(p.speed * float64(p.dur) * 100)
		l.AvgHeartRate = uint8(132 + i*10)
		add(l.ToMesg(nil))
		s := mesgdef.NewSession(nil)
		s.StartTime, s.Timestamp = s0, t
		s.Sport = p.sport
		s.TotalElapsedTime, s.TotalTimerTime = uint32(p.dur*1000), uint32(p.dur*1000)
		s.TotalDistance = uint32(p.speed * float64(p.dur) * 100)
		s.AvgHeartRate, s.MaxHeartRate = uint8(132+i*10), uint8(134+i*10)
		s.TotalAscent = 12
		s.TotalTrainingEffect = 31
		s.EnhancedAvgSpeed = uint32(p.speed * 1000)
		add(s.ToMesg(nil))
	}
	return fittest.Build(typedef.FileActivity, 1, t, msgs...)
}

func TestMultisport(t *testing.T) {
	start := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	acts, err := Decode(Multisport(start))
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 2 {
		t.Fatalf("%d sessions", len(acts))
	}
	run, bike := acts[0], acts[1]
	if run.Sport != typedef.SportRunning || bike.Sport != typedef.SportCycling || bike.Index != 1 {
		t.Fatalf("sports %v %v", run.Sport, bike.Sport)
	}
	if !run.Start.Equal(start) || run.Timer != 600 || run.Distance != 1800 || run.AvgHR != 132 || run.Ascent != 12 || run.TrainingEffect != 3.1 {
		t.Fatalf("run %+v", run)
	}
	if run.Track.Len() != 600 || bike.Track.Len() != 900 || len(run.Laps) != 1 || len(bike.Laps) != 1 {
		t.Fatalf("records %d/%d laps %d/%d", run.Track.Len(), bike.Track.Len(), len(run.Laps), len(bike.Laps))
	}
	if run.Bounds == nil || math.Abs(run.Bounds.MinLat-50) > 1e-6 || run.Polyline == "" {
		t.Fatalf("run bounds %+v", run.Bounds)
	}
	if !math.IsNaN(bike.Track.Lat[0]) || math.IsNaN(bike.Track.Lat[899]) {
		t.Fatal("missing positions not NaN")
	}
	if pts := geo.DecodePolyline(run.Polyline); len(pts) < 2 || len(pts) > 500 {
		t.Fatalf("polyline of %d points", len(pts))
	}

	data, fields, err := bike.Track.Columns()
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		T   []int64    `json:"t"`
		Lat []*float64 `json:"lat"`
		HR  []*float64 `json:"hr"`
	}
	if err := json.NewDecoder(zr).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.T) != 900 || doc.Lat[0] != nil || doc.Lat[899] == nil || *doc.HR[0] != 140 {
		t.Fatalf("columns: %d times, first lat %v, fields %v", len(doc.T), doc.Lat[0], fields)
	}
	if _, err := Decode(fittest.Build(typedef.FileActivity, 1, start)); err != ErrNoSession {
		t.Fatalf("file without a session: %v", err)
	}
}

// TestTimeInZone: the watch's own time in zones lands on its session and on its lap.
func TestTimeInZone(t *testing.T) {
	start := time.Now().Add(-time.Hour).Truncate(time.Second)
	var msgs []proto.Message
	for k := range 120 {
		r := mesgdef.NewRecord(nil)
		r.Timestamp, r.HeartRate = start.Add(time.Duration(k)*time.Second), 140
		msgs = append(msgs, r.ToMesg(nil))
	}
	end := start.Add(120 * time.Second)
	l := mesgdef.NewLap(nil)
	l.MessageIndex, l.StartTime, l.Timestamp, l.TotalTimerTime = 0, start, end, 120000
	s := mesgdef.NewSession(nil)
	s.MessageIndex, s.StartTime, s.Timestamp, s.TotalTimerTime, s.Sport = 0, start, end, 120000, typedef.SportRunning
	tiz := func(ref typedef.MesgNum, secs []uint32) proto.Message {
		z := mesgdef.NewTimeInZone(nil)
		z.ReferenceMesg, z.ReferenceIndex, z.Timestamp = ref, 0, end
		z.HrZoneHighBoundary = []uint8{92, 110, 128, 146, 165, 183}
		z.TimeInHrZone = secs
		z.PowerZoneHighBoundary = []uint16{0xFFFF, 0xFFFF}
		z.MaxHeartRate, z.ThresholdHeartRate, z.HrCalcType = 183, 161, typedef.HrZoneCalcPercentMaxHr
		return z.ToMesg(nil)
	}
	msgs = append(msgs, l.ToMesg(nil), s.ToMesg(nil),
		tiz(typedef.MesgNumSession, []uint32{0, 20000, 60500, 39500, 0, 0, 0}),
		tiz(typedef.MesgNumLap, []uint32{0, 20000, 60500, 39500, 0, 0, 0xFFFFFFFF}))
	acts, err := Decode(fittest.Build(typedef.FileActivity, 1, end, msgs...))
	if err != nil {
		t.Fatal(err)
	}
	z := acts[0].Zones
	if z == nil || !reflect.DeepEqual(z.HR, []int{92, 110, 128, 146, 165, 183}) || !reflect.DeepEqual(z.HRSeconds, []float64{0, 20, 60.5, 39.5, 0, 0, 0}) ||
		z.Power != nil || z.MaxHR != 183 || z.ThresholdHR != 161 || z.RestingHR != 0 || z.HRCalc != "percent_max_hr" {
		t.Fatalf("session zones %+v", z)
	}
	if lz := acts[0].Laps[0].Zones; lz == nil || lz.HRSeconds[6] != 0 || lz.HRSeconds[2] != 60.5 {
		t.Fatalf("lap zones %+v", lz)
	}
}

// An activity recorded with a workout names it; one without has none.
func TestWorkoutName(t *testing.T) {
	end := time.Now().Add(-time.Hour).Truncate(time.Second)
	start := end.Add(-10 * time.Minute)
	s := mesgdef.NewSession(nil)
	s.StartTime, s.Timestamp, s.TotalTimerTime, s.Sport = start, end, 600000, typedef.SportTraining
	w := mesgdef.NewWorkout(nil)
	w.WktName, w.Sport = " Core 20 ", typedef.SportTraining
	acts, err := Decode(fittest.Build(typedef.FileActivity, 1, end, w.ToMesg(nil), s.ToMesg(nil)))
	if err != nil || len(acts) != 1 || acts[0].Workout != "Core 20" {
		t.Fatalf("with a workout: %+v, %v", acts, err)
	}
	acts, err = Decode(fittest.Build(typedef.FileActivity, 1, end, s.ToMesg(nil)))
	if err != nil || acts[0].Workout != "" {
		t.Fatalf("without: %q, %v", acts[0].Workout, err)
	}
}
