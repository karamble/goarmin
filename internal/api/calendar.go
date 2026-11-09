// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/fitactivity"
)

// Kinds of a calendar day: what was recorded or planned. The dashboard draws one bar each.
const (
	kindActivity    = "activity"
	kindPlan        = "plan"
	kindSteps       = "steps"
	kindSleep       = "sleep"
	kindHRV         = "hrv"
	kindNap         = "nap"
	kindHeart       = "heart"
	kindECG         = "ecg"
	kindStress      = "stress"
	kindBodyBattery = "body_battery"
	kindWeighIn     = "weigh_in"
	kindDrink       = "drink"
)

// sportFamily groups sports for matching a plan with what was recorded: a planned run is done
// by a trail run or a walk, a ride by any ride (the Coach page's rule).
func sportFamily(s string) string {
	s = strings.ToLower(s)
	switch {
	case strings.Contains(s, "run"), strings.Contains(s, "walk"), strings.Contains(s, "hik"):
		return "run"
	case strings.Contains(s, "cycl"), strings.Contains(s, "bik"):
		return "bike"
	case strings.Contains(s, "swim"):
		return "swim"
	}
	return s
}

// fits says whether an activity counts for a planned sport (and sub sport, "" or generic for
// none): the same family, or the same particular sub sport (a pilates plan, a pilates session).
func fits(sport, sub string, a store.ActivitySummary) bool {
	if sport == "" || sportFamily(sport) == sportFamily(typedef.Sport(a.Sport).String()) {
		return true
	}
	return sub != "" && sub != "generic" && sub == typedef.SubSport(a.SubSport).String()
}

// calendarDay is one day of the calendar while it is put together.
type calendarDay struct {
	summary    map[string]any
	kinds      map[string]int
	activities []store.ActivitySummary
	coach      []map[string]any
	plans      []map[string]any
}

// calendar serves the days of [from, to] (at most 93) that have anything: the day's summary as
// /api/daily has it, which kinds of data it holds, its activities, Garmin Coach's workouts and
// the user's own plans, each plan done (with the activity), missed, or still ahead.
func (s *Server) calendar(w http.ResponseWriter, r *http.Request, u store.User) {
	from, to, err := dayRange(r, 93)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	days, today, err := s.calendarDays(r.Context(), u.ID, from, to)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "today": today, "days": days})
}

