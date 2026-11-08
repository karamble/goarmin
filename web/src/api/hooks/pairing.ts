// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { isOver } from '../../lib/pairing'
import { ApiError, request } from '../client'
import { keys } from '../keys'
import { streamPairing } from '../live'
import type { AdminPairing, PairComputer, PairView } from '../types'

/** notFoundAsNull turns a 404 into null (no session), any other failure stays an error. */
async function notFoundAsNull<T>(p: Promise<T>): Promise<T | null> {
  try {
    return await p
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null
    throw e
  }
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const t = setTimeout(resolve, ms)
    signal.addEventListener('abort', () => {
      clearTimeout(t)
      resolve()
    }, { once: true })
  })
}

/** lost marks a session the server no longer knows (it restarted, or a newer one replaced it). */
function lost(qc: QueryClient) {
  qc.setQueryData<PairView | null>(keys.pairing, (v) =>
    v ? { ...v, state: 'expired', error: 'The server no longer knows this attempt (it may have restarted). Start again.', error_code: null } : v)
}

/**
 * usePairing is the person's pairing session: the one that runs or ended a little while ago
 * (null if none; one they cancelled counts as none). While it runs, its event stream keeps the
 * cache current, reconnecting after 1, 2, then 5 s if the stream breaks.
 */
export function usePairing() {
  const qc = useQueryClient()
  const q = useQuery({
    queryKey: keys.pairing,
    queryFn: async () => {
      const v = await notFoundAsNull(request<PairView>('GET', '/pairing'))
      return v && v.state === 'canceled' ? null : v
    },
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  })
  const id = q.data && !isOver(q.data) ? q.data.id : null
  useEffect(() => {
    if (!id) return
    const c = new AbortController()
    void (async () => {
      let tries = 0
      while (!c.signal.aborted) {
        let over = false
        try {
          await streamPairing<PairView>(id, (v) => {
            tries = 0
            over = isOver(v)
            qc.setQueryData(keys.pairing, v)
          }, c.signal)
        } catch (e) {
          if (c.signal.aborted) return
          if (e instanceof ApiError && e.status === 404) return lost(qc)
        }
        if (c.signal.aborted || over) return
        // The stream broke off: read the session once, then listen again.
        try {
          const v = await request<PairView>('GET', `/pairing/${encodeURIComponent(id)}`)
          qc.setQueryData(keys.pairing, v)
          if (isOver(v)) return
        } catch (e) {
          if (c.signal.aborted) return
          if (e instanceof ApiError && e.status === 404) return lost(qc)
        }
        await sleep([1000, 2000, 5000][Math.min(tries++, 2)], c.signal)
      }
    })()
    return () => c.abort()
  }, [id, qc])
  return q
}

function usePairAction<V>(fn: (v: V) => Promise<PairView>) {
  const qc = useQueryClient()
  return useMutation({ mutationFn: fn, onSuccess: (v) => qc.setQueryData(keys.pairing, v) })
}

/** usePairComputers lists the computers that can pair a watch for owner: this server, sync nodes. */
export function usePairComputers(owner: number, enabled: boolean) {
  return useQuery({
    queryKey: keys.pairComputers(owner),
    queryFn: () => request<PairComputer[]>('GET', `/pairing/computers?owner=${owner}`),
    enabled,
    refetchInterval: 15_000,
  })
}

/** useStartPairing opens a session (an admin may name its owner; node picks the computer that
 * pairs) and starts the search for watches. */
export function useStartPairing() {
  return usePairAction(async ({ owner, node }: { owner?: number; node?: number } = {}) => {
    const body = { ...(owner ? { owner } : {}), ...(node ? { node } : {}) }
    const v = await request<PairView>('POST', '/pairing', Object.keys(body).length ? body : undefined)
    // A session of yours that runs already (another tab) is resumed as it is.
    if (v.state !== 'prepare') return v
    return request<PairView>('POST', `/pairing/${encodeURIComponent(v.id)}/scan`)
  })
}

const at = (id: string, step: string) => `/pairing/${encodeURIComponent(id)}/${step}`

export function usePairScan() {
  return usePairAction((id: string) => request<PairView>('POST', at(id, 'scan')))
}

export function usePairSelect() {
  return usePairAction((v: { id: string; address: string; repair?: boolean }) =>
    request<PairView>('POST', at(v.id, 'select'), { address: v.address, repair: !!v.repair }))
}

export function usePairConfirm() {
  return usePairAction((v: { id: string; match: boolean }) => request<PairView>('POST', at(v.id, 'confirm'), { match: v.match }))
}

export function usePairPasskey() {
  return usePairAction((v: { id: string; passkey: number }) => request<PairView>('POST', at(v.id, 'passkey'), { passkey: v.passkey }))
}

export interface PairSetup {
  nickname: string
  garmin_connect: boolean
  include_synced: boolean
}

/** usePairSetup adds the identified watch; the device list learns about it at once. */
export function usePairSetup() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { id: string } & PairSetup) =>
      request<PairView>('POST', at(v.id, 'setup'), { nickname: v.nickname, garmin_connect: v.garmin_connect, include_synced: v.include_synced }),
    onSuccess: (v) => {
      qc.setQueryData(keys.pairing, v)
      void qc.invalidateQueries({ queryKey: keys.devices })
    },
  })
}

/** useCancelPairing ends the session; afterwards there is none. */
export function useCancelPairing() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => request<void>('DELETE', `/pairing/${encodeURIComponent(id)}`),
    onSuccess: () => qc.setQueryData(keys.pairing, null),
  })
}

/** useAdminPairing is the session that runs on the server, whoever started it (admins). */
export function useAdminPairing(enabled: boolean) {
  return useQuery({
    queryKey: keys.adminPairing,
    queryFn: () => notFoundAsNull(request<AdminPairing>('GET', '/admin/pairing')),
    enabled,
    refetchInterval: 10_000,
  })
}

/** useEndPairing lets an admin end whatever session runs. */
export function useEndPairing() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => request<void>('DELETE', '/admin/pairing'),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: keys.adminPairing })
      void qc.invalidateQueries({ queryKey: keys.bluetooth })
    },
  })
}
