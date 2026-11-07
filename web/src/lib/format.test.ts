// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import { ago, clock, distance, hm, hourCycle, pace, setHourCycle, timeOfDay } from './format'

describe('format', () => {
  it('formats durations', () => {
    expect(hm(5 * 3600 + 42 * 60)).toBe('5h 42m')
    expect(hm(42 * 60)).toBe('42m')
    expect(hm(null)).toBe('–')
    expect(clock(3723)).toBe('1:02:03')
    expect(clock(123)).toBe('2:03')
  })
  it('formats distance and pace in both unit systems', () => {
    expect(distance(5000, 'metric')).toMatch(/^5[.,]00 km$/)
    expect(distance(1609.344, 'imperial')).toMatch(/^1[.,]00 mi$/)
    expect(pace(1000 / 300, 'metric')).toBe('5:00 /km')
    expect(pace(0, 'metric')).toBe('–')
  })
  it('says how long ago', () => {
    const now = Date.now()
    expect(ago(now / 1000 - 30, now)).toBe('just now')
    expect(ago(now / 1000 - 12 * 60, now)).toBe('12 min ago')
    expect(ago(new Date(now - 3 * 3600_000).toISOString(), now)).toBe('3 h ago')
  })
})

describe('clock', () => {
  it('shows times of day in the chosen clock', () => {
    const d = new Date()
    d.setUTCHours(13, 5, 0, 0)
    const unix = Math.floor(d.getTime() / 1000)
    try {
      setHourCycle('h23')
      expect(timeOfDay(unix, 'UTC')).toMatch(/13.05/)
      setHourCycle('h12')
      expect(timeOfDay(unix, 'UTC')).toMatch(/0?1.05/)
      expect(timeOfDay(unix, 'UTC')).not.toMatch(/13/)
    } finally {
      setHourCycle(undefined)
    }
    expect(hourCycle()).toBeUndefined()
  })
})
