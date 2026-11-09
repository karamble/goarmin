# goarmin HTTP API

Every route the server answers, for scripts and other clients. The dashboard uses the same
API. Back to the [documentation index](README.md); how the pieces fit is in
[architecture.md](architecture.md).

`goarmin serve` runs the API and the embedded dashboard on one origin. With `--with-sync` it
also runs the sync role:
- it syncs this computer's watches on a schedule (`--sync-interval`, 30 min; after a failure
  sooner, backing off from 1 min);
- it imports what they pulled;
- it streams live values.

```
goarmin serve [-listen 127.0.0.1:8642] [-public-url https://health.example.org] [-with-sync] [-adapter hci0] [-fake-pairer] [-tile-url URL]
```

**Map tiles** (`-tile-url`, or `GOARMIN_TILE_URL`): a tile server's URL template with `{z}`,
`{x}` and `{y}` (and optionally `{s}`).
- **Serving:** goarmin fetches a tile the first time someone looks at it, with a browser
  User-Agent, and keeps it under `<data>/tiles` for 30 days. It serves tiles to signed-in users
  only as `GET /tiles/{s}/{z}/{x}/{y}.png` with `Cache-Control: private` (which tiles someone
  looks at shows where they go). It never preloads.
- **Without a tile server** there are no tiles (`server.tiles` is false) and routes are drawn
  on their own.
- **OpenStreetMap's own servers** (`tile.openstreetmap.org`) are refused. They require an
  identifying User-Agent, which goarmin never sends, and they answer a browser one with an
  "Access blocked" picture. Use a tile service whose terms allow server-side use (usually with a
  key in the URL), or a self-hosted tile server.
- The tile cache is the module `github.com/karamble/tilecache` (v0.1.0 in `go.mod`). To work
  on both at once, a local `go.work` with `use (. ../tilecache)` links a sibling checkout; it
  is git-ignored.

**Running it with make** (`make help` lists everything):

| Target | |
|---|---|
| `make start` / `make stop` / `make restart` | build and run `serve -with-sync` in the background on `LISTEN` (default `127.0.0.1:8642`); `stop` waits for a running sync to end (`FORCE=1` kills it after 60 s) and also stops a server started in a terminal |
| `make run` | the same in the foreground |
| `make status`, `make logs`, `make logs-follow`, `make health`, `make open` | the log is `~/.local/state/goarmin/serve.log` |
| `make backup` | a consistent snapshot of the database (SQLite online backup) with the raw files, the watch spool and `node.json`, as `~/goarmin-backups/goarmin_backup_YYYYMMDD_HHMMSS.tar.gz`; safe while the server runs |
| `make backup-list`, `make backup-clean` | `clean` removes archives older than `KEEP_DAYS` (30) and always keeps the newest `KEEP_MIN` (3) |
| `make restore BACKUP=…` | checks the archive, asks, stops the server, keeps the current data as `…goarmin.before-restore-…`, swaps the backup in and starts the server again |
| `make info`, `make list-users`, `make list-watches`, `make list-syncs`, `make days`, `make activities`, `make db-shell` | read-only views of the data |

One Bluetooth operation runs at a time. The server holds `<data>/ble.lock` while it syncs, streams
live values or pairs, and the commands `pair`, `sync`, `probe` and `live` refuse to start
meanwhile.

The Bluetooth bond with the watch lives in BlueZ (`/var/lib/bluetooth`), not in the data
directory, so a backup restored on another computer needs the watch paired there again.

Passkeys are bound to the host of `-public-url` (default `http://localhost:<port>`), and an
https URL makes the cookies `Secure`.

**First run.** While no account can sign in, `serve` prints a one-time link,
`<public-url>/setup?token=…`, that creates the first admin. An account made earlier by
`goarmin import` is claimed with its data when the setup uses the same email.

## Security rules
- Sessions are server-side.
  - The cookie `goarmin_session` is HttpOnly and SameSite=Lax, valid 30 days.
  - The database keeps only the SHA-256 of a session token; the same applies to invite tokens.
- Every write needs a same-origin `Origin` (or `Sec-Fetch-Site`); cross-site requests get 403.
- Passwords are argon2id, 12–1024 characters.
  - Logins are limited to 10 failures per address and account, and 50 per address, per 15 minutes.
  - An unknown account costs the same time as a wrong password.
- **Isolation.** Every data route takes the user from the session and passes it to the store.
  Another account's IDs answer 404, so even their existence is hidden. Admins manage accounts
  and invites but see nobody's health data.
- CSP `default-src 'self'`. The browser never contacts a third party: map tiles come from
  `/tiles` on the same origin.
- Errors are `{"error": "…"}`. Times are Unix seconds everywhere, local days `YYYY-MM-DD`.
- JSON, HTML, scripts, styles and fonts are gzipped when the browser accepts it; the live stream
  is not.
