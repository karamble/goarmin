// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { CalendarPlus, Pencil, PersonStanding, Repeat, Undo2 } from 'lucide-react'
import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { useWorkout, useWorkoutEdits } from '../../api/hooks/training'
import type { Workout, WorkoutStep, WorkoutWatch } from '../../api/types'
import { usePlanActions } from '../../api/hooks/calendar'
import { HeaderActions, useSectionTitle } from '../../app/section/HeaderSlot'
import { useToday } from '../../app/section/usePeriod'
import { Sheet } from '../../ui/Sheet'
import { Switch } from '../../ui/kit'
import { useToast } from '../../ui/Toast'
import { hm, type Units } from '../../lib/format'
import { FigureDialog, type FigureItem } from '../../figures/FigureDialog'
import { blocks, durationText, intensityTone, repeatText, stepTitle, strip, targetText } from '../../lib/workout'
import { originText, sportWords } from '../../lib/workoutLibrary'
import { Card, Kpis, Loaded } from '../health/parts'
import { Presence } from './Workouts'

/** Workout shows a saved workout's steps, repeats as blocks, and its intensity over time. */
export default function WorkoutPage() {
  const id = Number(useParams().id)
  const q = useWorkout(id)
  const units = useMe().data?.units ?? 'metric'
  const [figure, setFigure] = useState<number | null>(null)
  const [planning, setPlanning] = useState(false)
  const [renaming, setRenaming] = useState(false)
  const { undo } = useWorkoutEdits()
  const toast = useToast()
  useSectionTitle(q.data?.name)
  if (!(id > 0)) return <NotFound />
  return (
    <Loaded q={[q]}>
      {() => {
        const w = q.data
        if (!w) return <NotFound />
        const steps = w.steps ?? []
        const segs = strip(steps)
        const total = segs.reduce((a, s) => a + (s.s || 60), 0)
        const exercises = new Set(steps.filter((s) => s.exercise).map((s) => `${s.exercise!.category}/${s.exercise!.number}`)).size
        const figures = figureItems(steps, units)
        const pose = (s: WorkoutStep) => {
          const i = s.exercise ? figures.findIndex((f) => f.category === s.exercise!.category && f.number === s.exercise!.number) : -1
          return i >= 0 ? () => setFigure(i) : undefined
        }
        return (
          <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
            <HeaderActions>
              <button type="button" onClick={() => setRenaming(true)}
                className="inline-flex items-center gap-1.5 rounded-full border border-border px-3 py-1.5 text-[13px] text-muted hover:border-lime hover:text-text">
                <Pencil size={14} aria-hidden /> Rename
              </button>
              <button type="button" onClick={() => setPlanning(true)}
                className="inline-flex items-center gap-1.5 rounded-full border border-border px-3 py-1.5 text-[13px] text-lime hover:border-lime">
                <CalendarPlus size={14} aria-hidden /> Plan
              </button>
            </HeaderActions>
            {planning && <PlanOn id={w.id} name={w.name} onClose={() => setPlanning(false)} />}
            {renaming && <Rename w={w} onClose={() => setRenaming(false)} />}
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Sport', value: sportWords(w.sport, w.sub_sport) },
                { label: 'Steps', value: `${w.step_count}` },
                { label: 'About', value: w.estimated_s ? hm(w.estimated_s) : '–', tone: 'lime', hint: 'timed steps with repeats' },
                ...(exercises ? [{ label: 'Exercises', value: `${exercises}` }] : []),
              ]} />
              <div className="mt-5 flex h-3 w-full gap-[2px] overflow-hidden rounded-full" role="img"
                aria-label={`Intensity over the workout: ${segs.map((s) => s.intensity).join(', ')}`}>
                {segs.map((s, i) => <span key={i} style={{ flexGrow: (s.s || 60) / total, background: intensityTone(s.intensity) }} />)}
              </div>
              <p className="m-0 mt-2 flex flex-wrap gap-x-4 text-[12px] text-muted" aria-hidden>
                {[['warmup', 'Warm-up and cool-down'], ['active', 'Work'], ['recovery', 'Recovery and rest']].map(([k, label]) => (
                  <span key={k} className="inline-flex items-center gap-1.5"><span className="h-2 w-2 rounded-[2px]" style={{ background: intensityTone(k) }} />{label}</span>
                ))}
              </p>
              <div className="mt-5 flex flex-wrap items-center gap-2" aria-label="Where it is">
                {(w.watches ?? []).map((x) => <Presence key={x.device_id} x={x} long />)}
                <span className="text-[12px] text-muted">{originText(w.origins ?? [w.origin])}</span>
              </div>
              {w.description && <p className="m-0 mt-3 max-w-[72ch] text-[14px] text-muted">{w.description}</p>}
            </section>
            {(w.watches ?? []).length > 0 && <OnWatches w={w} />}
            <Card title="Steps">
              <ol className="m-0 grid list-none gap-2 p-0">
                {blocks(steps).map((b, i) => b.kind === 'step' ? (
                  <StepRow key={i} s={b.step} units={units} onPose={pose(b.step)} />
                ) : (
                  <li key={i} className="rounded-xl border border-border p-2.5">
                    <p className="m-0 mb-2 flex items-center gap-2 px-1 font-mono text-[11px] uppercase tracking-[.14em] text-lime">
                      <Repeat size={13} aria-hidden /> {repeatText(b)}
                    </p>
                    <ol className="m-0 grid list-none gap-2 p-0">
                      {b.steps.map((s) => <StepRow key={s.index} s={s} units={units} onPose={pose(s)} />)}
                    </ol>
                  </li>
                ))}
              </ol>
              <p className="m-0 mt-4 text-[12px] text-faint">
                {w.origin === 'goarmin' ? 'Renamed in goarmin' : w.origin === 'watch' ? 'As saved on the watch' : 'As saved in Garmin Connect'},{' '}
                {new Date(w.recorded_at * 1000).toLocaleDateString()}.
                {w.origin === 'goarmin' && (
                  <button type="button" disabled={undo.isPending}
                    onClick={() => undo.mutate(w.id, {
                      onSuccess: () => toast({ text: 'Back to the original name' }),
                      onError: (e) => toast({ text: `Not undone: ${(e as Error).message}`, tone: 'error' }),
                    })}
                    className="ml-2 inline-flex items-center gap-1 text-muted underline-offset-2 hover:text-text hover:underline disabled:opacity-50">
                    <Undo2 size={12} aria-hidden /> Back to the original
                  </button>
                )}
              </p>
            </Card>
            {figure !== null && figures.length > 0 && <FigureDialog items={figures} start={figure} onClose={() => setFigure(null)} />}
          </div>
        )
      }}
    </Loaded>
  )
}

