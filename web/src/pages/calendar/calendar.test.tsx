// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { useState, type ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CalendarDay, Workout } from '../../api/types'
import { HeaderSlotContext } from '../../app/section/HeaderSlot'
import { FutureContext, PeriodsContext } from '../../app/section/usePeriod'
import { addDays, today } from '../../lib/period'
import { json, renderWith, stubApi, where } from '../../test/fixtures'
import { ToastProvider } from '../../ui/Toast'
import CalendarPage from './Calendar'

const day = today('UTC') // the test account's zone
const now = Math.floor(Date.now() / 1000)
const yesterday = addDays(day, -1)
const ahead = addDays(day, 2)

function Page({ children }: { children: ReactNode }) {
  const [slot, setSlot] = useState<HTMLDivElement | null>(null)
  return (
    <ToastProvider>
      <PeriodsContext.Provider value={['month', 'week']}>
        <FutureContext.Provider value>
          <div ref={setSlot} data-testid="header" />
          <HeaderSlotContext.Provider value={slot}>{children}</HeaderSlotContext.Provider>
        </FutureContext.Provider>
      </PeriodsContext.Provider>
    </ToastProvider>
  )
}

const days: CalendarDay[] = [
  {
    day: yesterday,
    summary: { day: yesterday, steps: 7305, values: { step_goal: 6000, weight: 75.2 }, sleep_score: 79, sleep_s: 23160, resting_hr: 59, hr_max: 141,
      stress_avg: 24, body_battery_min: 17, body_battery_max: 77 },
    kinds: { activity: 1, plan: 1, steps: 1, sleep: 1, heart: 1, stress: 1, body_battery: 1, weigh_in: 1 },
    activities: [{ id: 3, name: 'Morning', start: now - 86400, end: now - 86400 + 1800, sport: 'running', sub_sport: 'generic', sport_label: 'Run',
      timer_s: 1800, distance_m: 5200, workout: 'Tempo' }],
    coach: [],
    plans: [{ id: 9, day: yesterday, workout_id: 4, name: 'Tempo', sport: 'running', sub_sport: 'generic', estimated_s: 1500, note: '',
      missing: false, done_activity: 3, missed: false }],
  },
  {
    day: ahead, summary: null, kinds: { plan: 2 }, activities: [],
    coach: [{ index: 0, name: 'Base', description: '', sport: 'running', duration_s: 2400, benefit: 2, aerobic_te: 2.5, rest_day: false, done: false, missed: false }],
    plans: [{ id: 10, day: ahead, workout_id: 5, name: 'Core 20', sport: 'training', sub_sport: 'strength_training', estimated_s: 1200, note: 'easy',
      missing: false, done_activity: null, missed: false }],
  },
]

const library: Workout[] = [
  { id: 4, name: 'Tempo', sport: 'running', sub_sport: 'generic', estimated_s: 1500, step_count: 3, exercise_count: 0, recorded_at: now, origin: 'watch',
    origins: ['watch'], watches: [{ device_id: 1, name: 'Fenix 8', state: 'on', checked_at: now, want: true }] },
  { id: 5, name: 'Core 20', sport: 'training', sub_sport: 'strength_training', estimated_s: 1200, step_count: 6, exercise_count: 4, recorded_at: now,
    origin: 'garmin_connect', origins: ['garmin_connect'], watches: [] },
]

/** api answers the calendar's routes and records the writes. */
function api() {
  const writes: { method: string; path: string; body: unknown }[] = []
  const fn = stubApi({
    '/api/calendar': (u) => json({ from: u.searchParams.get('from'), to: u.searchParams.get('to'), today: day, days })(),
    '/api/workouts': json(library),
  })
  const base = fn.getMockImplementation() as (url: string, init?: RequestInit) => Promise<Response>
  fn.mockImplementation(async (url: string, init?: RequestInit) => {
    if (init?.method && init.method !== 'GET') {
      const body = init.body ? JSON.parse(String(init.body)) : undefined
      writes.push({ method: init.method, path: new URL(url, 'http://localhost').pathname, body })
      if (init.method === 'DELETE') return new Response(null, { status: 204 })
      if (url.includes('/me/settings/prefs')) return json({ value: body, rev: 2 })()
      return json({ ...days[1].plans[0], ...(body as object) }, init.method === 'POST' ? 201 : 200)()
    }
    return base(url, init)
  })
  return writes
}

let wide = true
beforeEach(() => {
  wide = true
  vi.stubGlobal('matchMedia', (q: string) => ({ matches: wide && q.includes('min-width'), media: q, addEventListener() {}, removeEventListener() {} }))
})
afterEach(() => vi.unstubAllGlobals())

