// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { LiveEnd, Reading } from '../api/live'

/** A value's recent history: times (Unix s) and values, oldest first. */
export interface Series {
  t: number[]
  v: number[]
}

export const emptySeries: Series = { t: [], v: [] }

/** The values a live page keeps a history of. */
export const tracked = ['heart_rate', 'stress', 'body_battery', 'respiration', 'spo2', 'steps', 'calories', 'active_calories', 'floors_up'] as const

/** addReadings adds readings to the histories (in time order; a repeat of the last point is
 * skipped) and drops what is older than keepS before the newest. */
export function addReadings(hist: Record<string, Series>, rs: Reading[], keepS: number): Record<string, Series> {
  const out: Record<string, Series> = { ...hist }
  const touched = new Set<string>()
  for (const r of rs) {
    if (!(tracked as readonly string[]).includes(r.name)) continue
    if (r.name === 'heart_rate' && r.value <= 0) continue
    let s = out[r.name]
    if (!touched.has(r.name)) {
      s = { t: [...(s?.t ?? [])], v: [...(s?.v ?? [])] }
      out[r.name] = s
      touched.add(r.name)
    }
    const n = s.t.length
    if (n && s.t[n - 1] === r.t && s.v[n - 1] === r.value) continue
    if (n && r.t < s.t[n - 1]) {
      // A replayed point that arrives late: put it in its place.
      let i = n - 1
      while (i >= 0 && s.t[i] > r.t) i--
      s.t.splice(i + 1, 0, r.t)
      s.v.splice(i + 1, 0, r.value)
    } else {
      s.t.push(r.t)
      s.v.push(r.value)
    }
  }
  for (const name of touched) {
    const s = out[name]
    const from = s.t[s.t.length - 1] - keepS
    const cut = s.t.findIndex((t) => t >= from)
    if (cut > 0) out[name] = { t: s.t.slice(cut), v: s.v.slice(cut) }
  }
  return out
}

/** cleanBeats drops beat intervals that cannot be a heart beat (300–2000 ms) and artefacts that
 * differ from the previous kept beat by more than 20 %. */
export function cleanBeats(rr: number[]): number[] {
  const out: number[] = []
  for (const x of rr) {
    if (x < 300 || x > 2000) continue
    const prev = out[out.length - 1]
    if (prev != null && Math.abs(x - prev) > prev * 0.2) continue
    out.push(x)
  }
  return out
}

/** rmssd is the root mean square of successive differences over the last n clean beats (the
 * short-term HRV a watch shows), or null with fewer than 10 beats. */
export function rmssd(rr: number[], n = 60): number | null {
  const b = cleanBeats(rr).slice(-n)
  if (b.length < 10) return null
  let sum = 0
  for (let i = 1; i < b.length; i++) sum += (b[i] - b[i - 1]) ** 2
  return Math.sqrt(sum / (b.length - 1))
}

/** stressLevel is Garmin's word for a stress value: rest 0–25, low 26–50, medium 51–75, high 76–100. */
export function stressLevel(v: number): { label: string; tone: 'cyan' | 'grey' | 'amber' | 'red' } {
  if (v <= 25) return { label: 'Rest', tone: 'cyan' }
  if (v <= 50) return { label: 'Low', tone: 'grey' }
  if (v <= 75) return { label: 'Medium', tone: 'amber' }
  return { label: 'High', tone: 'red' }
}

/** summary is the lowest, average (by time) and highest value of a series. */
export function summary(s: Series): { min: number; avg: number; max: number } | null {
  if (!s.v.length) return null
  let min = Infinity
  let max = -Infinity
  let area = 0
  let span = 0
  for (let i = 0; i < s.v.length; i++) {
    min = Math.min(min, s.v[i])
    max = Math.max(max, s.v[i])
    if (i + 1 < s.v.length) {
      const d = Math.min(s.t[i + 1] - s.t[i], 30) // a gap counts at most 30 s
      area += s.v[i] * d
      span += d
    }
  }
  return { min, max, avg: span ? area / span : s.v.reduce((a, b) => a + b, 0) / s.v.length }
}

/** gained is how much a running total grew within the series (steps or calories while live). */
export function gained(s: Series): number | null {
  if (s.v.length < 1) return null
  return Math.max(0, s.v[s.v.length - 1] - s.v[0])
}

/** endText says why a live link ended, in words for the page. */
export function endText(e: LiveEnd | null): string {
  switch (e?.reason) {
    case 'left':
      return 'The live link closed.'
    case 'time_limit':
      return 'Live paused after 30 minutes so the watch can sync.'
    case 'stopping':
      return 'The server is restarting.'
    case 'closed':
      return 'The watch closed the link.'
    case 'error':
      switch (e.error_code) {
        case 'not_in_range':
          return 'The watch is not in range of this computer (or Bluetooth is off on the watch).'
        case 'not_bonded':
          return 'The watch is no longer paired with this computer. Pair it again under Devices.'
        case 'adapter_off':
          return "This computer's Bluetooth is off. Turn it on under Devices."
        case 'timeout':
          return 'The watch did not answer in time.'
      }
      return e.error ? `The link failed: ${e.error}` : 'The link failed.'
  }
  return 'The link ended.'
}
