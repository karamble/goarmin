// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { useState, type ReactNode } from 'react'
import { Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Activity, ActivityDetail, CoachDay, Day, Streams } from '../../api/types'
import { HeaderSlotContext } from '../../app/section/HeaderSlot'
import { DefaultPeriodContext, PeriodsContext } from '../../app/section/usePeriod'
import type { Period } from '../../lib/period'
import { addDays, days as daysOf, periodRange, today } from '../../lib/period'
import { fixtureMe, json, renderWith, stubApi, where } from '../../test/fixtures'
import ActivityPage from './Activity'
import Activities from './Activities'
import Coach from './Coach'
import Course from './Course'
import Courses from './Courses'
import Performance from './Performance'
import Workout from './Workout'
import Workouts from './Workouts'

afterEach(() => vi.unstubAllGlobals())

const day = today('UTC') // the test account's zone (fixtureMe timezone UTC), not the machine's
const now = Math.floor(Date.now() / 1000)

function act(id: number, over: Partial<Activity> = {}): Activity {
  const start = now - id * 86400
  return {
    id, start, end: start + 1800, day: addDays(day, -id), name: '', sport: 'running', sub_sport: 'generic', sport_label: 'Run',
    elapsed_s: 1800, timer_s: 1800, distance_m: 5000, kcal: 400, avg_hr: 150, max_hr: 172, avg_speed: 2.78, max_speed: 3.5,
    avg_cadence: 168, avg_power: null, ascent_m: 40, descent_m: 38, training_effect: 3.1, anaerobic_effect: 1.2, training_load: 90,
    polyline: '', ...over,
  }
}

/** A 10-minute track, one point a second at 3 m/s, with heart rate, altitude and a position. */
function track(start: number): Streams {
  const n = 600
  const idx = Array.from({ length: n }, (_, i) => i)
  return {
    t: idx.map((i) => start + i), dist: idx.map((i) => i * 3), speed: idx.map(() => 3), hr: idx.map((i) => 120 + Math.round(i / 10)),
    alt: idx.map((i) => 400 + Math.sin(i / 60) * 10), lat: idx.map((i) => 45 + i * 0.00002), lon: idx.map((i) => 7 + Math.sin(i / 100) * 0.001),
    cad: idx.map(() => 170), fields: ['dist', 'speed', 'hr', 'alt', 'lat', 'lon', 'cad'],
  }
}

function Page({ periods, def, children }: { periods: Period[]; def?: Period; children: ReactNode }) {
  const [slot, setSlot] = useState<HTMLDivElement | null>(null)
  return (
    <PeriodsContext.Provider value={periods}>
      <DefaultPeriodContext.Provider value={def}>
        <div ref={setSlot} data-testid="header" />
        <HeaderSlotContext.Provider value={slot}>{children}</HeaderSlotContext.Provider>
      </DefaultPeriodContext.Provider>
    </PeriodsContext.Provider>
  )
}

const show = (ui: ReactNode, path: string, periods: Period[] = [], def?: Period) =>
  renderWith(<Page periods={periods} def={def}>{ui}</Page>, { path, me: fixtureMe({ timezone: 'UTC' }) })

