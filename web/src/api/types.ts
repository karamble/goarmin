// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

export interface ServerInfo {
  version: string
  /** Something syncs watches: this server, or a sync node (live view, sync now). */
  sync: boolean
  /** Add a watch works here (this server pairs watches itself). */
  pair?: boolean
  tiles: boolean
  /** How to show the map tiles (only with tiles): dark already, and the credit (HTML). */
  map?: { dark: boolean; attribution: string }
}

export interface User {
  id: number
  email: string
  display_name: string
  timezone: string
  units: 'metric' | 'imperial'
  is_admin: boolean
  created_at: string
}

export interface Me extends User {
  server: ServerInfo
}

export interface SessionInfo {
  id: string
  created_at: string
  last_seen: string
  expires_at: string
  user_agent: string
  ip: string
  current: boolean
}

export interface PasskeyInfo {
  id: string
  name: string
  created_at: string
  last_used_at?: string
}

export interface InviteInfo {
  email: string
  expires_at: string
}

export interface CreatedInvite {
  url: string
  email: string
  expires_at: string
}

/** A local day's summary (GET /api/daily); every value may be missing. */
export interface Day {
  day: string
  steps?: number | null
  distance_m?: number | null
  active_kcal?: number | null
  ascent_m?: number | null
  descent_m?: number | null
  moderate_min?: number | null
  vigorous_min?: number | null
  hr_min?: number | null
  hr_max?: number | null
  hr_avg?: number | null
  resting_hr?: number | null
  stress_avg?: number | null
  stress_max?: number | null
  body_battery_min?: number | null
  body_battery_max?: number | null
  respiration_avg?: number | null
  spo2_avg?: number | null
  sleep_score?: number | null
  sleep_s?: number | null
  /** Once-a-day values by metric name (resting_hr, step_goal, sleep_need, training_readiness, …). */
  values: Record<string, number>
  hrv?: HrvNight
}

/** HRV status codes as the watch writes them. */
export const hrvStatus = ['None', 'Poor', 'Low', 'Unbalanced', 'Balanced'] as const

export interface HrvNight {
  weekly_avg_ms: number | null
  last_night_ms: number | null
  last_night_high_ms: number | null
  baseline_low_upper_ms: number | null
  balanced_lower_ms: number | null
  balanced_upper_ms: number | null
  status: number | null
}

export interface Metric {
  id: number
  name: string
  unit: string
  scale: number
  total: boolean
  daily: boolean
}

/** A chart-ready series (GET /api/series/{metric}); from/to is the span asked for. */
export interface Series {
  metric: string
  unit: string
  total: boolean
  from: number
  to: number
  bucket_s: number
  t: number[]
  v: number[]
  min?: number[]
  max?: number[]
}

/** A night's stages and overnight values (GET /api/sleep/{start}/stages). */
export interface NightStages {
  start: number
  end: number
  sleep_stage: Series
  heart_rate: Series
  stress: Series
  body_battery: Series
  respiration: Series
  hrv: Series
}

/** A night (GET /api/sleep); day is the day it ended. */
export interface Night {
  day: string
  start: number
  end: number
  score: number | null
  quality_score: number | null
  duration_score: number | null
  recovery_score: number | null
  min_hr: number | null
  body_battery_start: number | null
  body_battery_end: number | null
  deep_s: number | null
  light_s: number | null
  rem_s: number | null
  awake_s: number | null
  unmeasurable_s: number | null
}

export interface Activity {
  id: number
  start: number
  end: number
  day: string
  name: string
  sport: string
  sub_sport: string
  sport_label: string
  elapsed_s: number | null
  timer_s: number | null
  distance_m: number | null
  kcal: number | null
  avg_hr: number | null
  max_hr: number | null
  avg_speed: number | null
  max_speed: number | null
  avg_cadence: number | null
  avg_power: number | null
  ascent_m: number | null
  descent_m: number | null
  training_effect: number | null
  anaerobic_effect: number | null
  training_load: number | null
  polyline: string
  bounds?: [number, number, number, number]
}

export interface ActivityPage {
  items: Activity[]
  next_before?: number
}

/** The watch's own time in zones for an activity or lap. */
export interface ActivityZones {
  /** Six limits in bpm: the bottom of zone 1, then the top of zones 1–5. */
  hr?: number[]
  /** Seconds below zone 1, in zones 1–5, above the top. */
  hr_s?: number[]
  power?: number[]
  power_s?: number[]
  max_hr?: number
  resting_hr?: number
  threshold_hr?: number
  ftp?: number
  hr_calc?: string
}

