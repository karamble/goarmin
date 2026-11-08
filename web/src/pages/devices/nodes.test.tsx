// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { SyncNode } from '../../api/types'
import { json, renderWith, stubApi } from '../../test/fixtures'
import { ToastProvider } from '../../ui/Toast'
import Nodes from './Nodes'

const sec = (ms: number) => Math.floor((Date.now() + ms) / 1000)

function node(over: Partial<SyncNode> = {}): SyncNode {
  return {
    id: 7, name: 'Laptop', hostname: 'laptop', user_id: 1, created_at: sec(-86_400_000), last_seen: sec(-5_000), version: 'v1',
    revoked: false, online: true, adapter: { name: 'hci0', powered: true, power_state: 'on' },
    watches: [{ address: 'C0:FF:EE:12:34:56', unit: 1, product: 4536, name: 'Fenix 8', firmware: '23.40' }], ...over,
  }
}

afterEach(() => vi.unstubAllGlobals())

describe('Sync nodes', () => {
  it('lists the nodes with their watches and approves a code', async () => {
    const list = [node(), node({ id: 8, name: 'Old Pi', online: false, last_seen: sec(-7200_000), watches: [] }), node({ id: 9, name: 'Gone', revoked: true })]
    const fetchFn = stubApi({ '/api/nodes': json(list), '/api/nodes/approve': json(node({ id: 10, name: 'Pi' })) })
    renderWith(<ToastProvider><Nodes /></ToastProvider>)
    expect(await screen.findByText('Laptop')).toBeTruthy()
    expect(screen.getByText('Fenix 8')).toBeTruthy()
    expect(screen.getByText(/Offline, last heard from 2 h ago/)).toBeTruthy()
    expect(screen.getByText(/Signed out \(1\)/)).toBeTruthy()

    const input = screen.getByPlaceholderText('ABCD-EFGH')
    fireEvent.change(input, { target: { value: 'kq4m-7xrt' } })
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }))
    await waitFor(() => expect(fetchFn.mock.calls.some((c) => {
      const init = (c as unknown[])[1] as RequestInit | undefined
      return String(c[0]).endsWith('/api/nodes/approve') && init?.method === 'POST' && String(init.body).includes('KQ4M-7XRT')
    })).toBe(true))
  })
})
