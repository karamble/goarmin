// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { HeaderSlotContext } from '../../app/section/HeaderSlot'
import { PeriodsContext } from '../../app/section/usePeriod'
import { fixtureMe, json, renderWith, stubApi, where } from '../../test/fixtures'
import { ToastProvider } from '../../ui/Toast'
import GlancePage from '../GlancePage'
import type { Layout } from '../types'

const unknown = { id: 'radar', type: 'weather-radar', size: 'L', options: { city: 'Kiel' }, tint: 'teal' }
const stored: Layout = {
  v: 1,
  cards: [
    { id: 'bb', type: 'body-battery', size: 'M', options: {} },
    { id: 'sl', type: 'sleep', size: 'M', options: {} },
    { id: 'st', type: 'steps', size: 'S', options: {} },
    unknown as Layout['cards'][number],
  ],
}

interface Put {
  at: number
  ifMatch: string | null
  body: Layout
}

function serve({ conflict = false } = {}) {
  const puts: Put[] = []
  let value: Layout = stored
  let rev = 3
  stubApi({
    '/api/me/settings/glance.layout': () => json({ value, rev })(),
    '/api/daily': json([]),
    '/api/sleep': json([]),
    '/api/series/body_battery': json({ metric: 'body_battery', unit: '%', total: false, from: 0, to: 1, bucket_s: 0, t: [], v: [] }),
    '/api/activities': json({ items: [] }),
    '/api/coach': json([]),
    '/api/devices': json([]),
  })
  const fetchStub = globalThis.fetch as unknown as ReturnType<typeof vi.fn>
  const base = fetchStub.getMockImplementation() as (url: string, init?: RequestInit) => Promise<Response>
  fetchStub.mockImplementation(async (url: string, init?: RequestInit) => {
    if (init?.method === 'PUT') {
      const body = JSON.parse(String(init.body)) as Layout
      puts.push({ at: Date.now(), ifMatch: new Headers(init.headers).get('If-Match'), body })
      if (conflict) {
        // Another device saved first: the server keeps its version.
        value = { v: 1, cards: [{ id: 'other', type: 'stress', size: 'S', options: {} }] }
        rev = 9
        return json({ error: 'the setting changed on another device' }, 412)()
      }
      value = body
      rev++
      return json({ value, rev })()
    }
    return base(url, init)
  })
  return puts
}

function Harness() {
  const [slot, setSlot] = useState<HTMLDivElement | null>(null)
  return (
    <ToastProvider>
      <PeriodsContext.Provider value={['day']}>
        <div ref={setSlot} data-testid="header" />
        <HeaderSlotContext.Provider value={slot}>
          <GlancePage />
        </HeaderSlotContext.Provider>
      </PeriodsContext.Provider>
    </ToastProvider>
  )
}

const order = () => Array.from(document.querySelectorAll<HTMLElement>('[data-card-id]')).map((e) => e.dataset.cardId)

