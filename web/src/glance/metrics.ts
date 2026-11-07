// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Day } from '../api/types'
import type { Units } from '../lib/format'

/** A daily value the trend card can chart. */
export interface DailyMetric {
  key: string
  label: string
  unit: (u: Units) => string
  digits: number
  get: (d: Day, u: Units) => number | null | undefined
}

const fixed = (s: string) => () => s
const miles = 1609.344

export const dailyMetrics: DailyMetric[] = [
  { key: 'steps', label: 'Steps', unit: fixed('steps'), digits: 0, get: (d) => d.steps },
  { key: 'resting_hr', label: 'Resting HR', unit: fixed('bpm'), digits: 0, get: (d) => d.values.resting_hr ?? d.resting_hr },
  { key: 'sleep_score', label: 'Sleep score', unit: fixed(''), digits: 0, get: (d) => d.sleep_score },
  { key: 'sleep', label: 'Sleep', unit: fixed('h'), digits: 1, get: (d) => (d.sleep_s == null ? null : d.sleep_s / 3600) },
  { key: 'hrv', label: 'HRV', unit: fixed('ms'), digits: 0, get: (d) => d.values.hrv_last_night ?? d.hrv?.last_night_ms },
  { key: 'stress', label: 'Stress', unit: fixed(''), digits: 0, get: (d) => d.stress_avg },
  { key: 'body_battery', label: 'Body Battery high', unit: fixed('%'), digits: 0, get: (d) => d.body_battery_max },
  { key: 'active_kcal', label: 'Active calories', unit: fixed('kcal'), digits: 0, get: (d) => d.active_kcal },
  { key: 'distance', label: 'Distance', unit: (u) => (u === 'imperial' ? 'mi' : 'km'), digits: 1,
    get: (d, u) => (d.distance_m == null ? null : d.distance_m / (u === 'imperial' ? miles : 1000)) },
  { key: 'intensity', label: 'Intensity minutes', unit: fixed('min'), digits: 0,
    get: (d) => (d.moderate_min == null && d.vigorous_min == null ? null : (d.moderate_min ?? 0) + 2 * (d.vigorous_min ?? 0)) },
  { key: 'respiration', label: 'Respiration', unit: fixed('brpm'), digits: 1, get: (d) => d.respiration_avg },
  { key: 'readiness', label: 'Training readiness', unit: fixed(''), digits: 0, get: (d) => d.values.training_readiness },
  { key: 'vo2max', label: 'VO2 max', unit: fixed('ml/kg/min'), digits: 1, get: (d) => d.values.vo2max },
]

export const metricByKey = new Map(dailyMetrics.map((m) => [m.key, m]))