/** OnWatches switches the workout on or off for each watch: on, the watch gets it at its next
 * sync; off, it waits to be deleted on the watch (a watch takes no deletes). */
function OnWatches({ w }: { w: Workout }) {
  const { onWatch } = useWorkoutEdits()
  const toast = useToast()
  const set = (x: WorkoutWatch, on: boolean) => onWatch.mutate({ id: w.id, device: x.device_id, on }, {
    onError: (e) => toast({ text: `Not changed: ${(e as Error).message}`, tone: 'error' }),
  })
  return (
    <Card title="On your watches">
      <ul className="m-0 grid list-none gap-3 p-0">
        {(w.watches ?? []).map((x) => (
          <li key={x.device_id} className="flex items-start gap-3">
            <Switch on={x.want} label={`On ${x.name}`} disabled={onWatch.isPending} onChange={(on) => set(x, on)} />
            <div className="min-w-0">
              <p className="m-0 text-[14px] text-text">{x.name}</p>
              <p className="m-0 text-[13px] text-muted">{watchLine(x, w.name)}</p>
              {x.failed && <p className="m-0 text-[13px] text-[#e08585]">Not sent: {x.failed}</p>}
            </div>
          </li>
        ))}
      </ul>
    </Card>
  )
}

/** watchLine says where the workout is on one watch and what happens next. */
export function watchLine(x: WorkoutWatch, name: string): string {
  if (x.pending === 'send') return 'Goes on the watch at its next sync.'
  if (x.pending === 'remove') return `Switched off. goarmin can't delete from a watch: on the watch, open Workouts › ${name} › Delete.`
  if (x.state === 'on') return 'On the watch.'
  return x.state === 'unknown' ? 'Not checked yet: the watch has not synced since.' : 'Not on the watch. Switch it on to send it at the next sync.'
}

/** Rename gives a workout another name or description. goarmin keeps it as its own version;
 * the original from the watch or Garmin Connect stays, and can be shown again. */
