// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/karamble/goarmin/internal/nodehub"
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
		"step_count": len(x.Steps), "recorded_at": x.RecordedAt.Unix(), "origin": x.Origin}
	exercises := map[[2]any]bool{}
	for _, st := range x.Steps {
		if st.Exercise != nil {
			exercises[[2]any{st.Exercise.Category, st.Exercise.Number}] = true
		}
	}
	out["exercise_count"] = len(exercises)
	if x.Description != "" {
		out["description"] = x.Description
	}
	if steps {
		out["steps"] = x.Steps
	}
	return out
}

// libraryJSON is a library workout: the version shown, where its versions came from, and on
// which of the account's watches it is (watches in the library's order).
func libraryJSON(x store.LibraryWorkout, watches []store.LibraryWatch, steps bool) map[string]any {
	out := workoutJSON(x.WorkoutRow, steps)
	out["origins"] = x.Origins
	out["made_here"] = !slices.ContainsFunc(x.Versions, func(v store.WorkoutRow) bool { return v.Origin != store.OriginGoarmin })
	ws := []map[string]any{}
	for i, p := range x.Watches {
		d := watches[i]
		name := d.Nickname
		if name == "" {
			name = d.Name
		}
		m := map[string]any{"device_id": p.DeviceID, "name": name, "state": p.State, "want": p.Want}
		if !p.At.IsZero() {
			m["since"] = p.At.Unix()
		}
		if p.Pending != "" {
			m["pending"] = p.Pending
		}
		if p.Failed != "" {
			m["failed"] = p.Failed
		}
		if p.OldCopies > 0 {
			m["old_copies"] = p.OldCopies
		}
		if !d.InventoryAt.IsZero() {
			m["checked_at"] = d.InventoryAt.Unix()
		}
		ws = append(ws, m)
	}
	out["watches"] = ws
	if steps {
		vs := []map[string]any{}
		for _, v := range x.Versions {
			vs = append(vs, map[string]any{"id": v.ID, "origin": v.Origin, "device_id": v.DeviceID, "recorded_at": v.RecordedAt.Unix(),
				"step_count": len(v.Steps)})
		}
		out["versions"] = vs
	}
	return out
}

// workouts lists the account's workout library: every workout once, with where it came from
// and on which watches it is.
func (s *Server) workouts(w http.ResponseWriter, r *http.Request, u store.User) {
	lib, err := s.st.WorkoutLibrary(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, x := range lib.Workouts {
		out = append(out, libraryJSON(x, lib.Watches, false))
	}
	writeJSON(w, http.StatusOK, out)
}

// workout is one library workout with its steps, by the id of any of its versions.
func (s *Server) workout(w http.ResponseWriter, r *http.Request, u store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "no such workout")
		return
	}
	x, watches, err := s.st.LibraryWorkoutByID(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such workout")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, libraryJSON(x, watches, true))
}

// setWorkoutWatch switches a library workout on or off for one of the account's watches. On,
// the watch gets it at its next sync (which starts now) if it lacks it; off, it stays on the
// watch until deleted there, which goarmin cannot do.
func (s *Server) setWorkoutWatch(w http.ResponseWriter, r *http.Request, u store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "no such workout")
		return
	}
	dev, err := strconv.ParseInt(r.PathValue("device"), 10, 64)
	if err != nil || dev <= 0 {
		writeError(w, http.StatusNotFound, "no such watch")
		return
	}
	var req struct {
		On *bool `json:"on"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.On == nil {
		writeError(w, http.StatusBadRequest, `say {"on": true} or {"on": false}`)
		return
	}
	x, watches, err := s.st.LibraryWorkoutByID(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such workout")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	i := slices.IndexFunc(watches, func(lw store.LibraryWatch) bool { return lw.ID == dev })
	if i < 0 {
		writeError(w, http.StatusNotFound, "no such watch of yours")
		return
	}
	if err := s.st.SetWorkoutTarget(r.Context(), u.ID, dev, x.EntryKey, *req.On); err != nil {
		s.internalError(w, r, err)
		return
	}
	x, watches, err = s.st.LibraryWorkoutByID(r.Context(), u.ID, id)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if x.Watches[i].Pending == store.PendingSend && s.opt.Node != nil && watches[i].Address != "" {
		if err := s.opt.Node.Sync(nodehub.Request{Addr: watches[i].Address, By: store.StartedByLibrary}); err != nil {
			s.log.Info("api: the watch gets the workout at its next sync", "device", dev, "err", err)
		}
	}
	writeJSON(w, http.StatusOK, libraryJSON(x, watches, true))
}

// MaxWorkoutDescription is the longest description goarmin keeps for a workout (Garmin
// Connect's run to about 400 characters).
const MaxWorkoutDescription = 1000

// workoutPatch renames a workout or changes its description.
type workoutPatch struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// patchWorkout renames a library workout or changes its description: goarmin keeps its own
// version of it (its steps as shown, the new name), which the library then shows, and which
// goarmin sends to a watch in place of the original.
func (s *Server) patchWorkout(w http.ResponseWriter, r *http.Request, u store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "no such workout")
		return
	}
	var req workoutPatch
	if !decode(w, r, &req) {
		return
	}
	if req.Name == nil && req.Description == nil {
		writeError(w, http.StatusBadRequest, "give a name or a description")
		return
	}
	x, _, err := s.st.LibraryWorkoutByID(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such workout")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	wk, desc := x.Workout, x.Description
	if req.Name != nil {
		name := strings.Join(strings.Fields(*req.Name), " ")
		switch n := utf8.RuneCountInString(name); {
		case n == 0:
			writeError(w, http.StatusBadRequest, "the workout needs a name")
			return
		case n > fitwatch.MaxWorkoutName:
			writeError(w, http.StatusBadRequest, fmt.Sprintf("a watch shows at most %d characters of a name", fitwatch.MaxWorkoutName))
			return
		}
		wk.Name = name
	}
	if req.Description != nil {
		desc = strings.TrimSpace(*req.Description)
		if utf8.RuneCountInString(desc) > MaxWorkoutDescription {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("the description is longer than %d characters", MaxWorkoutDescription))
			return
		}
	}
	vid, err := s.st.SaveWorkoutVersion(r.Context(), u.ID, x.EntryKey, wk, desc)
	if errors.Is(err, store.ErrWorkoutNameTaken) {
		writeError(w, http.StatusConflict, "another workout in your library has that name")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	x, watches, err := s.st.LibraryWorkoutByID(r.Context(), u.ID, vid)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, libraryJSON(x, watches, true))
}

// deleteWorkout removes goarmin's own version of a workout (a rename or edit): the library
// shows the original again. The originals from a watch or Garmin Connect cannot be removed.
func (s *Server) deleteWorkout(w http.ResponseWriter, r *http.Request, u store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "no such workout")
		return
	}
	if err := s.st.DeleteWorkoutVersion(r.Context(), u.ID, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such workout of goarmin's own; a watch's or Garmin Connect's cannot be removed")
		return
	} else if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
