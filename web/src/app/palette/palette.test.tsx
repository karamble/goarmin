// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import { Search } from 'lucide-react'
import { useMemo } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { addDays, shortDay, today } from '../../lib/period'
import { fixtureMe, json, renderWith, stubApi, where } from '../../test/fixtures'
import { ToastProvider } from '../../ui/Toast'
import { visibleSections } from '../routes'
import { PaletteActionsProvider, usePaletteActions } from './actions'
import { CommandPalette, openPalette } from './CommandPalette'

afterEach(() => vi.unstubAllGlobals())

const me = fixtureMe({ timezone: 'UTC', server: { version: 'dev', sync: true, tiles: false } })

function Offers() {
  const items = useMemo(() => [{ id: 'page-thing', label: 'Do the page thing', icon: Search, run: () => document.body.setAttribute('data-done', 'yes') }], [])
  usePaletteActions('test', items)
  return null
}

function page(path: string) {
  return renderWith(
    <ToastProvider>
      <PaletteActionsProvider>
        <Offers />
        <CommandPalette sections={visibleSections(me)} />
      </PaletteActionsProvider>
    </ToastProvider>, { path, me })
}

const api = () => stubApi({
  '/api/devices': json([{ id: 2, nickname: 'Fenix', model: 'Fenix 8', name: '', current: true, sync: { running: false } }]),
  '/api/activities': json({ items: [{ id: 7, name: 'Tempo run', sport_label: 'Run', day: addDays(today('UTC'), -2) }] }),
  '/api/workouts': json([{ id: 4, name: 'Core 20' }]),
  '/api/courses': json([{ id: 9, name: 'Lake loop' }]),
})

describe('CommandPalette', () => {
  it('goes to a typed day on a page with days', async () => {
    api()
    page('/health/sleep?p=week')
    act(() => openPalette())
    const input = await screen.findByRole('combobox')
    fireEvent.change(input, { target: { value: 'yesterday' } })
    const y = addDays(today('UTC'), -1)
    expect(screen.getAllByRole('option')[0].textContent).toContain(`Go to ${shortDay(y)}`)
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(where.path).toBe(`/health/sleep?p=week&d=${y}`))
  })

  it('finds activities, workouts, courses, actions and what the page offers', async () => {
    api()
    page('/live')
    act(() => openPalette())
    const input = await screen.findByRole('combobox')
    for (const [q, label] of [['tempo', 'Tempo run'], ['core', 'Core 20'], ['lake', 'Lake loop'], ['live', 'Go live with Fenix'], ['custom', 'Customize the glance'], ['page thing', 'Do the page thing']]) {
      fireEvent.change(input, { target: { value: q } })
      expect(await screen.findByRole('option', { name: new RegExp(label) })).toBeTruthy()
    }
    fireEvent.change(input, { target: { value: 'tempo' } })
    fireEvent.click(await screen.findByRole('option', { name: /Tempo run/ }))
    await waitFor(() => expect(where.path).toBe('/training/activities/7'))
  })

  it('sends a typed day from a page without days to the glance', async () => {
    api()
    page('/training/workouts')
    act(() => openPalette())
    const input = await screen.findByRole('combobox')
    fireEvent.change(input, { target: { value: '3 days ago' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    await waitFor(() => expect(where.path).toBe(`/?d=${addDays(today('UTC'), -3)}`))
  })
})
