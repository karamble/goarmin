// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/fitnamed"
	"github.com/karamble/goarmin/pkg/fitprofile"
)

// The routes of what the readers store beyond days, nights and activities: the raw files
// with a message inspector, naps, body measurements, ECG recordings, physiological metrics,
// personal records, gear and golf clubs, and a watch's solar charge, settings change log and
// documents.

func (s *Server) routesReaders(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/files", s.needUser(s.files))
	mux.HandleFunc("GET /api/files/{id}/messages", s.needUser(s.fileMessages))
	mux.HandleFunc("GET /api/files/{id}/download", s.needUser(s.fileDownload))
	mux.HandleFunc("GET /api/naps", s.needUser(s.naps))
	mux.HandleFunc("GET /api/body", s.needUser(s.body))
	mux.HandleFunc("GET /api/ecg", s.needUser(s.ecgs))
	mux.HandleFunc("GET /api/ecg/{id}", s.needUser(s.ecg))
	mux.HandleFunc("GET /api/physiology", s.needUser(s.physiology))
	mux.HandleFunc("GET /api/records", s.needUser(s.records))
	mux.HandleFunc("GET /api/gear", s.needUser(s.gear))
	mux.HandleFunc("GET /api/golf-clubs", s.needUser(s.golfClubs))
	mux.HandleFunc("GET /api/devices/{id}/solar", s.needUser(s.deviceSolar))
	mux.HandleFunc("GET /api/devices/{id}/changes", s.needUser(s.deviceChanges))
	mux.HandleFunc("GET /api/devices/{id}/documents", s.needUser(s.deviceDocuments))
	mux.HandleFunc("GET /api/account/documents", s.needUser(s.accountDocuments))
	mux.HandleFunc("GET /api/account/documents/{id}", s.needUser(s.accountDocument))
	mux.HandleFunc("GET /api/health-status", s.needUser(s.healthStatus))
	mux.HandleFunc("GET /api/hydration", s.needUser(s.hydration))
	mux.HandleFunc("GET /api/snapshots", s.needUser(s.snapshots))
}

