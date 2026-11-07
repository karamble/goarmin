// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import type { Activity, CoachDay } from '../api/types'
import { addDays, today } from './period'
import {
  change, distinctWorkouts, latest, loadRatio, optimalBand, planWeek, ratioBand, readinessLevel, sameSport, statusOf, statusRuns,
} from './training'

const day = today()

function act(d: string, sport: string, id: number): Activity {
  return { id, day: d, sport, sub_sport: 'generic', sport_label: sport, name: '', start: 0, end: 0 } as unknown as Activity
}

function coach(d: string, index: number, over: Partial<CoachDay> = {}): CoachDay {
  return { day: d, index, name: 'Base', description: '8:00/km', sport: 'running', duration_s: 1200, benefit: 2, aerobic_te: 2.8, rest_day: false, ...over }
}

describe('training', () => {
  it('names statuses, including a stored 255 and unknown values', () => {
    expect(statusOf(7)).toMatchObject({ label: 'Productive', tone: 'lime', confirmed: true })
    expect(statusOf(255)).toMatchObject({ value: -1, label: 'Paused' })
    expect(statusOf(3)).toMatchObject({ label: 'Overreaching', confirmed: false })
    expect(statusOf(42)?.label).toBe('Status 42')
    expect(statusOf(null)).toBeNull()
  })

  it('reads readiness and the load ratio as Garmin does', () => {
    expect([10, 30, 60, 80, 99].map(readinessLevel)).toEqual(['Poor', 'Low', 'Moderate', 'High', 'Prime'])
    expect(loadRatio(400, 0)).toBeNull()
    expect(loadRatio(450, 300)).toBe(1.5)
    expect([0.7, 0.8, 1.4, 1.5, 1.99, 2].map((r) => ratioBand(r).label)).toEqual(['Low', 'Optimal', 'Optimal', 'High', 'High', 'Very high'])
    expect(optimalBand([100, null])).toEqual({ lo: [80, null], hi: [150, null] })
  })

  it('finds the latest value and the change over a period', () => {
    expect(latest([null, 50, 51.5, null])).toEqual({ i: 2, value: 51.5 })
    expect(latest([null])).toBeNull()
    expect(change([null, 50, null, 52.5])).toBe(2.5)
    expect(change([50, null])).toBeNull()
  })

  it('folds days into status runs', () => {
    const days = [0, 1, 2, 3, 4].map((i) => addDays(day, i - 4))
    const runs = statusRuns(days, [4, 4, null, 7, 7])
    expect(runs.map((r) => [r.from, r.to, r.n, r.status?.label ?? null])).toEqual([
      [days[0], days[1], 2, 'Maintaining'], [days[2], days[2], 1, null], [days[3], days[4], 2, 'Productive'],
    ])
  })

  it('matches activities to planned sports and groups listed targets', () => {
    expect(sameSport('running', 'running')).toBe(true)
    expect(sameSport('running', 'walking')).toBe(true)
    expect(sameSport('cycling', 'running')).toBe(false)
    expect(sameSport('', 'swimming')).toBe(true)

    const days = [0, 1, 2].map((i) => addDays(day, i))
    const plan = [
      coach(days[0], 0), coach(days[0], 1, { description: '136bpm' }), coach(days[0], 2, { sport: 'cycling', description: '140W', duration_s: 1800 }),
      coach(days[1], 3, { name: '', description: '', sport: '', duration_s: 0, rest_day: true }),
      coach(days[2], 4),
    ]
    const week = planWeek(days, plan, [act(days[0], 'running', 1), act(days[0], 'strength_training', 2), act(days[2], 'swimming', 3)])
    expect(week[0].done.map((a) => a.id)).toEqual([1])
    expect(week[0].other.map((a) => a.id)).toEqual([2])
    expect(week[1]).toMatchObject({ rest: true, workouts: [] })
    expect(week[2].done).toEqual([])
    expect(distinctWorkouts(week[0].workouts)).toEqual([
      { sport: 'running', name: 'Base', duration_s: 1200, targets: ['8:00/km', '136bpm'], aerobic_te: 2.8 },
      { sport: 'cycling', name: 'Base', duration_s: 1800, targets: ['140W'], aerobic_te: 2.8 },
    ])
  })
})
