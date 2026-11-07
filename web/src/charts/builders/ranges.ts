// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Option } from '../core/echarts'
import { stats } from '../data/series'
import { roleColor, withAlpha } from '../theme/tokens'
import { stageColor, STAGE } from './stages'
import type { Ctx } from './types'

const tipHead = (ctx: Ctx, s: string) => `<div style="font-family:${ctx.tokens.mono};font-size:11px;color:${ctx.tokens.muted};margin-bottom:4px">${s}</div>`
const row = (color: string, name: string, v: string) =>
  `<div><span style="display:inline-block;width:8px;height:8px;border-radius:2px;background:${color};margin-right:6px"></span>${name} <b>${v}</b></div>`

// ---------------------------------------------------------------------------------------------

export interface StageBarsInput {
  /** One label per night (its day). */
  labels: string[]
  /** Hours per stage and night (null: no stages that night). */
  deep: (number | null)[]
  light: (number | null)[]
  rem: (number | null)[]
  awake: (number | null)[]
  /** The night's score, drawn as the lime line on its own axis. */
  score: (number | null)[]
  focus?: number
}

const hm = (h: number) => `${Math.floor(h)}h ${String(Math.round((h % 1) * 60)).padStart(2, '0')}m`

/** stageBars stacks each night's stage hours, with the sleep score as a line. */
export function stageBars(input: StageBarsInput, ctx: Ctx): Option {
  const t = ctx.tokens
  const stages = [
    { name: 'Deep', v: input.deep, color: stageColor(t, STAGE.deep) },
    { name: 'Light', v: input.light, color: stageColor(t, STAGE.light) },
    { name: 'REM', v: input.rem, color: stageColor(t, STAGE.rem) },
    { name: 'Awake', v: input.awake, color: stageColor(t, STAGE.awake) },
  ]
  return {
    grid: { left: 4, right: 4, top: 16, bottom: 4, containLabel: true },
    tooltip: {
      trigger: 'axis', axisPointer: { type: 'shadow' },
      formatter: (ps: unknown) => {
        const i = (ps as { dataIndex: number }[])[0].dataIndex
        const lines = stages.filter((s) => s.v[i] != null).map((s) => row(s.color, s.name, hm(s.v[i]!)))
        if (input.score[i] != null) lines.push(row(t.lime, 'Score', String(input.score[i])))
        return lines.length ? tipHead(ctx, input.labels[i]) + lines.join('') : ''
      },
    },
    xAxis: { type: 'category', data: input.labels, axisLabel: { hideOverlap: true } },
    yAxis: [
      { type: 'value', min: 0, splitNumber: 3, axisLabel: { formatter: (v: number) => `${v}h` } },
      { type: 'value', min: 0, max: 100, splitNumber: 2, splitLine: { show: false }, position: 'right' },
    ],
    series: [
      ...stages.map((s) => ({
        name: s.name, type: 'bar', stack: 'stages', barMaxWidth: 18, data: s.v,
        itemStyle: { color: s.color, opacity: 0.9 },
        emphasis: { disabled: true },
      })),
      {
        name: 'Score', type: 'line', yAxisIndex: 1, data: input.score, connectNulls: false, symbol: 'circle', symbolSize: 5,
        lineStyle: { color: t.lime, width: 2 }, itemStyle: { color: t.lime },
        markPoint: input.focus != null && input.score[input.focus] != null ? {
          symbol: 'circle', symbolSize: 10, silent: true, label: { show: false },
          itemStyle: { color: t.lime, shadowBlur: 10, shadowColor: t.lime }, data: [{ coord: [input.focus, input.score[input.focus]] }],
        } : undefined,
      },
    ],
  }
}

export function describeStageBars(input: StageBarsInput): string {
  const s = stats(input.score)
  const nights = input.score.filter((x) => x != null).length
  return s ? `Sleep over ${nights} nights: score from ${s.min} to ${s.max}, average ${Math.round(s.avg)}` : 'Sleep: no nights recorded'
}

// ---------------------------------------------------------------------------------------------

