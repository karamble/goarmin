// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo } from 'react'
import { useMe } from '../../api/hooks/auth'
import { useBody } from '../../api/hooks/readers'
import { Chart, sparkline } from '../../charts'
import { addDays, relDay } from '../../lib/period'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { ctx, Kpi } from './parts'

/** Weight: the last weigh-in up to the day, against the one before, with the recent ones. */
export default function Weight({ day, size, isToday, tz }: CardProps) {
  const imperial = useMe().data?.units === 'imperial'
  const q = useBody(addDays(day, -365), day)
  const ws = useMemo(() => (q.data?.weigh_ins ?? []).filter((w) => w.day <= day), [q.data, day])
  const f = imperial ? 2.20462 : 1
  const last = ws.at(-1)
  const prev = ws.at(-2)
  const option = useMemo(() => sparkline({ v: ws.slice(-14).map((w) => w.weight_kg * f), role: 'context2', lastDot: true }, ctx(tz)), [ws, f, tz])
  return (
    <Body q={[q]} empty={!last} isToday={isToday} size={size} emptyText="No weigh-in yet">
      {() => (
        <div className="flex h-full items-end justify-between gap-3">
          <Kpi value={(last!.weight_kg * f).toFixed(1)} unit={imperial ? 'lb' : 'kg'}>
            {relDay(last!.day, day)}{prev ? ` · ${last!.weight_kg >= prev.weight_kg ? '+' : '−'}${(Math.abs(last!.weight_kg - prev.weight_kg) * f).toFixed(1)}` : ''}
            {last!.fat_pct != null ? ` · ${last!.fat_pct.toFixed(1)} % fat` : ''}
          </Kpi>
          {ws.length >= 3 && <div className="w-[42%] shrink-0"><Chart ariaLabel="Recent weigh-ins" height={size === 'M' ? 120 : 48} option={option} /></div>}
        </div>
      )}
    </Body>
  )
}
