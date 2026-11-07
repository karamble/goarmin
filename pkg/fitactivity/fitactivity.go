// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package fitactivity reads activity FIT files (file type 4) from any device: one Activity per
// session (a multisport file has several), with its laps and the recorded track as columns.
package fitactivity

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/pkg/geo"
)

// Activity is one session. Numbers are NaN, or −1 for counts, when the file lacks them.
type Activity struct {
	Index              int // session index within the file
	Sport              typedef.Sport
	SubSport           typedef.SubSport
	Name               string // the sport profile's name, e.g. "Trail Run"
	Start, End         time.Time
	Elapsed, Timer     float64 // s
	Distance           float64 // m
	Calories           int
	AvgHR, MaxHR       int
	AvgSpeed, MaxSpeed float64 // m/s
	AvgCadence         int
	AvgPower           int
	Ascent, Descent    int     // m
	TrainingEffect     float64 // aerobic, 0–5
	AnaerobicEffect    float64
	TrainingLoad       float64
	Bounds             *geo.Bounds
	Polyline           string // simplified track, encoded (empty without GPS)
	Laps               []Lap
	Track              Track
	// Zones is the watch's own time in zones for the session, when the file has it.
	Zones *Zones
}

// Lap is one lap of a session.
type Lap struct {
	Start              time.Time
	Elapsed, Timer     float64
	Distance           float64
	Calories           int
	AvgHR, MaxHR       int
	AvgSpeed, MaxSpeed float64
	AvgCadence         int
	AvgPower           int
	Ascent, Descent    int
	Zones              *Zones
}

// Zones is the watch's own time in zones for a session or a lap (time_in_zone), with the
// limits it used.
type Zones struct {
	// HR are the heart-rate limits in bpm: the bottom of zone 1, then the top of zones 1–5.
	HR []int `json:"hr,omitempty"`
	// HRSeconds is the time below zone 1, in zones 1–5 and above the top, in that order.
	HRSeconds []float64 `json:"hr_s,omitempty"`
	// Power are the power limits in W, as HR; PowerSeconds as HRSeconds.
	Power        []int     `json:"power,omitempty"`
	PowerSeconds []float64 `json:"power_s,omitempty"`
	MaxHR        int       `json:"max_hr,omitempty"`
	RestingHR    int       `json:"resting_hr,omitempty"`
	ThresholdHR  int       `json:"threshold_hr,omitempty"`
	FTP          int       `json:"ftp,omitempty"`
	HRCalc       string    `json:"hr_calc,omitempty"`
}

// Track is the recorded data, one entry per record; NaN where a record lacks a value.
type Track struct {
	Time                      []int64 // Unix seconds
	Lat, Lon                  []float64
	Altitude, Distance, Speed []float64 // m, m, m/s
	HeartRate, Cadence, Power []float64
	Temperature               []float64 // °C
}

// Len is the number of records.
func (t Track) Len() int { return len(t.Time) }

// Add appends one record (an activity's or a course's) to the track.
func (t *Track) Add(r *mesgdef.Record) {
	t.Time = append(t.Time, r.Timestamp.Unix())
	t.Lat = append(t.Lat, deg(r.PositionLat))
	t.Lon = append(t.Lon, deg(r.PositionLong))
	alt := r.EnhancedAltitudeScaled()
	if math.IsNaN(alt) {
		alt = r.AltitudeScaled()
	}
	t.Altitude = append(t.Altitude, alt)
	t.Distance = append(t.Distance, r.DistanceScaled())
	sp := r.EnhancedSpeedScaled()
	if math.IsNaN(sp) {
		sp = r.SpeedScaled()
	}
	t.Speed = append(t.Speed, sp)
	t.HeartRate = append(t.HeartRate, u8(r.HeartRate))
	t.Cadence = append(t.Cadence, u8(r.Cadence))
	t.Power = append(t.Power, u16(r.Power))
	temp := math.NaN()
	if r.Temperature != basetype.Sint8Invalid {
		temp = float64(r.Temperature)
	}
	t.Temperature = append(t.Temperature, temp)
}

