// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Command devseed fills a scratch goarmin database with plausible made-up health data, so the
// dashboard's week, month and year views can be looked at before real history exists. It writes
// through the store's own ingest path (one record per day, source "devseed", no blob) and
// rebuilds the daily summaries. Every time is derived from now. It refuses the real data
// directory.
//
//	go run ./tools/devseed -data /tmp/scratch -email tester@example.invalid [-days 400]
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/karamble/goarmin/internal/config"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/fitactivity"
	"github.com/karamble/goarmin/pkg/fitwatch"
	"github.com/karamble/goarmin/pkg/garmin/gcexport"
	"github.com/karamble/goarmin/pkg/garmin/wellness"
	"github.com/karamble/goarmin/pkg/geo"
	"github.com/muktihari/fit/profile/typedef"
)

func main() {
	data := flag.String("data", "", "scratch data directory (never the real one)")
	email := flag.String("email", "", "the account to fill (it must exist: create it in the dashboard first)")
	n := flag.Int("days", 400, "how many days back from today")
	flag.Parse()
	if err := run(context.Background(), *data, *email, *n, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "devseed:", err)
		os.Exit(1)
	}
}

// checkTarget refuses an empty or the real data directory.
func checkTarget(dir string) error {
	if dir == "" {
		return errors.New("say which scratch data directory to fill (-data)")
	}
	a, err1 := filepath.Abs(dir)
	b, err2 := filepath.Abs(config.DataDir())
	if err1 != nil || err2 != nil || filepath.Clean(a) == filepath.Clean(b) {
		return fmt.Errorf("refusing %s: devseed only fills a scratch data directory", dir)
	}
	return nil
}

func run(ctx context.Context, dir, email string, n int, now time.Time) error {
	if err := checkTarget(dir); err != nil {
		return err
	}
	st, err := store.Open(ctx, filepath.Join(dir, "goarmin.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	u, err := st.UserByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("account %q: %w", email, err)
	}
	loc := time.Local
	if u.Timezone != "" {
		if l, err := time.LoadLocation(u.Timezone); err == nil {
			loc = l
		}
	}
	days, err := seed(ctx, st, u.ID, n, now, loc)
	if err != nil {
		return err
	}
	fmt.Printf("devseed: %d days written for %s; done\n", len(days), email)
	return nil
}

// seed writes n days ending today for user and rebuilds their summaries; it returns the days.
func seed(ctx context.Context, st *store.Store, user int64, n int, now time.Time, loc *time.Location) ([]string, error) {
	touched := map[string]bool{}
	today := now.In(loc)
	midnight := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	for i := n - 1; i >= 0; i-- {
		day := midnight.AddDate(0, 0, -i)
		name := day.Format(time.DateOnly)
		sum := sha256.Sum256([]byte(fmt.Sprintf("devseed|%d|%s", user, name)))
		f, _, err := st.AddRawFile(ctx, store.RawFile{SHA256: hex.EncodeToString(sum[:]), UserID: user, Source: "devseed",
			SourceName: "devseed-" + name})
		if err != nil {
			return nil, err
		}
		d, acts := dayData(day, now, int64(n-i))
		if i == 0 { // today's record also carries the plan for the coming week
			for j := 1; j <= 6; j++ {
				d.Suggestions = append(d.Suggestions, plan(day.AddDate(0, 0, j), int64(n+j))...)
			}
		}
		got, err := st.WriteFileData(ctx, f, d, acts, "devseed", 1)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		for _, x := range got {
			touched[x] = true
		}
	}
	if err := seedWatch(ctx, st, user, now); err != nil {
		return nil, err
	}
	if err := seedPlans(ctx, st, user, now, loc); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(touched))
	for d := range touched {
		out = append(out, d)
	}
	sort.Strings(out)
	return out, st.RebuildDays(ctx, user, out)
}

// The watch's zones in devseed: 50–90 % of a maximum of 183, as the Fenix 8 sets them.
var seedZones = []int{92, 110, 128, 146, 165, 183}

