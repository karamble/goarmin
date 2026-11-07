// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Activity, CoachDay, Day } from '../api/types'

/** Garmin's training-readiness bands. */
export function readinessLevel(v: number): string {
  return v < 25 ? 'Poor' : v < 50 ? 'Low' : v < 75 ? 'Moderate' : v < 95 ? 'High' : 'Prime'
}

export type Tone = 'lime' | 'grey' | 'dim' | 'amber' | 'red' | 'cyan' | 'pink'

export interface StatusInfo {
  value: number
  label: string
  tone: Tone
  /** What the status means, in a sentence. */
  note: string
  /** Gadgetbridge has seen this value on a watch (the others are its predictions). */
  confirmed: boolean
}

// Gadgetbridge's TrainingStatus (model/TrainingStatus.kt); 0, 2, 3 and 6 are its predictions.
const statuses: Record<number, Omit<StatusInfo, 'value'>> = {
  [-1]: { label: 'Paused', tone: 'grey', confirmed: true, note: 'Training is paused: illness, injury or a break.' },
  0: { label: 'No status', tone: 'grey', confirmed: false, note: 'Not enough recent activities with heart rate for a status.' },
  1: { label: 'Detraining', tone: 'dim', confirmed: true, note: 'Training has been much lighter than usual for a week or more.' },
  2: { label: 'Unproductive', tone: 'amber', confirmed: false, note: 'The load is fine, but fitness is going down: recovery may be short.' },
  3: { label: 'Overreaching', tone: 'red', confirmed: false, note: 'The load is very high and counterproductive: time to recover.' },
  4: { label: 'Maintaining', tone: 'grey', confirmed: true, note: 'The load keeps fitness where it is.' },
  5: { label: 'Recovery', tone: 'cyan', confirmed: true, note: 'A lighter load lets the body recover.' },
  6: { label: 'Peaking', tone: 'lime', confirmed: false, note: 'Ideal race condition: fitness is high and the load is going down.' },
  7: { label: 'Productive', tone: 'lime', confirmed: true, note: 'The load is moving fitness in the right direction.' },
  8: { label: 'Strained', tone: 'pink', confirmed: true, note: 'Recovery is not keeping up: HRV is unbalanced or sleep short.' },
}

/** statusOf names Garmin's training status (a stored 255 is -1, Paused). */
export function statusOf(raw: number | null | undefined): StatusInfo | null {
  if (raw == null) return null
  const v = raw > 127 ? raw - 256 : raw
  const s = statuses[v]
  return s ? { value: v, ...s } : { value: v, label: `Status ${v}`, tone: 'grey', confirmed: false, note: 'A status goarmin does not know yet.' }
}

export const toneVar: Record<Tone, string> = {
  lime: 'var(--lime)', grey: 'var(--grey2)', dim: 'var(--faint)', amber: 'var(--amber)', red: 'var(--red)', cyan: 'var(--cyan)', pink: 'var(--pink)',
}

/** loadRatio is acute over chronic load (Garmin's acute:chronic workload ratio). */
export function loadRatio(acute: number | null | undefined, chronic: number | null | undefined): number | null {
  if (acute == null || !chronic) return null
  return acute / chronic
}

/** ratioBand is how Garmin reads the load ratio: below 0.8 low, up to 1.4 optimal, up to 1.9
 * high, from 2.0 very high. */
export function ratioBand(r: number): { label: string; tone: Tone } {
  if (r < 0.8) return { label: 'Low', tone: 'cyan' }
  if (r < 1.5) return { label: 'Optimal', tone: 'lime' }
  if (r < 2) return { label: 'High', tone: 'amber' }
  return { label: 'Very high', tone: 'red' }
}

/** optimalBand is the acute load that keeps the ratio optimal, from the chronic load. */
export function optimalBand(chronic: (number | null)[]): { lo: (number | null)[]; hi: (number | null)[] } {
  return { lo: chronic.map((c) => (c == null ? null : c * 0.8)), hi: chronic.map((c) => (c == null ? null : c * 1.5)) }
}

