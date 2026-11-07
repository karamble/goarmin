// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import type { Streams } from '../api/types'
import { buckets, cadenceFactor, nearestIndex, niceAxis, paceSteps, robustRange, routePoints, speedKind, speedText, speedValue, splits, sportGroup, timeInZones, zoneOf } from './activity'

const now = Math.floor(Date.now() / 1000)

describe('activity', () => {
  it('reads speed the way each sport does', () => {
    expect(speedKind('running')).toBe('pace')
    expect(speedKind('Trail run')).toBe('pace')
    expect(speedKind('lap_swimming')).toBe('swim')
    expect(speedKind('cycling')).toBe('speed')
    expect(speedText(1000 / 300, 'running', 'metric')).toBe('5:00 /km')
    expect(speedText(1, 'swimming', 'metric')).toBe('1:40 /100m')
    expect(speedText(10, 'cycling', 'metric')).toBe('36.0 km/h')
    expect(speedText(10, 'cycling', 'imperial')).toBe('22.4 mph')
    expect(speedText(0, 'running', 'metric')).toBe('–')
    expect(speedValue(1000 / 300, 'pace', 'metric')).toBeCloseTo(300)
    expect(speedValue(0.1, 'pace', 'metric')).toBeNull() // standing still
    expect(speedValue(5, 'speed', 'metric')).toBe(18)
  })

  it('cuts a track into kilometres with a short last split', () => {
    // 2.5 km at 4 m/s, one point per 10 s (40 m), climbing 1 m per point.
    const n = 63
    const s: Streams = {
      t: Array.from({ length: n }, (_, i) => now + i * 10),
      dist: Array.from({ length: n }, (_, i) => i * 40),
      hr: Array.from({ length: n }, (_, i) => (i < 25 ? 140 : 160)),
      alt: Array.from({ length: n }, (_, i) => 100 + i),
    }
    const sp = splits(s, 'metric')
    expect(sp.map((x) => x.n)).toEqual([1, 2, 3])
    expect(sp[0].distance).toBe(1000)
    expect(sp[0].time).toBe(250)
    expect(sp[0].speed).toBe(4)
    expect(sp[0].hr).toBeCloseTo(140.8, 1) // the point at the boundary belongs to the split it closes
    expect(sp[0].ascent).toBe(25)
    expect(sp[2].distance).toBe(480)
    expect(splits({ t: [now] }, 'metric')).toEqual([])
  })

  it('counts time in heart-rate zones', () => {
    expect(zoneOf(95, 190)).toBe(1)
    expect(zoneOf(94, 190)).toBe(0)
    expect(zoneOf(175, 190)).toBe(5)
    const t = [now, now + 10, now + 20, now + 600, now + 610]
    // 150 of 190 is zone 3 (its 580 s gap capped at 30 s); 180 is zone 5.
    expect(timeInZones(t, [100, 120, 150, 180, null], 190)).toEqual([10, 10, 30, 0, 10])
    expect(timeInZones(t, undefined, 190)).toEqual([0, 0, 0, 0, 0])
  })

  it('bands values for colouring, ignoring outliers', () => {
    const v = [...Array.from({ length: 100 }, (_, i) => 100 + i), null, 1000]
    const { band, edges } = buckets(v, 5)
    expect(band[0]).toBe(0)
    expect(band[99]).toBe(4)
    expect(band[100]).toBe(-1)
    expect(band[101]).toBe(4) // a spike is clamped into the top band
    expect(edges).toHaveLength(6)
    expect(buckets([null], 5).band).toEqual([-1])
  })

  it('finds the nearest point and the positions of a track', () => {
    expect(nearestIndex([0, 10, 20, 30], 14)).toBe(1)
    expect(nearestIndex([0, 10, 20, 30], 16)).toBe(2)
    expect(nearestIndex([0, 10, 20, 30], 99)).toBe(3)
    expect(nearestIndex([], 1)).toBe(-1)
    expect(routePoints({ t: [now, now + 1, now + 2], lat: [null, 50, 50.1], lon: [null, 7, 7.1] })).toEqual([
      { i: 1, lat: 50, lon: 7 }, { i: 2, lat: 50.1, lon: 7.1 },
    ])
  })

  it('names sports for filters', () => {
    expect(sportGroup({ sport: 'swimming', sport_label: 'Pool swim' })).toBe('Swimming')
    expect(sportGroup({ sport: 'fitness_equipment', sport_label: 'Elliptical' })).toBe('Fitness equipment')
    expect(sportGroup({ sport: 'SportInvalid(60)', sport_label: 'Health Snapshot' })).toBe('Health Snapshot')
  })

  it('shows cadence in steps on foot, and an axis without outliers', () => {
    expect([cadenceFactor('running'), cadenceFactor('walking'), cadenceFactor('cycling'), cadenceFactor('swimming')]).toEqual([2, 2, 1, 1])
    const pace = [...Array.from({ length: 198 }, (_, i) => 300 + i / 2), 4000, null, 3990]
    const r = robustRange(pace)!
    expect(r.max).toBeLessThan(500)
    expect(r.min).toBeGreaterThan(280)
    expect(robustRange([5])).toBeNull()
    expect(niceAxis({ min: 318, max: 391 }, paceSteps)).toEqual({ min: 300, max: 420, interval: 30 })
    expect(niceAxis({ min: 9.2, max: 31.4 }, [0.5, 1, 2, 2.5, 5, 10])).toEqual({ min: 0, max: 40, interval: 10 })
  })
})
