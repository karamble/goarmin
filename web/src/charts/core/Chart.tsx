// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useEffect, type CSSProperties } from 'react'
import type { EChartsType } from './echarts'
import { useECharts, type UseEChartsOptions } from './useECharts'

export interface ChartProps extends UseEChartsOptions {
  /** Read by screen readers instead of the canvas. */
  ariaLabel: string
  height?: number | string
  className?: string
  style?: CSSProperties
  /** Hears the chart instance once it exists (and null when it goes), e.g. to move its
   * crosshair from outside. */
  onChart?: (c: EChartsType | null) => void
}

/** Chart renders one ECharts option into a sized box. */
export function Chart({ ariaLabel, height = 200, className, style, onChart, ...opt }: ChartProps) {
  const { ref, chart } = useECharts(opt)
  useEffect(() => {
    if (!onChart) return
    onChart(chart)
    return () => onChart(null)
  }, [chart, onChart])
  return <div ref={ref} role="img" aria-label={ariaLabel} className={className} style={{ width: '100%', height, ...style }} />
}
