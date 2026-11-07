// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Option } from '../core/echarts'
import { stats } from '../data/series'
import { roleColor, withAlpha, type Role } from '../theme/tokens'
import type { Ctx } from './types'

export interface DailyLine {
  name: string
  v: (number | null)[]
  /** primary (lime) for the one line that matters; context greys otherwise. */
  role?: Role
  dashed?: boolean
  area?: boolean
}

export interface DailyLinesInput {
  /** One label per day. */
  labels: string[]
  lines: DailyLine[]
  /** A shaded range per day (e.g. the optimal load range), drawn under the lines. */
  band?: { name: string; lo: (number | null)[]; hi: (number | null)[] }
  unit?: string
  /** Formats axis and tooltip values (race times as clock times). */
  format?: (v: number) => string
  /** Lower values at the top (race times: faster is up). */
  inverse?: boolean
  /** Join the points across days without a value (values the watch writes now and then). */
  connect?: boolean
  min?: number
  max?: number
  /** The smallest step between axis labels (0.1 for one decimal, so labels never repeat). */
  minInterval?: number
  /** The day in focus (the last one with a value gets a glowing dot). */
  focus?: number
}

const tipHead = (ctx: Ctx, s: string) => `<div style="font-family:${ctx.tokens.mono};font-size:11px;color:${ctx.tokens.muted};margin-bottom:4px">${s}</div>`

/** dailyLines draws values per day as lines on a day axis: one lime, the rest context, with an
 * optional per-day band. */
export function dailyLines(input: DailyLinesInput, ctx: Ctx): Option {
  const t = ctx.tokens
  const f = input.format ?? ((v: number) => String(Math.round(v)))
  const unit = input.unit ? ` ${input.unit}` : ''
  const band = input.band
  const lines = input.lines.map((l) => ({ ...l, color: roleColor(t, l.role ?? 'context') }))
  const dots = input.labels.length <= 62
  const bandSeries = band ? [
    { name: `${band.name} low`, type: 'line', stack: 'band', data: band.lo, symbol: 'none', lineStyle: { opacity: 0 }, silent: true, connectNulls: !!input.connect },
    {
      name: band.name, type: 'line', stack: 'band', symbol: 'none', lineStyle: { opacity: 0 }, silent: true, connectNulls: !!input.connect,
      data: band.hi.map((h, i) => (h == null || band.lo[i] == null ? null : h - band.lo[i]!)),
      areaStyle: { color: withAlpha(t.grey, 0.12) },
    },
  ] : []
  return {
    grid: { left: 4, right: 22, top: 14, bottom: 4, containLabel: true },
    tooltip: {
      trigger: 'axis',
      formatter: (ps: unknown) => {
        const i = (ps as { dataIndex: number }[])[0]?.dataIndex
        if (i == null) return ''
        const rows = lines.filter((l) => l.v[i] != null).map((l) =>
          `<div><span style="display:inline-block;width:8px;height:8px;border-radius:2px;background:${l.color};margin-right:6px"></span>${l.name} <b style="font-family:${t.mono}">${f(l.v[i]!)}</b>${unit}</div>`)
        if (band && band.lo[i] != null && band.hi[i] != null) {
          rows.push(`<div style="color:${t.muted}">${band.name} ${f(band.lo[i]!)}–${f(band.hi[i]!)}</div>`)
        }
        return rows.length ? tipHead(ctx, input.labels[i]) + rows.join('') : ''
      },
    },
    xAxis: { type: 'category', data: input.labels, boundaryGap: false, axisLabel: { hideOverlap: true } },
    yAxis: {
      type: 'value', inverse: !!input.inverse, scale: input.min == null, min: input.min, max: input.max, splitNumber: 3, minInterval: input.minInterval,
      axisLabel: { formatter: (v: number) => f(v) },
    },
    series: [
      ...bandSeries,
      ...lines.map((l) => {
        const primary = (l.role ?? 'context') === 'primary'
        const last = input.focus ?? lastIndex(l.v)
        return {
          name: l.name, type: 'line', data: l.v, connectNulls: !!input.connect,
          symbol: dots ? 'circle' : 'none', symbolSize: primary ? 5 : 3, showSymbol: dots,
          lineStyle: { color: l.color, width: primary ? 2 : 1.4, type: l.dashed ? 'dashed' : 'solid' },
          itemStyle: { color: l.color },
          areaStyle: l.area ? {
            color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: withAlpha(l.color, 0.24) }, { offset: 1, color: withAlpha(l.color, 0.02) }] },
          } : undefined,
          markPoint: primary && last >= 0 && l.v[last] != null ? {
            symbol: 'circle', symbolSize: 10, silent: true, label: { show: false },
            itemStyle: { color: l.color, shadowBlur: 10, shadowColor: l.color }, data: [{ coord: [last, l.v[last]] }],
          } : undefined,
        }
      }),
    ],
  }
}

function lastIndex(v: (number | null)[]): number {
  for (let i = v.length - 1; i >= 0; i--) if (v[i] != null) return i
  return -1
}

export function describeDailyLines(input: DailyLinesInput): string {
  const f = input.format ?? ((v: number) => String(Math.round(v)))
  const parts = input.lines.map((l) => {
    const s = stats(l.v)
    if (!s) return `${l.name}: no values`
    const first = l.v.find((x) => x != null)!
    const last = l.v[lastIndex(l.v)]!
    return `${l.name} from ${f(first)} to ${f(last)} (lowest ${f(s.min)}, highest ${f(s.max)})`
  })
  return `${parts.join('; ')}${input.unit ? ` ${input.unit}` : ''}`
}
