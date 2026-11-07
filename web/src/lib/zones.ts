// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { ActivityZones, WatchZones, ZonesInfo } from '../api/types'
import { zoneShares } from './activity'

export type ZoneSource = 'activity' | 'profile' | 'watch' | 'custom' | 'estimate'

/** Zones in use: five zones from their bottom limits (zone 1 from limits[0] … zone 5 from
 * limits[4]), the top (limits[5]), and where they come from. */
export interface HrZones {
  limits: number[]
  source: ZoneSource
  /** Said beside the zones: "from the watch's Run profile". */
  label: string
}

const valid = (hr?: number[]): hr is number[] => !!hr && hr.length === 6 && hr.every((v, i) => v > 0 && (i === 0 || v > hr[i - 1]))

/** Custom zones from preferences: a maximum with the usual shares, or five typed limits. */
export interface CustomZones {
  max_hr?: number
  edges?: number[]
}

/** zonesFor picks an activity's heart-rate zones: the watch's own for that activity, else its
 * sport profile's, else the watch's default, else the user's custom ones, else an estimate from
 * the highest heart rate seen. With prefer 'custom' or 'estimate' the user's choice goes first. */
export function zonesFor(opts: {
  activity?: ActivityZones | null
  sport?: string
  subSport?: string
  info?: ZonesInfo | null
  custom?: CustomZones | null
  estimateMax?: number
  prefer?: 'watch' | 'custom' | 'estimate'
}): HrZones | null {
  const fromMax = (max: number): number[] => [...zoneShares.map((z) => Math.round(z * max)), max]
  const custom = (): HrZones | null => {
    const c = opts.custom
    if (c?.edges?.length === 5 && c.max_hr) return { limits: [...c.edges, c.max_hr], source: 'custom', label: 'your own zones' }
    if (c?.max_hr) return { limits: fromMax(c.max_hr), source: 'custom', label: `your maximum of ${c.max_hr} bpm` }
    return null
  }
  const estimate = (): HrZones | null =>
    opts.estimateMax ? { limits: fromMax(opts.estimateMax), source: 'estimate', label: `your highest heart rate in recent activities (${opts.estimateMax} bpm)` } : null
  if (opts.prefer === 'custom') {
    const c = custom()
    if (c) return c
  }
  if (opts.prefer === 'estimate') {
    const e = estimate()
    if (e) return e
  }
  if (valid(opts.activity?.hr)) return { limits: opts.activity!.hr!, source: 'activity', label: 'the watch, as it counted this activity' }
  const profiles = opts.info?.profiles ?? []
  const p = profiles.find((x) => x.sport === opts.sport && x.sub_sport === opts.subSport && valid(x.zones.hr))
    ?? profiles.find((x) => x.sport === opts.sport && valid(x.zones.hr))
  if (p) return { limits: p.zones.hr!, source: 'profile', label: `the watch's ${p.name} profile` }
  if (valid(opts.info?.default?.hr)) return { limits: opts.info!.default!.hr!, source: 'watch', label: "the watch's zones" }
  return custom() ?? estimate()
}

/** watchMax is the maximum heart rate the watch uses, if it says. */
export function watchMax(z?: WatchZones | null): number | undefined {
  return z?.max_hr ?? (valid(z?.hr) ? z!.hr![5] : undefined)
}

/** secondsIn is the time per zone 1–5 from the watch's own counts (which also count below zone 1
 * and above the top; those are left out). */
export function secondsIn(z: ActivityZones): number[] | null {
  if (!z.hr_s || z.hr_s.length < 6) return null
  return z.hr_s.slice(1, 6)
}
