// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useEffect } from 'react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Me } from '../api/types'
import { App } from './App'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const me: Me = {
  id: 1, email: 'a@example.invalid', display_name: 'Ada', timezone: '', units: 'metric', is_admin: true,
  created_at: new Date().toISOString(), server: { version: 'dev', sync: false, tiles: false },
}

function api(routes: Record<string, () => Response>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const path = new URL(url, 'http://localhost').pathname
    const h = routes[path]
    return h ? h() : new Response(JSON.stringify({ error: 'no route' }), { status: 404 })
  }))
}
const json = (v: unknown, status = 200) => () => new Response(JSON.stringify(v), { status, headers: { 'Content-Type': 'application/json' } })

let where = ''
function Where() {
  const l = useLocation()
  // Record only what was committed: a navigation that suspends renders without committing.
  useEffect(() => {
    where = l.pathname + l.search
  })
  return null
}

function renderAt(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <App />
        <Where />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('App', () => {
  it('sends a signed-out visitor to sign-in, keeping where they wanted to go', async () => {
    api({ '/api/me': json({ error: 'sign in first' }, 401), '/api/setup': json({ needed: false }) })
    renderAt('/health/hrv')
    await screen.findByRole('heading', { name: 'Sign in' })
    expect(where).toBe('/login?next=%2Fhealth%2Fhrv')
  })

  it('offers the setup on a fresh server', async () => {
    api({ '/api/me': json({ error: 'sign in first' }, 401), '/api/setup': json({ needed: true }) })
    renderAt('/')
    await screen.findByRole('heading', { name: 'Create your account' })
  })

  it('shows the signed-in shell with section tabs that carry the date', async () => {
    api({ '/api/me': json(me) })
    const day = new Date().toISOString().slice(0, 10)
    renderAt(`/health?d=${day}&p=week`)
    await waitFor(() => expect(where.startsWith('/health/sleep')).toBe(true))
    const rail = screen.getAllByRole('navigation', { name: 'Sections' })[0]
    expect(rail.textContent).toContain('Health')
    expect(rail.textContent).not.toContain('Live') // no sync role on this server
    const tabs = screen.getByRole('navigation', { name: 'Health sections' })
    const hrv = Array.from(tabs.querySelectorAll('a')).find((a) => a.textContent === 'HRV')!
    expect(hrv.getAttribute('href')).toBe(`/health/hrv?d=${day}&p=week`)
    expect(screen.getByRole('heading', { level: 1 }).textContent).toBe('Sleep')
  })

  it('signs in with a password and lands where the visitor wanted to go', async () => {
    let signedIn = false
    vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
      const path = new URL(url, 'http://localhost').pathname
      if (path === '/api/auth/login' && init?.method === 'POST') {
        signedIn = true
        return json(me)()
      }
      if (path === '/api/me') return signedIn ? json(me)() : json({ error: 'sign in first' }, 401)()
      if (path === '/api/setup') return json({ needed: false })()
      return json([])()
    }))
    renderAt('/login?next=%2Fsettings%2Fprofile')
    fireEvent.change(await screen.findByLabelText(/email/i), { target: { value: 'a@example.invalid' } })
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: 'a long enough password' } })
    fireEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    await waitFor(() => expect(where).toBe('/settings/profile'))
    await screen.findByRole('heading', { level: 1, name: 'Profile' })
  })
})
