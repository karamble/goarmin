// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useEffect, useState } from 'react'
import { registerTheme, THEME } from '../theme/theme'
import { echarts, type EChartsType, type Option } from './echarts'
import { useReducedMotion } from './motion'

export type ChartEvents = Record<string, (params: unknown) => void>

export interface UseEChartsOptions {
  option: Option
  /** Charts in one group share tooltip, crosshair and zoom (echarts.connect). */
  group?: string
  onEvents?: ChartEvents
  /** Start the chart only when it scrolls near the viewport (default true). */
  lazy?: boolean
}

const groups = new Map<string, number>()

function join(g: string) {
  groups.set(g, (groups.get(g) ?? 0) + 1)
  echarts.connect(g)
}

function leave(g: string) {
  const n = (groups.get(g) ?? 1) - 1
  if (n > 0) {
    groups.set(g, n)
    return
  }
  groups.delete(g)
  echarts.disconnect(g)
}

/**
 * useECharts owns one ECharts instance: it starts it on the element (lazily), applies option,
 * follows the element's size, joins a group, honours reduced motion, and disposes it. Nothing
 * else: what a chart shows is decided by the builders.
 */
export function useECharts({ option, group, onEvents, lazy = true }: UseEChartsOptions) {
  const [el, setEl] = useState<HTMLDivElement | null>(null)
  const [chart, setChart] = useState<EChartsType | null>(null)
  const reduced = useReducedMotion()

  useEffect(() => {
    if (!el) return
    let c: EChartsType | null = null
    let ro: ResizeObserver | undefined
    let io: IntersectionObserver | undefined
    let raf = 0
    const start = () => {
      registerTheme() // the default palette, unless the app registered its own first
      c = echarts.init(el, THEME, { renderer: 'canvas' })
      if (group) {
        c.group = group
        join(group)
      }
      ro = new ResizeObserver(() => {
        cancelAnimationFrame(raf)
        raf = requestAnimationFrame(() => {
          if (el.clientWidth > 0) c?.resize()
        })
      })
      ro.observe(el)
      setChart(c)
    }
    if (lazy && typeof IntersectionObserver !== 'undefined') {
      io = new IntersectionObserver((es) => {
        if (es.some((e) => e.isIntersecting)) {
          io?.disconnect()
          start()
        }
      }, { rootMargin: '200px' })
      io.observe(el)
    } else {
      start()
    }
    return () => {
      io?.disconnect()
      ro?.disconnect()
      cancelAnimationFrame(raf)
      if (c) {
        if (group) leave(group)
        c.dispose()
      }
      setChart(null)
    }
  }, [el, group, lazy])

  useEffect(() => {
    if (!chart) return
    chart.setOption(reduced ? { ...option, animation: false } : option, { notMerge: true, lazyUpdate: true })
  }, [chart, option, reduced])

  useEffect(() => {
    if (!chart || !onEvents) return
    const entries = Object.entries(onEvents)
    for (const [k, fn] of entries) chart.on(k, fn)
    return () => {
      for (const [k, fn] of entries) chart.off(k, fn)
    }
  }, [chart, onEvents])

  return { ref: setEl, chart }
}
