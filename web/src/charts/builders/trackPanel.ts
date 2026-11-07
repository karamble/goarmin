// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Registers the visual map (zone colours): import this module directly, not through charts/index.
import type { Option } from '../core/echarts'
import '../core/extras'
import { stats } from '../data/series'
import { roleColor, withAlpha, type Role } from '../theme/tokens'
import type { Ctx } from './types'

export interface TrackPanelInput {
  /** Seconds from the start (time) or metres from the start (distance), one per point. */
  x: number[]
  xKind: 'time' | 'distance'
  imperial?: boolean
  v: (number | null)[]
  name: string
  unit?: string
  kind?: 'line' | 'area'
  role?: Role
  /** Lower is better at the top (pace). */
  inverse?: boolean
  /** Formats axis and tooltip values (pace as m:ss). */
  format?: (v: number) => string
  /** Heart-rate zone edges (zone 1 from zones[0], … zone 5 from zones[4]): colours the line. */
  zones?: number[]
  min?: number
  max?: number
  /** The y axis step (with min and max on multiples of it, the labels never collide). */
  interval?: number
}

const clockOf = (s: number) => {
  const r = Math.round(s)
  const h = Math.floor(r / 3600)
  const m = Math.floor((r % 3600) / 60)
  return h ? `${h}:${String(m).padStart(2, '0')}` : `${m}:${String(r % 60).padStart(2, '0')}`
}

/** xLabel names a point on an activity's x axis: elapsed time, or distance in km or mi. */
export function xLabel(x: number, kind: TrackPanelInput['xKind'], imperial = false): string {
  if (kind === 'time') return clockOf(x)
  return imperial ? `${(x / 1609.344).toFixed(1)} mi` : `${(x / 1000).toFixed(1)} km`
}

/** trackPanel draws one of an activity's values along the track. Panels built from the same
 * track share their points, so a group of them shares the crosshair point by point. */
export function trackPanel(input: TrackPanelInput, ctx: Ctx): Option {
  const t = ctx.tokens
  const color = roleColor(t, input.role ?? 'primary')
  const f = input.format ?? ((v: number) => String(Math.round(v)))
  const data = input.x.map((x, i) => [x, input.v[i] ?? null])
  const zoneColors = [t.grey2, t.grey, t.lime, t.amber, t.red]
  const zones = input.zones?.length === 5 ? input.zones : null
  const zoned = zones != null
  return {
    grid: { left: 4, right: 10, top: 10, bottom: 4, containLabel: true },
    tooltip: {
      trigger: 'axis',
      formatter: (ps: unknown) => {
        const p = (ps as { value: [number, number | null] }[])[0]
        if (!p || p.value[1] == null) return ''
        return `<span style="font-family:${t.mono};font-size:11px;color:${t.muted}">${xLabel(p.value[0], input.xKind, input.imperial)}</span><br>` +
          `${input.name} <b style="font-family:${t.mono}">${f(p.value[1])}</b> ${input.unit ?? ''}`
      },
    },
    xAxis: {
      type: 'value', min: input.x[0] ?? 0, max: input.x[input.x.length - 1] ?? 1, splitLine: { show: false },
      axisLabel: { formatter: (x: number) => xLabel(x, input.xKind, input.imperial), hideOverlap: true },
    },
    yAxis: {
      type: 'value', inverse: !!input.inverse, scale: input.min == null, min: input.min, max: input.max, splitNumber: 3, interval: input.interval,
      axisLabel: { formatter: (v: number) => f(v) },
    },
    visualMap: zones ? {
      type: 'piecewise', show: false, dimension: 1, seriesIndex: 0,
      pieces: [
        { lt: zones[0], color: t.faint },
        ...zones.map((z, i) => ({ gte: z, lt: zones[i + 1], color: zoneColors[i] })),
      ],
    } : undefined,
    series: [{
      type: 'line', name: input.name, data, symbol: 'none', connectNulls: false, sampling: 'lttb',
      // With zones the visual map colours the line; a colour set here would win over it.
      lineStyle: zoned ? { width: 1.6 } : { color, width: 1.6 }, itemStyle: zoned ? undefined : { color },
      areaStyle: (input.kind ?? 'line') === 'area' ? {
        color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: withAlpha(color, 0.28) }, { offset: 1, color: withAlpha(color, 0.02) }] },
      } : undefined,
    }],
  }
}

export function describeTrackPanel(input: TrackPanelInput): string {
  const s = stats(input.v)
  const f = input.format ?? ((v: number) => String(Math.round(v)))
  if (!s) return `${input.name}: no data`
  return `${input.name}: from ${f(input.inverse ? s.max : s.min)} to ${f(input.inverse ? s.min : s.max)}, average ${f(s.avg)} ${input.unit ?? ''}`.trim()
}