export interface RangeBarsInput {
  labels: string[]
  lo: (number | null)[]
  hi: (number | null)[]
  /** The range's name in tooltips ("Heart rate"). */
  name: string
  unit?: string
  /** A line over the bars (resting HR, average stress): lime. Without one, the focus bar is lime. */
  line?: { name: string; v: (number | null)[]; unit?: string }
  /** A dashed context line (e.g. the line's 7-day average). */
  avg?: { name: string; v: (number | null)[] }
  focus?: number
  /** Formats axis and tooltip values (clock times for bedtimes). */
  format?: (v: number) => string
  /** Fix the y range. */
  min?: number
  max?: number
  digits?: number
}

/** rangeBars draws a floating bar from low to high per day, with an optional line. */
export function rangeBars(input: RangeBarsInput, ctx: Ctx): Option {
  const t = ctx.tokens
  const f = input.format ?? ((v: number) => v.toFixed(input.digits ?? 0))
  const grey = roleColor(t, 'context2')
  const limeBars = !input.line
  const base = input.lo.map((lo, i) => (lo != null && input.hi[i] != null ? lo : null))
  const span = input.lo.map((lo, i) => (lo != null && input.hi[i] != null ? input.hi[i]! - lo : null))
  return {
    grid: { left: 4, right: 8, top: 12, bottom: 4, containLabel: true },
    tooltip: {
      trigger: 'axis', axisPointer: { type: 'shadow' },
      formatter: (ps: unknown) => {
        const i = (ps as { dataIndex: number }[])[0].dataIndex
        const lines: string[] = []
        if (input.lo[i] != null && input.hi[i] != null) lines.push(row(grey, input.name, `${f(input.lo[i]!)} – ${f(input.hi[i]!)} ${input.unit ?? ''}`))
        if (input.line?.v[i] != null) lines.push(row(t.lime, input.line.name, `${f(input.line.v[i]!)} ${input.line.unit ?? input.unit ?? ''}`))
        if (input.avg?.v[i] != null) lines.push(row(t.grey2, input.avg.name, f(input.avg.v[i]!)))
        return lines.length ? tipHead(ctx, input.labels[i]) + lines.join('') : ''
      },
    },
    xAxis: { type: 'category', data: input.labels, axisLabel: { hideOverlap: true } },
    yAxis: { type: 'value', min: input.min, max: input.max, scale: input.min == null, splitNumber: 3, axisLabel: { formatter: (v: number) => f(v) } },
    series: [
      { type: 'bar', stack: 'range', data: base, itemStyle: { color: 'transparent' }, silent: true, tooltip: { show: false }, barMaxWidth: 12 },
      {
        name: input.name, type: 'bar', stack: 'range', barMaxWidth: 12,
        data: span.map((v, i) => ({
          value: v,
          itemStyle: {
            color: limeBars && i === input.focus ? t.lime : withAlpha(grey, i === input.focus ? 0.85 : 0.45),
            borderRadius: 3,
          },
        })),
      },
      ...(input.line ? [{
        name: input.line.name, type: 'line', data: input.line.v, connectNulls: false, symbol: 'circle', symbolSize: 4,
        lineStyle: { color: t.lime, width: 2 }, itemStyle: { color: t.lime },
      }] : []),
      ...(input.avg ? [{
        name: input.avg.name, type: 'line', data: input.avg.v, connectNulls: true, symbol: 'none',
        lineStyle: { color: t.grey2, width: 1, type: 'dashed' },
      }] : []),
    ],
  }
}

export function describeRangeBars(input: RangeBarsInput): string {
  const f = input.format ?? ((v: number) => v.toFixed(input.digits ?? 0))
  const lo = stats(input.lo)
  const hi = stats(input.hi)
  if (!lo || !hi) return `${input.name}: no data`
  let s = `${input.name}: lowest ${f(lo.min)}, highest ${f(hi.max)} ${input.unit ?? ''}`.trim()
  const l = input.line && stats(input.line.v)
  if (l) s += `; ${input.line!.name} average ${f(l.avg)}`
  return s
}

