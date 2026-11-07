// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useCoachWeek } from '../../api/hooks/data'
import type { CoachDay } from '../../api/types'
import { clock } from '../../lib/format'
import { addDays, weekday, weekdayNames } from '../../lib/period'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { sportIcon } from './sport'


export default function Coach({ day, size, isToday }: CardProps) {
  const q = useCoachWeek(day)
  const byDay = new Map<string, CoachDay[]>()
  for (const x of q.data ?? []) byDay.set(x.day, [...(byDay.get(x.day) ?? []), x])
  const todays = (byDay.get(day) ?? []).filter((x) => !x.rest_day)
  const week = Array.from({ length: 7 }, (_, i) => addDays(day, i))
  return (
    <Body q={[q]} empty={(q.data ?? []).length === 0} isToday={isToday} size={size} emptyText="No Garmin Coach plan on the watch">
      {() => (
        <div className="flex h-full flex-col gap-3">
          <div className="min-h-0 flex-1">
            {todays.length === 0 ? (
              <p className="m-0 font-display text-[22px] uppercase tracking-[.02em]">Rest day</p>
            ) : (
              <>
                <p className="m-0 font-display text-[22px] uppercase leading-none tracking-[.02em]">
                  {todays[0].name} <span className="text-muted">·</span> <span className="num text-[20px] text-lime">{clock(todays[0].duration_s)}</span>
                </p>
                <ul className="m-0 mt-3 grid list-none gap-1.5 p-0">
                  {todays.slice(0, size === 'M' ? 2 : 4).map((x) => {
                    const Icon = sportIcon(x.sport)
                    return (
                      <li key={x.index} className="flex items-center gap-2.5 text-[13px] text-muted">
                        <Icon size={15} strokeWidth={1.75} className="shrink-0 text-faint" aria-hidden />
                        <span className="capitalize text-text">{x.sport || 'workout'}</span>
                        <span className="num">{x.description}</span>
                        <span className="num ml-auto text-faint">{clock(x.duration_s)}</span>
                      </li>
                    )
                  })}
                </ul>
              </>
            )}
          </div>
          <ol className="m-0 grid list-none grid-cols-7 gap-1.5 p-0" aria-label="The week ahead">
            {week.map((d) => {
              const w = (byDay.get(d) ?? []).filter((x) => !x.rest_day)
              return (
                <li key={d} className={`rounded-lg border px-1 py-1.5 text-center ${d === day ? 'border-lime-dim bg-lime-fill' : 'border-border'}`}
                  title={w.length ? `${w[0].name}, ${clock(w[0].duration_s)}` : 'Rest'}>
                  <span className={`block font-mono text-[10px] uppercase ${d === day ? 'text-lime' : 'text-faint'}`}>{weekdayNames('two')[weekday(d)]}</span>
                  <span className="block truncate text-[11px] text-muted">{w.length ? w[0].name : byDay.has(d) ? 'Rest' : '·'}</span>
                </li>
              )
            })}
          </ol>
        </div>
      )}
    </Body>
  )
}
