// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import { fitPath, mercator } from './mercator'
import { decodePolyline } from './polyline'

describe('decodePolyline', () => {
  it('reads the reference example', () => {
    // Google's documented example: (38.5, -120.2), (40.7, -120.95), (43.252, -126.453).
    expect(decodePolyline('_p~iF~ps|U_ulLnnqC_mqNvxq`@')).toEqual([[38.5, -120.2], [40.7, -120.95], [43.252, -126.453]])
    expect(decodePolyline('')).toEqual([])
    expect(decodePolyline('_p~iF')).toEqual([]) // cut short: no half points
  })
})

describe('mercator', () => {
  it('maps the world to the unit square', () => {
    expect(mercator(0, 0)).toEqual([0.5, 0.5])
    expect(mercator(0, 180)[0]).toBe(1)
    expect(mercator(85.0511, 0)[1]).toBeCloseTo(0, 3)
  })

  it('fits a track into the box, north up', () => {
    const p = fitPath([[50, 8], [50.01, 8.02]], 200, 100, 10)
    for (const [x, y] of p) {
      expect(x).toBeGreaterThanOrEqual(10 - 1e-9)
      expect(x).toBeLessThanOrEqual(190 + 1e-9)
      expect(y).toBeGreaterThanOrEqual(10 - 1e-9)
      expect(y).toBeLessThanOrEqual(90 + 1e-9)
    }
    expect(p[1][1]).toBeLessThan(p[0][1]) // further north is higher up
  })
})