// seedWatch writes what the watch keeps besides health and activities: its settings with zones
// and a bike, the Run and Bike profiles, goals, lifetime totals, two workouts and a course.
func seedWatch(ctx context.Context, st *store.Store, user int64, now time.Time) error {
	zones := fitwatch.Zones{MaxHR: 183, ThresholdHR: 161, FTP: 255, HRCalc: "percent_max_hr", PowerCalc: "percent_ftp", HR: seedZones}
	files := map[string]*fitwatch.File{
		"settings": {Settings: &fitwatch.Settings{Clock: "24h", DateMode: "day_month", RestingHR: 52, HRSetting: "max", WakeS: 7 * 3600, SleepS: 23 * 3600,
			RunStepM: 1.12, WalkStepM: 0.74, ActivityLvl: 60, Units: fitwatch.Units{Distance: "metric", Speed: "metric", Elevation: "metric", Temperature: "metric", Weight: "metric", Height: "metric"},
			Zones: []fitwatch.Zones{zones}, Bikes: []fitwatch.Bike{{Name: "Road bike", Sport: "cycling", SubSport: "road", OdometerM: 4821_300, WeightKg: 8.4, WheelMM: 2105, CrankMM: 172.5, Enabled: true, Sensors: []string{"power", "speed_cadence"}}}}},
		"sport-run":  {Sport: &fitwatch.SportProfile{Sport: "running", SubSport: "generic", Name: "Run", Zones: zones}},
		"sport-bike": {Sport: &fitwatch.SportProfile{Sport: "cycling", SubSport: "road", Name: "Road Bike", Zones: fitwatch.Zones{FTP: 255, HRCalc: "percent_max_hr"}}},
		"goals": {Goals: []fitwatch.Goal{{Type: "steps", Target: 8000, Unit: "steps", Source: "auto"}, {Type: "active_minutes", Target: 150, Unit: "min", Source: "user"},
			{Type: "ascent", Target: 30, Unit: "m", Source: "user"}}},
		"totals": {Totals: []fitwatch.Totals{{Sport: "running", TimerS: 412_200, ElapsedS: -1, ActiveS: -1, DistanceM: 1_172_400, Calories: 79_800, Sessions: 107},
			{Sport: "cycling", TimerS: 96_500, ElapsedS: -1, ActiveS: -1, DistanceM: 712_900, Calories: 18_100, Sessions: 26}}},
		"workout-intervals": {Workout: &fitwatch.Workout{Name: "6 × 400 m", Sport: "running", SubSport: "generic", Steps: []fitwatch.Step{
			{Index: 0, Intensity: "warmup", Duration: "time", DurationValue: 600, Target: "heart_rate", TargetZone: 2},
			{Index: 1, Intensity: "active", Duration: "distance", DurationValue: 400, Target: "speed", TargetLow: 4.0, TargetHigh: 4.3},
			{Index: 2, Intensity: "recovery", Duration: "time", DurationValue: 90, Target: "heart_rate", TargetZone: 2},
			{Index: 3, Duration: "repeat", From: 1, Repeat: 6},
			{Index: 4, Intensity: "cooldown", Duration: "open", Notes: "Easy until you press Lap"},
		}}},
		"workout-strength": {Workout: &fitwatch.Workout{Name: "Core 20", Sport: "training", SubSport: "strength_training", Steps: []fitwatch.Step{
			{Index: 0, Intensity: "warmup", Duration: "time", DurationValue: 120, Exercise: &fitwatch.Exercise{Category: "cardio", Name: "Jumping Jacks", Number: 12}},
			{Index: 1, Intensity: "active", Duration: "reps", DurationValue: 15, Exercise: &fitwatch.Exercise{Category: "crunch", Name: "Bicycle Crunch"}},
			{Index: 2, Intensity: "active", Duration: "time", DurationValue: 45, Exercise: &fitwatch.Exercise{Category: "plank", Name: "Plank", Number: 43}},
			{Index: 3, Intensity: "active", Duration: "reps", DurationValue: 10, WeightKg: 12, Exercise: &fitwatch.Exercise{Category: "squat", Name: "Goblet Squat", Number: 37}},
			{Index: 4, Intensity: "rest", Duration: "time", DurationValue: 60, Notes: "Shake it out"},
			{Index: 5, Duration: "repeat", From: 1, Repeat: 3},
		}}},
	}
	// A course: the loop of the latest run, as a planned route with two points.
	loop := activity(now.Add(-26*time.Hour), now.Add(-26*time.Hour+35*time.Minute), 3)
	course := &fitwatch.Course{Name: "Lake loop", Sport: "running", DistanceM: loop.Distance, AscentM: float64(loop.Ascent), DescentM: float64(loop.Descent),
		Bounds: loop.Bounds, Polyline: loop.Polyline, Track: fitactivity.Track{Time: loop.Track.Time, Lat: loop.Track.Lat, Lon: loop.Track.Lon,
			Altitude: loop.Track.Altitude, Distance: loop.Track.Distance}}
	mid := len(loop.Track.Lat) / 2
	course.Points = []fitwatch.CoursePoint{{Type: "water", Name: "Fountain", DistanceM: loop.Track.Distance[mid], Lat: loop.Track.Lat[mid], Lon: loop.Track.Lon[mid]},
		{Type: "summit", Name: "Hill top", DistanceM: loop.Track.Distance[mid/2], Lat: loop.Track.Lat[mid/2], Lon: loop.Track.Lon[mid/2]}}
	files["course"] = &fitwatch.File{Course: course}
	files["workout-mobility"] = &fitwatch.File{Workout: &fitwatch.Workout{Name: "Morning Mobility", Sport: "mobility", SubSport: "generic", Steps: []fitwatch.Step{
		{Index: 0, Intensity: "active", Duration: "time", DurationValue: 45, Exercise: &fitwatch.Exercise{Category: "pose", Name: "Cat Cow", Number: 51}},
		{Index: 1, Intensity: "active", Duration: "time", DurationValue: 60, Exercise: &fitwatch.Exercise{Category: "pose", Name: "Child's Pose", Number: 11}},
	}}}
	// The made-up watch the workouts are on, as a sync leaves them: each workout file stored
	// under its item's uid and linked to it.
	dev, err := st.EnsureDevice(ctx, store.Device{UnitID: 3_999_000_001, Product: 4536, Name: "fenix 8 - 47mm", Address: "C0:FF:EE:00:00:01"})
	if err != nil {
		return err
	}
	if err := st.ClaimDevice(ctx, dev.ID, user); err != nil && !errors.Is(err, store.ErrOwned) {
		return err
	}
	items := map[string]string{}
	for name, w := range files {
		rf := store.RawFile{UserID: user, Source: "devseed", SourceName: "devseed-" + name, ReceivedAt: now}
		if w.Workout != nil {
			w.Workout.EstimatedS = fitwatch.Estimate(w.Workout.Steps)
			items[name] = fmt.Sprintf("%016x-%016x", uint64(now.Unix()-631065600)<<32|uint64(len(items)+1), uint64(1)<<56|uint64(len(name)))
			rf.DeviceID, rf.SourceName = dev.ID, items[name]+".fit"
			rf.FIT = &store.FITIdentity{Type: store.WorkoutFITType, Manufacturer: 1, Serial: dev.UnitID, TimeCreated: now.AddDate(0, 0, -7).Unix()}
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("devseed|%d|watch|%s", user, name)))
		rf.SHA256 = hex.EncodeToString(sum[:])
		f, _, err := st.AddRawFile(ctx, rf)
		if err != nil {
			return err
		}
		if err := st.WriteWatchFile(ctx, f, w); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if w.Workout != nil {
			if err := st.LinkWatchItem(ctx, dev.ID, items[name], "FIT_TYPE_5", f.ID); err != nil {
				return err
			}
		}
	}
	// Garmin Connect's list, as its export brings it: Core 20 again, and two more.
	sum := sha256.Sum256([]byte(fmt.Sprintf("devseed|%d|connect-workouts", user)))
	jf, _, err := st.AddRawFile(ctx, store.RawFile{SHA256: hex.EncodeToString(sum[:]), UserID: user, Source: store.SourceGCExport,
		SourceName: "devseed/DI_CONNECT/DI-Connect-Fitness/devseed_workout.json", ReceivedAt: now})
	if err != nil {
		return err
	}
	connect := func(name, sport, sub, desc string, days int, steps ...fitwatch.Step) gcexport.Workout {
		return gcexport.Workout{Workout: fitwatch.Workout{Name: name, Sport: sport, SubSport: sub, Steps: steps, EstimatedS: fitwatch.Estimate(steps),
			Description: desc}, Updated: now.AddDate(0, 0, days)}
	}
	exp := &gcexport.Result{Kind: "workouts", Workouts: []gcexport.Workout{
		connect("Core 20", "training", "strength_training", "Twenty minutes for the middle.", -30, files["workout-strength"].Workout.Steps...),
		connect("Alignment 101", "training", "yoga", "Slow poses to find your lines.", -60,
			fitwatch.Step{Index: 0, Intensity: "active", Duration: "time", DurationValue: 60, Exercise: &fitwatch.Exercise{Category: "pose", Name: "mountain", Number: 30}},
			fitwatch.Step{Index: 1, Intensity: "active", Duration: "time", DurationValue: 60, Exercise: &fitwatch.Exercise{Category: "pose", Name: "downward_dog", Number: 17}}),
		connect("10-Minute AMRAP", "hiit", "generic", "As many rounds as you can.", -90,
			fitwatch.Step{Index: 0, Intensity: "active", Duration: "reps", DurationValue: 10, Exercise: &fitwatch.Exercise{Category: "squat", Name: "air_squat", Number: 4}},
			fitwatch.Step{Index: 1, Intensity: "active", Duration: "reps", DurationValue: 10, Exercise: &fitwatch.Exercise{Category: "push_up", Name: "push_up", Number: 77}},
			fitwatch.Step{Index: 2, Duration: "repeat_until_time", From: 0, DurationValue: 600}),
	}}
	if _, err := st.WriteExportFile(ctx, jf, exp, "devseed", 1); err != nil {
		return err
	}
	// Two workouts built in goarmin: strength in sets and a circuit; a run with targets.
	ex := func(cat string, n int, title string) *fitwatch.Exercise {
		return &fitwatch.Exercise{Category: cat, Number: n, Name: title, Title: title}
	}
	for _, w := range []fitwatch.Workout{
		{Name: "Built: Core Sets", Sport: "training", SubSport: "strength_training", Description: "Made in the workout builder.", Steps: []fitwatch.Step{
			{Index: 0, Intensity: "warmup", Duration: "time", DurationValue: 300},
			{Index: 1, Intensity: "active", Duration: "reps", DurationValue: 12, Target: "open", Exercise: ex("plank", 43, "Plank")},
			{Index: 2, Intensity: "rest", Duration: "time", DurationValue: 60, Target: "open"},
			{Index: 3, Duration: "repeat", From: 1, Repeat: 3},
			{Index: 4, Intensity: "active", Duration: "time", DurationValue: 40, Exercise: ex("core", 51, "Cat Cow")},
			{Index: 5, Intensity: "active", Duration: "time", DurationValue: 40, Exercise: ex("pose", 11, "Child's Pose")},
			{Index: 6, Intensity: "rest", Duration: "time", DurationValue: 20, Target: "open"},
			{Index: 7, Duration: "repeat", From: 4, Repeat: 4},
		}},
		{Name: "Built: 6 x 400 m", Sport: "running", SubSport: "generic", Steps: []fitwatch.Step{
			{Index: 0, Intensity: "warmup", Duration: "time", DurationValue: 600, Target: "heart_rate", TargetZone: 2},
			{Index: 1, Intensity: "active", Duration: "distance", DurationValue: 400, Target: "speed", TargetLow: 4.2, TargetHigh: 4.6},
			{Index: 2, Intensity: "recovery", Duration: "time", DurationValue: 90},
			{Index: 3, Duration: "repeat", From: 1, Repeat: 6},
			{Index: 4, Intensity: "cooldown", Duration: "open"},
		}},
	} {
		if _, err := st.CreateWorkout(ctx, user, w); err != nil && !errors.Is(err, store.ErrWorkoutNameTaken) {
			return err // seeding again finds it made already
		}
	}
	// The watch took stock 20 minutes ago: Morning Mobility had been deleted there.
	return st.SetWatchInventory(ctx, dev.ID, store.WatchInventory{Types: []string{"FIT_TYPE_5"}, At: now.Add(-20 * time.Minute), Items: []store.InventoryItem{
		{UID: items["workout-intervals"], Type: "FIT_TYPE_5", Size: 400}, {UID: items["workout-strength"], Type: "FIT_TYPE_5", Size: 600}}})
}

