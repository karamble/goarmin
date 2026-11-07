// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen, within } from '@testing-library/react'
import { useState, type ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Device } from '../../api/types'
import { HeaderSlotContext } from '../../app/section/HeaderSlot'
import { fixtureMe, json, renderWith, stubApi } from '../../test/fixtures'
import LivePage from './Live'

afterEach(() => vi.unstubAllGlobals())

const now = Math.floor(Date.now() / 1000)

function device(id: number, nickname: string): Device {
  return {
    id, unit_id: 3_500_000_000 + id, product: 4536, model: 'Fenix 8', name: 'fenix 8 - 47mm', nickname, firmware: '23.40', address: `AA:BB:CC:DD:EE:0${id}`,
    after_fetch: 'archive', paused: false, delete_support: '', battery: 80, current: true, owned_from: 0, owned_to: null, paired: true,
    sync: { address: `AA:BB:CC:DD:EE:0${id}`, running: false, live: false, last_ok: true, pulled: 0, imported: 0, failures: 0, away: false },
  } as unknown as Device
}

function sse(events: string[]): () => Response {
  return () => new Response(new ReadableStream<Uint8Array>({
    start(c) {
      const enc = new TextEncoder()
      for (const e of events) c.enqueue(enc.encode(e))
      c.close()
    },
  }), { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
}

const reading = (name: string, value: number, t = now) => `event: reading\ndata: ${JSON.stringify({ name, value, unit: '', t })}\n\n`

function Page({ children }: { children: ReactNode }) {
  const [slot, setSlot] = useState<HTMLDivElement | null>(null)
  return (
    <>
      <div ref={setSlot} data-testid="header" />
      <HeaderSlotContext.Provider value={slot}>{children}</HeaderSlotContext.Provider>
    </>
  )
}

const withSync = fixtureMe({ timezone: 'UTC', server: { version: 'dev', sync: true, tiles: false } })

describe('Live', () => {
  it('opens by itself, shows every value, and offers to keep going after the time limit', async () => {
    const asked: string[] = []
    stubApi({
      '/api/devices': json([device(1, 'Fenix'), device(2, 'Spare')]),
      '/api/activities': json({ items: [] }),
      '/api/devices/2/live': (u) => {
        asked.push(u.pathname)
        return sse([
          `event: hello\ndata: {"since":${now - 120}}\n\n`,
          reading('heart_rate', 72, now - 2), reading('resting_hr', 55), reading('heart_rate', 74), reading('stress', 31), reading('body_battery', 64),
          reading('respiration', 15), reading('steps', 4200), reading('step_goal', 8000), reading('calories', 2355), reading('active_calories', 176),
          reading('floors_up', 10), reading('floors_down', 13), reading('floors_goal', 10), reading('intensity_goal', 150), reading('spo2', 96, now - 600),
          ...Array.from({ length: 20 }, (_, i) => reading('rr_interval', i % 2 ? 800 : 820, now - 20 + i)),
          `event: end\ndata: {"reason":"time_limit"}\n\n`,
        ])()
      },
    })
    renderWith(<Page><LivePage /></Page>, { path: '/live?watch=2', me: withSync })
    const tiles = await screen.findByRole('list', { name: 'Live values' })
    expect(await within(tiles).findByText('74')).toBeTruthy()
    expect(asked).toEqual(['/api/devices/2/live'])
    for (const text of ['Low', '64', '15', '4,200', '2,355', '176 active', '10', '150', '96', '20']) {
      expect(within(tiles).getAllByText(text).length).toBeGreaterThan(0)
    }
    expect(within(tiles).getByText(/resting 55/)).toBeTruthy()
    expect(screen.getByRole('img', { name: /^Heart rate now 74 bpm/ })).toBeTruthy()
    expect(await screen.findByText(/paused after 30 minutes/)).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Keep going' })).toBeTruthy()
    // Two watches: a picker in the header.
    const picker = within(screen.getByTestId('header')).getByRole('combobox', { name: 'Watch' }) as HTMLSelectElement
    expect(picker.value).toBe('2')
    fireEvent.click(screen.getByRole('button', { name: 'Keep going' }))
    expect(asked).toHaveLength(2)
  })

  it('says what holds Bluetooth when the watch is busy', async () => {
    stubApi({
      '/api/devices': json([device(1, 'Fenix')]),
      '/api/activities': json({ items: [] }),
      '/api/devices/1/live': json({ error: 'The watch is syncing; try again in a minute.', busy: { kind: 'sync', since: now } }, 409),
    })
    renderWith(<Page><LivePage /></Page>, { path: '/live', me: withSync })
    expect(await screen.findByText('The watch is syncing; try again in a minute.')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy()
  })

  it('explains a server without the sync role, and an account without a watch', async () => {
    stubApi({ '/api/devices': json([]), '/api/activities': json({ items: [] }) })
    const { unmount } = renderWith(<Page><LivePage /></Page>, { path: '/live', me: fixtureMe() })
    expect(await screen.findByText('Live needs the sync role')).toBeTruthy()
    unmount()
    renderWith(<Page><LivePage /></Page>, { path: '/live', me: withSync })
    expect(await screen.findByText('No watch to watch')).toBeTruthy()
  })
})
