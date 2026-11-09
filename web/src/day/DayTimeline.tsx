// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo } from 'react'
import { useDaySeries, useNights, useRecentActivities } from '../api/hooks/data'
import type { Series } from '../api/types'
import { Chart, dayTimeline, describeSeries } from '../charts'
import { ctx } from '../glance/cards/parts'
import { addDays } from '../lib/period'

/** clean drops the watch's "no reading" values (≤ 0 heart rate, negative stress). */
function clean(s: Series | undefined, floor: number) {
  return s ? { t: s.t, v: s.v.map((x) => (x < floor ? null : x)) } : undefined
}

export const timelineLegend = [
  { label: 'Heart rate', color: 'var(--grey)' },
  { label: 'Stress', color: 'var(--amber)' },
  { label: 'Body Battery', color: 'var(--lime)' },
  { label: 'Sleep', color: 'var(--cyan)' },
]

/** An activity marked on the timeline. */
export interface TimelineActivity {
  start: number
  end: number
  label: string
}

/**
 * useDayTimeline is one day's heart rate, stress and Body Battery on one time axis, with its
 * sleep and activities marked: the glance's Your day and the calendar's day panel. Without
 * activities given it marks the recent ones that fall on the day.
 */
export function useDayTimeline(day: string, tz: string, activities?: TimelineActivity[]) {
  const hr = useDaySeries('heart_rate', day)
  const stress = useDaySeries('stress', day)
  const bb = useDaySeries('body_battery', day)
  const nights = useNights(addDays(day, -6), day)
  const recent = useRecentActivities(50, activities === undefined)
  const span = hr.data ?? bb.data ?? stress.data
  const acts = useMemo(
    () => activities ?? (recent.data?.items ?? []).map((a) => ({ start: a.start, end: a.end, label: a.sport_label })),
    [activities, recent.data],
  )
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
      activities: acts.filter((a) => within(a.start, a.end)).map((a) => ({ from: a.start, to: a.end, label: a.label })),
    }, ctx(tz))
  }, [span, hr.data, stress.data, bb.data, nights.data, acts, tz])
  const empty = !hr.data?.t.length && !stress.data?.t.length && !bb.data?.t.length
  const aria = [describeSeries('Heart rate', hr.data?.v ?? [], 'bpm'), describeSeries('Stress', stress.data?.v ?? []),
    describeSeries('Body Battery', bb.data?.v ?? [], '%')].join('. ')
  return { queries: [hr, stress, bb] as const, option, empty, aria, bodyBattery: bb.data }
}

/** TimelineLegend names the timeline's colours. */
export function TimelineLegend() {
  return (
    <ul className="m-0 flex list-none flex-wrap justify-end gap-x-4 gap-y-1 p-0" aria-hidden>
      {timelineLegend.map((l) => (
        <li key={l.label} className="inline-flex items-center gap-1.5 font-mono text-[10px] uppercase tracking-[.12em] text-muted">
          <span className="h-2 w-2 rounded-[2px]" style={{ background: l.color }} />
          {l.label}
        </li>
      ))}
    </ul>
  )
}

/** TimelineChart draws the timeline at the height given. */
export function TimelineChart({ t, height }: { t: ReturnType<typeof useDayTimeline>; height: number | string }) {
  return t.option ? <Chart ariaLabel={t.aria} height={height} option={t.option} /> : null
}
