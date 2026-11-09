// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import {
  closestCenter, DndContext, KeyboardSensor, MouseSensor, TouchSensor, useSensor, useSensors, type Announcements, type DragEndEvent,
} from '@dnd-kit/core'
import { SortableContext, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { GripVertical, MoreHorizontal, Plus, Repeat } from 'lucide-react'
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { errorText } from '../../api/client'
import { useMe } from '../../api/hooks/auth'
import { useSaveWorkout, useWorkout, useWorkoutSports } from '../../api/hooks/training'
import type { Workout, WorkoutSport, WorkoutStep } from '../../api/types'
import { HeaderActions, useSectionTitle } from '../../app/section/HeaderSlot'
import { drop, load, save as keep } from '../../app/storage'
import type { ExerciseInfo } from '../../figures/api'
import { clock, hm, type Units } from '../../lib/format'
import { intensityTone, stepTitle, strip } from '../../lib/workout'
import {
  addInto, addRepeat, addStep, body, canMoveInto, distanceFrom, distanceIn, duplicate, emptyDraft, estimate, flatten, maxName, maxNotes, move,
  moveInto, moveOut, newStep, paceText, parseDuration, parsePace, remove, reorder, setRepeat, setStep, stepNumbers, toTree, ungroup,
  usesExercises, validate, watchNotes, weightFrom, weightIn, wrap, type Draft, type EditRepeat, type EditStep, type Problem, type Tree,
} from '../../lib/workoutEditor'
import { Menu, type MenuItem } from '../../ui/Menu'
import { NumberInput, ParsedInput } from '../../ui/ParsedInput'
import { Confirm } from '../../ui/Sheet'
import { useToast } from '../../ui/Toast'
import { Notice, Skeleton } from '../../ui/kit'
import { ExercisePicker } from './ExercisePicker'

/** WorkoutEditor makes a workout (training/workouts/new, ?from=<id> to start from a copy) or
 * edits one (training/workouts/:id/edit). */
export default function WorkoutEditorPage() {
  const editId = Number(useParams().id) || 0
  const [sp] = useSearchParams()
  const fromId = editId ? 0 : Number(sp.get('from')) || 0
  const src = useWorkout(editId || fromId)
  const sports = useWorkoutSports()
  const units = useMe().data?.units ?? 'metric'
  useSectionTitle(editId ? (src.data ? `Edit ${src.data.name}` : 'Edit workout') : fromId ? 'New workout from a copy' : 'New workout')
  if ((editId || fromId) && src.isLoading) return <Skeleton className="h-64" />
  if ((editId || fromId) && !src.data) {
    return <section className="card p-6"><p className="m-0 text-muted">This workout is not in your library. <Link className="text-lime" to="/training/workouts">All workouts</Link></p></section>
  }
  const initial = src.data ? fromWorkout(src.data, !editId) : emptyDraft()
  return <Editor key={`${editId}-${fromId}`} initial={initial} editId={editId} draftKey={`workout-draft.${editId || (fromId ? `from-${fromId}` : 'new')}`}
    units={units} sports={sports.data ?? []} original={editId ? src.data ?? undefined : undefined} />
}

/** fromWorkout is the draft for editing a workout, or for a copy of it. */
function fromWorkout(w: Workout, copy: boolean): Draft {
  const name = copy ? `${w.name} (copy)` : w.name
  return { name: [...name].slice(0, maxName).join(''), sport: w.sport, sub_sport: w.sub_sport || 'generic', description: w.description ?? '', tree: toTree(w.steps ?? []) }
}

const sameBody = (a: Draft, b: Draft) => JSON.stringify(body(a)) === JSON.stringify(body(b))

function readDraft(key: string): ReturnType<typeof body> | null {
  try {
    const v = load(key)
    return v ? JSON.parse(v) : null
  } catch {
    return null
  }
}

function writeDraft(key: string, d: Draft | null) {
  if (d) keep(key, JSON.stringify(body(d)))
  else drop(key)
}

function Editor({ initial, editId, draftKey, units, sports, original }: {
  initial: Draft
  editId: number
  draftKey: string
  units: Units
  sports: WorkoutSport[]
  original?: Workout
}) {
  const [d, setD] = useState(initial)
  const [stored] = useState(() => {
    const b = readDraft(draftKey)
    return b && JSON.stringify(b) !== JSON.stringify(body(initial)) ? b : null
  })
  const [offer, setOffer] = useState(stored !== null)
  const [leaving, setLeaving] = useState(false)
  const [picking, setPicking] = useState<string | null>(null)
  const save = useSaveWorkout()
  const toast = useToast()
  const navigate = useNavigate()
  const dirty = !sameBody(d, initial)
  const problems = useMemo(() => validate(d), [d])
  const back = editId ? `/training/workouts/${editId}` : '/training/workouts'

  useEffect(() => {
    if (!offer) writeDraft(draftKey, dirty ? d : null)
  }, [d, dirty, draftKey, offer])
  useEffect(() => {
    if (!dirty) return
    const ask = (e: BeforeUnloadEvent) => e.preventDefault()
    window.addEventListener('beforeunload', ask)
    return () => window.removeEventListener('beforeunload', ask)
  }, [dirty])

  const edit = (f: (t: Tree) => Tree) => setD((x) => ({ ...x, tree: f(x.tree) }))
  const submit = () => save.mutate({ id: editId || undefined, body: body(d) }, {
    onSuccess: (w) => {
      writeDraft(draftKey, null)
      const sending = (w.watches ?? []).filter((x) => x.pending === 'send')
      const old = (w.watches ?? []).filter((x) => (x.old_copies ?? 0) > 0)
      toast({
        text: [`${w.name} saved`, sending.length ? `it goes to ${sending.map((x) => x.name).join(' and ')} at the next sync` : '',
          old.length ? 'delete the old version on the watch' : ''].filter(Boolean).join('; '),
      })
      navigate(`/training/workouts/${w.id}`)
    },
    onError: (e) => toast({ text: `Not saved: ${errorText(e)}`, tone: 'error' }),
  })
  const sportOptions = useMemo(() => {
    const list = [...sports]
    if (d.sport && !list.some((s) => s.sport === d.sport && s.sub_sport === d.sub_sport)) {
      list.unshift({ sport: d.sport, sub_sport: d.sub_sport, label: `${d.sport.replace(/_/g, ' ')} (${d.sub_sport.replace(/_/g, ' ')})` })
    }
    return list
  }, [sports, d.sport, d.sub_sport])
  const steps = flatten(d.tree)
  const segs = strip(steps)
  const total = segs.reduce((a, s) => a + (s.s || 60), 0)
  const est = estimate(d.tree)
  const numbers = stepNumbers(d.tree)
  const problemOf = (key: string) => problems.find((p) => p.key === key)
  const pickedFor = picking ? findStep(d.tree, picking) : undefined

  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
      <HeaderActions>
        <button type="button" onClick={() => (dirty ? setLeaving(true) : navigate(back))}
          className="rounded-full border border-border px-3 py-1.5 text-[13px] text-muted hover:text-text">Cancel</button>
        <button type="button" onClick={submit} disabled={problems.length > 0 || save.isPending || (!!editId && !dirty)}
          title={problems[0]?.text}
          className="rounded-full bg-lime px-3.5 py-1.5 text-[13px] font-medium text-bg disabled:opacity-50">Save</button>
      </HeaderActions>
      {offer && stored && (
        <Notice>
          <span className="flex flex-wrap items-center gap-3">
            You have unsaved changes to this workout from before.
            <button type="button" className="text-lime underline-offset-2 hover:underline" onClick={() => {
              setD({ name: stored.name, sport: stored.sport, sub_sport: stored.sub_sport, description: stored.description, tree: toTree(stored.steps) })
              setOffer(false)
            }}>Restore them</button>
            <button type="button" className="text-muted underline-offset-2 hover:underline" onClick={() => { writeDraft(draftKey, null); setOffer(false) }}>Discard them</button>
          </span>
        </Notice>
      )}
      {original && original.origin !== 'goarmin' && (
        <Notice>
          You are editing the {original.origin === 'watch' ? "watch's" : "Garmin Connect"} version. goarmin keeps the original too. A watch that has
          this workout gets the edit at its next sync and keeps the old version until you delete it on the watch.
        </Notice>
      )}

      <section className="card grid gap-4 p-5 sm:p-6" aria-label="The workout">
        <label className="grid gap-1 text-[13px] text-muted">
          Name
          <input value={d.name} maxLength={maxName} onChange={(e) => setD({ ...d, name: e.target.value })}
            className="rounded-lg border border-border bg-bg2 px-3 py-2 text-[15px] text-text focus:border-lime focus:outline-none" />
          <span className="text-[12px] text-faint">{[...d.name.trim()].length} of {maxName} characters, as many as a watch shows</span>
        </label>
        <label className="grid gap-1 text-[13px] text-muted">
          Sport
          <select value={`${d.sport}/${d.sub_sport}`} onChange={(e) => {
            const [sport, sub_sport] = e.target.value.split('/')
            setD({ ...d, sport, sub_sport })
          }} className="max-w-[320px] rounded-lg border border-border bg-bg2 px-3 py-2 text-[15px] text-text focus:border-lime focus:outline-none">
            {sportOptions.map((s) => <option key={`${s.sport}/${s.sub_sport}`} value={`${s.sport}/${s.sub_sport}`}>{s.label}</option>)}
          </select>
        </label>
        <label className="grid gap-1 text-[13px] text-muted">
          Description
          <textarea value={d.description} rows={3} maxLength={1000} onChange={(e) => setD({ ...d, description: e.target.value })}
            className="rounded-lg border border-border bg-bg2 px-3 py-2 text-[14px] text-text focus:border-lime focus:outline-none" />
        </label>
        <div>
          <div className="flex h-3 w-full gap-[2px] overflow-hidden rounded-full" role="img" aria-label={`Intensity over the workout: ${segs.map((s) => s.intensity).join(', ')}`}>
            {segs.map((s, i) => <span key={i} style={{ flexGrow: (s.s || 60) / Math.max(total, 1), background: intensityTone(s.intensity) }} />)}
          </div>
          <p className="m-0 mt-2 text-[13px] text-muted">
            {steps.length} steps{est > 0 ? ` · about ${hm(est)} of timed steps` : ''}
          </p>
        </div>
      </section>

      <StepList tree={d.tree} edit={edit} numbers={numbers} units={units} sport={d.sport} sub={d.sub_sport} problemOf={problemOf} onPick={setPicking} />

      {problems.length > 0 && (
        <Notice tone="error">
          <span className="grid gap-1">
            <span>Before saving:</span>
            <ul className="m-0 pl-4">{problems.slice(0, 5).map((p, i) => <li key={i}>{p.text}</li>)}</ul>
          </span>
        </Notice>
      )}

      {picking && (
        <ExercisePicker current={pickedFor?.exercise} onClose={() => setPicking(null)}
          onPick={(e: ExerciseInfo) => {
            edit((t) => setStep(t, picking, { exercise: { category: e.category, number: e.number, name: e.title, title: e.title } }))
            setPicking(null)
          }} />
      )}
      {leaving && (
        <Confirm title="Leave without saving?" confirm="Leave" danger onClose={() => setLeaving(false)}
          onConfirm={() => { writeDraft(draftKey, null); navigate(back) }}>
          Your changes to this workout are lost.
        </Confirm>
      )}
    </div>
  )
}

