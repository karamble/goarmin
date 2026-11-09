// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/karamble/goarmin/internal/store"
)

// settingKeys are the per-user settings the dashboard may store, with their size limits and a
// shape check. Anything else is refused, so the table never becomes a general blob store.
var settingKeys = map[string]struct {
	max   int
	check func(json.RawMessage) bool
}{
	"glance.layout": {16 << 10, func(b json.RawMessage) bool {
		var v struct {
			V     *int              `json:"v"`
			Cards []json.RawMessage `json:"cards"`
		}
		return json.Unmarshal(b, &v) == nil && v.V != nil && *v.V >= 1 && v.Cards != nil && len(v.Cards) <= 60
	}},
	"prefs": {4 << 10, func(b json.RawMessage) bool {
		_, err := parsePrefs(b)
		return err == nil
	}},
}

// Prefs are a user's preferences. A missing field means the default: as the watch where it
// has the value (clock, intensity goal, zones), else Monday weeks and the browser's clock.
type Prefs struct {
	V              int         `json:"v"`
	WeekStart      string      `json:"week_start,omitempty"`     // monday, sunday, saturday
	Clock          string      `json:"clock,omitempty"`          // watch, browser, 24h, 12h
	IntensityGoal  int         `json:"intensity_goal,omitempty"` // minutes a week; 0 = the watch's goal
	Zones          *PrefsZones `json:"zones,omitempty"`
	OnboardingDone bool        `json:"onboarding_done,omitempty"`
	// CalendarHidden are the calendar's kinds the user hid (calendarKinds).
	CalendarHidden []string `json:"calendar_hidden,omitempty"`
}

// calendarKinds are the kinds a calendar day can show, which prefs may hide.
var calendarKinds = []string{kindActivity, kindPlan, kindSteps, kindSleep, kindHRV, kindNap, kindHeart, kindECG, kindStress,
	kindBodyBattery, kindWeighIn, kindDrink}

// PrefsZones chooses the heart-rate zones: the watch's (default), custom (a maximum, with five
// limits or the usual shares of it), or an estimate from the highest heart rate seen.
type PrefsZones struct {
	Source string `json:"source"`
	MaxHR  int    `json:"max_hr,omitempty"`
	Edges  []int  `json:"edges,omitempty"`
}

// parsePrefs reads and checks preferences; unknown fields and values out of range are errors.
func parsePrefs(b []byte) (Prefs, error) {
	var p Prefs
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, err
	}
	in := func(v string, ok ...string) bool { return v == "" || slices.Contains(ok, v) }
	switch {
	case p.V < 1:
		return p, errors.New("prefs: v must be at least 1")
	case !in(p.WeekStart, "monday", "sunday", "saturday"):
		return p, errors.New("prefs: week_start must be monday, sunday or saturday")
	case !in(p.Clock, "watch", "browser", "24h", "12h"):
		return p, errors.New("prefs: clock must be watch, browser, 24h or 12h")
	case p.IntensityGoal != 0 && (p.IntensityGoal < 30 || p.IntensityGoal > 1000):
		return p, errors.New("prefs: intensity_goal must be 30–1000 minutes")
	}
	for _, k := range p.CalendarHidden {
		if !slices.Contains(calendarKinds, k) {
			return p, errors.New("prefs: calendar_hidden holds an unknown kind")
		}
	}
	if z := p.Zones; z != nil {
		switch {
		case !slices.Contains([]string{"watch", "custom", "estimate"}, z.Source):
			return p, errors.New("prefs: zones.source must be watch, custom or estimate")
		case z.MaxHR != 0 && (z.MaxHR < 100 || z.MaxHR > 240):
			return p, errors.New("prefs: zones.max_hr must be 100–240")
		case z.Source == "custom" && z.MaxHR == 0:
			return p, errors.New("prefs: custom zones need max_hr")
		case len(z.Edges) != 0 && len(z.Edges) != 5:
			return p, errors.New("prefs: zones.edges are the bottoms of zones 1–5")
		}
		for i, e := range z.Edges {
			if e < 40 || e > 240 || (i > 0 && e <= z.Edges[i-1]) || (z.MaxHR != 0 && e >= z.MaxHR) {
				return p, errors.New("prefs: zones.edges must rise, from 40 bpm and below max_hr")
			}
		}
	}
	return p, nil
}

