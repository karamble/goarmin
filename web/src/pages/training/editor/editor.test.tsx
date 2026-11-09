// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { useState, type ReactNode } from 'react'
import { Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Workout } from '../../../api/types'
import { HeaderSlotContext } from '../../../app/section/HeaderSlot'
import { fixtureMe, json, renderWith, stubApi, where } from '../../../test/fixtures'
import WorkoutEditor from './WorkoutEditor'

const now = Math.floor(Date.now() / 1000)

function Page({ children }: { children: ReactNode }) {
  const [slot, setSlot] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setSlot} data-testid="header" />
      <HeaderSlotContext.Provider value={slot}>{children}</HeaderSlotContext.Provider>
    </>
  )
}

const show = (path: string) =>
  renderWith(
    <Page>
      <Routes>
        <Route path="/training/workouts/new" element={<WorkoutEditor />} />
        <Route path="/training/workouts/:id/edit" element={<WorkoutEditor />} />
        <Route path="/training/workouts/:id" element={<p>the workout page</p>} />
      </Routes>
    </Page>,
    { path, me: fixtureMe({ timezone: 'UTC' }) },
  )

const catalog = {
  categories: [{ name: 'plank', number: 19, title: 'Plank', count: 2 }, { name: 'squat', number: 28, title: 'Squat', count: 1 }],
  exercises: [
    { key: 'plank/43', category: 'plank', category_num: 19, number: 43, name: 'plank', title: 'Plank', quality: 'own', family: 'plank', pose: 'p', mode: 'hold' },
    { key: 'plank/66', category: 'plank', category_num: 19, number: 66, name: 'side_plank', title: 'Side Plank', quality: 'family', family: 'plank', pose: 'p', mode: 'hold' },
    { key: 'squat/29', category: 'squat', category_num: 28, number: 29, name: 'dumbbell_squat', title: 'Dumbbell Squat', quality: 'family', family: 'squat', pose: 'p', mode: 'loop' },
  ],
}

const sets: Workout = {
  id: 4, name: 'Core Sets', sport: 'training', sub_sport: 'strength_training', estimated_s: 198, step_count: 4, exercise_count: 2, recorded_at: now - 3600,
  origin: 'goarmin', origins: ['goarmin'], description: 'Core first.', tag: '"t1"', watches: [],
  steps: [
    { index: 0, intensity: 'active', duration: 'reps', duration_value: 12, target: 'open', exercise: { category: 'plank', number: 43, name: 'Plank', title: 'Plank' } },
    { index: 1, intensity: 'rest', duration: 'time', duration_value: 30, target: 'open' },
    { index: 2, duration: 'repeat', from: 0, repeat: 3 },
    { index: 3, intensity: 'active', duration: 'time', duration_value: 45, exercise: { category: 'squat', number: 29, name: 'Dumbbell Squat', title: 'Dumbbell Squat' } },
  ],
}

type Call = [string, RequestInit | undefined]

afterEach(() => {
  vi.unstubAllGlobals()
  try {
    localStorage.clear() // the drafts; a missing store is no draft
  } catch {
    /* no storage in this environment */
  }
})