function findStep(tree: Tree, key: string): WorkoutStep | undefined {
  for (const it of tree) {
    if (it.kind === 'step' && it.key === key) return it.step
    if (it.kind === 'repeat') {
      const s = it.steps.find((x) => x.key === key)
      if (s) return s.step
    }
  }
  return undefined
}

interface ListProps {
  tree: Tree
  edit: (f: (t: Tree) => Tree) => void
  numbers: Map<string, number>
  units: Units
  sport: string
  sub: string
  problemOf: (key: string) => Problem | undefined
  onPick: (key: string) => void
}

/** StepList is the workout's steps and repeat blocks, reordered by a grip or a menu. */
function StepList(p: ListProps) {
  const { tree, edit, sport, sub } = p
  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 6 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 250, tolerance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )
  const name = (key: string) => {
    const n = p.numbers.get(key)
    return n ? `step ${n}` : 'the repeat'
  }
  const announcements: Announcements = {
    onDragStart: ({ active }) => `Picked up ${name(String(active.id))}.`,
    onDragOver: ({ active, over }) => (over ? `${name(String(active.id))} is over ${name(String(over.id))}.` : `${name(String(active.id))} is no longer over a step.`),
    onDragEnd: ({ active, over }) => (over ? `${name(String(active.id))} dropped at ${name(String(over.id))}.` : `${name(String(active.id))} dropped.`),
    onDragCancel: ({ active }) => `Moving ${name(String(active.id))} was cancelled.`,
  }
  const onDragEnd = (e: DragEndEvent) => {
    if (e.over && e.active.id !== e.over.id) edit((t) => reorder(t, String(e.active.id), String(e.over!.id)))
  }
  const last = tree[tree.length - 1]?.key
  return (
    <section className="grid gap-3" aria-label="Steps">
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}
        accessibility={{ announcements, screenReaderInstructions: { draggable: 'To move a step, press Space, then the arrow keys; Space drops it, Escape cancels.' } }}>
        <SortableContext items={tree.map((x) => x.key)} strategy={verticalListSortingStrategy}>
          <ol className="m-0 grid list-none gap-3 p-0">
            {tree.map((it, i) => it.kind === 'step'
              ? <Sortable key={it.key} id={it.key}><StepCard {...p} it={it} first={i === 0} lastInList={i === tree.length - 1} /></Sortable>
              : <Sortable key={it.key} id={it.key}><RepeatCard {...p} it={it} first={i === 0} lastInList={i === tree.length - 1} /></Sortable>)}
          </ol>
        </SortableContext>
      </DndContext>
      <div className="flex flex-wrap gap-2">
        <button type="button" onClick={() => edit((t) => addStep(t, newStep(sport, sub), last)[0])}
          className="inline-flex items-center gap-1.5 rounded-full border border-border px-3 py-1.5 text-[13px] text-lime hover:border-lime">
          <Plus size={14} aria-hidden /> Add a step
        </button>
        <button type="button" onClick={() => edit((t) => addRepeat(t, newStep(sport, sub), last)[0])}
          className="inline-flex items-center gap-1.5 rounded-full border border-border px-3 py-1.5 text-[13px] text-muted hover:border-lime hover:text-text">
          <Repeat size={14} aria-hidden /> Add a repeat
        </button>
      </div>
    </section>
  )
}

