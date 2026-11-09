// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/internal/figures"
	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/outbox"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/fitwatch"
)

// The workout builder: workouts made, edited, duplicated, hidden and downloaded in goarmin.
// A workout is written as Garmin Connect writes one (fitwatch.EncodeWorkout); goarmin keeps it
// as its own version of a library entry (store.SaveWorkoutVersion).

// workoutBody is a workout as the builder sends it: its steps as get_workout returns them.
type workoutBody struct {
	Name        string          `json:"name"`
	Sport       string          `json:"sport"`
	SubSport    string          `json:"sub_sport"`
	Description string          `json:"description"`
	Steps       []fitwatch.Step `json:"steps"`
}

// Limits of a step beyond fitwatch.ValidateWorkout's.
const (
	maxStepName = 40
	maxWeightKg = 500
)

// stepIntensities are the intensities a step may have.
var stepIntensities = map[string]bool{"": true, "active": true, "rest": true, "warmup": true, "cooldown": true, "recovery": true, "interval": true}

// stepDurations are the durations a step may have, besides repeats.
var stepDurations = map[string]bool{"time": true, "distance": true, "reps": true, "calories": true, "open": true,
	"hr_less_than": true, "hr_greater_than": true, "power_less_than": true, "power_greater_than": true}

// stepTargets are the targets a step may aim for ("" none, open none named).
var stepTargets = map[string]bool{"": true, "open": true, "heart_rate": true, "speed": true, "power": true, "cadence": true}

// workoutOf checks a body against what a watch takes and turns it into a workout: names and
// spacing cleaned, each exercise named and titled from the catalogue.
func workoutOf(b workoutBody) (fitwatch.Workout, error) {
	bad := func(format string, a ...any) (fitwatch.Workout, error) {
		return fitwatch.Workout{}, fmt.Errorf(format, a...)
	}
	w := fitwatch.Workout{Name: strings.Join(strings.Fields(b.Name), " "), Sport: b.Sport, SubSport: b.SubSport,
		Description: strings.TrimSpace(b.Description), Steps: b.Steps}
	switch n := utf8.RuneCountInString(w.Name); {
	case n == 0:
		return bad("the workout needs a name")
	case n > fitwatch.MaxWorkoutName:
		return bad("a watch shows at most %d characters of a name", fitwatch.MaxWorkoutName)
	}
	if utf8.RuneCountInString(w.Description) > MaxWorkoutDescription {
		return bad("the description is longer than %d characters", MaxWorkoutDescription)
	}
	if typedef.SportFromString(w.Sport) == typedef.SportInvalid {
		return bad("unknown sport %q", w.Sport)
	}
	if w.SubSport != "" && typedef.SubSportFromString(w.SubSport) == typedef.SubSportInvalid {
		return bad("unknown sub sport %q", w.SubSport)
	}
	cat, err := figures.Load()
	if err != nil {
		return fitwatch.Workout{}, err
	}
	w.Steps = make([]fitwatch.Step, len(b.Steps))
	for i, s := range b.Steps {
		at := fmt.Sprintf("step %d", i+1)
		s.Name, s.Notes = strings.TrimSpace(s.Name), strings.TrimSpace(s.Notes)
		if utf8.RuneCountInString(s.Name) > maxStepName {
			return bad("%s: a step name holds at most %d characters", at, maxStepName)
		}
		if !stepIntensities[s.Intensity] {
			return bad("%s: unknown intensity %q", at, s.Intensity)
		}
		if fitwatch.IsRepeat(s) {
			s.Name, s.Notes, s.Intensity, s.Target, s.Exercise, s.WeightKg = "", "", "", "", nil, 0
			w.Steps[i] = s
			continue
		}
		if !stepDurations[s.Duration] {
			return bad("%s: unknown duration %q", at, s.Duration)
		}
		if s.Duration != "open" && s.DurationValue <= 0 {
			return bad("%s: the %s needs a value above 0", at, strings.ReplaceAll(s.Duration, "_", " "))
		}
		if !stepTargets[s.Target] {
			return bad("%s: unknown target %q", at, s.Target)
		}
		switch {
		case s.Target == "" || s.Target == "open":
			s.TargetZone, s.TargetLow, s.TargetHigh = 0, 0, 0
		case s.TargetZone > 0:
			if s.TargetZone > 7 || s.Target == "heart_rate" && s.TargetZone > 5 {
				return bad("%s: no such zone %d", at, s.TargetZone)
			}
			s.TargetLow, s.TargetHigh = 0, 0
		case s.TargetLow <= 0 || s.TargetHigh <= 0 || s.TargetLow > s.TargetHigh:
			return bad("%s: a %s target needs a zone, or a low and a high with low ≤ high", at, strings.ReplaceAll(s.Target, "_", " "))
		}
		pct := s.Target == "heart_rate" || s.Target == "power" || strings.HasPrefix(s.Duration, "hr_") || strings.HasPrefix(s.Duration, "power_")
		s.TargetPct = s.TargetPct && pct
		if s.WeightKg < 0 || s.WeightKg > maxWeightKg {
			return bad("%s: a weight of 0 to %d kg", at, maxWeightKg)
		}
		if e := s.Exercise; e != nil {
			x, _, ok := cat.Lookup(e.Category, e.Number)
			if !ok {
				return bad("%s: no exercise %s/%d in the catalogue", at, e.Category, e.Number)
			}
			title := strings.TrimSpace(e.Title)
			if title == "" {
				title = x.Title
			}
			s.Exercise = &fitwatch.Exercise{Category: x.Category, Number: x.Number, Name: title, Title: title}
		}
		w.Steps[i] = s
	}
	if err := fitwatch.ValidateWorkout(w); err != nil {
		return fitwatch.Workout{}, errors.New(strings.TrimPrefix(err.Error(), fitwatch.ErrInvalidWorkout.Error()+": "))
	}
	return w, nil
}