func (s *Server) calendarDays(ctx context.Context, user int64, from, to string) ([]map[string]any, string, error) {
	z, err := s.st.Zone(ctx, user)
	if err != nil {
		return nil, "", err
	}
	today := z.Day(time.Now().Unix())
	byDay := map[string]*calendarDay{}
	day := func(d string) *calendarDay {
		x := byDay[d]
		if x == nil {
			x = &calendarDay{kinds: map[string]int{}}
			byDay[d] = x
		}
		return x
	}

	summaries, err := s.dailyJSON(ctx, user, from, to)
	if err != nil {
		return nil, "", err
	}
	for _, m := range summaries {
		x := day(m["day"].(string))
		x.summary = m
		set := func(kind string, ok bool) {
			if ok {
				x.kinds[kind] = 1
			}
		}
		set(kindSteps, positive(m["steps"]))
		set(kindHeart, positive(m["resting_hr"]) || positive(m["hr_avg"]))
		set(kindStress, positive(m["stress_avg"]))
		set(kindBodyBattery, positive(m["body_battery_max"]))
		set(kindSleep, positive(m["sleep_s"]) || positive(m["sleep_score"]))
	}
	counts, err := s.st.CalendarCounts(ctx, user, from, to)
	if err != nil {
		return nil, "", err
	}
	for d, c := range counts {
		x := day(d)
		for k, n := range map[string]string{store.CountNights: kindSleep, store.CountNaps: kindNap, store.CountHRV: kindHRV,
			store.CountWeighIns: kindWeighIn, store.CountECG: kindECG, store.CountDrinks: kindDrink} {
			if c[k] > 0 {
				x.kinds[n] = max(x.kinds[n], c[k])
			}
		}
	}
	acts, err := s.st.ActivitiesIn(ctx, user, from, to)
	if err != nil {
		return nil, "", err
	}
	for i := len(acts) - 1; i >= 0; i-- { // oldest first within a day
		x := day(acts[i].Day)
		x.activities = append(x.activities, acts[i])
		x.kinds[kindActivity]++
	}

	// Garmin Coach: a day's workouts count as done by any activity of their sport.
	coach, err := s.st.SuggestedWorkouts(ctx, user, from, to)
	if err != nil {
		return nil, "", err
	}
	for _, c := range coach {
		sport := ""
		if c.Sport != nil {
			sport = typedef.Sport(*c.Sport).String()
		}
		rest := c.Name == "" && c.DurationS == 0
		x := day(c.Day)
		done := false
		for _, a := range x.activities {
			if !rest && fits(sport, "", a) {
				done = true
			}
		}
		x.coach = append(x.coach, map[string]any{"index": c.Index, "name": c.Name, "description": c.Description, "sport": sport,
			"duration_s": c.DurationS, "benefit": c.Benefit, "aerobic_te": c.AerobicTE, "rest_day": rest, "done": done,
			"missed": !rest && !done && c.Day < today})
		if !rest {
			x.kinds[kindPlan]++
		}
	}

	// Own plans, matched with the day's activities: by the workout's name first, then by sport.
	plans, err := s.st.Plans(ctx, user, from, to)
	if err != nil {
		return nil, "", err
	}
	if len(plans) > 0 {
		lib, err := s.st.WorkoutLibrary(ctx, user)
		if err != nil {
			return nil, "", err
		}
		used := map[int64]bool{}
		for _, p := range plans {
			x := day(p.Day)
			x.plans = append(x.plans, planJSON(p, resolvePlan(lib, p), x.activities, used, today))
			x.kinds[kindPlan]++
		}
	}

	out := []map[string]any{}
	for d := from; d <= to; d = nextDay(d) {
		x := byDay[d]
		if x == nil || len(x.kinds) == 0 && len(x.coach) == 0 {
			continue
		}
		var summary any
		if x.summary != nil {
			summary = x.summary
		}
		brief := []map[string]any{}
		for _, a := range x.activities {
			brief = append(brief, map[string]any{"id": a.ID, "name": a.Name, "start": a.Start, "end": a.End,
				"sport": typedef.Sport(a.Sport).String(), "sub_sport": typedef.SubSport(a.SubSport).String(),
				"sport_label": fitactivity.Label(typedef.Sport(a.Sport), typedef.SubSport(a.SubSport), a.Name),
				"timer_s":     a.Timer, "distance_m": a.Distance, "workout": a.WorkoutName})
		}
		out = append(out, map[string]any{"day": d, "summary": summary, "kinds": x.kinds, "activities": brief,
			"coach": orEmptyList(x.coach), "plans": orEmptyList(x.plans)})
	}
	return out, today, nil
}

func orEmptyList(l []map[string]any) []map[string]any {
	if l == nil {
		return []map[string]any{}
	}
	return l
}

// positive says whether a summary value is there and above zero.
func positive(v any) bool {
	switch x := v.(type) {
	case *int64:
		return x != nil && *x > 0
	case *float64:
		return x != nil && *x > 0
	case int64:
		return x > 0
	case float64:
		return x > 0
	}
	return false
}

func nextDay(d string) string {
	t, err := time.Parse(time.DateOnly, d)
	if err != nil {
		return "9999-12-31"
	}
	return t.AddDate(0, 0, 1).Format(time.DateOnly)
}

// resolvePlan finds a plan's workout in the library: the version it was planned with, or else
// the workout of the same name and sport (a newer version), or nil when it is gone.
func resolvePlan(lib store.Library, p store.WorkoutPlan) *store.LibraryWorkout {
	key := store.WorkoutKey(p.Name, p.Sport)
	for i := range lib.Workouts {
		w := &lib.Workouts[i]
		for _, v := range w.Versions {
			if v.ID == p.WorkoutID {
				return w
			}
		}
	}
	for i := range lib.Workouts {
		w := &lib.Workouts[i]
		if store.WorkoutKey(w.Name, w.Sport) == key || slices.ContainsFunc(w.Versions, func(v store.WorkoutRow) bool {
			return store.WorkoutKey(v.Name, v.Sport) == key // a renamed workout by its old name
		}) {
			return w
		}
	}
	return nil
}

