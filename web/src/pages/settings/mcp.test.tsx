// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { McpItem, McpOverview, McpToken } from '../../api/types'
import { json, renderWith, stubApi } from '../../test/fixtures'
import { ToastProvider } from '../../ui/Toast'
import Mcp, { snippets } from './Mcp'

afterEach(() => vi.unstubAllGlobals())

const now = Math.floor(Date.now() / 1000)
const endpoint = 'http://localhost:8642/mcp'

const token = (o: Partial<McpToken> = {}): McpToken => ({
  id: 1, name: 'Claude laptop', prefix: 'gma_Ab3x', scope: 'write', created_at: now - 86400 * 3, expires_at: now + 86400 * 87,
  last_used_at: now - 120, revoked_at: null, live: true, calls_24h: 12, ...o,
})

const catalogue: McpItem[] = [
  { kind: 'tool', name: 'get_daily', title: 'Daily summaries', description: 'One summary per local day.', area: 'Health', level: 'read', read_only: true },
  { kind: 'tool', name: 'plan_workout', title: 'Plan a workout', description: 'Put a library workout on a day.', area: 'Calendar', level: 'write' },
  { kind: 'tool', name: 'delete_device_data', title: 'Delete a device’s data', description: 'Delete everything from a device.', area: 'Devices', level: 'write', destructive: true },
  { kind: 'tool', name: 'list_users', title: 'Users', description: 'The server’s accounts.', area: 'Admin', level: 'admin', read_only: true },
  { kind: 'resource', name: 'activity_fit', title: 'Activity FIT file', description: 'The original FIT file.', area: 'Files', level: 'read', uri: 'goarmin://activities/{id}/fit' },
  { kind: 'prompt', name: 'review_week', title: 'Review my week', description: 'A look back at seven days.', area: 'Prompts', level: 'read' },
]

const overview = (o: Partial<McpOverview> = {}): McpOverview => ({
  enabled: true, endpoint, secure: false, scopes: ['read', 'write'], retention_days: 30, catalogue,
  limits: { calls_per_minute: 120, destructive_per_minute: 10 },
  tokens: [token(), token({ id: 2, name: 'Old one', scope: 'read', live: false, revoked_at: now - 3600, calls_24h: 0 })],
  calls: [{ id: 5, token_id: 1, token: 'Claude laptop', at: now - 60, tool: 'get_daily', ok: true, ms: 14, error: '' },
    { id: 4, token_id: 1, token: 'Claude laptop', at: now - 90, tool: 'sync_device', ok: false, ms: 3, error: 'The watch is syncing (HTTP 409)' }],
  ...o,
})

function api(o: Partial<McpOverview> = {}) {
  const writes: { method: string; path: string; body: unknown }[] = []
  const fn = stubApi({ '/api/me/mcp': json(overview(o)) })
  const base = fn.getMockImplementation() as (url: string, init?: RequestInit) => Promise<Response>
  fn.mockImplementation(async (url: string, init?: RequestInit) => {
    if (init?.method && init.method !== 'GET') {
      const path = new URL(url, 'http://localhost').pathname
      const body = init.body ? JSON.parse(String(init.body)) : undefined
      writes.push({ method: init.method, path, body })
      if (init.method === 'POST') return json({ token: 'gma_SECRETSECRET', item: token({ id: 3, name: body.name, scope: body.scope }) }, 201)()
      return new Response(null, { status: 204 })
    }
    return base(url, init)
  })
  return writes
}

const page = () => renderWith(<ToastProvider><Mcp /></ToastProvider>)