/** valuesOf is one daily value for each of the days (null where the day has none). */
export function valuesOf(days: string[], byDay: Map<string, Day>, key: string): (number | null)[] {
  return days.map((d) => byDay.get(d)?.values[key] ?? null)
}

/** latest is the last value in v with its index, or null. */
export function latest(v: (number | null)[]): { i: number; value: number } | null {
  for (let i = v.length - 1; i >= 0; i--) if (v[i] != null) return { i, value: v[i]! }
  return null
}

/** change is the last value minus the first one (null with fewer than two values). */
export function change(v: (number | null)[]): number | null {
  const xs = v.filter((x): x is number => x != null)
  return xs.length < 2 ? null : xs[xs.length - 1] - xs[0]
}

/** Race distances in metres, for the paces of the predictions. */
export const races = [
  { key: 'race_5k', label: '5K', metres: 5000 },
  { key: 'race_10k', label: '10K', metres: 10000 },
  { key: 'race_half', label: 'Half marathon', metres: 21097.5 },
  { key: 'race_marathon', label: 'Marathon', metres: 42195 },
] as const

/** statusRuns folds consecutive days with the same status into runs (for the timeline). */
export function statusRuns(days: string[], v: (number | null)[]): { from: string; to: string; n: number; status: StatusInfo | null }[] {
  const out: { from: string; to: string; n: number; status: StatusInfo | null }[] = []
  v.forEach((raw, i) => {
    const s = statusOf(raw)
    const last = out[out.length - 1]
    if (last && (last.status?.value ?? null) === (s?.value ?? null)) {
      last.to = days[i]
      last.n++
    } else {
      out.push({ from: days[i], to: days[i], n: 1, status: s })
    }
  })
  return out
}

/** sameSport says whether an activity counts for a planned workout's sport. */
export function sameSport(planned: string, done: string): boolean {
  if (!planned) return true
  const fam = (s: string) => (/run|walk|hik/.test(s) ? 'run' : /cycl|bik/.test(s) ? 'bike' : /swim/.test(s) ? 'swim' : s)
  return fam(planned.toLowerCase()) === fam(done.toLowerCase())
}

export interface PlanDay {
  day: string
  workouts: CoachDay[]
  rest: boolean
  /** Activities that day matching a planned workout's sport. */
  done: Activity[]
  /** Other activities that day. */
  other: Activity[]
}

/** planWeek lays Garmin Coach's plan and the activities over the week's days. A day with a
 * plan has its workouts; Garmin Coach lists one workout several ways (by pace, heart rate,
 * power), so a day counts as done when any activity of a planned sport was recorded. */
export function planWeek(days: string[], plan: CoachDay[], acts: Activity[]): PlanDay[] {
  return days.map((day) => {
    const all = plan.filter((x) => x.day === day)
    const workouts = all.filter((x) => !x.rest_day)
    const mine = acts.filter((a) => a.day === day)
    const done = mine.filter((a) => workouts.some((w) => sameSport(w.sport, a.sport)))
    return { day, workouts, rest: all.length > 0 && workouts.length === 0, done, other: mine.filter((a) => !done.includes(a)) }
  })
}

/** distinctWorkouts groups a day's listed workouts by sport: Garmin Coach lists the same
 * session with a pace target and with a heart-rate target (running), or power and heart rate
 * (cycling). */
export function distinctWorkouts(ws: CoachDay[]): { sport: string; name: string; duration_s: number | null; targets: string[]; aerobic_te: number | null }[] {
  const out = new Map<string, { sport: string; name: string; duration_s: number | null; targets: string[]; aerobic_te: number | null }>()
  for (const w of ws) {
    const key = `${w.sport}|${w.name}|${w.duration_s}`
    const have = out.get(key)
    if (have) {
      if (w.description) have.targets.push(w.description)
    } else {
      out.set(key, { sport: w.sport, name: w.name, duration_s: w.duration_s, targets: w.description ? [w.description] : [], aerobic_te: w.aerobic_te })
    }
  }
  return [...out.values()]
}