// seedPlans puts library workouts on days around now: one a few days ago (missed, or done by a
// run that day), today's, and three ahead.
func seedPlans(ctx context.Context, st *store.Store, user int64, now time.Time, loc *time.Location) error {
	lib, err := st.WorkoutLibrary(ctx, user)
	if err != nil {
		return err
	}
	byName := map[string]store.LibraryWorkout{}
	for _, w := range lib.Workouts {
		byName[w.Name] = w
	}
	for _, p := range []struct {
		name string
		days int
		note string
	}{
		{"Morning Mobility", -5, ""}, {"6 × 400 m", -3, "on the track"}, {"Core 20", 0, ""},
		{"6 × 400 m", 2, ""}, {"Alignment 101", 4, "slowly"}, {"10-Minute AMRAP", 9, ""},
	} {
		w, ok := byName[p.name]
		if !ok {
			return fmt.Errorf("devseed: no workout %q to plan", p.name)
		}
		if _, err := st.AddPlan(ctx, user, store.WorkoutPlan{Day: now.In(loc).AddDate(0, 0, p.days).Format(time.DateOnly), WorkoutID: w.ID,
			Name: w.Name, Sport: w.Sport, Note: p.note}); err != nil {
			return err
		}
	}
	return nil
}

// nightOf is the night that ends on day k: to bed about 22:30 the evening before, up about
// 06:50. It depends on k alone, so the day before knows when its evening ends.
func nightOf(day time.Time, k int64) (bed, wake time.Time) {
	r := rand.New(rand.NewPCG(uint64(k), 99))
	j := func(minutes int) time.Duration { return time.Duration(r.IntN(2*minutes+1)-minutes) * time.Minute }
	return day.Add(-90*time.Minute + j(50)), day.Add(6*time.Hour + 50*time.Minute + j(40))
}