/** Heart-rate zones as the watch sets them. */
export interface WatchZones {
  max_hr?: number
  threshold_hr?: number
  ftp?: number
  hr_calc?: string
  power_calc?: string
  /** Six limits in bpm, as ActivityZones.hr. */
  hr?: number[]
}

export interface Bike {
  name: string
  sport?: string
  sub_sport?: string
  odometer_m?: number
  weight_kg?: number
  wheel_mm?: number
  crank_mm?: number
  enabled: boolean
  sensors?: string[]
}

/** The watch's settings (GET /api/me/watch-profile). */
export interface WatchSettings {
  clock?: '24h' | '12h'
  date_mode?: string
  units: { distance?: string; speed?: string; elevation?: string; temperature?: string; weight?: string; height?: string }
  resting_hr?: number
  max_hr?: number
  hr_setting?: string
  /** Seconds after midnight, -1 unknown. */
  wake_s: number
  sleep_s: number
  run_step_m?: number
  walk_step_m?: number
  height_m?: number
  weight_kg?: number
  activity_level?: number
  athlete?: boolean
  zones?: WatchZones[]
  bikes?: Bike[]
}

export interface WatchGoal {
  type: string
  sport: string
  target: number
  unit: string
  source?: string
  recurrence?: string
}

export interface WatchProfile {
  settings: WatchSettings | null
  recorded_at?: number
  device_id?: number
  goals: WatchGoal[]
}

export interface SportZones {
  sport: string
  sub_sport: string
  name: string
  zones: WatchZones
  own: boolean
  effective?: WatchZones
  recorded_at: number
}

export interface ZonesInfo {
  default: WatchZones | null
  profiles: SportZones[]
}

export interface SportTotals {
  sport: string
  timer_s: number
  distance_m: number
  kcal: number
  sessions: number
}

export interface Totals {
  sports: SportTotals[]
  watches: number
  recorded_at: number
}

export interface WorkoutStep {
  index: number
  name?: string
  notes?: string
  intensity?: string
  duration: string
  duration_value?: number
  from?: number
  repeat?: number
  target?: string
  target_zone?: number
  target_low?: number
  target_high?: number
  target_pct?: boolean
  exercise?: { category: string; name: string; number: number; title?: string }
  weight_kg?: number
}

/** Where a stored workout came from: a watch's workout file, a Garmin Connect export, or
 * goarmin itself (a workout renamed or edited here). */
export type WorkoutOrigin = 'goarmin' | 'watch' | 'garmin_connect'

/** Where a library workout is, seen from one watch: on it now, removed from it (on the watch or
 * by Garmin Connect), never on it, or not known yet (the watch has not been checked). */
export type WorkoutPresence = 'on' | 'removed' | 'absent' | 'unknown'

export interface WorkoutWatch {
  device_id: number
  name: string
  state: WorkoutPresence
  /** on: when the watch last listed it; removed: when it first did not. */
  since?: number
  /** When the watch last listed its workouts (absent: not yet). */
  checked_at?: number
  /** Whether it belongs on this watch: the library's switch, or (never switched) whether the
   * watch has it. */
  want: boolean
  /** What the next sync does: send it, or nothing while it waits to be deleted on the watch
   * (remove; goarmin cannot delete from a watch). */
  pending?: 'send' | 'remove'
  /** Why the last try to send it failed. */
  failed?: string
  /** The watch holds only an older version of it. */
  outdated?: boolean
  /** Older versions the watch holds beside the current one, by name: they wait to be
   * deleted on the watch. */
  old_copies?: string[]
}

export interface WorkoutVersion {
  id: number
  origin: WorkoutOrigin
  device_id: number
  recorded_at: number
  step_count: number
}

/** A workout of the library: the version shown (a watch's if there is one), where its versions
 * came from, and on which of the account's watches it is. */
export interface Workout {
  id: number
  name: string
  sport: string
  sub_sport: string
  estimated_s: number
  step_count: number
  exercise_count: number
  recorded_at: number
  origin: WorkoutOrigin
  origins: WorkoutOrigin[]
  description?: string
  /** The version shown, for If-Match when it is edited. */
  tag?: string
  /** Hidden from the library (shown while a watch has it). */
  hidden?: boolean
  watches: WorkoutWatch[]
  steps?: WorkoutStep[]
  versions?: WorkoutVersion[]
}