/** Sortable is one draggable item; its grip is the handle passed down through context. */
function Sortable({ id, children }: { id: string; children: ReactNode }) {
  const s = useSortable({ id })
  return (
    <li ref={s.setNodeRef} style={{ transform: CSS.Translate.toString(s.transform), transition: s.transition }} className={s.isDragging ? 'opacity-50' : ''}>
      <GripContext value={{ ref: s.setActivatorNodeRef, props: { ...s.attributes, ...s.listeners } }}>{children}</GripContext>
    </li>
  )
}

const GripContext = createContext<{ ref: (el: HTMLElement | null) => void; props: Record<string, unknown> } | null>(null)

function Grip({ label }: { label: string }) {
  const g = useContext(GripContext)
  if (!g) return null
  return (
    <button ref={g.ref} {...g.props} type="button" aria-label={`Move ${label}`}
      className="grid h-8 w-8 shrink-0 cursor-grab touch-none place-items-center rounded-lg text-muted hover:bg-panel2 hover:text-lime active:cursor-grabbing">
      <GripVertical size={16} />
    </button>
  )
}

const intensities: [string, string][] = [['warmup', 'Warm-up'], ['active', 'Work'], ['recovery', 'Recovery'], ['rest', 'Rest'], ['cooldown', 'Cool-down']]
/** titleOf names a step in the editor: its exercise, its name, or its intensity in the
 * intensity menu's words. */
