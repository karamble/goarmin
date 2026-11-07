// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import type { ZonesInfo } from '../api/types'
import { secondsIn, watchMax, zonesFor } from './zones'

const watch = [92, 110, 128, 146, 165, 183]
const info: ZonesInfo = {
  default: { max_hr: 183, hr: watch },
  profiles: [
    { sport: 'running', sub_sport: 'trail', name: 'Trail Run', zones: { hr: [95, 114, 133, 152, 171, 190] }, own: true, recorded_at: 0 },
    { sport: 'cycling', sub_sport: 'road', name: 'Road Bike', zones: {}, own: false, recorded_at: 0 },
  ],
}

describe('zones', () => {
  it('prefers the activity, then the sport profile, then the watch, then custom, then an estimate', () => {
    expect(zonesFor({ activity: { hr: [90, 100, 110, 120, 130, 140], hr_s: [] }, info })).toMatchObject({ source: 'activity', limits: [90, 100, 110, 120, 130, 140] })
    expect(zonesFor({ sport: 'running', subSport: 'trail', info })).toMatchObject({ source: 'profile', label: "the watch's Trail Run profile" })
    expect(zonesFor({ sport: 'cycling', subSport: 'road', info })).toMatchObject({ source: 'watch', limits: watch })
    expect(zonesFor({ custom: { max_hr: 190 } })).toMatchObject({ source: 'custom', limits: [95, 114, 133, 152, 171, 190] })
    expect(zonesFor({ estimateMax: 180 })).toMatchObject({ source: 'estimate', limits: [90, 108, 126, 144, 162, 180] })
    expect(zonesFor({})).toBeNull()
  })

  it('lets the user put their own zones first', () => {
    const custom = { max_hr: 200, edges: [100, 120, 140, 160, 180] }
    expect(zonesFor({ activity: { hr: watch }, info, custom, prefer: 'custom' })).toMatchObject({ source: 'custom', limits: [100, 120, 140, 160, 180, 200] })
    expect(zonesFor({ info, estimateMax: 170, prefer: 'estimate' })).toMatchObject({ source: 'estimate' })
  })

  it('reads the watch maximum and the seconds in zones 1–5', () => {
    expect(watchMax({ hr: watch })).toBe(183)
    expect(watchMax(null)).toBeUndefined()
    expect(secondsIn({ hr_s: [5, 10, 20, 30, 40, 50, 1] })).toEqual([10, 20, 30, 40, 50])
    expect(secondsIn({})).toBeNull()
  })
})
