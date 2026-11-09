// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Check, ChevronLeft, ChevronRight, LayoutGrid, Plus, X } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { usePlanActions } from '../../api/hooks/calendar'
import { useDaySeries } from '../../api/hooks/data'
import type { CalendarDay, Plan } from '../../api/types'
import { hrvStatus } from '../../api/types'
import { TimelineChart, TimelineLegend, useDayTimeline } from '../../day/DayTimeline'
import { sportIcon } from '../../glance/cards/sport'
import { distance, hm, int, type Units } from '../../lib/format'
import { chargeDrain, hrvOf } from '../../lib/health'
import { stressLevel } from '../../lib/live'
import { addDays, relDay } from '../../lib/period'
import { distinctWorkouts } from '../../lib/training'
import { sportWords } from '../../lib/workoutLibrary'
import { useToast } from '../../ui/Toast'

const longDay = new Intl.DateTimeFormat(undefined, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' })
const dateOf = (d: string) => new Date(`${d}T00:00:00Z`)

/** dayLink is a page for one day: ?d= unless it is today. */
const dayLink = (path: string, day: string, today: string) => (day === today ? path : `${path}?d=${day}`)

/**
 * DayPanel is one calendar day: its timeline, what was planned (Garmin Coach's and your own,
 * done or missed), its activities, and a line per value recorded, each opening its page for the
 * day. Days ahead hold only plans.
 */
export function DayPanel({ day, cd, today, latest, tz, units, onDay, onPlan, onMove, inSheet }: {
  day: string; cd?: CalendarDay; today: string; latest: string; tz: string; units: Units
  onDay: (d: string) => void; onPlan: () => void; onMove: (id: number, day: string) => void
  /** In a sheet (a phone): the sheet's title names the day, so no frame and no big date here. */
  inSheet?: boolean
}) {
  const past = day <= today
  return (
    <section className={`grid min-w-0 gap-5 ${inSheet ? '' : 'card p-4 sm:p-5'}`} aria-label={`Day: ${longDay.format(dateOf(day))}`}>
      <header className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <p className="m-0 font-mono text-[11px] uppercase tracking-[.14em] text-muted">{relDay(day, today)}</p>
          {!inSheet && <h2 className="m-0 mt-0.5 font-display text-[22px] uppercase leading-tight tracking-[.02em]">{longDay.format(dateOf(day))}</h2>}
        </div>
        <button type="button" onClick={() => onDay(addDays(day, -1))} aria-label="Previous day"
          className="grid h-8 w-8 place-items-center rounded-full border border-border text-muted hover:text-text"><ChevronLeft size={16} aria-hidden /></button>
        <button type="button" onClick={() => onDay(addDays(day, 1))} disabled={addDays(day, 1) > latest} aria-label="Next day"
          className="grid h-8 w-8 place-items-center rounded-full border border-border text-muted hover:text-text disabled:opacity-40"><ChevronRight size={16} aria-hidden /></button>
      </header>
      {past && <Timeline day={day} tz={tz} cd={cd} />}
      <Planned day={day} cd={cd} today={today} latest={latest} onPlan={onPlan} onMove={onMove} />
      {past && <Activities cd={cd} units={units} />}
      {past && <Details day={day} cd={cd} today={today} />}
      {past && (
        <Link to={day === today ? '/' : `/?d=${day}`} className="inline-flex items-center gap-1.5 justify-self-start text-[13px] text-lime hover:underline">
          <LayoutGrid size={14} aria-hidden /> Open the day on the glance
        </Link>
      )}
    </section>
  )
}

function Heading({ children }: { children: ReactNode }) {
  return <h3 className="m-0 mb-2 font-mono text-[11px] uppercase tracking-[.14em] text-muted">{children}</h3>
}

function Timeline({ day, tz, cd }: { day: string; tz: string; cd?: CalendarDay }) {
  const acts = useMemo(() => (cd?.activities ?? []).map((a) => ({ start: a.start, end: a.end, label: a.sport_label })), [cd])
  const t = useDayTimeline(day, tz, acts)
  if (t.empty) return null
  return (
    <div>
      <TimelineLegend />
      <TimelineChart t={t} height={170} />
    </div>
  )
}

function Planned({ day, cd, today, latest, onPlan, onMove }: {
  day: string; cd?: CalendarDay; today: string; latest: string; onPlan: () => void; onMove: (id: number, day: string) => void
}) {
  const coach = distinctWorkouts((cd?.coach ?? []).filter((c) => !c.rest_day).map((c) => ({ ...c, day })))
  const doneSport = new Map((cd?.coach ?? []).map((c) => [c.sport, c]))
  const rest = (cd?.coach ?? []).some((c) => c.rest_day)
  const plans = cd?.plans ?? []
  return (
    <div>
      <Heading>Planned</Heading>
      <ul className="m-0 grid list-none gap-2 p-0">
        {coach.map((c) => {
          const s = doneSport.get(c.sport)
          return (
            <li key={`c${c.sport}`} className="rounded-lg border border-dashed border-lime-dim/60 px-3 py-2">
              <p className="m-0 flex items-baseline justify-between gap-2 text-[14px]">
                <span className="text-text">{c.name || 'Workout'} <span className="text-[12px] text-muted">· Garmin Coach</span></span>
                <State done={!!s?.done} missed={!!s?.missed} />
              </p>
              <p className="m-0 mt-0.5 text-[12px] text-muted">{[sportWords(c.sport), c.duration_s ? hm(c.duration_s) : '', ...c.targets].filter(Boolean).join(' · ')}</p>
            </li>
          )
        })}
        {rest && coach.length === 0 && <li className="text-[13px] text-muted">Garmin Coach: rest day</li>}
        {plans.map((p) => <PlanRow key={p.id} p={p} latest={latest} onMove={onMove} />)}
        {coach.length === 0 && plans.length === 0 && !rest && <li className="text-[13px] text-faint">Nothing planned</li>}
      </ul>
      {day >= addDays(today, -7) && (
        <button type="button" onClick={onPlan}
          className="mt-3 inline-flex items-center gap-1.5 rounded-full border border-border px-3 py-1.5 text-[13px] text-lime hover:border-lime">
          <Plus size={14} aria-hidden /> Plan a workout
        </button>
      )}
    </div>
  )
}

function State({ done, missed }: { done: boolean; missed: boolean }) {
  if (done) return <span className="inline-flex shrink-0 items-center gap-1 text-[12px] text-lime"><Check size={12} aria-hidden />Done</span>
  if (missed) return <span className="shrink-0 text-[12px] text-faint">Missed</span>
  return <span className="shrink-0 text-[12px] text-muted">Planned</span>
}

/** PlanRow is one of your plans: its workout, whether it is done, and Move to… and Remove. */
function PlanRow({ p, latest, onMove }: { p: Plan; latest: string; onMove: (id: number, day: string) => void }) {
  const { add, remove } = usePlanActions()
  const toast = useToast()
  const [moving, setMoving] = useState(false)
  const [to, setTo] = useState(p.day)
  const drop = () => remove.mutate(p.id, {
    onSuccess: () => toast({
      text: `${p.name} removed`,
      action: { label: 'Undo', run: () => add.mutate({ day: p.day, workout_id: p.workout_id, note: p.note || undefined }) },
    }),
    onError: (e) => toast({ text: `Not removed: ${(e as Error).message}`, tone: 'error' }),
  })
  return (
    <li className="rounded-lg border border-dashed border-lime-dim/60 px-3 py-2">
      <p className="m-0 flex items-baseline justify-between gap-2 text-[14px]">
        {p.missing
          ? <span className="text-muted">{p.name} <span className="text-[12px]">(no longer stored)</span></span>
          : <Link to={`/training/workouts/${p.workout_id}`} className="truncate text-text hover:text-lime">{p.name}</Link>}
        <State done={p.done_activity != null} missed={p.missed} />
      </p>
      <p className="m-0 mt-0.5 text-[12px] text-muted">
        {[sportWords(p.sport, p.sub_sport), p.estimated_s ? `about ${hm(p.estimated_s)}` : '', p.note].filter(Boolean).join(' · ')}
        {p.done_activity != null && <> · <Link to={`/training/activities/${p.done_activity}`} className="text-lime hover:underline">the activity</Link></>}
      </p>
      {moving ? (
        <form className="mt-2 flex flex-wrap items-center gap-2" onSubmit={(e) => { e.preventDefault(); if (to && to !== p.day) onMove(p.id, to); setMoving(false) }}>
          <label className="text-[12px] text-muted">
            <span className="sr-only">Move {p.name} to</span>
            <input type="date" value={to} max={latest} onChange={(e) => setTo(e.target.value)} autoFocus
              className="rounded-lg border border-border bg-bg2 px-2 py-1 text-[13px] text-text focus:border-lime focus:outline-none" />
          </label>
          <button type="submit" className="rounded-full border border-lime-dim px-3 py-1 text-[12px] text-lime">Move</button>
          <button type="button" onClick={() => setMoving(false)} aria-label="Cancel moving" className="grid h-7 w-7 place-items-center text-muted"><X size={14} aria-hidden /></button>
        </form>
      ) : (
        <p className="m-0 mt-1.5 flex gap-3 text-[12px]">
          <button type="button" onClick={() => setMoving(true)} className="text-muted hover:text-text">Move to…</button>
          <button type="button" onClick={drop} className="text-muted hover:text-crit">Remove</button>
        </p>
      )}
    </li>
  )
}

function Activities({ cd, units }: { cd?: CalendarDay; units: Units }) {
  const list = cd?.activities ?? []
  if (!list.length) return null
  return (
    <div>
      <Heading>Activities</Heading>
      <ul className="m-0 grid list-none gap-1.5 p-0">
        {list.map((a) => {
          const Icon = sportIcon(a.sport)
          return (
            <li key={a.id}>
              <Link to={`/training/activities/${a.id}`} className="flex items-center gap-2.5 rounded-lg bg-bg2 px-3 py-2 text-[14px] text-text hover:text-lime">
                <Icon size={15} strokeWidth={1.75} className="shrink-0 text-faint" aria-hidden />
                <span className="min-w-0 flex-1 truncate">{a.sport_label || a.name}{a.workout ? <span className="text-[12px] text-muted"> · {a.workout}</span> : null}</span>
                <span className="num shrink-0 text-[12px] text-muted">{[a.distance_m ? distance(a.distance_m, units) : '', a.timer_s ? hm(a.timer_s) : ''].filter(Boolean).join(' · ')}</span>
              </Link>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

/** Details is the day in lines, as Connect's Daily Details: only what was recorded, each
 * opening its page for the day. */
function Details({ day, cd, today }: { day: string; cd?: CalendarDay; today: string }) {
  const bb = useDaySeries('body_battery', day)
  const s = cd?.summary
  const v = s?.values ?? {}
  const k = cd?.kinds ?? {}
  const lines: { label: string; value: string; to: string }[] = []
  const push = (label: string, value: string | null | false | undefined | 0, path: string) => {
    if (value) lines.push({ label, value, to: dayLink(path, day, today) })
  }
  const steps = s?.steps ?? 0
  const goal = v.step_goal ?? 0
  push('Steps', steps > 0 && `${int(steps)}${goal ? ` · ${Math.round((steps / goal) * 100)} % of goal` : ''}`, '/health/movement')
  push('Sleep', (s?.sleep_score != null || s?.sleep_s) && [s?.sleep_score != null ? `Score ${s.sleep_score}` : '', s?.sleep_s ? hm(s.sleep_s) : ''].filter(Boolean).join(' · '), '/health/sleep')
  push('Sleep need', v.sleep_need != null && hm(v.sleep_need * 60), '/health/sleep')
  const hrv = hrvOf(s)
  push('HRV', hrv.value != null && `${Math.round(hrv.value)} ms${hrv.status ? ` · ${hrvStatus[hrv.status] ?? ''}` : ''}`, '/health/hrv')
  push('Heart rate', (s?.resting_hr != null || s?.hr_max != null) && [s?.resting_hr != null ? `Resting ${s.resting_hr} bpm` : '', s?.hr_max != null ? `max ${s.hr_max} bpm` : ''].filter(Boolean).join(' · '), '/health/heart')
  push('Stress', s?.stress_avg != null && `Average ${s.stress_avg} · ${stressLevel(s.stress_avg).label}`, '/health/energy')
  const cdr = bb.data?.v.length ? chargeDrain(bb.data.v) : null
  push('Body Battery', cdr && (cdr.charged || cdr.drained) ? `Charged +${cdr.charged} · drained −${cdr.drained}`
    : v.bb_charged != null ? `Charged +${v.bb_charged} · drained −${v.bb_drained ?? 0}`
    : s?.body_battery_max != null ? `${s.body_battery_min ?? '–'} to ${s.body_battery_max}` : false, '/health/energy')
  push('Respiration', s?.respiration_avg != null && `${s.respiration_avg.toFixed(1)} brpm${s.spo2_avg != null ? ` · SpO2 ${s.spo2_avg} %` : ''}`, '/health/breathing')
  push('Intensity minutes', ((s?.moderate_min ?? 0) + (s?.vigorous_min ?? 0)) > 0 && `${s?.moderate_min ?? 0} moderate · ${s?.vigorous_min ?? 0} vigorous`, '/health/movement')
  push('Calories', (s?.active_kcal ?? 0) > 0 && `${int(s!.active_kcal)} active${v.total_kcal ? ` · ${int(v.total_kcal)} in all` : ''}`, '/health/movement')
  push('Weight', v.weight != null && `${v.weight.toFixed(1)} kg${v.body_fat != null ? ` · ${v.body_fat.toFixed(1)} % fat` : ''}`, '/health/body')
  push('Drinks', v.hydration != null && `${(v.hydration / 1000).toFixed(1)} l`, '/health/body')
  push('ECG', !!k.ecg && `${k.ecg} recording${k.ecg > 1 ? 's' : ''}`, '/health/heart')
  push('Naps', !!k.nap && `${k.nap} nap${k.nap > 1 ? 's' : ''}`, '/health/sleep')
  if (!lines.length) return <p className="m-0 text-[13px] text-faint">Nothing recorded this day.</p>
  return (
    <div>
      <Heading>The day</Heading>
      <ul className="m-0 grid list-none gap-0.5 p-0">
        {lines.map((l) => (
          <li key={l.label}>
            <Link to={l.to} className="flex items-baseline justify-between gap-3 rounded-lg px-2 py-1.5 text-[14px] hover:bg-bg2">
              <span className="text-text">{l.label}</span>
              <span className="num text-right text-[13px] text-muted">{l.value}</span>
            </Link>
          </li>
        ))}
      </ul>
    </div>
  )
}
