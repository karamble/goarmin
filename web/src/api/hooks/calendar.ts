// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { request } from '../client'
import { keys } from '../keys'
import type { Calendar, Plan } from '../types'

/** useCalendar is the days from..to that hold anything (at most 93 days). */
export function useCalendar(from: string, to: string) {
  return useQuery({
    queryKey: keys.calendar(from, to),
    queryFn: () => request<Calendar>('GET', `/calendar?from=${from}&to=${to}`),
    placeholderData: keepPreviousData,
  })
}

/** usePlanActions puts library workouts on days, moves and removes them; the calendar (and the
 * glance's workout for today) refetch afterwards. */
export function usePlanActions() {
  const qc = useQueryClient()
  const refresh = () => qc.invalidateQueries({ queryKey: keys.calendarAll })
  return {
    add: useMutation({
      mutationFn: (v: { day: string; workout_id: number; note?: string }) => request<Plan>('POST', '/plans', v),
      onSettled: refresh,
    }),
    move: useMutation({
      mutationFn: (v: { id: number; day: string }) => request<Plan>('PATCH', `/plans/${v.id}`, { day: v.day }),
      onSettled: refresh,
    }),
    note: useMutation({
      mutationFn: (v: { id: number; note: string }) => request<Plan>('PATCH', `/plans/${v.id}`, { note: v.note }),
      onSettled: refresh,
    }),
    remove: useMutation({
      mutationFn: (id: number) => request<void>('DELETE', `/plans/${id}`),
      onSettled: refresh,
    }),
  }
}
