// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { useState, type ReactNode } from 'react'
import { Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { WorkoutBody } from '../../api/types'
import { HeaderSlotContext } from '../../app/section/HeaderSlot'
import type { ExerciseInfo } from '../../figures/api'
import { fixtureMe, json, renderWith, stubApi, where } from '../../test/fixtures'
import WorkoutEditor from './WorkoutEditor'

const now = Math.floor(Date.now() / 1000)

beforeEach(() => {
  const store = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v), removeItem: (k: string) => void store.delete(k),
  })
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null) // no WebGL: figures say so
})
afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

function Page({ children }: { children: ReactNode }) {
  const [slot, setSlot] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setSlot} data-testid="header" />
      <HeaderSlotContext.Provider value={slot}>{children}</HeaderSlotContext.Provider>
    </>
  )
}

const show = (path: string) => renderWith(
  <Page>
    <Routes>
      <Route path="/training/workouts/new" element={<WorkoutEditor />} />
      <Route path="/training/workouts/:id/edit" element={<WorkoutEditor />} />
      <Route path="/training/workouts/:id" element={<p>workout page</p>} />
      <Route path="/training/workouts" element={<p>library</p>} />
    </Routes>
  </Page>,
  { path, me: fixtureMe({ timezone: 'UTC' }) },
)

const ex = (category: string, number: number, title: string): ExerciseInfo => ({
  key: `${category}/${number}`, category, category_num: 1, number, name: title.toLowerCase().replace(/ /g, '_'), title, quality: 'own',
  family: 'f', pose: 'p', mode: 'hold',
})
const catalog = {
  categories: [{ name: 'plank', number: 19, title: 'Plank', count: 2 }, { name: 'squat', number: 28, title: 'Squat', count: 1 }],
  exercises: [ex('plank', 43, 'Plank'), ex('plank', 44, 'Side Plank'), ex('squat', 37, 'Goblet Squat')],
}
const sports = [
  { sport: 'running', sub_sport: 'generic', label: 'Running' },
  { sport: 'training', sub_sport: 'strength_training', label: 'Strength' },
]

const original = {
  id: 4, name: '6 × 400 m', sport: 'running', sub_sport: 'generic', estimated_s: 1140, step_count: 5, exercise_count: 0, recorded_at: now - 86400,
  origin: 'watch', origins: ['watch'], watches: [{ device_id: 7, name: 'Fenix 8', state: 'on', want: true, checked_at: now }],
  steps: [
    { index: 0, intensity: 'warmup', duration: 'time', duration_value: 600 },
    { index: 1, intensity: 'active', duration: 'distance', duration_value: 400, target: 'speed', target_low: 4, target_high: 4.3 },
    { index: 2, intensity: 'recovery', duration: 'time', duration_value: 90 },
    { index: 3, duration: 'repeat', from: 1, repeat: 6 },
    { index: 4, intensity: 'cooldown', duration: 'open' },
  ],
}

const calls = (fn: ReturnType<typeof stubApi>) => fn.mock.calls as unknown as [string, RequestInit | undefined][]
const sent = (fn: ReturnType<typeof stubApi>, method: string): WorkoutBody =>
  JSON.parse(String(calls(fn).find(([, init]) => init?.method === method)![1]!.body))

