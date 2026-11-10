// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Day, Night } from '../api/types'

/** Sleep stages as the watch writes them (FIT sleep_level). */
export const Stage = { unmeasurable: 0, awake: 1, light: 2, deep: 3, rem: 4 } as const

/** The stages people see, in hypnogram lane order (top to bottom), with their colours. */
export const stageInfo = [
  { code: Stage.awake, key: 'awake_s', label: 'Awake', color: 'var(--amber)' },
  { code: Stage.rem, key: 'rem_s', label: 'REM', color: 'var(--pink)' },
  { code: Stage.light, key: 'light_s', label: 'Light', color: 'var(--lime-dim)' },
  { code: Stage.deep, key: 'deep_s', label: 'Deep', color: 'var(--cyan)' },
] as const

export interface Segment {
  from: number
  to: number
  stage: number
}

/** stageSegments turns stage changes (Unix seconds, stage codes) into segments, each lasting
 * until the next change or the end of the night; repeats merge, unmeasured stretches are left out. */
export function stageSegments(t: number[], v: number[], end: number): Segment[] {
  const out: Segment[] = []
  for (let i = 0; i < t.length; i++) {
    const to = Math.min(i + 1 < t.length ? t[i + 1] : end, end)
    if (to <= t[i] || v[i] === Stage.unmeasurable) continue
    const last = out[out.length - 1]
    if (last && last.stage === v[i] && last.to === t[i]) last.to = to
    else out.push({ from: t[i], to, stage: v[i] })
  }
  return out
}

/** asleep is the time asleep in a night (deep + light + REM), null when the watch kept no stages. */
export function asleep(n: Pick<Night, 'deep_s' | 'light_s' | 'rem_s'>): number | null {
  if (n.deep_s == null && n.light_s == null && n.rem_s == null) return null
  return (n.deep_s ?? 0) + (n.light_s ?? 0) + (n.rem_s ?? 0)
}

/** hourlyTotals sums amounts per interval (a running total, as the series API sends it) into
 * the hours of the day from..to (23 or 25 of them on a DST day). */
export function hourlyTotals(s: { t: number[]; v: number[]; from: number; to: number }): number[] {
  const hours = Math.max(1, Math.round((s.to - s.from) / 3600))
  const out = new Array<number>(hours).fill(0)
  s.t.forEach((t, i) => {
    const h = Math.floor((t - s.from) / 3600)
    if (h >= 0 && h < hours) out[h] += s.v[i] ?? 0
  })
  return out
}

/** floorMM is one floor as a Garmin watch counts it: 10 ft (3.048 m) climbed. */
export const floorMM = 3048

/** hourlyFloors spreads a day's climb (an ascent series: metres per interval) over its hours as
 * whole floors, the way the watch counts them: an hour gets the floors its climb completes, so
 * the hours add up to the day's floors. */
export function hourlyFloors(s: { t: number[]; v: (number | null)[]; from: number; to: number }): number[] {
  const hours = Math.max(1, Math.round((s.to - s.from) / 3600))
  const out = new Array<number>(hours).fill(0)
  let mm = 0
  s.t.forEach((t, i) => {
    const before = Math.floor(mm / floorMM)
    mm += Math.round((s.v[i] ?? 0) * 1000)
    const h = Math.floor((t - s.from) / 3600)
    if (h >= 0 && h < hours) out[h] += Math.floor(mm / floorMM) - before
  })
  return out
}

/** chargeDrain sums the rises and the falls of a Body Battery series. */
export function chargeDrain(v: (number | null)[]): { charged: number; drained: number } {
  let charged = 0
  let drained = 0
  let prev: number | null = null
  for (const x of v) {
    if (x == null) continue
    if (prev != null) {
      if (x > prev) charged += x - prev
      else drained += prev - x
    }
    prev = x
  }
  return { charged: Math.round(charged), drained: Math.round(drained) }
}

export interface StressTime {
  rest: number
  low: number
  medium: number
  high: number
}