// accountDocuments lists the account's records from Garmin Connect exports (profile,
// settings, orders, consents, inReach, …), without their contents.
func (s *Server) accountDocuments(w http.ResponseWriter, r *http.Request, u store.User) {
	list, err := s.st.AccountDocuments(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, d := range list {
		out = append(out, map[string]any{"id": d.FileID, "kind": d.Kind, "name": path.Base(d.Name), "recorded_at": d.RecordedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) accountDocument(w http.ResponseWriter, r *http.Request, u store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such document")
		return
	}
	d, err := s.st.AccountDocument(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such document")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": d.FileID, "kind": d.Kind, "name": path.Base(d.Name), "recorded_at": d.RecordedAt, "data": d.Data})
}

func (s *Server) healthStatus(w http.ResponseWriter, r *http.Request, u store.User) {
	from, to, err := dayRange(r, 3660)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := s.st.HealthStatus(r.Context(), u.ID, from, to)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, h := range list {
		out = append(out, map[string]any{"day": h.Day, "metric": h.Metric, "status": h.Status, "value": h.Value, "low": h.Low,
			"high": h.High, "pct": h.Pct})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) hydration(w http.ResponseWriter, r *http.Request, u store.User) {
	from, to, err := dayRange(r, 3660)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := s.st.HydrationLogs(r.Context(), u.ID, from, to)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, h := range list {
		out = append(out, map[string]any{"ts": h.TS, "day": h.Day, "source": h.Source, "ml": h.ML, "sweat_ml": h.SweatML})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) snapshots(w http.ResponseWriter, r *http.Request, u store.User) {
	from, to, err := dayRange(r, 3660)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := s.st.HealthSnapshots(r.Context(), u.ID, from, to)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, h := range list {
		out = append(out, map[string]any{"start": h.Start, "end": h.End, "day": h.Day, "name": h.Name, "data": h.Data})
	}
	writeJSON(w, http.StatusOK, out)
}

func fileJSON(f store.RawFile) map[string]any {
	m := map[string]any{"id": f.ID, "name": f.SourceName, "source": f.Source, "size": f.Size, "received_at": f.ReceivedAt.Unix(),
		"device_id": nil, "fit_type": nil, "type_name": "Other file", "created": nil}
	if f.DeviceID != 0 {
		m["device_id"] = f.DeviceID
	}
	if f.FIT != nil {
		m["fit_type"], m["type_name"] = f.FIT.Type, fitprofile.FileTypeTitle(f.FIT.Type)
		if f.FIT.TimeCreated != 0 {
			m["created"] = f.FIT.TimeCreated
		}
	}
	return m
}

// files lists the user's raw files, newest first: ?device=, ?type= (a FIT type, or "other"),
// ?before= (an id, for the next page), ?limit= (1–500, 100).
func (s *Server) files(w http.ResponseWriter, r *http.Request, u store.User) {
	q := r.URL.Query()
	fq := store.FileQuery{Type: -1, Limit: 100}
	var err error
	num := func(name string, dst *int64) bool {
		if v := q.Get(name); v != "" {
			if *dst, err = strconv.ParseInt(v, 10, 64); err != nil || *dst < 0 {
				writeError(w, http.StatusBadRequest, name+" must be a number")
				return false
			}
		}
		return true
	}
	var limit, typ int64 = 100, -1
	if !num("device", &fq.Device) || !num("before", &fq.Before) || !num("limit", &limit) {
		return
	}
	if limit < 1 || limit > 500 {
		writeError(w, http.StatusBadRequest, "limit must be 1..500")
		return
	}
	switch v := q.Get("type"); v {
	case "":
	case "other":
		typ = -2
	default:
		if typ, err = strconv.ParseInt(v, 10, 64); err != nil || typ < 0 || typ > 255 {
			writeError(w, http.StatusBadRequest, "type must be a FIT file type or other")
			return
		}
	}
	fq.Type, fq.Limit = int(typ), int(limit)
	list, err := s.st.UserFiles(r.Context(), u.ID, fq)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, f := range list {
		out = append(out, fileJSON(f))
	}
	var next any
	if len(list) == fq.Limit {
		next = list[len(list)-1].ID
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "next_before": next})
}

func (s *Server) userFile(w http.ResponseWriter, r *http.Request, u store.User) (store.RawFile, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such file")
		return store.RawFile{}, false
	}
	f, err := s.st.UserFile(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such file")
		return f, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return f, false
	}
	return f, true
}

// maxInspect bounds the bytes the inspector decodes (the importer's own limit per file).
const maxInspect = 64 << 20

// fileMessages decodes one of the user's FIT files on request and names what it holds: every
// message number with its count, and the messages themselves (?num= one number only,
// ?offset=, ?limit= 1–2000, 200), fields named, scaled and in words where the FIT profile
// knows them.
func (s *Server) fileMessages(w http.ResponseWriter, r *http.Request, u store.User) {
	f, ok := s.userFile(w, r, u)
	if !ok {
		return
	}
	if f.FIT == nil {
		writeError(w, http.StatusUnprocessableEntity, "this file is not a FIT file")
		return
	}
	q := r.URL.Query()
	limit, offset, only := 200, 0, -1
	for name, dst := range map[string]*int{"limit": &limit, "offset": &offset, "num": &only} {
		if v := q.Get(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				writeError(w, http.StatusBadRequest, name+" must be a number")
				return
			}
			*dst = n
		}
	}
	if limit < 1 || limit > 2000 {
		writeError(w, http.StatusBadRequest, "limit must be 1..2000")
		return
	}
	rd, err := s.blobs.Open(f.SHA256)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	b, err := io.ReadAll(io.LimitReader(rd, maxInspect))
	rd.Close()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	msgs, derr := fitnamed.Decode(b, nil)
	type count struct {
		Num      uint16 `json:"num"`
		Name     string `json:"name"`
		Count    int    `json:"count"`
		Known    bool   `json:"known"`
		Official bool   `json:"official"`
	}
	counts := []*count{}
	byNum := map[uint16]*count{}
	shown := []fitnamed.Message{}
	seen := 0
	for _, m := range msgs {
		c := byNum[m.Num]
		if c == nil {
			c = &count{Num: m.Num, Name: m.Name, Known: m.Known, Official: m.Official}
			byNum[m.Num] = c
			counts = append(counts, c)
		}
		c.Count++
		if only >= 0 && int(m.Num) != only {
			continue
		}
		if seen >= offset && len(shown) < limit {
			shown = append(shown, m)
		}
		seen++
	}
	out := map[string]any{"file": fileJSON(f), "counts": counts, "messages": shown, "total": seen, "offset": offset}
	if derr != nil {
		out["error"] = derr.Error() // what could be read before the file broke off
	}
	writeJSON(w, http.StatusOK, out)
}

// fileDownload hands over one of the user's original files.
func (s *Server) fileDownload(w http.ResponseWriter, r *http.Request, u store.User) {
	f, ok := s.userFile(w, r, u)
	if !ok {
		return
	}
	rd, err := s.blobs.Open(f.SHA256)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	defer rd.Close()
	name := path.Base(f.SourceName)
	if name == "" || name == "." || name == "/" {
		name = strconv.FormatInt(f.ID, 10)
	}
	if f.FIT != nil {
		w.Header().Set("Content-Type", "application/vnd.ant.fit")
	} else if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		w.Header().Set("Content-Type", t)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	_, _ = io.Copy(w, rd)
}

// unixDays turns a day range into Unix seconds in the user's zone: [start of from, end of to).
func (s *Server) unixDays(r *http.Request, u store.User, maxDays int) (from, to int64, fromDay, toDay string, err error) {
	if fromDay, toDay, err = dayRange(r, maxDays); err != nil {
		return
	}
	z, err := s.st.Zone(r.Context(), u.ID)
	if err != nil {
		return
	}
	if from, _, err = z.DayRange(fromDay); err != nil {
		return
	}
	_, to, err = z.DayRange(toDay)
	return
}

func (s *Server) naps(w http.ResponseWriter, r *http.Request, u store.User) {
	from, to, err := dayRange(r, 3660)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := s.st.Naps(r.Context(), u.ID, from, to)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, n := range list {
		out = append(out, map[string]any{"start": n.Start, "end": n.End, "day": n.Day, "feedback": n.Feedback, "how": n.How})
	}
	writeJSON(w, http.StatusOK, out)
}

// body returns the period's weigh-ins and nights of skin temperature.
func (s *Server) body(w http.ResponseWriter, r *http.Request, u store.User) {
	from, to, fromDay, toDay, err := s.unixDays(r, u, 3660)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ws, err := s.st.WeighIns(r.Context(), u.ID, from, to)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	skin, err := s.st.SkinTempNights(r.Context(), u.ID, fromDay, toDay)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	weigh := []map[string]any{}
	for _, x := range ws {
		weigh = append(weigh, map[string]any{"ts": x.TS, "day": x.Day, "weight_kg": x.WeightKg, "fat_pct": x.FatPct,
			"hydration_pct": x.HydrationPct, "bone_kg": x.BoneKg, "muscle_kg": x.MuscleKg, "visceral_fat_kg": x.VisceralFatKg, "bmi": x.BMI,
			"bmr_kcal": x.BMR, "active_mr_kcal": x.ActiveMR, "metabolic_age": x.MetabolicAge, "visceral_rating": x.VisceralRating,
			"source": x.Source})
	}
	nights := []map[string]any{}
	for _, n := range skin {
		nights = append(nights, map[string]any{"day": n.Day, "ts": n.TS, "nightly_c": n.NightlyC, "deviation_c": n.DeviationC,
			"deviation_7d_c": n.Deviation7dC, "calibrated_days": n.CalibratedDays})
	}
	writeJSON(w, http.StatusOK, map[string]any{"weigh_ins": weigh, "skin_temp": nights})
}

// ecgClass names the watch's rhythm classification (ecg_rhythm_classification).
func ecgClass(c *int64) string {
	if c == nil {
		return ""
	}
	if name := fitprofile.EnumValue("ecg_rhythm_classification", uint32(*c)); name != "" {
		return name
	}
	return strconv.FormatInt(*c, 10)
}

func ecgJSON(e store.ECGRecording) map[string]any {
	return map[string]any{"id": e.ID, "start": e.Start, "day": e.Day, "class": ecgClass(e.Class), "avg_hr": e.AvgHR,
		"rmssd_ms": e.RMSSD, "seconds": e.Seconds, "raw_rate": e.RawRate, "smooth_rate": e.SmoothRate, "source": e.Source}
}

func (s *Server) ecgs(w http.ResponseWriter, r *http.Request, u store.User) {
	list, err := s.st.ECGs(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, e := range list {
		out = append(out, ecgJSON(e))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) ecg(w http.ResponseWriter, r *http.Request, u store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such recording")
		return
	}
	e, err := s.st.ECG(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such recording")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	m := ecgJSON(e)
	m["samples"] = e.Samples
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) physiology(w http.ResponseWriter, r *http.Request, u store.User) {
	from, to, _, _, err := s.unixDays(r, u, 3660)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := s.st.Physiologies(r.Context(), u.ID, from, to)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, p := range list {
		out = append(out, map[string]any{"ts": p.TS, "sport": p.Sport, "vo2max": p.VO2Max, "lthr": p.LTHR, "lt_power": p.LTPower,
			"lt_speed_kmh": p.LTSpeedKmh, "max_hr": p.MaxHR, "avg_hr": p.AvgHR, "recovery_min": p.Recovery, "aerobic_te": p.AerobicTE,
			"anaerobic_te": p.AnaerobicTE, "duration_s": p.DurationS, "distance_m": p.DistanceM, "race_5k_s": p.Race5K,
			"race_10k_s": p.Race10K, "race_half_s": p.RaceHalf, "race_marathon_s": p.RaceMarathon})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) records(w http.ResponseWriter, r *http.Request, u store.User) {
	list, err := s.st.PersonalRecords(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	acts, err := s.st.ConnectActivities(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, x := range list {
		m := map[string]any{"source": x.Source, "metric": x.Metric, "sport": x.Sport, "ts": x.TS, "value": x.Value,
			"distance_m": x.DistanceM, "activity_id": nil, "data": x.Data}
		if x.ActivityID != nil {
			if a, ok := acts[*x.ActivityID]; ok {
				m["activity_id"] = a.ID // goarmin's activity, for a link
			}
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) gear(w http.ResponseWriter, r *http.Request, u store.User) {
	list, err := s.st.Gear(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	acts, err := s.st.ConnectActivities(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, g := range list {
		m := map[string]any{"uuid": g.UUID, "source": g.Source, "type": g.Type, "name": g.Name, "brand": g.Brand, "model": g.Model, "data": g.Data}
		// Garmin Connect's gear lists the activities it was used in: their distance, and how many.
		var d struct {
			Activities []int64 `json:"activities"`
		}
		if json.Unmarshal(g.Data, &d) == nil && len(d.Activities) > 0 {
			sum := 0.0
			for _, id := range d.Activities {
				sum += acts[id].Distance
			}
			m["distance_m"], m["uses"] = sum, len(d.Activities)
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) golfClubs(w http.ResponseWriter, r *http.Request, u store.User) {
	list, err := s.st.GolfClubs(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, c := range list {
		out = append(out, map[string]any{"index": c.Index, "type": c.Type, "name": c.Name, "avg_distance_m": c.AvgDistanceM,
			"max_distance_m": c.MaxDistanceM})
	}
	writeJSON(w, http.StatusOK, out)
}

// deviceSolar returns a watch's solar charge in hourly buckets over a day range (from, to), or
// without one over the last 7 days the watch has data for.
func (s *Server) deviceSolar(w http.ResponseWriter, r *http.Request, u store.User) {
	d, ok := s.userDevice(w, r, u)
	if !ok {
		return
	}
	var from, to int64
	var err error
	if r.URL.Query().Get("from") == "" && r.URL.Query().Get("to") == "" {
		last, err := s.st.DeviceSeriesLast(r.Context(), u.ID, d.ID, "solar")
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		to = last + 3600
		from = to - 7*86400
	} else if from, to, _, _, err = s.unixDays(r, u, 366); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out := map[string]any{"from": from, "to": to}
	for _, kind := range []string{"solar", "solar_gain"} {
		pts, err := s.st.DeviceSeries(r.Context(), u.ID, d.ID, kind, from, to)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		t, v := make([]int64, 0, len(pts)), make([]float64, 0, len(pts))
		for _, p := range pts {
			t, v = append(t, p.TS), append(v, p.Value)
		}
		out[kind] = map[string]any{"t": t, "v": v}
	}
	writeJSON(w, http.StatusOK, out)
}

// deviceChanges pages a watch's settings change log, newest first (?before= Unix seconds).
func (s *Server) deviceChanges(w http.ResponseWriter, r *http.Request, u store.User) {
	d, ok := s.userDevice(w, r, u)
	if !ok {
		return
	}
	var before int64
	if v := r.URL.Query().Get("before"); v != "" {
		var err error
		if before, err = strconv.ParseInt(v, 10, 64); err != nil {
			writeError(w, http.StatusBadRequest, "before must be Unix seconds")
			return
		}
	}
	const limit = 100
	list, err := s.st.SettingsChanges(r.Context(), u.ID, d.ID, before, limit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, c := range list {
		out = append(out, map[string]any{"ts": c.TS, "message": c.Message, "field": c.Field, "value": c.Value, "kind": c.Kind, "sport": c.Sport})
	}
	var next any
	if len(list) == limit {
		next = list[len(list)-1].TS
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "next_before": next})
}

// deviceDocuments returns what a watch's settings, device and multisport files hold beyond
// the tables, message by message.
func (s *Server) deviceDocuments(w http.ResponseWriter, r *http.Request, u store.User) {
	d, ok := s.userDevice(w, r, u)
	if !ok {
		return
	}
	list, err := s.st.DeviceDocuments(r.Context(), u.ID, d.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, x := range list {
		out = append(out, map[string]any{"kind": x.Kind, "recorded_at": x.RecordedAt.Unix(), "data": x.Data})
	}
	writeJSON(w, http.StatusOK, out)
}
