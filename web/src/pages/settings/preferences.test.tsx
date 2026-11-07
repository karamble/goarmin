// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PrefsProvider } from '../../app/PrefsProvider'
import { setHourCycle } from '../../lib/format'
import { setWeekStart } from '../../lib/period'
import { fixtureMe, json, renderWith, stubApi } from '../../test/fixtures'
import { ToastProvider } from '../../ui/Toast'
import { Preferences } from './Preferences'

afterEach(() => {
  vi.unstubAllGlobals()
  setWeekStart(1)
  setHourCycle(undefined)
})

function api(stored: unknown, putStatus = 200) {
  const puts: { body: unknown; ifMatch: string | null }[] = []
  const fn = stubApi({
    '/api/me/settings/prefs': json({ value: stored, rev: stored ? 3 : 0 }),
    '/api/me/watch-profile': json({ settings: { clock: '24h', units: { distance: 'statute' }, wake_s: -1, sleep_s: -1 }, goals: [{ type: 'active_minutes', sport: '', target: 150, unit: 'min' }] }),
    '/api/me/zones': json({ default: { max_hr: 183, hr: [92, 110, 128, 146, 165, 183] }, profiles: [] }),
    '/api/activities': json({ items: [{ id: 1, max_hr: 178 }] }),
  })
  const base = fn.getMockImplementation() as (url: string, init?: RequestInit) => Promise<Response>
  fn.mockImplementation(async (url: string, init?: RequestInit) => {
    if (init?.method === 'PUT') {
      puts.push({ body: JSON.parse(String(init.body)), ifMatch: new Headers(init.headers).get('If-Match') })
      return putStatus === 200 ? json({ value: JSON.parse(String(init.body)), rev: 4 })() : json({ error: 'changed' }, putStatus)()
    }
    return base(url, init)
  })
  return puts
}

const page = () => renderWith(<ToastProvider><PrefsProvider><Preferences /></PrefsProvider></ToastProvider>, { me: fixtureMe({ units: 'metric' }) })

describe('Preferences', () => {
  it('starts as the watch, and saves only what differs from the defaults', async () => {
    const puts = api(null)
    page()
    expect(await screen.findByRole('radio', { name: 'As the watch (24 h)' })).toBeTruthy()
    expect(screen.getByText(/The watch's zones: 92 \/ 110 \/ 128 \/ 146 \/ 165 bpm, maximum 183/)).toBeTruthy()
    expect(screen.getByText(/Your watch shows miles/)).toBeTruthy()
    fireEvent.click(screen.getByRole('radio', { name: 'Sunday' }))
    fireEvent.click(screen.getByRole('radio', { name: '12 h' }))
    fireEvent.click(screen.getByRole('radio', { name: 'Estimate' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Use 178 bpm as my maximum' }))
    fireEvent.change(screen.getByRole('textbox', { name: 'Zone 1 starts at' }), { target: { value: '90' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save preferences' }))
    await waitFor(() => expect(puts).toHaveLength(1))
    expect(puts[0].ifMatch).toBe('"0"')
    // Only one of five limits typed: the usual shares of the maximum apply.
    expect(puts[0].body).toEqual({ v: 1, week_start: 'sunday', clock: '12h', zones: { source: 'custom', max_hr: 178 } })
    expect(await screen.findByText('Preferences saved')).toBeTruthy()
    // The new week and clock are in effect for every page.
    expect(await screen.findByRole('radio', { name: 'Sunday', checked: true })).toBeTruthy()
  })

  it('says when another device saved first', async () => {
    const puts = api({ v: 1, intensity_goal: 200 }, 412)
    page()
    const goal = await screen.findByRole('textbox', { name: /Weekly intensity goal/ })
    expect((goal as HTMLInputElement).value).toBe('200')
    fireEvent.change(goal, { target: { value: '250' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save preferences' }))
    expect(await screen.findByText(/Changed on another device/)).toBeTruthy()
    expect(puts[0]).toEqual({ body: { v: 1, intensity_goal: 250 }, ifMatch: '"3"' })
  })
})