func deg(semi int32) float64 {
	if semi == basetype.Sint32Invalid {
		return math.NaN()
	}
	return float64(semi) * (180.0 / (1 << 31))
}

func u8(v uint8) float64 {
	if v == basetype.Uint8Invalid {
		return math.NaN()
	}
	return float64(v)
}

func u16(v uint16) float64 {
	if v == basetype.Uint16Invalid {
		return math.NaN()
	}
	return float64(v)
}

func count8(v uint8) int {
	if v == basetype.Uint8Invalid {
		return -1
	}
	return int(v)
}

func count16(v uint16) int {
	if v == basetype.Uint16Invalid {
		return -1
	}
	return int(v)
}

// ErrNoSession is returned for a FIT file without a session message.
var ErrNoSession = errors.New("fitactivity: no session in the file")

// Decode reads all sessions of an activity file.
func Decode(b []byte) ([]Activity, error) {
	var sessions []*mesgdef.Session
	var laps []*mesgdef.Lap
	sessionZones := map[int]*Zones{}
	lapZones := map[int]*Zones{}
	var records []*mesgdef.Record
	var sportName string
	dec := decoder.New(bytes.NewReader(b))
	for dec.Next() {
		f, err := dec.Decode()
		if err != nil {
			return nil, err
		}
		for i := range f.Messages {
			m := &f.Messages[i]
			switch m.Num {
			case typedef.MesgNumSession:
				sessions = append(sessions, mesgdef.NewSession(m))
			case typedef.MesgNumLap:
				laps = append(laps, mesgdef.NewLap(m))
			case typedef.MesgNumRecord:
				r := mesgdef.NewRecord(m)
				if !r.Timestamp.IsZero() {
					records = append(records, r)
				}
			case typedef.MesgNumSport:
				if s := mesgdef.NewSport(m); s.Name != "" && sportName == "" {
					sportName = s.Name
				}
			case typedef.MesgNumTimeInZone:
				z := mesgdef.NewTimeInZone(m)
				idx := int(z.ReferenceIndex)
				if z.ReferenceIndex == typedef.MessageIndexInvalid {
					idx = 0
				}
				switch z.ReferenceMesg {
				case typedef.MesgNumSession:
					sessionZones[idx] = zonesOf(z)
				case typedef.MesgNumLap:
					lapZones[idx] = zonesOf(z)
				}
			}
		}
	}
	if len(sessions) == 0 {
		return nil, ErrNoSession
	}
	var out []Activity
	for i, s := range sessions {
		a := Activity{
			Index: i, Sport: s.Sport, SubSport: s.SubSport, Name: strings.TrimSpace(s.SportProfileName),
			Start: s.StartTime, Elapsed: s.TotalElapsedTimeScaled(), Timer: s.TotalTimerTimeScaled(),
			Distance: s.TotalDistanceScaled(), Calories: count16(s.TotalCalories),
			AvgHR: count8(s.AvgHeartRate), MaxHR: count8(s.MaxHeartRate),
			AvgSpeed: s.EnhancedAvgSpeedScaled(), MaxSpeed: s.EnhancedMaxSpeedScaled(),
			AvgCadence: count8(s.AvgCadence), AvgPower: count16(s.AvgPower),
			Ascent: count16(s.TotalAscent), Descent: count16(s.TotalDescent),
			TrainingEffect: s.TotalTrainingEffectScaled(), AnaerobicEffect: s.TotalAnaerobicTrainingEffectScaled(),
			TrainingLoad: s.TrainingLoadPeakScaled(),
			Zones:        sessionZones[i],
		}
		if math.IsNaN(a.AvgSpeed) {
			a.AvgSpeed = s.AvgSpeedScaled()
		}
		if a.Name == "" {
			a.Name = sportName
		}
		if a.Start.IsZero() {
			continue
		}
		a.End = a.Start.Add(time.Duration(math.Max(0, nanTo0(a.Elapsed)) * float64(time.Second)))
		if !s.Timestamp.IsZero() && s.Timestamp.After(a.End) {
			a.End = s.Timestamp
		}
		// Sessions of a multisport file meet: one ends the second the next starts. A record
		// or lap at that second belongs to the next one.
		last := i == len(sessions)-1
		in := func(t time.Time) bool {
			if len(sessions) == 1 {
				return true
			}
			return !t.Before(a.Start) && (t.Before(a.End) || last && t.Equal(a.End))
		}
		for j, l := range laps {
			if !in(l.StartTime) {
				continue
			}
			a.Laps = append(a.Laps, Lap{Start: l.StartTime, Elapsed: l.TotalElapsedTimeScaled(), Timer: l.TotalTimerTimeScaled(),
				Distance: l.TotalDistanceScaled(), Calories: count16(l.TotalCalories), AvgHR: count8(l.AvgHeartRate),
				MaxHR: count8(l.MaxHeartRate), AvgSpeed: l.EnhancedAvgSpeedScaled(), MaxSpeed: l.EnhancedMaxSpeedScaled(),
				AvgCadence: count8(l.AvgCadence), AvgPower: count16(l.AvgPower), Ascent: count16(l.TotalAscent),
				Descent: count16(l.TotalDescent), Zones: lapZones[j]})
		}
		var pts []geo.Point
		for _, r := range records {
			if !in(r.Timestamp) {
				continue
			}
			a.Track.Add(r)
			pts = append(pts, geo.Point{Lat: a.Track.Lat[a.Track.Len()-1], Lon: a.Track.Lon[a.Track.Len()-1]})
		}
		if b, ok := geo.BoundsOf(pts); ok {
			a.Bounds = &b
			a.Polyline = geo.EncodePolyline(geo.SimplifyTo(pts, 500))
		}
		out = append(out, a)
	}
	return out, nil
}

