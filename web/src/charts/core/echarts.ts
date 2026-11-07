// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// The core: what the glance and most pages draw. Heavier chart types (custom shapes, heatmaps,
// calendars, scatter dots, visual maps) are registered in ./extras by the builders that need
// them, so they load with those pages only.
import { BarChart, LineChart } from 'echarts/charts'
import {
  AxisPointerComponent, DataZoomInsideComponent, DataZoomSliderComponent, GridComponent,
  MarkAreaComponent, MarkLineComponent, MarkPointComponent, TooltipComponent,
} from 'echarts/components'
import * as echarts from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'

echarts.use([
  LineChart, BarChart,
  GridComponent, TooltipComponent, AxisPointerComponent, DataZoomInsideComponent, DataZoomSliderComponent,
  MarkLineComponent, MarkAreaComponent, MarkPointComponent,
  CanvasRenderer,
])

export { echarts }
export type { EChartsCoreOption as Option, EChartsType } from 'echarts/core'
