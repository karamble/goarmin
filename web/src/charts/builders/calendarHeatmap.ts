// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Registers heatmap, calendar and visual map: import this module directly, not through charts/index.
import type { Option } from '../core/echarts'
import '../core/extras'
import { stats } from '../data/series'
import { withAlpha } from '../theme/tokens'
import type { Ctx } from './types'

export interface CalendarHeatmapInput {
  /** The days shown, YYYY-MM-DD (a year). */
  from: string
  to: string
  /** One value per day (missing days stay empty). */
  data: [string, number | null][]
  name: string
  unit?: string
  /** The scale; by default the data's own range. */
  min?: number
  max?: number
  /** Higher is worse (stress): the scale runs to amber instead of lime. */
  worse?: boolean
  /** The week's first day as Date.getUTCDay counts (1 Monday, the default; 0 Sunday). */
  firstDay?: number
  digits?: number
}

const STEPS = 5

/** calendarHeatmap draws a year as a week grid, each day shaded by its value (lime, or amber
 * when higher is worse). Click events carry the day as data[0]. */
export function calendarHeatmap(input: CalendarHeatmapInput, ctx: Ctx): Option {
  const t = ctx.tokens
  const vals = input.data.map(([, v]) => v)
  const s = stats(vals)
  const min = input.min ?? s?.min ?? 0
  const max = Math.max(input.max ?? s?.max ?? 1, min + 1)
  const color = input.worse ? t.amber : t.lime
  const step = (max - min) / STEPS
  const pieces = Array.from({ length: STEPS }, (_, i) => ({
    gte: min + i * step, lt: i === STEPS - 1 ? undefined : min + (i + 1) * step, lte: i === STEPS - 1 ? max : undefined,
    color: withAlpha(color, 0.18 + (0.82 * i) / (STEPS - 1)),
  }))
  return {
    tooltip: {
      formatter: (p: unknown) => {
        const [day, v] = (p as { value: [string, number | null] }).value
        return `<span style="font-family:${t.mono};font-size:11px;color:${t.muted}">${day}</span><br>` +
          (v == null ? 'no data' : `${input.name} <b style="font-family:${t.mono}">${v.toFixed(input.digits ?? 0)}</b> ${input.unit ?? ''}`)
      },
    },
    visualMap: { type: 'piecewise', show: false, dimension: 1, pieces, outOfRange: { color: t.track } },
    calendar: {
      range: [input.from, input.to], top: 22, left: 30, right: 6, bottom: 4, cellSize: ['auto', 'auto'],
      orient: 'horizontal', splitLine: { show: false },
      itemStyle: { color: t.panel, borderColor: t.grid, borderWidth: 2 },
      dayLabel: { firstDay: input.firstDay ?? ctx.firstDay ?? 1, nameMap: ['S', 'M', 'T', 'W', 'T', 'F', 'S'], color: t.faint, fontFamily: t.mono, fontSize: 9 },
      monthLabel: { color: t.muted, fontFamily: t.mono, fontSize: 10 },
      yearLabel: { show: false },
    },
    series: [{
      type: 'heatmap', coordinateSystem: 'calendar',
      data: input.data.filter(([, v]) => v != null),
    }],
  }
}

export function describeCalendarHeatmap(input: CalendarHeatmapInput): string {
  const s = stats(input.data.map(([, v]) => v))
  if (!s) return `${input.name}: no days recorded`
  const d = input.digits ?? 0
  return `${input.name} on ${s.n} days: from ${s.min.toFixed(d)} to ${s.max.toFixed(d)}, average ${s.avg.toFixed(d)} ${input.unit ?? ''}`.trim()
}
