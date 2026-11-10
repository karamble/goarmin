// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { afterEach, describe, expect, it } from 'vitest'
import type { CalendarDay } from '../api/types'
import { dayEntries, fitBars, monthCells, weekCells } from './calendar'
import { addDays, setWeekStart, today, weekday } from './period'

afterEach(() => setWeekStart(1))

const day = today('UTC')

function cal(over: Partial<CalendarDay> = {}): CalendarDay {
  return {
    day, kinds: {}, activities: [], coach: [], plans: [],
    summary: { day, steps: 7305, floors_up: 9, floors_goal: 10, values: { step_goal: 8000, weight: 75.24 }, sleep_score: 79, sleep_s: 23160, resting_hr: 59, stress_avg: 24,
      body_battery_min: 17, body_battery_max: 77 },
    ...over,
  }
}

describe('calendar', () => {
  it('lays a month out in six weeks from the week start', () => {
    for (const ws of [1, 0, 6]) {
      setWeekStart(ws)
      const cells = monthCells(day)
      const first = day.slice(0, 8) + '01'
      expect(cells).toHaveLength(42)
      expect(weekday(cells[0])).toBe(0)
      expect(cells).toContain(first)
      expect(cells.indexOf(first)).toBeLessThan(7)
      expect(cells[41]).toBe(addDays(cells[0], 41))
      const week = weekCells(day)
      expect(week).toHaveLength(7)
      expect(week).toContain(day)
      expect(weekday(week[0])).toBe(0)
    }
  })

  it('names activities and plans and gives the other kinds bars with their numbers', () => {
    const d = cal({
      kinds: { activity: 1, plan: 2, steps: 1, floors: 1, sleep: 1, heart: 1, stress: 1, body_battery: 1, weigh_in: 1 },
      activities: [{ id: 3, name: 'Morning', start: 0, end: 0, sport: 'running', sub_sport: 'generic', sport_label: 'Run', timer_s: 1800, distance_m: 5200, workout: '' }],
      coach: [
        { index: 0, name: 'Base', description: '', sport: 'running', duration_s: 2400, benefit: 2, aerobic_te: 2.5, rest_day: false, done: true, missed: false },
        { index: 1, name: 'Base', description: '', sport: 'running', duration_s: 2400, benefit: 2, aerobic_te: 2.5, rest_day: false, done: true, missed: false },
      ],
      plans: [{ id: 9, day, workout_id: 4, name: 'Tempo', sport: 'running', note: '', missing: false, done_activity: null, missed: true }],
    })
    const e = dayEntries(d, new Set(), 'metric')
    expect(e.named.map((n) => [n.kind, n.text, n.state, n.plan])).toEqual([
      ['activity', 'Run 5.20 km', undefined, undefined],
      ['plan', 'Base', 'done', undefined], // Coach lists one workout twice: once here
      ['plan', 'Tempo', 'missed', 9],
    ])
    const bars = e.bars.filter((b) => b.kind !== 'activity' && b.kind !== 'plan')
    expect(bars.map((b) => b.text)).toEqual(['7,305 steps', '9 floors', 'Sleep 79', 'Resting 59 bpm', 'Stress 24', 'Body Battery 17–77', '75.2 kg'])
    expect(bars[0].faint).toBe(true) // below the step goal
    expect(bars[1].faint).toBe(true) // and below the floors goal
    expect(dayEntries(cal({ kinds: { floors: 1 }, summary: { day, floors_up: 12, floors_goal: 10, values: {} } }), new Set(), 'metric').bars)
      .toMatchObject([{ kind: 'floors', text: '12 floors', faint: false }])
    expect(e.bars.filter((b) => b.kind === 'plan').every((b) => b.hatched)).toBe(true)

    const hidden = dayEntries(d, new Set(['activity', 'plan', 'steps']), 'metric')
    expect(hidden.named).toEqual([])
    expect(hidden.bars.map((b) => b.kind)).toEqual(['floors', 'sleep', 'heart', 'stress', 'body_battery', 'weigh_in'])
    expect(dayEntries(d, new Set(['floors']), 'metric').bars.some((b) => b.kind === 'floors')).toBe(false)
  })

  it('fits bars into a cell with a count of the rest', () => {
    const bars = Array.from({ length: 7 }, (_, i) => ({ key: `${i}`, kind: 'sleep' as const, text: '' }))
    expect(fitBars(bars, 5)).toMatchObject({ more: 3 })
    expect(fitBars(bars, 5).shown).toHaveLength(4)
    expect(fitBars(bars.slice(0, 5), 5)).toMatchObject({ more: 0 })
  })
})
