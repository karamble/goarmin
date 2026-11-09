// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, request } from '../client'
import { keys } from '../keys'
import type { Activity, ActivityDetail, ActivityPage, Course, Neighbours, Streams, Totals, WatchProfile, Workout, WorkoutStep, ZonesInfo } from '../types'

/** useActivityPages pages through every activity, newest first (Load more). */
export function useActivityPages(limit = 30) {
  return useInfiniteQuery({
    queryKey: keys.activityPages(limit),
    initialPageParam: 0,
    queryFn: ({ pageParam }) => request<ActivityPage>('GET', `/activities?limit=${limit}${pageParam ? `&before=${pageParam}` : ''}`),
    getNextPageParam: (last) => last.next_before,
  })
}

/** useActivitiesIn is every activity recorded on the days from..to, newest first. */
export function useActivitiesIn(from: string, to: string, enabled = true) {
  return useQuery({
    queryKey: keys.activitiesIn(from, to),
    queryFn: async () => (await request<{ items: Activity[] }>('GET', `/activities?from_day=${from}&to_day=${to}`)).items,
    placeholderData: keepPreviousData,
    enabled,
  })
}

/** useActivity is one activity with its laps (null when it does not exist or is not yours). */
export function useActivity(id: number) {
  return useQuery({
    queryKey: keys.activity(id),
    queryFn: async () => {
      try {
        return await request<ActivityDetail>('GET', `/activities/${id}`)
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return null
        throw e
      }
    },
    enabled: id > 0,
  })
}

/** useStreams is the activity's track (null when it has none). */
export function useStreams(id: number) {
  return useQuery({
    queryKey: keys.streams(id),
    queryFn: async () => {
      try {
        return await request<Streams>('GET', `/activities/${id}/streams`)
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return null
        throw e
      }
    },
    enabled: id > 0,
    staleTime: Infinity,
  })
}

export function useNeighbours(id: number) {
  return useQuery({ queryKey: keys.neighbours(id), queryFn: () => request<Neighbours>('GET', `/activities/${id}/neighbours`), enabled: id > 0 })
}

/** useTotals is the watches' lifetime totals per sport. */
export function useTotals() {
  return useQuery({ queryKey: keys.totals, queryFn: () => request<Totals>('GET', '/totals'), staleTime: 5 * 60_000 })
}

/** useWatchProfile is what the watch keeps: its settings and goals. */
export function useWatchProfile() {
  return useQuery({ queryKey: keys.watchProfile, queryFn: () => request<WatchProfile>('GET', '/me/watch-profile'), staleTime: 5 * 60_000 })
}

/** useZones is the heart-rate zones of the watch and its sport profiles. */
export function useZones() {
  return useQuery({ queryKey: keys.zones, queryFn: () => request<ZonesInfo>('GET', '/me/zones'), staleTime: 5 * 60_000 })
}

export function useWorkouts() {
  return useQuery({ queryKey: keys.workouts, queryFn: () => request<Workout[]>('GET', '/workouts') })
}

/** useWorkout is one workout with its steps (null when it is not there). */
export function useWorkout(id: number) {
  return useQuery({ queryKey: keys.workout(id), queryFn: () => nullOn404(request<Workout>('GET', `/workouts/${id}`)), enabled: id > 0 })
}

/** A workout as the builder sends it. */
export interface WorkoutBody {
  name: string
  sport: string
  sub_sport: string
  description: string
  steps: WorkoutStep[]
}

/** useWorkoutBuilder makes, saves (If-Match with the version edited), duplicates and hides
 * workouts; each refreshes the library, the calendar and the devices. */
export function useWorkoutBuilder() {
  const qc = useQueryClient()
  const done = (w?: Workout) => {
    if (w) qc.setQueryData(keys.workout(w.id), w)
    void qc.invalidateQueries({ queryKey: keys.workouts })
    void qc.invalidateQueries({ queryKey: keys.calendarAll })
    void qc.invalidateQueries({ queryKey: keys.devices })
  }
  return {
    create: useMutation({ mutationFn: (b: WorkoutBody) => request<Workout>('POST', '/workouts', b), onSuccess: done }),
    save: useMutation({
      mutationFn: ({ id, tag, body }: { id: number; tag?: string; body: WorkoutBody }) =>
        request<Workout>('PUT', `/workouts/${id}`, body, { headers: tag ? { 'If-Match': tag } : {} }),
      onSuccess: done,
    }),
    duplicate: useMutation({
      mutationFn: ({ id, name }: { id: number; name?: string }) => request<Workout>('POST', `/workouts/${id}/duplicate`, name ? { name } : undefined),
      onSuccess: done,
    }),
    hide: useMutation({
      mutationFn: ({ id, hide }: { id: number; hide: boolean }) => request<Workout>(hide ? 'POST' : 'DELETE', `/workouts/${id}/hide`),
      onSuccess: done,
    }),
  }
}

/** useWorkoutEdits renames a workout or changes its description (goarmin keeps its own
 * version), and undoes that again. Both refresh the library, its workouts and the calendar. */
export function useWorkoutEdits() {
  const qc = useQueryClient()
  const done = () => {
    void qc.invalidateQueries({ queryKey: keys.workouts })
    void qc.invalidateQueries({ queryKey: keys.calendarAll })
  }
  return {
    rename: useMutation({
      mutationFn: ({ id, ...body }: { id: number; name?: string; description?: string }) => request<Workout>('PATCH', `/workouts/${id}`, body),
      onSuccess: done,
    }),
    undo: useMutation({ mutationFn: (id: number) => request<void>('DELETE', `/workouts/${id}`), onSuccess: done }),
    /** onWatch switches the workout on or off for a watch; on, the watch gets it at its next
     * sync, which starts at once. */
    onWatch: useMutation({
      mutationFn: ({ id, device, on }: { id: number; device: number; on: boolean }) =>
        request<Workout>('PUT', `/workouts/${id}/watches/${device}`, { on }),
      onSuccess: (w, v) => {
        qc.setQueryData(keys.workout(v.id), w)
        done()
        void qc.invalidateQueries({ queryKey: keys.devices })
      },
    }),
  }
}

export function useCourses() {
  return useQuery({ queryKey: keys.courses, queryFn: () => request<Course[]>('GET', '/courses') })
}

/** useCourse is one course with its points (null when it is not there). */
export function useCourse(id: number) {
  return useQuery({ queryKey: keys.course(id), queryFn: () => nullOn404(request<Course>('GET', `/courses/${id}`)), enabled: id > 0 })
}

/** useCourseStreams is a course's track (null without one). */
export function useCourseStreams(id: number) {
  return useQuery({
    queryKey: keys.courseStreams(id), queryFn: () => nullOn404(request<Streams>('GET', `/courses/${id}/streams`)), enabled: id > 0, staleTime: Infinity,
  })
}

async function nullOn404<T>(p: Promise<T>): Promise<T | null> {
  try {
    return await p
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null
    throw e
  }
}