beforeEach(() => {
  // jsdom lays nothing out: give each card a box in a row, so the keyboard sensor can aim.
  const box = (x: number, w: number, h: number) => ({ x, y: 0, left: x, top: 0, width: w, height: h, right: x + w, bottom: h, toJSON() {} })
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
    const host = this.closest?.('[data-card-id]')
    if (!host) return box(0, 0, 0) as DOMRect
    return box(Array.from(document.querySelectorAll('[data-card-id]')).indexOf(host) * 220, 200, 140) as DOMRect
  })
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('glance editor', () => {
  it('shows the stored layout, unknown cards included', async () => {
    serve()
    renderWith(<Harness />, { me: fixtureMe() })
    await screen.findByRole('article', { name: 'Body Battery' })
    expect(order()).toEqual(['bb', 'sl', 'st', 'radar'])
    expect(screen.getByRole('article', { name: 'Unknown card' })).toBeTruthy()
    expect(screen.queryByRole('article', { name: 'HRV' })).toBeNull()
  })

  it('moves a card with the keyboard and saves once, after the pause, against the stored revision', async () => {
    const puts = serve()
    renderWith(<Harness />, { path: '/?edit=1' })
    const grip = await screen.findByRole('button', { name: 'Move Body Battery' })
    const t0 = Date.now()
    // The keyboard sensor listens for the next keys from the following tick on.
    const tick = () => act(() => new Promise((r) => setTimeout(r, 20)))
    grip.focus()
    fireEvent.keyDown(grip, { key: ' ', code: 'Space' })
    await tick()
    fireEvent.keyDown(grip, { key: 'ArrowRight', code: 'ArrowRight' })
    await tick()
    fireEvent.keyDown(grip, { key: ' ', code: 'Space' })
    await waitFor(() => expect(order()).toEqual(['sl', 'bb', 'st', 'radar']))
    expect(puts).toHaveLength(0)
    await waitFor(() => expect(puts).toHaveLength(1))
    expect(puts[0].at - t0).toBeGreaterThanOrEqual(700)
    expect(puts[0].ifMatch).toBe('"3"')
    expect(puts[0].body.cards.map((c) => c.id)).toEqual(['sl', 'bb', 'st', 'radar'])
    expect(puts[0].body.cards[3]).toEqual(unknown) // saved back untouched
    await screen.findByText('Saved')
  })

  it('removes with Undo, resizes, and saves the end result once', async () => {
    const puts = serve()
    renderWith(<Harness />, { path: '/?edit=1' })
    fireEvent.click(await screen.findByRole('button', { name: 'Remove Sleep' }))
    expect(order()).toEqual(['bb', 'st', 'radar'])
    fireEvent.click(await screen.findByRole('button', { name: 'Undo' }))
    expect(order()).toEqual(['bb', 'sl', 'st', 'radar'])
    const sizes = screen.getByRole('radiogroup', { name: 'Size of Body Battery' })
    fireEvent.click(within(sizes).getByRole('radio', { name: 'Large' }))
    await waitFor(() => expect(puts).toHaveLength(1))
    expect(puts[0].body.cards.map((c) => `${c.id}:${c.size}`)).toEqual(['bb:L', 'sl:M', 'st:S', 'radar:L'])
  })

  it('shows the other device’s version when the save conflicts', async () => {
    const puts = serve({ conflict: true })
    renderWith(<Harness />, { path: '/?edit=1' })
    fireEvent.click(await screen.findByRole('button', { name: 'Remove Steps' }))
    await waitFor(() => expect(puts).toHaveLength(1))
    await screen.findByText(/changed on another device/)
    await waitFor(() => expect(order()).toEqual(['other']))
  })

  it('adds a card from the catalog and puts the keyboard on it', async () => {
    serve()
    renderWith(<Harness />, { path: '/?edit=1' })
    fireEvent.click(await screen.findByRole('button', { name: 'Add card' }))
    const sheet = await screen.findByRole('dialog', { name: 'Add a card' })
    // Singletons already placed cannot be added twice; unknown types do not appear.
    fireEvent.change(within(sheet).getByPlaceholderText('Search cards…'), { target: { value: 'breath' } })
    fireEvent.click(within(sheet).getByRole('button', { name: 'Add Breathing' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Add a card' })).toBeNull())
    await waitFor(() => expect(document.activeElement?.getAttribute('aria-label')).toBe('Move Breathing'))
    expect(order().length).toBe(5)
  })

  it('saves at once and leaves edit mode on Done', async () => {
    const puts = serve()
    renderWith(<Harness />, { path: '/?edit=1' })
    fireEvent.click(await screen.findByRole('button', { name: 'Remove Steps' }))
    fireEvent.click(screen.getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(puts).toHaveLength(1))
    expect(where.path).toBe('/')
    expect(screen.getByRole('button', { name: /Customize/ })).toBeTruthy()
  })
})
