// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mcpserver

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Argument shorthands.

func id(what string) Arg {
	return Arg{Name: "id", In: InPath, Type: "integer", Required: true, Desc: what}
}

func days(maxDays string) []Arg {
	return []Arg{
		{Name: "from", In: InQuery, Type: "string", Desc: "first local day, YYYY-MM-DD (default: 29 days before to)"},
		{Name: "to", In: InQuery, Type: "string", Desc: "last local day, YYYY-MM-DD (default: today); at most " + maxDays + " days in all"},
	}
}

func deviceID() Arg { return id("the device's id (list_devices)") }

// workoutArgs are a workout's fields as create_workout and update_workout take them.
func workoutArgs() []Arg {
	return []Arg{
		{Name: "name", In: InBody, Type: "string", Required: true, Desc: "the name, at most 40 characters"},
		{Name: "sport", In: InBody, Type: "string", Required: true, Desc: "the FIT sport: training, fitness_equipment, hiit, running, cycling, walking, mobility, …"},
		{Name: "sub_sport", In: InBody, Type: "string", Desc: "the FIT sub sport: strength_training, cardio_training, yoga, pilates, amrap, emom, tabata, …"},
		{Name: "description", In: InBody, Type: "string", Desc: "the description, at most 1,000 characters"},
		{Name: "steps", In: InBody, Type: "array", Items: "object", Required: true, Desc: "the steps, as get_workout returns them"},
	}
}

func confirm() Arg {
	return Arg{Name: "confirm", In: InTool, Type: "string", Required: true,
		Desc: "the device's name (nickname, model or name) exactly as the user typed it, to confirm; never guess it"}
}

func pairingID() Arg { return id("the pairing session's id (start_pairing or get_pairing)") }

