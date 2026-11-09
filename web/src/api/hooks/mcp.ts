// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { request } from '../client'
import { keys } from '../keys'
import type { McpCreated, McpOverview, McpScope } from '../types'

/** useMcp is the Settings › MCP tab: the endpoint, the tokens, recent calls and the catalogue. */
export function useMcp() {
  return useQuery({ queryKey: keys.mcp, queryFn: () => request<McpOverview>('GET', '/me/mcp'), refetchInterval: 30_000 })
}

export interface NewToken {
  name: string
  scope: McpScope
  expires_days: number
}

/** useCreateMcpToken makes a token; the answer holds it once. */
export function useCreateMcpToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (t: NewToken) => request<McpCreated>('POST', '/me/mcp/tokens', t),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.mcp }),
  })
}

export function useRenameMcpToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, name }: { id: number; name: string }) => request<void>('PATCH', `/me/mcp/tokens/${id}`, { name }),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.mcp }),
  })
}

export function useRevokeMcpToken() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => request<void>('DELETE', `/me/mcp/tokens/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.mcp }),
  })
}