- Behind a reverse proxy, every request comes from the proxy's address: the per-address login
  limit then counts everyone together. goarmin trusts no forwarding headers.
- Outside `/api` and `/tiles`, only the dashboard's own routes (`/`, `/health…`, `/training…`,
  `/live`, `/devices…`, `/settings…`, `/login`, `/setup`, `/invite/…`) and its bundled files are
  served; any other path is a plain 404.

## Health checks

| Route | |
|---|---|
| `GET /healthz` | `ok` (text/plain), for container health checks and uptime probes: no session, no database, never logged. `goarmin healthcheck` asks it on this machine. |
| `GET /api/health` | `{"ok": true}` |

## Accounts

| Route | |
|---|---|
| `GET /api/setup` | `{needed}` |
| `POST /api/setup` | `{token, email, name, password}`, creates the first admin and signs in |
| `POST /api/auth/login` | `{email, password}` |
| `POST /api/auth/logout` | |
| `POST /api/auth/passkey/login/begin`, `…/finish` | discoverable passkey sign-in (no username) |
| `GET /api/me`, `POST /api/me/password` | `{current, new}`. `/api/me` also says what the server can do: `server: {version, sync, tiles}` |
| `PATCH /api/me` | `{display_name?, timezone?, units?}`; the time zone is an IANA name (or `""` for the server's), units `metric` or `imperial` |
| `GET /api/me/settings` | every stored setting: `{key: {value, rev, updated_at}}` |
| `GET/PUT/DELETE /api/me/settings/{key}` | keys `glance.layout` (16 KiB, at most 60 cards) and `prefs` (4 KiB). PUT takes the raw JSON; with `If-Match: "<rev>"` (`"0"` for never saved) it answers 412 if another device saved first. 413 too large, 400 wrong shape, 404 unknown key |
| `GET /api/me/sessions`, `DELETE /api/me/sessions/{id}` | sign other browsers out |
| `GET /api/me/passkeys`, `POST /api/me/passkeys/begin` `{name}` then `…/finish`, `DELETE /api/me/passkeys/{id}` | the last way to sign in cannot be removed |
| `POST /api/admin/invites` `{email?}` | returns a link valid for 7 days, optionally for one email |
| `GET /api/invites/{token}`, `POST /api/invites/{token}/accept` `{email, name, password}` | |
| `GET /api/admin/users` | |

## Health data (the signed-in user's own)

| Route | |
|---|---|
| `GET /api/metrics` | every series and daily value: name, unit, scale |
| `GET /api/daily?from&to` | daily summaries with the day's values (resting HR, HRV, sleep need, readiness, VO2max, …) and, where the watch wrote one, the night's `hrv` status (weekly average, last night, balanced range, status) |
| `GET /api/series/{metric}?from&to&max_points` or `?from_day&to_day&max_points` | values in the metric's unit; bucketed (avg, min, max) beyond max_points. Running totals (steps, distance, …) come as the amount per interval. `from_day`/`to_day` (`YYYY-MM-DD`, `to_day` defaults to `from_day`) are local days as the watch kept them, the same days `/api/daily` uses; the answer's `from`/`to` give the resolved span for the x-axis |
| `GET /api/sleep?from&to` | nights with scores, stages, Body Battery |
| `GET /api/sleep/{start}/stages` | the night's stages with its HR, stress, Body Battery, respiration and HRV |
| `GET /api/activities?before&limit` | keyset pages, newest first; `next_before`. Each has `sport`/`sub_sport` (FIT names) and a readable `sport_label` ("Pool swim", "Trail run"; for a sport the FIT profile does not know, the watch's own profile name, e.g. "Health Snapshot") |
| `GET /api/activities?from_day&to_day` | every activity recorded on those local days (at most 2000), newest first: `{items}`; 400 for a bad or reversed range |
| `GET /api/activities/{id}` | summary and laps |
| `GET /api/activities/{id}/neighbours` | the activities just before and after it: `{prev, next}`, each an activity or `null` |
| (in `GET /api/activities/{id}`) | `zones` (and each lap's `zones`) when the file has the watch's own time in zones: `{hr, hr_s, power?, power_s?, max_hr, resting_hr, threshold_hr, ftp, hr_calc}`. `hr` holds six limits: the bottom of zone 1, then the top of zones 1–5. `hr_s` holds the seconds below zone 1, in zones 1–5 and above the top |
| `GET /api/me/watch-profile` | what the user's watch keeps, from its newest settings file: `{settings, recorded_at, device_id, goals}`. `settings` is `{clock, date_mode, units, resting_hr, max_hr, hr_setting, wake_s, sleep_s, run_step_m, walk_step_m, height_m, weight_kg, activity_level, athlete, zones, bikes}`, or `null` before the watch sent one. `goals` is `[{type, sport, target, unit, source, recurrence}]` (steps, active_minutes in min/week, ascent in m, …) |
| `GET /api/me/zones` | heart-rate zones: `default` (the settings' first zone target: `{max_hr, threshold_hr, ftp, hr_calc, hr}`) and `profiles`, `[{sport, sub_sport, name, zones, own, effective, recorded_at}]`, one per sport profile. `effective` is the profile's own zones or else the default |
| `GET /api/totals` | the watches' lifetime totals per sport, summed over the user's watches: `{sports: [{sport, timer_s, distance_m, kcal, sessions}], watches, recorded_at}` |
| `GET /api/workouts`, `GET /api/workouts/{id}` | the workout library: every stored workout once (a watch's and Garmin Connect's versions of one, by name and sport, are one) `{id, name, sport, sub_sport, estimated_s, step_count, exercise_count, recorded_at, origin (watch, garmin_connect: the version shown), origins, description?, watches: [{device_id, name, state, since?, checked_at?}]}`. `state` per current watch: `on` (listed by its last inventory; `since` then), `removed` (listed before, not now; `since` when it was first missing), `absent`, `unknown` (the watch has not been checked; `checked_at` is its last inventory). The single workout (by the id of any version) adds `versions: [{id, origin, device_id, recorded_at, step_count}]` and `steps`: `[{index, name, notes, intensity, duration (time, distance, reps, calories, open, repeat, repeat_until_time, …), duration_value, from, repeat, target, target_zone, target_low, target_high, target_pct, exercise: {category, name, number, title}, weight_kg}]`. A `repeat` step runs the steps from `from` to itself `repeat` times; `repeat_until_time` runs them for `duration_value` seconds |
| `GET /api/courses`, `GET /api/courses/{id}`, `GET /api/courses/{id}/streams`, `GET /api/courses/{id}/fit` | saved courses `{id, name, sport, distance_m, ascent_m, descent_m, bounds, polyline, point_count, recorded_at}`. The single course adds `points`: `[{type, name, distance_m, lat, lon}]`. `streams` is the track as for activities; `fit` is the original file |
| `GET /api/activities/{id}/streams` | track columns (`t`, `lat`, `lon`, `alt`, `dist`, `speed`, `hr`, `cad`, `power`, `temp`), gzipped |
| `GET /api/activities/{id}/fit` | the original file |
| `GET /api/coach?from&to` | Garmin Coach's suggested workouts |
| `GET /api/calendar?from&to` | the calendar (at most 93 days): `{from, to, today, days: [{day, summary, kinds, activities, coach, plans}]}`, only days that hold anything. `summary` is the day as `/api/daily` serves it (or null); `kinds` counts what the day has: `activity`, `plan`, `steps`, `sleep`, `hrv`, `nap`, `heart`, `ecg`, `stress`, `body_battery`, `weigh_in`, `drink`. `activities: [{id, name, start, end, sport, sub_sport, sport_label, timer_s, distance_m, workout}]` (`workout`: the workout it followed, `''` if none). `coach`: Garmin Coach's workouts as `/api/coach` has them, plus `done` (an activity of their sport that day) and `missed`. `plans`: your own, as below |
| `POST /api/plans` `{day, workout_id, note?}` | puts a library workout (the id of any version) on a day (within five years of today; a note of at most 500 characters); 201 with the plan `{id, day, workout_id, name, sport, sub_sport, estimated_s, step_count, note, missing, done_activity, missed}`. A plan is done by an activity of that day that followed a workout of its name, else by one of its sport; missed once its day is past; `missing` when the workout is no longer stored. Another account's workout is 404 |
| `PATCH /api/plans/{id}` `{day?, note?}`, `DELETE /api/plans/{id}` | moves a plan or changes its note (its workout stays); removes it (204). Another account's plan is 404 |
| `GET /api/exercises` | the exercise catalogue: every exercise of Garmin's FIT profile with its 3D figure, `{categories: [{name, number, title, count}], exercises: [{key, category, category_num, number, name, title, quality (own, family, category), family, pose, mode (hold, loop)}]}`. The data is built into the server (`make figures`), so the answer carries an `ETag` and `Cache-Control: private, max-age=3600`; `If-None-Match` gets a 304 |
| `GET /api/exercises/{category}/{number}` | one exercise's figure: `{exercise, parts, keys}`. Each key pose is the root's position followed by a quaternion `[x, y, z, w]` per part in `parts` order, in glTF axes (Y up), for the dashboard's mannequin; a hold has one key, a loop several. 404 for an exercise the profile does not have |

## Watches

| Route | |
|---|---|
| `GET /api/devices` | the watches the user has or had, current ones first: `{id, unit_id, product, model, name, nickname, firmware, address, after_fetch, paused, delete_support, battery, battery_at, current, owned_from, owned_to, paired?, sync?}`. `paired` (with `--with-sync`) says whether this computer syncs it. `sync` (with `--with-sync`, only while the watch is theirs) is `{running, live, last_start, last_end, last_ok, last_error, error_code, pulled, imported, next_sync, failures, away, away_since, phase, listed, done, total}`: `phase` (connecting, listing, fetching, importing) and `done`/`total` show a running sync; a watch out of range is `away` and does not count as a failure |
| `PATCH /api/devices/{id}` | `{nickname?, after_fetch?, paused?, usb_auto?}`, each optional. `usb_auto` (default on) imports the watch when it is plugged in by USB. `nickname` is at most 60 characters (`""` = the watch's own name). `after_fetch`: `keep` is the default and never flags files; `archive` makes the watch delete health files once fetched; `archive_delete` also deletes activities. `paused` stops scheduled syncs (Sync now still works) |
| `POST /api/devices/{id}/sync` | sync now (202). An optional body `{include_synced: true}` also fetches what another app already synced (older activities still on the watch) |
| `GET /api/devices/{id}/summary` | what came from the watch into the user's account: `{files, bytes, by_type, activities, nights, days, first_day, last_day, first_at, last_at}` |
| `GET /api/devices/{id}/files` | the files goarmin fetched from the watch that are the user's, newest first: `[{uid, type, fit_type, size, recorded, fetched, state, on_watch, deletable}]`. `state` is `pending` (kept until archiving is on), `kept`, `archived` (health files then leave the watch; activities stay), `deleted` or `gone` |
| `POST /api/devices/{id}/files/delete` | `{uids: […]}` (at most 500): deletes those files from the watch at the next connection, which starts at once (202). They stay in goarmin. 409 if the watch is known not to delete on request |
| `POST /api/devices/{id}/unpair` | this computer forgets the watch (BlueZ removes the bond, `node.json` drops it). It stays the user's, with all its data |
| `POST /api/devices/{id}/release` | hand-over: the user's ownership ends now, the spool's fetched files go, and with `{unpair: true}` the pairing too. What it recorded until now stays the user's; whoever adds it next owns it from then on |
| `POST /api/devices/{id}/delete-data` | `{confirm}`: the watch's nickname, model or Bluetooth name. Deletes everything that came from it into the user's account (raw files and their copies, derived data, the user's syncs of it), re-imports overlapping files of other watches, rebuilds the days. Returns `{files, activities, nights, days, syncs}`. Also for a former owner |
| `GET /api/devices/{id}/raw.zip` | the user's original files from the watch |
| `GET /api/admin/devices` | admins: every watch with its owner (identity only), whether it is paired here, its last sync |
| `POST /api/admin/devices/{id}/release`, `…/forget` | admins: end the current owner's time with the watch; forget its pairing |
| `GET /api/sync-runs?device&problems&before&limit` | the sync log across the user's watches, newest first: `{items: [{id, device_id, started_by, started_at, finished_at, ok, listed, failed, pulled, archived, deleted, bytes, imported, error, error_code}], next_before?}`. `problems=1` keeps failed runs; `before` is the last id of the previous page |
| `GET /api/bluetooth` | this computer's adapter: `{available, adapter, address, alias, powered, power_state, discovering, busy: {kind, since}?}`. `power_state` is `off-blocked` when rfkill blocks it |
| `POST /api/bluetooth/power` | `{on: true}` switches the adapter on. Switching it off is not offered |
| `GET /api/devices/{id}/sync-runs` | the sync log: `[{id, started_at, finished_at, ok, pulled, archived, deleted, bytes, imported, error}]`, only the runs from the user's own time with the watch |
| `GET /api/devices/{id}/live` | Server-Sent Events, described below |

**Owners.** Every watch has ownership periods. Files a watch recorded belong to whoever owned it
then.
- After a hand-over, the previous owner still sees the watch (`current: false`) and their own
  syncs, but PATCH, sync and live answer 404.
- A watch nobody ever owned goes to the only account of a single-person server. On a shared
  server it waits, and its syncs fail with "nobody owns this watch now", until someone adds it.

## Sync nodes

A sync node is a computer that syncs the watches near it for this server (`goarmin sync
-server`). The dashboard manages them; the routes it uses:

| Route | |
|---|---|
| `GET /api/nodes` | your nodes (every node for an admin): `{id, name, hostname, user_id, created_at, last_seen, version, revoked, online, adapter: {name, powered, power_state, busy}?, watches: [{address, unit, product, name, firmware}]}` |
| `PATCH /api/nodes/{id}` `{name}` | renames a node |
| `DELETE /api/nodes/{id}` | signs it out: its token stops working at once (204) |
| `POST /api/nodes/approve` `{code}` | approves the code a node shows after `goarmin node login` without an e-mail; the node is then the signed-in account's |

A watch synced by a node carries `node` (its name), `offline` and `last_seen` in its `sync`
status; a sync request for a watch whose node is offline answers 503 with when it was last heard
from. `GET /api/me`'s `server.sync` is true once a node has been heard from; `server.pair` tells
whether *Add a watch* works here: the server pairs watches itself, or a sync node is online
([Adding a watch](#adding-a-watch-pairing)).

**The node API** (`/api/node/v1`, `internal/nodeproto`) is what nodes speak; they authenticate
with their token as `Authorization: Bearer`:

| Route | |
|---|---|
| `POST /login` `{email, password, name, hostname}` | signs a node in with the account's password (the sign-in limits apply) → `{node_id, token, account}` |
| `POST /enroll` `{name, hostname}` → `{code, poll, expires}`; `GET /enroll/{poll}` | a code approval: 202 while it waits, then `{token}` once |
| `POST /hello` `{version, hostname, adapter, watches}` | the node's paired watches → per watch `{device_id, sync, reason?, owner, after_fetch, paused, zone}`; `sync` is false for a watch nobody owns (`no_owner`) or another account's (`other_account`) |
| `PUT /files/{sha256}` (`X-Goarmin-Unit`, `X-Goarmin-Name`) | one pulled file: 200 once stored and ingested (also when it was there already), 403 for a watch the node may not sync, 409 for a wrong hash or a file recorded while nobody owned the watch |
| `POST /stored` `{unit, uids}` | which of a watch's items the server holds (deleting from the watch after the spool copy is gone) |
| `POST /runs`, `GET /history?unit=` | a finished sync (with battery and delete support); the last runs, for a node that starts |
| `POST /inventory` `{unit, types, at, items: [{uid, type, size}]}` | what the watch keeps of `types` (`FIT_TYPE_5`, its workouts) at the end of a sync, for the workout library; 204. A node treats 404 (a server from before the library) as nothing to do |
| `POST /progress` | the node's status per watch, its Bluetooth and who holds it |
| `GET /commands` (long poll, 25 s), `POST /commands/{id}` | commands (`sync`, `live_start`, `live_stop`, `files`, `forget`, `purge`, `power_on`, `hello`, and the pairing steps `pair_start`, `pair_scan`, `pair_select`, `pair_confirm`, `pair_passkey`, `pair_setup`, `pair_cancel`, `pair_touch`) and their results; a pairing step answers with the session (`pairing`) or an error code (`wrong_state`, `no_session`, `owned_elsewhere`, `not_listed`, `pairing_active`, `stopping`) |
| `POST /live` | a live link's readings, a batch a second, and how it ended |
| `POST /pairing` `{view}` | the node's pairing session after every change |
| `POST /pairing/owners` `{owner, watches: [{address, unit?}]}` | whose the listed watches are → `{flags}` (`yours`, `other`, `nobody`, `new` by address) |
| `POST /pairing/identified` `{owner, unit}` | whose the identified watch is → `{ownership, former_owner, nickname?, after_fetch?}` |
| `POST /pairing/claim` `{owner, address, record, battery, nickname, after_fetch}` | the Set up step: adds the watch to the account, routes it to the node → `{device_id, watch}` (its sync config); 409 when another account owns it |

The three pairing routes answer only within a session the node runs for that account (403
otherwise), and the claim only for the watch that session identified.

## Adding a watch (pairing)

*Add a watch* runs on a computer with Bluetooth: this server with `--with-sync`, or a sync node.
On a node the session runs there and the server relays it: every step below becomes a command to
the node, the node sends each change back, and whose a watch is (and adding it to an account) is
decided by the server. One session runs per server at a time, wherever it runs; only the account
that started it sees or drives it (an admin may cancel any).

| Route | |
|---|---|
| `GET /api/pairing/computers?owner=` | the computers that can pair a watch for the account (default you; another account for admins only): `[{node, name, here, adapter: {name, powered, power_state, busy}}]`, this server first (`here`, `node` 0), then the online sync nodes that may sync its watches |
| `POST /api/pairing` `{owner?, node?}` | starts a session (201). Your own running one comes back with 200; someone else's is 409. `owner` (an account id) lets an admin add a watch for someone else (403 otherwise, 404 for an unknown account). `node` pairs on that sync node; without it goarmin picks (this server, else a node with Bluetooth on). 409 when no computer can pair for the account, 503 when nothing here syncs watches or the node does not answer |
| `GET /api/pairing` | your session, running or ended in the last 10 minutes (404 if none), so a reload finds it again |
| `GET /api/pairing/{id}` | the session (404 for anyone but its starter) |
| `GET /api/pairing/{id}/events` | Server-Sent Events: `event: session` with the session at once and after every change, `: ping` every 15 s, `event: end` once it is over |
| `POST /api/pairing/{id}/scan` | takes Bluetooth (waiting for a running sync) and starts discovery; again to resume it after its 2 minutes |
| `POST /api/pairing/{id}/select` `{address, repair?}` | pairs with a listed watch; `repair` removes an existing pairing first. 409 for a watch of another account or one not listed |
| `POST /api/pairing/{id}/confirm` `{match}` | answers the code comparison |
| `POST /api/pairing/{id}/passkey` `{passkey}` | the code the watch shows, for watches that ask for one |
| `POST /api/pairing/{id}/setup` `{nickname, garmin_connect, include_synced}` | adds the identified watch to the account and starts its first sync. `garmin_connect: true` keeps files on the watch (keep), otherwise they are marked as synced (archive). `include_synced` is ignored when another account had the watch before |
| `DELETE /api/pairing/{id}`, `DELETE /api/admin/pairing` | cancel (yours; admins: whatever runs) |
| `GET /api/admin/pairing` | admins: the running session `{id, state, started_at, starter, owner}` (identity only), or 404 |

Actions answer with the session; a step that does not fit the current state is 409 with a
sentence to show.

**The session:** `{id, state, started_at, expires_at, owner: {id, email, name}, error, error_code,
retry, discovering, watches, selected, prompt, steps, identified, ownership, former_owner, bond,
nickname?, after_fetch?, device_id, node, adapter, busy}`. `node` is `{id, name}` of the sync
node that pairs, or null when this server does.
- `state`: `prepare` → `waiting` (for Bluetooth) → `finding` → `pairing` → `confirm` |
  `enter_passkey` | `show_passkey` → `identifying` → `setup` → `done`, or `failed`, `canceled`,
  `expired`. A failed pairing goes back to `finding` with `error`, `error_code` and `retry`.
- `watches`: `[{address, name, rssi, signal, seen_now, paired_here, owner}]`, strongest first.
  `signal` is strong, ok or weak; `owner` is `yours`, `other` (cannot be added; the account is
  never named), `nobody` (known here, waiting for an owner) or `new`.
- `prompt`: `{kind: confirm|enter|show, passkey, deadline}`. A question waits 30 s.
- `steps`: the identify checklist `[{name: connected|link|battery, done}]`; `identified`:
  `{unit, product, model, name, firmware, battery}`; `ownership`: `new`, `nobody` or `yours`
  (then `nickname` and `after_fetch` hold its current settings).
- `error_code`: `mismatch`, `timeout`, `auth_failed`, `connect_failed`, `gone`, `in_progress`,
  `adapter_off`, `not_bonded`, `identify`, `scan`, `busy` (a goarmin command holds Bluetooth),
  `owned_elsewhere`, `stopping`; through a node also `node_offline` (it went silent) and
  `node_gone` (it no longer has the session: restarted or signed out).
- `bond` once the pairing step is over: `existing` (the pairing this computer had is used),
  `code` (paired just now with a confirmed or typed code) or `no_code` (paired just now; the
  watch asked for no code). The server logs the same as `nodehub: paired`.
- `adapter` `{name, powered, power_state, pairable}` and `busy` `{kind, since}` (who else holds
  Bluetooth on the computer that pairs) while the session runs.

The session holds Bluetooth from the scan to the end of identify; scheduled syncs wait meanwhile,
and a live view answers 409. It ends after 3 minutes without anyone looking (no open event
stream, no GET; for a node's session the server tells the node every minute while someone
looks) or 10 minutes in all. Nothing is saved before setup: a cancelled session leaves
at most the Bluetooth pairing, and the next one lists the watch as paired here. A watch that turns
out to be another account's has a pairing made just now removed.

`GOARMIN_FIRST_SESSION=1` makes the first connection after a fresh pairing tell the watch its
setup is complete, as Garmin's apps do (off until tested on hardware). `serve -fake-pairer` (with
`-with-sync` and a scratch `-data` directory) replaces Bluetooth with two made-up watches, for
trying the wizard; `goarmin sync -server URL -fake-pairer` (a scratch `-data` directory) does the
same on a node.

`GET /api/bluetooth?node=` and `POST /api/bluetooth/power` `{on, node}` read and switch on a sync
node's Bluetooth (a node you may pair with; any for admins).

**The live stream.**
- **`event: hello`**, `data: {"since"}`, comes first: when the link opened (Unix seconds).
- **`event: reading`**, `data: {"name","value","unit","t"}`, follows for every value the watch
  sends:
  - `heart_rate`, `resting_hr`;
  - `steps`, `step_goal`;
  - `stress`, `body_battery`, `respiration`;
  - `spo2` (spot readings; `t` is the watch's own time);
  - `rr_interval` (beat to beat, ms);
  - `calories`, `active_calories`;
  - `floors_up`, `floors_down`, `floors_goal`;
  - `intensity_goal`.

  Some values only come when they change. The first readings are the link's last 10 minutes,
  so a viewer who joins later (a reload, a second tab) has the history at once.
- **`event: end`** says why the link ended: `data: {"reason"}`. `reason` is one of:
  - `left`: the viewers left;
  - `time_limit`: 30 minutes, so a forgotten page never keeps the watch from syncing; open it
    again to continue;
  - `stopping`: the server stops;
  - `closed`: the watch closed it;
  - `error`, with `error_code` (`not_in_range`, `not_bonded`, `adapter_off`, `timeout`,
    `transfer`) and `error`.
- The link opens with the first viewer, in about 10 s, and closes 30 s after the last viewer
  leaves.
- A sync and a live link never run at the same time. A scheduled sync waits for the next
  minute.
- **409** while something else holds Bluetooth: `{error, busy: {kind, since}}`. `kind` is
  `sync`, `pairing`, `live` (another watch; Bluetooth serves one at a time) or `command` (a
  goarmin command on this computer).

## Imports

Files reach goarmin outside a sync through imports: files uploaded from the browser, and
watches plugged in by USB. One import runs at a time, in the background; leaving the page does
not stop it. Every file goes through the same rules as a sync: stored once by its content, into
the account that owned the watch when the file was recorded. Importing the same files again
changes nothing.

| Route | |
|---|---|
| `POST /api/imports` | `multipart/form-data`, one part per file. A part's `filename` is its path in what was picked (`GARMIN/Activity/x.fit` for a folder), so folders stay recognisable. Takes `.fit` files, folders and `.zip` archives (nested ones too). The files are streamed to disk and the import is queued: **202** with the import. **413** beyond the limits (`serve -import-max-gb`, default 8 GB; 100,000 files), **415** when it is not multipart, **400** with no file in it. A refused upload is on record as failed |
| `GET /api/imports` | Your imports, newest first: `{items, next_before}`; `?before=<id>&limit=` (at most 100) |
| `GET /api/imports/{id}` | One import, with its progress while it runs |
| `DELETE /api/imports/{id}` | Cancels it: a queued import never runs, a running one stops after the file it is on (what it stored stays). **409** once it has finished, or while its files are still arriving |
| `GET /api/usb` | The Garmin watches plugged in at this server: `[{id, name, how, port, mounted, owner, device, auto, importing, last}]`. `how` is `direct` (goarmin reads MTP over USB), `gvfs` (through the desktop's mount, when a file manager holds the watch) or `drive` (a watch that is a USB drive). `owner` is `yours`, `nobody`, `other` or `unknown` (goarmin has not seen its files yet). Only your own watch shows `auto` and its `last` import; another account's shows no name |
| `POST /api/usb/{id}/import` | Imports that watch: **202** with the import, **200** with the one already running for it, **409** for another account's watch, **404** when it is not plugged in |

**An import** is `{id, kind, source, name, device_id, started_by, status, created_at, started_at,
finished_at, report, error}`:

- `kind`: `upload` or `usb`. `source`: what the files are stored as, `upload`, `usb` or
  `gc_export` (an upload that held a Garmin Connect export).
- `started_by`: `manual`, or `plugged` (the watch was plugged in and its owner has `usb_auto`).
- `status`: `uploading`, `queued`, `running`, `done`, `failed` or `canceled`.
- `report`: `{seen, new, known, duplicates, invalid, ingested, failed, waiting, unchanged, kept,
  kept_by, export, other, archives, bytes, errors, errors_more}`.
  - `seen` counts FIT files; `new` were stored now, `known` already were; `duplicates` are
    another copy of a stored recording.
  - `unchanged` FIT files were not read at all: a USB import found them as it left them.
  - `kept`: a Garmin Connect export's other files (its JSON), stored as they are for readers to
    come; `kept_by` counts them by the export's folder (`DI-Connect-Fitness`, …).
  - `other` files were neither FIT nor part of an export and were left out.
  - `errors` holds the first 200 problems, `errors_more` how many more there were.

**USB.** The server looks for Garmin watches (USB vendor `091e`) every 5 seconds and imports a
watch that appears for its owner when the owner has `usb_auto` on (`serve -no-usb` turns this
off). A watch goarmin does not know yet is only listed until someone imports it; its first
import teaches goarmin which watch reports that USB serial. Reading never changes anything on
the watch. A file that has not changed since the last USB import (same path, size and time, and
still stored for that account) is not read again. `goarmin usb` lists the watches on USB and can
copy a watch's GARMIN folder; `goarmin import -usb` imports one from the command line. Both take
`<data>/usb.lock`, which the server's USB imports share.

**Limits on archives.** At most 200,000 entries and 20 GiB uncompressed per import, 64 MiB per
file, archives nested three deep, and no entry compressed more than 200 times (over 1 MiB). An
archive beyond them is reported in `errors` and the rest goes on.

**Progress.** While an import runs, its `report` also carries `read` (files dealt with so far),
`found` (files the import knows of: a zip's nested archives add theirs when they are opened;
absent when it cannot tell) and `current` (the path being read).

## Devices of every kind

`GET /api/devices` lists watches, scales and other devices alike; each has `kind`: `watch`,
`scale`, `bike_computer`, `sensor`, `handheld`, `app` or `other`. What only a watch does answers
409 for another kind ("this device is a scale, not a watch"): `POST …/sync`, `GET …/live`,
`GET …/files`, `POST …/files/delete`, `POST …/unpair`, `POST …/release {"unpair": true}` and
`PATCH` of `after_fetch`, `paused` or `usb_auto`. A nickname can be set on any device.

| Route | Answers |
|---|---|
| `GET /api/devices/{id}/solar` | Solar charge in hourly buckets: `{from, to, solar: {t, v}, solar_gain: {t, v}}` (percent, battery ms gained). `?from&to` (days), else the device's last 7 days with data |
| `GET /api/devices/{id}/changes?before=` | The watch's settings change log, newest first, 100 a page: `{items: [{ts, message, field, value, kind, sport}], next_before}` |
| `GET /api/devices/{id}/documents` | What its settings, device and multisport files hold beyond the tables: `[{kind, recorded_at, data: {message: [fields…]}}]` |

## Files and the inspector

| Route | Answers |
|---|---|
| `GET /api/files?type=&device=&before=&limit=` | The account's stored files, newest first (copies left out): `{items: [{id, name, source, size, received_at, device_id, fit_type, type_name, created}], next_before}`. `type` is a FIT file type or `other` (not FIT) |
| `GET /api/files/{id}/messages?num=&offset=&limit=` | A FIT file decoded on request: `{file, counts: [{num, name, count, known, official}], messages: [{num, name, known, official, fields: [{num, name, units, value, raw, known}]}], total, offset}`; values scaled, times as RFC 3339, enumerations by name, unknown messages and fields by number. `error` when the file breaks off. 422 for a file that is not FIT |
| `GET /api/files/{id}/download` | The file as it arrived |

Another account's file is 404.

## More health and training data

| Route | Answers |
|---|---|
| `GET /api/naps?from&to` | `[{start, end, day, feedback, how}]` |
| `GET /api/body?from&to` | `{weigh_ins: [{ts, day, weight_kg, fat_pct, hydration_pct, bone_kg, muscle_kg, visceral_fat_kg, bmi, bmr_kcal, active_mr_kcal, metabolic_age, visceral_rating, source}], skin_temp: [{day, ts, nightly_c, deviation_c, deviation_7d_c, calibrated_days}]}`. A scale's own weigh-in wins over the export's record of it |
| `GET /api/ecg`, `GET /api/ecg/{id}` | ECG recordings `{id, start, day, class, avg_hr, rmssd_ms, seconds, raw_rate, smooth_rate, source}`; one with `samples: {raw, smooth}` |
| `GET /api/physiology?from&to` | Physiological metrics after activities: VO2 max, lactate threshold, max HR, recovery, training effects, race times |
| `GET /api/health-status?from&to` | Garmin Connect's health status per day and metric: `{day, metric, status, value, low, high, pct}` |
| `GET /api/hydration?from&to` | Logged drinks `{ts, day, source, ml, sweat_ml}` |
| `GET /api/snapshots?from&to` | Health snapshots `{start, end, day, name, data}` |
| `GET /api/records` | Personal records `{source, metric, sport, ts, value, distance_m, activity_id, data}`: Garmin Connect's (times in s, distances in m) and the watch's own; `activity_id` is goarmin's |
| `GET /api/gear` | Gear `{uuid, source, type, name, brand, model, data, distance_m?, uses?}`; Garmin Connect's with the distance of the activities it was used in |
| `GET /api/golf-clubs` | The watch's golf clubs |
| `GET /api/account/documents`, `GET /api/account/documents/{id}` | The account's records from a Garmin Connect export (profile, settings, orders, consents, inReach, …): a list of `{id, kind, name, recorded_at}`, one with `data` |

`/api/daily` values gain the export's and the new readers' kinds (weight, body_fat, bmi,
skin_temp_deviation, recovery_time, acwr, heat_acclimation, fitness_age, hydration, the daily
summaries' day_* values, …; `/api/metrics` lists them all). A watch's own value of a day wins
over the export's. Activities carry `extra`: what only Garmin Connect knows about them.
