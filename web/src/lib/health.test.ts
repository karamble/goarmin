// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import {
  asleep, chargeDrain, clockOfEvening, eveningMinutes, hourlyFloors, hourlyTotals, hrvOf, intensityWeek, localMinutes, rolling, Stage,
  stageSegments, stressTime,
} from './health'

const now = Math.floor(Date.now() / 1000)
const start = now - 8 * 3600

describe('health', () => {
  it('turns stage changes into segments until the next change or the end', () => {
    const t = [start, start + 600, start + 1200, start + 1800, start + 1900]
    const v = [Stage.light, Stage.light, Stage.deep, Stage.unmeasurable, Stage.rem]
    expect(stageSegments(t, v, start + 2500)).toEqual([
      { from: start, to: start + 1200, stage: Stage.light },
      { from: start + 1200, to: start + 1800, stage: Stage.deep },
      { from: start + 1900, to: start + 2500, stage: Stage.rem },
    ])
    expect(stageSegments([start], [Stage.awake], start)).toEqual([])
    expect(asleep({ deep_s: 3600, light_s: 7200, rem_s: null })).toBe(10800)
    expect(asleep({ deep_s: null, light_s: null, rem_s: null })).toBeNull()
  })

  it('sums running totals into hours, also on a 25-hour day', () => {
    const from = now - 25 * 3600
    const s = { from, to: from + 25 * 3600, t: [from + 60, from + 120, from + 3700, from + 24 * 3600 + 10, from + 26 * 3600], v: [10, 5, 7, 3, 99] }
    const h = hourlyTotals(s)
    expect(h).toHaveLength(25)
    expect(h[0]).toBe(15)
    expect(h[1]).toBe(7)
    expect(h[24]).toBe(3)
    expect(h.reduce((a, b) => a + b, 0)).toBe(25) // the point past the day is left out
  })

  it('counts whole floors per hour, adding up to the day', () => {
    const from = now - 24 * 3600
    // The Fenix 8's climbs of one day (metres per interval): 9 floors, finished in hours 2, 5 and 6.
    const v = [3.048, 2.424, 2.956, 3.764, 1.659, 6.05, 2.571, 2.693, 2.267]
    const t = [from + 2 * 3600, from + 5 * 3600, from + 5 * 3600 + 60, from + 5 * 3600 + 120, from + 6 * 3600,
      from + 6 * 3600 + 60, from + 6 * 3600 + 120, from + 6 * 3600 + 180, from + 6 * 3600 + 240]
    const h = hourlyFloors({ from, to: from + 24 * 3600, t, v })
    expect(h).toHaveLength(24)
    expect(h[2]).toBe(1)
    expect(h[5]).toBe(3)
    expect(h[6]).toBe(5)
    expect(h.reduce((a, b) => a + b, 0)).toBe(9)
    expect(hourlyFloors({ from, to: from + 24 * 3600, t: [from + 60], v: [2.9] }).every((x) => x === 0)).toBe(true)
  })

  it('sums Body Battery rises and falls', () => {
    expect(chargeDrain([20, 25, null, 60, 40, 45, 10])).toEqual({ charged: 45, drained: 55 })
    expect(chargeDrain([])).toEqual({ charged: 0, drained: 0 })
  })

  it('splits stress time into levels, capping gaps and skipping unmeasured values', () => {
    const t = [start, start + 180, start + 360, start + 540, start + 5000]
    const v = [10, 40, -1, 80, 60]
    expect(stressTime(t, v, start + 5100)).toEqual({ rest: 180, low: 180, medium: 100, high: 300 })
  })

  it('averages the last days over the values present', () => {
    expect(rolling([10, null, 20, 30], 2)).toEqual([10, 10, 20, 25])
    expect(rolling([null, null], 7)).toEqual([null, null])
  })

  it('places bedtimes and wake times on a night axis', () => {
    // Only the time of day matters: the date comes from now.
    const today = new Date()
    const at = (h: number, m: number) => Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate(), h, m) / 1000
    expect(localMinutes(at(23, 30), 'UTC')).toBe(1410)
    expect(eveningMinutes(at(23, 30), 'UTC')).toBe(330)
    expect(eveningMinutes(at(7, 15), 'UTC')).toBe(795)
    expect(eveningMinutes(at(18, 0), 'UTC')).toBe(0)
    expect(clockOfEvening(330)).toBe('23:30')
    expect(clockOfEvening(795)).toBe('07:15')
  })

  it('reads HRV from the status file, else from the daily values', () => {
    expect(hrvOf({ values: {}, hrv: { weekly_avg_ms: 55, last_night_ms: 57, last_night_high_ms: null, baseline_low_upper_ms: null,
      balanced_lower_ms: 46, balanced_upper_ms: 60, status: 4 } })).toEqual({ value: 57, low: 46, high: 60, weekly: 55, status: 4 })
    expect(hrvOf({ values: { hrv_last_night: 50, hrv_target_low: 44, hrv_target_high: 62 } }))
      .toEqual({ value: 50, low: 44, high: 62, weekly: null, status: null })
    expect(hrvOf(null).value).toBeNull()
  })

  it('counts intensity minutes up to the day, vigorous twice', () => {
    const days = [{ day: 'a', moderate_min: 10, vigorous_min: 5 }, { day: 'b', moderate_min: 20, vigorous_min: null }, { day: 'c', moderate_min: 99, vigorous_min: 0 }]
    expect(intensityWeek(days, 'b')).toBe(40)
  })
})