export interface CoursePoint {
  type: string
  name?: string
  distance_m: number
  lat: number
  lon: number
}

export interface Course {
  id: number
  name: string
  sport: string
  distance_m: number
  ascent_m: number
  descent_m: number
  bounds?: [number, number, number, number]
  polyline: string
  point_count: number
  recorded_at: number
  points?: CoursePoint[]
}

/** One lap of an activity. */
export interface Lap {
  index: number
  start: number
  elapsed_s: number | null
  timer_s: number | null
  distance_m: number | null
  kcal: number | null
  avg_hr: number | null
  max_hr: number | null
  avg_speed: number | null
  max_speed: number | null
  avg_cadence: number | null
  avg_power: number | null
  ascent_m: number | null
  descent_m: number | null
  zones?: ActivityZones
}

/** An activity with its laps (GET /api/activities/{id}). */
export interface ActivityDetail extends Activity {
  laps: Lap[]
  zones?: ActivityZones
}

/** A recorded track as columns, one value per point (GET /api/activities/{id}/streams). */
export interface Streams {
  /** Unix seconds. */
  t: number[]
  lat?: (number | null)[]
  lon?: (number | null)[]
  /** Metres. */
  alt?: (number | null)[]
  /** Metres from the start. */
  dist?: (number | null)[]
  /** Metres per second. */
  speed?: (number | null)[]
  hr?: (number | null)[]
  cad?: (number | null)[]
  power?: (number | null)[]
  temp?: (number | null)[]
  fields?: string[]
}

/** The activities before and after one (GET /api/activities/{id}/neighbours). */
export interface Neighbours {
  prev: Activity | null
  next: Activity | null
}

/** Garmin Coach's suggestion for a day (GET /api/coach). */
export interface CoachDay {
  day: string
  index: number
  name: string
  description: string
  sport: string
  duration_s: number | null
  /** Garmin's training-benefit code; name already says it ("Base"). */
  benefit: number | null
  aerobic_te: number | null
  rest_day: boolean
}

/** A watch's sync state (times in Unix seconds). */
export interface SyncStatus {
  address: string
  running: boolean
  live: boolean
  last_start?: number
  last_end?: number
  last_ok: boolean
  /** When the last good sync started. */
  last_good?: number
  last_error?: string
  /** not_in_range, not_bonded, adapter_off, timeout, transfer, no_owner, import. */
  error_code?: string
  pulled: number
  imported: number
  next_sync?: number
  /** Real failures in a row; being out of range is not one (see away). */
  failures: number
  away: boolean
  away_since?: number
  /** While a sync runs: connecting, listing, fetching, importing. */
  phase?: string
  listed: number
  done: number
  total: number
  /** The sync node that syncs the watch (absent: this server), and whether it went silent. */
  node?: string
  offline?: boolean
  last_seen?: number
}

/** A sync node: a computer that syncs the watches near it for this server. */
export interface SyncNode {
  id: number
  name: string
  hostname: string
  user_id: number
  created_at: number
  last_seen?: number
  version: string
  revoked: boolean
  online: boolean
  adapter?: { name: string; powered: boolean; power_state: string; busy?: { kind: string; since: number } }
  watches: { address: string; unit: number; product: number; name: string; firmware: string }[]
}

export type AfterFetch = 'keep' | 'archive' | 'archive_delete'

/** What sort of device it is: only a watch syncs, streams live values or is paired. */
export type DeviceKind = 'watch' | 'scale' | 'bike_computer' | 'sensor' | 'handheld' | 'app' | 'other'

export interface Device {
  id: number
  unit_id: number
  product: number
  kind: DeviceKind
  /** The model ("Fenix 8"). */
  model: string
  /** The watch's own Bluetooth name ("fenix 8 - 47mm"). */
  name: string
  /** The owner's name for it ('' = name). */
  nickname: string
  firmware: string
  address: string
  after_fetch: AfterFetch
  paused: boolean
  /** Whether the watch deletes files on request: '' (not tried yet), 'yes', 'no'. */
  delete_support: '' | 'yes' | 'no'
  /** Last reported charge in percent, and when. */
  battery: number | null
  battery_at?: number
  /** Yours now; false for a watch you handed over (read-only). */
  current: boolean
  /** Your latest period with it, Unix seconds (owned_from 0 = from the start, owned_to null = still). */
  owned_from: number
  owned_to: number | null
  /** Imported by itself when it is plugged in by USB. */
  usb_auto: boolean
  /** Paired with this computer (only on a server that syncs watches). */
  paired?: boolean
  /** Only while it is yours and this server syncs it. */
  sync?: SyncStatus
}

