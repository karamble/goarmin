// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useQuery } from '@tanstack/react-query'
import { ApiError, request } from '../api/client'

/** ExerciseInfo is one exercise of Garmin's FIT profile with its figure. */
export interface ExerciseInfo {
  key: string // category/number
  category: string
  category_num: number
  number: number
  name: string
  title: string
  quality: 'own' | 'family' | 'category'
  family: string
  pose: string
  mode: 'hold' | 'loop'
}

export interface ExerciseCategory {
  name: string
  number: number
  title: string
  count: number
}

export interface ExerciseCatalog {
  categories: ExerciseCategory[]
  exercises: ExerciseInfo[]
}

/** ExercisePose is an exercise's key poses: root position, then one quaternion per part. */
export interface ExercisePose {
  exercise: ExerciseInfo
  parts: string[]
  keys: number[][]
}

const figureKeys = {
  catalog: ['exercises'] as const,
  pose: (category: string, number: number) => ['exercises', category, number] as const,
}

/** useExercises is the whole catalogue (the data ships with the server; it never changes while it runs). */
export function useExercises() {
  return useQuery({ queryKey: figureKeys.catalog, queryFn: () => request<ExerciseCatalog>('GET', '/exercises'), staleTime: Infinity })
}

/** useExercisePose is one exercise's key poses (null when the profile has no such exercise). */
export function useExercisePose(category: string | undefined, number: number | undefined) {
  return useQuery({
    queryKey: figureKeys.pose(category ?? '', number ?? -1),
    queryFn: async () => {
      try {
        return await request<ExercisePose>('GET', `/exercises/${encodeURIComponent(category!)}/${number}`)
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return null
        throw e
      }
    },
    enabled: !!category && number !== undefined && number >= 0,
    staleTime: Infinity,
  })
}
