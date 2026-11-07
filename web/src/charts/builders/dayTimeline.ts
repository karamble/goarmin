// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Option } from '../core/echarts'
import { points } from '../data/series'
import { withAlpha } from '../theme/tokens'
import { timeLabels, tzFormat, type Ctx } from './types'

export interface Track {
  t: number[]
  v: (number | null)[]
}

export interface DayTimelineInput {
  from: number
  to: number
  hr?: Track
  stress?: Track
  bodyBattery?: Track
  sleep?: { from: number; to: number }[]
  activities?: { from: number; to: number; label: string }[]
}

/**
 * dayTimeline stacks a day's heart rate (grey line), stress (amber bars) and Body Battery (the
 * lime area: the one thing that matters) on one shared time axis with one crosshair. Sleep is a
 * cyan band, activities grey bands.
 */
export function dayTimeline(input: DayTimelineInput, ctx: Ctx): Option {
  const t = ctx.tokens
  const grids = [
    { left: 36, right: 10, top: 10, height: '28%' },
    { left: 36, right: 10, top: '42%', height: '18%' },
    { left: 36, right: 10, top: '66%', height: '24%' },
  ]
  const label = timeLabels(ctx, input.to - input.from)
  const tip = tzFormat(ctx, { hour: '2-digit', minute: '2-digit' })
  const bands = [
    ...(input.sleep ?? []).map((s) => [{ xAxis: s.from * 1000, itemStyle: { color: withAlpha(t.cyan, 0.08) } }, { xAxis: s.to * 1000 }]),
    ...(input.activities ?? []).map((a) => [{ xAxis: a.from * 1000, itemStyle: { color: withAlpha(t.grey, 0.08) } }, { xAxis: a.to * 1000 }]),
  ]
  const markArea = bands.length ? { silent: true, data: bands } : undefined
  const x = (i: number) => ({
    type: 'time', gridIndex: i, min: input.from * 1000, max: input.to * 1000,
    axisLabel: { show: i === 2, formatter: label, hideOverlap: true }, splitLine: { show: false },
  })
  const y = (i: number, name: string, max?: number) => ({
    type: 'value', gridIndex: i, splitNumber: 2, min: 0, max, name, nameLocation: 'end', nameGap: 4,
    nameTextStyle: { align: 'left', padding: [0, 0, 0, -30] },
  })
  return {
    animationDuration: 500,
    axisPointer: { link: [{ xAxisIndex: 'all' }], label: { show: false } },
    tooltip: {
      trigger: 'axis', axisPointer: { type: 'line' },
      formatter: (params: unknown) => {
        const ps = params as { seriesName: string; value: [number, number | null]; color: string }[]
        const shown = ps.filter((p) => p.value?.[1] != null)
        if (!shown.length) return ''
        return `<div style="font-family:${t.mono};font-size:11px;color:${t.muted};margin-bottom:4px">${tip(shown[0].value[0])}</div>` +
          shown.map((p) => `<div>${p.seriesName} <b style="font-family:${t.mono}">${Math.round(p.value[1] as number)}</b></div>`).join('')
      },
    },
    grid: grids,
    xAxis: [x(0), x(1), x(2)],
    yAxis: [y(0, 'HR'), y(1, 'STRESS', 100), y(2, 'BB', 100)],
    series: [
      { name: 'Heart rate', type: 'line', xAxisIndex: 0, yAxisIndex: 0, symbol: 'none', sampling: 'lttb',
        lineStyle: { color: t.grey, width: 1.5 }, itemStyle: { color: t.grey },
        data: input.hr ? points(input.hr.t, input.hr.v, 900) : [], markArea },
      { name: 'Stress', type: 'bar', xAxisIndex: 1, yAxisIndex: 1, barMaxWidth: 3, itemStyle: { color: t.amber, opacity: 0.75 },
        data: input.stress ? points(input.stress.t, input.stress.v) : [], markArea },
      { name: 'Body Battery', type: 'line', xAxisIndex: 2, yAxisIndex: 2, symbol: 'none', sampling: 'lttb',
        lineStyle: { color: t.lime, width: 2 }, itemStyle: { color: t.lime },
        areaStyle: { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [
          { offset: 0, color: withAlpha(t.lime, 0.3) }, { offset: 1, color: withAlpha(t.lime, 0.02) }] } },
        data: input.bodyBattery ? points(input.bodyBattery.t, input.bodyBattery.v, 900) : [], markArea },
    ],
  }
}