describe('Settings › MCP', () => {
  it('shows the endpoint, the tokens, the calls and a plain-HTTP note', async () => {
    api()
    page()
    expect(((await screen.findByRole('textbox', { name: 'Endpoint' })) as HTMLInputElement).value).toBe(endpoint)
    expect(screen.getByText(/tokens travel unencrypted/)).toBeTruthy()
    const list = screen.getByRole('list', { name: 'Tokens' })
    expect(within(list).getByText('Claude laptop')).toBeTruthy()
    expect(within(list).getByText('Read and write')).toBeTruthy()
    expect(within(list).getByText(/revoked/)).toBeTruthy()
    expect(within(list).queryByRole('button', { name: 'Revoke Old one' })).toBeNull()
    expect(screen.getByText('The watch is syncing (HTTP 409)')).toBeTruthy()
  })

  it('makes a token and shows it once with ready snippets', async () => {
    const writes = api()
    page()
    const form = await screen.findByRole('region', { name: 'New token' })
    fireEvent.change(within(form).getByRole('textbox', { name: 'Name' }), { target: { value: 'Desktop' } })
    fireEvent.click(within(form).getByRole('radio', { name: 'Read and write' }))
    fireEvent.click(within(form).getByRole('radio', { name: 'Never' }))
    expect(within(form).queryByRole('radio', { name: 'Admin' })).toBeNull()
    fireEvent.click(within(form).getByRole('button', { name: /Make token/ }))
    await waitFor(() => expect(writes).toContainEqual({ method: 'POST', path: '/api/me/mcp/tokens', body: { name: 'Desktop', scope: 'write', expires_days: 0 } }))
    const sheet = await screen.findByRole('dialog', { name: 'Your new token' })
    expect((within(sheet).getByRole('textbox', { name: 'Token' }) as HTMLInputElement).value).toBe('gma_SECRETSECRET')
    expect((within(sheet).getByRole('textbox', { name: 'Claude Code' }) as HTMLInputElement).value).toContain(`--header "Authorization: Bearer gma_SECRETSECRET"`)
    fireEvent.click(within(sheet).getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(screen.queryByText('gma_SECRETSECRET')).toBeNull())
    expect(screen.queryByDisplayValue('gma_SECRETSECRET')).toBeNull()
  })

  it('renames and revokes', async () => {
    const writes = api()
    page()
    fireEvent.click(await screen.findByRole('button', { name: 'Rename Claude laptop' }))
    const field = screen.getAllByRole('textbox', { name: 'Name' }).find((f) => (f as HTMLInputElement).value === 'Claude laptop')!
    fireEvent.change(field, { target: { value: 'Laptop' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(writes).toContainEqual({ method: 'PATCH', path: '/api/me/mcp/tokens/1', body: { name: 'Laptop' } }))
    fireEvent.click(screen.getByRole('button', { name: 'Revoke Claude laptop' }))
    fireEvent.click(within(await screen.findByRole('alertdialog', { name: 'Revoke this token?' })).getByRole('button', { name: 'Revoke' }))
    await waitFor(() => expect(writes).toContainEqual({ method: 'DELETE', path: '/api/me/mcp/tokens/1', body: undefined }))
  })

  it('lists the catalogue by access and search', async () => {
    api()
    page()
    const cat = await screen.findByRole('region', { name: 'What MCP can do' })
    expect(within(cat).getByText('plan_workout')).toBeTruthy()
    expect(within(cat).queryByText('list_users')).toBeNull()
    expect(within(cat).getByText(/removes data/)).toBeTruthy()
    fireEvent.click(within(cat).getByRole('radio', { name: 'Read' }))
    expect(within(cat).queryByText('plan_workout')).toBeNull()
    expect(within(cat).getByText('goarmin://activities/{id}/fit')).toBeTruthy()
    fireEvent.change(within(cat).getByRole('searchbox', { name: 'Search the catalogue' }), { target: { value: 'week' } })
    expect(within(cat).getByText('review_week')).toBeTruthy()
    expect(within(cat).queryByText('get_daily')).toBeNull()
  })

  it('says when MCP is off', async () => {
    api({ enabled: false, tokens: [] })
    page()
    expect(await screen.findByText(/MCP is off on this server/)).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Make token/ })).toBeNull()
  })
})

describe('snippets', () => {
  it('fills in the endpoint and the token', () => {
    const s = snippets(endpoint, 'gma_x')
    expect(JSON.parse(s.http).mcpServers.goarmin).toEqual({ type: 'http', url: endpoint, headers: { Authorization: 'Bearer gma_x' } })
    expect(JSON.parse(s.stdio).mcpServers.goarmin.args).toEqual(['mcp', '-server', 'http://localhost:8642'])
  })
})