const titleOf = (s: WorkoutStep) =>
  s.exercise ? s.exercise.title || stepTitle(s) : s.name || intensities.find(([v]) => v === (s.intensity || 'active'))?.[1] || stepTitle(s)

const durations: [string, string][] = [
  ['time', 'Time'], ['distance', 'Distance'], ['reps', 'Reps'], ['calories', 'Calories'], ['open', 'Until you press Lap'],
  ['hr_greater_than', 'Until heart rate is above'], ['hr_less_than', 'Until heart rate is below'],
]
const durationDefault: Record<string, number | undefined> = { time: 300, distance: 1000, reps: 10, calories: 50, hr_greater_than: 150, hr_less_than: 120 }
const select = 'rounded-lg border border-border bg-bg2 px-2.5 py-1.5 text-[14px] text-text focus:border-lime focus:outline-none'

/** endurance tells sports whose pace target reads as a pace (per km or mile), not a speed. */
const byPace = (sport: string) => sport === 'running' || sport === 'walking' || sport === 'hiking'

function StepCard(p: ListProps & { it: EditStep; first: boolean; lastInList: boolean; inRepeat?: EditRepeat }) {
  const { it, edit, units, sport, sub, inRepeat } = p
  const s = it.step
  const n = p.numbers.get(it.key) ?? 0
  const set = (patch: Partial<WorkoutStep>) => edit((t) => setStep(t, it.key, patch))
  const problem = p.problemOf(it.key)
  const items: MenuItem[] = [
    { label: 'Move earlier', run: () => edit((t) => move(t, it.key, -1)), disabled: p.first },
    { label: 'Move later', run: () => edit((t) => move(t, it.key, 1)), disabled: p.lastInList },
    ...(inRepeat ? [{ label: 'Move out of the repeat', run: () => edit((t) => moveOut(t, it.key)) }] : []),
    ...(!inRepeat ? [
      { label: 'Move into the repeat above', run: () => edit((t) => moveInto(t, it.key)), disabled: !canMoveInto(p.tree, it.key) },
      { label: 'Repeat this step', run: () => edit((t) => wrap(t, it.key)[0]) },
    ] : []),
    { label: 'Add a step after it', run: () => edit((t) => addStep(t, newStep(sport, sub), it.key)[0]), group: 'Step' },
    { label: 'Duplicate', run: () => edit((t) => duplicate(t, it.key)[0]) },
    { label: 'Remove', run: () => edit((t) => remove(t, it.key)), danger: true },
  ]
  // Rest and recovery steps have no exercise (unless one came with the workout).
  const exercises = !!s.exercise || (usesExercises(sport, sub) && s.intensity !== 'rest' && s.intensity !== 'recovery')
  return (
    <article className={`rounded-xl border bg-panel p-3 ${problem ? 'border-[#c9605f]' : 'border-border'}`} aria-label={`Step ${n}: ${titleOf(s)}`}>
      <header className="mb-3 flex items-center gap-2">
        <Grip label={`step ${n}`} />
        <span aria-hidden className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: intensityTone(s.intensity) }} />
        <span className="min-w-0 flex-1 truncate text-[14px] text-text">{n}. {titleOf(s)}</span>
        <Menu label={`More for step ${n}`} icon={<MoreHorizontal size={16} />} items={items} />
      </header>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="grid gap-1 text-[12px] text-muted">
          Intensity
          <select className={select} value={s.intensity ?? 'active'} onChange={(e) => set({ intensity: e.target.value })}>
            {intensities.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
            {s.intensity && !intensities.some(([v]) => v === s.intensity) && <option value={s.intensity}>{s.intensity}</option>}
          </select>
        </label>
        <div className="grid gap-1 text-[12px] text-muted">
          <label htmlFor={`${it.key}-dur`}>Duration</label>
          <div className="flex gap-2">
            <select id={`${it.key}-dur`} className={`${select} min-w-0 flex-1`} value={s.duration}
              onChange={(e) => set({ duration: e.target.value, duration_value: durationDefault[e.target.value] })}>
              {durations.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
              {!durations.some(([v]) => v === s.duration) && <option value={s.duration}>{s.duration.replace(/_/g, ' ')}</option>}
            </select>
            <DurationValue s={s} units={units} set={set} n={n} />
          </div>
        </div>
        <TargetFields s={s} units={units} set={set} n={n} pace={byPace(sport)} />
        {exercises && (
          <div className="grid gap-1 text-[12px] text-muted">
            Exercise
            <div className="flex items-center gap-2">
              <button type="button" onClick={() => p.onPick(it.key)}
                className="min-w-0 flex-1 truncate rounded-lg border border-border bg-bg2 px-2.5 py-1.5 text-left text-[14px] text-text hover:border-lime">
                {s.exercise ? (s.exercise.title || s.exercise.name) : 'Choose an exercise'}
              </button>
              {s.exercise && (
                <button type="button" onClick={() => set({ exercise: undefined, weight_kg: undefined })} className="text-[13px] text-muted hover:text-text">Clear</button>
              )}
            </div>
          </div>
        )}
        {s.exercise && (
          <label className="grid gap-1 text-[12px] text-muted">
            Weight ({units === 'imperial' ? 'lb' : 'kg'}, empty for none)
            <NumberInput label={`Weight of step ${n}`} decimals={1} value={s.weight_kg ? weightIn(s.weight_kg, units) : undefined}
              onChange={(v) => set({ weight_kg: v > 0 ? Number(weightFrom(v, units).toFixed(2)) : undefined })} />
          </label>
        )}
        <label className="grid gap-1 text-[12px] text-muted sm:col-span-2">
          Notes
          <input value={s.notes ?? ''} maxLength={maxNotes} onChange={(e) => set({ notes: e.target.value || undefined })}
            className="rounded-lg border border-border bg-bg2 px-2.5 py-1.5 text-[14px] text-text focus:border-lime focus:outline-none" />
          <span className="text-faint">{[...(s.notes ?? '')].length} of {maxNotes}; a watch shows the first {watchNotes}</span>
        </label>
      </div>
      {problem && <p className="m-0 mt-2 text-[13px] text-[#e08585]">{problem.text}</p>}
    </article>
  )
}

