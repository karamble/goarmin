// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { echarts } from '../core/echarts'
import { micavoss, type ChartTokens } from './tokens'

export const THEME = 'micavoss'

/** buildTheme is the registered ECharts theme: transparent, mono axes, hairline grid, glass tooltip. */
export function buildTheme(t: ChartTokens): object {
  const axis = {
    axisLine: { show: false, lineStyle: { color: t.border } },
    axisTick: { show: false },
    axisLabel: { color: t.muted, fontFamily: t.mono, fontSize: 11 },
    splitLine: { lineStyle: { color: t.grid } },
    nameTextStyle: { color: t.faint, fontFamily: t.mono, fontSize: 11 },
  }
  return {
    backgroundColor: 'transparent',
    color: [t.lime, t.grey, t.cyan, t.pink, t.amber],
    textStyle: { fontFamily: t.sans, color: t.muted },
    categoryAxis: { ...axis, splitLine: { show: false } },
    valueAxis: axis,
    timeAxis: { ...axis, splitLine: { show: false } },
    tooltip: {
      backgroundColor: 'rgba(22, 27, 34, 0.94)',
      borderColor: t.border,
      borderWidth: 1,
      padding: [8, 12],
      textStyle: { color: t.text, fontFamily: t.sans, fontSize: 12 },
      extraCssText: 'border-radius: 12px; box-shadow: 0 12px 32px rgba(0,0,0,.45); backdrop-filter: blur(8px);',
      axisPointer: { lineStyle: { color: t.faint }, crossStyle: { color: t.faint } },
    },
    line: { symbol: 'none', lineStyle: { width: 2 }, emphasis: { disabled: true } },
    bar: { itemStyle: { borderRadius: [3, 3, 0, 0] }, emphasis: { disabled: true } },
    markLine: { symbol: 'none', silent: true },
  }
}

let registered = false

/** registerTheme installs the theme once; call it before the first chart. */
export function registerTheme(tokens: ChartTokens = micavoss): void {
  if (registered) return
  echarts.registerTheme(THEME, buildTheme(tokens))
  registered = true
}