function Rename({ w, onClose }: { w: Workout; onClose: () => void }) {
  const [name, setName] = useState(w.name)
  const [description, setDescription] = useState(w.description ?? '')
  const { rename } = useWorkoutEdits()
  const toast = useToast()
  const clean = name.trim().replace(/\s+/g, ' ')
  const changed = clean !== w.name || description.trim() !== (w.description ?? '')
  const submit = () => rename.mutate({ id: w.id, name: clean, description: description.trim() }, {
    onSuccess: () => {
      toast({ text: `Renamed to ${clean}` })
      onClose()
    },
    onError: (e) => toast({ text: `Not renamed: ${(e as Error).message}`, tone: 'error' }),
  })
  const field = 'w-full rounded-lg border border-border bg-bg2 px-3 py-2 text-[14px] text-text focus:border-lime focus:outline-none'
  return (
    <Sheet title={`Rename ${w.name}`} onClose={onClose}
      footer={<button type="button" onClick={submit} disabled={!clean || !changed || rename.isPending}
        className="rounded-full bg-lime px-4 py-2 text-[14px] font-medium text-bg disabled:opacity-50">Save</button>}>
      <div className="grid gap-4">
        <label className="grid gap-1 text-[13px] text-muted">
          Name
          <input value={name} maxLength={40} onChange={(e) => setName(e.target.value)} className={field} />
          <span className="text-[12px] text-faint">{clean.length} of 40 characters, as many as a watch shows</span>
        </label>
        <label className="grid gap-1 text-[13px] text-muted">
          Description
          <textarea value={description} maxLength={1000} rows={4} onChange={(e) => setDescription(e.target.value)} className={field} />
        </label>
        <p className="m-0 text-[13px] text-muted">
          The steps stay as they are. goarmin keeps the original too, and sends this version when it puts the workout on a watch.
        </p>
      </div>
    </Sheet>
  )
}

/** PlanOn puts this workout on a day (the calendar shows it there). */
function PlanOn({ id, name, onClose }: { id: number; name: string; onClose: () => void }) {
  const me = useMe().data
  const today = useToday(me?.timezone ?? '')
  const [day, setDay] = useState(today)
  const { add } = usePlanActions()
  const toast = useToast()
  const navigate = useNavigate()
  const submit = () => add.mutate({ day, workout_id: id }, {
    onSuccess: () => {
      toast({ text: `${name} planned`, action: { label: 'Open the calendar', run: () => navigate(`/calendar?d=${day}`) } })
      onClose()
    },
    onError: (e) => toast({ text: `Not planned: ${(e as Error).message}`, tone: 'error' }),
  })
  return (
    <Sheet title={`Plan ${name}`} onClose={onClose}
      footer={<button type="button" onClick={submit} disabled={!day || add.isPending}
        className="rounded-full bg-lime px-4 py-2 text-[14px] font-medium text-bg disabled:opacity-50">Plan it</button>}>
      <label className="grid gap-1 text-[13px] text-muted">
        On
        <input type="date" value={day} min={today} onChange={(e) => setDay(e.target.value)}
          className="w-full max-w-[220px] rounded-lg border border-border bg-bg2 px-3 py-2 text-[14px] text-text focus:border-lime focus:outline-none" />
      </label>
      <p className="m-0 mt-3 text-[13px] text-muted">The calendar shows it on that day and ticks it off when you record it. It isn't sent to the watch yet.</p>
    </Sheet>
  )
}

/** figureItems is the workout's exercises in order, each once, for the figure dialog. */
export function figureItems(steps: WorkoutStep[], units: Units): FigureItem[] {
  const out: FigureItem[] = []
  for (const s of steps) {
    const e = s.exercise
    if (!e || out.some((f) => f.category === e.category && f.number === e.number)) continue
    const detail = [durationText(s, units), s.weight_kg ? `${s.weight_kg} kg` : ''].filter(Boolean).join(' · ')
    out.push({ category: e.category, number: e.number, title: e.title || e.name || stepTitle(s), detail })
  }
  return out
}

function StepRow({ s, units, onPose }: { s: WorkoutStep; units: Units; onPose?: () => void }) {
  const target = targetText(s, units)
  return (
    <li className="flex items-start gap-3 rounded-lg bg-bg2 px-3 py-2.5">
      <span aria-hidden className="mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: intensityTone(s.intensity) }} />
      <div className="min-w-0 flex-1">
        <p className="m-0 flex flex-wrap items-baseline justify-between gap-x-3 text-[14px]">
          <span className="text-text">{stepTitle(s)}</span>
          <span className="num text-muted">{durationText(s, units)}</span>
        </p>
        {(target || s.weight_kg || s.notes) && (
          <p className="m-0 mt-0.5 text-[12px] text-muted">
            {[target, s.weight_kg ? `${s.weight_kg} kg` : '', s.notes ?? ''].filter(Boolean).join(' · ')}
          </p>
        )}
      </div>
      {onPose && (
        <button type="button" onClick={onPose} aria-label={`View pose: ${stepTitle(s)}`}
          className="inline-flex shrink-0 items-center gap-1 self-center rounded-full border border-border px-2.5 py-1 text-[12px] text-muted hover:border-lime hover:text-lime">
          <PersonStanding size={14} aria-hidden /> View pose
        </button>
      )}
    </li>
  )
}

function NotFound() {
  return (
    <section className="card grid place-items-center px-6 py-12 text-center">
      <p className="m-0 font-display text-[22px] uppercase">Workout not found</p>
      <Link to="/training/workouts" className="mt-4 text-[14px] text-lime hover:underline">All workouts</Link>
    </section>
  )
}
