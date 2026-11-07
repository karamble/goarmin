// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen, waitFor } from '@testing-library/react'
import { useNavigate } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Day, Night } from '../../api/types'
import { PeriodsContext, usePeriod } from '../../app/section/usePeriod'
import { addDays, today } from '../../lib/period'
import { fixtureMe, json, renderWith, stubApi, where } from '../../test/fixtures'
import Breathing from './Breathing'
import Energy from './Energy'
import Heart from './Heart'
import Hrv from './Hrv'
import Movement from './Movement'
import { dayClick } from './parts'
import Sleep from './Sleep'

afterEach(() => vi.unstubAllGlobals())

const day = today('UTC') // the test account's zone (fixtureMe timezone UTC), not the machine's
const now = Math.floor(Date.now() / 1000)
const from = now - 12 * 3600
const periods = ['day', 'week', 'month', 'year'] as const

function series(metric: string, v: number[], step = 300, start = from) {
  return { metric, unit: '', total: false, from: start, to: now, bucket_s: 0, t: v.map((_, i) => start + i * step), v }
}

function dayRow(d: string, over: Partial<Day> = {}): Day {
  return { day: d, values: {}, ...over }
}

const night: Night = {
  day, start: now - 9 * 3600, end: now - 2 * 3600, score: 78, quality_score: 80, duration_score: 70, recovery_score: 85, min_hr: 49,
  body_battery_start: 20, body_battery_end: 72, deep_s: 5400, light_s: 14400, rem_s: 5000, awake_s: 400, unmeasurable_s: 0,
}

function show(ui: React.ReactNode, path: string) {
  return renderWith(<PeriodsContext.Provider value={[...periods]}>{ui}</PeriodsContext.Provider>, { path, me: fixtureMe({ timezone: 'UTC' }) })
}

const img = (name: RegExp) => screen.findByRole('img', { name })

