// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import { useEffect, type ReactNode } from 'react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { vi } from 'vitest'
import { keys } from '../api/keys'
import type { Me } from '../api/types'

/** A signed-in account; times are relative to now. */
export function fixtureMe(over: Partial<Me> = {}): Me {
  return {
    id: 1, email: 'a@example.invalid', display_name: 'Ada', timezone: '', units: 'metric', is_admin: true,
    created_at: new Date().toISOString(), server: { version: 'dev', sync: false, tiles: false }, ...over,
  }
}

export const json = (v: unknown, status = 200) => () =>
  new Response(JSON.stringify(v), { status, headers: { 'Content-Type': 'application/json' } })

/** stubApi answers fetch by path; unknown paths are 404. */
export function stubApi(routes: Record<string, (url: URL) => Response>) {
  const fn = vi.fn(async (url: string) => {
    const u = new URL(url, 'http://localhost')
    const h = routes[u.pathname]
    return h ? h(u) : new Response(JSON.stringify({ error: 'no route' }), { status: 404 })
  })
  vi.stubGlobal('fetch', fn)
  return fn
}

export const where = { path: '' }
function Where() {
  const l = useLocation()
  // Record only what was committed: a navigation that suspends renders without committing.
  useEffect(() => {
    where.path = l.pathname + l.search
  })
  return null
}

/** renderWith renders inside a router and a query client that already knows the account. */
export function renderWith(ui: ReactNode, { path = '/', me = fixtureMe() }: { path?: string; me?: Me } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  qc.setQueryData(keys.me, me)
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        {ui}
        <Where />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}
