// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"errors"
	"net/http"
	"sort"
	"strconv"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/fitwatch"
)

// watchProfile is what the user's watch knows and set: its settings (clock, units, resting HR,
// sleep window, step lengths, height, weight, zones, bikes) and its goals.
func (s *Server) watchProfile(w http.ResponseWriter, r *http.Request, u store.User) {
	goals, err := s.st.WatchGoals(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if goals == nil {
		goals = []fitwatch.Goal{}
	}
	out := map[string]any{"settings": nil, "goals": goals}
	ws, err := s.st.LatestWatchSettings(r.Context(), u.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		s.internalError(w, r, err)
		return
	default:
		out["settings"] = ws.Settings
		out["recorded_at"] = ws.RecordedAt.Unix()
		out["device_id"] = ws.DeviceID
	}
	writeJSON(w, http.StatusOK, out)
}

// zones lists the heart-rate zones the user's watches use: the default (the settings' first
// zone target) and each sport profile's, with the zones in effect for it.
func (s *Server) zones(w http.ResponseWriter, r *http.Request, u store.User) {
	var def *fitwatch.Zones
	if ws, err := s.st.LatestWatchSettings(r.Context(), u.ID); err == nil && len(ws.Settings.Zones) > 0 && ws.Settings.Zones[0].Valid() {
		def = &ws.Settings.Zones[0]
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, r, err)
		return
	}
	rows, err := s.st.SportProfiles(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	seen := map[[3]string]bool{}
	profiles := []map[string]any{}
	for _, p := range rows { // newest first: the first of each profile wins
		k := [3]string{p.Sport, p.SubSport, p.Name}
		if seen[k] {
			continue
		}
		seen[k] = true
		x := map[string]any{"sport": p.Sport, "sub_sport": p.SubSport, "name": p.Name, "zones": p.Zones,
			"recorded_at": p.RecordedAt.Unix(), "own": p.Zones.Valid()}
		switch {
		case p.Zones.Valid():
			x["effective"] = p.Zones
		case def != nil:
			x["effective"] = def
		}
		profiles = append(profiles, x)
	}
	sort.SliceStable(profiles, func(i, j int) bool { return profiles[i]["name"].(string) < profiles[j]["name"].(string) })
	writeJSON(w, http.StatusOK, map[string]any{"default": def, "profiles": profiles})
}

// totals sums the lifetime totals of the user's watches per sport.
func (s *Server) totals(w http.ResponseWriter, r *http.Request, u store.User) {
	rows, err := s.st.LifetimeTotals(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	by := map[string]*fitwatch.Totals{}
	var newest int64
	watches := map[int64]bool{}
	var order []string
	for _, t := range rows {
		x := by[t.Sport]
		if x == nil {
			x = &fitwatch.Totals{Sport: t.Sport}
			by[t.Sport] = x
			order = append(order, t.Sport)
		}
		x.TimerS += t.TimerS
		x.DistanceM += t.DistanceM
		x.Calories += t.Calories
		x.Sessions += t.Sessions
		watches[t.DeviceID] = true
		if at := t.RecordedAt.Unix(); at > newest {
			newest = at
		}
	}
	sports := []map[string]any{}
	for _, sp := range order {
		t := by[sp]
		sports = append(sports, map[string]any{"sport": t.Sport, "timer_s": t.TimerS, "distance_m": t.DistanceM, "kcal": t.Calories, "sessions": t.Sessions})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sports": sports, "watches": len(watches), "recorded_at": newest})
}

func workoutJSON(x store.WorkoutRow, steps bool) map[string]any {
	out := map[string]any{"id": x.ID, "name": x.Name, "sport": x.Sport, "sub_sport": x.SubSport, "estimated_s": x.EstimatedS,
		"step_count": len(x.Steps), "recorded_at": x.RecordedAt.Unix()}
	if steps {
		out["steps"] = x.Steps
	}
	return out
}

func (s *Server) workouts(w http.ResponseWriter, r *http.Request, u store.User) {
	list, err := s.st.Workouts(r.Context(), u.ID, 0)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, x := range list {
		out = append(out, workoutJSON(x, false))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) workout(w http.ResponseWriter, r *http.Request, u store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "no such workout")
		return
	}
	list, err := s.st.Workouts(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such workout")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, workoutJSON(list[0], true))
}

func courseJSON(c store.CourseRow, points bool) map[string]any {
	out := map[string]any{"id": c.ID, "name": c.Name, "sport": c.Sport, "distance_m": c.DistanceM, "ascent_m": c.AscentM,
		"descent_m": c.DescentM, "polyline": c.Polyline, "point_count": len(c.Points), "recorded_at": c.RecordedAt.Unix()}
	if c.Bounds != nil {
		out["bounds"] = c.Bounds
	}
	if points {
		pts := c.Points
		if pts == nil {
			pts = []fitwatch.CoursePoint{}
		}
		out["points"] = pts
	}
	return out
}

func (s *Server) courses(w http.ResponseWriter, r *http.Request, u store.User) {
	list, err := s.st.Courses(r.Context(), u.ID, 0)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, c := range list {
		out = append(out, courseJSON(c, false))
	}
	writeJSON(w, http.StatusOK, out)
}

// userCourse finds one of u's courses from the path, or answers 404.
func (s *Server) userCourse(w http.ResponseWriter, r *http.Request, u store.User) (store.CourseRow, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "no such course")
		return store.CourseRow{}, false
	}
	list, err := s.st.Courses(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such course")
		return store.CourseRow{}, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return store.CourseRow{}, false
	}
	return list[0], true
}

func (s *Server) course(w http.ResponseWriter, r *http.Request, u store.User) {
	if c, ok := s.userCourse(w, r, u); ok {
		writeJSON(w, http.StatusOK, courseJSON(c, true))
	}
}

func (s *Server) courseStreams(w http.ResponseWriter, r *http.Request, u store.User) {
	c, ok := s.userCourse(w, r, u)
	if !ok {
		return
	}
	_, data, err := s.st.CourseStream(r.Context(), u.ID, c.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "this course has no track")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.serveGzipJSON(w, r, data)
}

func (s *Server) courseFIT(w http.ResponseWriter, r *http.Request, u store.User) {
	if c, ok := s.userCourse(w, r, u); ok {
		s.serveFIT(w, r, u, c.RawFileID, safeName(c.Name)+".fit")
	}
}

// safeName keeps a name usable as a file name.
func safeName(s string) string {
	out := []rune{}
	for _, r := range s {
		switch {
		case r == '"' || r == '/' || r == '\\' || r < 32:
			out = append(out, '_')
		default:
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return "course"
	}
	return string(out)
}