function DurationValue({ s, units, set, n }: { s: WorkoutStep; units: Units; set: (p: Partial<WorkoutStep>) => void; n: number }) {
  const w = 'w-28 shrink-0'
  switch (s.duration) {
    case 'time':
      return <ParsedInput label={`Time of step ${n}`} className={w} placeholder="1:30" value={s.duration_value || undefined}
        parse={parseDuration} format={clock} onChange={(v) => set({ duration_value: v })} />
    case 'distance':
      return <NumberInput label={`Distance of step ${n} in ${units === 'imperial' ? 'miles' : 'km'}`} className={w} decimals={2}
        value={s.duration_value ? distanceIn(s.duration_value, units) : undefined}
        onChange={(v) => set({ duration_value: Math.round(distanceFrom(v, units)) })} />
    case 'reps':
    case 'calories':
      return <NumberInput label={`${s.duration === 'reps' ? 'Reps' : 'Calories'} of step ${n}`} className={w} min={1} value={s.duration_value || undefined}
        onChange={(v) => set({ duration_value: v })} />
    case 'hr_greater_than':
    case 'hr_less_than':
    case 'power_greater_than':
    case 'power_less_than':
      return <NumberInput label={`${s.duration.startsWith('hr') ? 'Heart rate (bpm)' : 'Power (W)'} of step ${n}`} className={w} min={1}
        value={s.duration_value || undefined} onChange={(v) => set({ duration_value: v })} />
  }
  return null
}

