// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { addDays } from '../../lib/period'
import { request } from '../client'
import { keys } from '../keys'
import type { ActivityPage, CoachDay, Day, Device, Night, NightStages, Series } from '../types'

// The hooks have fixed shapes, so cards asking for the same thing share one request.

/** useDailyWindow is the n days ending on day (one request; cards select what they need). */
export function useDailyWindow<T = Day[]>(day: string, n: 14 | 90 | 365, select?: (d: Day[]) => T) {
  const from = addDays(day, -(n - 1))
  return useQuery({
    queryKey: keys.daily(from, day),
    queryFn: () => request<Day[]>('GET', `/daily?from=${from}&to=${day}`),
    placeholderData: keepPreviousData,
    select,
  })
}

/** useDailyRange is every day from..to (a page's period). */
export function useDailyRange(from: string, to: string) {
  return useQuery({
    queryKey: keys.daily(from, to),
    queryFn: () => request<Day[]>('GET', `/daily?from=${from}&to=${to}`),
    placeholderData: keepPreviousData,
  })
}

/** useDay is one day out of the 90-day window. */
export function useDay(day: string) {
  return useDailyWindow(day, 90, (list) => list.find((d) => d.day === day) ?? null)
}

/** useDaySeries is a metric over local days (the watch's own day boundaries). */
export function useDaySeries(metric: string, fromDay: string, toDay = fromDay, maxPoints = 720) {
  return useQuery({
    queryKey: keys.series(metric, fromDay, toDay, maxPoints),
    queryFn: () => request<Series>('GET', `/series/${metric}?from_day=${fromDay}&to_day=${toDay}&max_points=${maxPoints}`),
    placeholderData: keepPreviousData,
  })
}

/** useNights lists the nights that ended in [from, to]. */
export function useNights(from: string, to: string) {
  return useQuery({
    queryKey: keys.sleep(from, to),
    queryFn: () => request<Night[]>('GET', `/sleep?from=${from}&to=${to}`),
    placeholderData: keepPreviousData,
  })
}

/** useNight is the night that ended on day (the longest, if the watch saw several). */
export function useNight(day: string) {
  const q = useNights(addDays(day, -6), day)
  const nights = (q.data ?? []).filter((n) => n.day === day)
  const night = nights.sort((a, b) => b.end - b.start - (a.end - a.start))[0] ?? null
  return { ...q, night }
}

/** useNightStages is a night's stages and overnight values (start: the night's start). */
export function useNightStages(start: number | undefined) {
  return useQuery({
    queryKey: keys.nightStages(start ?? 0),
    queryFn: () => request<NightStages>('GET', `/sleep/${start}/stages`),
    enabled: start != null,
    placeholderData: keepPreviousData,
  })
}

/** useActivitiesOn is the activities recorded on day; end is the day's end (Unix seconds). */
export function useActivitiesOn(day: string, end: number | undefined) {
  const q = useQuery({
    queryKey: keys.activitiesBefore(end ?? 0, 30),
    queryFn: () => request<ActivityPage>('GET', `/activities?before=${end}&limit=30`),
    enabled: end != null,
  })
  return { ...q, activities: (q.data?.items ?? []).filter((a) => a.day === day) }
}

/** useRecentActivities is the newest page of activities. */
export function useRecentActivities(limit = 50) {
  return useQuery({
    queryKey: keys.activities(limit),
    queryFn: () => request<ActivityPage>('GET', `/activities?limit=${limit}`),
  })
}

/** useCoachWeek is Garmin Coach's plan for the 7 days from day. */
export function useCoachWeek(day: string) {
  const to = addDays(day, 6)
  return useQuery({
    queryKey: keys.coach(day, to),
    queryFn: () => request<CoachDay[]>('GET', `/coach?from=${day}&to=${to}`),
    placeholderData: keepPreviousData,
  })
}

/** useDevices polls the watches: every 30 s, every 2 s while one syncs. */
export function useDevices() {
  return useQuery({
    queryKey: keys.devices,
    queryFn: () => request<Device[]>('GET', '/devices'),
    refetchInterval: (q) => (q.state.data?.some((d) => d.sync?.running) ? 2_000 : 30_000),
    staleTime: 0,
  })
}
