// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Registers the visual map (zone colours): import this module directly, not through charts/index.
import type { Option } from '../core/echarts'
import '../core/extras'
import { stats } from '../data/series'
import { roleColor, withAlpha, type Role } from '../theme/tokens'
import { tzFormat, type Ctx } from './types'

export interface LiveLineInput {
  /** Unix seconds and values, oldest first. */
  t: number[]
  v: number[]
  /** The right edge (Unix s); the window is [now − windowS, now]. */
  now: number
  windowS?: number
  name: string
  unit?: string
  role?: Role
  kind?: 'line' | 'area' | 'step'
  min?: number
  max?: number
  /** Heart-rate zone limits (zone 1 from zones[0] … zone 5 from zones[4]): colours the line. */
  zones?: number[]
  digits?: number
}

/** liveLine draws a live value scrolling through a fixed window ending now. It never animates:
 * it is redrawn about once a second. A pause of more than 30 s breaks the line. */
export function liveLine(input: LiveLineInput, ctx: Ctx): Option {
  const t = ctx.tokens
  const win = input.windowS ?? 600
  const color = roleColor(t, input.role ?? 'primary')
  const digits = input.digits ?? 0
  const fmt = (v: number) => v.toFixed(digits)
  const clock = tzFormat(ctx, { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  const axisClock = tzFormat(ctx, { hour: '2-digit', minute: '2-digit' })
  const data: [number, number | null][] = []
  input.t.forEach((x, i) => {
    if (i > 0 && x - input.t[i - 1] > 30) data.push([(input.t[i - 1] + 1) * 1000, null])
    data.push([x * 1000, input.v[i]])
  })
  const zones = input.zones?.length === 5 ? input.zones : null
  const zoneColors = [t.grey2, t.grey, t.lime, t.amber, t.red]
  return {
    animation: false,
    grid: { left: 4, right: 12, top: 10, bottom: 4, containLabel: true },
    tooltip: {
      trigger: 'axis',
      formatter: (ps: unknown) => {
        const p = (ps as { value: [number, number | null] }[])[0]
        if (!p || p.value[1] == null) return ''
        return `<span style="font-family:${t.mono};font-size:11px;color:${t.muted}">${clock(p.value[0])}</span><br>` +
          `${input.name} <b style="font-family:${t.mono}">${fmt(p.value[1])}</b> ${input.unit ?? ''}`
      },
    },
    xAxis: {
      type: 'time', min: (input.now - win) * 1000, max: input.now * 1000, splitNumber: 5, splitLine: { show: false },
      axisLabel: { formatter: (ms: number) => axisClock(ms), hideOverlap: true },
    },
    yAxis: { type: 'value', scale: input.min == null, min: input.min, max: input.max, splitNumber: 3, minInterval: 10 ** -digits, axisLabel: { formatter: (v: number) => fmt(v) } },
    visualMap: zones ? {
      type: 'piecewise', show: false, dimension: 1, seriesIndex: 0,
      pieces: [{ lt: zones[0], color: t.faint }, ...zones.map((z, i) => ({ gte: z, lt: zones[i + 1], color: zoneColors[i] }))],
    } : undefined,
    series: [{
      type: 'line', name: input.name, data, symbol: 'none', connectNulls: false, step: input.kind === 'step' ? 'end' : undefined,
      // With zones the visual map colours the line; a colour set here would win over it.
      lineStyle: zones ? { width: 2 } : { color, width: 2 }, itemStyle: zones ? undefined : { color },
      areaStyle: input.kind === 'area' ? {
        color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: withAlpha(color, 0.26) }, { offset: 1, color: withAlpha(color, 0.02) }] },
      } : undefined,
      markPoint: data.length ? {
        symbol: 'circle', symbolSize: 9, silent: true, label: { show: false },
        itemStyle: { color, shadowBlur: 10, shadowColor: color }, data: [{ coord: [input.t[input.t.length - 1] * 1000, input.v[input.v.length - 1]] }],
      } : undefined,
    }],
  }
}

export function describeLiveLine(input: LiveLineInput): string {
  const s = stats(input.v)
  const digits = input.digits ?? 0
  if (!s) return `${input.name}: no values yet`
  const last = input.v[input.v.length - 1]
  return `${input.name} now ${last.toFixed(digits)}${input.unit ? ` ${input.unit}` : ''}; over the last ${Math.round((input.windowS ?? 600) / 60)} minutes from ${s.min.toFixed(digits)} to ${s.max.toFixed(digits)}`
}
