// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import { micavoss } from '../theme/tokens'
import { calendarHeatmap, describeCalendarHeatmap } from './calendarHeatmap'
import { dailyLines, describeDailyLines } from './dailyLines'
import { describeLiveLine, liveLine } from './liveLine'
import { describeHrvBaseline, hrvBaseline } from './hrvBaseline'
import { describeHypnogram, hypnogram } from './hypnogram'
import { describeRangeBars, describeStageBars, rangeBars, stageBars } from './ranges'
import { STAGE } from './stages'
import { describeTrackPanel, trackPanel, xLabel } from './trackPanel'

const ctx = { tokens: micavoss, tz: 'UTC' }
const now = Math.floor(Date.now() / 1000)
type S = { id?: string; type: string; data: unknown[]; stack?: string; yAxisIndex?: number } & Record<string, unknown>
const seriesOf = (o: object) => (o as { series: S[] }).series
const limeCount = (o: object) => seriesOf(o).filter((s) => JSON.stringify(s).includes(micavoss.lime)).length

/** The days of the last n days, YYYY-MM-DD, derived from now. */
const lastDays = (n: number) => Array.from({ length: n }, (_, i) => new Date((now - (n - 1 - i) * 86400) * 1000).toISOString().slice(0, 10))

describe('hypnogram', () => {
  const from = now - 8 * 3600
  const input = {
    from, to: now, segments: [
      { from, to: from + 1800, stage: STAGE.light },
      { from: from + 1800, to: from + 4200, stage: STAGE.deep },
      { from: from + 4200, to: from + 4800, stage: STAGE.awake },
      { from: from + 4800, to: from + 6000, stage: 9 }, // unknown: left out
    ],
  }
  it('puts each segment on its lane, deep at the bottom', () => {
    const o = hypnogram(input, ctx) as { xAxis: { min: number; max: number }; yAxis: { data: string[] } }
    expect(o.yAxis.data).toEqual(['Deep', 'Light', 'REM', 'Awake'])
    expect(o.xAxis.min).toBe(from * 1000)
    expect(o.xAxis.max).toBe(now * 1000)
    expect(seriesOf(o)[0].data).toEqual([
      [from * 1000, (from + 1800) * 1000, 1, STAGE.light],
      [(from + 1800) * 1000, (from + 4200) * 1000, 0, STAGE.deep],
      [(from + 4200) * 1000, (from + 4800) * 1000, 3, STAGE.awake],
    ])
    expect(limeCount(o)).toBe(0)
  })
  it('describes the minutes per stage', () => {
    expect(describeHypnogram(input)).toBe('Sleep stages: Awake 10 minutes, Light 30 minutes, Deep 40 minutes')
    expect(describeHypnogram({ from, to: now, segments: [] })).toBe('Sleep stages: none recorded')
  })
})

describe('stageBars', () => {
  const labels = lastDays(3)
  const input = { labels, deep: [1.5, null, 1], light: [4, null, 4.5], rem: [1.2, null, 1], awake: [0.3, null, 0.2], score: [78, 64, null], focus: 0 }
  it('stacks the stages and draws the score on its own axis, the only lime', () => {
    const o = stageBars(input, ctx)
    const s = seriesOf(o)
    expect(s.slice(0, 4).map((x) => x.stack)).toEqual(['stages', 'stages', 'stages', 'stages'])
    expect(s[4].yAxisIndex).toBe(1)
    expect(s[4].data).toEqual([78, 64, null])
    expect(limeCount(o)).toBe(1)
    expect(describeStageBars(input)).toBe('Sleep over 2 nights: score from 64 to 78, average 71')
  })
})