// zonesOf reads a time_in_zone message; limits stop at the first invalid one, and a kind
// without limits is left out.
func zonesOf(z *mesgdef.TimeInZone) *Zones {
	out := &Zones{HRCalc: z.HrCalcType.String()}
	if strings.Contains(out.HRCalc, "Invalid") {
		out.HRCalc = ""
	}
	for _, v := range z.HrZoneHighBoundary {
		if v == basetype.Uint8Invalid || v == 0 {
			break
		}
		out.HR = append(out.HR, int(v))
	}
	for _, v := range z.PowerZoneHighBoundary {
		if v == basetype.Uint16Invalid || v == 0 {
			break
		}
		out.Power = append(out.Power, int(v))
	}
	secs := func(in []uint32) []float64 {
		var o []float64
		for _, v := range in {
			if v == basetype.Uint32Invalid {
				o = append(o, 0)
				continue
			}
			o = append(o, float64(v)/1000)
		}
		return o
	}
	if len(out.HR) > 0 {
		out.HRSeconds = secs(z.TimeInHrZone)
	}
	if len(out.Power) > 0 {
		out.PowerSeconds = secs(z.TimeInPowerZone)
	}
	if len(out.HR) == 0 && len(out.Power) == 0 {
		return nil
	}
	if v := count8(z.MaxHeartRate); v > 0 {
		out.MaxHR = v
	}
	if v := count8(z.RestingHeartRate); v > 0 {
		out.RestingHR = v
	}
	if v := count8(z.ThresholdHeartRate); v > 0 {
		out.ThresholdHR = v
	}
	if v := count16(z.FunctionalThresholdPower); v > 0 {
		out.FTP = v
	}
	return out
}

func nanTo0(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return v
}

// Columns returns the track as gzipped JSON: {"fields": [...], "t": [...], "<field>": [...]},
// numbers rounded to sensible precision, null where a record lacks a value, and only fields
// that have at least one value. This is how goarmin stores and serves tracks.
func (t Track) Columns() ([]byte, []string, error) {
	cols := []struct {
		name string
		vals []float64
		prec float64
	}{
		{"lat", t.Lat, 1e7}, {"lon", t.Lon, 1e7}, {"alt", t.Altitude, 10}, {"dist", t.Distance, 10},
		{"speed", t.Speed, 1000}, {"hr", t.HeartRate, 1}, {"cad", t.Cadence, 1}, {"power", t.Power, 1},
		{"temp", t.Temperature, 1},
	}
	doc := map[string]any{"t": t.Time}
	var fields []string
	for _, c := range cols {
		any := false
		out := make([]*float64, len(c.vals))
		for i, v := range c.vals {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			r := math.Round(v*c.prec) / c.prec
			out[i] = &r
			any = true
		}
		if any {
			doc[c.name] = out
			fields = append(fields, c.name)
		}
	}
	doc["fields"] = fields
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(doc); err != nil {
		return nil, nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), fields, nil
}

