// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/fitactivity"
	"github.com/karamble/goarmin/pkg/garmin/wellness"
)

func (s *Server) metrics(w http.ResponseWriter, r *http.Request, _ store.User) {
	out := []map[string]any{}
	for _, k := range wellness.Kinds() {
		i := k.Info()
		out = append(out, map[string]any{"id": k, "name": i.Name, "unit": i.Unit, "scale": i.Scale, "total": i.Total, "daily": i.Daily})
	}
	writeJSON(w, http.StatusOK, out)
}

// dayRange reads from/to as YYYY-MM-DD, defaulting to the last 30 days, at most maxDays long.
func dayRange(r *http.Request, maxDays int) (from, to string, err error) {
	now := time.Now()
	to = r.URL.Query().Get("to")
	if to == "" {
		to = now.Format(time.DateOnly)
	}
	tt, err := time.Parse(time.DateOnly, to)
	if err != nil {
		return "", "", errors.New("to must be YYYY-MM-DD")
	}
	from = r.URL.Query().Get("from")
	if from == "" {
		from = tt.AddDate(0, 0, -29).Format(time.DateOnly)
	}
	ft, err := time.Parse(time.DateOnly, from)
	if err != nil {
		return "", "", errors.New("from must be YYYY-MM-DD")
	}
	if ft.After(tt) || tt.Sub(ft) > time.Duration(maxDays)*24*time.Hour {
		return "", "", fmt.Errorf("from..to must be ascending and at most %d days", maxDays)
	}
	return from, to, nil
}