describe('rangeBars', () => {
  const labels = lastDays(4)
  const lo = [48, null, 50, 52]
  const hi = [140, 150, null, 130]
  it('floats a bar from low to high and leaves days without both empty', () => {
    const o = rangeBars({ labels, lo, hi, name: 'Heart rate', unit: 'bpm', line: { name: 'Resting', v: [55, 56, null, 54] }, focus: 3 }, ctx)
    const [base, span, line] = seriesOf(o)
    expect(base.data).toEqual([48, null, null, 52])
    expect((span.data as { value: number | null }[]).map((d) => d.value)).toEqual([92, null, null, 78])
    expect(line.data).toEqual([55, 56, null, 54])
    expect(limeCount(o)).toBe(1) // the line, not the bars
  })
  it('makes the focus bar lime when there is no line', () => {
    const o = rangeBars({ labels, lo, hi, name: 'Body Battery', focus: 0 }, ctx)
    expect(limeCount(o)).toBe(1)
    const span = seriesOf(o)[1].data as { itemStyle: { color: string } }[]
    expect(span[0].itemStyle.color).toBe(micavoss.lime)
    expect(span[3].itemStyle.color).not.toBe(micavoss.lime)
  })
  it('formats values, e.g. clock times', () => {
    const fmt = (m: number) => `${Math.floor(m / 60)}:${String(m % 60).padStart(2, '0')}`
    const d = describeRangeBars({ labels, lo: [300, 330], hi: [780, 800], name: 'Night', format: fmt })
    expect(d).toBe('Night: lowest 5:00, highest 13:20')
    expect(describeRangeBars({ labels, lo: [null], hi: [null], name: 'Night' })).toBe('Night: no data')
  })
})

describe('hrvBaseline', () => {
  const labels = lastDays(4)
  const input = { labels, value: [50, 70, null, 40], low: [45, 45, 45, 45], high: [60, 60, 60, 60], avg: [50, 60, 60, 53], focus: 3 }
  it('marks nights outside the balanced range amber and keeps lime for the average', () => {
    const o = hrvBaseline(input, ctx)
    const dots = seriesOf(o).find((s) => s.id === 'nights')!.data as { itemStyle: { color: string } }[]
    expect(dots.map((d) => d.itemStyle.color)).toEqual([micavoss.grey2, micavoss.amber, micavoss.grey2, micavoss.amber])
    const band = seriesOf(o).find((s) => s.id === 'band')!
    expect(band.data).toEqual([15, 15, 15, 15])
    expect(limeCount(o)).toBe(1)
    expect(describeHrvBaseline(input)).toBe('HRV over 3 nights: 40 to 70 ms, average 53 ms; 2 outside the balanced range')
  })
})

describe('calendarHeatmap', () => {
  const days = lastDays(10)
  const data: [string, number | null][] = days.map((d, i) => [d, i === 4 ? null : 60 + i])
  it('spans the given days and shades only days with a value', () => {
    const o = calendarHeatmap({ from: days[0], to: days[9], data, name: 'Sleep score' }, ctx) as {
      calendar: { range: string[] }; visualMap: { pieces: { color: string }[] }
    }
    expect(o.calendar.range).toEqual([days[0], days[9]])
    expect(seriesOf(o)[0].data).toHaveLength(9)
    expect(o.visualMap.pieces).toHaveLength(5)
    expect(o.visualMap.pieces[4].color).toContain('200, 245, 66') // lime at full strength
  })
  it('runs to amber when higher is worse', () => {
    const o = calendarHeatmap({ from: days[0], to: days[9], data, name: 'Stress', worse: true }, ctx) as { visualMap: { pieces: { color: string }[] } }
    expect(o.visualMap.pieces[4].color).toContain('255, 176, 32')
    expect(describeCalendarHeatmap({ from: days[0], to: days[9], data, name: 'Stress' })).toBe('Stress on 9 days: from 60 to 69, average 65')
  })
})