/** A watch as admins see it: identity only. */
export interface AdminWatch {
  id: number
  unit_id: number
  model: string
  kind: DeviceKind
  name: string
  address: string
  owner: { id: number; email: string; name: string } | null
  paired: boolean
  last_end?: number
  last_ok?: boolean
  away?: boolean
}

/** One sync of a watch (GET /api/devices/{id}/sync-runs). */
export interface SyncRun {
  id: number
  started_at: number
  finished_at: number
  ok: boolean
  pulled: number
  archived: number
  deleted: number
  bytes: number
  imported: number
  error: string
}

/** A stored per-user setting; rev 0 means never saved. */
export interface Setting<T = unknown> {
  value: T | null
  rev: number
  updated_at?: string
}

/** A run in the sync log (GET /api/sync-runs). */
export interface LogRun extends SyncRun {
  device_id: number
  started_by: 'schedule' | 'manual' | 'wizard' | 'delete' | 'library'
  listed: number
  failed: number
  error_code: string
  /** Files the sync pushed to the watch (workouts). */
  sent?: number
}

export interface LogPage {
  items: LogRun[]
  next_before?: number
}

/** What came from a watch into the account (GET /api/devices/{id}/summary). */
export interface DeviceSummary {
  files: number
  bytes: number
  by_type: Record<string, number>
  activities: number
  nights: number
  days: number
  first_day: string
  last_day: string
  first_at: number
  last_at: number
}

/** A file goarmin fetched from a watch (GET /api/devices/{id}/files). */
export interface WatchFile {
  uid: string
  type: string
  fit_type: number
  size: number
  recorded: number
  fetched: number
  state: 'pending' | 'kept' | 'archived' | 'deleted' | 'gone'
  on_watch: boolean
  deletable: boolean
  /** The stored copy, for the inspector (absent when goarmin has none). */
  file_id?: number
}

/** This computer's Bluetooth adapter (GET /api/bluetooth). */
export interface BluetoothState {
  available: boolean
  error?: string
  adapter?: string
  address?: string
  alias?: string
  powered?: boolean
  power_state?: 'on' | 'off' | 'off-enabling' | 'on-disabling' | 'off-blocked'
  discovering?: boolean
  busy?: { kind: 'sync' | 'live' | 'pairing'; since: number } | null
}

/** A pairing session's state (Add a watch). */
export type PairState =
  | 'prepare' | 'waiting' | 'finding' | 'pairing' | 'confirm' | 'enter_passkey' | 'show_passkey' | 'identifying' | 'setup'
  | 'done' | 'failed' | 'canceled' | 'expired'

/** Who owns a listed watch, seen from the session's owner. */
export type PairOwner = 'yours' | 'other' | 'nobody' | 'new'

export interface PairWatch {
  address: string
  name: string
  rssi: number | null
  signal: 'strong' | 'ok' | 'weak' | null
  /** Advertising now. */
  seen_now: boolean
  /** Paired with this computer already. */
  paired_here: boolean
  owner: PairOwner
}

export interface PairPrompt {
  kind: 'confirm' | 'enter' | 'show'
  /** Six digits ('' when the watch shows the code and you type it here). */
  passkey: string
  /** Unix seconds. */
  deadline: number
}

export interface PairIdentified {
  unit: number
  product: number
  model: string
  name: string
  firmware: string
  battery: number | null
}

export interface PairAccount {
  id: number
  email?: string
  name?: string
}

/** A pairing session (GET /api/pairing/{id}); times in Unix seconds. */
export interface PairView {
  id: string
  state: PairState
  started_at: number
  expires_at: number
  owner: PairAccount
  error: string | null
  error_code: string | null
  /** The error can be retried from the list. */
  retry: boolean
  discovering: boolean
  watches: PairWatch[]
  selected: string | null
  prompt: PairPrompt | null
  steps: { name: 'connected' | 'link' | 'battery'; done: boolean }[]
  identified: PairIdentified | null
  ownership: 'new' | 'nobody' | 'yours' | null
  /** Another account had the watch before: its older files are theirs. */
  former_owner: boolean
  /** How the watch is paired, once the pairing step is over: the pairing this computer had, paired
   * just now with a code, or paired just now without one (the watch asked for none). */
  bond: 'existing' | 'code' | 'no_code' | null
  /** A watch that is yours already: its current settings. */
  nickname?: string
  after_fetch?: AfterFetch
  device_id: number | null
  /** The sync node that pairs (null: this server pairs itself). */
  node: { id: number; name: string } | null
  adapter: { name: string; powered: boolean; power_state: string; pairable: boolean } | null
  /** Who else holds Bluetooth while this session waits. */
  busy: { kind: 'sync' | 'live' | 'pairing'; since: number } | null
}

