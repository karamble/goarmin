// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import { blendAt, DWELL, MOVE } from './motion'

describe('blendAt', () => {
  it('holds a single key', () => {
    expect(blendAt(1, 12.3)).toEqual({ a: 0, b: 0, u: 0 })
  })
  it('rests on a key, then moves to the next, and loops back to the first', () => {
    const seg = MOVE + DWELL
    expect(blendAt(2, DWELL / 2)).toEqual({ a: 0, b: 1, u: 0 })
    expect(blendAt(2, DWELL + MOVE / 2).u).toBeCloseTo(0.5)
    expect(blendAt(2, seg + DWELL / 2)).toEqual({ a: 1, b: 0, u: 0 })
    expect(blendAt(2, 2 * seg + DWELL / 2)).toEqual({ a: 0, b: 1, u: 0 })
    const late = blendAt(3, 2 * seg + DWELL + MOVE * 0.9)
    expect(late.a).toBe(2)
    expect(late.b).toBe(0)
    expect(late.u).toBeGreaterThan(0.9)
  })
  it('eases at both ends', () => {
    expect(blendAt(2, DWELL + MOVE * 0.1).u).toBeLessThan(0.1)
    expect(blendAt(2, DWELL + MOVE * 0.9).u).toBeGreaterThan(0.9)
  })
})
