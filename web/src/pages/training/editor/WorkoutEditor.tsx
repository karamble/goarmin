// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Dumbbell, Footprints, Moon, Redo2, Repeat, Sun, Timer, Undo2 } from 'lucide-react'
import { useEffect, useMemo, useReducer, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { ApiError } from '../../../api/client'
import { useMe } from '../../../api/hooks/auth'
import { useWorkout, useWorkoutBuilder, type WorkoutBody } from '../../../api/hooks/training'
import type { Workout, WorkoutStep } from '../../../api/types'
import { HeaderActions, useSectionTitle } from '../../../app/section/HeaderSlot'
import type { ExerciseInfo } from '../../../figures/api'
import { hm } from '../../../lib/format'
import { intensityTone, strip } from '../../../lib/workout'
import {
  estimate, find, fromSteps, initState, MAX_STEPS, newId, problems, reduce, stepCount, stepOf, toSteps, type Action, type Item, type Meta,
} from '../../../lib/workoutEdit'
import { sportWords } from '../../../lib/workoutLibrary'
import { Notice, Skeleton } from '../../../ui/kit'
import { Sheet } from '../../../ui/Sheet'
import { useToast } from '../../../ui/Toast'
import { ExercisePicker } from './ExercisePicker'
import { fieldClass, Labelled } from './fields'
import { Inspector } from './Inspector'
import { LeaveGuard } from './LeaveGuard'
import { itemTitle, StepList } from './StepList'

/** The sports a workout can be for, as a watch writes them. */
const sports: [string, string, string][] = [
  ['training', 'strength_training', 'Strength'], ['training', 'cardio_training', 'Cardio'], ['hiit', 'generic', 'HIIT'],
  ['hiit', 'amrap', 'HIIT · AMRAP'], ['hiit', 'emom', 'HIIT · EMOM'], ['hiit', 'tabata', 'HIIT · Tabata'], ['training', 'yoga', 'Yoga'],
  ['fitness_equipment', 'pilates', 'Pilates'], ['mobility', 'generic', 'Mobility'], ['training', 'breathing', 'Breathing'],
  ['running', 'generic', 'Running'], ['running', 'treadmill', 'Treadmill run'], ['running', 'trail', 'Trail run'],
  ['cycling', 'generic', 'Cycling'], ['cycling', 'indoor_cycling', 'Indoor cycling'], ['walking', 'generic', 'Walking'],
  ['training', 'generic', 'Other training'],
]

const draftKey = (id: number) => `goarmin:workout-draft:${id || 'new'}`

interface Draft { tag?: string; meta: Meta; steps: WorkoutStep[] }

function readDraft(id: number): Draft | null {
  try {
    return JSON.parse(localStorage.getItem(draftKey(id)) ?? 'null') as Draft | null
  } catch {
    return null
  }
}
function writeDraft(id: number, d: Draft | null) {
  try {
    if (d) localStorage.setItem(draftKey(id), JSON.stringify(d))
    else localStorage.removeItem(draftKey(id))
  } catch {
    /* private window or full: the draft is a convenience */
  }
}

const metaOf = (w: Workout | null): Meta => ({
  name: w?.name ?? '', sport: w?.sport ?? 'training', sub_sport: w?.sub_sport ?? 'strength_training', description: (w?.description ?? '').trim(),
})
const snapshot = (meta: Meta, items: Item[]) => JSON.stringify({ meta: { ...meta, name: meta.name.trim(), description: meta.description.trim() }, steps: toSteps(items) })

/** WorkoutEditor builds a workout (`/training/workouts/new`) or edits one in place
 * (`/:id/edit`): the steps as a list to arrange, the selected one's settings beside it. */
export default function WorkoutEditor() {
  const id = Number(useParams().id ?? 0)
  const q = useWorkout(id)
  if (id > 0 && q.isPending) return <Skeleton className="h-96" />
  if (id > 0 && !q.data) {
    return (
      <section className="card p-8 text-center">
        <p className="m-0 text-muted">This workout is not in your library.</p>
        <Link to="/training/workouts" className="mt-3 inline-block text-lime">Back to the workouts</Link>
      </section>
    )
  }
  return <Editor key={id} base={q.data ?? null} />
}

function useWide() {
  const query = '(min-width: 1024px)'
  const [wide, setWide] = useState(() => typeof window !== 'undefined' && !!window.matchMedia?.(query).matches)
  useEffect(() => {
    const m = window.matchMedia?.(query)
    if (!m) return
    const on = () => setWide(m.matches)
    m.addEventListener?.('change', on)
    return () => m.removeEventListener?.('change', on)
  }, [])
  return wide
}

function Editor({ base }: { base: Workout | null }) {
  const id = base?.id ?? 0
  const units = useMe().data?.units ?? 'metric'
  const navigate = useNavigate()
  const toast = useToast()
  const wide = useWide()
  const { create, save } = useWorkoutBuilder()
  const [st, dispatch] = useReducer(reduce, null, () => initState(metaOf(base), fromSteps(base?.steps ?? [])))
  const [baseline, setBaseline] = useState(() => snapshot(st.meta, st.items))
  const [draft, setDraft] = useState(() => {
    const d = readDraft(id)
    return d && (!base || d.tag === base.tag) && JSON.stringify({ meta: d.meta, steps: d.steps }) !== baseline ? d : null
  })
  const [picker, setPicker] = useState<null | 'add' | 'replace'>(null)
  const [panel, setPanel] = useState(false)
  const [done, setDone] = useState<number | null>(null)
  useSectionTitle(base ? `Edit ${base.name}` : 'New workout')

  const now = snapshot(st.meta, st.items)
  const dirty = now !== baseline
  const issues = problems(st)
  const marked = useMemo(() => new Set(issues.map((p) => p.id).filter(Boolean) as string[]), [issues])
  const sel = st.selected[0] ? find(st.items, st.selected[0])?.item : undefined
  const saving = create.isPending || save.isPending

  // The draft follows the changes, so a closed tab loses nothing.
  useEffect(() => {
    const t = setTimeout(() => writeDraft(id, dirty ? { tag: base?.tag, meta: st.meta, steps: toSteps(st.items) } : null), 400)
    return () => clearTimeout(t)
  }, [now, dirty, id, base?.tag, st.meta, st.items])
  // Saved: leave for the workout once the guard knows there is nothing to lose.
  useEffect(() => {
    if (done) navigate(`/training/workouts/${done}`)
  }, [done, navigate])

  const submit = () => {
    if (issues.length || saving) return
    const body: WorkoutBody = { ...st.meta, steps: toSteps(st.items) }
    const ok = (w: Workout) => {
      writeDraft(id, null)
      setBaseline(now)
      toast({ text: `${w.name} saved${w.watches?.some((x) => x.pending === 'send') ? '; it goes to the watch at the next sync' : ''}` })
      setDone(w.id)
    }
    const fail = (e: unknown) =>
      toast({ text: e instanceof ApiError && e.status === 412 ? 'The workout changed elsewhere meanwhile: reload to see it' : `Not saved: ${(e as Error).message}`, tone: 'error' })
    if (base) save.mutate({ id: base.id, tag: base.tag, body }, { onSuccess: ok, onError: fail })
    else create.mutate(body, { onSuccess: ok, onError: fail })
  }

  // Ctrl+Z, Ctrl+Shift+Z (or Ctrl+Y) undo and redo outside text fields; Ctrl+S saves.
  const keys = useRef({ submit, dispatch })
  keys.current = { submit, dispatch }
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey)) return
      const k = e.key.toLowerCase()
      if (k === 's') {
        e.preventDefault()
        keys.current.submit()
        return
      }
      const t = e.target as HTMLElement
      if (t.closest('input, textarea, select, [contenteditable]')) return
      if (k === 'z') {
        e.preventDefault()
        keys.current.dispatch({ type: e.shiftKey ? 'redo' : 'undo' })
      } else if (k === 'y') {
        e.preventDefault()
        keys.current.dispatch({ type: 'redo' })
      }
    }
    document.addEventListener('keydown', on)
    return () => document.removeEventListener('keydown', on)
  }, [])

  const add = (step: WorkoutStep) => dispatch({ type: 'insert', item: { kind: 'step', id: newId(), step }, after: st.selected[0] })
  const addRepeat = () => {
    const tops = st.selected.filter((x) => st.items.some((it) => it.id === x && it.kind === 'step'))
    if (tops.length) dispatch({ type: 'wrap', ids: tops })
    else dispatch({ type: 'insert', item: { kind: 'repeat', id: newId(), items: [], rounds: 3 }, after: st.selected[0] })
  }
  const pick = (e: ExerciseInfo, keep: boolean) => {
    const ex = { category: e.category, number: e.number, name: e.title, title: e.title }
    if (picker === 'replace' && sel && sel.kind !== 'repeat') dispatch({ type: 'step', id: sel.id, patch: { exercise: ex } })
    else add(stepOf.exercise(e))
    if (!keep) setPicker(null)
  }
  const open = (itemId: string) => {
    dispatch({ type: 'select', ids: [itemId] })
    if (!wide) setPanel(true)
  }
  const used = useMemo(() => {
    const out: string[] = []
    for (const s of toSteps(st.items)) if (s.exercise && !out.includes(`${s.exercise.category}/${s.exercise.number}`)) out.push(`${s.exercise.category}/${s.exercise.number}`)
    return out
  }, [st.items])

  const steps = toSteps(st.items)
  const segs = strip(steps)
  const total = segs.reduce((a, s) => a + (s.s || 60), 0)
  const count = stepCount(st.items)
  const sport = sports.find(([sp, sub]) => sp === st.meta.sport && sub === (st.meta.sub_sport || 'generic'))
  const sportOptions = sport ? sports : [[st.meta.sport, st.meta.sub_sport, sportWords(st.meta.sport, st.meta.sub_sport)] as [string, string, string], ...sports]
  const inspector = sel && (
    <Inspector item={sel} units={units} dispatch={dispatch} onPick={() => setPicker('replace')} figure={!picker} />
  )
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5 lg:grid-cols-[minmax(0,1fr)_400px] lg:items-start">
      <HeaderActions>
        <button type="button" onClick={() => dispatch({ type: 'undo' })} disabled={!st.past.length} aria-label="Undo" title="Undo (Ctrl+Z)"
          className="grid h-9 w-9 place-items-center rounded-full text-muted hover:text-text disabled:opacity-40"><Undo2 size={16} /></button>
        <button type="button" onClick={() => dispatch({ type: 'redo' })} disabled={!st.future.length} aria-label="Redo" title="Redo (Ctrl+Shift+Z)"
          className="grid h-9 w-9 place-items-center rounded-full text-muted hover:text-text disabled:opacity-40"><Redo2 size={16} /></button>
        <button type="button" onClick={() => navigate(base ? `/training/workouts/${base.id}` : '/training/workouts')}
          className="rounded-full px-3 py-1.5 text-[13px] text-muted hover:text-text">Cancel</button>
        <button type="button" onClick={submit} disabled={saving || issues.length > 0 || (!!base && !dirty)}
          className="rounded-full border-2 border-lime bg-lime px-4 py-1.5 text-[13px] font-bold text-bg disabled:opacity-50">
          {saving ? 'Saving…' : 'Save'}
        </button>
      </HeaderActions>
      <LeaveGuard when={dirty && !done} text="Your changes to this workout are not saved. The browser keeps a draft you can pick up when you come back." />

      <div className="grid min-w-0 gap-5">
        {draft && (
          <Notice>
            <span className="flex flex-wrap items-center gap-3">
              Changes from earlier were not saved.
              <button type="button" className="text-lime underline-offset-2 hover:underline"
                onClick={() => { dispatch({ type: 'replace', meta: draft.meta, items: fromSteps(draft.steps) }); setDraft(null) }}>Restore them</button>
              <button type="button" className="text-muted hover:text-text" onClick={() => { writeDraft(id, null); setDraft(null) }}>Discard</button>
            </span>
          </Notice>
        )}
        <section className="card grid gap-4 p-5 sm:p-6">
          <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_240px]">
            <Labelled label="Name" hint={`${[...st.meta.name.trim()].length} of 40 characters, as many as a watch shows`}>
              {(fid) => <input id={fid} value={st.meta.name} maxLength={40} autoFocus={!base} placeholder="Name the workout"
                onChange={(e) => dispatch({ type: 'meta', patch: { name: e.target.value } })} className={fieldClass} />}
            </Labelled>
            <Labelled label="Sport">
              {(fid) => (
                <select id={fid} value={`${st.meta.sport}/${st.meta.sub_sport || 'generic'}`} className={fieldClass}
                  onChange={(e) => {
                    const [sp, sub] = e.target.value.split('/')
                    dispatch({ type: 'meta', patch: { sport: sp, sub_sport: sub } })
                  }}>
                  {sportOptions.map(([sp, sub, w]) => <option key={`${sp}/${sub}`} value={`${sp}/${sub || 'generic'}`}>{w}</option>)}
                </select>
              )}
            </Labelled>
          </div>
          <Labelled label="Description" hint="The watch shows the first 160 characters; goarmin keeps the whole text in the file.">
            {(fid) => <textarea id={fid} rows={2} maxLength={1000} value={st.meta.description}
              onChange={(e) => dispatch({ type: 'meta', patch: { description: e.target.value } })} className={fieldClass} />}
          </Labelled>
        </section>

        <section className="card grid gap-3 p-5 sm:p-6" aria-label="Summary">
          <p className="m-0 flex flex-wrap gap-x-5 gap-y-1 text-[14px] text-muted">
            <span><span className="text-lime">{hm(estimate(st.items)) || '–'}</span> about</span>
            <span>{used.length} {used.length === 1 ? 'exercise' : 'exercises'}</span>
            <span className={count > MAX_STEPS ? 'text-err' : ''}>FIT steps {count} of {MAX_STEPS}</span>
          </p>
          {segs.length > 0 && (
            <div className="flex h-3 w-full gap-[2px] overflow-hidden rounded-full" role="img" aria-label={`Intensity over the workout: ${segs.map((s) => s.intensity).join(', ')}`}>
              {segs.map((s, i) => <span key={i} style={{ flexGrow: (s.s || 60) / total, background: intensityTone(s.intensity) }} />)}
            </div>
          )}
          {issues.length > 0 && (
            <ul className="m-0 grid list-none gap-1 p-0 text-[13px] text-err" aria-label="What to fix before saving">
              {issues.slice(0, 6).map((p, i) => (
                <li key={i}>
                  {p.id ? <button type="button" className="text-left underline-offset-2 hover:underline" onClick={() => open(p.id!)}>{p.text}</button> : p.text}
                </li>
              ))}
              {issues.length > 6 && <li>… and {issues.length - 6} more</li>}
            </ul>
          )}
        </section>

        <section className="card grid gap-3 p-5 sm:p-6" aria-label="Steps of the workout">
          {st.items.length === 0
            ? <p className="m-0 text-[14px] text-muted">No steps yet. Add an exercise, a rest or an interval below; drag them into order.</p>
            : <StepList items={st.items} selected={st.selected} units={units} problems={marked} dispatch={dispatch} onOpen={open}
              onHint={(t) => toast({ text: t })} />}
          <AddBar onExercise={() => setPicker('add')} onRepeat={addRepeat} onStep={add} wrapping={st.selected.filter((x) => st.items.some((it) => it.id === x && it.kind === 'step')).length} />
          <p className="m-0 text-[12px] text-faint">
            Drag a step by its grip (on a phone, hold it first), or focus a step and use Alt+↑ and Alt+↓. Shift-click selects several steps to wrap in a repeat.
          </p>
        </section>
      </div>

      <aside className="card sticky top-28 hidden p-5 lg:block" aria-label="The selected step">
        {(wide && inspector) || <p className="m-0 text-[14px] text-muted">Select a step to set it up: its exercise and figure, how long, how hard, sets and rests.</p>}
      </aside>
      {!wide && panel && sel && (
        <Sheet title={itemTitle(sel)} onClose={() => setPanel(false)}>{inspector}</Sheet>
      )}
      {picker && <ExercisePicker replacing={picker === 'replace'} used={used} onPick={pick} onClose={() => setPicker(null)} />}
    </div>
  )
}

