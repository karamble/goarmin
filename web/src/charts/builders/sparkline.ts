// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Option } from '../core/echarts'
import { points } from '../data/series'
import { roleColor, withAlpha, type Role } from '../theme/tokens'
import type { Ctx } from './types'

export interface SparklineInput {
  /** Values in order (nulls break the line). */
  v: (number | null)[]
  role?: Role
  area?: boolean
  /** Mark the last value with a dot. */
  lastDot?: boolean
  min?: number
  max?: number
  /** Place the values in time (Unix seconds) over from..to, breaking at gaps longer than gapS. */
  t?: number[]
  from?: number
  to?: number
  gapS?: number
}

/** sparkline is a tiny line without axes. */
export function sparkline(input: SparklineInput, ctx: Ctx): Option {
  const color = roleColor(ctx.tokens, input.role ?? 'primary')
  let last = -1
  input.v.forEach((x, i) => {
    if (x != null) last = i
  })
  const timed = !!input.t && input.t.length === input.v.length
  const data = timed ? points(input.t!, input.v, input.gapS) : input.v
  const lastCoord = timed ? [input.t![last] * 1000, input.v[last]] : [last, input.v[last]]
  return {
    animationDuration: 400,
    grid: { left: 2, right: input.lastDot ? 6 : 2, top: 4, bottom: 2 },
    xAxis: timed
      ? { type: 'time', show: false, min: input.from != null ? input.from * 1000 : 'dataMin', max: input.to != null ? input.to * 1000 : 'dataMax' }
      : { type: 'category', show: false, boundaryGap: false, data: input.v.map((_, i) => i) },
    yAxis: { type: 'value', show: false, min: input.min ?? 'dataMin', max: input.max ?? 'dataMax' },
    series: [{
      type: 'line', data, symbol: 'none', silent: true, connectNulls: false,
      lineStyle: { color, width: 1.75 },
      areaStyle: input.area ? { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [
        { offset: 0, color: withAlpha(color, 0.28) }, { offset: 1, color: withAlpha(color, 0) }] } } : undefined,
      markPoint: input.lastDot && last >= 0 ? {
        symbol: 'circle', symbolSize: 6, silent: true, label: { show: false },
        itemStyle: { color, shadowBlur: 8, shadowColor: color }, data: [{ coord: lastCoord }],
      } : undefined,
    }],
  }
}
