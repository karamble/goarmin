// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo } from 'react'
import { useDailyWindow } from '../../api/hooks/data'
import { hrvStatus, type Day } from '../../api/types'
import { Chart, sparkline } from '../../charts'
import { addDays } from '../../lib/period'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { ctx, Kpi, n } from './parts'

function hrvOf(d: Day | undefined) {
  if (!d) return null
  const value = d.values.hrv_last_night ?? d.hrv?.last_night_ms ?? null
  if (value == null) return null
  return {
    value,
    low: d.values.hrv_target_low ?? d.hrv?.balanced_lower_ms ?? null,
    high: d.values.hrv_target_high ?? d.hrv?.balanced_upper_ms ?? null,
    weekly: d.hrv?.weekly_avg_ms ?? null,
    status: d.hrv?.status != null && d.hrv.status > 0 ? hrvStatus[d.hrv.status] ?? null : null,
  }
}

export default function Hrv({ day, size, isToday, tz }: CardProps) {
  const q = useDailyWindow(day, 90)
  const { h, nights } = useMemo(() => {
    const byDay = new Map((q.data ?? []).map((d) => [d.day, d]))
    const nights: (number | null)[] = []
    for (let i = 13; i >= 0; i--) nights.push(hrvOf(byDay.get(addDays(day, -i)))?.value ?? null)
    return { h: hrvOf(byDay.get(day)), nights }
  }, [q.data, day])
  const option = useMemo(() => sparkline({ v: nights, lastDot: true, role: 'primary' }, ctx(tz)), [nights, tz])
  const known = nights.filter((x) => x != null).length

  const place = h && h.low != null && h.high != null ? (h.value < h.low ? 'below' : h.value > h.high ? 'above' : 'within') : null
  const verdict = h?.status ?? (place ? `${place[0].toUpperCase()}${place.slice(1)} your range` : '')

  return (
    <Body q={[q]} empty={!h} isToday={isToday} size={size} emptyText="No HRV from last night">
      {() => {
        const v = h!
        const lo = Math.min(v.low ?? v.value, v.value) - 12
        const hi = Math.max(v.high ?? v.value, v.value) + 12
        const x = (ms: number) => `${((ms - lo) / (hi - lo)) * 100}%`
        return size === 'S' ? (
          <div className="flex h-full items-end justify-between gap-3">
            <Kpi value={n(v.value)} unit="ms">{verdict}</Kpi>
          </div>
        ) : (
          <div className="flex h-full flex-col gap-4">
            <div className="flex items-start justify-between gap-3">
              <Kpi value={n(v.value)} unit="ms" big>last night</Kpi>
              {verdict && (
                <span className={`badge-cut ${place === 'within' || v.status === 'Balanced' ? '' : '!bg-amber'}`}>{verdict}</span>
              )}
            </div>
            {v.low != null && v.high != null && (
              <div className="pt-4" role="img" aria-label={`HRV ${v.value} ms; your range ${v.low} to ${v.high} ms`}>
                <div className="relative h-2 rounded-full bg-track">
                  <div className="absolute inset-y-0 rounded-full bg-grey/25" style={{ left: x(v.low), width: `calc(${x(v.high)} - ${x(v.low)})` }} />
                  <div className="absolute top-1/2 h-3.5 w-3.5 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-bg bg-lime shadow-[0_0_12px_var(--lime-glow)]"
                    style={{ left: x(v.value) }} />
                </div>
                <div className="relative mt-2 h-4 font-mono text-[11px] text-faint">
                  <span className="absolute -translate-x-1/2" style={{ left: x(v.low) }}>{v.low}</span>
                  <span className="absolute -translate-x-1/2" style={{ left: x(v.high) }}>{v.high}</span>
                </div>
              </div>
            )}
            <div className="mt-auto">
              {known >= 3 ? (
                <Chart ariaLabel={`HRV over the last 14 nights, ${known} nights recorded`} height={size === 'L' ? 70 : 52} option={option} />
              ) : (
                <p className="m-0 text-[12px] text-faint">
                  {v.weekly != null ? <>7-day average <span className="num text-muted">{n(v.weekly)} ms</span></> : 'The trend shows after a few nights.'}
                </p>
              )}
            </div>
          </div>
        )
      }}
    </Body>
  )
}
