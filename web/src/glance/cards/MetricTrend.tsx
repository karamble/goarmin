// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo } from 'react'
import { useMe } from '../../api/hooks/auth'
import { useDailyWindow } from '../../api/hooks/data'
import { Chart, dailyBars, describeSeries, stats } from '../../charts'
import { fixed } from '../../lib/format'
import { addDays, shortDay } from '../../lib/period'
import { Body } from '../CardFrame'
import { metricByKey } from '../metrics'
import type { CardProps } from '../types'
import { ctx, Kpi } from './parts'

export interface TrendOptions {
  metric: string
  days: '14' | '30' | '90' | '365'
  chart: 'line' | 'bar'
}

export default function MetricTrend({ day, size, isToday, tz, options }: CardProps<TrendOptions>) {
  const units = useMe().data?.units ?? 'metric'
  const m = metricByKey.get(options.metric) ?? metricByKey.get('steps')!
  const n = Number(options.days)
  const q = useDailyWindow(day, n > 90 ? 365 : 90)
  const { labels, v } = useMemo(() => {
    const byDay = new Map((q.data ?? []).map((x) => [x.day, x]))
    const labels: string[] = []
    const v: (number | null)[] = []
    for (let i = n - 1; i >= 0; i--) {
      const d = addDays(day, -i)
      const x = byDay.get(d)
      labels.push(shortDay(d))
      v.push(x ? (m.get(x, units) ?? null) : null)
    }
    return { labels, v }
  }, [q.data, day, n, m, units])
  const s = stats(v)
  const unit = m.unit(units)
  const option = useMemo(
    () => dailyBars({ labels, v, focus: v.length - 1, kind: options.chart, axis: size !== 'S', yAxis: size === 'W' || size === 'L', unit, digits: m.digits }, ctx(tz)),
    [labels, v, options.chart, size, unit, m.digits, tz],
  )
  return (
    <Body q={[q]} empty={!s} isToday={isToday} size={size}>
      {() => (
        <div className="flex h-full flex-col gap-2">
          <div className="flex items-baseline justify-between gap-3">
            <Kpi value={fixed(v[v.length - 1], m.digits)} unit={unit}>{isToday ? 'today' : shortDay(day)}</Kpi>
            <span className="font-mono text-[11px] text-faint">{n} days · avg {fixed(s!.avg, m.digits)}</span>
          </div>
          <div className="min-h-0 flex-1">
            <Chart ariaLabel={`${m.label} over ${n} days. ${describeSeries(m.label, v, unit, m.digits)}`} height="100%" option={option} />
          </div>
        </div>
      )}
    </Body>
  )
}
