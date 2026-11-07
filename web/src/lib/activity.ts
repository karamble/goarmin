// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Streams } from '../api/types'
import { clock, type Units } from './format'

/** How a sport's speed is read: pace per km/mi (on foot), per 100 m/yd (swimming), else speed. */
export type SpeedKind = 'pace' | 'swim' | 'speed'

export function speedKind(sport: string): SpeedKind {
  const s = sport.toLowerCase()
  if (s.includes('swim')) return 'swim'
  if (/run|walk|hik|trail|treadmill|mountaineer|snowshoe/.test(s)) return 'pace'
  return 'speed'
}

/** sportGroup names an activity's sport without its sub sport ("Swimming" for a pool swim), for
 * filters; a sport the FIT profile does not know keeps the activity's own label. */
export function sportGroup(a: { sport: string; sport_label: string }): string {
  if (!a.sport || a.sport.startsWith('SportInvalid')) return a.sport_label
  const s = a.sport.replace(/_/g, ' ')
  return s[0].toUpperCase() + s.slice(1)
}

/** cadenceFactor turns a FIT cadence into what Garmin shows: on foot the watch records strides
 * (one leg) per minute, shown as steps (×2); riding and swimming stay as recorded. */
export function cadenceFactor(sport: string): number {
  return speedKind(sport) === 'pace' ? 2 : 1
}

/** robustRange is a value axis that ignores the outer few percent (GPS jumps, standing still):
 * the q-th to the (1−q)-th percentile, widened by pad on both sides. */
export function robustRange(v: (number | null | undefined)[], q = 0.02, pad = 0.08): { min: number; max: number } | null {
  const xs = v.filter((x): x is number => x != null && Number.isFinite(x)).sort((a, b) => a - b)
  if (xs.length < 2) return null
  const lo = xs[Math.floor((xs.length - 1) * q)]
  const hi = xs[Math.ceil((xs.length - 1) * (1 - q))]
  const w = (hi - lo) * pad || Math.abs(hi) * pad || 1
  return { min: lo - w, max: hi + w }
}

/** niceAxis widens a range to multiples of the smallest step that splits it into at most n
 * parts, and returns that step as the axis interval. */
export function niceAxis(r: { min: number; max: number }, steps: readonly number[], n = 3): { min: number; max: number; interval: number } {
  const step = steps.find((s) => (r.max - r.min) / s <= n) ?? steps[steps.length - 1]
  return { min: Math.floor(r.min / step) * step, max: Math.ceil(r.max / step) * step, interval: step }
}

/** Axis steps: pace in seconds (clock times), speed in km/h or mph. */
export const paceSteps = [5, 10, 15, 20, 30, 60, 120, 300, 600, 1200] as const
export const speedSteps = [0.5, 1, 2, 2.5, 5, 10, 20, 50] as const

/** speedText shows a speed in m/s the way the sport reads it. */
export function speedText(mps: number | null | undefined, sport: string, u: Units): string {
  if (!mps || mps <= 0) return '–'
  switch (speedKind(sport)) {
    case 'pace':
      return `${clock((u === 'imperial' ? 1609.344 : 1000) / mps)} /${u === 'imperial' ? 'mi' : 'km'}`
    case 'swim':
      return `${clock((u === 'imperial' ? 91.44 : 100) / mps)} /100${u === 'imperial' ? 'yd' : 'm'}`
  }
  return u === 'imperial' ? `${(mps * 2.236936).toFixed(1)} mph` : `${(mps * 3.6).toFixed(1)} km/h`
}

/** speedValue is a speed for a chart: pace in seconds per km/mi/100 m (lower is faster), else
 * km/h or mph; null when standing still (a pace there would be infinite). */
export function speedValue(mps: number | null | undefined, kind: SpeedKind, u: Units): number | null {
  if (mps == null || mps <= 0.3) return null
  switch (kind) {
    case 'pace':
      return (u === 'imperial' ? 1609.344 : 1000) / mps
    case 'swim':
      return (u === 'imperial' ? 91.44 : 100) / mps
  }
  return u === 'imperial' ? mps * 2.236936 : mps * 3.6
}

export interface Split {
  n: number
  /** Metres covered in the split (the last one may be short). */
  distance: number
  /** Seconds. */
  time: number
  /** Metres per second. */
  speed: number
  hr: number | null
  /** Metres climbed. */
  ascent: number
}

/** splits cuts a track into whole kilometres (or miles), with the time, speed, average heart
 * rate and climb of each; a remainder of at least 50 m is the last split. */