const targets = (pace: boolean): [string, string][] => [['', 'None'], ['heart_rate', 'Heart rate'], ['speed', pace ? 'Pace' : 'Speed'], ['power', 'Power'], ['cadence', 'Cadence']]

function TargetFields({ s, units, set, n, pace }: { s: WorkoutStep; units: Units; set: (p: Partial<WorkoutStep>) => void; n: number; pace: boolean }) {
  const t = s.target === 'open' ? '' : s.target ?? ''
  const zones = t === 'heart_rate' ? 5 : t === 'power' ? 7 : 0
  const byZone = zones > 0 && (s.target_zone ?? 0) > 0
  const clear = { target_zone: undefined, target_low: undefined, target_high: undefined, target_pct: undefined }
  const kmh = units === 'imperial' ? 0.44704 : 1 / 3.6 // km/h or mph in m/s
  return (
    <div className="grid gap-1 text-[12px] text-muted sm:col-span-2">
      <label htmlFor={`target-${n}`}>Target</label>
      <div className="flex flex-wrap items-center gap-2">
        <select id={`target-${n}`} className={select} value={t}
          onChange={(e) => set({ target: e.target.value || undefined, ...clear, ...(e.target.value === 'heart_rate' || e.target.value === 'power' ? { target_zone: 2 } : {}) })}>
          {targets(pace).map(([v, l]) => <option key={v} value={v}>{l}</option>)}
        </select>
        {zones > 0 && (
          <select aria-label={`Zone or range for step ${n}`} className={select} value={byZone ? 'zone' : 'range'}
            onChange={(e) => set(e.target.value === 'zone' ? { ...clear, target_zone: 2 } : { ...clear, target_low: t === 'heart_rate' ? 120 : 150, target_high: t === 'heart_rate' ? 140 : 200 })}>
            <option value="zone">Zone</option>
            <option value="range">Range</option>
          </select>
        )}
        {byZone && (
          <select aria-label={`Zone of step ${n}`} className={select} value={s.target_zone} onChange={(e) => set({ target_zone: Number(e.target.value) })}>
            {Array.from({ length: zones }, (_, i) => <option key={i} value={i + 1}>Zone {i + 1}</option>)}
          </select>
        )}
        {t && !byZone && t !== 'speed' && (
          <>
            <NumberInput label={`Target from, step ${n}`} className="w-24" min={1} value={s.target_low || undefined} onChange={(v) => set({ target_low: v })} />
            <span>to</span>
            <NumberInput label={`Target to, step ${n}`} className="w-24" min={1} value={s.target_high || undefined} onChange={(v) => set({ target_high: v })} />
            <span>{t === 'heart_rate' ? (s.target_pct ? '% max HR' : 'bpm') : t === 'power' ? (s.target_pct ? '% FTP' : 'W') : 'rpm'}</span>
          </>
        )}
        {t === 'speed' && pace && (
          <>
            <ParsedInput label={`Slowest pace, step ${n}`} className="w-24" placeholder="5:30" value={s.target_low || undefined}
              parse={(x) => parsePace(x, units)} format={(v) => paceText(v, units)} onChange={(v) => set({ target_low: v })} />
            <span>to</span>
            <ParsedInput label={`Fastest pace, step ${n}`} className="w-24" placeholder="5:00" value={s.target_high || undefined}
              parse={(x) => parsePace(x, units)} format={(v) => paceText(v, units)} onChange={(v) => set({ target_high: v })} />
            <span>/{units === 'imperial' ? 'mi' : 'km'}</span>
          </>
        )}
        {t === 'speed' && !pace && (
          <>
            <NumberInput label={`Slowest speed, step ${n}`} className="w-24" decimals={1} value={s.target_low ? s.target_low / kmh : undefined}
              onChange={(v) => set({ target_low: Number((v * kmh).toFixed(3)) })} />
            <span>to</span>
            <NumberInput label={`Fastest speed, step ${n}`} className="w-24" decimals={1} value={s.target_high ? s.target_high / kmh : undefined}
              onChange={(v) => set({ target_high: Number((v * kmh).toFixed(3)) })} />
            <span>{units === 'imperial' ? 'mph' : 'km/h'}</span>
          </>
        )}
      </div>
    </div>
  )
}

