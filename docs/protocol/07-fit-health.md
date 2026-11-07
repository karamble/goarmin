# 07 — Health data in the Fenix 8's FIT files

Part of the goarmin protocol notes (`docs/protocol/`). This note covers what
`pkg/garmin/wellness` reads, checked against our Fenix 8's own files:
- the 18 monitor and 12 metrics files Garmin Connect pulled in the capture (05);
- the USB copy;
- the files goarmin pulled in M2 (06).

Many of these messages are missing from the public FIT profile. Their field numbers and scales
come from Gadgetbridge's FIT profile (`FitCodeGenerator/src/main/resources/fit_profile.json` in
the Codeberg clone). Values were checked on our files where the files have them.

**Times:**
- FIT time is seconds since 1989-12-31 00:00 UTC (Unix − 631065600).
- `timestamp_16` is the low 16 bits of the time. It counts from the last full timestamp in the
  file and wraps every 65536 s.

## Which file holds what

| FIT type | What goarmin takes from it |
|---|---|
| 32 monitoring | heart rate, steps/distance/calories/active time (running totals), intensity, stress, Body Battery, respiration, altitude, UTC offset |
| 44 metrics | training status, acute and chronic load, training readiness, race predictions, endurance and hill score |
| 49 sleep | sleep stages, sleep assessment (scores). **No file yet**: Garmin Connect archived them, and the watch deletes archived health files (06) |
| 68 HRV | 5-minute HRV values, nightly HRV status. **No file yet**, for the same reason |
| 72 daily | about 100 nights of `DAILY_SLEEP`, a week of `SLEEP_SUMMARY` with stage minutes, HR minimum, naps, HRV stats |
| 74 | `USER_METRICS` (VO2max, age, weight, max HR) and 98 `PHYSIOLOGICAL_METRICS` records |
| 4 activity | `pkg/fitactivity`: one activity per `SESSION` (a multisport file has several), its `LAP`s and `RECORD`s, assigned by time (sessions meet at one second, which belongs to the next). The track is kept as gzipped JSON columns (t, lat, lon, alt, dist, speed, hr, cad, power, temp) plus a polyline of at most 500 points. Our 36 activity files: walks, runs, trail run, pool swims (up to 42 lengths), pilates, yoga, meditation and "Health Snapshot" (sport 60, which the public profile lacks): 201 laps, 25,837 records, 280 KB of compressed tracks |
| 77 suggested workouts | Garmin Coach's daily suggestions for the next 7 days: `WORKOUT` (26) and `WORKOUT_STEP` (27), and `WORKOUT_SCHEDULE` (428). The schedule entries give workout_index, est_benefit (3; 2 = base, 1 = recovery, 255 = rest day), sport, duration in ms and local_timestamp. On 2026-11-07: run "Base" 20:00 (136 bpm or 8:00/km) and bike "Base" 30:00, as the watch shows. M3b |
| not in any file | hydration (goal 3043 ml, nothing logged; this watch's `DEVICE_SETTINGS` has no hydration fields), sunrise/sunset (the watch computes them from position and date), challenges (Garmin's servers) |

## Monitoring (FIT 32)

- **`MONITORING` (55)**: every record has a full `timestamp` (253) or a `timestamp_16` (26).
  - `heart_rate` (27, bpm). 0 means no reading.
  - `cycles` (3), `distance` (2, cm), `active_calories` (19, kcal), `active_time` (4, ms):
    **running totals since local midnight, one per activity type**.
    - The activity type is `activity_type` (5), or else the low 5 bits of
      `current_activity_type_intensity` (24).
    - **The raw `cycles` value is the step count.** The FIT scale of 2 is undone by "×2",
      as in Gadgetbridge.
    - So a day's steps are the sum, over the activity types, of each type's largest total that
      day. This does not depend on file boundaries or file order.
  - `current_activity_type_intensity` (24): activity type in bits 0–4, intensity in bits 5–7.
  - `moderate_activity_minutes` (33), `vigorous_activity_minutes` (34): the minutes credited in the
    record.
  - `total_ascent` (35), `total_descent` (36): mm. They were 0 in every file so far.
  - The night files from the capture contain **no `cycles`, `distance` or calories**. The totals
    only appear when there are steps, so steps are still unchecked against the watch.
- **`MONITORING_INFO` (103)**: `local_timestamp` (0) − `timestamp` (253) is the watch's UTC offset.
  It was +3600 s (CET) on 2026-11-06.
- **`STRESS_LEVEL` (227)**, one a minute, timed by `stress_level_time` (1):
  - `stress_level_value` (0): 0–100; −1 means no value (moving, off the wrist);
  - `body_energy` (3) is **Body Battery** (0–100). It was 14–15 on the evening before the capture;
  - fields 2 and 4 are unknown (2 was 101 when 0 was −1).
