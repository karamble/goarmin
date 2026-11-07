// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo } from 'react'
import { useDailyWindow } from '../../api/hooks/data'
import type { Day } from '../../api/types'
import { Chart, sparkline, stats } from '../../charts'
import { addDays } from '../../lib/period'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { ctx, Kpi, n } from './parts'

const rhr = (d: Day | undefined) => d?.values.resting_hr ?? d?.resting_hr ?? null

export default function RestingHr({ day, size, isToday, tz }: CardProps) {
  const q = useDailyWindow(day, 90)
  const series = useMemo(() => {
    const byDay = new Map((q.data ?? []).map((d) => [d.day, d]))
    const out: (number | null)[] = []
    for (let i = 13; i >= 0; i--) out.push(rhr(byDay.get(addDays(day, -i))))
    return out
  }, [q.data, day])
  const value = series[series.length - 1]
  const week = stats(series.slice(-8, -1))
  const delta = value != null && week ? Math.round(value - week.avg) : null
  const option = useMemo(() => sparkline({ v: series, role: 'context2', lastDot: true }, ctx(tz)), [series, tz])
  const known = series.filter((x) => x != null).length
  return (
    <Body q={[q]} empty={value == null} isToday={isToday} size={size}>
      {() => (
        <div className="flex h-full items-end justify-between gap-3">
          <Kpi value={n(value)} unit="bpm">
            {delta == null ? 'resting' : delta === 0 ? 'same as your week' : `${delta > 0 ? '+' : '−'}${Math.abs(delta)} vs your week`}
          </Kpi>
          {known >= 3 && <div className="w-[42%] shrink-0"><Chart ariaLabel={`Resting heart rate over 14 days`} height={size === 'M' ? 120 : 48} option={option} /></div>}
        </div>
      )}
    </Body>
  )
}
