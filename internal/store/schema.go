// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

// migrations are append-only: never edit a shipped entry, add a new one. Times are Unix
// seconds (UTC); local days are "YYYY-MM-DD" in the owner's local time at that moment.
var migrations = []string{
	// 1: accounts, devices, raw files, ingest, health data.
	`
CREATE TABLE users (
	id INTEGER PRIMARY KEY,
	email TEXT NOT NULL UNIQUE COLLATE NOCASE,
	display_name TEXT NOT NULL DEFAULT '',
	timezone TEXT NOT NULL DEFAULT '',          -- IANA name; '' = the server's zone
	is_admin INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);

CREATE TABLE devices (
	id INTEGER PRIMARY KEY,
	unit_id INTEGER NOT NULL UNIQUE,            -- the watch's unit number = FIT serial
	product INTEGER NOT NULL DEFAULT 0,
	name TEXT NOT NULL DEFAULT '',
	firmware TEXT NOT NULL DEFAULT '',
	address TEXT NOT NULL DEFAULT '',           -- Bluetooth address, capitals
	created_at INTEGER NOT NULL
);

-- Who wore a device when. Handing a watch on closes one period and opens another, so data
-- never leaks to the next owner.
CREATE TABLE device_assignments (
	device_id INTEGER NOT NULL REFERENCES devices(id),
	user_id INTEGER NOT NULL REFERENCES users(id),
	valid_from INTEGER NOT NULL,                -- 0 = from the start
	valid_to INTEGER,                           -- NULL = still
	PRIMARY KEY (device_id, valid_from)
);

-- Every file goarmin received, kept verbatim in the blob store under sha256. The source of
-- truth: everything below is derived from these and can be rebuilt.
CREATE TABLE raw_files (
	id INTEGER PRIMARY KEY,
	sha256 TEXT NOT NULL UNIQUE,
	size INTEGER NOT NULL,
	user_id INTEGER NOT NULL REFERENCES users(id),
	device_id INTEGER REFERENCES devices(id),
	source TEXT NOT NULL,                       -- ble, usb, gc_export, upload
	source_name TEXT NOT NULL DEFAULT '',
	is_fit INTEGER NOT NULL DEFAULT 0,
	fit_type INTEGER,
	fit_manufacturer INTEGER,
	fit_product INTEGER,
	fit_serial INTEGER,
	fit_time_created INTEGER,
	fit_number INTEGER,
	duplicate_of INTEGER REFERENCES raw_files(id),
	received_at INTEGER NOT NULL
);
CREATE INDEX raw_files_identity ON raw_files(fit_serial, fit_type, fit_time_created, fit_number);
CREATE INDEX raw_files_user ON raw_files(user_id, fit_type);

CREATE TABLE ingest_state (
	raw_file_id INTEGER NOT NULL REFERENCES raw_files(id),
	handler TEXT NOT NULL,
	version INTEGER NOT NULL,
	status TEXT NOT NULL,                       -- ok, error
	error TEXT NOT NULL DEFAULT '',
	span_from INTEGER,
	span_to INTEGER,
	ingested_at INTEGER NOT NULL,
	PRIMARY KEY (raw_file_id, handler)
);

-- Time series (pkg/garmin/wellness kinds). sub separates per-activity-type running totals.
CREATE TABLE samples (
	user_id INTEGER NOT NULL,
	kind INTEGER NOT NULL,
	ts INTEGER NOT NULL,
	sub INTEGER NOT NULL DEFAULT 0,
	value INTEGER NOT NULL,
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, kind, ts, sub)
) WITHOUT ROWID;
CREATE INDEX samples_file ON samples(raw_file_id);

-- The watch's own UTC offset history: from ts on, local = UTC + offset_s.
CREATE TABLE tz_offsets (
	user_id INTEGER NOT NULL,
	ts INTEGER NOT NULL,
	offset_s INTEGER NOT NULL,
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, ts)
) WITHOUT ROWID;
CREATE INDEX tz_offsets_file ON tz_offsets(raw_file_id);

CREATE TABLE daily_values (
	user_id INTEGER NOT NULL,
	kind INTEGER NOT NULL,
	day TEXT NOT NULL,
	ts INTEGER NOT NULL,
	value INTEGER NOT NULL,
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, kind, day)
) WITHOUT ROWID;
CREATE INDEX daily_values_file ON daily_values(raw_file_id);

-- One row per sleep; day is the local date of waking up. Several files may describe the same
-- night: each fills the columns it knows.
CREATE TABLE sleep_sessions (
	user_id INTEGER NOT NULL,
	start_ts INTEGER NOT NULL,
	end_ts INTEGER NOT NULL,
	day TEXT NOT NULL,
	score INTEGER,
	quality_score INTEGER,
	duration_score INTEGER,
	recovery_score INTEGER,
	min_hr INTEGER,
	bb_start INTEGER,
	bb_end INTEGER,
	deep_s INTEGER,
	light_s INTEGER,
	rem_s INTEGER,
	awake_s INTEGER,
	unmeasurable_s INTEGER,
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, start_ts)
);
CREATE INDEX sleep_sessions_day ON sleep_sessions(user_id, day);
CREATE INDEX sleep_sessions_file ON sleep_sessions(raw_file_id);

CREATE TABLE hrv_nightly (
	user_id INTEGER NOT NULL,
	day TEXT NOT NULL,
	ts INTEGER NOT NULL,
	weekly_avg_ms REAL,
	last_night_ms REAL,
	last_night_high_ms REAL,
	baseline_low_upper_ms REAL,
	balanced_lower_ms REAL,
	balanced_upper_ms REAL,
	status INTEGER,
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, day)
);
CREATE INDEX hrv_nightly_file ON hrv_nightly(raw_file_id);

-- Rebuilt from the tables above whenever a file touching the day is ingested.
CREATE TABLE daily_summary (
	user_id INTEGER NOT NULL,
	day TEXT NOT NULL,
	steps INTEGER,
	distance_cm INTEGER,
	active_kcal INTEGER,
	ascent_mm INTEGER,
	descent_mm INTEGER,
	moderate_min INTEGER,
	vigorous_min INTEGER,
	hr_min INTEGER,
	hr_max INTEGER,
	hr_avg INTEGER,
	resting_hr INTEGER,
	stress_avg INTEGER,
	stress_max INTEGER,
	bb_min INTEGER,
	bb_max INTEGER,
	respiration_avg INTEGER,
	spo2_avg INTEGER,
	sleep_score INTEGER,
	sleep_s INTEGER,
	updated_at INTEGER NOT NULL,
	PRIMARY KEY (user_id, day)
);
`,
	// 2: activities (one row per session), laps, tracks; Garmin Coach suggestions.
	`
CREATE TABLE activities (
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id),
	raw_file_id INTEGER NOT NULL REFERENCES raw_files(id),
	session_index INTEGER NOT NULL DEFAULT 0,
	start_ts INTEGER NOT NULL,
	end_ts INTEGER NOT NULL,
	day TEXT NOT NULL,
	sport INTEGER NOT NULL,
	sub_sport INTEGER NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	elapsed_s REAL,
	timer_s REAL,
	distance_m REAL,
	kcal INTEGER,
	avg_hr INTEGER,
	max_hr INTEGER,
	avg_speed REAL,                             -- m/s
	max_speed REAL,
	avg_cadence INTEGER,
	avg_power INTEGER,
	ascent_m INTEGER,
	descent_m INTEGER,
	training_effect REAL,
	anaerobic_effect REAL,
	training_load REAL,
	min_lat REAL, min_lon REAL, max_lat REAL, max_lon REAL,
	polyline TEXT NOT NULL DEFAULT '',          -- simplified track, encoded polyline
	UNIQUE (user_id, start_ts, session_index)
);
CREATE INDEX activities_user_start ON activities(user_id, start_ts DESC);
CREATE INDEX activities_file ON activities(raw_file_id);

CREATE TABLE laps (
	activity_id INTEGER NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
	idx INTEGER NOT NULL,
	start_ts INTEGER NOT NULL,
	elapsed_s REAL,
	timer_s REAL,
	distance_m REAL,
	kcal INTEGER,
	avg_hr INTEGER,
	max_hr INTEGER,
	avg_speed REAL,
	max_speed REAL,
	avg_cadence INTEGER,
	avg_power INTEGER,
	ascent_m INTEGER,
	descent_m INTEGER,
	PRIMARY KEY (activity_id, idx)
);

-- The recorded track as gzipped JSON columns (fitactivity.Track.Columns).
CREATE TABLE activity_streams (
	activity_id INTEGER PRIMARY KEY REFERENCES activities(id) ON DELETE CASCADE,
	points INTEGER NOT NULL,
	fields TEXT NOT NULL,                       -- "lat,lon,hr,…"
	data BLOB NOT NULL
);

CREATE TABLE suggested_workouts (
	user_id INTEGER NOT NULL,
	day TEXT NOT NULL,
	idx INTEGER NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	sport INTEGER,
	sub_sport INTEGER,
	duration_s INTEGER,
	benefit INTEGER,
	aerobic_te REAL,
	anaerobic_te REAL,
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, day, idx)
);
`,
	// 3: logins. Tokens are stored as their SHA-256 only.
	`
ALTER TABLE users ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN webauthn_id BLOB;      -- random passkey user handle
ALTER TABLE users ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX users_webauthn_id ON users(webauthn_id);

CREATE TABLE sessions (
	token_hash TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id),
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	last_seen INTEGER NOT NULL,
	user_agent TEXT NOT NULL DEFAULT '',
	ip TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE invites (
	token_hash TEXT PRIMARY KEY,
	email TEXT NOT NULL DEFAULT '',              -- '' = whoever has the link
	created_by INTEGER NOT NULL REFERENCES users(id),
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	used_at INTEGER,
	used_by INTEGER REFERENCES users(id)
);

CREATE TABLE webauthn_credentials (
	id BLOB PRIMARY KEY,                         -- the credential ID
	user_id INTEGER NOT NULL REFERENCES users(id),
	name TEXT NOT NULL DEFAULT '',
	credential TEXT NOT NULL,                    -- webauthn.Credential as JSON
	created_at INTEGER NOT NULL,
	last_used_at INTEGER
);
CREATE INDEX webauthn_credentials_user ON webauthn_credentials(user_id);
`,
	// 4: what happens on the watch after a fetch; the sync log.
	`
ALTER TABLE devices ADD COLUMN after_fetch TEXT NOT NULL DEFAULT 'keep';   -- keep, archive, archive_delete

CREATE TABLE sync_runs (
	id INTEGER PRIMARY KEY,
	device_id INTEGER NOT NULL REFERENCES devices(id),
	started_at INTEGER NOT NULL,
	finished_at INTEGER NOT NULL,
	ok INTEGER NOT NULL,
	pulled INTEGER NOT NULL DEFAULT 0,
	archived INTEGER NOT NULL DEFAULT 0,
	deleted INTEGER NOT NULL DEFAULT 0,
	bytes INTEGER NOT NULL DEFAULT 0,
	imported INTEGER NOT NULL DEFAULT 0,
	error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sync_runs_device ON sync_runs(device_id, started_at DESC);
`,
	// 5: per-user settings (the glance layout, display preferences) and units.
	`
CREATE TABLE user_settings (
	user_id INTEGER NOT NULL REFERENCES users(id),
	key TEXT NOT NULL,
	value TEXT NOT NULL,                        -- JSON
	rev INTEGER NOT NULL,                       -- bumped on every write; If-Match compares it
	updated_at INTEGER NOT NULL,
	PRIMARY KEY (user_id, key)
);
ALTER TABLE users ADD COLUMN units TEXT NOT NULL DEFAULT 'metric';
`,
	// 6: the Devices section: a watch's own name, battery, pause, whether it deletes files; who
	// owned a watch at each sync, how the sync started, and why it failed.
	`
ALTER TABLE devices ADD COLUMN nickname TEXT NOT NULL DEFAULT '';          -- set by its owner; '' = the watch's name
ALTER TABLE devices ADD COLUMN battery INTEGER;                            -- percent, as last reported
ALTER TABLE devices ADD COLUMN battery_at INTEGER;
ALTER TABLE devices ADD COLUMN paused INTEGER NOT NULL DEFAULT 0;          -- no scheduled syncs
ALTER TABLE devices ADD COLUMN delete_support TEXT NOT NULL DEFAULT '';    -- '' unknown, 'yes', 'no'

ALTER TABLE sync_runs ADD COLUMN owner_id INTEGER REFERENCES users(id);    -- the owner at the time
ALTER TABLE sync_runs ADD COLUMN started_by TEXT NOT NULL DEFAULT 'schedule'; -- schedule, manual, wizard, delete
ALTER TABLE sync_runs ADD COLUMN listed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sync_runs ADD COLUMN failed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sync_runs ADD COLUMN error_code TEXT NOT NULL DEFAULT '';     -- not_in_range, not_bonded, adapter_off, …

CREATE INDEX raw_files_device ON raw_files(device_id);
`,
	// 7: error codes for the syncs logged before there were any: a connection that timed out was
	// the watch out of range; a missing owner is not a failure of the watch.
	`
UPDATE sync_runs SET error_code = 'not_in_range'
	WHERE error_code = '' AND ok = 0 AND error LIKE 'gattlink: connecting to %context deadline exceeded%';
UPDATE sync_runs SET error_code = 'no_owner'
	WHERE error_code = '' AND ok = 0 AND (error LIKE 'nodehub: no account yet%' OR error LIKE 'nodehub: nobody owns%');
UPDATE sync_runs SET error_code = 'transfer' WHERE error_code = '' AND ok = 0;
`,
	// 8: a battery stored from a sync that never reached the watch (0 %, no time) is unknown.
	`
UPDATE devices SET battery = NULL, battery_at = NULL WHERE battery_at IS NOT NULL AND battery_at <= 0;
`,
	// 9: what else the watch keeps: its own time in zones per activity, its settings, sport
	// profiles, goals, lifetime totals, workouts and courses. Rows belong to the file's owner;
	// the newest file wins per key. device_id 0: a file from before the device was recorded.
	`
CREATE TABLE time_in_zone (
	activity_id INTEGER NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
	lap INTEGER NOT NULL,                       -- -1: the session
	data TEXT NOT NULL,                         -- JSON: limits, seconds per zone, max/resting/threshold HR
	PRIMARY KEY (activity_id, lap)
) WITHOUT ROWID;

CREATE TABLE watch_settings (
	user_id INTEGER NOT NULL REFERENCES users(id),
	device_id INTEGER NOT NULL,
	data TEXT NOT NULL,                         -- JSON: clock, units, resting HR, sleep window, zones, bikes
	recorded_at INTEGER NOT NULL,
	raw_file_id INTEGER NOT NULL REFERENCES raw_files(id),
	PRIMARY KEY (user_id, device_id)
);

CREATE TABLE sport_profiles (
	user_id INTEGER NOT NULL REFERENCES users(id),
	device_id INTEGER NOT NULL,
	sport TEXT NOT NULL,
	sub_sport TEXT NOT NULL,
	name TEXT NOT NULL,
	zones TEXT NOT NULL,                        -- JSON: max, threshold, FTP, method, six HR limits
	recorded_at INTEGER NOT NULL,
	raw_file_id INTEGER NOT NULL REFERENCES raw_files(id),
	PRIMARY KEY (user_id, device_id, sport, sub_sport, name)
);

CREATE TABLE watch_goals (
	user_id INTEGER NOT NULL REFERENCES users(id),
	device_id INTEGER NOT NULL,
	type TEXT NOT NULL,                         -- steps, active_minutes, ascent, …
	sport TEXT NOT NULL,                        -- '' for all
	target INTEGER NOT NULL,
	unit TEXT NOT NULL,
	source TEXT NOT NULL DEFAULT '',            -- auto, user
	recurrence TEXT NOT NULL DEFAULT '',
	recorded_at INTEGER NOT NULL,
	raw_file_id INTEGER NOT NULL REFERENCES raw_files(id),
	PRIMARY KEY (user_id, device_id, type, sport)
);

CREATE TABLE lifetime_totals (
	user_id INTEGER NOT NULL REFERENCES users(id),
	device_id INTEGER NOT NULL,
	sport TEXT NOT NULL,
	timer_s INTEGER NOT NULL,
	elapsed_s INTEGER,
	active_s INTEGER,
	distance_m REAL NOT NULL,
	kcal INTEGER NOT NULL,
	sessions INTEGER NOT NULL,
	recorded_at INTEGER NOT NULL,
	raw_file_id INTEGER NOT NULL REFERENCES raw_files(id),
	PRIMARY KEY (user_id, device_id, sport)
);

CREATE TABLE workouts (
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id),
	device_id INTEGER NOT NULL,
	name TEXT NOT NULL,
	sport TEXT NOT NULL,
	sub_sport TEXT NOT NULL,
	steps TEXT NOT NULL,                        -- JSON steps
	estimated_s INTEGER NOT NULL,
	recorded_at INTEGER NOT NULL,
	raw_file_id INTEGER NOT NULL REFERENCES raw_files(id),
	UNIQUE (user_id, device_id, name, sport)
);

CREATE TABLE courses (
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id),
	device_id INTEGER NOT NULL,
	name TEXT NOT NULL,
	sport TEXT NOT NULL,
	distance_m REAL NOT NULL,
	ascent_m REAL NOT NULL,
	descent_m REAL NOT NULL,
	min_lat REAL, min_lon REAL, max_lat REAL, max_lon REAL,
	polyline TEXT NOT NULL DEFAULT '',
	points TEXT NOT NULL,                       -- JSON course points
	recorded_at INTEGER NOT NULL,
	raw_file_id INTEGER NOT NULL REFERENCES raw_files(id),
	UNIQUE (user_id, device_id, name)
);

CREATE TABLE course_streams (
	course_id INTEGER PRIMARY KEY REFERENCES courses(id) ON DELETE CASCADE,
	points INTEGER NOT NULL,
	fields TEXT NOT NULL,
	data BLOB NOT NULL                          -- gzip columnar JSON, as activity_streams
);
`,
	// 10: imports (files uploaded in the browser, a watch plugged in by USB) and what a USB
	// import has read from each watch already.
	`
ALTER TABLE devices ADD COLUMN usb_auto INTEGER NOT NULL DEFAULT 1;    -- import it when it is plugged in by USB
ALTER TABLE devices ADD COLUMN usb_serial TEXT NOT NULL DEFAULT '';    -- the serial it reports on USB, learnt from an import

CREATE TABLE imports (
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	kind TEXT NOT NULL,                         -- upload, usb
	source TEXT NOT NULL,                       -- the raw files' source: upload, usb, gc_export
	name TEXT NOT NULL DEFAULT '',              -- what was imported, in words
	device_id INTEGER REFERENCES devices(id) ON DELETE SET NULL,
	started_by TEXT NOT NULL DEFAULT 'manual',  -- manual, plugged
	status TEXT NOT NULL,                       -- uploading, queued, running, done, failed, canceled
	created_at INTEGER NOT NULL,
	started_at INTEGER,
	finished_at INTEGER,
	report TEXT NOT NULL DEFAULT '{}',          -- JSON: the counts, the errors, what was kept
	error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX imports_user ON imports(user_id, id);

CREATE TABLE usb_seen (
	serial TEXT NOT NULL,                       -- the watch's USB serial
	path TEXT NOT NULL,                         -- the file's path on the watch
	size INTEGER NOT NULL,
	modified INTEGER NOT NULL,
	sha256 TEXT NOT NULL,
	PRIMARY KEY (serial, path)
) WITHOUT ROWID;
`,
	// 11: nights from parts. Each file's account of a sleep is a part (a sleep file's stages, the
	// daily-health or metrics file's summary, an export's record); sleep_sessions holds the nights
	// merged from the parts that overlap, rebuilt whenever parts change, so one sleep is one night
	// whatever files describe it. The nights stored so far become parts of their files. Naps.
	`
CREATE TABLE sleep_parts (
	user_id INTEGER NOT NULL,
	raw_file_id INTEGER NOT NULL,
	source TEXT NOT NULL,                       -- stages, summary, json, legacy
	prio INTEGER NOT NULL,                      -- which part leads a night: stages 3, summary and legacy 2, json 1
	start_ts INTEGER NOT NULL,
	end_ts INTEGER NOT NULL,
	score INTEGER,
	quality_score INTEGER,
	duration_score INTEGER,
	recovery_score INTEGER,
	min_hr INTEGER,
	bb_start INTEGER,
	bb_end INTEGER,
	deep_s INTEGER,
	light_s INTEGER,
	rem_s INTEGER,
	awake_s INTEGER,
	unmeasurable_s INTEGER,
	PRIMARY KEY (raw_file_id, start_ts)
);
CREATE INDEX sleep_parts_user ON sleep_parts(user_id, start_ts);

INSERT OR IGNORE INTO sleep_parts (user_id, raw_file_id, source, prio, start_ts, end_ts, score, quality_score, duration_score,
	recovery_score, min_hr, bb_start, bb_end, deep_s, light_s, rem_s, awake_s, unmeasurable_s)
	SELECT user_id, raw_file_id, 'legacy', 2, start_ts, end_ts, score, quality_score, duration_score,
	recovery_score, min_hr, bb_start, bb_end, deep_s, light_s, rem_s, awake_s, unmeasurable_s FROM sleep_sessions;

CREATE TABLE naps (
	user_id INTEGER NOT NULL,
	start_ts INTEGER NOT NULL,
	end_ts INTEGER NOT NULL,
	day TEXT NOT NULL,                          -- local date
	feedback INTEGER,                           -- FIT nap_period_feedback
	how INTEGER,                                -- FIT nap_source: 0 automatic, 1 manual, …
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, start_ts)
);
CREATE INDEX naps_day ON naps(user_id, day);
CREATE INDEX naps_file ON naps(raw_file_id);
`,
	// 12: samples lose their index by file: a file's samples lie within the span its ingest
	// recorded, where the primary key finds them, and the index made every insert of a large
	// import a third dearer.
	`
DROP INDEX samples_file;
`,
	// 13: what sort of device each is (product.Kind: watch, scale, bike_computer, sensor,
	// handheld, app, other); '' until classified (at start, from the product and the files).
	`
ALTER TABLE devices ADD COLUMN kind TEXT NOT NULL DEFAULT '';
`,
	// 14: every FIT reader's tables. Weigh-ins (a scale's files, the export's biometrics), the
	// nightly skin temperature, ECG recordings with their samples, physiological metrics, gear,
	// golf clubs, personal records, the settings change log, device series (solar charge) and
	// device documents (what a settings, device or multisport file holds beyond the tables).
	`
CREATE TABLE weigh_ins (
	user_id INTEGER NOT NULL,
	ts INTEGER NOT NULL,
	weight_kg REAL NOT NULL,
	fat_pct REAL,
	hydration_pct REAL,
	bone_kg REAL,
	muscle_kg REAL,
	visceral_fat_kg REAL,
	bmi REAL,
	bmr_kcal INTEGER,
	active_mr_kcal INTEGER,
	metabolic_age INTEGER,
	visceral_rating INTEGER,
	build INTEGER,                              -- the scale's physique rating
	source TEXT NOT NULL,                       -- fit, json
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, ts)
);
CREATE INDEX weigh_ins_file ON weigh_ins(raw_file_id);

CREATE TABLE skin_temp_nightly (
	user_id INTEGER NOT NULL,
	day TEXT NOT NULL,
	ts INTEGER NOT NULL,
	nightly_c REAL,
	deviation_c REAL,
	deviation7d_c REAL,
	calibrated_days INTEGER,
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, day)
);
CREATE INDEX skin_temp_nightly_file ON skin_temp_nightly(raw_file_id);

CREATE TABLE ecg_recordings (
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL,
	start_ts INTEGER NOT NULL,
	day TEXT NOT NULL,
	class INTEGER,                              -- ecg_rhythm_classification
	side INTEGER,
	avg_hr REAL,
	rmssd_ms INTEGER,
	seconds INTEGER,
	raw_rate REAL,
	smooth_rate REAL,
	samples BLOB,                               -- gzip JSON {"raw": [...], "smooth": [...]}
	source TEXT NOT NULL,                       -- fit, json
	raw_file_id INTEGER NOT NULL,
	UNIQUE (user_id, start_ts)
);
CREATE INDEX ecg_recordings_file ON ecg_recordings(raw_file_id);

CREATE TABLE physiology (
	user_id INTEGER NOT NULL,
	ts INTEGER NOT NULL,
	sport TEXT NOT NULL DEFAULT '',
	vo2max REAL,
	lthr INTEGER,
	lt_power INTEGER,
	lt_speed_kmh REAL,
	max_hr INTEGER,
	avg_hr INTEGER,
	recovery_min INTEGER,
	aerobic_te REAL,
	anaerobic_te REAL,
	duration_s REAL,
	distance_m REAL,
	race_5k_s INTEGER,
	race_10k_s INTEGER,
	race_half_s INTEGER,
	race_marathon_s INTEGER,
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, ts)
);
CREATE INDEX physiology_file ON physiology(raw_file_id);

CREATE TABLE gear (
	user_id INTEGER NOT NULL,
	uuid TEXT NOT NULL,
	source TEXT NOT NULL,                       -- fit, json
	type TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL DEFAULT '',
	brand TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	data TEXT NOT NULL DEFAULT '{}',            -- JSON: everything else the source says
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, uuid, source)
);
CREATE INDEX gear_file ON gear(raw_file_id);

CREATE TABLE golf_clubs (
	user_id INTEGER NOT NULL,
	device_id INTEGER NOT NULL DEFAULT 0,
	idx INTEGER NOT NULL,
	type INTEGER,
	name TEXT NOT NULL DEFAULT '',
	avg_distance_m REAL,
	max_distance_m REAL,
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, device_id, idx)
);
CREATE INDEX golf_clubs_file ON golf_clubs(raw_file_id);

CREATE TABLE personal_records (
	user_id INTEGER NOT NULL,
	source TEXT NOT NULL,                       -- fit, json
	metric TEXT NOT NULL,                       -- the source's record kind ("@distance" for one over a distance)
	sport TEXT NOT NULL DEFAULT '',
	ts INTEGER NOT NULL,
	value REAL,
	distance_m REAL,
	activity_id INTEGER,
	data TEXT NOT NULL DEFAULT '{}',
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, source, metric, sport)
);
CREATE INDEX personal_records_file ON personal_records(raw_file_id);

CREATE TABLE settings_changes (
	user_id INTEGER NOT NULL,
	device_id INTEGER NOT NULL DEFAULT 0,
	ts INTEGER NOT NULL,
	message TEXT NOT NULL,
	field TEXT NOT NULL,
	value TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL DEFAULT '',
	sport TEXT NOT NULL DEFAULT '',
	raw_file_id INTEGER NOT NULL
);
CREATE INDEX settings_changes_user ON settings_changes(user_id, device_id, ts);
CREATE INDEX settings_changes_file ON settings_changes(raw_file_id);

CREATE TABLE device_series (
	user_id INTEGER NOT NULL,
	device_id INTEGER NOT NULL,
	kind TEXT NOT NULL,                         -- solar (percent), solar_gain (ms)
	ts INTEGER NOT NULL,                        -- the start of an hour
	value REAL NOT NULL,
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, device_id, kind, ts)
) WITHOUT ROWID;
CREATE INDEX device_series_file ON device_series(raw_file_id);

CREATE TABLE device_documents (
	user_id INTEGER NOT NULL,
	device_id INTEGER NOT NULL,
	kind TEXT NOT NULL,                         -- device, settings, multisport
	data TEXT NOT NULL,                         -- JSON: message name → the messages
	recorded_at INTEGER NOT NULL,
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, device_id, kind)
);
CREATE INDEX device_documents_file ON device_documents(raw_file_id);
`,
	// 15: a Garmin Connect export's files read. Daily values rank by source (a watch's FIT file
	// 2 over the export's JSON 1); activities keep what only Garmin Connect knows; health status,
	// hydration and health snapshots; the account's records as documents.
	`
ALTER TABLE daily_values ADD COLUMN rank INTEGER NOT NULL DEFAULT 2;
CREATE INDEX daily_values_day ON daily_values(user_id, day);
ALTER TABLE activities ADD COLUMN extra TEXT NOT NULL DEFAULT '{}';

CREATE TABLE health_status (
	user_id INTEGER NOT NULL,
	day TEXT NOT NULL,
	metric TEXT NOT NULL,                       -- hrv, hr, respiration, skin_temp_c, spo2, …
	value REAL,
	low REAL,
	high REAL,
	pct REAL,
	status TEXT NOT NULL DEFAULT '',
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, day, metric)
);
CREATE INDEX health_status_file ON health_status(raw_file_id);

CREATE TABLE hydration_logs (
	user_id INTEGER NOT NULL,
	ts INTEGER NOT NULL,
	day TEXT NOT NULL,
	ml REAL NOT NULL,
	sweat_ml REAL NOT NULL DEFAULT 0,
	source TEXT NOT NULL DEFAULT '',
	activity_id INTEGER,
	raw_file_id INTEGER NOT NULL
);
CREATE INDEX hydration_logs_day ON hydration_logs(user_id, day);
CREATE INDEX hydration_logs_file ON hydration_logs(raw_file_id);

CREATE TABLE health_snapshots (
	user_id INTEGER NOT NULL,
	start_ts INTEGER NOT NULL,
	end_ts INTEGER NOT NULL,
	day TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	data TEXT NOT NULL DEFAULT '{}',
	raw_file_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, start_ts)
);
CREATE INDEX health_snapshots_file ON health_snapshots(raw_file_id);

CREATE TABLE account_documents (
	raw_file_id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL,
	kind TEXT NOT NULL,                         -- profile, settings, orders, consents, inreach_billing, …
	name TEXT NOT NULL DEFAULT '',              -- the file's path in the export
	data TEXT NOT NULL,                         -- JSON
	recorded_at INTEGER NOT NULL
);
CREATE INDEX account_documents_user ON account_documents(user_id, kind);
`,
	// 16: sync nodes. A node is a computer that syncs watches near it and uploads their files;
	// it belongs to the account that signed it in, and its token is kept as a SHA-256 only.
	`
CREATE TABLE sync_nodes (
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id),
	name TEXT NOT NULL,
	hostname TEXT NOT NULL DEFAULT '',
	token_hash TEXT NOT NULL UNIQUE,
	created_at INTEGER NOT NULL,
	last_seen INTEGER NOT NULL DEFAULT 0,
	version TEXT NOT NULL DEFAULT '',
	revoked_at INTEGER
);
CREATE INDEX sync_nodes_user ON sync_nodes(user_id);
ALTER TABLE devices ADD COLUMN node_id INTEGER;

CREATE TABLE node_enrollments (
	code TEXT PRIMARY KEY,                      -- shown on the node, typed in the dashboard
	poll_hash TEXT NOT NULL UNIQUE,             -- the node polls with this secret
	name TEXT NOT NULL,
	hostname TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	user_id INTEGER,                            -- set once approved
	node_id INTEGER,                            -- the node made on approval
	token TEXT                                  -- handed to the node once, then cleared
);
`,
	// 17: the workout library. A workout comes from a watch's workout file or from a Garmin
	// Connect export (origin); the library groups the versions of one workout by name and
	// sport. workout_files names every workout file a watch handed over (also one a newer
	// version replaced). watch_items is what each watch keeps of the types a sync takes stock of
	// (its workouts), item by item, linked to the file a sync stored it as; devices.inventory_at
	// is when a watch last took stock.
	`
CREATE TABLE workouts_v17 (
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id),
	origin TEXT NOT NULL DEFAULT 'watch',       -- watch, garmin_connect
	device_id INTEGER NOT NULL,
	name TEXT NOT NULL,
	sport TEXT NOT NULL,
	sub_sport TEXT NOT NULL,
	steps TEXT NOT NULL,                        -- JSON steps
	estimated_s INTEGER NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	recorded_at INTEGER NOT NULL,
	raw_file_id INTEGER NOT NULL REFERENCES raw_files(id),
	UNIQUE (user_id, origin, device_id, name, sport)
);
INSERT INTO workouts_v17 (id, user_id, origin, device_id, name, sport, sub_sport, steps, estimated_s, recorded_at, raw_file_id)
	SELECT id, user_id, 'watch', device_id, name, sport, sub_sport, steps, estimated_s, recorded_at, raw_file_id FROM workouts;
DROP TABLE workouts;
ALTER TABLE workouts_v17 RENAME TO workouts;
CREATE INDEX workouts_file ON workouts(raw_file_id);

CREATE TABLE workout_files (
	raw_file_id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL,
	name TEXT NOT NULL,
	sport TEXT NOT NULL
);
INSERT INTO workout_files (raw_file_id, user_id, name, sport) SELECT raw_file_id, user_id, name, sport FROM workouts;

CREATE TABLE watch_items (
	device_id INTEGER NOT NULL,
	uid TEXT NOT NULL,                          -- the watch's item id, as goarmin prints it
	type TEXT NOT NULL,                         -- FIT_TYPE_5
	size INTEGER NOT NULL DEFAULT 0,
	raw_file_id INTEGER,                        -- the file a sync stored it as (NULL: none yet)
	listed_at INTEGER,                          -- the last inventory that listed it
	gone_at INTEGER,                            -- the first inventory that no longer did
	PRIMARY KEY (device_id, uid)
) WITHOUT ROWID;
CREATE INDEX watch_items_file ON watch_items(raw_file_id);
ALTER TABLE devices ADD COLUMN inventory_at INTEGER;
`,
	// 18: the calendar. An activity recorded with a workout names it (a reingest fills older
	// ones). workout_plans are library workouts put on a day; name and sport are kept, so a
	// plan finds its workout again if the stored version is replaced.
	`
ALTER TABLE activities ADD COLUMN workout_name TEXT NOT NULL DEFAULT '';

CREATE TABLE workout_plans (
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id),
	day TEXT NOT NULL,                          -- local date, YYYY-MM-DD
	workout_id INTEGER NOT NULL,                -- a library workout's version when planned
	name TEXT NOT NULL,
	sport TEXT NOT NULL,
	note TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE INDEX workout_plans_user_day ON workout_plans(user_id, day);
`,
}
