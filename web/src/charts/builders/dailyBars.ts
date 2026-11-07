// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Option } from '../core/echarts'
import { roleColor, withAlpha, type Role } from '../theme/tokens'
import type { Ctx } from './types'

export interface DailyBarsInput {
  /** Category labels (e.g. dates) and values. */
  labels: string[]
  v: (number | null)[]
  /** The index drawn lime (the day in focus); the rest are grey. */
  focus?: number
  goal?: number
  goalLabel?: string
  role?: Role
  unit?: string
  digits?: number
  /** Show the category axis labels. */
  axis?: boolean
  /** Show value labels on the left. */
  yAxis?: boolean
  /** bar (default): the focus bar lime, the rest grey; line: one lime line, the focus dotted. */
  kind?: 'bar' | 'line'
  /** The top of the value axis (a score out of 100). */
  max?: number
}

/** dailyBars draws one bar per day: lime for the day in focus, grey for context, a goal line. */
export function dailyBars(input: DailyBarsInput, ctx: Ctx): Option {
  const t = ctx.tokens
  const base = roleColor(t, input.role ?? 'context2')
  const line = input.kind === 'line'
  const goal = input.goal != null ? {
    silent: true, symbol: 'none',
    data: [{ yAxis: input.goal, label: { formatter: input.goalLabel ?? 'goal', color: t.muted, fontFamily: t.mono, fontSize: 10, position: 'insideEndTop' },
      lineStyle: { color: t.grey2, type: 'dashed', width: 1 } }],
  } : undefined
  const focus = input.focus != null && input.v[input.focus] != null ? input.focus : undefined
  return {
    grid: { left: input.yAxis ? 4 : 2, right: line ? 8 : 2, top: input.goal != null ? 14 : 8, bottom: input.axis ? 4 : 2, containLabel: !!(input.yAxis || input.axis) },
    tooltip: {
      trigger: 'axis', axisPointer: { type: line ? 'line' : 'none' },
      formatter: (ps: unknown) => {
        const p = (ps as { dataIndex: number; value: number | null }[])[0]
        const v = typeof p.value === 'object' && p.value != null ? (p.value as { value: number }).value : p.value
        return v == null ? '' : `<span style="font-family:${t.mono};font-size:11px;color:${t.muted}">${input.labels[p.dataIndex]}</span><br><b style="font-family:${t.mono}">${v.toFixed(input.digits ?? 0)}</b> ${input.unit ?? ''}`
      },
    },
    xAxis: { type: 'category', data: input.labels, show: !!input.axis, boundaryGap: !line, axisLabel: { interval: 'auto', hideOverlap: true } },
    yAxis: { type: 'value', show: !!input.yAxis, min: line ? 'dataMin' : 0, max: input.max, scale: line, splitNumber: 3 },
    series: [line ? {
      type: 'line', data: input.v, symbol: 'none', connectNulls: false,
      lineStyle: { color: t.lime, width: 2 },
      areaStyle: { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [
        { offset: 0, color: withAlpha(t.lime, 0.22) }, { offset: 1, color: withAlpha(t.lime, 0) }] } },
      markPoint: focus != null ? { symbol: 'circle', symbolSize: 7, silent: true, label: { show: false },
        itemStyle: { color: t.lime, shadowBlur: 8, shadowColor: t.lime }, data: [{ coord: [focus, input.v[focus]] }] } : undefined,
      markLine: goal,
    } : {
      type: 'bar', barMaxWidth: 14, barCategoryGap: '30%',
      data: input.v.map((v, i) => ({ value: v, itemStyle: { color: i === focus ? t.lime : base, opacity: i === focus ? 1 : 0.55 } })),
      markLine: goal,
    }],
  }
}