describe('the workout editor', () => {
  it('makes a strength workout with a repeat and an exercise', async () => {
    const fn = stubApi({
      '/api/workout-sports': json(sports),
      '/api/exercises': json(catalog),
      '/api/workouts': json({ ...original, id: 12, name: 'Core 20', origin: 'goarmin', watches: [] }, 201),
    })
    show('/training/workouts/new')
    const header = within(screen.getByTestId('header'))
    expect(header.getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(true) // no name yet
    expect(await screen.findByText('The workout needs a name.')).toBeTruthy()
    fireEvent.change(screen.getByRole('textbox', { name: /^Name/ }), { target: { value: 'Core 20' } })
    fireEvent.change(await screen.findByRole('combobox', { name: 'Sport' }), { target: { value: 'training/strength_training' } })

    // Step 2 (work) gets an exercise and a weight, then goes into a repeat.
    const work = screen.getByRole('article', { name: /^Step 2/ })
    fireEvent.change(within(work).getByRole('combobox', { name: 'Duration' }), { target: { value: 'reps' } })
    fireEvent.click(within(work).getByRole('button', { name: 'Choose an exercise' }))
    const picker = await screen.findByRole('dialog', { name: 'Choose an exercise' })
    fireEvent.change(await within(picker).findByRole('searchbox', { name: 'Search exercises' }), { target: { value: 'goblet' } })
    fireEvent.click(within(picker).getByRole('button', { name: /Goblet Squat/ }))
    fireEvent.click(within(picker).getByRole('button', { name: 'Use Goblet Squat' }))
    const work2 = screen.getByRole('article', { name: /^Step 2: Goblet Squat/ })
    fireEvent.change(within(work2).getByRole('textbox', { name: 'Weight of step 2' }), { target: { value: '12.5' } })
    fireEvent.click(within(work2).getByRole('button', { name: 'More for step 2' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Repeat this step' }))
    const repeat = screen.getByRole('region', { name: 'A repeat' })
    fireEvent.change(within(repeat).getByRole('textbox', { name: 'Times to repeat' }), { target: { value: '3' } })

    fireEvent.click(header.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(calls(fn).some(([, init]) => init?.method === 'POST')).toBe(true))
    const b = sent(fn, 'POST')
    expect(b.name).toBe('Core 20')
    expect([b.sport, b.sub_sport]).toEqual(['training', 'strength_training'])
    expect(b.steps.map((s) => s.duration)).toEqual(['time', 'reps', 'repeat', 'time'])
    expect(b.steps[1]).toMatchObject({ index: 1, duration_value: 10, weight_kg: 12.5, exercise: { category: 'squat', number: 37, title: 'Goblet Squat' } })
    expect(b.steps[2]).toEqual({ index: 2, duration: 'repeat', from: 1, repeat: 3 })
    await waitFor(() => expect(where.path).toBe('/training/workouts/12'))
  })

  it('edits a watch workout in place and keeps its repeats', async () => {
    const fn = stubApi({
      '/api/workout-sports': json(sports),
      '/api/workouts/4': () => json(original)(),
    })
    show('/training/workouts/4/edit')
    expect(await screen.findByText(/You are editing the watch's version/)).toBeTruthy()
    const header = within(screen.getByTestId('header'))
    expect(header.getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(true) // nothing changed yet
    const repeat = screen.getByRole('region', { name: 'A repeat' })
    expect((within(repeat).getByRole('textbox', { name: 'Times to repeat' }) as HTMLInputElement).value).toBe('6')
    expect((within(repeat).getByRole('textbox', { name: 'Slowest pace, step 2' }) as HTMLInputElement).value).toBe('4:10')
    fireEvent.change(within(repeat).getByRole('textbox', { name: 'Times to repeat' }), { target: { value: '8' } })
    fireEvent.click(header.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(calls(fn).some(([, init]) => init?.method === 'PUT')).toBe(true))
    const b = sent(fn, 'PUT')
    expect(b.steps).toEqual(original.steps.map((s) => (s.duration === 'repeat' ? { ...s, repeat: 8 } : s)))
  })

  it('asks before leaving with changes, and offers them again later', async () => {
    stubApi({ '/api/workout-sports': json(sports), '/api/workouts/4': () => json(original)() })
    const { unmount } = show('/training/workouts/4/edit')
    fireEvent.change(await screen.findByRole('textbox', { name: /^Name/ }), { target: { value: 'Hill repeats' } })
    fireEvent.click(within(screen.getByTestId('header')).getByRole('button', { name: 'Cancel' }))
    const ask = screen.getByRole('alertdialog', { name: 'Leave without saving?' })
    fireEvent.click(within(ask).getByRole('button', { name: 'Cancel' }))
    unmount() // left another way: the draft stays

    show('/training/workouts/4/edit')
    fireEvent.click(await screen.findByRole('button', { name: 'Restore them' }))
    expect((screen.getByRole('textbox', { name: /^Name/ }) as HTMLInputElement).value).toBe('Hill repeats')
  })

  it('starts a copy from a workout', async () => {
    stubApi({ '/api/workout-sports': json(sports), '/api/workouts/4': () => json(original)() })
    show('/training/workouts/new?from=4')
    expect(((await screen.findByRole('textbox', { name: /^Name/ })) as HTMLInputElement).value).toBe('6 × 400 m (copy)')
    expect(screen.queryByText(/You are editing/)).toBeNull()
  })
})
