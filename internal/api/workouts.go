// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/karamble/goarmin/internal/figures"
	"github.com/karamble/goarmin/internal/outbox"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/fitwatch"
)

// WorkoutSport is a sport the workout editor offers: the FIT sport and sub sport a watch
// writes for it (as Garmin Connect's, gcexport.connectSports), and its name.
type WorkoutSport struct {
	Sport    string `json:"sport"`
	SubSport string `json:"sub_sport"`
	Label    string `json:"label"`
}

// WorkoutSports are the sports a workout made in goarmin can have: Garmin Connect's, without
// swimming (pool lengths and drills) and breathwork (the watch's own breathing workouts).
var WorkoutSports = []WorkoutSport{
	{"running", "generic", "Running"}, {"running", "trail", "Trail running"}, {"running", "treadmill", "Treadmill running"},
	{"cycling", "generic", "Cycling"}, {"cycling", "road", "Road cycling"}, {"cycling", "mountain", "Mountain biking"},
	{"cycling", "indoor_cycling", "Indoor cycling"},
	{"walking", "generic", "Walking"}, {"hiking", "generic", "Hiking"},
	{"training", "strength_training", "Strength"}, {"training", "cardio_training", "Cardio"}, {"training", "yoga", "Yoga"},
	{"fitness_equipment", "pilates", "Pilates"},
	{"hiit", "generic", "HIIT"}, {"hiit", "amrap", "HIIT: AMRAP"}, {"hiit", "emom", "HIIT: EMOM"}, {"hiit", "tabata", "HIIT: Tabata"},
	{"mobility", "generic", "Mobility"},
	{"fitness_equipment", "elliptical", "Elliptical"}, {"fitness_equipment", "stair_climbing", "Stair climbing"},
	{"rowing", "indoor_rowing", "Indoor rowing"},
}

func offeredSport(sport, sub string) bool {
	for _, s := range WorkoutSports {
		if s.Sport == sport && s.SubSport == sub {
			return true
		}
	}
	return false
}

func (s *Server) workoutSports(w http.ResponseWriter, _ *http.Request, _ store.User) {
	writeJSON(w, http.StatusOK, WorkoutSports)
}

// workoutBody is a whole workout as the editor saves it.
type workoutBody struct {
	Name        string          `json:"name"`
	Sport       string          `json:"sport"`
	SubSport    string          `json:"sub_sport"`
	Description string          `json:"description"`
	Steps       []fitwatch.Step `json:"steps"`
}

// workoutFrom checks a saved workout and returns it ready to store: a sport the editor offers
// (or keep, the sport of the workout being edited), steps a watch takes, exercises from the
// catalogue (named by it), and a file goarmin can write.
func workoutFrom(b workoutBody, keep *store.WorkoutRow) (fitwatch.Workout, string, error) {
	w := fitwatch.Workout{Name: strings.Join(strings.Fields(b.Name), " "), Sport: b.Sport, SubSport: b.SubSport, Steps: b.Steps}
	if w.SubSport == "" {
		w.SubSport = "generic"
	}
	if !offeredSport(w.Sport, w.SubSport) && (keep == nil || keep.Sport != w.Sport || keep.SubSport != w.SubSport) {
		return w, "", fmt.Errorf("goarmin cannot make a workout of %s/%s; GET /api/workout-sports lists the sports", w.Sport, w.SubSport)
	}
	desc := strings.TrimSpace(b.Description)
	if utf8.RuneCountInString(desc) > MaxWorkoutDescription {
		return w, "", fmt.Errorf("the description is longer than %d characters", MaxWorkoutDescription)
	}
	cat, err := figures.Load()
	if err != nil {
		return w, "", err
	}
	for i := range w.Steps {
		e := w.Steps[i].Exercise
		if e == nil {
			continue
		}
		x, _, ok := cat.Lookup(e.Category, e.Number)
		if !ok {
			return w, "", fmt.Errorf("step %d: no exercise %s/%d in the catalogue", i+1, e.Category, e.Number)
		}
		title := strings.TrimSpace(e.Title)
		if title == "" {
			title = x.Title
		}
		w.Steps[i].Exercise = &fitwatch.Exercise{Category: x.Category, Number: x.Number, Name: title, Title: title}
	}
	if err := fitwatch.ValidateWorkout(w); err != nil {
		return w, "", err
	}
	// Whatever the watch would refuse is refused here: the file must be writable.
	if _, err := fitwatch.EncodeWorkout(w, desc, fitwatch.EncodeOptions{Serial: 1, Created: time.Now()}); err != nil {
		return w, "", err
	}
	w.EstimatedS = fitwatch.Estimate(w.Steps)
	return w, desc, nil
}

// createWorkout adds a workout made in the editor to the library.
func (s *Server) createWorkout(w http.ResponseWriter, r *http.Request, u store.User) {
	var b workoutBody
	if !decode(w, r, &b) {
		return
	}
	wk, desc, err := workoutFrom(b, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := s.st.CreateWorkout(r.Context(), u.ID, wk, desc)
	if errors.Is(err, store.ErrWorkoutNameTaken) {
		writeError(w, http.StatusConflict, "another workout in your library has that name")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.writeWorkout(w, r, u, id, http.StatusCreated)
}

// putWorkout saves an edited workout: goarmin's own version of it, which the library then
// shows and sends to the watches it is switched on for (an older version on a watch stays
// until it is deleted there).
func (s *Server) putWorkout(w http.ResponseWriter, r *http.Request, u store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "no such workout")
		return
	}
	var b workoutBody
	if !decode(w, r, &b) {
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
	wk, desc, err := workoutFrom(b, &x.WorkoutRow)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
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
	s.writeWorkout(w, r, u, vid, http.StatusOK)
}

// writeWorkout answers with a library workout as GET shows it.
func (s *Server) writeWorkout(w http.ResponseWriter, r *http.Request, u store.User, id int64, code int) {
	x, watches, err := s.st.LibraryWorkoutByID(r.Context(), u.ID, id)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, code, libraryJSON(x, watches, true))
}

// workoutFIT downloads the version of a workout the library shows as a FIT file: a watch's file
// as it is, goarmin's or Garmin Connect's written as goarmin sends it.
func (s *Server) workoutFIT(w http.ResponseWriter, r *http.Request, u store.User) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "no such workout")
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
	data, err := outbox.WorkoutFile(r.Context(), s.st, s.blobs, store.OutboxEntry{EntryKey: x.EntryKey, Version: x.WorkoutRow, Description: x.Description})
	if err != nil {
		writeError(w, http.StatusConflict, "this workout cannot be written as a file: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.ant.fit")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeName(x.Name)+`.fit"`)
	_, _ = w.Write(data)
}