// workoutTag is the tag of a library workout's shown version, for If-Match: a PUT on a workout
// that changed meanwhile (another tab, an assistant) answers 412.
func workoutTag(x store.LibraryWorkout) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d\x00%d\x00", x.ID, x.RecordedAt.Unix())
	_ = json.NewEncoder(h).Encode(x.Workout) // what it says, so an edit within the second changes the tag too
	return `"` + hex.EncodeToString(h.Sum(nil))[:20] + `"`
}

// userWorkout finds u's library workout from the path, or answers 404.
func (s *Server) userWorkout(w http.ResponseWriter, r *http.Request, u store.User) (store.LibraryWorkout, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "no such workout")
		return store.LibraryWorkout{}, false
	}
	x, _, err := s.st.LibraryWorkoutByID(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such workout")
		return store.LibraryWorkout{}, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return store.LibraryWorkout{}, false
	}
	return x, true
}

// savedWorkout answers with the library workout a save made (version vid), after recording
// the sha of the file it is written as, so the library tells an older copy on a watch apart.
func (s *Server) savedWorkout(w http.ResponseWriter, r *http.Request, u store.User, vid int64, code int) {
	ctx := r.Context()
	x, watches, err := s.st.LibraryWorkoutByID(ctx, u.ID, vid)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if v := x.Versions[0]; v.Origin == store.OriginGoarmin {
		if b, err := outbox.FileOf(ctx, s.st, s.blobs, x.EntryKey, v); err == nil {
			sum := sha256.Sum256(b)
			if err := s.st.SetWorkoutFileSHA(ctx, u.ID, v.ID, hex.EncodeToString(sum[:])); err != nil {
				s.internalError(w, r, err)
				return
			}
			if x, watches, err = s.st.LibraryWorkoutByID(ctx, u.ID, vid); err != nil {
				s.internalError(w, r, err)
				return
			}
		} else {
			s.log.Warn("api: a saved workout cannot be written as a file", "workout", vid, "err", err)
		}
	}
	s.syncForWorkout(r, x, watches)
	writeJSON(w, code, libraryJSON(x, watches, true))
}

// syncForWorkout starts a sync of each watch the saved workout now waits to go to.
func (s *Server) syncForWorkout(r *http.Request, x store.LibraryWorkout, watches []store.LibraryWatch) {
	if s.opt.Node == nil {
		return
	}
	for i, p := range x.Watches {
		if p.Pending == store.PendingSend && watches[i].Address != "" {
			if err := s.opt.Node.Sync(nodehubRequest(watches[i].Address)); err != nil {
				s.log.Info("api: the watch gets the workout at its next sync", "device", watches[i].ID, "err", err)
			}
		}
	}
}

// nodehubRequest is a sync a workout change asks for.
func nodehubRequest(addr string) nodehub.Request {
	return nodehub.Request{Addr: addr, By: store.StartedByLibrary}
}

