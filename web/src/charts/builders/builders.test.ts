// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import { micavoss } from '../theme/tokens'
import { dailyBars } from './dailyBars'
import { dayTimeline } from './dayTimeline'
import { sparkline } from './sparkline'
import { describeSeries, timeSeries } from './timeSeries'

const ctx = { tokens: micavoss, tz: 'UTC' }
const now = Math.floor(Date.now() / 1000)
const from = now - 86400
const to = now

type Series = { id?: string; type: string; data: unknown[]; stack?: string } & Record<string, unknown>
const seriesOf = (o: object) => (o as { series: Series[] }).series

/** A series is lime when its lime colour shows (not just a translucent fill). */
const isLime = (s: object) => JSON.stringify(s).includes(micavoss.lime)
const limeCount = (o: object) => seriesOf(o).filter(isLime).length

describe('timeSeries', () => {
  const t = [from, from + 60, from + 120, from + 4000, from + 4060]
  const v = [60, 62, 64, 70, 71]

  it('spans exactly the server range', () => {
    const o = timeSeries({ from, to, series: [{ id: 'hr', name: 'HR', t, v, kind: 'line', role: 'primary' }] }, ctx) as { xAxis: { min: number; max: number } }
    expect(o.xAxis.min).toBe(from * 1000)
    expect(o.xAxis.max).toBe(to * 1000)
  })

  it('breaks the line at gaps', () => {
    const o = timeSeries({ from, to, series: [{ id: 'hr', name: 'HR', t, v, kind: 'line', role: 'primary', gapS: 300 }] }, ctx)
    const data = seriesOf(o)[0].data as [number, number | null][]
    expect(data).toHaveLength(6)
    expect(data[3][1]).toBeNull()
    expect(data[3][0]).toBeGreaterThan(t[2] * 1000)
    expect(data[3][0]).toBeLessThan(t[3] * 1000)
  })

  it('stacks a min/max band under the line', () => {
    const o = timeSeries({ from, to, series: [{ id: 'hr', name: 'HR', t, v, min: v.map((x) => x - 5), max: v.map((x) => x + 5), kind: 'line', role: 'primary' }] }, ctx)
    const [lo, band, line] = seriesOf(o)
    expect(lo.stack).toBe(band.stack)
    expect((lo.data as [number, number][])[0][1]).toBe(55)
    expect((band.data as [number, number][])[0][1]).toBe(10)
    expect(line.id).toBe('hr')
  })

  it('draws one lime series among context series', () => {
    const o = timeSeries({ from, to, series: [
      { id: 'a', name: 'A', t, v, kind: 'area', role: 'primary' },
      { id: 'b', name: 'B', t, v, kind: 'line', role: 'context' },
      { id: 'c', name: 'C', t, v, kind: 'bar', role: 'warn' },
    ] }, ctx)
    expect(limeCount(o)).toBe(1)
  })

  it('describes a series for screen readers', () => {
    expect(describeSeries('Heart rate', [50, null, 70], 'bpm')).toBe('Heart rate: minimum 50, maximum 70, average 60 bpm')
    expect(describeSeries('Heart rate', [])).toBe('Heart rate: no data')
  })
})

describe('dailyBars', () => {
  it('draws only the focused day lime', () => {
    const o = dailyBars({ labels: ['a', 'b', 'c'], v: [1, 2, 3], focus: 2, goal: 2 }, ctx)
    const data = seriesOf(o)[0].data as { itemStyle: { color: string } }[]
    expect(data.filter((d) => d.itemStyle.color === micavoss.lime)).toHaveLength(1)
    expect(data[2].itemStyle.color).toBe(micavoss.lime)
  })

  it('has no lime without a focus', () => {
    expect(limeCount(dailyBars({ labels: ['a'], v: [1] }, ctx))).toBe(0)
  })
})

describe('dayTimeline', () => {
  it('keeps Body Battery the one lime series on one axis span', () => {
    const tr = { t: [from, from + 60], v: [1, 2] }
    const o = dayTimeline({ from, to, hr: tr, stress: tr, bodyBattery: tr, sleep: [{ from, to: from + 3600 }] }, ctx) as {
      xAxis: { min: number; max: number }[]
    }
    expect(limeCount(o)).toBe(1)
    expect(isLime(seriesOf(o)[2])).toBe(true)
    for (const x of o.xAxis) {
      expect(x.min).toBe(from * 1000)
      expect(x.max).toBe(to * 1000)
    }
  })
})

describe('sparkline', () => {
  it('dots the last value', () => {
    const o = sparkline({ v: [1, 2, null], lastDot: true }, ctx)
    const mp = seriesOf(o)[0].markPoint as { data: { coord: [number, number] }[] }
    expect(mp.data[0].coord).toEqual([1, 2])
    expect(limeCount(o)).toBe(1)
  })
})

describe('sparkline in time', () => {
  it('spans from..to and breaks at gaps', () => {
    const o = sparkline({ v: [10, 20, 30], t: [from, from + 60, from + 7200], from, to, gapS: 900, lastDot: true }, ctx) as {
      xAxis: { type: string; min: number; max: number }
    }
    expect(o.xAxis.type).toBe('time')
    expect(o.xAxis.min).toBe(from * 1000)
    expect(o.xAxis.max).toBe(to * 1000)
    const data = seriesOf(o)[0].data as [number, number | null][]
    expect(data.map((p) => p[1])).toEqual([10, 20, null, 30])
    const mp = seriesOf(o)[0].markPoint as { data: { coord: [number, number] }[] }
    expect(mp.data[0].coord).toEqual([(from + 7200) * 1000, 30])
  })
})

describe('dailyBars as a line', () => {
  it('draws one lime line and dots the focus', () => {
    const o = dailyBars({ labels: ['a', 'b', 'c'], v: [1, null, 3], focus: 2, kind: 'line', goal: 2 }, ctx)
    expect(limeCount(o)).toBe(1)
    const s = seriesOf(o)[0]
    expect(s.type).toBe('line')
    expect((s.markPoint as { data: { coord: number[] }[] }).data[0].coord).toEqual([2, 3])
  })
})
