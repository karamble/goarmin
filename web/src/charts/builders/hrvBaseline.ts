// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Registers the scatter series: import this module directly, not through charts/index.
import type { Option } from '../core/echarts'
import '../core/extras'
import { stats } from '../data/series'
import { withAlpha } from '../theme/tokens'
import type { Ctx } from './types'

const tipHead = (ctx: Ctx, s: string) => `<div style="font-family:${ctx.tokens.mono};font-size:11px;color:${ctx.tokens.muted};margin-bottom:4px">${s}</div>`
const row = (color: string, name: string, v: string) =>
  `<div><span style="display:inline-block;width:8px;height:8px;border-radius:2px;background:${color};margin-right:6px"></span>${name} <b>${v}</b></div>`

export interface HrvBaselineInput {
  labels: string[]
  value: (number | null)[]
  low: (number | null)[]
  high: (number | null)[]
  /** The 7-day average: the lime line. */
  avg: (number | null)[]
  focus?: number
}

/** hrvBaseline draws the balanced band per night, each night's HRV as a dot (amber outside the
 * band) and the 7-day average as the lime line. */
export function hrvBaseline(input: HrvBaselineInput, ctx: Ctx): Option {
  const t = ctx.tokens
  const out = (i: number) => {
    const v = input.value[i]
    return v != null && ((input.low[i] != null && v < input.low[i]!) || (input.high[i] != null && v > input.high[i]!))
  }
  return {
    grid: { left: 4, right: 8, top: 12, bottom: 4, containLabel: true },
    tooltip: {
      trigger: 'axis',
      formatter: (ps: unknown) => {
        const i = (ps as { dataIndex: number }[])[0].dataIndex
        const lines: string[] = []
        if (input.value[i] != null) lines.push(row(out(i) ? t.amber : t.grey, 'Last night', `${Math.round(input.value[i]!)} ms`))
        if (input.low[i] != null && input.high[i] != null) lines.push(row(t.track, 'Balanced', `${Math.round(input.low[i]!)} – ${Math.round(input.high[i]!)} ms`))
        if (input.avg[i] != null) lines.push(row(t.lime, '7-day average', `${Math.round(input.avg[i]!)} ms`))
        return lines.length ? tipHead(ctx, input.labels[i]) + lines.join('') : ''
      },
    },
    xAxis: { type: 'category', data: input.labels, boundaryGap: false, axisLabel: { hideOverlap: true } },
    yAxis: { type: 'value', scale: true, splitNumber: 3, axisLabel: { formatter: (v: number) => `${v}` } },
    series: [
      { id: 'lo', type: 'line', stack: 'band', data: input.low, symbol: 'none', lineStyle: { opacity: 0 }, silent: true, tooltip: { show: false }, connectNulls: false },
      {
        id: 'band', type: 'line', stack: 'band', symbol: 'none', lineStyle: { opacity: 0 }, silent: true, tooltip: { show: false }, connectNulls: false,
        data: input.low.map((lo, i) => (lo != null && input.high[i] != null ? input.high[i]! - lo : null)),
        areaStyle: { color: withAlpha(t.grey, 0.1) },
      },
      {
        id: 'nights', name: 'Last night', type: 'scatter',
        data: input.value.map((v, i) => ({
          value: v, symbolSize: i === input.focus ? 10 : 6,
          itemStyle: { color: out(i) ? t.amber : t.grey2, borderColor: i === input.focus ? t.text : undefined, borderWidth: i === input.focus ? 1.5 : 0 },
        })),
      },
      { id: 'avg', name: '7-day average', type: 'line', data: input.avg, symbol: 'none', connectNulls: true, lineStyle: { color: t.lime, width: 2 } },
    ],
  }
}

export function describeHrvBaseline(input: HrvBaselineInput): string {
  const s = stats(input.value)
  if (!s) return 'HRV: no nights recorded'
  const outside = input.value.filter((v, i) => v != null && ((input.low[i] != null && v < input.low[i]!) || (input.high[i] != null && v > input.high[i]!))).length
  return `HRV over ${s.n} nights: ${Math.round(s.min)} to ${Math.round(s.max)} ms, average ${Math.round(s.avg)} ms; ${outside} outside the balanced range`
}