function RepeatCard(p: ListProps & { it: EditRepeat; first: boolean; lastInList: boolean }) {
  const { it, edit, sport, sub } = p
  const problem = p.problemOf(it.key)
  const forTime = !!it.forS
  const items: MenuItem[] = [
    { label: 'Move earlier', run: () => edit((t) => move(t, it.key, -1)), disabled: p.first },
    { label: 'Move later', run: () => edit((t) => move(t, it.key, 1)), disabled: p.lastInList },
    { label: 'Ungroup', run: () => edit((t) => ungroup(t, it.key)), group: 'Repeat' },
    { label: 'Duplicate', run: () => edit((t) => duplicate(t, it.key)[0]) },
    { label: 'Remove with its steps', run: () => edit((t) => remove(t, it.key)), danger: true },
  ]
  const lastInner = it.steps[it.steps.length - 1]?.key
  return (
    <section className={`rounded-xl border-2 border-dashed p-2.5 ${problem ? 'border-[#c9605f]' : 'border-border'}`} aria-label="A repeat">
      <header className="mb-2.5 flex flex-wrap items-center gap-2 px-1">
        <Grip label="the repeat" />
        <Repeat size={14} aria-hidden className="text-lime" />
        <select aria-label="How the repeat runs" className={select} value={forTime ? 'time' : 'times'}
          onChange={(e) => edit((t) => setRepeat(t, it.key, e.target.value === 'time' ? { forS: 600 } : { forS: 0, times: Math.max(2, it.times) }))}>
          <option value="times">Repeat</option>
          <option value="time">As many rounds as fit in</option>
        </select>
        {forTime ? (
          <ParsedInput label="Time of the repeat" className="w-24" value={it.forS} parse={parseDuration} format={clock}
            onChange={(v) => edit((t) => setRepeat(t, it.key, { forS: v }))} />
        ) : (
          <>
            <NumberInput label="Times to repeat" className="w-20" min={1} max={99} value={it.times} onChange={(v) => edit((t) => setRepeat(t, it.key, { times: v }))} />
            <span className="text-[13px] text-muted">times</span>
          </>
        )}
        <span className="flex-1" />
        <Menu label="More for the repeat" icon={<MoreHorizontal size={16} />} items={items} />
      </header>
      {problem && <p className="m-0 mb-2 px-1 text-[13px] text-[#e08585]">{problem.text}</p>}
      <SortableContext items={it.steps.map((x) => x.key)} strategy={verticalListSortingStrategy}>
        <ol className="m-0 grid list-none gap-2 p-0">
          {it.steps.map((s, i) => (
            <Sortable key={s.key} id={s.key}>
              <StepCard {...p} it={s} first={i === 0} lastInList={i === it.steps.length - 1} inRepeat={it} />
            </Sortable>
          ))}
        </ol>
      </SortableContext>
      <button type="button" onClick={() => edit((t) => (lastInner ? addStep(t, newStep(sport, sub), lastInner)[0] : addInto(t, it.key, newStep(sport, sub))[0]))}
        className="mt-2 inline-flex items-center gap-1.5 rounded-full px-2 py-1 text-[13px] text-lime hover:bg-panel2">
        <Plus size={14} aria-hidden /> Add a step to the repeat
      </button>
    </section>
  )
}