/** A computer that can pair a watch (GET /api/pairing/computers): this server, or a sync node. */
export interface PairComputer {
  /** The sync node's id; 0 for this server. */
  node: number
  name: string
  here: boolean
  adapter: { name: string; powered: boolean; power_state: string; busy: { kind: 'sync' | 'live' | 'pairing'; since: number } | null } | null
}

/** The running session as admins see it (GET /api/admin/pairing). */
export interface AdminPairing {
  id: string
  state: PairState
  started_at: number
  starter: PairAccount
  owner: PairAccount
}

/** What an import found (GET /api/imports/{id}). */
export interface ImportReport {
  /** FIT files found; new ones stored now, known ones already stored. */
  seen?: number
  new?: number
  known?: number
  duplicates?: number
  invalid?: number
  ingested?: number
  failed?: number
  waiting?: number
  /** FIT files not read: unchanged since the last USB import. */
  unchanged?: number
  /** A Garmin Connect export's other files, kept for later, and every such file by folder. */
  kept?: number
  kept_by?: Record<string, number>
  export?: boolean
  /** Files neither FIT nor part of an export, left out. */
  other?: number
  archives?: number
  bytes?: number
  /** Every file dealt with so far; found: how many the import knows of (a zip's nested archives
   * add theirs when they are opened; absent when it cannot tell); current: the path being read. */
  read?: number
  found?: number
  current?: string
  errors?: string[]
  errors_more?: number
}

export type ImportStatus = 'uploading' | 'queued' | 'running' | 'done' | 'failed' | 'canceled'

/** One import run. Times are Unix seconds. */
export interface ImportRun {
  id: number
  kind: 'upload' | 'usb'
  source: 'upload' | 'usb' | 'gc_export'
  name: string
  device_id: number | null
  started_by: 'manual' | 'plugged'
  status: ImportStatus
  created_at: number
  started_at: number | null
  finished_at: number | null
  report: ImportReport
  error: string
}

export interface ImportPage {
  items: ImportRun[]
  next_before: number | null
}

/** A Garmin watch plugged in at the server. */
export interface USBWatch {
  id: string
  name: string
  /** direct: goarmin reads it over USB; gvfs: through the desktop's mount; drive: a USB drive. */
  how: 'direct' | 'gvfs' | 'drive'
  port: string
  mounted: boolean
  owner: 'yours' | 'nobody' | 'other' | 'unknown'
  device: { id: number; model: string; name: string; nickname: string } | null
  /** Your watch only: imported when plugged in. */
  auto: boolean | null
  importing: ImportRun | null
  last: ImportRun | null
}

// What the readers store beyond days, nights and activities (M6b).

export interface WeighIn {
  ts: number
  day: string
  weight_kg: number
  fat_pct: number | null
  hydration_pct: number | null
  bone_kg: number | null
  muscle_kg: number | null
  visceral_fat_kg: number | null
  bmi: number | null
  bmr_kcal: number | null
  active_mr_kcal: number | null
  metabolic_age: number | null
  visceral_rating: number | null
  source: 'fit' | 'json'
}

export interface SkinTempNight {
  day: string
  ts: number
  nightly_c: number | null
  deviation_c: number | null
  deviation_7d_c: number | null
  calibrated_days: number | null
}

export interface Body {
  weigh_ins: WeighIn[]
  skin_temp: SkinTempNight[]
}

export interface ECGSummary {
  id: number
  start: number
  day: string
  class: string
  avg_hr: number | null
  rmssd_ms: number | null
  seconds: number | null
  raw_rate: number | null
  smooth_rate: number | null
  source: 'fit' | 'json'
}

export interface ECGRecording extends ECGSummary {
  samples: { raw: number[] | null; smooth: number[] | null } | null
}

export interface NapRow {
  start: number
  end: number
  day: string
  feedback: number | null
  how: number | null
}

export interface HealthStatusRow {
  day: string
  metric: string
  status: string
  value: number | null
  low: number | null
  high: number | null
  pct: number | null
}