describe('trackPanel', () => {
  const x = [0, 60, 120, 180]
  it('puts points on a value axis, time as m:ss and distance in km', () => {
    const o = trackPanel({ x, xKind: 'time', v: [120, 140, null, 170], name: 'Heart rate', zones: [95, 114, 133, 152, 171] }, ctx) as {
      xAxis: { min: number; max: number; axisLabel: { formatter: (v: number) => string } }; visualMap: { pieces: unknown[] }
    }
    expect(o.xAxis.min).toBe(0)
    expect(o.xAxis.max).toBe(180)
    expect(o.xAxis.axisLabel.formatter(125)).toBe('2:05')
    expect(seriesOf(o)[0].data).toEqual([[0, 120], [60, 140], [120, null], [180, 170]])
    expect(o.visualMap.pieces).toHaveLength(6)
    // A colour on the line would win over the zones' colours.
    expect((seriesOf(o)[0].lineStyle as { color?: string }).color).toBeUndefined()
    expect(seriesOf(o)[0].itemStyle).toBeUndefined()
    expect(xLabel(2500, 'distance')).toBe('2.5 km')
    expect(xLabel(1609.344, 'distance', true)).toBe('1.0 mi')
    expect(xLabel(3725, 'time')).toBe('1:02')
  })
  it('turns pace upside down and describes it from slowest to fastest', () => {
    const input = { x, xKind: 'distance' as const, v: [330, 300, 280, null], name: 'Pace', inverse: true, format: (v: number) => `${Math.floor(v / 60)}:${String(Math.round(v % 60)).padStart(2, '0')}` }
    const o = trackPanel(input, ctx) as { yAxis: { inverse: boolean } }
    expect(o.yAxis.inverse).toBe(true)
    expect(describeTrackPanel(input)).toBe('Pace: from 5:30 to 4:40, average 5:03')
    expect(limeCount(o)).toBe(1)
  })

  it('dailyLines: one lime line, a stacked band, gaps joined on request', () => {
    const labels = lastDays(5)
    const input = {
      labels, connect: true,
      band: { name: 'Optimal range', lo: [80, 84, null, 88, 90], hi: [150, 158, null, 165, 168] },
      lines: [
        { name: 'Acute load', v: [120, null, 140, 150, 170], role: 'primary' as const, area: true },
        { name: 'Chronic load', v: [100, 105, null, 110, 112], dashed: true },
      ],
    }
    const o = dailyLines(input, ctx) as { series: S[]; xAxis: { data: string[] } }
    expect(o.xAxis.data).toEqual(labels)
    const [lo, width, acute, chronic] = o.series
    expect(lo.stack).toBe('band')
    expect(width.stack).toBe('band')
    expect(width.data).toEqual([70, 74, null, 77, 78])
    expect(acute.connectNulls).toBe(true)
    expect((acute.markPoint as { data: { coord: unknown[] }[] }).data[0].coord).toEqual([4, 170])
    expect(chronic.markPoint).toBeUndefined()
    expect(limeCount(o)).toBe(1)
    expect(describeDailyLines(input)).toBe('Acute load from 120 to 170 (lowest 120, highest 170); Chronic load from 100 to 112 (lowest 100, highest 112)')

    const race = { labels, inverse: true, format: (v: number) => `${Math.floor(v / 60)}:${String(v % 60).padStart(2, '0')}`, lines: [{ name: '5K', v: [1400, 1390, 1380, null, 1370], role: 'primary' as const }] }
    const r = dailyLines(race, ctx) as { yAxis: { inverse: boolean }; series: S[] }
    expect(r.yAxis.inverse).toBe(true)
    expect(r.series[0].connectNulls).toBe(false)
    expect(describeDailyLines(race)).toBe('5K from 23:20 to 22:50 (lowest 22:50, highest 23:20)')
  })

  it('liveLine: a window ending now, gaps broken, zones without a line colour', () => {
    const t = [now - 300, now - 299, now - 200, now - 199]
    const input = { t, v: [120, 125, 160, 162], now, name: 'Heart rate', unit: 'bpm', zones: [90, 108, 126, 144, 162] }
    const o = liveLine(input, ctx) as { animation: boolean; xAxis: { min: number; max: number }; series: S[]; visualMap: { pieces: unknown[] } }
    expect(o.animation).toBe(false)
    expect([o.xAxis.min, o.xAxis.max]).toEqual([(now - 600) * 1000, now * 1000])
    expect(o.series[0].data).toEqual([[t[0] * 1000, 120], [t[1] * 1000, 125], [(t[1] + 1) * 1000, null], [t[2] * 1000, 160], [t[3] * 1000, 162]])
    expect(o.visualMap.pieces).toHaveLength(6)
    expect((o.series[0].lineStyle as { color?: string }).color).toBeUndefined()
    expect(describeLiveLine(input)).toBe('Heart rate now 162 bpm; over the last 10 minutes from 120 to 162')
    expect(describeLiveLine({ ...input, v: [] })).toBe('Heart rate: no values yet')
  })
})
