// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Registers the custom series: import this module directly, not through charts/index.
import type { Option } from '../core/echarts'
import '../core/extras'
import { STAGE, stageColor } from './stages'
import { timeLabels, tzFormat, type Ctx } from './types'

/** Lanes bottom to top: deep sleep at the bottom, awake on top. */
const lanes = [STAGE.deep, STAGE.light, STAGE.rem, STAGE.awake]
const laneNames = ['Deep', 'Light', 'REM', 'Awake']

export interface HypnogramInput {
  /** The night, Unix seconds. */
  from: number
  to: number
  segments: { from: number; to: number; stage: number }[]
}

interface RenderApi {
  value: (i: number) => number
  coord: (v: [number, number]) => [number, number]
  size: (v: [number, number]) => [number, number]
}

/** hypnogram draws a night's stages as rounded bars on four lanes (deep at the bottom). */
export function hypnogram(input: HypnogramInput, ctx: Ctx): Option {
  const t = ctx.tokens
  const span = input.to - input.from
  const time = tzFormat(ctx, { hour: '2-digit', minute: '2-digit' })
  const data = input.segments
    .filter((s) => lanes.includes(s.stage as (typeof lanes)[number]))
    .map((s) => [s.from * 1000, s.to * 1000, lanes.indexOf(s.stage as (typeof lanes)[number]), s.stage])
  return {
    grid: { left: 4, right: 8, top: 8, bottom: 4, containLabel: true },
    tooltip: {
      trigger: 'item',
      formatter: (p: unknown) => {
        const v = (p as { value: number[] }).value
        const min = Math.round((v[1] - v[0]) / 60000)
        return `<span style="font-family:${t.mono};font-size:11px;color:${t.muted}">${time(v[0])} – ${time(v[1])}</span><br>` +
          `<b>${laneNames[v[2]]}</b> ${min} min`
      },
    },
    xAxis: { type: 'time', min: input.from * 1000, max: input.to * 1000, axisLabel: { formatter: timeLabels(ctx, span), hideOverlap: true } },
    yAxis: { type: 'category', data: laneNames, axisTick: { show: false }, splitLine: { show: true } },
    series: [{
      type: 'custom',
      encode: { x: [0, 1], y: 2 },
      data,
      renderItem: (_: unknown, api: RenderApi) => {
        const a = api.coord([api.value(0), api.value(2)])
        const b = api.coord([api.value(1), api.value(2)])
        const h = api.size([0, 1])[1] * 0.62
        return {
          type: 'rect',
          shape: { x: a[0], y: a[1] - h / 2, width: Math.max(b[0] - a[0], 1), height: h, r: 2 },
          style: { fill: stageColor(t, api.value(3)) },
        }
      },
    }],
  }
}

/** describeHypnogram is the screen-reader text: minutes per stage. */
export function describeHypnogram(input: HypnogramInput): string {
  const min = new Map<number, number>()
  for (const s of input.segments) min.set(s.stage, (min.get(s.stage) ?? 0) + (s.to - s.from) / 60)
  const parts = lanes.slice().reverse().filter((l) => min.get(l)).map((l) => `${laneNames[lanes.indexOf(l)]} ${Math.round(min.get(l)!)} minutes`)
  return parts.length ? `Sleep stages: ${parts.join(', ')}` : 'Sleep stages: none recorded'
}
