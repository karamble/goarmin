// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

export interface ServerInfo {
  version: string
  sync: boolean
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

export interface Workout {
  id: number
  name: string
  sport: string
  sub_sport: string
  estimated_s: number
  step_count: number
  recorded_at: number
  steps?: WorkoutStep[]
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
}

export type AfterFetch = 'keep' | 'archive' | 'archive_delete'

export interface Device {
  id: number
  unit_id: number
  product: number
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
  started_by: 'schedule' | 'manual' | 'wizard' | 'delete'
  listed: number
  failed: number
  error_code: string
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
  adapter: { name: string; powered: boolean; power_state: string; pairable: boolean } | null
  /** Who else holds Bluetooth while this session waits. */
  busy: { kind: 'sync' | 'live' | 'pairing'; since: number } | null
}

/** The running session as admins see it (GET /api/admin/pairing). */
export interface AdminPairing {
  id: string
  state: PairState
  started_at: number
  starter: PairAccount
  owner: PairAccount
}
