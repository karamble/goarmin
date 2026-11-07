// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo } from 'react'
import { useDailyWindow } from '../../api/hooks/data'
import { Chart, sparkline } from '../../charts'
import { fixed } from '../../lib/format'
import { addDays } from '../../lib/period'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { ctx, Kpi, n, Stat } from './parts'

export default function Breathing({ day, size, isToday, tz }: CardProps) {
  const q = useDailyWindow(day, 90)
  const { d, trend } = useMemo(() => {
    const byDay = new Map((q.data ?? []).map((x) => [x.day, x]))
    const trend: (number | null)[] = []
    for (let i = 13; i >= 0; i--) trend.push(byDay.get(addDays(day, -i))?.respiration_avg ?? null)
    return { d: byDay.get(day), trend }
  }, [q.data, day])
  const option = useMemo(() => sparkline({ v: trend, role: 'context2', lastDot: true }, ctx(tz)), [trend, tz])
  const resp = d?.respiration_avg ?? null
  return (
    <Body q={[q]} empty={resp == null && d?.spo2_avg == null} isToday={isToday} size={size}>
      {() => (
        <div className="flex h-full flex-col justify-end gap-3">
          <div className="flex items-end justify-between gap-3">
            <Kpi value={fixed(resp, 1)} unit="brpm">respiration</Kpi>
            {d?.spo2_avg != null && <dl className="m-0 text-right"><Stat label="SpO2" value={n(d.spo2_avg)} unit="%" /></dl>}
          </div>
          {size !== 'S' && trend.filter((x) => x != null).length >= 3 && (
            <Chart ariaLabel="Respiration over 14 days" height={70} option={option} />
          )}
        </div>
      )}
    </Body>
  )
}