// planJSON is one plan with its workout and whether it is done: an activity of that day that
// followed a workout of its name, else one of its sport, each activity counting for one plan.
func planJSON(p store.WorkoutPlan, w *store.LibraryWorkout, acts []store.ActivitySummary, used map[int64]bool, today string) map[string]any {
	m := map[string]any{"id": p.ID, "day": p.Day, "workout_id": p.WorkoutID, "name": p.Name, "sport": p.Sport, "note": p.Note,
		"missing": w == nil}
	sub := ""
	if w != nil {
		m["workout_id"], m["name"], m["sport"], sub = w.ID, w.Name, w.Sport, w.SubSport
		m["sub_sport"], m["estimated_s"], m["step_count"] = w.SubSport, w.EstimatedS, len(w.Steps)
	}
	var done *int64
	for _, byName := range []bool{true, false} {
		for _, a := range acts {
			if done != nil || used[a.ID] {
				continue
			}
			if byName && a.WorkoutName != "" && strings.EqualFold(strings.TrimSpace(a.WorkoutName), strings.TrimSpace(p.Name)) ||
				!byName && fits(p.Sport, sub, a) {
				id := a.ID
				done = &id
				used[id] = true
			}
		}
	}
	m["done_activity"] = done
	m["missed"] = done == nil && p.Day < today
	return m
}

// planRequest is a plan as the dashboard sends it.
type planRequest struct {
	Day       *string `json:"day"`
	WorkoutID int64   `json:"workout_id"`
	Note      *string `json:"note"`
}

// validPlanDay checks a plan's day: a date within five years of today.
func validPlanDay(d string) error {
	t, err := time.Parse(time.DateOnly, d)
	if err != nil {
		return errors.New("day must be YYYY-MM-DD")
	}
	if now := time.Now(); t.Before(now.AddDate(-5, 0, -1)) || t.After(now.AddDate(5, 0, 1)) {
		return errors.New("day must be within five years of today")
	}
	return nil
}

func validNote(n *string) (string, error) {
	if n == nil {
		return "", nil
	}
	v := strings.TrimSpace(*n)
	if utf8.RuneCountInString(v) > 500 {
		return "", errors.New("a note is at most 500 characters")
	}
	return v, nil
}

// addPlan puts a library workout on a day.
func (s *Server) addPlan(w http.ResponseWriter, r *http.Request, u store.User) {
	var req planRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Day == nil {
		writeError(w, http.StatusBadRequest, "a plan needs its day")
		return
	}
	if err := validPlanDay(*req.Day); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	note, err := validNote(req.Note)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wk, _, err := s.st.LibraryWorkoutByID(r.Context(), u.ID, req.WorkoutID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such workout")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	p, err := s.st.AddPlan(r.Context(), u.ID, store.WorkoutPlan{Day: *req.Day, WorkoutID: wk.ID, Name: wk.Name, Sport: wk.Sport, Note: note})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.writePlan(w, r, u, p, http.StatusCreated)
}

// patchPlan moves a plan to another day or changes its note.
func (s *Server) patchPlan(w http.ResponseWriter, r *http.Request, u store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "no such plan")
		return
	}
	var req planRequest
	if !decode(w, r, &req) {
		return
	}
	if req.WorkoutID != 0 {
		writeError(w, http.StatusBadRequest, "a plan keeps its workout; remove it and plan another")
		return
	}
	if req.Day != nil {
		if err := validPlanDay(*req.Day); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	var note *string
	if req.Note != nil {
		n, err := validNote(req.Note)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		note = &n
	}
	p, err := s.st.UpdatePlan(r.Context(), u.ID, id, req.Day, note)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such plan")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.writePlan(w, r, u, p, http.StatusOK)
}

// deletePlan removes a plan.
func (s *Server) deletePlan(w http.ResponseWriter, r *http.Request, u store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "no such plan")
		return
	}
	if err := s.st.DeletePlan(r.Context(), u.ID, id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such plan")
		return
	} else if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writePlan answers with a plan as the calendar shows it on its day.
func (s *Server) writePlan(w http.ResponseWriter, r *http.Request, u store.User, p store.WorkoutPlan, code int) {
	ctx := r.Context()
	lib, err := s.st.WorkoutLibrary(ctx, u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	acts, err := s.st.ActivitiesIn(ctx, u.ID, p.Day, p.Day)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	z, err := s.st.Zone(ctx, u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, code, planJSON(p, resolvePlan(lib, p), acts, map[int64]bool{}, z.Day(time.Now().Unix())))
}
