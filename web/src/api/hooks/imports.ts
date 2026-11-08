// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { finished } from '../../lib/imports'
import { request } from '../client'
import { keys } from '../keys'
import type { Device, ImportPage, ImportRun, USBWatch } from '../types'

/** useImports is the person's imports, newest first, polled while one is unfinished. When one
 * ends, the health and training pages refetch (as after a sync). */
export function useImports(limit = 20) {
  const qc = useQueryClient()
  const q = useInfiniteQuery({
    queryKey: [...keys.imports, limit],
    queryFn: ({ pageParam }) => request<ImportPage>('GET', `/imports?limit=${limit}${pageParam ? `&before=${pageParam}` : ''}`),
    initialPageParam: 0,
    getNextPageParam: (last) => last.next_before ?? undefined,
    refetchInterval: (query) => (query.state.data?.pages.some((p) => p.items.some((i) => !finished(i))) ? 1500 : false),
  })
  const items = q.data?.pages.flatMap((p) => p.items) ?? []
  const open = items.filter((i) => !finished(i)).map((i) => i.id).join(',')
  const prev = useRef<string | null>(null)
  useEffect(() => {
    if (prev.current != null && prev.current !== open && prev.current !== '') {
      void qc.invalidateQueries({ queryKey: keys.health })
      void qc.invalidateQueries({ queryKey: keys.training })
      void qc.invalidateQueries({ queryKey: keys.devices })
      void qc.invalidateQueries({ queryKey: keys.usb })
    }
    prev.current = open
  }, [open, qc])
  return { ...q, items }
}

/** useUSB is the Garmin watches plugged in at the server, polled. */
export function useUSB() {
  return useQuery({
    queryKey: keys.usb,
    queryFn: () => request<USBWatch[]>('GET', '/usb'),
    refetchInterval: (query) => (query.state.data?.some((w) => w.importing && !finished(w.importing)) ? 1500 : 5000),
  })
}

/** useImportUSB starts an import of a watch on USB. */
export function useImportUSB() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => request<ImportRun>('POST', `/usb/${encodeURIComponent(id)}/import`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.usb })
      void qc.invalidateQueries({ queryKey: keys.imports })
    },
  })
}

/** useCancelImport stops a queued or running import. */
export function useCancelImport() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => request<ImportRun>('DELETE', `/imports/${id}`),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: keys.imports })
      void qc.invalidateQueries({ queryKey: keys.usb })
    },
  })
}

/** useSetUSBAuto turns importing a watch when it is plugged in on or off. */
export function useSetUSBAuto() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { device: number; on: boolean }) => request<Device>('PATCH', `/devices/${v.device}`, { usb_auto: v.on }),
    onMutate: ({ device, on }) => {
      qc.setQueryData<USBWatch[]>(keys.usb, (list) => list?.map((w) => (w.device?.id === device ? { ...w, auto: on } : w)))
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: keys.usb })
      void qc.invalidateQueries({ queryKey: keys.devices })
    },
  })
}
