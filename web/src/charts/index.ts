// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

export { Chart } from './core/Chart'
export { useECharts } from './core/useECharts'
export { useReducedMotion } from './core/motion'
export { registerTheme, THEME } from './theme/theme'
export { micavoss, roleColor, withAlpha, type ChartTokens, type Role } from './theme/tokens'
export { timeSeries, describeSeries, type SeriesSpec, type TimeSeriesInput } from './builders/timeSeries'
export { sparkline } from './builders/sparkline'
export { dailyBars } from './builders/dailyBars'
export { dayTimeline } from './builders/dayTimeline'
export { stageColor, STAGE } from './builders/stages'
export { stageBars, describeStageBars, rangeBars, describeRangeBars, type StageBarsInput, type RangeBarsInput } from './builders/ranges'
// hypnogram, hrvBaseline and calendarHeatmap register heavier chart types (core/extras): import
// them from their modules, so only the pages that draw them load those types.
export type { Ctx } from './builders/types'
export { points, stats } from './data/series'
export { Ring, Arc } from './svg/Ring'