// SportName is a readable name for a sport and sub-sport, e.g. "Pool swim" or "Trail run".
// Combinations without a name of their own fall back to the FIT names, tidied.
func SportName(s typedef.Sport, sub typedef.SubSport) string {
	if n, ok := subSportNames[[2]string{s.String(), sub.String()}]; ok {
		return n
	}
	if sub != typedef.SubSportGeneric && sub != typedef.SubSportInvalid {
		if n, ok := subSportNames[[2]string{"", sub.String()}]; ok {
			return n
		}
	}
	if n, ok := sportNames[s.String()]; ok {
		return n
	}
	if sub != typedef.SubSportGeneric && sub != typedef.SubSportInvalid {
		return tidy(sub.String())
	}
	return tidy(s.String())
}

var sportNames = map[string]string{
	"generic": "Activity", "running": "Run", "cycling": "Ride", "walking": "Walk", "hiking": "Hike",
	"swimming": "Swim", "training": "Training", "fitness_equipment": "Workout", "rowing": "Row",
	"e_biking": "E-bike ride", "mountaineering": "Mountaineering", "multisport": "Multisport",
	"cross_country_skiing": "Cross-country ski", "alpine_skiing": "Ski", "snowboarding": "Snowboard",
	"stand_up_paddleboarding": "Paddleboard", "kayaking": "Kayak", "paddling": "Paddle", "rock_climbing": "Climb",
	"floor_climbing": "Floor climb", "golf": "Golf", "tennis": "Tennis", "yoga": "Yoga", "pilates": "Pilates",
}

// subSportNames name sport/sub-sport pairs; an empty sport matches any.
var subSportNames = map[[2]string]string{
	{"running", "trail"}: "Trail run", {"running", "treadmill"}: "Treadmill run", {"running", "track"}: "Track run",
	{"running", "indoor_running"}: "Indoor run", {"running", "street"}: "Run",
	{"cycling", "indoor_cycling"}: "Indoor ride", {"cycling", "mountain"}: "Mountain bike ride", {"cycling", "road"}: "Road ride",
	{"cycling", "gravel_cycling"}: "Gravel ride", {"cycling", "virtual_activity"}: "Virtual ride", {"cycling", "commuting"}: "Commute",
	{"walking", "indoor_walking"}: "Indoor walk", {"walking", "casual_walking"}: "Walk", {"walking", "speed_walking"}: "Speed walk",
	{"swimming", "lap_swimming"}: "Pool swim", {"swimming", "open_water"}: "Open water swim",
	{"", "strength_training"}: "Strength", {"", "cardio_training"}: "Cardio", {"", "hiit"}: "HIIT", {"", "yoga"}: "Yoga",
	{"", "pilates"}: "Pilates", {"", "breathing"}: "Breathwork", {"", "elliptical"}: "Elliptical",
	{"", "stair_climbing"}: "Stair climb", {"", "indoor_rowing"}: "Indoor row", {"", "indoor_climbing"}: "Indoor climb",
	{"", "bouldering"}: "Bouldering",
}

// Label is SportName, or the sport profile's own name (profile) for a sport this FIT profile
// does not know yet: the Fenix 8 records "Health Snapshot" as sport 60, which the SDK's enum
// lacks.
func Label(s typedef.Sport, sub typedef.SubSport, profile string) string {
	if !strings.HasPrefix(s.String(), "SportInvalid") || profile == "" {
		return SportName(s, sub)
	}
	return profile
}

// tidy turns a FIT enum name into words: "sport_invalid(77)" stays, "open_water" → "Open water".
func tidy(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
