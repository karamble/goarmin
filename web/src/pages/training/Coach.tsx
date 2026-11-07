// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Check, Moon } from 'lucide-react'
import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { useCoachWeek } from '../../api/hooks/data'
import { useActivitiesIn } from '../../api/hooks/training'
import { usePeriod } from '../../app/section/usePeriod'
import { sportIcon } from '../../glance/cards/sport'
import { distance, hm, type Units } from '../../lib/format'
import { days as daysOf } from '../../lib/period'
import { distinctWorkouts, planWeek, type PlanDay } from '../../lib/training'
import { Empty, Kpis, Loaded } from '../health/parts'

const dayName = (d: string) => new Intl.DateTimeFormat(undefined, { weekday: 'long', timeZone: 'UTC' }).format(new Date(d + 'T12:00:00Z'))
const dayDate = (d: string) => new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', timeZone: 'UTC' }).format(new Date(d + 'T12:00:00Z'))

export default function Coach() {
  const p = usePeriod()
  const plan = useCoachWeek(p.range.from)
  const acts = useActivitiesIn(p.range.from, p.range.to)
  const units = useMe().data?.units ?? 'metric'
  const week = useMemo(() => planWeek(daysOf(p.range), plan.data ?? [], acts.data ?? []), [p.range, plan.data, acts.data])
  return (
    <Loaded q={[plan, acts]}>
      {() => {
        if ((plan.data ?? []).length === 0) {
          return (
            <Empty p={p} what="Garmin Coach plan">
              Garmin Coach's workouts come from the watch's schedule. Start a Garmin Coach plan (or a daily suggested workout) on
              the watch, and the next sync brings it here.
            </Empty>
          )
        }
        const planned = week.filter((d) => d.workouts.length > 0)
        const done = planned.filter((d) => d.done.length > 0)
        const plannedS = planned.reduce((a, d) => a + Math.max(0, ...distinctWorkouts(d.workouts).map((w) => w.duration_s ?? 0)), 0)
        const doneS = week.reduce((a, d) => a + d.done.reduce((b, x) => b + (x.timer_s ?? x.elapsed_s ?? 0), 0), 0)
        const next = week.find((d) => d.day >= p.today && d.workouts.length > 0 && d.done.length === 0)
        return (
          <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Done', value: `${done.length}/${planned.length}`, tone: 'lime', hint: 'planned days with an activity' },
                { label: 'Planned time', value: hm(plannedS) },
                { label: 'Trained', value: hm(doneS), hint: 'in planned sports' },
                { label: 'Rest days', value: `${week.filter((d) => d.rest).length}` },
                { label: 'Next', value: next ? (next.day === p.today ? 'Today' : dayName(next.day).slice(0, 3)) : '–', hint: next ? distinctWorkouts(next.workouts)[0]?.name : undefined },
              ]} />
            </section>
            <ol className="m-0 grid list-none gap-3 p-0 sm:grid-cols-2 lg:grid-cols-4 2xl:grid-cols-7" aria-label="The week's plan">
              {week.map((d) => <DayCard key={d.day} d={d} today={p.today} units={units} />)}
            </ol>
            <p className="m-0 text-[12px] text-faint">
              A planned day counts as done when an activity of a suggested sport was recorded that day. The watch suggests a run and a
              ride as alternatives, each with its targets (pace or power, and heart rate).
            </p>
          </div>
        )
      }}
    </Loaded>
  )
}

function DayCard({ d, today, units }: { d: PlanDay; today: string; units: Units }) {
  const ws = distinctWorkouts(d.workouts)
  const isToday = d.day === today
  const past = d.day < today
  const state = d.done.length ? 'done' : ws.length && past ? 'missed' : ws.length ? (isToday ? 'today' : 'planned') : d.rest ? 'rest' : 'none'
  const badge = {
    done: ['Done', ''],
    missed: ['Missed', '!bg-amber'],
    today: ['Today', ''],
    planned: ['Planned', '!bg-track !text-muted'],
    rest: ['Rest day', '!bg-track !text-muted'],
    none: ['No plan', '!bg-track !text-faint'],
  }[state]
  return (
    <li className={`card flex min-h-[180px] flex-col gap-3 p-4 ${isToday ? 'outline outline-1 outline-lime-dim' : ''}`} aria-current={isToday ? 'date' : undefined}>
      <header className="flex items-start justify-between gap-2">
        <div>
          <p className={`m-0 font-display text-[18px] uppercase leading-none tracking-[.02em] ${isToday ? 'text-lime' : ''}`}>{dayName(d.day)}</p>
          <p className="m-0 mt-1 font-mono text-[11px] text-faint">{dayDate(d.day)}</p>
        </div>
        <span className={`badge-cut shrink-0 ${badge[1]}`}>
          {state === 'done' && <Check size={11} className="-mt-px mr-1 inline" aria-hidden />}{badge[0]}
        </span>
      </header>
      {ws.length > 0 ? (
        <ul className="m-0 grid list-none gap-2.5 p-0">
          {ws.map((w, i) => {
            const Icon = sportIcon(w.sport)
            return (
              <li key={`${w.sport}|${w.name}`} className="flex gap-2.5">
                {i > 0 && w.sport !== ws[i - 1].sport && <span className="sr-only">or</span>}
                <Icon size={16} strokeWidth={1.75} className="mt-0.5 shrink-0 text-faint" aria-hidden />
                <div className="min-w-0">
                  {i > 0 && w.sport !== ws[i - 1].sport && <p aria-hidden className="m-0 mb-1 font-mono text-[10px] uppercase tracking-[.14em] text-faint">or</p>}
                  <p className="m-0 text-[14px] text-text">
                    <span className="capitalize">{w.sport || 'Workout'}</span> · {w.name || 'Workout'} <span className="num text-muted">{hm(w.duration_s)}</span>
                  </p>
                  {w.targets.length > 0 && <p className="num m-0 mt-0.5 truncate text-[12px] text-muted">{w.targets.join(' · ')}</p>}
                  {w.aerobic_te != null && <p className="m-0 mt-0.5 text-[12px] text-faint">Aerobic effect {w.aerobic_te.toFixed(1)}</p>}
                </div>
              </li>
            )
          })}
        </ul>
      ) : d.rest ? (
        <p className="m-0 flex items-center gap-2 text-[13px] text-muted"><Moon size={15} aria-hidden /> Recover today.</p>
      ) : null}
      {(d.done.length > 0 || d.other.length > 0) && (
        <ul className="m-0 mt-auto grid list-none gap-1.5 border-t border-border p-0 pt-3">
          {[...d.done, ...d.other].map((a) => {
            const Icon = sportIcon(a.sport)
            return (
              <li key={a.id}>
                <Link to={`/training/activities/${a.id}`} className="flex items-center gap-2 text-[13px] text-muted hover:text-lime">
                  <Icon size={14} strokeWidth={1.75} className={d.done.includes(a) ? 'text-lime' : 'text-faint'} aria-hidden />
                  <span className="min-w-0 truncate">{a.name || a.sport_label}</span>
                  <span className="num ml-auto shrink-0">{a.distance_m ? distance(a.distance_m, units) : hm(a.timer_s ?? a.elapsed_s)}</span>
                </Link>
              </li>
            )
          })}
        </ul>
      )}
    </li>
  )
}