func (s *Server) daily(w http.ResponseWriter, r *http.Request, u store.User) {
	from, to, err := dayRange(r, 3660)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := s.dailyJSON(r.Context(), u.ID, from, to)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// dailyJSON is user's days in [from, to] as /api/daily serves them: each day's summary, its
// once-a-day values and its HRV night, by day.
func (s *Server) dailyJSON(ctx context.Context, user int64, from, to string) ([]map[string]any, error) {
	days, err := s.st.DaySummaries(ctx, user, from, to)
	if err != nil {
		return nil, err
	}
	vals, err := s.st.DailyValues(ctx, user, from, to)
	if err != nil {
		return nil, err
	}
	hrv, err := s.st.HRVNights(ctx, user, from, to)
	if err != nil {
		return nil, err
	}
	watchGoal, err := s.watchFloorsGoal(ctx, user)
	if err != nil {
		return nil, err
	}
	byDay := map[string]map[string]float64{}
	for _, v := range vals {
		if byDay[v.Day] == nil {
			byDay[v.Day] = map[string]float64{}
		}
		byDay[v.Day][v.Kind.String()] = float64(v.Value) * v.Kind.Info().Scale
	}
	out := []map[string]any{}
	seen := map[string]bool{}
	for _, d := range days {
		seen[d.Day] = true
		out = append(out, map[string]any{
			"day": d.Day, "steps": d.Steps, "distance_m": scaledP(d.DistanceCM, 0.01), "active_kcal": d.ActiveKcal,
			"ascent_m": scaledP(d.AscentMM, 0.001), "descent_m": scaledP(d.DescentMM, 0.001),
			"moderate_min": d.ModerateMin, "vigorous_min": d.VigorousMin,
			"hr_min": d.HRMin, "hr_max": d.HRMax, "hr_avg": d.HRAvg, "resting_hr": d.RestingHR,
			"stress_avg": d.StressAvg, "stress_max": d.StressMax, "body_battery_min": d.BBMin, "body_battery_max": d.BBMax,
			"respiration_avg": scaledP(d.RespirationAvg, 0.01), "spo2_avg": d.SpO2Avg,
			"sleep_score": d.SleepScore, "sleep_s": d.Sleep, "values": orEmpty(byDay[d.Day]),
		})
		if h, ok := hrv[d.Day]; ok {
			out[len(out)-1]["hrv"] = h
		}
		if d.AscentMM != nil {
			// Floors as the watch counts them, with the day's own goal (a Garmin Connect export
			// keeps one per day), else the goal the watch has now.
			m := out[len(out)-1]
			m["floors_up"] = wellness.Floors(*d.AscentMM)
			if d.DescentMM != nil {
				m["floors_down"] = wellness.Floors(*d.DescentMM)
			}
			if g := byDay[d.Day]["floors_goal"]; g > 0 {
				m["floors_goal"] = int64(math.Round(g))
			} else if watchGoal > 0 {
				m["floors_goal"] = watchGoal
			}
		}
	}
	for day, v := range byDay { // days with daily values but no summary row
		if !seen[day] {
			out = append(out, map[string]any{"day": day, "values": v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["day"].(string) < out[j]["day"].(string) })
	return out, nil
}

// watchFloorsGoal is the floors goal user's watch keeps (its daily ascent goal, in metres) as
// floors; 0 without one.
func (s *Server) watchFloorsGoal(ctx context.Context, user int64) (int64, error) {
	goals, err := s.st.WatchGoals(ctx, user)
	if err != nil {
		return 0, err
	}
	for _, g := range goals {
		if g.Type == "ascent" && g.Unit == "m" && g.Target > 0 {
			return wellness.FloorsFromMetres(float64(g.Target)), nil
		}
	}
	return 0, nil
}

func orEmpty(m map[string]float64) map[string]float64 {
	if m == nil {
		return map[string]float64{}
	}
	return m
}

func scaledP(p *int64, k float64) *float64 {
	if p == nil {
		return nil
	}
	v := float64(*p) * k
	return &v
}

// timeRange reads from/to as Unix seconds, defaulting to the last 24 hours.
func timeRange(r *http.Request, max time.Duration) (from, to int64, err error) {
	q := r.URL.Query()
	to = time.Now().Unix()
	if v := q.Get("to"); v != "" {
		if to, err = strconv.ParseInt(v, 10, 64); err != nil {
			return 0, 0, errors.New("to must be Unix seconds")
		}
	}
	from = to - 86400
	if v := q.Get("from"); v != "" {
		if from, err = strconv.ParseInt(v, 10, 64); err != nil {
			return 0, 0, errors.New("from must be Unix seconds")
		}
	}
	if from >= to || time.Duration(to-from)*time.Second > max {
		return 0, 0, fmt.Errorf("from..to must be ascending and at most %s", max)
	}
	return from, to, nil
}

// dayTimes resolves local days [fromDay, toDay] (toDay "" = fromDay) to a UTC span with the
// user's zone history.
func (s *Server) dayTimes(r *http.Request, u store.User, fromDay, toDay string, maxDays int) (int64, int64, error) {
	if toDay == "" {
		toDay = fromDay
	}
	a, err1 := time.Parse(time.DateOnly, fromDay)
	b, err2 := time.Parse(time.DateOnly, toDay)
	if err1 != nil || err2 != nil {
		return 0, 0, errors.New("from_day and to_day must be YYYY-MM-DD")
	}
	if b.Before(a) || b.Sub(a) > time.Duration(maxDays)*24*time.Hour {
		return 0, 0, fmt.Errorf("from_day..to_day must be ascending and at most %d days", maxDays)
	}
	z, err := s.st.Zone(r.Context(), u.ID)
	if err != nil {
		return 0, 0, err
	}
	from, _, err := z.DayRange(fromDay)
	if err != nil {
		return 0, 0, err
	}
	_, to, err := z.DayRange(toDay)
	return from, to, err
}

// seriesJSON is a chart-ready series: values in the metric's unit, bucketed when there would be
// more than max_points; min and max are present for bucketed non-total series.
type seriesJSON struct {
	Metric string    `json:"metric"`
	Unit   string    `json:"unit"`
	Total  bool      `json:"total"`
	From   int64     `json:"from"` // the span asked for (local days resolved), for the x-axis
	To     int64     `json:"to"`
	Bucket int64     `json:"bucket_s"`
	T      []int64   `json:"t"`
	V      []float64 `json:"v"`
	Min    []float64 `json:"min,omitempty"`
	Max    []float64 `json:"max,omitempty"`
}

func (s *Server) series(w http.ResponseWriter, r *http.Request, u store.User) {
	k, ok := wellness.KindByName(r.PathValue("metric"))
	if !ok || k.Info().Daily {
		writeError(w, http.StatusNotFound, "no such time series (GET /api/metrics lists them)")
		return
	}
	var from, to int64
	var err error
	if fd := r.URL.Query().Get("from_day"); fd != "" {
		// Local days as the watch had them (its UTC offset history), the same days the
		// daily summaries use.
		from, to, err = s.dayTimes(r, u, fd, r.URL.Query().Get("to_day"), 400)
	} else {
		from, to, err = timeRange(r, 400*24*time.Hour)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	maxPoints := 1500
	if v := r.URL.Query().Get("max_points"); v != "" {
		if maxPoints, err = strconv.Atoi(v); err != nil || maxPoints < 10 || maxPoints > 20000 {
			writeError(w, http.StatusBadRequest, "max_points must be 10..20000")
			return
		}
	}
	// Running totals need the last value before the range to give its first interval.
	pts, err := s.st.Series(r.Context(), u.ID, k, from, to)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, buildSeries(k, pts, from, to, maxPoints))
}

// buildSeries turns stored samples into a chart series. Running totals (steps, …) become the
// amount per sample interval, summed over activity types; a total that drops has restarted at
// midnight, so its new value is the interval's amount.
func buildSeries(k wellness.Kind, pts []store.Point, from, to int64, maxPoints int) seriesJSON {
	info := k.Info()
	out := seriesJSON{Metric: info.Name, Unit: info.Unit, Total: info.Total, From: from, To: to, T: []int64{}, V: []float64{}}
	type tv struct {
		t int64
		v float64
	}
	var raw []tv
	if info.Total {
		last := map[uint8]int64{}
		have := map[uint8]bool{}
		sum := map[int64]float64{}
		for _, p := range pts {
			d := p.Value
			if have[p.Sub] && p.Value >= last[p.Sub] {
				d = p.Value - last[p.Sub]
			}
			last[p.Sub], have[p.Sub] = p.Value, true
			sum[p.TS] += float64(d)
		}
		for t, v := range sum {
			raw = append(raw, tv{t, v * info.Scale})
		}
		slices.SortFunc(raw, func(a, b tv) int { return int(a.t - b.t) })
	} else {
		for _, p := range pts {
			raw = append(raw, tv{p.TS, float64(p.Value) * info.Scale})
		}
	}
	if len(raw) <= maxPoints {
		for _, x := range raw {
			out.T, out.V = append(out.T, x.t), append(out.V, x.v)
		}
		return out
	}
	bucket := (to - from + int64(maxPoints) - 1) / int64(maxPoints)
	out.Bucket = bucket
	i := 0
	for i < len(raw) {
		b := from + (raw[i].t-from)/bucket*bucket
		n, sum, lo, hi := 0, 0.0, math.Inf(1), math.Inf(-1)
		for i < len(raw) && raw[i].t < b+bucket {
			n++
			sum += raw[i].v
			lo, hi = math.Min(lo, raw[i].v), math.Max(hi, raw[i].v)
			i++
		}
		out.T = append(out.T, b)
		if info.Total {
			out.V = append(out.V, sum)
		} else {
			out.V = append(out.V, sum/float64(n))
			out.Min, out.Max = append(out.Min, lo), append(out.Max, hi)
		}
	}
	return out
}

func (s *Server) sleep(w http.ResponseWriter, r *http.Request, u store.User) {
	from, to, err := dayRange(r, 3660)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := s.st.SleepSessions(r.Context(), u.ID, from, to)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, x := range list {
		out = append(out, map[string]any{"day": x.Day, "start": x.Start, "end": x.End, "score": x.Score,
			"quality_score": x.QualityScore, "duration_score": x.DurationScore, "recovery_score": x.RecoveryScore,
			"min_hr": x.MinHR, "body_battery_start": x.BBStart, "body_battery_end": x.BBEnd,
			"deep_s": x.Deep, "light_s": x.Light, "rem_s": x.REM, "awake_s": x.Awake, "unmeasurable_s": x.Unmeasurable})
	}
	writeJSON(w, http.StatusOK, out)
}

// sleepStages returns the stage changes of the night that started at {start} (Unix seconds),
// with the HR, stress, Body Battery, respiration and HRV samples of the night.
func (s *Server) sleepStages(w http.ResponseWriter, r *http.Request, u store.User) {
	start, err := strconv.ParseInt(r.PathValue("start"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such night")
		return
	}
	day := time.Unix(start, 0).UTC()
	list, err := s.st.SleepSessions(r.Context(), u.ID, day.AddDate(0, 0, -1).Format(time.DateOnly), day.AddDate(0, 0, 2).Format(time.DateOnly))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	i := slices.IndexFunc(list, func(x store.SleepSession) bool { return x.Start == start })
	if i < 0 {
		writeError(w, http.StatusNotFound, "no such night")
		return
	}
	night := list[i]
	out := map[string]any{"start": night.Start, "end": night.End}
	for _, k := range []wellness.Kind{wellness.SleepStage, wellness.HeartRate, wellness.Stress, wellness.BodyBattery,
		wellness.Respiration, wellness.HRV} {
		pts, err := s.st.Series(r.Context(), u.ID, k, night.Start, night.End+1)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		out[k.String()] = buildSeries(k, pts, night.Start, night.End+1, 2000)
	}
	writeJSON(w, http.StatusOK, out)
}

func activityJSON(a store.ActivitySummary) map[string]any {
	m := map[string]any{"id": a.ID, "start": a.Start, "end": a.End, "day": a.Day, "name": a.Name,
		"sport": typedef.Sport(a.Sport).String(), "sub_sport": typedef.SubSport(a.SubSport).String(),
		"sport_label": fitactivity.Label(typedef.Sport(a.Sport), typedef.SubSport(a.SubSport), a.Name),
		"elapsed_s":   a.Elapsed, "timer_s": a.Timer, "distance_m": a.Distance, "kcal": a.Kcal,
		"avg_hr": a.AvgHR, "max_hr": a.MaxHR, "avg_speed": a.AvgSpeed, "max_speed": a.MaxSpeed,
		"avg_cadence": a.AvgCadence, "avg_power": a.AvgPower, "ascent_m": a.Ascent, "descent_m": a.Descent,
		"training_effect": a.TrainingEffect, "anaerobic_effect": a.AnaerobicEffect, "training_load": a.TrainingLoad,
		"polyline": a.Polyline, "extra": a.Extra}
	if a.MinLat != nil {
		m["bounds"] = []float64{*a.MinLat, *a.MinLon, *a.MaxLat, *a.MaxLon}
	}
	return m
}

func (s *Server) activities(w http.ResponseWriter, r *http.Request, u store.User) {
	q := r.URL.Query()
	limit, before := 50, int64(0)
	var err error
	if v := q.Get("limit"); v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 || limit > 200 {
			writeError(w, http.StatusBadRequest, "limit must be 1..200")
			return
		}
	}
	if v := q.Get("before"); v != "" {
		if before, err = strconv.ParseInt(v, 10, 64); err != nil {
			writeError(w, http.StatusBadRequest, "before must be Unix seconds")
			return
		}
	}
	if q.Get("from_day") != "" || q.Get("to_day") != "" {
		// A period: every activity recorded on those local days, newest first.
		from, to := q.Get("from_day"), q.Get("to_day")
		if to == "" {
			to = from
		}
		_, e1 := time.Parse(time.DateOnly, from)
		_, e2 := time.Parse(time.DateOnly, to)
		if e1 != nil || e2 != nil || from > to {
			writeError(w, http.StatusBadRequest, "from_day and to_day must be YYYY-MM-DD, from_day first")
			return
		}
		list, err := s.st.ActivitiesIn(r.Context(), u.ID, from, to)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		items := []map[string]any{}
		for _, a := range list {
			items = append(items, activityJSON(a))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	list, err := s.st.Activities(r.Context(), u.ID, before, limit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	items := []map[string]any{}
	for _, a := range list {
		items = append(items, activityJSON(a))
	}
	resp := map[string]any{"items": items}
	if len(list) == limit {
		resp["next_before"] = list[len(list)-1].Start
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) userActivity(w http.ResponseWriter, r *http.Request, u store.User) (store.ActivitySummary, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such activity")
		return store.ActivitySummary{}, false
	}
	a, err := s.st.Activity(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such activity")
		return a, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return a, false
	}
	return a, true
}

func (s *Server) activity(w http.ResponseWriter, r *http.Request, u store.User) {
	a, ok := s.userActivity(w, r, u)
	if !ok {
		return
	}
	laps, err := s.st.Laps(r.Context(), u.ID, a.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	zones, err := s.st.ActivityZones(r.Context(), u.ID, a.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := activityJSON(a)
	lj := []map[string]any{}
	for _, l := range laps {
		x := map[string]any{"index": l.Index, "start": l.Start, "elapsed_s": l.Elapsed, "timer_s": l.Timer,
			"distance_m": l.Distance, "kcal": l.Kcal, "avg_hr": l.AvgHR, "max_hr": l.MaxHR, "avg_speed": l.AvgSpeed,
			"max_speed": l.MaxSpeed, "avg_cadence": l.AvgCadence, "avg_power": l.AvgPower, "ascent_m": l.Ascent,
			"descent_m": l.Descent}
		if z, ok := zones[l.Index]; ok {
			x["zones"] = z
		}
		lj = append(lj, x)
	}
	out["laps"] = lj
	// The watch's own time in zones and the limits it used, when the file has them.
	if z, ok := zones[-1]; ok {
		out["zones"] = z
	}
	writeJSON(w, http.StatusOK, out)
}

// activityNeighbours returns the activities just before and after one: {prev, next}, each
// null at the ends.
func (s *Server) activityNeighbours(w http.ResponseWriter, r *http.Request, u store.User) {
	a, ok := s.userActivity(w, r, u)
	if !ok {
		return
	}
	out := map[string]any{"prev": nil, "next": nil}
	if list, err := s.st.Activities(r.Context(), u.ID, a.Start, 1); err != nil {
		s.internalError(w, r, err)
		return
	} else if len(list) == 1 {
		out["prev"] = activityJSON(list[0])
	}
	if n, err := s.st.ActivityAfter(r.Context(), u.ID, a.Start); err == nil {
		out["next"] = activityJSON(n)
	} else if !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// activityStreams serves the stored track columns, gzipped as stored when the browser accepts
// it (every browser does), else inflated.
func (s *Server) activityStreams(w http.ResponseWriter, r *http.Request, u store.User) {
	a, ok := s.userActivity(w, r, u)
	if !ok {
		return
	}
	_, data, err := s.st.Stream(r.Context(), u.ID, a.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "this activity has no recorded track")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.serveGzipJSON(w, r, data)
}

// serveGzipJSON serves stored gzipped JSON (a track), as is when the browser takes gzip.
func (s *Server) serveGzipJSON(w http.ResponseWriter, r *http.Request, data []byte) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "private, max-age=3600")
	h.Set("Vary", "Accept-Encoding")
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		_, _ = w.Write(data)
		return
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	_, _ = io.Copy(w, zr)
}

// activityFIT downloads the original file.
func (s *Server) activityFIT(w http.ResponseWriter, r *http.Request, u store.User) {
	a, ok := s.userActivity(w, r, u)
	if !ok {
		return
	}
	s.serveFIT(w, r, u, a.RawFileID, time.Unix(a.Start, 0).UTC().Format("2006-01-02-15-04-05")+".fit")
}

// serveFIT downloads one of user's original files under name.
func (s *Server) serveFIT(w http.ResponseWriter, r *http.Request, u store.User, rawFileID int64, name string) {
	f, err := s.st.RawFile(r.Context(), rawFileID)
	if err != nil || f.UserID != u.ID {
		writeError(w, http.StatusNotFound, "the original file is missing")
		return
	}
	rd, err := s.blobs.Open(f.SHA256)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	defer rd.Close()
	w.Header().Set("Content-Type", "application/vnd.ant.fit")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	_, _ = io.Copy(w, rd)
}

func (s *Server) coach(w http.ResponseWriter, r *http.Request, u store.User) {
	from, to, err := dayRange(r, 60)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := s.st.SuggestedWorkouts(r.Context(), u.ID, from, to)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, x := range list {
		sport := ""
		if x.Sport != nil {
			sport = typedef.Sport(*x.Sport).String()
		}
		out = append(out, map[string]any{"day": x.Day, "index": x.Index, "name": x.Name, "description": x.Description,
			"sport": sport, "duration_s": x.DurationS, "benefit": x.Benefit, "aerobic_te": x.AerobicTE,
			"rest_day": x.Name == "" && x.DurationS == 0})
	}
	writeJSON(w, http.StatusOK, out)
}
