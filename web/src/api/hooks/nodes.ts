// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { request } from '../client'
import { keys } from '../keys'
import type { SyncNode } from '../types'

/** useNodes lists the sync nodes (yours; every node for an admin), refreshed every 10 s. */
export function useNodes() {
  return useQuery({ queryKey: keys.nodes, queryFn: () => request<SyncNode[]>('GET', '/nodes'), refetchInterval: 10_000 })
}

function useNodeMutation<V, R>(fn: (v: V) => Promise<R>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: keys.nodes, exact: true })
      void qc.invalidateQueries({ queryKey: keys.devices, exact: true })
    },
  })
}

/** useApproveNode approves the code a computer shows after goarmin node login without an e-mail. */
export function useApproveNode() {
  return useNodeMutation((code: string) => request<SyncNode>('POST', '/nodes/approve', { code }))
}

export function useRenameNode() {
  return useNodeMutation((v: { id: number; name: string }) => request<SyncNode>('PATCH', `/nodes/${v.id}`, { name: v.name }))
}

/** useRevokeNode ends a node's sign-in: it stops syncing for this server. */
export function useRevokeNode() {
  return useNodeMutation((id: number) => request<void>('DELETE', `/nodes/${id}`))
}