// createWorkout makes a workout: a library entry of its own.
func (s *Server) createWorkout(w http.ResponseWriter, r *http.Request, u store.User) {
	var b workoutBody
	if !decode(w, r, &b) {
		return
	}
	wk, err := workoutOf(b)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	vid, err := s.st.CreateWorkout(r.Context(), u.ID, wk)
	if errors.Is(err, store.ErrWorkoutNameTaken) {
		writeError(w, http.StatusConflict, "another workout in your library has that name")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.savedWorkout(w, r, u, vid, http.StatusCreated)
}

// putWorkout edits a library workout in place: goarmin's own version of it, the original
// kept. An original's file fields are kept; its stated total only while its steps stay the
// same. If-Match, when sent, must name the version shown (412 otherwise).
func (s *Server) putWorkout(w http.ResponseWriter, r *http.Request, u store.User) {
	x, ok := s.userWorkout(w, r, u)
	if !ok {
		return
	}
	if m := r.Header.Get("If-Match"); m != "" && m != workoutTag(x) {
		writeError(w, http.StatusPreconditionFailed, "the workout changed meanwhile; reload it")
		return
	}
	var b workoutBody
	if !decode(w, r, &b) {
		return
	}
	wk, err := workoutOf(b)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wk.Extra = editedExtra(x.Workout, wk)
	vid, err := s.st.SaveWorkoutVersion(r.Context(), u.ID, x.EntryKey, wk)
	if errors.Is(err, store.ErrWorkoutNameTaken) {
		writeError(w, http.StatusConflict, "another workout in your library has that name")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.savedWorkout(w, r, u, vid, http.StatusOK)
}

// editedExtra is what an edited workout keeps of the file fields its version had: the workout
// message's unnamed fields and the exercise titles' order, written as Garmin Connect writes
// today (with a description memo, secondary target values and titles, whatever wrote the
// original). The stated total stays while the steps take as long as before; otherwise goarmin
// states the steps' time.
func editedExtra(old, w fitwatch.Workout) *fitwatch.WorkoutExtra {
	if old.Extra == nil {
		return nil
	}
	x := *old.Extra
	x.Software, x.NoDescriptionMemo, x.NoSecondary, x.NoTitles = nil, false, false, false
	if !sameTiming(old.Steps, w.Steps) {
		x.F10, x.F21 = nil, nil
	}
	return &x
}

// sameTiming reports whether two step lists take the same time: the same durations and
// repeats in the same order (names, notes, targets and exercises aside).
func sameTiming(a, b []fitwatch.Step) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Duration != b[i].Duration || a[i].DurationValue != b[i].DurationValue || a[i].From != b[i].From || a[i].Repeat != b[i].Repeat {
			return false
		}
	}
	return true
}

// copySuffix finds a name's trailing copy number: "Tempo 2" is Tempo's second.
var copySuffix = regexp.MustCompile(`^(.*\S) (\d+)$`)

// duplicateWorkout makes a new workout from a library workout's shown version, named "‹name›
// 2" (or the next free number) unless the body names it.
func (s *Server) duplicateWorkout(w http.ResponseWriter, r *http.Request, u store.User) {
	x, ok := s.userWorkout(w, r, u)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	wk := x.Workout
	if x.Extra != nil { // a copy keeps what its file says, written by today's Garmin Connect
		extra := *x.Extra
		extra.Software = nil
		wk.Extra = &extra
	}
	base, n := x.Name, 2
	if m := copySuffix.FindStringSubmatch(base); m != nil {
		base = m[1]
		n, _ = strconv.Atoi(m[2])
		n++
	}
	for tries := 0; ; tries++ {
		wk.Name = strings.Join(strings.Fields(req.Name), " ")
		if wk.Name == "" {
			suffix := " " + strconv.Itoa(n+tries)
			wk.Name = truncateRunes(base, fitwatch.MaxWorkoutName-utf8.RuneCountInString(suffix)) + suffix
		}
		if utf8.RuneCountInString(wk.Name) > fitwatch.MaxWorkoutName {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("a watch shows at most %d characters of a name", fitwatch.MaxWorkoutName))
			return
		}
		vid, err := s.st.CreateWorkout(r.Context(), u.ID, wk)
		switch {
		case errors.Is(err, store.ErrWorkoutNameTaken) && req.Name == "" && tries < 50:
			continue
		case errors.Is(err, store.ErrWorkoutNameTaken):
			writeError(w, http.StatusConflict, "another workout in your library has that name")
		case err != nil:
			s.internalError(w, r, err)
		default:
			s.savedWorkout(w, r, u, vid, http.StatusCreated)
		}
		return
	}
}

func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n]))
}

// hideWorkout hides a library workout (POST) or shows it again (DELETE). A hidden workout shows
// while a watch has it.
func (s *Server) hideWorkout(w http.ResponseWriter, r *http.Request, u store.User) {
	x, ok := s.userWorkout(w, r, u)
	if !ok {
		return
	}
	if err := s.st.SetWorkoutHidden(r.Context(), u.ID, x.EntryKey, r.Method == http.MethodPost); err != nil {
		s.internalError(w, r, err)
		return
	}
	x, watches, err := s.st.LibraryWorkoutByID(r.Context(), u.ID, x.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, libraryJSON(x, watches, true))
}

// unsafeName are the characters a download's file name leaves out.
var unsafeName = regexp.MustCompile(`[^\pL\pN _.,()&+-]+`)

// workoutFIT downloads a library workout as the file a watch gets of it: a watch's own as it
// came, else written as Garmin Connect writes one.
func (s *Server) workoutFIT(w http.ResponseWriter, r *http.Request, u store.User) {
	x, ok := s.userWorkout(w, r, u)
	if !ok {
		return
	}
	name := strings.TrimSpace(unsafeName.ReplaceAllString(x.Name, ""))
	if name == "" {
		name = "workout"
	}
	b, err := outbox.FileOf(r.Context(), s.st, s.blobs, x.EntryKey, x.Versions[0])
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "this workout cannot be written as a file: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/vnd.ant.fit")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.fit"`)
	_, _ = w.Write(b)
}
