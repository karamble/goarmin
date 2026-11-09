// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Link } from 'react-router-dom'
import { useCoachWeek } from '../../api/hooks/data'
import { useCalendar } from '../../api/hooks/calendar'
import { useWorkouts } from '../../api/hooks/training'
import { clock, hm } from '../../lib/format'
import { distinctWorkouts } from '../../lib/training'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { sportIcon } from './sport'

/** WorkoutToday is the day's own plans, then Garmin Coach's workout, or else the workouts on the
 * watch (the library's, while no watch has been checked). */
export default function WorkoutToday({ day, size, isToday }: CardProps) {
  const coach = useCoachWeek(day)
  const saved = useWorkouts()
  const cal = useCalendar(day, day)
  const mine = cal.data?.days.find((d) => d.day === day)?.plans ?? []
  const todays = distinctWorkouts((coach.data ?? []).filter((x) => x.day === day && !x.rest_day))
  const rest = (coach.data ?? []).some((x) => x.day === day && x.rest_day)
  // What is on a watch, if goarmin knows; else the whole library (no watch checked yet).
  const library = saved.data ?? []
  const onWatch = library.filter((w) => w.watches.some((x) => x.state === 'on'))
  const known = library.some((w) => w.watches.some((x) => x.state !== 'unknown'))
  const list = known ? onWatch : library
  return (
    <Body q={[coach, saved]} empty={!mine.length && !todays.length && !rest && !list.length} isToday={isToday} size={size}
      emptyText="No Garmin Coach plan and no saved workouts on the watch">
      {() => (
        <div className="flex h-full flex-col gap-2">
          {mine.length > 0 && (
            <>
              <p className="caps m-0 !text-[10px] !text-faint">Planned by you</p>
              <ul className="m-0 grid list-none gap-1.5 p-0">
                {mine.slice(0, size === 'M' ? 2 : 4).map((p) => {
                  const Icon = sportIcon(p.sport)
                  return (
                    <li key={p.id}>
                      <Link to={`/training/workouts/${p.workout_id}`} className="flex items-center gap-2.5 text-[14px] text-text hover:text-lime">
                        <Icon size={15} strokeWidth={1.75} className="shrink-0 text-faint" aria-hidden />
                        <span className="truncate">{p.name}</span>
                        <span className="num ml-auto shrink-0 text-[12px] text-muted">
                          {p.done_activity != null ? 'done' : p.estimated_s ? hm(p.estimated_s) : ''}
                        </span>
                      </Link>
                    </li>
                  )
                })}
              </ul>
            </>
          )}
          {mine.length > 0 && !todays.length ? null : todays.length > 0 ? (
            <>
              <p className="caps m-0 !text-[10px] !text-faint">Garmin Coach</p>
              {todays.slice(0, size === 'M' ? 2 : 4).map((w) => {
                const Icon = sportIcon(w.sport)
                return (
                  <p key={`${w.sport}|${w.name}`} className="m-0 flex items-center gap-2.5 text-[14px]">
                    <Icon size={16} strokeWidth={1.75} className="shrink-0 text-faint" aria-hidden />
                    <span className="text-text">{w.name}</span>
                    <span className="num text-lime">{clock(w.duration_s)}</span>
                    <span className="num truncate text-[12px] text-muted">{w.targets.join(' · ')}</span>
                  </p>
                )
              })}
            </>
          ) : rest ? (
            <p className="m-0 font-display text-[22px] uppercase">Rest day</p>
          ) : (
            <>
              <p className="caps m-0 !text-[10px] !text-faint">{known ? 'On your watch' : 'In your library'}</p>
              <ul className="m-0 grid list-none gap-1.5 p-0">
                {list.slice(0, size === 'M' ? 3 : 6).map((w) => {
                  const Icon = sportIcon(w.sport)
                  return (
                    <li key={w.id}>
                      <Link to={`/training/workouts/${w.id}`} className="flex items-center gap-2.5 text-[14px] text-text hover:text-lime">
                        <Icon size={15} strokeWidth={1.75} className="shrink-0 text-faint" aria-hidden />
                        <span className="truncate">{w.name}</span>
                        {w.estimated_s > 0 && <span className="num ml-auto shrink-0 text-[12px] text-muted">{hm(w.estimated_s)}</span>}
                      </Link>
                    </li>
                  )
                })}
              </ul>
            </>
          )}
        </div>
      )}
    </Body>
  )
}
