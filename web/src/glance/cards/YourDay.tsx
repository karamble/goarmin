// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo } from 'react'
import { useDaySeries, useNights, useRecentActivities } from '../../api/hooks/data'
import type { Series } from '../../api/types'
import { Chart, dayTimeline, describeSeries } from '../../charts'
import { addDays } from '../../lib/period'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { ctx } from './parts'

/** clean drops the watch's "no reading" values (≤ 0 heart rate, negative stress). */
function clean(s: Series | undefined, floor: number) {
  return s ? { t: s.t, v: s.v.map((x) => (x < floor ? null : x)) } : undefined
}

const legend = [
  { label: 'Heart rate', color: 'var(--grey)' },
  { label: 'Stress', color: 'var(--amber)' },
  { label: 'Body Battery', color: 'var(--lime)' },
  { label: 'Sleep', color: 'var(--cyan)' },
]

export default function YourDay({ day, size, isToday, tz }: CardProps) {
  const hr = useDaySeries('heart_rate', day)
  const stress = useDaySeries('stress', day)
  const bb = useDaySeries('body_battery', day)
  const nights = useNights(addDays(day, -6), day)
  const acts = useRecentActivities(50)
  const span = hr.data ?? bb.data ?? stress.data
  const option = useMemo(() => {
    if (!span) return null
    const { from, to } = span
    const within = (a: number, b: number) => a < to && b > from
    return dayTimeline({
      from, to,
      hr: clean(hr.data, 1),
      stress: clean(stress.data, 0),
      bodyBattery: clean(bb.data, 0),
      sleep: (nights.data ?? []).filter((x) => within(x.start, x.end)).map((x) => ({ from: Math.max(x.start, from), to: Math.min(x.end, to) })),
      activities: (acts.data?.items ?? []).filter((a) => within(a.start, a.end)).map((a) => ({ from: a.start, to: a.end, label: a.sport_label })),
    }, ctx(tz))
  }, [span, hr.data, stress.data, bb.data, nights.data, acts.data, tz])
  const empty = !hr.data?.t.length && !stress.data?.t.length && !bb.data?.t.length
  const aria = [describeSeries('Heart rate', hr.data?.v ?? [], 'bpm'), describeSeries('Stress', stress.data?.v ?? []),
    describeSeries('Body Battery', bb.data?.v ?? [], '%')].join('. ')
  return (
    <Body q={[hr, stress, bb]} empty={empty} isToday={isToday} size={size}>
      {() => (
        <div className="flex h-full flex-col">
          <ul className="m-0 -mt-1 mb-1 flex list-none flex-wrap justify-end gap-x-4 gap-y-1 p-0" aria-hidden>
            {legend.map((l) => (
              <li key={l.label} className="inline-flex items-center gap-1.5 font-mono text-[10px] uppercase tracking-[.12em] text-muted">
                <span className="h-2 w-2 rounded-[2px]" style={{ background: l.color }} />
                {l.label}
              </li>
            ))}
          </ul>
          <div className="min-h-0 flex-1">{option && <Chart ariaLabel={aria} height="100%" option={option} />}</div>
        </div>
      )}
    </Body>
  )
}
