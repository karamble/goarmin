// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import type { Workout, WorkoutWatch } from '../api/types'
import { filterLibrary, originText, presenceText, sportWords, watchShares } from './workoutLibrary'

const now = Math.floor(Date.now() / 1000)

function wk(id: number, name: string, sport: string, sub: string, origins: Workout['origins'], watches: WorkoutWatch[]): Workout {
  return {
    id, name, sport, sub_sport: sub, estimated_s: 600, step_count: 5, exercise_count: 3, recorded_at: now - id * 86400,
    origin: origins[0], origins, watches,
  }
}

const fenix = (state: WorkoutWatch['state'], since?: number): WorkoutWatch => ({ device_id: 7, name: 'Fenix 8', state, since, checked_at: now - 600 })

const lib = [
  wk(1, 'Master Core Control', 'fitness_equipment', 'pilates', ['watch', 'garmin_connect'], [fenix('on', now - 600)]),
  wk(2, 'Coherence', 'training', 'breathing', ['watch'], [fenix('removed', now - 3600)]),
  wk(3, 'Alignment 101', 'training', 'yoga', ['garmin_connect'], [fenix('absent')]),
]
const none = { q: '', sport: '', origin: '', where: '' } as const

describe('workout library', () => {
  it('filters by words, sport, source and where it is', () => {
    expect(filterLibrary(lib, none).map((w) => w.id)).toEqual([1, 2, 3])
    expect(filterLibrary(lib, { ...none, q: 'core  master' }).map((w) => w.id)).toEqual([1])
    expect(filterLibrary(lib, { ...none, q: 'yoga' }).map((w) => w.id)).toEqual([3])
    expect(filterLibrary(lib, { ...none, sport: 'Pilates' }).map((w) => w.id)).toEqual([1])
    expect(filterLibrary(lib, { ...none, origin: 'garmin_connect' }).map((w) => w.id)).toEqual([1, 3])
    expect(filterLibrary(lib, { ...none, origin: 'watch' }).map((w) => w.id)).toEqual([1, 2])
    expect(filterLibrary(lib, { ...none, where: 'on:7' }).map((w) => w.id)).toEqual([1])
    expect(filterLibrary(lib, { ...none, where: 'none' }).map((w) => w.id)).toEqual([2, 3])
    expect(filterLibrary(lib, { ...none, where: 'on:8' })).toEqual([])
  })

  it('sums the library up per watch', () => {
    expect(watchShares(lib)).toEqual([{ device_id: 7, name: 'Fenix 8', on: 1, removed: 1, checked_at: now - 600 }])
    expect(watchShares([])).toEqual([])
  })

  it('says where a workout is and came from', () => {
    expect(presenceText(fenix('on')).long).toBe('On Fenix 8')
    expect(presenceText(fenix('removed', now - 3600)).long).toMatch(/^Removed from Fenix 8 \(.+\)$/)
    expect(presenceText(fenix('absent')).long).toBe('Not on Fenix 8')
    expect(presenceText(fenix('unknown')).long).toBe('Fenix 8 has not been checked yet')
    expect(originText(['watch', 'garmin_connect'])).toBe('From a watch and Garmin Connect')
    expect(originText(['garmin_connect'])).toBe('From Garmin Connect')
    expect(sportWords('fitness_equipment', 'pilates')).toBe('Pilates')
    expect(sportWords('mobility', 'generic')).toBe('Mobility')
    expect(sportWords('hiit', 'generic')).toBe('HIIT')
    expect(sportWords('hiit', 'amrap')).toBe('AMRAP')
  })
})
