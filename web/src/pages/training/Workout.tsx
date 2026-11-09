// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { PersonStanding, Repeat } from 'lucide-react'
import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { useWorkout } from '../../api/hooks/training'
import type { WorkoutStep } from '../../api/types'
import { useSectionTitle } from '../../app/section/HeaderSlot'
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
                {w.origin === 'watch' ? 'As saved on the watch' : 'As saved in Garmin Connect'}, {new Date(w.recorded_at * 1000).toLocaleDateString()}.
              </p>
            </Card>
            {figure !== null && figures.length > 0 && <FigureDialog items={figures} start={figure} onClose={() => setFigure(null)} />}
          </div>
        )
      }}
    </Loaded>
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
