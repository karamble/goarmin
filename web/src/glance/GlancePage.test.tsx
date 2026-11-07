// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PeriodsContext } from '../app/section/usePeriod'
import { addDays, today } from '../lib/period'
import { fixtureMe, json, renderWith, stubApi } from '../test/fixtures'
import GlancePage from './GlancePage'

afterEach(() => vi.unstubAllGlobals())

const day = today()
const now = Math.floor(Date.now() / 1000)
const from = now - 6 * 3600

function series(metric: string, v: number[]) {
  return { metric, unit: '', total: false, from, to: now, bucket_s: 0, t: v.map((_, i) => from + i * 180), v }
}

function serve() {
  return stubApi({
    '/api/daily': json([
      { day: addDays(day, -1), values: { resting_hr: 61 }, steps: 9000 },
      { day, steps: 3990, distance_m: 2900, active_kcal: 120, stress_avg: 31, stress_max: 88,
        values: { resting_hr: 59, step_goal: 7980, sleep_need: 430, training_readiness: 78, hrv_last_night: 57, hrv_target_low: 46, hrv_target_high: 60 } },
    ]),
    '/api/series/body_battery': json(series('body_battery', [40, 60, 72, 65])),
    '/api/series/heart_rate': json(series('heart_rate', [58, 0, 70])),
    '/api/series/stress': json(series('stress', [20, -1, 40])),
    '/api/sleep': json([{ day, start: now - 12 * 3600, end: now - 6 * 3600, score: 78, deep_s: 3600, light_s: 10800, rem_s: 6120, awake_s: 600,
      quality_score: null, duration_score: null, recovery_score: null, min_hr: 50, body_battery_start: 20, body_battery_end: 72, unmeasurable_s: 0 }]),
    '/api/activities': json({ items: [{ id: 7, day: addDays(day, -1), start: now - 30 * 3600, end: now - 29 * 3600, name: '', sport: 'running',
      sub_sport: 'generic', sport_label: 'Run', timer_s: 1800, elapsed_s: 1850, distance_m: 5000, avg_hr: 140, polyline: '' }] }),
    '/api/coach': json([{ day, index: 0, name: 'Base', description: '8:00/km', sport: 'running', duration_s: 1200, benefit: 2, aerobic_te: 2.7, rest_day: false }]),
    '/api/devices': json([]),
  })
}

describe('GlancePage', () => {
  it('shows the day from every card', async () => {
    serve()
    renderWith(<PeriodsContext.Provider value={['day']}><GlancePage /></PeriodsContext.Provider>)
    const card = (name: string) => screen.findByRole('article', { name })
    await waitFor(async () => expect(within(await card('Body Battery')).getByText('65')).toBeTruthy())
    await within(await card('Sleep')).findByText('78')
    await within(await card('Steps')).findByText('3,990')
    await within(await card('HRV')).findByText('Within your range')
    await within(await card('Resting HR')).findByText('−2 vs your week')
    await within(await card('Training readiness')).findByText('High')
    await within(await card('Stress')).findByText(/Medium|Low/)
    await within(await card('Recent activities')).findByText('Run')
    expect((await within(await card('Garmin Coach')).findAllByText('20:00')).length).toBeGreaterThan(0)
    // Without the sync role there is no watch card.
    expect(screen.queryByRole('article', { name: 'Watch' })).toBeNull()
  })

  it('says when a past day has nothing', async () => {
    stubApi({ '/api/daily': json([]), '/api/sleep': json([]), '/api/series/body_battery': json(series('body_battery', [])),
      '/api/series/heart_rate': json(series('heart_rate', [])), '/api/series/stress': json(series('stress', [])),
      '/api/activities': json({ items: [] }), '/api/coach': json([]) })
    renderWith(<PeriodsContext.Provider value={['day']}><GlancePage /></PeriodsContext.Provider>, {
      path: `/?d=${addDays(day, -400)}`, me: fixtureMe(),
    })
    const sleep = await screen.findByRole('article', { name: 'Sleep' })
    await within(sleep).findByText('No night recorded')
    expect(within(sleep).queryByText(/next sync/)).toBeNull()
  })
})
