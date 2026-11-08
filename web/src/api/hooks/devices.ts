// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { request } from '../client'
import { keys } from '../keys'
import type { AdminWatch, AfterFetch, BluetoothState, Device, DeviceSummary, LogPage, WatchFile } from '../types'
import { useDevices } from './data'

/** useDevice is one watch out of the polled list (so it updates while it syncs). */
export function useDevice(id: number) {
  const q = useDevices()
  return { ...q, device: q.data?.find((d) => d.id === id) }
}

/** useSyncNow asks for a sync; the watch shows as syncing at once. */
export function useSyncNow() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { id: number; includeSynced?: boolean }) =>
      request<Device>('POST', `/devices/${v.id}/sync`, v.includeSynced ? { include_synced: true } : undefined),
    onMutate: ({ id }) => {
      qc.setQueryData<Device[]>(keys.devices, (list) =>
        list?.map((d) => (d.id === id && d.sync ? { ...d, sync: { ...d.sync, running: true, phase: 'connecting', done: 0, total: 0 } } : d)))
    },
    onSettled: () => qc.invalidateQueries({ queryKey: keys.devices, exact: true }),
  })
}

export interface DeviceUpdate {
  nickname?: string
  after_fetch?: AfterFetch
  paused?: boolean
  usb_auto?: boolean
}

export function useUpdateDevice(id: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: DeviceUpdate) => request<Device>('PATCH', `/devices/${id}`, v),
    onSuccess: (d) => qc.setQueryData<Device[]>(keys.devices, (list) => list?.map((x) => (x.id === id ? { ...x, ...d, sync: x.sync } : x))),
  })
}

export function useDeviceSummary(id: number) {
  return useQuery({ queryKey: keys.deviceSummary(id), queryFn: () => request<DeviceSummary>('GET', `/devices/${id}/summary`) })
}

export function useDeviceFiles(id: number) {
  return useQuery({ queryKey: keys.deviceFiles(id), queryFn: () => request<WatchFile[]>('GET', `/devices/${id}/files`) })
}

/** useDeleteFromWatch deletes files from the watch at its next connection (they stay in goarmin). */
export function useDeleteFromWatch(id: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (uids: string[]) => request<void>('POST', `/devices/${id}/files/delete`, { uids }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.devices, exact: true })
    },
  })
}

/** useSyncLog pages through the sync log (device 0 = every watch). */
export function useSyncLog(device = 0, problems = false, limit = 50) {
  return useInfiniteQuery({
    queryKey: keys.syncLog(device, problems, limit),
    initialPageParam: 0,
    queryFn: ({ pageParam }) => {
      const q = new URLSearchParams({ limit: String(limit) })
      if (device) q.set('device', String(device))
      if (problems) q.set('problems', '1')
      if (pageParam) q.set('before', String(pageParam))
      return request<LogPage>('GET', `/sync-runs?${q}`)
    },
    getNextPageParam: (last) => last.next_before,
  })
}

export function useBluetooth(enabled: boolean) {
  return useQuery({
    queryKey: keys.bluetooth,
    queryFn: () => request<BluetoothState>('GET', '/bluetooth'),
    enabled,
    refetchInterval: 15_000,
  })
}

export function usePowerOn() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => request<BluetoothState>('POST', '/bluetooth/power', { on: true }),
    onSuccess: (b) => qc.setQueryData(keys.bluetooth, b),
  })
}

function useDeviceChange<V, R>(fn: (v: V) => Promise<R>) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: fn,
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: keys.devices })
      void qc.invalidateQueries({ queryKey: keys.health })
      void qc.invalidateQueries({ queryKey: keys.training })
    },
  })
}

/** useUnpair removes this computer's pairing; the watch stays yours. */
export function useUnpair(id: number) {
  return useDeviceChange(() => request<Device>('POST', `/devices/${id}/unpair`))
}

/** useRelease hands the watch over (optionally unpairing it). */
export function useRelease(id: number) {
  return useDeviceChange((unpair: boolean) => request<Device>('POST', `/devices/${id}/release`, { unpair }))
}

export interface DeletedData {
  files: number
  activities: number
  nights: number
  days: number
  syncs: number
}

/** useDeleteData deletes what came from the watch into your account; confirm is its name. */
export function useDeleteData(id: number) {
  return useDeviceChange((confirm: string) => request<DeletedData>('POST', `/devices/${id}/delete-data`, { confirm }))
}

export function useAdminWatches() {
  return useQuery({ queryKey: ['admin', 'devices'], queryFn: () => request<AdminWatch[]>('GET', '/admin/devices') })
}

export function useAdminWatchAction() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { id: number; action: 'release' | 'forget' }) => request<void>('POST', `/admin/devices/${v.id}/${v.action}`),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ['admin', 'devices'] })
      void qc.invalidateQueries({ queryKey: keys.devices })
    },
  })
}