describe('Calendar', () => {
  it('shows the month with bars and names, and a day in the panel', async () => {
    api()
    renderWith(<Page><CalendarPage /></Page>, { path: `/calendar?d=${yesterday}` })
    const cell = await screen.findByRole('button', { name: new RegExp(`^${new Intl.DateTimeFormat(undefined, { weekday: 'long', day: 'numeric', month: 'long', timeZone: 'UTC' }).format(new Date(`${yesterday}T00:00:00Z`))}`) })
    expect(cell.getAttribute('aria-current')).toBe('date')
    await waitFor(() => expect(cell.getAttribute('aria-label')).toMatch(/Run 5\.20 km, Tempo done, 7,305 steps, Sleep 79/))
    const panel = screen.getByRole('region', { name: /^Day: / })
    expect(within(panel).getByText('Steps').closest('a')!.getAttribute('href')).toBe(`/health/movement?d=${yesterday}`)
    expect(within(panel).getByText(/7,305 · 122 % of goal/)).toBeTruthy()
    expect(within(panel).getByText('Resting 59 bpm · max 141 bpm')).toBeTruthy()
    expect(within(panel).getByRole('link', { name: 'Tempo' }).getAttribute('href')).toBe('/training/workouts/4')
    expect(within(panel).getByText('Done')).toBeTruthy()
    expect(within(panel).getByRole('link', { name: 'the activity' }).getAttribute('href')).toBe('/training/activities/3')
    expect(screen.getByRole('list', { name: 'Legend' })).toBeTruthy()

    // Arrow keys move the day; a day ahead holds its plans, Coach's and your own.
    fireEvent.keyDown(cell, { key: 'ArrowRight' })
    fireEvent.keyDown(screen.getByRole('button', { name: /today/ }), { key: 'ArrowRight' })
    await waitFor(() => expect(where.path).toContain(`d=${addDays(day, 1)}`))
    fireEvent.click(screen.getByRole('button', { name: /Core 20 planned/ }))
    await waitFor(() => expect(where.path).toContain(`d=${ahead}`))
    const p2 = screen.getByRole('region', { name: /^Day: / })
    expect(within(p2).getByText(/Garmin Coach/)).toBeTruthy()
    expect(within(p2).getByText(/Strength training · about 20m · easy/)).toBeTruthy()
    expect(within(p2).queryByText('Steps')).toBeNull()
  })

  it('plans, moves and removes workouts', async () => {
    const writes = api()
    renderWith(<Page><CalendarPage /></Page>, { path: `/calendar?d=${ahead}` })
    const panel = await screen.findByRole('region', { name: /^Day: / })
    await within(panel).findByText('Core 20')

    fireEvent.click(within(panel).getByRole('button', { name: 'Plan a workout' }))
    const picker = await screen.findByRole('dialog', { name: 'Plan a workout' })
    fireEvent.change(within(picker).getByRole('searchbox', { name: 'Search workouts' }), { target: { value: 'tempo' } })
    fireEvent.click(within(within(picker).getByRole('list', { name: 'Workouts to plan' })).getByRole('button', { name: /Tempo/ }))
    await waitFor(() => expect(writes).toContainEqual({ method: 'POST', path: '/api/plans', body: { day: ahead, workout_id: 4 } }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Plan a workout' })).toBeNull())

    fireEvent.click(within(panel).getByRole('button', { name: 'Move to…' }))
    fireEvent.change(within(panel).getByLabelText('Move Core 20 to'), { target: { value: addDays(day, 5) } })
    fireEvent.click(within(panel).getByRole('button', { name: 'Move' }))
    await waitFor(() => expect(writes).toContainEqual({ method: 'PATCH', path: '/api/plans/10', body: { day: addDays(day, 5) } }))

    fireEvent.click(within(panel).getByRole('button', { name: 'Remove' }))
    await waitFor(() => expect(writes).toContainEqual({ method: 'DELETE', path: '/api/plans/10', body: undefined }))
    fireEvent.click(await screen.findByRole('button', { name: 'Undo' }))
    await waitFor(() => expect(writes).toContainEqual({ method: 'POST', path: '/api/plans', body: { day: ahead, workout_id: 5, note: 'easy' } }))
  })

  it('hides kinds through Show and opens a day in a sheet on a phone', async () => {
    wide = false
    const writes = api()
    renderWith(<Page><CalendarPage /></Page>, { path: `/calendar?d=${ahead}` })
    await screen.findAllByRole('button', { name: /planned/ })
    expect(screen.queryByRole('region', { name: /^Day: / })).toBeNull()
    fireEvent.click(within(screen.getByTestId('header')).getByRole('button', { name: 'Show' }))
    fireEvent.click(screen.getByRole('menuitemcheckbox', { name: 'Steps' }))
    await waitFor(() => expect(writes).toContainEqual({ method: 'PUT', path: '/api/me/settings/prefs', body: { v: 1, calendar_hidden: ['steps'] } }))
    fireEvent.click(screen.getByRole('button', { name: /Run 5\.20 km/ }))
    const sheet = await screen.findByRole('dialog')
    expect(within(sheet).getByRole('region', { name: /^Day: / })).toBeTruthy()
  })

  it('shows a week as an agenda', async () => {
    api()
    renderWith(<Page><CalendarPage /></Page>, { path: `/calendar?d=${yesterday}&p=week` })
    const list = await screen.findByRole('list', { name: 'Days' })
    expect(within(list).getAllByRole('listitem')).toHaveLength(7)
    expect(within(list).getAllByText(/Nothing recorded or planned/).length).toBeGreaterThan(0)
  })
})
