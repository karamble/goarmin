// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { CalendarDay, CalendarKind } from '../api/types'
import { distance, hm, int, type Units } from './format'
import { hrvOf } from './health'
import { addDays, periodRange, weekday } from './period'

/** monthCells is the month grid around day: six weeks from the week start before its 1st. */
export function monthCells(day: string): string[] {
  const first = day.slice(0, 8) + '01'
  const start = addDays(first, -weekday(first))
  return Array.from({ length: 42 }, (_, i) => addDays(start, i))
}

/** weekCells is the week around day, in the week's order. */
export function weekCells(day: string): string[] {
  const r = periodRange(day, 'week')
  return Array.from({ length: 7 }, (_, i) => addDays(r.from, i))
}

export interface KindInfo {
  label: string
  /** The bar's colour (a theme variable). */
  tone: string
}

/** The kinds in the order a day shows them, grouped by colour so the palette holds: training
 * lime, movement lime-dim, sleep and recovery cyan, heart pink, stress and energy amber, body grey. */
export const kinds: [CalendarKind, KindInfo][] = [
  ['activity', { label: 'Activities', tone: 'var(--lime)' }],
  ['plan', { label: 'Planned workouts', tone: 'var(--lime)' }],
  ['steps', { label: 'Steps', tone: 'var(--lime-dim)' }],
  ['floors', { label: 'Floors', tone: 'var(--lime-dim)' }],
  ['sleep', { label: 'Sleep', tone: 'var(--cyan)' }],
  ['hrv', { label: 'HRV', tone: 'var(--cyan)' }],
  ['nap', { label: 'Naps', tone: 'var(--cyan)' }],
  ['heart', { label: 'Heart rate', tone: 'var(--pink)' }],
  ['ecg', { label: 'ECG', tone: 'var(--pink)' }],
  ['stress', { label: 'Stress', tone: 'var(--amber)' }],
  ['body_battery', { label: 'Body Battery', tone: 'var(--amber)' }],
  ['weigh_in', { label: 'Weigh-ins', tone: 'var(--grey2)' }],
  ['drink', { label: 'Drinks', tone: 'var(--grey2)' }],
]

export const kindInfo = Object.fromEntries(kinds) as Record<CalendarKind, KindInfo>

/** An entry named on a wide day cell: an activity, or a plan (Garmin Coach's or the user's). */
export interface Named {
  key: string
  kind: 'activity' | 'plan'
  text: string
  state?: 'done' | 'missed' | 'ahead'
  /** An own plan's id (Coach entries have none and can't be moved). */
  plan?: number
}

/** A bar of a day: one kind (activities and plans one bar each), with its words for wide cells. */
export interface Bar {
  key: string
  kind: CalendarKind
  text: string
  /** A plan is hatched; a reached goal is solid, else the bar is faint. */
  hatched?: boolean
  faint?: boolean
  state?: Named['state']
}

/** sportShort is a sport in one word: running → run, cycling → ride. */
function sportShort(sport: string): string {
  const s = sport.toLowerCase()
  if (/run|walk|hik/.test(s)) return 'run'
  if (/cycl|bik/.test(s)) return 'ride'
  if (/swim/.test(s)) return 'swim'
  return s.replace(/_/g, ' ')
}

const planState = (done: boolean, missed: boolean): Named['state'] => (done ? 'done' : missed ? 'missed' : 'ahead')

/** dayEntries lays a calendar day out: what a wide cell names (activities, then plans), and the
 * bars of every shown kind (also activities and plans, one each, for narrow cells). */
export function dayEntries(d: CalendarDay, hidden: Set<CalendarKind>, units: Units): { named: Named[]; bars: Bar[] } {
  const named: Named[] = []
  const bars: Bar[] = []
  const s = d.summary
  const v = s?.values ?? {}
  if (!hidden.has('activity')) {
    for (const a of d.activities) {
      const what = [a.sport_label || a.name, a.distance_m ? distance(a.distance_m, units) : a.timer_s ? hm(a.timer_s) : ''].filter(Boolean).join(' ')
      named.push({ key: `a${a.id}`, kind: 'activity', text: what })
      bars.push({ key: `a${a.id}`, kind: 'activity', text: what })
    }
  }
  if (!hidden.has('plan')) {
    const coach = new Map<string, (typeof d.coach)[number]>()
    for (const c of d.coach) if (!c.rest_day && !coach.has(c.sport)) coach.set(c.sport, c)
    for (const c of coach.values()) {
      // Coach names a run and a ride alike ("Base"): the sport tells them apart.
      const text = [c.name || 'Garmin Coach', coach.size > 1 ? sportShort(c.sport) : ''].filter(Boolean).join(' · ')
      named.push({ key: `c${c.sport}`, kind: 'plan', text, state: planState(c.done, c.missed) })
      bars.push({ key: `c${c.sport}`, kind: 'plan', text, hatched: true, state: planState(c.done, c.missed) })
    }
    for (const p of d.plans) {
      const state = planState(p.done_activity != null, p.missed)
      named.push({ key: `p${p.id}`, kind: 'plan', text: p.name, state, plan: p.id })
      bars.push({ key: `p${p.id}`, kind: 'plan', text: p.name, hatched: true, state })
    }
  }
  const add = (kind: CalendarKind, text: string, extra: Partial<Bar> = {}) => {
    if (!hidden.has(kind) && d.kinds[kind]) bars.push({ key: kind, kind, text, ...extra })
  }
  const steps = s?.steps ?? 0
  const goal = v.step_goal ?? 0
  add('steps', `${int(steps)} steps`, { faint: goal > 0 && steps < goal })
  const floors = s?.floors_up ?? 0
  const floorsGoal = s?.floors_goal ?? 0
  add('floors', `${int(floors)} floor${floors === 1 ? '' : 's'}`, { faint: floorsGoal > 0 && floors < floorsGoal })
  add('sleep', s?.sleep_score != null ? `Sleep ${s.sleep_score}` : s?.sleep_s ? `Sleep ${hm(s.sleep_s)}` : 'Sleep')
  const hrv = hrvOf(s).value
  add('hrv', hrv != null ? `HRV ${Math.round(hrv)} ms` : 'HRV')
  add('nap', (d.kinds.nap ?? 0) > 1 ? `${d.kinds.nap} naps` : 'Nap')
  add('heart', s?.resting_hr != null ? `Resting ${s.resting_hr} bpm` : 'Heart rate')
  add('ecg', 'ECG')
  add('stress', s?.stress_avg != null ? `Stress ${s.stress_avg}` : 'Stress')
  add('body_battery', s?.body_battery_max != null ? `Body Battery ${s.body_battery_min ?? '–'}–${s.body_battery_max}` : 'Body Battery')
  add('weigh_in', v.weight != null ? `${v.weight.toFixed(1)} kg` : 'Weigh-in')
  add('drink', v.hydration != null ? `${(v.hydration / 1000).toFixed(1)} l` : 'Drinks')
  return { named, bars }
}

/** fitBars keeps what a cell has room for: the first n bars, and how many it leaves out. */
export function fitBars(bars: Bar[], n: number): { shown: Bar[]; more: number } {
  if (bars.length <= n) return { shown: bars, more: 0 }
  return { shown: bars.slice(0, n - 1), more: bars.length - (n - 1) }
}

/** planStateText says a plan's state in words. */
export function planStateText(state: Named['state']): string {
  return state === 'done' ? 'done' : state === 'missed' ? 'missed' : 'planned'
}
