// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PrefsProvider } from '../app/PrefsProvider'
import { fixtureMe, json, renderWith, stubApi } from '../test/fixtures'
import { ToastProvider } from '../ui/Toast'
import { GettingStarted } from './GettingStarted'

afterEach(() => vi.unstubAllGlobals())

const me = fixtureMe({ timezone: 'UTC', server: { version: 'dev', sync: true, tiles: false } })

function api(over: Record<string, (u: URL) => Response> = {}) {
  const puts: unknown[] = []
  const fn = stubApi({
    '/api/me/settings/prefs': json({ value: null, rev: 0 }),
    '/api/me/watch-profile': json({ settings: null, goals: [] }),
    '/api/me/passkeys': json([]),
    '/api/devices': json([]),
    '/api/daily': json([]),
    ...over,
  })
  const base = fn.getMockImplementation() as (url: string, init?: RequestInit) => Promise<Response>
  fn.mockImplementation(async (url: string, init?: RequestInit) => {
    if (init?.method === 'PUT') {
      puts.push(JSON.parse(String(init.body)))
      return json({ value: JSON.parse(String(init.body)), rev: 1 })()
    }
    return base(url, init)
  })
  return puts
}

const show = () => renderWith(<ToastProvider><PrefsProvider><GettingStarted me={me} /></PrefsProvider></ToastProvider>, { me })

describe('GettingStarted', () => {
  it('guides a new account and goes when dismissed', async () => {
    const puts = api()
    show()
    const card = await screen.findByRole('region', { name: 'Getting started' })
    expect(within(card).getByText('0 of 4 done')).toBeTruthy()
    expect(within(card).getByRole('link', { name: 'Add a passkey' }).getAttribute('href')).toBe('/settings/security')
    expect(within(card).getByRole('link', { name: 'Add a watch' }).getAttribute('href')).toBe('/devices/add')
    fireEvent.click(within(card).getByRole('button', { name: 'Hide getting started' }))
    await waitFor(() => expect(puts).toEqual([{ v: 1, onboarding_done: true }]))
    await waitFor(() => expect(screen.queryByRole('region', { name: 'Getting started' })).toBeNull())
  })

  it('stays away once the account is set up', async () => {
    api({
      '/api/me/passkeys': json([{ id: 'k', name: 'Phone' }]),
      '/api/devices': json([{ id: 1, current: true, sync: { last_good: Math.floor(Date.now() / 1000) - 600 } }]),
    })
    show()
    await screen.findByText((_, el) => el?.tagName === 'BODY')
    await new Promise((r) => setTimeout(r, 50))
    expect(screen.queryByRole('region', { name: 'Getting started' })).toBeNull()
  })
})