// dayData makes one day: the night that ended in its morning, then the day itself until the
// next night starts (or now).
func dayData(day, now time.Time, k int64) (*wellness.Data, []fitactivity.Activity) {
	r := rand.New(rand.NewPCG(uint64(k), 7))
	d := &wellness.Data{}
	add := func(kind wellness.Kind, t time.Time, sub uint8, v int64) {
		if !t.After(now) {
			d.Samples = append(d.Samples, wellness.Sample{Kind: kind, At: t, Sub: sub, Value: v})
		}
	}
	jitter := func(minutes int) time.Duration { return time.Duration(r.IntN(2*minutes+1)-minutes) * time.Minute }
	season := math.Sin(float64(k) / 45) // slow drifts over weeks
	calm := 0.6 + 0.8*r.Float64()       // some days are calmer than others
	resting := 52 + int64(math.Round(3*season)) + int64(r.IntN(3))
	baseline := 54 + 6*season

	// The night: from the evening before to this morning.
	bed, wake := nightOf(day, k)
	nextBed, _ := nightOf(day.AddDate(0, 0, 1), k+1)
	var deep, light, rem, awake int
	stage := func(t time.Time, s int64) { add(wellness.SleepStage, t, 0, s) }
	t := bed
	for cycle := 0; t.Before(wake); cycle++ {
		steps := []struct {
			s int64
			m int
		}{{wellness.SleepLight, 20 + r.IntN(10)}, {wellness.SleepDeep, max(8, 40-cycle*8+r.IntN(10))},
			{wellness.SleepLight, 15 + r.IntN(10)}, {wellness.SleepREM, 10 + cycle*5 + r.IntN(8)}}
		if r.IntN(3) == 0 {
			steps = append(steps, struct {
				s int64
				m int
			}{wellness.SleepAwake, 2 + r.IntN(5)})
		}
		for _, x := range steps {
			if !t.Before(wake) {
				break
			}
			stage(t, x.s)
			end := t.Add(time.Duration(x.m) * time.Minute)
			if end.After(wake) {
				end = wake
			}
			sec := int(end.Sub(t).Seconds())
			switch x.s {
			case wellness.SleepDeep:
				deep += sec
			case wellness.SleepLight:
				light += sec
			case wellness.SleepREM:
				rem += sec
			default:
				awake += sec
			}
			t = end
		}
	}
	stage(wake, wellness.SleepAwake)
	asleepH := float64(deep+light+rem) / 3600
	score := int(math.Max(40, math.Min(96, 30+(asleepH-5)*16+float64(r.IntN(12)))))
	bbStart := 15 + r.IntN(25)
	bbEnd := min(100, bbStart+45+r.IntN(30))
	d.Sleep = append(d.Sleep, wellness.Sleep{Start: bed, End: wake, Score: score, QualityScore: score - 5 + r.IntN(10),
		DurationScore: int(math.Min(100, asleepH/8*100)), RecoveryScore: score - 8 + r.IntN(16), MinHR: int(resting) - 4,
		BodyBatteryStart: bbStart, BodyBatteryEnd: bbEnd, Deep: deep, Light: light, REM: rem, Awake: awake})

	// Overnight values.
	var hrvSum float64
	var hrvN int
	for t := bed; t.Before(wake); t = t.Add(5 * time.Minute) {
		v := baseline + 8*math.Sin(float64(t.Unix())/2400) + float64(r.IntN(9)-4)
		add(wellness.HRV, t, 0, int64(v*128))
		hrvSum += v
		hrvN++
	}
	for t := bed; t.Before(wake); t = t.Add(time.Hour) {
		add(wellness.SpO2, t, 0, int64(93+r.IntN(5)))
	}

	// Heart rate, stress, Body Battery and respiration across the evening, the night and the day.
	workout := k%3 == 0
	wStart := day.Add(18*time.Hour + jitter(30))
	wEnd := wStart.Add(time.Duration(40+r.IntN(30)) * time.Minute)
	bb := float64(bbStart)
	for i, t := 0, bed; t.Before(nextBed); i, t = i+1, t.Add(2*time.Minute) {
		asleep := t.Before(wake)
		inWorkout := workout && !t.Before(wStart) && t.Before(wEnd)
		var hr float64
		switch {
		case asleep:
			hr = float64(resting) - 3 + float64(r.IntN(5))
		case inWorkout:
			hr = 135 + 20*math.Sin(float64(t.Sub(wStart).Minutes())/12) + float64(r.IntN(9))
		default:
			hr = 68 + 10*math.Sin(float64(t.Hour())/3.5) + float64(r.IntN(12))
		}
		add(wellness.HeartRate, t, 0, int64(hr))
		if i%3 == 0 { // every 6 minutes
			var stress int64
			switch {
			case asleep:
				stress = int64(8 + r.IntN(14))
			case inWorkout:
				stress = -2 // active: not measured
			default:
				stress = int64(calm * (22 + 25*math.Max(0, math.Sin(float64(t.Hour()-8)/3)) + float64(r.IntN(18))))
			}
			add(wellness.Stress, t, 0, stress)
			switch {
			case asleep:
				bb = math.Min(100, bb+float64(bbEnd-bbStart)/math.Max(1, wake.Sub(bed).Minutes()/6))
			case inWorkout:
				bb = math.Max(5, bb-1.2)
			default:
				bb = math.Max(5, bb-0.35)
			}
			add(wellness.BodyBattery, t, 0, int64(bb))
		}
		if asleep || i%5 == 0 { // every 2 minutes asleep, every 10 awake
			resp := 14.0 + float64(r.IntN(200))/100
			if !asleep {
				resp += 2.5
			}
			add(wellness.Respiration, t, 0, int64(resp*100))
		}
	}

	// Running totals since midnight, every 15 minutes while awake.
	var steps, dist, kcal, ascent int64
	for t := day.Add(7 * time.Hour); t.Before(nextBed) && t.Before(day.Add(24*time.Hour)); t = t.Add(15 * time.Minute) {
		s := int64(r.IntN(330))
		if workout && !t.Before(wStart) && t.Before(wEnd) {
			s += 2000
		}
		steps += s
		dist += s * 78 // centimetres
		kcal += s / 20
		ascent += int64(r.IntN(3)) * 1000 // millimetres
		add(wellness.Steps, t, 6, steps)
		add(wellness.Distance, t, 6, dist)
		add(wellness.ActiveCalories, t, 6, kcal)
		add(wellness.Ascent, t, 0, ascent)
	}

	// Once-a-day values and the HRV status, written in the morning.
	morning := wake.Add(5 * time.Minute)
	acute := int64(360 + 140*math.Sin(float64(k)/9) + float64(r.IntN(40)))
	chronic := int64(380 + 70*season)
	if !morning.After(now) {
		name := day.Format(time.DateOnly)
		last := hrvSum / math.Max(1, float64(hrvN))
		for kind, v := range map[wellness.Kind]int64{
			wellness.RestingHR: resting, wellness.SleepNeed: int64(450 + r.IntN(60)), wellness.StepGoal: 8000,
			wellness.HRVLastNight: int64(last), wellness.HRVTargetLow: 46, wellness.HRVTargetHigh: 62,
			wellness.VO2Max: int64((50 + 1.5*season) * 100), wellness.TrainingReadiness: int64(40 + r.IntN(55)),
			wellness.EnduranceScore: int64(5900 + 250*season + float64(r.IntN(40))), wellness.HillScore: int64(62 + 6*season),
			wellness.AcuteLoad: acute, wellness.ChronicLoad: chronic, wellness.TrainingStatus: trainingStatus(acute, chronic, season),
			wellness.Race5K: int64(1350 - 40*season), wellness.Race10K: int64(2830 - 85*season),
			wellness.RaceHalf: int64(6300 - 190*season), wellness.RaceMarathon: int64(13300 - 420*season),
		} {
			d.Daily = append(d.Daily, wellness.Daily{Kind: kind, At: morning.UTC(), Day: name, Value: v})
		}
		status := 4
		if last < 46 || last > 62 {
			status = 3
		}
		d.HRV = append(d.HRV, wellness.HRVNight{At: morning, WeeklyAverage: baseline, LastNight: last, LastNightFiveMinHigh: last + 12,
			BaselineLowUpper: 44, BalancedLower: 46, BalancedUpper: 62, Status: status})
	}
	d.Suggestions = plan(day, k)
	var acts []fitactivity.Activity
	if workout && !wEnd.After(now) {
		acts = append(acts, activity(wStart, wEnd, k))
	}
	return d, acts
}

