// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import type { WatchProfile } from '../types'
import { effective } from './prefs'

const watch: WatchProfile = { settings: { clock: '24h', units: {}, wake_s: -1, sleep_s: -1 }, goals: [{ type: 'active_minutes', sport: '', target: 200, unit: 'min' }] }

describe('prefs', () => {
  it('takes the watch as the default and says so', () => {
    const e = effective(null, 0, watch)
    expect(e).toMatchObject({ weekStart: 1, hourCycle: 'h23', clockSource: 'watch', intensityGoal: 200, intensitySource: 'watch', zones: { source: 'watch' } })
    expect(effective(null, 0, null)).toMatchObject({ hourCycle: undefined, clockSource: 'default', intensityGoal: 150, intensitySource: 'default' })
  })

  it('lets the user override each one', () => {
    const e = effective({ v: 1, week_start: 'sunday', clock: '12h', intensity_goal: 300, zones: { source: 'custom', max_hr: 190 } }, 4, watch)
    expect(e).toMatchObject({ weekStart: 0, hourCycle: 'h12', clockSource: 'you', intensityGoal: 300, intensitySource: 'you', rev: 4 })
    expect(e.zones).toEqual({ source: 'custom', max_hr: 190 })
    expect(effective({ v: 1, clock: 'browser', week_start: 'saturday' }, 1, watch)).toMatchObject({ hourCycle: undefined, clockSource: 'you', weekStart: 6 })
  })
})