- **`RESPIRATION_RATE` (297)**: breaths per minute ×100 (1885 = 18.85); −200 means no value.
- **`MONITORING_ALTITUDE` (279)**: altitude ×5 m.
- **Not decoded**: 24 (27-byte arrays that start with a timestamp), 233 (4 bytes), 355, 408, 484.
  Body Battery, stress and HR do not need them.

## Daily file (FIT 72)

- **`DAILY_SLEEP` (384)**, one per night, about 100 nights in one 8 KB file:
  - `sleep_start_time` (9), `sleep_end_time` (11), in UTC;
  - their UTC offsets (10, 12, minutes);
  - `sleep_score` (2), `min_heart_rate` (0);
  - Body Battery at bedtime (14) and wake-up (16), filled only for the latest nights;
  - `sleep_duration_score` (20), `sleep_recovery_score` (23), `sleep_quality_score` (25).
- **`SLEEP_SUMMARY` (411)**, one per night for the last 7 nights:
  - `sleep_score` (0);
  - minutes of deep (1), light (2), REM (3), awake (4) and unmeasurable (5) sleep;
  - start and end times in local time (9, 7) and UTC (10, 8).
- Example, the night to 2026-11-07: 04:35–10:17 local, score 78, deep 44, light 233, REM 65 min,
  minimum HR 55, Body Battery 26 → 89.
- `HR_MINIMUM` (431): `local_timestamp` (2) and `hr_min` (3).
- `NAP_EVENT` (412), `HRV_STATS` (401), with 28-day arrays, and 282, 383, 391, 413, 507, 513, 545
  are read but not used yet.
- The 384 records of one file skipped Oct 30–Nov 6. The sleep files (FIT 49) are the source for
  every night from now on.

## Other daily values

| Value | Message | Field | Note |
|---|---|---|---|
| VO2max | `USER_METRICS` (79) | `vo2_max` (0) | ÷292.571 ml/kg/min: 36.4 on our watch; `time_zone_offset` (23, minutes) |
| Training readiness | `TRAINING_READINESS` (369) | 0 | 86; `local_timestamp` (20) |
| Race predictions | `RACE_PREDICTION` (339) | 1–4 | seconds for 5 km, 10 km, half and full marathon (the full marathon was 28390 s) |
| Acute / chronic load | `TRAINING_LOAD` (378) | 3, 4 | |
| Training status | `LOAD_HISTORY` (232) | 2 | Garmin's enum |
| Endurance score, hill score | 403, 402 | 0 | |
| Resting HR | `MONITORING_HR_DATA` (211) | `current_day_resting_heart_rate` (1) | Not in any file yet (expected in daytime monitor files) |

## Checked against the watch (2026-11-07, about 11:30)

| On the watch | goarmin | Source |
|---|---|---|
| Steps 98, goal 3990 | 98, goal 3990 | `MONITORING.cycles`, summed over activity types; `MONITORING_INFO.cycles_goal` (103 field 7, one per activity type) |
| Sleep score 78, 5 h 42 min | 78, 5 h 42 min (04:35–10:17) | `DAILY_SLEEP`, `SLEEP_SUMMARY` (FIT 72) |
| Sleep coach 7 h 10 min | 430 min | `SLEEP_DEMAND.demand` (410 field 1, in the metrics file) |
| HRV last night (to be confirmed: 57 ms?) | 57 ms, range 46–60 | `HRV_STATS` (401, FIT 72): the last valid entries of the arrays in fields 2, 3 and 4 |
| Training Readiness 78 | 86 | `TRAINING_READINESS` (369) from 00:02, before the night. The watch had not yet written the value it recalculated after waking; it should arrive in the next metrics file |
| Pulse Ox 96 %, last reading 2025-09-16 | no SpO2 records | the watch takes spot readings only |

`HRV_STATS` (401) carries no UTC timestamp (field 253 is invalid); its `local_timestamp` (0) dates
the arrays, oldest night first, with unused slots set to 0xFFFF.

## Sleep (FIT 49) and HRV (FIT 68), as implemented but not yet seen on hardware

- `SLEEP_LEVEL` (275): stage from `timestamp` on.
  - Stages: 0 unmeasurable, 1 awake, 2 light, 3 deep, 4 REM.
  - `END_OF_FILE` (276) ends the last stage.
- `SLEEP_ASSESSMENT` (346) carries no time; goarmin assigns it to the file's night.
  - Fields: `overall_sleep_score` (6), `sleep_quality_score` (7), `sleep_duration_score` (4),
    `sleep_recovery_score` (8).
- `HRV_VALUE` (371): ms ×128.
- `HRV_STATUS_SUMMARY` (370):
  - weekly average (0), last night (1), the last night's 5-minute high (2);
  - baseline bounds (3–5);
  - status (6).