// trainingStatus picks Garmin's status from the load ratio (Gadgetbridge's values: 1
// detraining, 4 maintaining, 5 recovery, 7 productive, 8 strained).
func trainingStatus(acute, chronic int64, season float64) int64 {
	switch r := float64(acute) / float64(chronic); {
	case r < 0.75:
		return 1
	case r < 0.9:
		return 5
	case r > 1.45:
		return 8
	case season > 0.2:
		return 7
	}
	return 4
}

// plan is Garmin Coach's day k: every third day a rest day, otherwise a base or threshold run
// listed twice (pace and heart-rate targets), and on some days a ride (power and heart rate).
func plan(day time.Time, k int64) []wellness.Suggestion {
	name := day.Format(time.DateOnly)
	if k%3 == 1 {
		return []wellness.Suggestion{{Day: name, Index: 0}}
	}
	kind, pace, hr, te, benefit := "Base", "6:10/km", "138bpm", 2.8, 2
	if k%3 == 0 && k%2 == 0 {
		kind, pace, hr, te, benefit = "Threshold", "5:05/km", "164bpm", 3.6, 4
	}
	dur := time.Duration(35+5*(k%4)) * time.Minute
	out := []wellness.Suggestion{
		{Day: name, Index: 0, Name: kind, Description: pace, Sport: int(typedef.SportRunning), Duration: dur, Benefit: benefit, AerobicTE: te},
		{Day: name, Index: 1, Name: kind, Description: hr, Sport: int(typedef.SportRunning), Duration: dur, Benefit: benefit, AerobicTE: te},
	}
	if k%5 == 0 {
		out = append(out,
			wellness.Suggestion{Day: name, Index: 2, Name: "Base", Description: "150W", Sport: int(typedef.SportCycling), Duration: 45 * time.Minute, Benefit: 2, AerobicTE: 2.5},
			wellness.Suggestion{Day: name, Index: 3, Name: "Base", Description: "128bpm", Sport: int(typedef.SportCycling), Duration: 45 * time.Minute, Benefit: 2, AerobicTE: 2.5})
	}
	return out
}

