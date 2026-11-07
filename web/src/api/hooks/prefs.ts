// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createContext, useContext } from 'react'
import { request } from '../client'
import { keys } from '../keys'
import type { Setting, WatchProfile } from '../types'

/** Prefs as stored (setting "prefs"); a missing field means the default. */
export interface Prefs {
  v: number
  week_start?: 'monday' | 'sunday' | 'saturday'
  clock?: 'watch' | 'browser' | '24h' | '12h'
  /** Minutes a week; absent = the watch's goal. */
  intensity_goal?: number
  zones?: { source: 'watch' | 'custom' | 'estimate'; max_hr?: number; edges?: number[] }
  onboarding_done?: boolean
}

export type Source = 'you' | 'watch' | 'default'

/** Prefs in effect: the user's, else the watch's, else the defaults, each with its source. */
export interface Effective {
  /** The week's first day as Date.getUTCDay counts. */
  weekStart: number
  hourCycle: 'h23' | 'h12' | undefined
  clockSource: Source
  /** The watch's own clock, if it says. */
  watchClock?: '24h' | '12h'
  intensityGoal: number
  intensitySource: Source
  zones: NonNullable<Prefs['zones']>
  onboardingDone: boolean
  raw: Prefs
  rev: number
  loaded: boolean
}

const weekDays = { monday: 1, sunday: 0, saturday: 6 } as const

/** effective merges the stored prefs with what the watch has. */
export function effective(p: Prefs | null, rev: number, watch: WatchProfile | null | undefined, loaded = true): Effective {
  const raw: Prefs = p ?? { v: 1 }
  const wclock = watch?.settings?.clock
  let hourCycle: Effective['hourCycle']
  let clockSource: Source = 'default'
  switch (raw.clock) {
    case '24h':
      hourCycle = 'h23'
      clockSource = 'you'
      break
    case '12h':
      hourCycle = 'h12'
      clockSource = 'you'
      break
    case 'browser':
      clockSource = 'you'
      break
    default: // as the watch
      if (wclock) {
        hourCycle = wclock === '12h' ? 'h12' : 'h23'
        clockSource = 'watch'
      }
  }
  const wgoal = watch?.goals?.find((g) => g.type === 'active_minutes' && !g.sport)?.target
  const intensityGoal = raw.intensity_goal ?? wgoal ?? 150
  return {
    weekStart: weekDays[raw.week_start ?? 'monday'],
    hourCycle,
    clockSource,
    watchClock: wclock,
    intensityGoal,
    intensitySource: raw.intensity_goal ? 'you' : wgoal ? 'watch' : 'default',
    zones: raw.zones ?? { source: 'watch' },
    onboardingDone: !!raw.onboarding_done,
    raw,
    rev,
    loaded,
  }
}

export const PrefsContext = createContext<Effective>(effective(null, 0, null, false))

/** usePrefs is the preferences in effect (from PrefsProvider). */
export function usePrefs(): Effective {
  return useContext(PrefsContext)
}

export function usePrefsSetting() {
  return useQuery({ queryKey: keys.setting('prefs'), queryFn: () => request<Setting<Prefs>>('GET', '/me/settings/prefs'), staleTime: 60_000 })
}

/** useSavePrefs stores changed preferences against the revision they were read at; a newer
 * version from another device is a 412, after which the page shows that version. */
export function useSavePrefs() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ next, rev }: { next: Prefs; rev: number }) =>
      request<Setting<Prefs>>('PUT', '/me/settings/prefs', undefined, { raw: JSON.stringify(next), headers: { 'If-Match': `"${rev}"` } }),
    onSuccess: (s) => qc.setQueryData(keys.setting('prefs'), s),
    onError: () => qc.invalidateQueries({ queryKey: keys.setting('prefs') }),
  })
}