type settingJSON struct {
	Value     json.RawMessage `json:"value"`
	Rev       int64           `json:"rev"`
	UpdatedAt *time.Time      `json:"updated_at,omitempty"`
}

func toSettingJSON(st store.Setting) settingJSON {
	t := st.UpdatedAt
	return settingJSON{Value: json.RawMessage(st.Value), Rev: st.Rev, UpdatedAt: &t}
}

func (s *Server) allSettings(w http.ResponseWriter, r *http.Request, u store.User) {
	all, err := s.st.Settings(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := map[string]settingJSON{}
	for k, st := range all {
		if _, ok := settingKeys[k]; ok {
			out[k] = toSettingJSON(st)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getSetting(w http.ResponseWriter, r *http.Request, u store.User) {
	key := r.PathValue("key")
	if _, ok := settingKeys[key]; !ok {
		writeError(w, http.StatusNotFound, "no such setting")
		return
	}
	st, err := s.st.Setting(r.Context(), u.ID, key)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusOK, settingJSON{Value: json.RawMessage("null"), Rev: 0})
	case err != nil:
		s.internalError(w, r, err)
	default:
		w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(st.Rev, 10)))
		writeJSON(w, http.StatusOK, toSettingJSON(st))
	}
}

// putSetting stores the raw JSON body. If-Match carries the revision the client last read
// ("0" for unset); without it the write is unconditional.
func (s *Server) putSetting(w http.ResponseWriter, r *http.Request, u store.User) {
	key := r.PathValue("key")
	spec, ok := settingKeys[key]
	if !ok {
		writeError(w, http.StatusNotFound, "no such setting")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(spec.max)+1))
	if err != nil || len(body) > spec.max {
		writeError(w, http.StatusRequestEntityTooLarge, "the setting is too large")
		return
	}
	if !json.Valid(body) || !spec.check(body) {
		writeError(w, http.StatusBadRequest, "the setting has the wrong shape")
		return
	}
	ifRev := int64(-1)
	if m := strings.Trim(r.Header.Get("If-Match"), `" `); m != "" {
		if ifRev, err = strconv.ParseInt(m, 10, 64); err != nil || ifRev < 0 {
			writeError(w, http.StatusBadRequest, "If-Match must be a revision number")
			return
		}
	}
	rev, err := s.st.PutSetting(r.Context(), u.ID, key, string(body), ifRev)
	if errors.Is(err, store.ErrStale) {
		writeError(w, http.StatusPreconditionFailed, "the setting changed on another device")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(rev, 10)))
	now := time.Now()
	writeJSON(w, http.StatusOK, settingJSON{Value: body, Rev: rev, UpdatedAt: &now})
}

func (s *Server) deleteSetting(w http.ResponseWriter, r *http.Request, u store.User) {
	key := r.PathValue("key")
	if _, ok := settingKeys[key]; !ok {
		writeError(w, http.StatusNotFound, "no such setting")
		return
	}
	if err := s.st.DeleteSetting(r.Context(), u.ID, key); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// patchMe changes the profile: display name, time zone (an IANA name, or "" for the
// server's), units.
func (s *Server) patchMe(w http.ResponseWriter, r *http.Request, u store.User) {
	var req struct {
		DisplayName *string `json:"display_name"`
		Timezone    *string `json:"timezone"`
		Units       *string `json:"units"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.DisplayName != nil {
		n := strings.TrimSpace(*req.DisplayName)
		if utf8.RuneCountInString(n) > 100 {
			writeError(w, http.StatusBadRequest, "the name may have at most 100 characters")
			return
		}
		req.DisplayName = &n
	}
	if req.Timezone != nil && *req.Timezone != "" {
		if _, err := time.LoadLocation(*req.Timezone); err != nil || strings.Contains(*req.Timezone, "..") {
			writeError(w, http.StatusBadRequest, "unknown time zone")
			return
		}
	}
	if req.Units != nil && *req.Units != "metric" && *req.Units != "imperial" {
		writeError(w, http.StatusBadRequest, "units must be metric or imperial")
		return
	}
	if err := s.st.UpdateProfile(r.Context(), u.ID, store.Profile{DisplayName: req.DisplayName, Timezone: req.Timezone, Units: req.Units}); err != nil {
		s.internalError(w, r, err)
		return
	}
	nu, err := s.st.User(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.me(w, r, nu)
}