// activity makes the evening workout: a run (or every fifth one a ride, with power) on a loop
// with a hill, recorded every second, with a lap per kilometre (per 5 km riding).
func activity(start, end time.Time, k int64) fitactivity.Activity {
	r := rand.New(rand.NewPCG(uint64(k), 11))
	ride := k%5 == 0
	sport, sub, label, base, lapEvery := typedef.SportRunning, typedef.SubSportGeneric, "Run", 2.85, 1000.0
	if ride {
		sport, sub, label, base, lapEvery = typedef.SportCycling, typedef.SubSportRoad, "Road Bike", 7.4, 5000.0
	}
	n := int(end.Sub(start) / time.Second)
	t := fitactivity.Track{}
	// The loop: an ellipse whose length is about the planned distance, around a made-up spot.
	loop := base * float64(n)
	lat0, lon0 := 45.0+float64(k%7)*0.01, 7.0+float64(k%5)*0.01
	radius := loop / (2 * math.Pi) // metres
	var dist, ascent, descent float64
	var hrSum, hrMax, cadSum, powSum float64
	lastAlt := 0.0
	type acc struct {
		start  int
		d, asc float64
	}
	laps := []acc{{start: 0}}
	var lapsOut []fitactivity.Lap
	closeLap := func(i int) {
		l := laps[len(laps)-1]
		hs, hm, cs, ps, nn := 0.0, 0.0, 0.0, 0.0, float64(i-l.start+1)
		for j := l.start; j <= i; j++ {
			hs += t.HeartRate[j]
			hm = math.Max(hm, t.HeartRate[j])
			cs += t.Cadence[j]
			if ride {
				ps += t.Power[j]
			}
		}
		el := float64(t.Time[i] - t.Time[l.start])
		lap := fitactivity.Lap{Start: time.Unix(t.Time[l.start], 0), Elapsed: el, Timer: el, Distance: t.Distance[i] - l.d,
			AvgHR: int(hs / nn), MaxHR: int(hm), AvgSpeed: (t.Distance[i] - l.d) / math.Max(1, el), AvgCadence: int(cs / nn), Ascent: int(ascent - l.asc)}
		if ride {
			lap.AvgPower = int(ps / nn)
		}
		lapsOut = append(lapsOut, lap)
		laps = append(laps, acc{start: i + 1, d: t.Distance[i], asc: ascent})
	}
	for i := 0; i < n; i++ {
		frac := dist / loop
		ang := 2 * math.Pi * frac
		alt := 420 + 35*math.Sin(ang) + 8*math.Sin(3*ang)
		grade := 0.0
		if i > 0 {
			grade = alt - lastAlt
		}
		speed := base*(1-1.2*grade/math.Max(0.5, base)) + 0.15*math.Sin(float64(i)/40) + (r.Float64()-0.5)*0.1
		if i < 60 { // warming up
			speed *= 0.7 + 0.3*float64(i)/60
		}
		speed = math.Max(0.6, speed)
		hr := 128 + 22*(1-math.Exp(-float64(i)/400)) + 6*math.Sin(ang) + float64(r.IntN(3))
		if i > n-240 {
			hr += 10 * float64(i-(n-240)) / 240 // the finish
		}
		t.Time = append(t.Time, start.Add(time.Duration(i)*time.Second).Unix())
		t.Lat = append(t.Lat, lat0+(radius*math.Sin(ang))/111320)
		t.Lon = append(t.Lon, lon0+(radius*0.6*(1-math.Cos(ang)))/(111320*math.Cos(lat0*math.Pi/180)))
		t.Altitude = append(t.Altitude, alt)
		t.Distance = append(t.Distance, dist)
		t.Speed = append(t.Speed, speed)
		t.HeartRate = append(t.HeartRate, math.Round(hr))
		cad := 84 + 3*math.Sin(float64(i)/90) + float64(r.IntN(2)) // strides (one leg) per minute, as the watch records running
		if ride {
			cad = 86 + 6*math.Sin(float64(i)/70) + float64(r.IntN(3))
			p := 165 + 160*math.Max(-0.5, grade) + 15*math.Sin(float64(i)/30) + float64(r.IntN(10))
			t.Power = append(t.Power, math.Max(0, math.Round(p)))
			powSum += t.Power[i]
		} else {
			t.Power = append(t.Power, math.NaN())
		}
		t.Cadence = append(t.Cadence, math.Round(cad))
		t.Temperature = append(t.Temperature, 19-3*float64(i)/float64(n))
		if i > 0 {
			if alt > lastAlt {
				ascent += alt - lastAlt
			} else {
				descent += lastAlt - alt
			}
		}
		lastAlt = alt
		hrSum += t.HeartRate[i]
		hrMax = math.Max(hrMax, t.HeartRate[i])
		cadSum += t.Cadence[i]
		if dist-laps[len(laps)-1].d >= lapEvery {
			closeLap(i)
		}
		dist += speed
	}
	if laps[len(laps)-1].start < n-1 {
		closeLap(n - 1)
	}
	pts := make([]geo.Point, n)
	for i := range pts {
		pts[i] = geo.Point{Lat: t.Lat[i], Lon: t.Lon[i]}
	}
	b, _ := geo.BoundsOf(pts)
	fn := float64(n)
	a := fitactivity.Activity{
		Sport: sport, SubSport: sub, Name: label, Start: start, End: end, Elapsed: fn, Timer: fn, Distance: dist,
		Calories: int(fn / 60 * 11), AvgHR: int(hrSum / fn), MaxHR: int(hrMax), AvgSpeed: dist / fn, AvgCadence: int(cadSum / fn),
		Ascent: int(ascent), Descent: int(descent), TrainingEffect: 2.6 + float64(k%10)/10, AnaerobicEffect: 0.8 + float64(k%4)/5,
		TrainingLoad: 60 + float64(k%40), Bounds: &b, Polyline: geo.EncodePolyline(geo.SimplifyTo(pts, 500)), Laps: lapsOut, Track: t,
	}
	for _, s := range t.Speed {
		a.MaxSpeed = math.Max(a.MaxSpeed, s)
	}
	if ride {
		a.AvgPower = int(powSum / fn)
	}
	// The watch's own time in zones: below zone 1, zones 1–5, above the top.
	secs := make([]float64, 7)
	for _, hr := range t.HeartRate {
		z := 0
		for z < len(seedZones) && int(hr) >= seedZones[z] {
			z++
		}
		secs[z]++
	}
	a.Zones = &fitactivity.Zones{HR: seedZones, HRSeconds: secs, MaxHR: 183, ThresholdHR: 161, HRCalc: "percent_max_hr"}
	return a
}
