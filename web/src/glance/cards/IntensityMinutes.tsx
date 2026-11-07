// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo } from 'react'
import { usePrefs } from '../../api/hooks/prefs'
import { useDailyWindow } from '../../api/hooks/data'
import { Chart, dailyBars, Ring } from '../../charts'
import { addDays, periodRange, weekday, weekdayNames } from '../../lib/period'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { ctx, Kpi, n } from './parts'


export default function IntensityMinutes({ day, size, isToday, tz }: CardProps) {
  const GOAL = usePrefs().intensityGoal // the watch's goal unless the user set one (Garmin's default: 150)
  const q = useDailyWindow(day, 14)
  const week = useMemo(() => {
    const byDay = new Map((q.data ?? []).map((x) => [x.day, x]))
    const from = periodRange(day, 'week').from
    return Array.from({ length: 7 }, (_, i) => {
      const x = byDay.get(addDays(from, i))
      return x && (x.moderate_min != null || x.vigorous_min != null) ? (x.moderate_min ?? 0) + 2 * (x.vigorous_min ?? 0) : null
    })
  }, [q.data, day])
  const total = week.reduce<number>((a, x) => a + (x ?? 0), 0)
  const known = week.some((x) => x != null)
  const option = useMemo(() => dailyBars({ labels: weekdayNames('narrow'), v: week, focus: weekday(day), axis: true, unit: 'min' }, ctx(tz)), [week, day, tz])
  return (
    <Body q={[q]} empty={!known} isToday={isToday} size={size}>
      {() => (
        <div className="flex h-full items-end justify-between gap-4">
          <div className="flex h-full flex-col justify-end gap-3">
            <Kpi value={n(total)} unit="min">of {GOAL} this week</Kpi>
          </div>
          {size === 'S' ? (
            <Ring value={total} max={GOAL} size={74} stroke={7} label={`${n(total)} of ${GOAL} intensity minutes this week`}>
              <span className="num text-[14px]">{Math.round((total / GOAL) * 100)}%</span>
            </Ring>
          ) : (
            <div className="h-full min-w-0 flex-1"><Chart ariaLabel="Intensity minutes per day this week" height="100%" option={option} /></div>
          )}
        </div>
      )}
    </Body>
  )
}