/** The stress levels, as Garmin names them. */
export const stressLevels = [
  { key: 'rest', label: 'Rest', max: 25, color: 'var(--faint)' },
  { key: 'low', label: 'Low', max: 50, color: 'var(--grey2)' },
  { key: 'medium', label: 'Medium', max: 75, color: 'color-mix(in srgb, var(--amber) 55%, var(--panel))' },
  { key: 'high', label: 'High', max: 100, color: 'var(--amber)' },
] as const

/** stressTime is how long stress was at rest (0–25), low, medium and high, in seconds. Each
 * sample lasts until the next one, at most maxS (time the watch did not measure is left out, as
 * are negative values: unmeasurable or active). */
export function stressTime(t: number[], v: (number | null)[], end: number, maxS = 300): StressTime {
  const out: StressTime = { rest: 0, low: 0, medium: 0, high: 0 }
  for (let i = 0; i < t.length; i++) {
    const x = v[i]
    if (x == null || x < 0) continue
    const d = Math.min((i + 1 < t.length ? t[i + 1] : end) - t[i], maxS)
    if (d <= 0) continue
    const level = x <= 25 ? 'rest' : x <= 50 ? 'low' : x <= 75 ? 'medium' : 'high'
    out[level] += d
  }
  return out
}

/** rolling is the average of the last n values (over those present), null where none are. */
export function rolling(v: (number | null)[], n = 7): (number | null)[] {
  return v.map((_, i) => {
    let sum = 0
    let k = 0
    for (let j = Math.max(0, i - n + 1); j <= i; j++) {
      const x = v[j]
      if (x != null) {
        sum += x
        k++
      }
    }
    return k ? sum / k : null
  })
}

/** localMinutes is the time of day of a Unix second in tz, as minutes after midnight. */
export function localMinutes(unix: number, tz = ''): number {
  let f: Intl.DateTimeFormat
  try {
    f = new Intl.DateTimeFormat('en-GB', { hour: '2-digit', minute: '2-digit', hourCycle: 'h23', timeZone: tz || undefined })
  } catch {
    f = new Intl.DateTimeFormat('en-GB', { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
  }
  const parts = f.formatToParts(unix * 1000)
  const h = Number(parts.find((p) => p.type === 'hour')?.value ?? 0)
  const m = Number(parts.find((p) => p.type === 'minute')?.value ?? 0)
  return h * 60 + m
}

/** eveningMinutes places a time of day on a night's axis: minutes after 18:00 (so 23:30 is 330
 * and 07:15 the next morning is 795). */
export function eveningMinutes(unix: number, tz = ''): number {
  return (localMinutes(unix, tz) - 18 * 60 + 1440) % 1440
}

/** clockOfEvening turns eveningMinutes back into "HH:MM". */
export function clockOfEvening(m: number): string {
  const x = (Math.round(m) + 18 * 60) % 1440
  return `${String(Math.floor(x / 60)).padStart(2, '0')}:${String(x % 60).padStart(2, '0')}`
}

export interface HrvDay {
  value: number | null
  low: number | null
  high: number | null
  weekly: number | null
  status: number | null
}

/** hrvOf is a day's HRV night: from the HRV status file when the watch wrote one, else from the
 * daily values (last night, the personal range). */
export function hrvOf(d: Pick<Day, 'hrv' | 'values'> | null | undefined): HrvDay {
  const h = d?.hrv
  const v = d?.values ?? {}
  return {
    value: h?.last_night_ms ?? v.hrv_last_night ?? null,
    low: h?.balanced_lower_ms ?? v.hrv_target_low ?? null,
    high: h?.balanced_upper_ms ?? v.hrv_target_high ?? null,
    weekly: h?.weekly_avg_ms ?? null,
    status: h?.status ?? null,
  }
}

/** intensityWeek is the week's intensity minutes up to day (vigorous count twice, as Garmin
 * does); days is the week's days. */
export function intensityWeek(days: Pick<Day, 'day' | 'moderate_min' | 'vigorous_min'>[], upTo: string): number {
  return days.filter((d) => d.day <= upTo).reduce((a, d) => a + (d.moderate_min ?? 0) + 2 * (d.vigorous_min ?? 0), 0)
}