describe('the workout editor', () => {
  it('builds a workout: an exercise from the picker, a rest, saved as Garmin lays it out', async () => {
    let saved: unknown = null
    const fn = stubApi({
      '/api/exercises': json(catalog),
      '/api/workouts': () => json({ ...sets, id: 9, name: 'Morning core' }, 201)(),
    })
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
    show('/training/workouts/new')
    const header = screen.getByTestId('header')
    const save = within(header).getByRole('button', { name: 'Save' })
    expect(save).toHaveProperty('disabled', true) // no name, no steps
    expect(screen.getByText('The workout needs a name.')).toBeTruthy()
    fireEvent.change(screen.getByRole('textbox', { name: 'Name' }), { target: { value: '  Morning   core ' } })

    fireEvent.click(screen.getByRole('button', { name: 'Exercise' }))
    const picker = await screen.findByRole('dialog', { name: 'Add an exercise' })
    fireEvent.change(await within(picker).findByRole('combobox', { name: 'Search exercises' }), { target: { value: 'side' } })
    expect(within(within(picker).getByRole('listbox', { name: 'Exercises' })).getAllByRole('option').map((o) => o.textContent))
      .toEqual([expect.stringContaining('Side Plank')])
    fireEvent.click(within(picker).getByRole('button', { name: 'Add' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Add an exercise' })).toBeNull())

    const list = screen.getByRole('list', { name: 'Steps' })
    expect(within(list).getByText('Side Plank')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Rest' }))
    expect(within(list).getAllByRole('button', { name: /^\d\. / })).toHaveLength(2)
    // On a phone the step's settings open in a sheet when it is tapped.
    fireEvent.click(within(list).getByRole('button', { name: /^2\. Rest/ }))
    const panel = await screen.findByRole('dialog', { name: 'Rest' })
    fireEvent.change(within(panel).getByRole('textbox', { name: 'Time' }), { target: { value: '0:45' } })
    fireEvent.click(within(panel).getByRole('button', { name: 'Close' }))

    expect(save).toHaveProperty('disabled', false)
    fireEvent.click(save)
    await waitFor(() => expect((fn.mock.calls as unknown as Call[]).some(([u, i]) => u === '/api/workouts' && i?.method === 'POST')).toBe(true))
    const call = (fn.mock.calls as unknown as Call[]).find(([, i]) => i?.method === 'POST')!
    saved = JSON.parse(String(call[1]!.body))
    expect(saved).toEqual({
      name: '  Morning   core ', sport: 'training', sub_sport: 'strength_training', description: '',
      steps: [
        { index: 0, intensity: 'active', duration: 'reps', duration_value: 10, target: 'open', exercise: { category: 'plank', number: 66, name: 'Side Plank', title: 'Side Plank' } },
        { index: 1, intensity: 'rest', duration: 'time', duration_value: 45, target: 'open' },
      ],
    })
    await waitFor(() => expect(where.path).toBe('/training/workouts/9'))
  })

  it('edits a workout of sets in place, moves a step by keyboard, and saves with its tag', async () => {
    const fn = stubApi({
      '/api/workouts/4': json(sets),
      '/api/exercises': json(catalog),
    })
    vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
    show('/training/workouts/4/edit')
    const list = await screen.findByRole('list', { name: 'Steps' })
    const rows = within(list).getAllByRole('button', { name: /^\d\. / })
    expect(rows.map((r) => r.getAttribute('aria-label'))).toEqual([
      expect.stringMatching(/^1\. Plank, 3 sets × 12 reps · rest 0:30/),
      expect.stringMatching(/^2\. Dumbbell Squat, 0:45/),
    ])
    const save = within(screen.getByTestId('header')).getByRole('button', { name: 'Save' })
    expect(save).toHaveProperty('disabled', true) // nothing changed yet

    // Alt+↑ puts the squat first; Ctrl+Z puts it back; Ctrl+Shift+Z again.
    act(() => { rows[1].focus() })
    fireEvent.keyDown(rows[1], { key: 'ArrowUp', altKey: true })
    const order = () => within(list).getAllByRole('button', { name: /^\d\. / }).map((r) => r.getAttribute('aria-label')!.replace(/,.*/, ''))
    expect(order()).toEqual(['1. Dumbbell Squat', '2. Plank'])
    fireEvent.keyDown(document.body, { key: 'z', ctrlKey: true })
    expect(order()).toEqual(['1. Plank', '2. Dumbbell Squat'])
    fireEvent.keyDown(document.body, { key: 'z', ctrlKey: true, shiftKey: true })
    expect(order()).toEqual(['1. Dumbbell Squat', '2. Plank'])

    // Four sets instead of three.
    fireEvent.click(within(list).getByRole('button', { name: /^2\. Plank/ }))
    fireEvent.change(screen.getByRole('spinbutton', { name: 'Sets' }), { target: { value: '4' } })
    fireEvent.click(save)
    await waitFor(() => expect((fn.mock.calls as unknown as Call[]).some(([, i]) => i?.method === 'PUT')).toBe(true))
    const [, init] = (fn.mock.calls as unknown as Call[]).find(([, i]) => i?.method === 'PUT')!
    expect((init!.headers as Record<string, string>)['If-Match']).toBe('"t1"')
    expect(JSON.parse(String(init!.body)).steps.map((s: { duration: string; repeat?: number }) => s.duration + (s.repeat ? s.repeat : ''))).toEqual(
      ['time', 'reps', 'time', 'repeat4'])
  })
})