/** AddBar adds after the selected step: an exercise (the picker), a rest, a warm-up, a
 * cool-down, an interval, or a repeat (around the selected steps, or empty). */
function AddBar({ onExercise, onRepeat, onStep, wrapping }: { onExercise: () => void; onRepeat: () => void; onStep: (s: WorkoutStep) => void; wrapping: number }) {
  const b = 'inline-flex items-center gap-1.5 rounded-full border border-border px-3 py-1.5 text-[13px] text-muted hover:border-lime hover:text-text'
  return (
    <div className="flex flex-wrap gap-2" role="group" aria-label="Add">
      <button type="button" onClick={onExercise} className={`${b} !border-lime-dim !text-lime`}><Dumbbell size={14} aria-hidden /> Exercise</button>
      <button type="button" onClick={() => onStep(stepOf.rest())} className={b}><Timer size={14} aria-hidden /> Rest</button>
      <button type="button" onClick={() => onStep(stepOf.warmup())} className={b}><Sun size={14} aria-hidden /> Warm-up</button>
      <button type="button" onClick={() => onStep(stepOf.cooldown())} className={b}><Moon size={14} aria-hidden /> Cool-down</button>
      <button type="button" onClick={() => onStep(stepOf.interval())} className={b}><Footprints size={14} aria-hidden /> Interval</button>
      <button type="button" onClick={onRepeat} className={b}><Repeat size={14} aria-hidden /> {wrapping > 0 ? `Repeat the ${wrapping === 1 ? 'step' : `${wrapping} steps`}` : 'Repeat'}</button>
    </div>
  )
}

export type { Action }