export function splits(s: Streams, u: Units): Split[] {
  const dist = s.dist
  if (!dist || dist.length < 2) return []
  const unit = u === 'imperial' ? 1609.344 : 1000
  const out: Split[] = []
  let startT = s.t[0]
  let startD = 0
  let hrSum = 0
  let hrN = 0
  let climb = 0
  let lastAlt: number | null = null
  const close = (i: number, d: number) => {
    const time = s.t[i] - startT
    out.push({ n: out.length + 1, distance: d - startD, time, speed: time > 0 ? (d - startD) / time : 0, hr: hrN ? hrSum / hrN : null, ascent: climb })
    startT = s.t[i]
    startD = d
    hrSum = hrN = climb = 0
  }
  for (let i = 0; i < dist.length; i++) {
    const d = dist[i]
    const h = s.hr?.[i]
    if (h != null && h > 0) {
      hrSum += h
      hrN++
    }
    const a = s.alt?.[i]
    if (a != null) {
      if (lastAlt != null && a > lastAlt) climb += a - lastAlt
      lastAlt = a
    }
    if (d == null) continue
    while (d - startD >= unit) close(i, startD + unit)
  }
  const lastD = [...dist].reverse().find((x) => x != null)
  if (lastD != null && lastD - startD >= 50) close(dist.length - 1, lastD)
  return out
}

/** Heart-rate zones as shares of the maximum: zone 1 from 50 %, 2 from 60 %, … 5 from 90 %. */
export const zoneShares = [0.5, 0.6, 0.7, 0.8, 0.9] as const

/** zoneOf is a heart rate's zone 1–5 for maxHr (0 below zone 1). */
export function zoneOf(hr: number, maxHr: number): number {
  let z = 0
  zoneShares.forEach((s, i) => {
    if (hr >= s * maxHr) z = i + 1
  })
  return z
}

/** timeInZones is the time spent in zones 1–5 (seconds); each sample lasts until the next one,
 * at most capS (pauses do not count). */
export function timeInZones(t: number[], hr: (number | null)[] | undefined, maxHr: number, capS = 30): number[] {
  const out = [0, 0, 0, 0, 0]
  if (!hr || maxHr <= 0) return out
  for (let i = 0; i + 1 < t.length; i++) {
    const h = hr[i]
    if (h == null || h <= 0) continue
    const z = zoneOf(h, maxHr)
    if (z > 0) out[z - 1] += Math.min(t[i + 1] - t[i], capS)
  }
  return out
}

/** timeInLimits is the time spent in zones 1–5 with explicit limits (zone k from limits[k-1]);
 * each sample lasts until the next one, at most capS. */
export function timeInLimits(t: number[], hr: (number | null)[] | undefined, limits: number[], capS = 30): number[] {
  const out = [0, 0, 0, 0, 0]
  if (!hr || limits.length < 5) return out
  for (let i = 0; i + 1 < t.length; i++) {
    const h = hr[i]
    if (h == null || h <= 0 || h < limits[0]) continue
    let z = 0
    while (z < 4 && h >= limits[z + 1]) z++
    out[z] += Math.min(t[i + 1] - t[i], capS)
  }
  return out
}

/** buckets sorts values into n equal bands between their 5th and 95th percentile (so a few
 * spikes do not wash the colours out): the band of each point, or -1 for none. */
export function buckets(v: (number | null | undefined)[], n = 5): { band: number[]; edges: number[] } {
  const xs = v.filter((x): x is number => x != null && Number.isFinite(x)).sort((a, b) => a - b)
  if (!xs.length) return { band: v.map(() => -1), edges: [] }
  const lo = xs[Math.floor(xs.length * 0.05)]
  const hi = xs[Math.min(xs.length - 1, Math.floor(xs.length * 0.95))]
  const step = (hi - lo) / n || 1
  const edges = Array.from({ length: n + 1 }, (_, i) => lo + i * step)
  const band = v.map((x) => (x == null || !Number.isFinite(x) ? -1 : Math.max(0, Math.min(n - 1, Math.floor((x - lo) / step)))))
  return { band, edges }
}

/** nearestIndex is the index of the value in sorted xs closest to x. */
export function nearestIndex(xs: number[], x: number): number {
  if (!xs.length) return -1
  let lo = 0
  let hi = xs.length - 1
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (xs[mid] < x) lo = mid + 1
    else hi = mid
  }
  return lo > 0 && Math.abs(xs[lo - 1] - x) <= Math.abs(xs[lo] - x) ? lo - 1 : lo
}

/** routePoints is the track's positions with their point index (points without a fix left out). */
export function routePoints(s: Streams): { i: number; lat: number; lon: number }[] {
  const out: { i: number; lat: number; lon: number }[] = []
  s.lat?.forEach((lat, i) => {
    const lon = s.lon?.[i]
    if (lat != null && lon != null) out.push({ i, lat, lon })
  })
  return out
}
