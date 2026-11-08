// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo } from 'react'
import { useDailyWindow } from '../../api/hooks/data'
import { Chart, sparkline } from '../../charts'
import { addDays } from '../../lib/period'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { ctx, Kpi } from './parts'

/** dailyValueCard makes a card of one daily value: the day's (or the last before it within a
 * week), with two weeks of it beside. */
export function dailyValueCard(opt: { key: string; unit: string; digits: number; signed?: boolean; label: string; empty: string }) {
  return function DailyValue({ day, size, isToday, tz }: CardProps) {
    const q = useDailyWindow(day, 90)
    const series = useMemo(() => {
      const byDay = new Map((q.data ?? []).map((d) => [d.day, d]))
      const out: (number | null)[] = []
      for (let i = 13; i >= 0; i--) out.push(byDay.get(addDays(day, -i))?.values[opt.key] ?? null)
      return out
    }, [q.data, day])
    const lastI = series.findLastIndex((v) => v != null)
    const value = lastI >= 7 ? series[lastI] : null
    const option = useMemo(() => sparkline({ v: series, role: 'context2', lastDot: true }, ctx(tz)), [series, tz])
    const text = value == null ? '–' : `${opt.signed && value > 0 ? '+' : value < 0 ? '−' : ''}${Math.abs(value).toFixed(opt.digits)}`
    return (
      <Body q={[q]} empty={value == null} isToday={isToday} size={size} emptyText={opt.empty}>
        {() => (
          <div className="flex h-full items-end justify-between gap-3">
            <Kpi value={text} unit={opt.unit}>{lastI === 13 ? opt.label : `${opt.label} · ${13 - lastI} days ago`}</Kpi>
            {series.filter((v) => v != null).length >= 3 && <div className="w-[42%] shrink-0"><Chart ariaLabel={`${opt.label} over 14 days`} height={size === 'M' ? 120 : 48} option={option} /></div>}
          </div>
        )}
      </Body>
    )
  }
}
