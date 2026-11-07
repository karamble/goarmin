// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// The heavier chart types, registered by the builders that draw them (hypnogram: custom;
// calendarHeatmap: heatmap, calendar, visual map; hrvBaseline: scatter). Importing this module
// is enough; registering twice is harmless.
import { CustomChart, HeatmapChart, ScatterChart } from 'echarts/charts'
import { CalendarComponent, VisualMapPiecewiseComponent } from 'echarts/components'
import * as echarts from 'echarts/core'

echarts.use([CustomChart, HeatmapChart, ScatterChart, CalendarComponent, VisualMapPiecewiseComponent])

export const extras = true