// Tools is every MCP tool, one per API route (the guard test in the api package keeps it
// complete). Descriptions are short: an assistant reads all of them.
var Tools = []Tool{
	// Account.
	{Name: "get_account", Title: "Account", Area: "Account", Level: Read, Method: "GET", Path: "/api/me", ReadOnly: true,
		Desc: "The signed-in account: name, e-mail, time zone, units, admin or not, and what this server does (sync, tiles, imports)."},
	{Name: "update_profile", Title: "Update profile", Area: "Account", Level: Write, Method: "PATCH", Path: "/api/me", Idempotent: true,
		Desc: "Change the account's display name, time zone (IANA, e.g. Europe/Berlin) or units.",
		Args: []Arg{
			{Name: "display_name", In: InBody, Type: "string", Desc: "the name shown in goarmin"},
			{Name: "timezone", In: InBody, Type: "string", Desc: "IANA time zone; empty: the watch's own"},
			{Name: "units", In: InBody, Type: "string", Enum: []string{"metric", "imperial"}, Desc: "metric or imperial"},
		}},
	{Name: "get_settings", Title: "All settings", Area: "Account", Level: Read, Method: "GET", Path: "/api/me/settings", ReadOnly: true,
		Desc: "Every stored setting of the account: prefs (week start, clock, intensity goal, zones, calendar kinds hidden) and glance.layout (the At a glance cards)."},
	{Name: "get_setting", Title: "One setting", Area: "Account", Level: Read, Method: "GET", Path: "/api/me/settings/{key}", ReadOnly: true,
		Desc: "One setting with its revision (rev, for put_setting).",
		Args: []Arg{{Name: "key", In: InPath, Type: "string", Required: true, Enum: []string{"prefs", "glance.layout"}, Desc: "the setting"}}},
	{Name: "put_setting", Title: "Save a setting", Area: "Account", Level: Write, Method: "PUT", Path: "/api/me/settings/{key}", Idempotent: true,
		Desc: "Replace a setting's whole value. Read it first (get_setting) and pass its rev, so a change made meanwhile is not overwritten (HTTP 412 then).",
		Args: []Arg{
			{Name: "key", In: InPath, Type: "string", Required: true, Enum: []string{"prefs", "glance.layout"}, Desc: "the setting"},
			{Name: "value", In: InTool, Type: "object", Required: true, Desc: "the whole new value, in the shape get_setting returns it"},
			{Name: "rev", In: InTool, Type: "integer", Desc: "the revision read last; omit to overwrite whatever is stored"},
		},
		Body: func(args map[string]any) ([]byte, http.Header, error) {
			b, err := json.Marshal(args["value"])
			if err != nil {
				return nil, nil, errors.New("value could not be encoded")
			}
			h := http.Header{"Content-Type": {"application/json"}}
			if rev, ok := args["rev"]; ok {
				h.Set("If-Match", `"`+argString(rev)+`"`)
			}
			return b, h, nil
		}},
	{Name: "reset_setting", Title: "Reset a setting", Area: "Account", Level: Write, Method: "DELETE", Path: "/api/me/settings/{key}", Destructive: true, Idempotent: true,
		Desc: "Remove a stored setting, so its default applies again (the default glance, default preferences).",
		Args: []Arg{{Name: "key", In: InPath, Type: "string", Required: true, Enum: []string{"prefs", "glance.layout"}, Desc: "the setting"}}},
	{Name: "get_watch_profile", Title: "Watch profile", Area: "Account", Level: Read, Method: "GET", Path: "/api/me/watch-profile", ReadOnly: true,
		Desc: "What the current watch's settings file says: clock, units, resting heart rate, sleep window, step lengths, height and weight, goals, bikes."},
	{Name: "get_zones", Title: "Heart-rate zones", Area: "Account", Level: Read, Method: "GET", Path: "/api/me/zones", ReadOnly: true,
		Desc: "The watch's sport profiles with their heart-rate zones, and the zones in effect per sport."},

	// Health.
	{Name: "list_metrics", Title: "Metrics", Area: "Health", Level: Read, Method: "GET", Path: "/api/metrics", ReadOnly: true,
		Desc: "Every metric goarmin stores, with its unit and scale: the names get_series takes (daily ones are in get_daily)."},
	{Name: "get_daily", Title: "Daily summaries", Area: "Health", Level: Read, Method: "GET", Path: "/api/daily", ReadOnly: true,
		Desc: "One summary per local day: steps, distance, calories, heart rate, stress, Body Battery, sleep, intensity minutes, HRV and every daily value (training readiness, load, VO2 max, …).",
		Args: days("3660")},
	{Name: "get_series", Title: "Time series", Area: "Health", Level: Read, Method: "GET", Path: "/api/series/{metric}", ReadOnly: true,
		Desc: "A metric over time (heart rate, stress, Body Battery, respiration, SpO2, steps, …), bucketed to at most max_points; t is Unix seconds, v the values, min/max when bucketed.",
		Args: []Arg{
			{Name: "metric", In: InPath, Type: "string", Required: true, Desc: "a metric name from list_metrics, e.g. heart_rate"},
			{Name: "from_day", In: InQuery, Type: "string", Desc: "first local day, YYYY-MM-DD (or use from/to)"},
			{Name: "to_day", In: InQuery, Type: "string", Desc: "last local day (default: from_day)"},
			{Name: "from", In: InQuery, Type: "integer", Desc: "start, Unix seconds (default: 24 hours before to)"},
			{Name: "to", In: InQuery, Type: "integer", Desc: "end, Unix seconds (default: now)"},
			{Name: "max_points", In: InQuery, Type: "integer", Desc: "10..20000, default 1500; fewer points keep the answer small"},
		}},
	{Name: "list_nights", Title: "Nights", Area: "Health", Level: Read, Method: "GET", Path: "/api/sleep", ReadOnly: true,
		Desc: "Nights of sleep with their scores (overall, quality, duration, recovery), stage totals, sleep need and Body Battery.",
		Args: days("3660")},
	{Name: "get_night", Title: "A night in detail", Area: "Health", Level: Read, Method: "GET", Path: "/api/sleep/{start}/stages", ReadOnly: true,
		Desc: "One night's stages over time with heart rate, HRV, respiration, stress and Body Battery during it.",
		Args: []Arg{{Name: "start", In: InPath, Type: "integer", Required: true, Desc: "the night's start, Unix seconds, as list_nights gives it"}}},
	{Name: "list_naps", Title: "Naps", Area: "Health", Level: Read, Method: "GET", Path: "/api/naps", ReadOnly: true,
		Desc: "Naps the watch recorded.", Args: days("3660")},
	{Name: "get_body", Title: "Body", Area: "Health", Level: Read, Method: "GET", Path: "/api/body", ReadOnly: true,
		Desc: "Weigh-ins with body composition, and nightly skin temperature.", Args: days("3660")},
	{Name: "list_ecgs", Title: "ECG recordings", Area: "Health", Level: Read, Method: "GET", Path: "/api/ecg", ReadOnly: true,
		Desc: "ECG recordings with their classification, average heart rate and RMSSD."},
	{Name: "get_ecg", Title: "An ECG", Area: "Health", Level: Read, Method: "GET", Path: "/api/ecg/{id}", ReadOnly: true,
		Desc: "One ECG recording with its waveform samples.", Args: []Arg{id("the recording's id (list_ecgs)")}},
	{Name: "get_physiology", Title: "Physiology", Area: "Health", Level: Read, Method: "GET", Path: "/api/physiology", ReadOnly: true,
		Desc: "The watch's physiological snapshots: VO2 max, lactate threshold, maximum heart rate, recovery and the effects of each activity.",
		Args: days("3660")},
	{Name: "get_health_status", Title: "Health status", Area: "Health", Level: Read, Method: "GET", Path: "/api/health-status", ReadOnly: true,
		Desc: "Garmin Connect's health status per night (heart rate, HRV, respiration, skin temperature, SpO2 against their baselines), from the export.",
		Args: days("3660")},
	{Name: "get_hydration", Title: "Hydration", Area: "Health", Level: Read, Method: "GET", Path: "/api/hydration", ReadOnly: true,
		Desc: "Logged drinks and the daily goal.", Args: days("3660")},
	{Name: "get_snapshots", Title: "Health snapshots", Area: "Health", Level: Read, Method: "GET", Path: "/api/snapshots", ReadOnly: true,
		Desc: "Health Snapshot sessions (a two-minute reading of heart rate, HRV, respiration, stress and SpO2).", Args: days("3660")},

	// Training.
	{Name: "list_activities", Title: "Activities", Area: "Training", Level: Read, Method: "GET", Path: "/api/activities", ReadOnly: true,
		Desc: "Recorded activities, newest first: sport, start, duration, distance, heart rate, training effect. Either a period (from_day/to_day) or pages (before/limit).",
		Args: []Arg{
			{Name: "from_day", In: InQuery, Type: "string", Desc: "first local day, YYYY-MM-DD: every activity of the period"},
			{Name: "to_day", In: InQuery, Type: "string", Desc: "last local day (default: from_day)"},
			{Name: "before", In: InQuery, Type: "integer", Desc: "for the next page: the start (Unix seconds) of the last activity seen"},
			{Name: "limit", In: InQuery, Type: "integer", Desc: "1..200, default 50"},
		}},
	{Name: "get_activity", Title: "An activity", Area: "Training", Level: Read, Method: "GET", Path: "/api/activities/{id}", ReadOnly: true,
		Desc: "One activity in full: laps, splits, time in heart-rate zones, training effect, the workout it followed.",
		Args: []Arg{id("the activity's id (list_activities)")}},
	{Name: "get_activity_track", Title: "An activity's track", Area: "Training", Level: Read, Method: "GET", Path: "/api/activities/{id}/streams", ReadOnly: true,
		Desc: "The activity's recorded samples by column (t, heart rate, speed, altitude, cadence, power, position, …), thinned to max_points.",
		Args: []Arg{id("the activity's id"), {Name: "max_points", In: InTool, Type: "integer", Desc: "samples to keep, 10..5000, default 600"}},
		Post: thinColumns},
	{Name: "get_activity_neighbours", Title: "Previous and next activity", Area: "Training", Level: Read, Method: "GET", Path: "/api/activities/{id}/neighbours", ReadOnly: true,
		Desc: "The activities before and after this one.", Args: []Arg{id("the activity's id")}},
	{Name: "get_totals", Title: "Lifetime totals", Area: "Training", Level: Read, Method: "GET", Path: "/api/totals", ReadOnly: true,
		Desc: "Lifetime totals per sport from the watch: time, distance, calories, sessions."},
	{Name: "get_coach", Title: "Garmin Coach", Area: "Training", Level: Read, Method: "GET", Path: "/api/coach", ReadOnly: true,
		Desc: "Garmin Coach's suggested workouts per day, with targets, and whether each was done.", Args: days("60")},
	{Name: "list_records", Title: "Personal records", Area: "Training", Level: Read, Method: "GET", Path: "/api/records", ReadOnly: true,
		Desc: "Personal records (fastest 5 km, longest ride, …) with their activities."},
	{Name: "list_gear", Title: "Gear", Area: "Training", Level: Read, Method: "GET", Path: "/api/gear", ReadOnly: true,
		Desc: "Shoes and bikes with the distance used against their limits."},
	{Name: "list_golf_clubs", Title: "Golf clubs", Area: "Training", Level: Read, Method: "GET", Path: "/api/golf-clubs", ReadOnly: true,
		Desc: "Golf clubs the watch knows."},
	{Name: "list_workouts", Title: "Workout library", Area: "Training", Level: Read, Method: "GET", Path: "/api/workouts", ReadOnly: true,
		Desc: "The workout library: every workout from the watches and from Garmin Connect, once each, with whether each watch has it on it."},
	{Name: "get_workout", Title: "A workout", Area: "Training", Level: Read, Method: "GET", Path: "/api/workouts/{id}", ReadOnly: true,
		Desc: "One workout's steps (durations, targets, repeats, exercises) and its versions.", Args: []Arg{id("the workout's id (list_workouts)")}},
	{Name: "create_workout", Title: "Build a workout", Area: "Training", Level: Write, Method: "POST", Path: "/api/workouts",
		Desc: "Make a workout of goarmin's own, written as Garmin Connect writes workout files. Steps as get_workout returns them: each with index, intensity (active, rest, warmup, cooldown, recovery, interval), duration (time s, distance m, reps, calories, open = lap button, hr_less_than/hr_greater_than, power_less_than/power_greater_than) and duration_value; optional target (heart_rate, speed m/s, power, cadence, open) with target_zone or target_low/target_high, exercise {category, number} from list_exercises, weight_kg, notes. A repeat step {duration: repeat, from: index of its first step, repeat: 2-99} or {duration: repeat_until_time, from, duration_value: seconds}; one level only. Sets of an exercise are [exercise, rest] in a repeat. At most 100 steps.",
		Args: workoutArgs()},
	{Name: "update_workout", Title: "Edit a workout", Area: "Training", Level: Write, Method: "PUT", Path: "/api/workouts/{id}", Idempotent: true,
		Desc: "Replace a library workout's name, sport, description and steps (the same fields as create_workout). goarmin keeps its own version; the original from the watch or Garmin Connect stays. A watch that has the workout gets the new version at its next sync; the old one stays on the watch until deleted there.",
		Args: append([]Arg{id("the workout's id (list_workouts)")}, workoutArgs()...)},
	{Name: "duplicate_workout", Title: "Duplicate a workout", Area: "Training", Level: Write, Method: "POST", Path: "/api/workouts/{id}/duplicate",
		Desc: "Make a new workout from a library workout, named \"‹name› 2\" (or the next free number) unless a name is given.",
		Args: []Arg{id("the workout's id (list_workouts)"), {Name: "name", In: InBody, Type: "string", Desc: "the copy's name, at most 40 characters"}}},
	{Name: "hide_workout", Title: "Hide a workout", Area: "Training", Level: Write, Method: "POST", Path: "/api/workouts/{id}/hide", Idempotent: true,
		Desc: "Hide a workout from the library (list_workouts leaves it out). It shows again while a watch has it.",
		Args: []Arg{id("the workout's id (list_workouts)")}},
	{Name: "show_workout", Title: "Show a hidden workout", Area: "Training", Level: Write, Method: "DELETE", Path: "/api/workouts/{id}/hide", Idempotent: true,
		Desc: "Bring a hidden workout back into the library.",
		Args: []Arg{id("the workout's id")}},
	{Name: "rename_workout", Title: "Rename a workout", Area: "Training", Level: Write, Method: "PATCH", Path: "/api/workouts/{id}", Idempotent: true,
		Desc: "Rename a library workout or change its description. goarmin keeps its own version (origin goarmin) with the steps unchanged; the library shows it and sends it to a watch in place of the original.",
		Args: []Arg{
			id("the workout's id (list_workouts)"),
			{Name: "name", In: InBody, Type: "string", Desc: "the new name, at most 40 characters"},
			{Name: "description", In: InBody, Type: "string", Desc: "the new description, at most 1,000 characters"},
		}},
	{Name: "set_workout_on_watch", Title: "Put a workout on a watch, or take it off", Area: "Training", Level: Write, Method: "PUT", Path: "/api/workouts/{id}/watches/{device}", Idempotent: true,
		Desc: "Switch a library workout on or off for one of the account's watches (watches in list_workouts). On: the watch gets it at its next sync, which starts now, if it lacks it. Off: goarmin sends it no more, but a watch takes no deletes; the user deletes it on the watch (Workouts › the workout › Delete).",
		Args: []Arg{
			id("the workout's id (list_workouts)"),
			{Name: "device", In: InPath, Type: "integer", Required: true, Desc: "the watch's device id (list_workouts' watches)"},
			{Name: "on", In: InBody, Type: "boolean", Required: true, Desc: "true: on the watch; false: not"},
		}},
	{Name: "discard_workout_edit", Title: "Undo a workout's rename or edit", Area: "Training", Level: Write, Method: "DELETE", Path: "/api/workouts/{id}", Destructive: true, Idempotent: true,
		Desc: "Remove goarmin's own version of a workout (its id is the version of origin goarmin in get_workout's versions); the library shows the original again. A watch's or Garmin Connect's version cannot be removed.",
		Args: []Arg{id("the id of the workout's goarmin version")}},
	{Name: "list_exercises", Title: "Exercises", Area: "Training", Level: Read, Method: "GET", Path: "/api/exercises", ReadOnly: true,
		Desc: "Garmin's exercise catalogue (1,846 exercises in 51 categories) by category and number, the way workout steps name them. Filter it: the whole list is large.",
		Args: []Arg{
			{Name: "category", In: InTool, Type: "string", Desc: "only this category, e.g. plank"},
			{Name: "search", In: InTool, Type: "string", Desc: "only exercises whose name contains this"},
		},
		Post: filterExercises},
	{Name: "get_exercise", Title: "An exercise", Area: "Training", Level: Read, Method: "GET", Path: "/api/exercises/{category}/{number}", ReadOnly: true,
		Desc: "One exercise of the catalogue: its name, category and how its 3D figure is posed.",
		Args: []Arg{
			{Name: "category", In: InPath, Type: "string", Required: true, Desc: "the category, e.g. plank"},
			{Name: "number", In: InPath, Type: "integer", Required: true, Desc: "the exercise's number in it"},
		},
		Post: exerciseOnly},
	{Name: "list_courses", Title: "Courses", Area: "Training", Level: Read, Method: "GET", Path: "/api/courses", ReadOnly: true,
		Desc: "Courses (routes) on the watches, with distance and ascent."},
	{Name: "get_course", Title: "A course", Area: "Training", Level: Read, Method: "GET", Path: "/api/courses/{id}", ReadOnly: true,
		Desc: "One course with its points (climbs, water, turns) and laps.", Args: []Arg{id("the course's id (list_courses)")}},
	{Name: "get_course_track", Title: "A course's track", Area: "Training", Level: Read, Method: "GET", Path: "/api/courses/{id}/streams", ReadOnly: true,
		Desc: "The course's track by column (distance, altitude, position), thinned to max_points.",
		Args: []Arg{id("the course's id"), {Name: "max_points", In: InTool, Type: "integer", Desc: "points to keep, 10..5000, default 600"}},
		Post: thinColumns},

	// Calendar.
	{Name: "get_calendar", Title: "Calendar", Area: "Calendar", Level: Read, Method: "GET", Path: "/api/calendar", ReadOnly: true,
		Desc: "Days with anything on them: the day's summary, which kinds of data it holds, its activities, Garmin Coach's workouts and your plans (done, missed or ahead).",
		Args: days("93")},
	{Name: "plan_workout", Title: "Plan a workout", Area: "Calendar", Level: Write, Method: "POST", Path: "/api/plans",
		Desc: "Put a library workout on a day. It is stored in goarmin and matched against what is recorded; it is not sent to the watch.",
		Args: []Arg{
			{Name: "day", In: InBody, Type: "string", Required: true, Desc: "the local day, YYYY-MM-DD"},
			{Name: "workout_id", In: InBody, Type: "integer", Required: true, Desc: "the workout's id (list_workouts)"},
			{Name: "note", In: InBody, Type: "string", Desc: "a note, at most 500 characters"},
		}},
	{Name: "update_plan", Title: "Move or note a plan", Area: "Calendar", Level: Write, Method: "PATCH", Path: "/api/plans/{id}", Idempotent: true,
		Desc: "Move a plan to another day, or change its note.",
		Args: []Arg{
			id("the plan's id (get_calendar)"),
			{Name: "day", In: InBody, Type: "string", Desc: "the new local day, YYYY-MM-DD"},
			{Name: "note", In: InBody, Type: "string", Desc: "the new note"},
		}},
	{Name: "remove_plan", Title: "Remove a plan", Area: "Calendar", Level: Write, Method: "DELETE", Path: "/api/plans/{id}", Destructive: true, Idempotent: true,
		Desc: "Take a plan off the calendar (the workout stays in the library).", Args: []Arg{id("the plan's id")}},

	// Devices.
	{Name: "list_devices", Title: "Devices", Area: "Devices", Level: Read, Method: "GET", Path: "/api/devices", ReadOnly: true,
		Desc: "The account's devices (watches, scales, …), current and earlier: model, nickname, battery, firmware, sync status, after-fetch setting."},
	{Name: "update_device", Title: "Change a device's settings", Area: "Devices", Level: Write, Method: "PATCH", Path: "/api/devices/{id}", Idempotent: true,
		Desc: "Change a watch's nickname, what happens after fetching (keep, archive, archive_delete), pause scheduled syncs, or import when plugged in by USB. archive_delete makes the watch delete activities and needs confirm.",
		Args: []Arg{
			deviceID(),
			{Name: "nickname", In: InBody, Type: "string", Desc: "the name shown, at most 60 characters"},
			{Name: "after_fetch", In: InBody, Type: "string", Enum: []string{"keep", "archive", "archive_delete"},
				Desc: "keep: leave files on the watch; archive: mark them synced (the watch deletes health files); archive_delete: also delete activities from the watch"},
			{Name: "paused", In: InBody, Type: "boolean", Desc: "pause scheduled syncs"},
			{Name: "usb_auto", In: InBody, Type: "boolean", Desc: "import the watch when it is plugged in by USB"},
			{Name: "confirm", In: InTool, Type: "string", Desc: "for after_fetch archive_delete: the watch's name as the user typed it"},
		},
		Confirm: confirmDevice, ConfirmWhen: func(a map[string]any) bool { return a["after_fetch"] == "archive_delete" }},
	{Name: "sync_device", Title: "Sync now", Area: "Devices", Level: Write, Method: "POST", Path: "/api/devices/{id}/sync",
		Desc: "Sync a watch now (through the computer it is paired with). Follow it with list_devices.",
		Args: []Arg{deviceID(), {Name: "include_synced", In: InBody, Type: "boolean", Desc: "also fetch activities the watch already marked as synced"}}},
	{Name: "get_device_sync_runs", Title: "A device's recent syncs", Area: "Devices", Level: Read, Method: "GET", Path: "/api/devices/{id}/sync-runs", ReadOnly: true,
		Desc: "The device's last syncs.", Args: []Arg{deviceID()}},
	{Name: "get_sync_log", Title: "Sync log", Area: "Devices", Level: Read, Method: "GET", Path: "/api/sync-runs", ReadOnly: true,
		Desc: "The sync log across the account's watches, newest first, with results and error hints.",
		Args: []Arg{
			{Name: "device", In: InQuery, Type: "integer", Desc: "only this device"},
			{Name: "problems", In: InQuery, Type: "boolean", Desc: "only failed syncs"},
			{Name: "before", In: InQuery, Type: "integer", Desc: "for the next page: the last run's id"},
			{Name: "limit", In: InQuery, Type: "integer", Desc: "1..200, default 50"},
		}},
	{Name: "get_device_summary", Title: "What came from a device", Area: "Devices", Level: Read, Method: "GET", Path: "/api/devices/{id}/summary", ReadOnly: true,
		Desc: "What came from a device: files, activities, nights and days, first and last day.", Args: []Arg{deviceID()}},
	{Name: "list_watch_files", Title: "A watch's files", Area: "Devices", Level: Read, Method: "GET", Path: "/api/devices/{id}/files", ReadOnly: true,
		Desc: "What goarmin fetched from a watch, with each file's state on the watch and whether it can be deleted from it.", Args: []Arg{deviceID()}},
	{Name: "get_device_solar", Title: "Solar charge", Area: "Devices", Level: Read, Method: "GET", Path: "/api/devices/{id}/solar", ReadOnly: true,
		Desc: "A solar watch's hourly charge from the sun.",
		Args: []Arg{deviceID(), {Name: "from", In: InQuery, Type: "string", Desc: "first local day, YYYY-MM-DD"}, {Name: "to", In: InQuery, Type: "string", Desc: "last local day"}}},
	{Name: "get_device_changes", Title: "Settings history", Area: "Devices", Level: Read, Method: "GET", Path: "/api/devices/{id}/changes", ReadOnly: true,
		Desc: "Changes made to the watch's settings on the watch, newest first.",
		Args: []Arg{deviceID(), {Name: "before", In: InQuery, Type: "integer", Desc: "for the next page: the last change's time, Unix seconds"}}},
	{Name: "get_device_documents", Title: "What else the watch keeps", Area: "Devices", Level: Read, Method: "GET", Path: "/api/devices/{id}/documents", ReadOnly: true,
		Desc: "The watch's other records (device, settings and multisport files) as documents.", Args: []Arg{deviceID()}},
	{Name: "get_live_readings", Title: "Live readings", Area: "Devices", Level: Read, Method: "GET", Path: "/api/devices/{id}/live", ReadOnly: true,
		Desc: "Open the watch's live link and read its values (heart rate, stress, Body Battery, respiration, steps, calories, floors) for a few seconds. Opening takes about 10 s; it holds the watch's Bluetooth meanwhile. Nothing to stop afterwards: the link closes by itself 30 s after the read (a call within them reuses it).",
		Args: []Arg{deviceID(), {Name: "seconds", In: InTool, Type: "integer", Desc: "how long to read once the link is open, 1..30, default 10"}},
		Run:  liveReadings},
	{Name: "get_bluetooth", Title: "Bluetooth", Area: "Devices", Level: Read, Method: "GET", Path: "/api/bluetooth", ReadOnly: true,
		Desc: "The sync computer's Bluetooth adapter: on, off or blocked, and who is using it.",
		Args: []Arg{{Name: "node", In: InQuery, Type: "integer", Desc: "a sync node's id (default: the one that pairs)"}}},
	{Name: "bluetooth_power_on", Title: "Turn Bluetooth on", Area: "Devices", Level: Write, Method: "POST", Path: "/api/bluetooth/power", Idempotent: true,
		Desc: "Turn the sync computer's Bluetooth adapter on (goarmin never turns it off).",
		Args: []Arg{{Name: "node", In: InBody, Type: "integer", Desc: "a sync node's id (default: the one that pairs)"}},
		Body: func(args map[string]any) ([]byte, http.Header, error) {
			body := map[string]any{"on": true}
			if n, ok := args["node"]; ok {
				body["node"] = n
			}
			b, _ := json.Marshal(body)
			return b, http.Header{"Content-Type": {"application/json"}}, nil
		}},
	{Name: "delete_watch_files", Title: "Delete files from a watch", Area: "Devices", Level: Write, Method: "POST", Path: "/api/devices/{id}/files/delete", Destructive: true,
		Desc:    "Delete activities from the watch at the next connection (they stay in goarmin). Only files list_watch_files marks deletable.",
		Args:    []Arg{deviceID(), {Name: "uids", In: InBody, Type: "array", Items: "string", Required: true, Desc: "the files' uids from list_watch_files"}, confirm()},
		Confirm: confirmDevice},
	{Name: "unpair_device", Title: "Unpair a watch", Area: "Devices", Level: Write, Method: "POST", Path: "/api/devices/{id}/unpair", Destructive: true,
		Desc: "Remove the watch's Bluetooth pairing with its sync computer. Ownership and data stay; Add a watch pairs it again.",
		Args: []Arg{deviceID(), confirm()}, Confirm: confirmDevice},
	{Name: "hand_over_device", Title: "Hand over a watch", Area: "Devices", Level: Write, Method: "POST", Path: "/api/devices/{id}/release", Destructive: true,
		Desc:    "End your ownership of a watch now (the next account that adds it owns it from then on). Its data stays yours.",
		Args:    []Arg{deviceID(), {Name: "unpair", In: InBody, Type: "boolean", Desc: "also remove its Bluetooth pairing"}, confirm()},
		Confirm: confirmDevice},
	{Name: "delete_device_data", Title: "Delete a device's data", Area: "Devices", Level: Write, Method: "POST", Path: "/api/devices/{id}/delete-data", Destructive: true,
		Desc: "Delete everything that came from a device from goarmin: its files and all data read from them. This cannot be undone.",
		Args: []Arg{deviceID(), confirm()}, Confirm: confirmDevice, SendConfirm: true},
	{Name: "list_sync_nodes", Title: "Sync nodes", Area: "Devices", Level: Read, Method: "GET", Path: "/api/nodes", ReadOnly: true,
		Desc: "The computers that sync watches for this account (all of them for an admin): last seen, version, Bluetooth, watches."},

	// Imports.
	{Name: "list_imports", Title: "Imports", Area: "Imports", Level: Read, Method: "GET", Path: "/api/imports", ReadOnly: true,
		Desc: "Imports (uploads, USB, Garmin Connect exports), newest first, with their counts.",
		Args: []Arg{
			{Name: "before", In: InQuery, Type: "integer", Desc: "for the next page: the last import's id"},
			{Name: "limit", In: InQuery, Type: "integer", Desc: "how many"},
		}},
	{Name: "get_import", Title: "An import", Area: "Imports", Level: Read, Method: "GET", Path: "/api/imports/{id}", ReadOnly: true,
		Desc: "One import's progress and summary.", Args: []Arg{id("the import's id")}},
	{Name: "import_files", Title: "Import files", Area: "Imports", Level: Write, Method: "POST", Path: "/api/imports",
		Desc: "Import FIT files or zips (a USB copy, a Garmin Connect export), sent as base64, at most 32 MB in all; larger uploads go through the dashboard. Follow it with get_import.",
		Args: []Arg{{Name: "files", In: InTool, Type: "array", Items: "object", Required: true,
			Desc: "the files: [{name: \"2026-11-08.fit\", content_base64: \"…\"}]; name may carry a folder path"}},
		Run: importFiles},
	{Name: "cancel_import", Title: "Stop an import", Area: "Imports", Level: Write, Method: "DELETE", Path: "/api/imports/{id}", Idempotent: true,
		Desc: "Stop a running import (what it imported so far stays).", Args: []Arg{id("the import's id")}},
	{Name: "list_usb_watches", Title: "Watches on USB", Area: "Imports", Level: Read, Method: "GET", Path: "/api/usb", ReadOnly: true,
		Desc: "Watches plugged in by USB at this server, how each is read and whose it is."},
	{Name: "import_usb", Title: "Import a watch on USB", Area: "Imports", Level: Write, Method: "POST", Path: "/api/usb/{id}/import",
		Desc: "Import a watch plugged in by USB now (read-only: nothing on the watch changes).",
		Args: []Arg{{Name: "id", In: InPath, Type: "string", Required: true, Desc: "the USB watch's id (list_usb_watches)"}}},

	// Pairing.
	{Name: "list_pairing_computers", Title: "Computers that pair", Area: "Pairing", Level: Read, Method: "GET", Path: "/api/pairing/computers", ReadOnly: true,
		Desc: "The computers that can add a watch (the server itself or sync nodes), with their Bluetooth state.",
		Args: []Arg{{Name: "owner", In: InQuery, Type: "integer", Desc: "admins: the account the watch is for"}}},
	{Name: "start_pairing", Title: "Add a watch", Area: "Pairing", Level: Write, Method: "POST", Path: "/api/pairing",
		Desc: "Start adding a watch: the user puts the watch in pairing mode (Watch Settings › Connectivity › Phone › Pair Phone), then pairing_scan. Follow the session with get_pairing_session.",
		Args: []Arg{
			{Name: "owner", In: InBody, Type: "integer", Desc: "admins: the account the watch is for"},
			{Name: "node", In: InBody, Type: "integer", Desc: "the computer that pairs (list_pairing_computers), when there are several"},
		}},
	{Name: "get_pairing", Title: "Your pairing session", Area: "Pairing", Level: Read, Method: "GET", Path: "/api/pairing", ReadOnly: true,
		Desc: "Your running or recent pairing session, if any."},
	{Name: "get_pairing_session", Title: "A pairing session", Area: "Pairing", Level: Read, Method: "GET", Path: "/api/pairing/{id}", ReadOnly: true,
		Desc: "A pairing session's state: the watches found, a code to compare, the identified watch. Poll it every few seconds while pairing.",
		Args: []Arg{pairingID()}, Covers: []string{"GET /api/pairing/{id}/events"}},
	{Name: "pairing_scan", Title: "Scan for watches", Area: "Pairing", Level: Write, Method: "POST", Path: "/api/pairing/{id}/scan",
		Desc: "Start or restart the search for watches in pairing mode.", Args: []Arg{pairingID()}},
	{Name: "pairing_select", Title: "Choose a watch", Area: "Pairing", Level: Write, Method: "POST", Path: "/api/pairing/{id}/select",
		Desc: "Pair the chosen watch. The watch then shows a code: ask the user to compare it with the session's code before pairing_confirm.",
		Args: []Arg{pairingID(),
			{Name: "address", In: InBody, Type: "string", Required: true, Desc: "the watch's address from the session's list"},
			{Name: "repair", In: InBody, Type: "boolean", Desc: "remove an existing pairing first (Pair again)"}}},
	{Name: "pairing_confirm", Title: "Confirm the code", Area: "Pairing", Level: Write, Method: "POST", Path: "/api/pairing/{id}/confirm",
		Desc: "Answer whether the code on the watch matches the session's code. Only the user can tell: ask them, and they confirm on the watch too.",
		Args: []Arg{pairingID(), {Name: "match", In: InBody, Type: "boolean", Required: true, Desc: "true when both codes match"}}},
	{Name: "pairing_passkey", Title: "Enter the watch's code", Area: "Pairing", Level: Write, Method: "POST", Path: "/api/pairing/{id}/passkey",
		Desc: "Send the six-digit code the watch shows, for watches that ask for it to be typed.",
		Args: []Arg{pairingID(), {Name: "passkey", In: InBody, Type: "integer", Required: true, Desc: "the code, 0..999999"}}},
	{Name: "pairing_setup", Title: "Set up the watch", Area: "Pairing", Level: Write, Method: "POST", Path: "/api/pairing/{id}/setup",
		Desc: "Finish adding the identified watch: its name, whether Garmin Connect on a phone still syncs it, and the first sync.",
		Args: []Arg{pairingID(),
			{Name: "nickname", In: InBody, Type: "string", Required: true, Desc: "the watch's name, at most 60 characters"},
			{Name: "garmin_connect", In: InBody, Type: "boolean", Required: true, Desc: "true: a phone's Garmin Connect still syncs it (files stay on the watch)"},
			{Name: "include_synced", In: InBody, Type: "boolean", Desc: "also fetch older activities still on the watch"}}},
	{Name: "cancel_pairing", Title: "Cancel adding a watch", Area: "Pairing", Level: Write, Method: "DELETE", Path: "/api/pairing/{id}", Idempotent: true,
		Desc: "Cancel a pairing session.", Args: []Arg{pairingID()}},

	// Files.
	{Name: "list_files", Title: "Stored files", Area: "Files", Level: Read, Method: "GET", Path: "/api/files", ReadOnly: true,
		Desc: "The account's stored raw files, newest first; read one with the goarmin://files/{id} resource or inspect_file.",
		Args: []Arg{
			{Name: "device", In: InQuery, Type: "integer", Desc: "only this device's files"},
			{Name: "type", In: InQuery, Type: "string", Desc: "a FIT file type number, or other"},
			{Name: "before", In: InQuery, Type: "integer", Desc: "for the next page: the last file's id"},
			{Name: "limit", In: InQuery, Type: "integer", Desc: "1..500, default 100"},
		}},
	{Name: "inspect_file", Title: "Inspect a FIT file", Area: "Files", Level: Read, Method: "GET", Path: "/api/files/{id}/messages", ReadOnly: true,
		Desc: "A stored FIT file decoded: counts per message, then messages with named fields and units.",
		Args: []Arg{
			id("the file's id (list_files)"),
			{Name: "num", In: InQuery, Type: "integer", Desc: "only this message number"},
			{Name: "offset", In: InQuery, Type: "integer", Desc: "skip this many messages"},
			{Name: "limit", In: InQuery, Type: "integer", Desc: "1..2000, default 200"},
		}},
	{Name: "list_account_documents", Title: "Account data", Area: "Files", Level: Read, Method: "GET", Path: "/api/account/documents", ReadOnly: true,
		Desc: "Records from the Garmin Connect export (profile, settings, goals, orders, consents, device registrations, …), listed."},
	{Name: "get_account_document", Title: "An account record", Area: "Files", Level: Read, Method: "GET", Path: "/api/account/documents/{id}", ReadOnly: true,
		Desc: "One record from the Garmin Connect export.", Args: []Arg{id("the record's id (list_account_documents)")}},

	// Admin.
	{Name: "list_users", Title: "Users", Area: "Admin", Level: Admin, Method: "GET", Path: "/api/admin/users", ReadOnly: true,
		Desc: "The server's accounts and open invites."},
	{Name: "create_invite", Title: "Invite someone", Area: "Admin", Level: Admin, Method: "POST", Path: "/api/admin/invites",
		Desc: "Invite a person to make an account: returns a link to send them (valid 7 days).",
		Args: []Arg{{Name: "email", In: InBody, Type: "string", Required: true, Desc: "their e-mail address"}}},
	{Name: "list_all_devices", Title: "Devices on this server", Area: "Admin", Level: Admin, Method: "GET", Path: "/api/admin/devices", ReadOnly: true,
		Desc: "Every device on the server with its owner and sync state (no health data)."},
	{Name: "admin_release_device", Title: "Release a device", Area: "Admin", Level: Admin, Method: "POST", Path: "/api/admin/devices/{id}/release", Destructive: true,
		Desc: "End a device's current ownership (its data stays with the owner).", Args: []Arg{id("the device's id (list_all_devices)"), confirm()},
		Confirm: confirmAdminDevice},
	{Name: "admin_forget_device", Title: "Forget a pairing", Area: "Admin", Level: Admin, Method: "POST", Path: "/api/admin/devices/{id}/forget", Destructive: true,
		Desc: "Remove a watch's Bluetooth pairing from the computer that syncs it.", Args: []Arg{id("the device's id (list_all_devices)"), confirm()},
		Confirm: confirmAdminDevice},
	{Name: "get_running_pairing", Title: "Pairing on the server", Area: "Admin", Level: Admin, Method: "GET", Path: "/api/admin/pairing", ReadOnly: true,
		Desc: "Whoever is adding a watch on the server right now."},
	{Name: "end_running_pairing", Title: "End the pairing session", Area: "Admin", Level: Admin, Method: "DELETE", Path: "/api/admin/pairing", Idempotent: true,
		Desc: "End whatever pairing session runs on the server."},
	{Name: "rename_sync_node", Title: "Rename a sync node", Area: "Admin", Level: Admin, Method: "PATCH", Path: "/api/nodes/{id}", Idempotent: true,
		Desc: "Rename a sync node.", Args: []Arg{id("the node's id (list_sync_nodes)"), {Name: "name", In: InBody, Type: "string", Required: true, Desc: "the new name"}}},
	{Name: "revoke_sync_node", Title: "Revoke a sync node", Area: "Admin", Level: Admin, Method: "DELETE", Path: "/api/nodes/{id}", Destructive: true, Idempotent: true,
		Desc: "End a sync node's sign-in: it stops syncing until it signs in again.", Args: []Arg{id("the node's id (list_sync_nodes)")}},
	{Name: "approve_sync_node", Title: "Approve a sync node", Area: "Admin", Level: Admin, Method: "POST", Path: "/api/nodes/approve",
		Desc: "Approve a computer's sign-in code (goarmin node login without a password shows it), so it syncs for this account.",
		Args: []Arg{{Name: "code", In: InBody, Type: "string", Required: true, Desc: "the code the computer shows, e.g. KQ4M-7XRT"}}},
}