export interface HydrationRow {
  ts: number
  day: string
  source: string
  ml: number
  sweat_ml: number
}

export interface PersonalRecordRow {
  source: 'fit' | 'json'
  metric: string
  sport: string
  ts: number
  value: number | null
  distance_m: number | null
  activity_id: number | null
  data: Record<string, unknown>
}

export interface GearRow {
  uuid: string
  source: 'fit' | 'json'
  type: string
  name: string
  brand: string
  model: string
  data: Record<string, unknown>
  distance_m?: number
  uses?: number
}

export interface SolarSeries {
  from: number
  to: number
  solar: { t: number[]; v: number[] }
  solar_gain: { t: number[]; v: number[] }
}

export interface SettingsChange {
  ts: number
  message: string
  field: string
  value: string
  kind: string
  sport: string
}

export interface DeviceDocument {
  kind: string
  recorded_at: number
  data: Record<string, Record<string, unknown>[]>
}

export interface StoredFile {
  id: number
  name: string
  source: string
  size: number
  received_at: number
  device_id: number | null
  fit_type: number | null
  type_name: string
  created: number | null
}

export interface FilePage {
  items: StoredFile[]
  next_before: number | null
}

export interface NamedField {
  num: number
  name: string
  units?: string
  value: unknown
  raw?: unknown
  known: boolean
}

export interface NamedMessage {
  num: number
  name: string
  known: boolean
  official: boolean
  fields: NamedField[]
}

export interface FileMessages {
  file: StoredFile
  counts: { num: number; name: string; count: number; known: boolean; official: boolean }[]
  messages: NamedMessage[]
  total: number
  offset: number
  error?: string
}

export interface AccountDocumentRow {
  id: number
  kind: string
  name: string
  recorded_at: number
}

export interface AccountDocument extends AccountDocumentRow {
  data: unknown
}

/** The kinds of data a calendar day can hold; the calendar draws one bar each. */
export type CalendarKind =
  | 'activity' | 'plan' | 'steps' | 'sleep' | 'hrv' | 'nap' | 'heart' | 'ecg' | 'stress' | 'body_battery' | 'weigh_in' | 'drink'

export interface CalendarActivity {
  id: number
  name: string
  start: number
  end: number
  sport: string
  sub_sport: string
  sport_label: string
  timer_s: number | null
  distance_m: number | null
  /** The workout it followed, '' if none. */
  workout: string
}

/** One of Garmin Coach's workouts on a calendar day, done by an activity of its sport. */
export interface CalendarCoach extends Omit<CoachDay, 'day'> {
  done: boolean
  missed: boolean
}

/** A library workout put on a day: done (with the activity), missed once the day is past, or
 * ahead. missing: the workout is no longer stored. */
export interface Plan {
  id: number
  day: string
  workout_id: number
  name: string
  sport: string
  sub_sport?: string
  estimated_s?: number
  step_count?: number
  note: string
  missing: boolean
  done_activity: number | null
  missed: boolean
}

export interface CalendarDay {
  day: string
  summary: Day | null
  kinds: Partial<Record<CalendarKind, number>>
  activities: CalendarActivity[]
  coach: CalendarCoach[]
  plans: Plan[]
}

export interface Calendar {
  from: string
  to: string
  today: string
  days: CalendarDay[]
}


/** MCP: tokens for AI assistants (Settings › MCP). */
export type McpScope = 'read' | 'write' | 'admin'

export interface McpToken {
  id: number
  name: string
  prefix: string
  scope: McpScope
  created_at: number
  expires_at: number | null
  last_used_at: number | null
  revoked_at: number | null
  live: boolean
  calls_24h: number
}

export interface McpCall {
  id: number
  token_id: number
  token: string
  at: number
  tool: string
  ok: boolean
  ms: number
  error: string
}

export interface McpItem {
  kind: 'tool' | 'resource' | 'prompt'
  name: string
  title: string
  description: string
  area: string
  level: McpScope
  destructive?: boolean
  read_only?: boolean
  uri?: string
  route?: string
}

export interface McpOverview {
  enabled: boolean
  endpoint: string
  secure: boolean
  tokens: McpToken[]
  calls: McpCall[]
  scopes: McpScope[]
  catalogue: McpItem[]
  retention_days: number
  limits: { calls_per_minute: number; destructive_per_minute: number }
}

export interface McpCreated {
  token: string
  item: McpToken
}