describe('Activities', () => {
  const all: Period[] = ['all', 'week', 'month', 'year']

  it('lists every activity in pages, filters by sport, and switches to a table', async () => {
    const asked: string[] = []
    stubApi({
      '/api/activities': (u) => {
        asked.push(u.search)
        return u.searchParams.get('before')
          ? json({ items: [act(5, { name: 'Long ride', sport: 'cycling', sport_label: 'Road bike', avg_speed: 7 })] })()
          : json({ items: [act(1, { name: 'Morning run' }), act(2, { name: 'Ride home', sport: 'cycling', sport_label: 'Road bike', avg_speed: 6.5 })], next_before: act(2).start })()
      },
    })
    show(<Activities />, '/training/activities', all)
    expect(await screen.findByText('Morning run')).toBeTruthy()
    expect(screen.getByText('Ride home')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Load more' }))
    expect(await screen.findByText('Long ride')).toBeTruthy()
    expect(asked[1]).toContain(`before=${act(2).start}`)

    fireEvent.click(screen.getByRole('button', { name: 'Cycling' }))
    await waitFor(() => expect(screen.queryByText('Morning run')).toBeNull())
    expect(screen.getByText('Long ride')).toBeTruthy()

    fireEvent.click(within(screen.getByTestId('header')).getByRole('button', { name: 'Table' }))
    const table = await screen.findByRole('table')
    expect(within(table).getAllByRole('row')).toHaveLength(3) // header + 2 rides
    expect(within(table).getByRole('link', { name: 'Ride home' }).getAttribute('href')).toBe('/training/activities/2')
  })

  it('shows a period with its totals, and an empty one calmly', async () => {
    const r = periodRange(day, 'week')
    stubApi({
      '/api/activities': (u) => json({ items: u.searchParams.get('from_day') === r.from ? [act(0), act(0, { id: 9, distance_m: 10000, timer_s: 3600 })] : [] })(),
    })
    const { unmount } = show(<Activities />, '/training/activities?p=week', all)
    expect(await screen.findByText('Activities')).toBeTruthy()
    expect(screen.getByText('2')).toBeTruthy()
    expect(screen.getByText(/^15(\.0+)? km$/)).toBeTruthy()
    unmount()
    show(<Activities />, `/training/activities?p=week&d=${addDays(day, -21)}`, all)
    expect(await screen.findByText('No activities in this week')).toBeTruthy()
  })
})

describe('Activity', () => {
  const a: ActivityDetail = {
    ...act(3, { name: 'Tempo run' }),
    laps: [0, 1].map((i) => ({
      index: i, start: act(3).start + i * 900, elapsed_s: 900, timer_s: 900, distance_m: 2500, kcal: 200, avg_hr: 148 + i * 4, max_hr: 165 + i,
      avg_speed: 2.78, max_speed: 3.4, avg_cadence: 168, avg_power: null, ascent_m: 20, descent_m: 19,
    })),
  }
  const routes = (extra: Record<string, (u: URL) => Response> = {}) => stubApi({
    '/api/activities/3': json(a),
    '/api/activities/3/streams': json(track(a.start)),
    '/api/activities/3/neighbours': json({ prev: act(4), next: act(2) }),
    '/api/activities': json({ items: [act(1, { max_hr: 185 })] }),
    ...extra,
  })
  const page = (path: string) => show(<Routes><Route path="/training/activities/:id" element={<ActivityPage />} /></Routes>, path)

  it('shows the numbers, the route, the panels, laps, splits and zones', async () => {
    routes()
    page('/training/activities/3')
    expect(await screen.findByText('Laps (2)')).toBeTruthy()
    expect(screen.getByText(/^5(\.0+)? km$/)).toBeTruthy()
    expect(screen.getByRole('img', { name: /^Route of Tempo run, coloured by heart rate$/ })).toBeTruthy()
    expect(screen.getByRole('img', { name: /^Heart rate: from 120 to 180, average 150 bpm$/ })).toBeTruthy()
    expect(screen.getByRole('img', { name: /^Elevation: / })).toBeTruthy()
    expect(screen.getByRole('img', { name: /^Pace: from 5:33 to 5:33/ })).toBeTruthy()
    expect(screen.getByText('Splits per kilometre')).toBeTruthy()
    expect(screen.getByText('Time in heart-rate zones')).toBeTruthy()
    expect(screen.getByText(/highest heart rate in recent activities \(185 bpm\)/)).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Elevation' }))
    expect(screen.getByRole('img', { name: /coloured by elevation$/ })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Distance' }))
    expect(screen.getByRole('button', { name: 'Distance', pressed: true })).toBeTruthy()

    const header = within(screen.getByTestId('header'))
    expect(header.getByRole('link', { name: 'FIT file' }).getAttribute('href')).toBe('/api/activities/3/fit')
    expect(header.getByRole('link', { name: 'Previous activity' }).getAttribute('href')).toBe('/training/activities/4')
    fireEvent.keyDown(window, { key: 'ArrowRight' })
    await waitFor(() => expect(where.path).toBe('/training/activities/2'))
  })

  it('says so for an activity that is not there, and without a track', async () => {
    routes({ '/api/activities/3/streams': json({ error: 'no track' }, 404) })
    const { unmount } = page('/training/activities/3')
    expect(await screen.findByText('This activity has no recorded track.')).toBeTruthy()
    unmount()
    page('/training/activities/8')
    expect(await screen.findByText('Activity not found')).toBeTruthy()
    expect(within(screen.getByTestId('header')).queryByRole('link', { name: 'FIT file' })).toBeNull()
  })
})

describe('Performance', () => {
  const periods: Period[] = ['week', 'month', 'year']

  it('shows the month by default: status, readiness, load and predictions', async () => {
    const r = periodRange(day, 'month')
    const ds = daysOf(r).filter((d) => d <= day)
    const rows: Day[] = ds.map((d, i) => ({
      day: d, values: {
        training_status: i === ds.length - 1 ? 7 : 4, training_readiness: 60 + (i % 30), acute_load: 300 + i, chronic_load: 280,
        vo2max: 50 + i / 10, race_5k: 1400 - i, endurance_score: 6000 + i,
      },
    }))
    const asked: string[] = []
    stubApi({ '/api/daily': (u) => (asked.push(u.search), json(rows)()) })
    show(<Performance />, '/training/performance', periods, 'month')
    expect(await screen.findByText('The load is moving fitness in the right direction.')).toBeTruthy()
    expect(asked[0]).toBe(`?from=${r.from}&to=${r.to}`)
    expect(screen.getByRole('img', { name: /^Acute load from 300 to \d+.*; Chronic load from 280 to 280/ })).toBeTruthy()
    expect(screen.getByRole('img', { name: /^VO2 max from 50\.0 to/ })).toBeTruthy()
    expect(screen.getByRole('img', { name: /^5K from 23:20 to/ })).toBeTruthy()
    expect(screen.getByRole('list', { name: 'Training status by day' })).toBeTruthy()
    expect(screen.getByText(/Not recorded in this month: hill score\./)).toBeTruthy()
  })

  it('explains a period without training values', async () => {
    stubApi({ '/api/daily': json([{ day, values: { resting_hr: 52 } }]) })
    show(<Performance />, '/training/performance?p=week', periods, 'month')
    expect(await screen.findByText('No training values in this week')).toBeTruthy()
  })
})

describe('Coach', () => {
  it('ticks done days, marks missed ones and rest days', async () => {
    const d = addDays(day, -7) // a whole week in the past
    const r = periodRange(d, 'week')
    const w = daysOf(r)
    const plan: CoachDay[] = [
      { day: w[0], index: 0, name: 'Base', description: '8:00/km', sport: 'running', duration_s: 1200, benefit: 2, aerobic_te: 2.8, rest_day: false },
      { day: w[0], index: 1, name: 'Base', description: '136bpm', sport: 'running', duration_s: 1200, benefit: 2, aerobic_te: 2.8, rest_day: false },
      { day: w[1], index: 2, name: '', description: '', sport: '', duration_s: 0, benefit: null, aerobic_te: null, rest_day: true },
      { day: w[2], index: 3, name: 'Threshold', description: '5:05/km', sport: 'running', duration_s: 2400, benefit: 4, aerobic_te: 3.6, rest_day: false },
    ]
    stubApi({
      '/api/coach': json(plan),
      '/api/activities': json({ items: [act(1, { day: w[0], name: 'Easy run' })] }),
    })
    show(<Coach />, `/training/coach?d=${d}`, ['week'])
    const list = await screen.findByRole('list', { name: "The week's plan" })
    const cards = within(list).getAllByRole('listitem').filter((li) => li.parentElement === list)
    expect(cards).toHaveLength(7)
    expect(within(cards[0]).getByText('Done')).toBeTruthy()
    expect(within(cards[0]).getByText('8:00/km · 136bpm')).toBeTruthy()
    expect(within(cards[0]).getByRole('link', { name: /Easy run/ }).getAttribute('href')).toBe('/training/activities/1')
    expect(within(cards[1]).getByText('Rest day')).toBeTruthy()
    expect(within(cards[2]).getByText('Missed')).toBeTruthy()
    expect(within(cards[3]).getByText('No plan')).toBeTruthy()
    expect(screen.getByText('1/2')).toBeTruthy()
  })

  it('explains a week without a plan', async () => {
    stubApi({ '/api/coach': json([]), '/api/activities': json({ items: [] }) })
    show(<Coach />, '/training/coach', ['week'])
    expect(await screen.findByText('No Garmin Coach plan in this week')).toBeTruthy()
  })
})

describe('watch data', () => {
  const workout = {
    id: 4, name: '6 × 400 m', sport: 'running', sub_sport: 'generic', estimated_s: 1830, step_count: 5, exercise_count: 1, recorded_at: now - 86400,
    origin: 'watch', origins: ['watch', 'garmin_connect'], description: 'Short and fast',
    watches: [{ device_id: 7, name: 'Fenix 8', state: 'on', since: now - 600, checked_at: now - 600, want: true }],
    versions: [{ id: 4, origin: 'watch', device_id: 7, recorded_at: now - 86400, step_count: 5 }, { id: 9, origin: 'garmin_connect', device_id: 0, recorded_at: now - 2 * 86400, step_count: 5 }],
    steps: [
      { index: 0, intensity: 'warmup', duration: 'time', duration_value: 600, target: 'heart_rate', target_zone: 2 },
      { index: 1, intensity: 'active', duration: 'distance', duration_value: 400, target: 'speed', target_low: 4, target_high: 4.3 },
      { index: 2, intensity: 'recovery', duration: 'time', duration_value: 90 },
      { index: 3, duration: 'repeat', from: 1, repeat: 6 },
      { index: 4, intensity: 'active', duration: 'reps', duration_value: 12, weight_kg: 12, exercise: { category: 'squat', name: 'Goblet Squat', number: 37 }, notes: 'slow' },
    ],
  }
  const course = {
    id: 9, name: 'Lake loop', sport: 'running', distance_m: 6985, ascent_m: 39, descent_m: 39, polyline: '', point_count: 2, recorded_at: now - 86400,
    points: [{ type: 'water', name: 'Fountain', distance_m: 3200, lat: 45.01, lon: 7.01 }, { type: 'right', distance_m: 57, lat: 45.001, lon: 7.0 }],
  }
  const page = (ui: ReactNode, path: string, route: string) => show(<Routes><Route path={route} element={ui} /></Routes>, path)

  it('lists workouts and shows one with its repeats', async () => {
    stubApi({ '/api/workouts': json([{ ...workout, steps: undefined }]), '/api/workouts/4': json(workout) })
    const { unmount } = show(<Workouts />, '/training/workouts')
    const link = await screen.findByRole('link', { name: /6 × 400 m/ })
    expect(link.getAttribute('href')).toBe('/training/workouts/4')
    expect(screen.getByText(/5 steps · about 31m/)).toBeTruthy()
    unmount()
    page(<Workout />, '/training/workouts/4', '/training/workouts/:id')
    expect(await screen.findByText('6 times')).toBeTruthy()
    expect(screen.getByText('On Fenix 8')).toBeTruthy()
    expect(screen.getByText('From a watch and Garmin Connect')).toBeTruthy()
    expect(screen.getByText('Short and fast')).toBeTruthy()
    expect(screen.getByText('Warm up')).toBeTruthy()
    expect(screen.getByText('HR zone 2')).toBeTruthy()
    expect(screen.getByText('3:53–4:10 /km')).toBeTruthy()
    expect(screen.getByText('Goblet Squat')).toBeTruthy()
    expect(screen.getByText('12 kg · slow')).toBeTruthy()
    expect(screen.getAllByRole('button', { name: /^View pose/ })).toHaveLength(1) // only the step with an exercise
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
    fireEvent.click(screen.getByRole('button', { name: 'View pose: Goblet Squat' }))
    expect(screen.getByRole('dialog', { name: 'Goblet Squat' })).toBeTruthy()
    expect(screen.getByText('12 reps · 12 kg')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(screen.getByRole('img', { name: /^Intensity over the workout: warmup, active, recovery, active/ })).toBeTruthy()
  })

  it('renames a workout, and goes back to the original', async () => {
    const renamed = { ...workout, id: 12, name: '6 × 400 m fast', origin: 'goarmin', origins: ['goarmin', 'watch', 'garmin_connect'] }
    let shown: object = workout
    const fn = stubApi({
      '/api/workouts/4': () => json(shown)(),
      '/api/workouts/12': () => json(renamed)(),
    })
    const calls = () => fn.mock.calls as unknown as [string, RequestInit | undefined][]
    page(<Workout />, '/training/workouts/4', '/training/workouts/:id')
    fireEvent.click(await screen.findByRole('button', { name: 'Rename' }))
    const name = screen.getByRole('textbox', { name: /^Name/ })
    fireEvent.change(name, { target: { value: '  6 × 400 m   fast ' } })
    shown = renamed
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(calls().some(([u, init]) => u.endsWith('/api/workouts/4') && init?.method === 'PATCH')).toBe(true))
    const call = calls().find(([, init]) => init?.method === 'PATCH')!
    expect(JSON.parse(String(call[1]!.body))).toEqual({ name: '6 × 400 m fast', description: 'Short and fast' })
    expect(await screen.findByText(/^Renamed in goarmin/)).toBeTruthy()
    expect(screen.getByText('Edited in goarmin, from a watch and Garmin Connect')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Back to the original' }))
    await waitFor(() => expect(calls().some(([u, init]) => u.endsWith('/api/workouts/12') && init?.method === 'DELETE')).toBe(true))
  })

  it('switches a workout on for a watch, and says what happens next', async () => {
    const off = { ...workout, watches: [{ device_id: 7, name: 'Fenix 8', state: 'removed', since: now - 600, checked_at: now - 600, want: false }] }
    const sending = { ...workout, watches: [{ ...off.watches[0], want: true, pending: 'send' }] }
    let shown: object = off
    const fn = stubApi({
      '/api/workouts/4': () => json(shown)(),
      '/api/workouts/4/watches/7': () => { shown = sending; return json(sending)() },
    })
    const calls = () => fn.mock.calls as unknown as [string, RequestInit | undefined][]
    page(<Workout />, '/training/workouts/4', '/training/workouts/:id')
    const sw = await screen.findByRole('switch', { name: 'On Fenix 8' })
    expect(sw.getAttribute('aria-checked')).toBe('false')
    expect(screen.getByText('Not on the watch. Switch it on to send it at the next sync.')).toBeTruthy()
    fireEvent.click(sw)
    await waitFor(() => expect(calls().some(([u, init]) => u.endsWith('/api/workouts/4/watches/7') && init?.method === 'PUT')).toBe(true))
    expect(JSON.parse(String(calls().find(([, init]) => init?.method === 'PUT')![1]!.body))).toEqual({ on: true })
    expect(await screen.findByText('Goes on the watch at its next sync.')).toBeTruthy()
    expect(screen.getByRole('switch', { name: 'On Fenix 8' }).getAttribute('aria-checked')).toBe('true')
  })

  it('is a library: every workout with where it is, searchable and filtered', async () => {
    const lib = [
      { ...workout, steps: undefined, versions: undefined },
      { ...workout, steps: undefined, versions: undefined, id: 5, name: 'Coherence', sport: 'training', sub_sport: 'breathing', origins: ['watch'],
        description: undefined, watches: [{ device_id: 7, name: 'Fenix 8', state: 'removed', since: now - 3600, checked_at: now - 600 }] },
      { ...workout, steps: undefined, versions: undefined, id: 6, name: 'Alignment 101', sport: 'training', sub_sport: 'yoga', origin: 'garmin_connect',
        origins: ['garmin_connect'], description: undefined, watches: [{ device_id: 7, name: 'Fenix 8', state: 'absent', checked_at: now - 600 }] },
    ]
    stubApi({ '/api/workouts': json(lib) })
    show(<Workouts />, '/training/workouts')
    const cards = await screen.findByRole('list', { name: 'Workouts' })
    expect(within(cards).getAllByRole('link')).toHaveLength(3)
    expect(screen.getByText('3 workouts in your library')).toBeTruthy()
    expect(screen.getByText(/1 on it · 1 removed · checked 10 min ago/)).toBeTruthy()
    expect(within(cards).getByText('On Fenix 8')).toBeTruthy() // the badge's spoken text
    expect(within(cards).getByText(/^Removed from Fenix 8/)).toBeTruthy()
    expect(within(cards).getByText('Not on Fenix 8')).toBeTruthy()

    fireEvent.click(within(screen.getByRole('group', { name: 'On a watch' })).getByRole('button', { name: 'On Fenix 8' }))
    expect(within(screen.getByRole('list', { name: 'Workouts' })).getAllByRole('link').map((a) => a.textContent)).toEqual([expect.stringContaining('6 × 400 m')])
    expect(screen.getByText('1 of 3 workouts')).toBeTruthy()
    fireEvent.click(within(screen.getByRole('group', { name: 'On a watch' })).getByRole('button', { name: 'On no watch' }))
    expect(within(screen.getByRole('list', { name: 'Workouts' })).getAllByRole('link')).toHaveLength(2)
    fireEvent.click(within(screen.getByRole('group', { name: 'Source' })).getByRole('button', { name: 'From Garmin Connect' }))
    expect(within(screen.getByRole('list', { name: 'Workouts' })).getAllByRole('link').map((a) => a.textContent)).toEqual([expect.stringContaining('Alignment 101')])
    fireEvent.click(within(screen.getByRole('group', { name: 'Source' })).getByRole('button', { name: 'Every source' }))
    fireEvent.click(within(screen.getByRole('group', { name: 'On a watch' })).getByRole('button', { name: 'Anywhere' }))
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search workouts' }), { target: { value: 'breathing' } })
    expect(within(screen.getByRole('list', { name: 'Workouts' })).getAllByRole('link').map((a) => a.textContent)).toEqual([expect.stringContaining('Coherence')])
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search workouts' }), { target: { value: 'nothing like it' } })
    expect(screen.getByText('No workout fits these filters.')).toBeTruthy()
  })

  it('says how workouts reach an empty library', async () => {
    stubApi({ '/api/workouts': json([]) })
    show(<Workouts />, '/training/workouts')
    expect(await screen.findByText('No workouts yet')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Devices › Import' }).getAttribute('href')).toBe('/devices/import')
  })

  it('lists courses and shows one with its points', async () => {
    stubApi({
      '/api/courses': json([{ ...course, points: undefined }]),
      '/api/courses/9': json(course),
      '/api/courses/9/streams': json(track(now - 3600)),
    })
    const { unmount } = show(<Courses />, '/training/courses')
    expect((await screen.findByRole('link', { name: /Lake loop/ })).getAttribute('href')).toBe('/training/courses/9')
    unmount()
    page(<Course />, '/training/courses/9', '/training/courses/:id')
    expect(await screen.findByText('Points (2)')).toBeTruthy()
    expect(screen.getByText('Fountain')).toBeTruthy()
    expect(screen.getByText('Turn right')).toBeTruthy()
    expect(screen.getByRole('img', { name: 'Route of Lake loop, coloured by elevation, with 2 points' })).toBeTruthy()
    expect(within(screen.getByTestId('header')).getByRole('link', { name: 'FIT file' }).getAttribute('href')).toBe('/api/courses/9/fit')
  })

  it('shows the time in zones the watch counted, with its limits', async () => {
    const a = { ...act(3, { name: 'Tempo run' }), laps: [], zones: { hr: [92, 110, 128, 146, 165, 183], hr_s: [0, 60, 600, 1200, 300, 0, 0] } }
    stubApi({
      '/api/activities/3': json(a),
      '/api/activities/3/streams': json(track(a.start)),
      '/api/activities/3/neighbours': json({ prev: null, next: null }),
      '/api/activities': json({ items: [] }),
      '/api/me/zones': json({ default: null, profiles: [] }),
    })
    page(<ActivityPage />, '/training/activities/3', '/training/activities/:id')
    expect(await screen.findByText('Zones from the watch, as it counted this activity.')).toBeTruthy()
    expect(screen.getByText('128–146')).toBeTruthy()
    expect(screen.getByText('20m')).toBeTruthy() // zone 3: 1200 s
  })

  it('shows the lifetime totals the watch keeps', async () => {
    stubApi({
      '/api/activities': json({ items: [act(1)] }),
      '/api/totals': json({ sports: [{ sport: 'running', timer_s: 6155, distance_m: 11944, kcal: 1149, sessions: 3 }], watches: 1, recorded_at: now }),
    })
    show(<Activities />, '/training/activities', ['all', 'week', 'month', 'year'])
    const list = await screen.findByRole('list', { name: 'Lifetime totals' })
    expect(within(list).getByText(/3 sessions/)).toBeTruthy()
    expect(within(list).getByText(/^11\.9\d? km$/)).toBeTruthy()
  })
})

describe('maps', () => {
  it('draws the route on the cached map tiles when the server has them', async () => {
    const a = { ...act(3, { name: 'Tempo run' }), laps: [] }
    stubApi({
      '/api/activities/3': json(a),
      '/api/activities/3/streams': json(track(a.start)),
      '/api/activities/3/neighbours': json({ prev: null, next: null }),
      '/api/activities': json({ items: [] }),
    })
    const me = fixtureMe({ timezone: 'UTC', server: { version: 'dev', sync: false, tiles: true } })
    renderWith(<Page periods={[]}><Routes><Route path="/training/activities/:id" element={<ActivityPage />} /></Routes></Page>, { path: '/training/activities/3', me })
    const map = await screen.findByRole('img', { name: 'Map of Tempo run, the route coloured by heart rate' })
    await waitFor(() => expect(map.querySelector('.leaflet-control-attribution')?.textContent).toContain('OpenStreetMap'))
    expect(map.querySelector('img.leaflet-tile')?.getAttribute('src') ?? '/tiles/').toMatch(/^\/tiles\//)
    expect(screen.queryByText(/no map tiles/)).toBeNull()
  })
})
