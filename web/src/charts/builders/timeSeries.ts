// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Option } from '../core/echarts'
import { points, stats } from '../data/series'
import { roleColor, withAlpha, type Role } from '../theme/tokens'
import { timeLabels, tzFormat, type Ctx } from './types'

export interface SeriesSpec {
  id: string
  name: string
  t: number[]
  v: (number | null)[]
  /** A band behind the line (bucketed series). */
  min?: number[]
  max?: number[]
  kind: 'line' | 'area' | 'bar' | 'step'
  role: Role
  unit?: string
  /** Break the line where samples are further apart than this (s). */
  gapS?: number
  yAxis?: number
}

export interface TimeSeriesInput {
  /** The x span in Unix seconds (the server's from/to, so a day axis is a whole day). */
  from: number
  to: number
  series: SeriesSpec[]
  y?: { min?: number; max?: number; unit?: string }[]
  markLines?: { value: number; label: string; role?: Role }[]
  markAreas?: { from: number; to: number; role: Role; label?: string }[]
  zoom?: boolean
  /** Fixed decimals for tooltip values. */
  digits?: number
}

/** timeSeries draws lines, areas, bars or steps on a time axis, with optional min/max bands. */
export function timeSeries(input: TimeSeriesInput, ctx: Ctx): Option {
  const t = ctx.tokens
  const series: object[] = []
  for (const s of input.series) {
    const color = roleColor(t, s.role)
    const data = points(s.t, s.v, s.gapS)
    if (s.min && s.max && s.min.length === s.t.length) {
      // Band: an invisible lower line and the stacked difference, filled.
      const stack = 'band-' + s.id
      series.push({
        id: s.id + '-lo', type: 'line', stack, silent: true, symbol: 'none', yAxisIndex: s.yAxis ?? 0,
        lineStyle: { opacity: 0 }, data: points(s.t, s.min, s.gapS), tooltip: { show: false },
      })
      series.push({
        id: s.id + '-band', type: 'line', stack, silent: true, symbol: 'none', yAxisIndex: s.yAxis ?? 0,
        lineStyle: { opacity: 0 }, areaStyle: { color: withAlpha(color, 0.14) }, tooltip: { show: false },
        data: points(s.t, s.max.map((x, i) => x - s.min![i]), s.gapS),
      })
    }
    const base = { id: s.id, name: s.name, yAxisIndex: s.yAxis ?? 0, data, sampling: 'lttb' as const }
    if (s.kind === 'bar') {
      series.push({ ...base, type: 'bar', itemStyle: { color }, barMaxWidth: 12 })
    } else {
      series.push({
        ...base, type: 'line', symbol: 'none', step: s.kind === 'step' ? 'end' : undefined,
        lineStyle: { color, width: 2 }, itemStyle: { color },
        areaStyle: s.kind === 'area' ? { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [
          { offset: 0, color: withAlpha(color, 0.32) }, { offset: 1, color: withAlpha(color, 0.02) }] } } : undefined,
      })
    }
  }
  const first = series.find((s) => !String((s as { id: string }).id).includes('-')) as Record<string, unknown> | undefined
  if (first && (input.markLines?.length || input.markAreas?.length)) {
    if (input.markLines?.length) {
      first.markLine = {
        silent: true, symbol: 'none',
        data: input.markLines.map((m) => ({
          yAxis: m.value, label: { formatter: m.label, color: t.muted, fontFamily: t.mono, fontSize: 10, position: 'insideEndTop' },
          lineStyle: { color: roleColor(t, m.role ?? 'context2'), type: 'dashed', width: 1 },
        })),
      }
    }
    if (input.markAreas?.length) {
      first.markArea = {
        silent: true,
        data: input.markAreas.map((a) => [
          { xAxis: a.from * 1000, itemStyle: { color: withAlpha(roleColor(t, a.role), 0.08) },
            label: a.label ? { show: true, formatter: a.label, color: t.faint, fontFamily: t.mono, fontSize: 10, position: 'insideTop' } : undefined },
          { xAxis: a.to * 1000 },
        ]),
      }
    }
  }
  const span = input.to - input.from
  const tip = tzFormat(ctx, span <= 2 * 86400 ? { hour: '2-digit', minute: '2-digit' } : { weekday: 'short', day: 'numeric', month: 'short' })
  const ys = input.y ?? [{}]
  return {
    grid: { left: 4, right: 8, top: 18, bottom: input.zoom ? 34 : 4, containLabel: true },
    tooltip: {
      trigger: 'axis',
      formatter: (params: unknown) => {
        const ps = (Array.isArray(params) ? params : [params]) as { seriesId: string; seriesName: string; value: [number, number | null]; color: string }[]
        const shown = ps.filter((p) => !p.seriesId.includes('-') && p.value[1] != null)
        if (!shown.length) return ''
        const head = `<div style="font-family:${t.mono};font-size:11px;color:${t.muted};margin-bottom:4px">${tip(shown[0].value[0])}</div>`
        return head + shown.map((p) => {
          const spec = input.series.find((s) => s.id === p.seriesId)
          const v = p.value[1] as number
          return `<div><span style="display:inline-block;width:8px;height:8px;border-radius:2px;background:${p.color};margin-right:6px"></span>${p.seriesName} <b style="font-family:${t.mono}">${v.toFixed(input.digits ?? 0)}</b> ${spec?.unit ?? ''}</div>`
        }).join('')
      },
    },
    xAxis: { type: 'time', min: input.from * 1000, max: input.to * 1000, axisLabel: { formatter: timeLabels(ctx, span), hideOverlap: true } },
    yAxis: ys.map((y, i) => ({ type: 'value', min: y.min, max: y.max, scale: y.min == null, splitNumber: 3, position: i ? 'right' : 'left',
      splitLine: { show: i === 0 }, axisLabel: { formatter: (v: number) => String(Math.round(v * 100) / 100) } })),
    dataZoom: input.zoom ? [{ type: 'inside', zoomOnMouseWheel: 'ctrl', moveOnMouseWheel: 'shift', moveOnMouseMove: true },
      { type: 'slider', height: 18, bottom: 4, borderColor: t.border, fillerColor: withAlpha(t.lime, 0.12), dataBackground: { lineStyle: { color: t.faint }, areaStyle: { color: t.track } } }] : undefined,
    series,
  }
}

/** describeSeries is the screen-reader text of one series. */
export function describeSeries(name: string, v: (number | null)[], unit = '', digits = 0): string {
  const s = stats(v)
  if (!s) return `${name}: no data`
  const f = (x: number) => x.toFixed(digits)
  return `${name}: minimum ${f(s.min)}, maximum ${f(s.max)}, average ${f(s.avg)} ${unit}`.trim()
}
