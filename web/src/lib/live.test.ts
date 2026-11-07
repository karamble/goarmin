// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import type { Reading } from '../api/live'
import { addReadings, cleanBeats, endText, gained, rmssd, stressLevel, summary } from './live'

const now = Math.floor(Date.now() / 1000)
const r = (name: string, value: number, t: number): Reading => ({ name, value, unit: '', t })

describe('live', () => {
  it('keeps histories in time order, skips repeats and old points', () => {
    let h = addReadings({}, [r('heart_rate', 70, now - 700), r('heart_rate', 72, now - 10), r('heart_rate', 0, now - 9), r('raw', 1, now)], 600)
    expect(h.heart_rate).toEqual({ t: [now - 10], v: [72] }) // 700 s old dropped, 0 bpm skipped, raw not kept
    h = addReadings(h, [r('heart_rate', 72, now - 10), r('heart_rate', 75, now), r('heart_rate', 73, now - 5)], 600)
    expect(h.heart_rate).toEqual({ t: [now - 10, now - 5, now], v: [72, 73, 75] }) // repeat skipped, late point placed
    expect(h.stress).toBeUndefined()
  })

  it('computes live HRV from clean beats only', () => {
    const beats = Array.from({ length: 30 }, (_, i) => (i % 2 ? 800 : 820))
    expect(rmssd(beats)).toBe(20)
    expect(cleanBeats([800, 2500, 810, 1200, 805, 250])).toEqual([800, 810, 805]) // impossible and jumping beats dropped
    expect(rmssd([800, 810, 820])).toBeNull()
  })

  it('names stress, sums a session and counts what grew', () => {
    expect([10, 30, 60, 90].map((v) => stressLevel(v).label)).toEqual(['Rest', 'Low', 'Medium', 'High'])
    expect(summary({ t: [now, now + 10, now + 20], v: [60, 80, 70] })).toEqual({ min: 60, max: 80, avg: 70 })
    expect(summary({ t: [], v: [] })).toBeNull()
    expect(gained({ t: [now, now + 60], v: [5000, 5120] })).toBe(120)
  })

  it('says why a link ended', () => {
    expect(endText({ reason: 'time_limit' })).toMatch(/30 minutes/)
    expect(endText({ reason: 'error', error_code: 'not_in_range' })).toMatch(/not in range/)
    expect(endText({ reason: 'error', error: 'boom' })).toBe('The link failed: boom')
    expect(endText(null)).toBe('The link ended.')
  })
})
