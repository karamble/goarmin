// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { keepPreviousData, useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { request } from '../client'
import type {
  AccountDocument, AccountDocumentRow, Body, DeviceDocument, ECGRecording, ECGSummary, FileMessages, FilePage, GearRow, HealthStatusRow,
  HydrationRow, NapRow, PersonalRecordRow, SettingsChange, SolarSeries,
} from '../types'

// The hooks of what the readers store beyond days, nights and activities. Health data lives under
// ['health'] and training data under ['training'], so a sync or an import refreshes them.

export function useBody(from: string, to: string) {
  return useQuery({ queryKey: ['health', 'body', from, to], queryFn: () => request<Body>('GET', `/body?from=${from}&to=${to}`), placeholderData: keepPreviousData })
}

export function useNaps(from: string, to: string) {
  return useQuery({ queryKey: ['health', 'naps', from, to], queryFn: () => request<NapRow[]>('GET', `/naps?from=${from}&to=${to}`), placeholderData: keepPreviousData })
}

export function useHealthStatus(from: string, to: string) {
  return useQuery({ queryKey: ['health', 'status', from, to], queryFn: () => request<HealthStatusRow[]>('GET', `/health-status?from=${from}&to=${to}`), placeholderData: keepPreviousData })
}

export function useHydration(from: string, to: string) {
  return useQuery({ queryKey: ['health', 'hydration', from, to], queryFn: () => request<HydrationRow[]>('GET', `/hydration?from=${from}&to=${to}`), placeholderData: keepPreviousData })
}

export function useECGs() {
  return useQuery({ queryKey: ['health', 'ecg'], queryFn: () => request<ECGSummary[]>('GET', '/ecg') })
}

export function useECG(id: number | null) {
  return useQuery({ queryKey: ['health', 'ecg', id], queryFn: () => request<ECGRecording>('GET', `/ecg/${id}`), enabled: id != null })
}

export function useRecords() {
  return useQuery({ queryKey: ['training', 'records'], queryFn: () => request<PersonalRecordRow[]>('GET', '/records') })
}

export function useGear() {
  return useQuery({ queryKey: ['training', 'gear'], queryFn: () => request<GearRow[]>('GET', '/gear') })
}

/** useSolar is a watch's solar charge: the last 7 days it has data for. */
export function useSolar(device: number) {
  return useQuery({ queryKey: ['devices', 'solar', device], queryFn: () => request<SolarSeries>('GET', `/devices/${device}/solar`) })
}

export function useSettingsChanges(device: number) {
  return useInfiniteQuery({
    queryKey: ['devices', 'changes', device],
    queryFn: ({ pageParam }) => request<{ items: SettingsChange[]; next_before: number | null }>('GET', `/devices/${device}/changes${pageParam ? `?before=${pageParam}` : ''}`),
    initialPageParam: 0,
    getNextPageParam: (last) => last.next_before ?? undefined,
  })
}

export function useDeviceDocuments(device: number) {
  return useQuery({ queryKey: ['devices', 'documents', device], queryFn: () => request<DeviceDocument[]>('GET', `/devices/${device}/documents`) })
}

export function useFiles(type: string, device?: number) {
  return useInfiniteQuery({
    queryKey: ['files', type, device ?? 0],
    queryFn: ({ pageParam }) => {
      const q = new URLSearchParams({ limit: '100' })
      if (type) q.set('type', type)
      if (device) q.set('device', String(device))
      if (pageParam) q.set('before', String(pageParam))
      return request<FilePage>('GET', `/files?${q}`)
    },
    initialPageParam: 0,
    getNextPageParam: (last) => last.next_before ?? undefined,
  })
}

export function useFileMessages(id: number, num: number | null, offset: number, limit = 200) {
  const q = new URLSearchParams({ offset: String(offset), limit: String(limit) })
  if (num != null) q.set('num', String(num))
  return useQuery({ queryKey: ['files', id, 'messages', num, offset, limit], queryFn: () => request<FileMessages>('GET', `/files/${id}/messages?${q}`), placeholderData: keepPreviousData })
}

export function useAccountDocuments() {
  return useQuery({ queryKey: ['account', 'documents'], queryFn: () => request<AccountDocumentRow[]>('GET', '/account/documents') })
}

export function useAccountDocument(id: number | null) {
  return useQuery({ queryKey: ['account', 'documents', id], queryFn: () => request<AccountDocument>('GET', `/account/documents/${id}`), enabled: id != null })
}