describe('health pages', () => {
  it('Sleep: a night with its stages and overnight values', async () => {
    const s = night.start
    stubApi({
      '/api/sleep': json([night]),
      '/api/daily': json([dayRow(day, { values: { sleep_need: 470 } })]),
      [`/api/sleep/${s}/stages`]: json({
        start: s, end: night.end,
        sleep_stage: { ...series('sleep_stage', [2, 3, 2, 4, 1], 3600, s) },
        heart_rate: series('heart_rate', [55, 52, 50], 1800, s), hrv: series('hrv', [50, 60], 1800, s),
        respiration: series('respiration', [14.2, 14.8], 1800, s), stress: series('stress', [12, 18], 1800, s),
        body_battery: series('body_battery', [20, 50], 1800, s),
      }),
    })
    show(<Sleep />, '/health/sleep')
    await img(/^Sleep score 78$/)
    expect(screen.getByText('Fell asleep')).toBeTruthy()
    expect(screen.getByText('need 7h 50m')).toBeTruthy()
    expect(screen.getByText('+52')).toBeTruthy()
    await img(/^Sleep stages: Awake .*REM 60 minutes.*Deep 60 minutes/)
    await img(/^Heart rate: minimum 50/)
    await img(/^HRV: minimum 50/)
  })

  it('Sleep: a week of nights, a year as a heatmap', async () => {
    const nights = [0, 1, 3].map((i) => ({ ...night, day: addDays(day, -i), start: night.start - i * 86400, end: night.end - i * 86400, score: 70 + i }))
    stubApi({ '/api/sleep': json(nights), '/api/daily': json([]) })
    const { unmount } = show(<Sleep />, '/health/sleep?p=week')
    await img(/^Sleep over \d nights: score from 70 to 7\d/)
    expect(screen.getByText('Average score')).toBeTruthy()
    expect(screen.getByRole('img', { name: /^In bed: lowest/ })).toBeTruthy()
    unmount()
    show(<Sleep />, '/health/sleep?p=year')
    await img(/^Sleep score on 3 days/)
  })

  it('says when a period has nothing', async () => {
    stubApi({ '/api/series/heart_rate': json(series('heart_rate', [])), '/api/daily': json([]), '/api/activities': json({ items: [] }) })
    show(<Heart />, `/health/heart?d=${addDays(day, -3)}`)
    await screen.findByText('No heart rate recorded on this day')
    expect(screen.getByText(/Older history arrives with the Garmin Connect import/)).toBeTruthy()
  })

  it('Heart: the day with activities, and a month of ranges', async () => {
    stubApi({
      '/api/series/heart_rate': json({ ...series('heart_rate', [60, 70, 140, 65]), min: [55, 60, 120, 60], max: [70, 80, 160, 70] }),
      '/api/daily': json([dayRow(day, { resting_hr: 54, hr_min: 50, hr_max: 160, hr_avg: 70 }), dayRow(addDays(day, -1), { resting_hr: 56, hr_min: 52, hr_max: 120 })]),
      '/api/activities': json({ items: [{ id: 1, day, start: from + 600, end: from + 1500, name: '', sport: 'running', sub_sport: 'generic', sport_label: 'Run' }] }),
    })
    const { unmount } = show(<Heart />, '/health/heart')
    await img(/^Heart rate: minimum 60/)
    await screen.findByText(/Shaded: your activities/)
    unmount()
    show(<Heart />, '/health/heart?p=month')
    await img(/^Heart rate: lowest 5\d, highest 160 bpm; Resting average 5\d/)
  })

  it('HRV: last night in its range, and nights over a week', async () => {
    const d = (i: number, v: number) => dayRow(addDays(day, -i), { values: { hrv_last_night: v, hrv_target_low: 46, hrv_target_high: 60 } })
    stubApi({ '/api/daily': json([d(0, 57), d(1, 65), d(2, 50)]), '/api/sleep': json([]) })
    const { unmount } = show(<Hrv />, '/health/hrv')
    await img(/^Last night 57 ms, balanced range 46 to 60 ms$/)
    unmount()
    show(<Hrv />, '/health/hrv?p=week')
    await img(/^HRV over \d nights: .*; 1 outside the balanced range$/)
  })

  it('Stress & Body Battery: the day and its stress levels', async () => {
    stubApi({
      '/api/series/body_battery': json(series('body_battery', [40, 60, 72, 65])),
      '/api/series/stress': json(series('stress', [10, 30, 60, 80, -1], 180)),
      '/api/daily': json([dayRow(day, { body_battery_max: 72, body_battery_min: 40, stress_avg: 35, stress_max: 80 })]),
      '/api/sleep': json([]),
    })
    show(<Energy />, '/health/energy')
    await screen.findByText('+32')
    expect(screen.getByText('−7')).toBeTruthy()
    await img(/^Rest 3m, Low 3m, Medium 3m, High 3m$/)
  })

  it('Breathing and Movement over a week', async () => {
    const days = [0, 1, 2].map((i) => dayRow(addDays(day, -i), { respiration_avg: 14 + i / 10, spo2_avg: 95, steps: 8000 + i * 1000, distance_m: 6000,
      values: { step_goal: 8500 } }))
    stubApi({ '/api/daily': json(days) })
    const { unmount } = show(<Breathing />, '/health/breathing?p=week')
    await img(/^Daily respiration: minimum 14.0/)
    await img(/^Daily SpO2: minimum 95/)
    unmount()
    show(<Movement />, '/health/movement?p=week')
    await img(/^Daily steps: minimum 8000, maximum 10000/)
    expect(screen.getByText('Goal reached')).toBeTruthy()
  })

  it('Movement: steps by hour and the goal ring', async () => {
    const start = Math.floor(Date.now() / 1000) - 5 * 3600
    stubApi({
      '/api/series/steps': json({ ...series('steps', [100, 200, 300], 1800, start), from: start, to: start + 24 * 3600, total: true }),
      '/api/daily': json([dayRow(day, { steps: 600, distance_m: 450, values: { step_goal: 8000 } })]),
    })
    show(<Movement />, '/health/movement')
    await img(/^600 steps of 8,000$/)
    await img(/^Steps per hour: minimum 0, maximum 300/)
  })

  it('opens a clicked day in the Day view, Back returns to the week', async () => {
    function Probe() {
      const p = usePeriod()
      const back = useNavigate()
      const ev = dayClick([addDays(day, -2), addDays(day, -1)], p.open)
      return (
        <>
          <button type="button" onClick={() => ev.click({ dataIndex: 1 })}>{p.period}</button>
          <button type="button" onClick={() => back(-1)}>Back</button>
        </>
      )
    }
    renderWith(<PeriodsContext.Provider value={[...periods]}><Probe /></PeriodsContext.Provider>, { path: '/health/sleep?p=week' })
    fireEvent.click(await screen.findByRole('button', { name: 'week' }))
    await waitFor(() => expect(where.path).toBe(`/health/sleep?d=${addDays(day, -1)}`))
    expect(screen.getByRole('button', { name: 'day' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    await screen.findByRole('button', { name: 'week' })
    const heat = dayClick([], () => {})
    expect(() => heat.click({ value: [day, 3] })).not.toThrow()
  })
})
