// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { WorkoutStep } from '../api/types'
import { isRepeat } from './workout'

/** The workout builder's model. A workout's steps (as a watch file holds them, repeat steps
 * after the steps they repeat) become items: a single step, sets of one exercise (exercise and
 * rest in a repeat, as Garmin writes strength), or a repeat block of single steps. One level,
 * as in every Garmin workout: a repeat holds no sets and no repeat. */

export interface StepItem { kind: 'step'; id: string; step: WorkoutStep }
export interface SetsItem { kind: 'sets'; id: string; step: WorkoutStep; rest?: WorkoutStep; count: number }
export interface RepeatItem { kind: 'repeat'; id: string; items: StepItem[]; rounds: number; forS?: number }
export type Item = StepItem | SetsItem | RepeatItem

let seq = 0
/** newId names an item for this page's life. */
export const newId = () => `i${Date.now().toString(36)}${(++seq).toString(36)}`

export const isRest = (s: WorkoutStep) => s.intensity === 'rest' && !s.exercise
const hasExercise = (s: WorkoutStep) => !!s.exercise

/** fromSteps reads a workout's steps as items: a repeat of [exercise] or [exercise, rest] is
 * sets; any other repeat a block; an AMRAP a block for a time. */
export function fromSteps(steps: WorkoutStep[]): Item[] {
  const out: Item[] = []
  for (const s of steps) {
    if (!isRepeat(s)) {
      out.push({ kind: 'step', id: newId(), step: s })
      continue
    }
    const from = s.from ?? 0
    const inner: StepItem[] = []
    while (out.length) {
      const last = out[out.length - 1]
      if (last.kind !== 'step' || last.step.index < from) break
      inner.unshift(last)
      out.pop()
    }
    const sets = s.duration === 'repeat' && hasExercise(inner[0]?.step ?? ({} as WorkoutStep)) &&
      (inner.length === 1 || (inner.length === 2 && isRest(inner[1].step)))
    if (sets) out.push({ kind: 'sets', id: newId(), step: inner[0].step, rest: inner[1]?.step, count: s.repeat ?? 2 })
    else if (s.duration === 'repeat_until_time') out.push({ kind: 'repeat', id: newId(), items: inner, rounds: 0, forS: s.duration_value ?? 0 })
    else out.push({ kind: 'repeat', id: newId(), items: inner, rounds: s.repeat ?? 2 })
  }
  return out
}

/** toSteps writes items back as a watch file's steps, numbered in order: sets as exercise,
 * rest and a repeat (one set: no repeat), a block as its steps and a repeat; an empty block
 * writes nothing. */
export function toSteps(items: Item[]): WorkoutStep[] {
  const out: WorkoutStep[] = []
  const push = (s: WorkoutStep) => {
    out.push({ ...s, index: out.length })
    return out.length - 1
  }
  for (const it of items) {
    if (it.kind === 'step') push(it.step)
    else if (it.kind === 'sets') {
      const first = push(it.step)
      if (it.rest) push(it.rest)
      if (it.count > 1) push({ index: 0, duration: 'repeat', from: first, repeat: it.count })
    } else if (it.items.length) {
      const first = out.length
      it.items.forEach((c) => push(c.step))
      push(it.forS !== undefined ? { index: 0, duration: 'repeat_until_time', from: first, duration_value: it.forS } : { index: 0, duration: 'repeat', from: first, repeat: it.rounds })
    }
  }
  return out
}

/** stepCount is how many steps the file holds (a watch takes at most 100). */
export const stepCount = (items: Item[]) => toSteps(items).length
export const MAX_STEPS = 100

/** REP_SECONDS is goarmin's reckoning of one repetition, as the server states a total. */
export const REP_SECONDS = 3

/** estimate is about how long the workout takes, as goarmin states a built workout's total:
 * timed steps, and REP_SECONDS a repetition, with their sets and rounds. */
export function estimate(items: Item[]): number {
  const one = (s: WorkoutStep) => (s.duration === 'time' ? (s.duration_value ?? 0) : s.duration === 'reps' ? Math.round(s.duration_value ?? 0) * REP_SECONDS : 0)
  let t = 0
  for (const it of items) {
    if (it.kind === 'step') t += one(it.step)
    else if (it.kind === 'sets') t += (one(it.step) + (it.rest ? one(it.rest) : 0)) * Math.max(1, it.count)
    else {
      const round = it.items.reduce((a, c) => a + one(c.step), 0)
      t += it.forS !== undefined ? Math.max(round, it.forS) : round * Math.max(1, it.rounds)
    }
  }
  return Math.round(t)
}

// Steps the add bar makes.
export const stepOf = {
  exercise: (e: { category: string; number: number; title: string }): WorkoutStep => ({
    index: 0, intensity: 'active', duration: 'reps', duration_value: 10, target: 'open',
    exercise: { category: e.category, number: e.number, name: e.title, title: e.title },
  }),
  rest: (): WorkoutStep => ({ index: 0, intensity: 'rest', duration: 'time', duration_value: 30, target: 'open' }),
  warmup: (): WorkoutStep => ({ index: 0, intensity: 'warmup', duration: 'time', duration_value: 300 }),
  cooldown: (): WorkoutStep => ({ index: 0, intensity: 'cooldown', duration: 'time', duration_value: 300 }),
  interval: (): WorkoutStep => ({ index: 0, intensity: 'active', duration: 'time', duration_value: 60 }),
}

/** The workout's own fields. */
export interface Meta { name: string; sport: string; sub_sport: string; description: string }

export interface EditorState {
  meta: Meta
  items: Item[]
  /** The selected items, the focused one first. */
  selected: string[]
  past: { meta: Meta; items: Item[] }[]
  future: { meta: Meta; items: Item[] }[]
}

export type Action =
  | { type: 'meta'; patch: Partial<Meta> }
  | { type: 'insert'; item: Item; after?: string | null }
  | { type: 'remove'; id: string }
  | { type: 'duplicate'; id: string }
  | { type: 'move'; id: string; parent: string | null; index: number }
  | { type: 'moveBy'; id: string; delta: -1 | 1 }
  | { type: 'into'; id: string; repeat: string }
  | { type: 'out'; id: string }
  | { type: 'wrap'; ids: string[] }
  | { type: 'unwrap'; id: string }
  | { type: 'toSets'; id: string }
  | { type: 'toSingle'; id: string }
  | { type: 'step'; id: string; patch: Partial<WorkoutStep> }
  | { type: 'rest'; id: string; patch: Partial<WorkoutStep> | null }
  | { type: 'sets'; id: string; count: number }
  | { type: 'repeat'; id: string; rounds?: number; forS?: number | null }
  | { type: 'select'; ids: string[] }
  | { type: 'replace'; meta: Meta; items: Item[] }
  | { type: 'undo' }
  | { type: 'redo' }

export function initState(meta: Meta, items: Item[]): EditorState {
  return { meta, items, selected: [], past: [], future: [] }
}

/** find locates an item: its block (null at the top) and its place in it. */
export function find(items: Item[], id: string): { parent: RepeatItem | null; index: number; item: Item } | null {
  for (let i = 0; i < items.length; i++) {
    const it = items[i]
    if (it.id === id) return { parent: null, index: i, item: it }
    if (it.kind === 'repeat') {
      const j = it.items.findIndex((c) => c.id === id)
      if (j >= 0) return { parent: it, index: j, item: it.items[j] }
    }
  }
  return null
}

const clone = (items: Item[]): Item[] => items.map((it) => (it.kind === 'repeat' ? { ...it, items: [...it.items] } : { ...it }))

/** take removes an item from wherever it is (in a copy) and returns it. */
function take(items: Item[], id: string): Item | null {
  const at = find(items, id)
  if (!at) return null
  if (at.parent) {
    const p = items.find((x) => x.id === at.parent!.id) as RepeatItem
    p.items.splice(at.index, 1)
  } else items.splice(at.index, 1)
  return at.item
}

/** canHold tells whether an item may go into a repeat block: single steps only. */
export const canHold = (it: Item) => it.kind === 'step'

const freshCopy = (it: Item): Item =>
  it.kind === 'repeat' ? { ...it, id: newId(), items: it.items.map((c) => ({ ...c, id: newId() })) } : { ...it, id: newId() }

/** edit applies an action to items (a copy), or returns null when it does not apply. */
function edit(items: Item[], meta: Meta, a: Action): { items: Item[]; meta: Meta; selected?: string[] } | null {
  const next = clone(items)
  switch (a.type) {
    case 'meta':
      return { items, meta: { ...meta, ...a.patch } }
    case 'insert': {
      const at = a.after ? find(next, a.after) : null
      if (at?.parent && canHold(a.item)) {
        const p = next.find((x) => x.id === at.parent!.id) as RepeatItem
        p.items.splice(at.index + 1, 0, a.item as StepItem)
      } else {
        const root = at?.parent ? next.findIndex((x) => x.id === at.parent!.id) : at ? at.index : next.length - 1
        next.splice(root + 1, 0, a.item)
      }
      return { items: next, meta, selected: [a.item.id] }
    }
    case 'remove':
      return take(next, a.id) ? { items: next, meta, selected: [] } : null
    case 'duplicate': {
      const at = find(next, a.id)
      if (!at) return null
      const copy = freshCopy(at.item)
      if (at.parent) (next.find((x) => x.id === at.parent!.id) as RepeatItem).items.splice(at.index + 1, 0, copy as StepItem)
      else next.splice(at.index + 1, 0, copy)
      return { items: next, meta, selected: [copy.id] }
    }
    case 'move': {
      const it = find(next, a.id)?.item
      if (!it) return null
      if (a.parent) {
        const p = next.find((x) => x.id === a.parent)
        if (!p || p.kind !== 'repeat' || !canHold(it)) return null
        take(next, a.id)
        p.items.splice(Math.max(0, Math.min(a.index, p.items.length)), 0, it as StepItem)
      } else {
        take(next, a.id)
        next.splice(Math.max(0, Math.min(a.index, next.length)), 0, it)
      }
      return { items: next, meta }
    }
    case 'moveBy': {
      const at = find(next, a.id)
      if (!at) return null
      if (at.parent) {
        const p = next.find((x) => x.id === at.parent!.id) as RepeatItem
        const to = at.index + a.delta
        if (to < 0 || to >= p.items.length) {
          // Off the block's edge: out of it, before or after it.
          p.items.splice(at.index, 1)
          const pi = next.indexOf(p)
          next.splice(a.delta < 0 ? pi : pi + 1, 0, at.item)
        } else {
          p.items.splice(at.index, 1)
          p.items.splice(to, 0, at.item as StepItem)
        }
      } else {
        const to = at.index + a.delta
        if (to < 0 || to >= next.length) return null
        next.splice(at.index, 1)
        next.splice(to, 0, at.item)
      }
      return { items: next, meta }
    }
    case 'into': {
      const it = find(next, a.id)?.item
      const p = next.find((x) => x.id === a.repeat)
      if (!it || !p || p.kind !== 'repeat' || !canHold(it)) return null
      const below = next.indexOf(p) > next.indexOf(it)
      take(next, a.id)
      if (below) p.items.unshift(it as StepItem)
      else p.items.push(it as StepItem)
      return { items: next, meta }
    }
    case 'out': {
      const at = find(next, a.id)
      if (!at?.parent) return null
      const p = next.find((x) => x.id === at.parent!.id) as RepeatItem
      p.items.splice(at.index, 1)
      const pi = next.indexOf(p)
      next.splice(at.index < p.items.length / 2 ? pi : pi + 1, 0, at.item)
      return { items: next, meta }
    }
    case 'wrap': {
      const idx = a.ids.map((id) => next.findIndex((x) => x.id === id)).filter((i) => i >= 0).sort((x, y) => x - y)
      if (!idx.length || idx.some((i) => next[i].kind !== 'step')) return null
      const lo = idx[0]
      const hi = idx[idx.length - 1]
      const steps = next.slice(lo, hi + 1)
      if (steps.some((x) => x.kind !== 'step')) return null
      const r: RepeatItem = { kind: 'repeat', id: newId(), items: steps as StepItem[], rounds: 3 }
      next.splice(lo, hi - lo + 1, r)
      return { items: next, meta, selected: [r.id] }
    }
    case 'unwrap': {
      const i = next.findIndex((x) => x.id === a.id)
      const r = next[i]
      if (!r || r.kind !== 'repeat') return null
      next.splice(i, 1, ...r.items)
      return { items: next, meta, selected: r.items.length ? [r.items[0].id] : [] }
    }
    case 'toSets': {
      const i = next.findIndex((x) => x.id === a.id)
      const it = next[i]
      if (!it || it.kind !== 'step' || !it.step.exercise) return null
      next[i] = { kind: 'sets', id: it.id, step: it.step, rest: stepOf.rest(), count: 3 }
      return { items: next, meta }
    }
    case 'toSingle': {
      const i = next.findIndex((x) => x.id === a.id)
      const it = next[i]
      if (!it || it.kind !== 'sets') return null
      const steps: StepItem[] = [{ kind: 'step', id: it.id, step: it.step }]
      if (it.rest) steps.push({ kind: 'step', id: newId(), step: it.rest })
      next.splice(i, 1, ...steps)
      return { items: next, meta }
    }
    case 'step': {
      const at = find(next, a.id)
      if (!at || at.item.kind === 'repeat') return null
      const step = withPatch(at.item.step, a.patch)
      const changed = { ...at.item, step } as Item
      if (at.parent) (next.find((x) => x.id === at.parent!.id) as RepeatItem).items[at.index] = changed as StepItem
      else next[at.index] = changed
      return { items: next, meta }
    }
    case 'rest': {
      const i = next.findIndex((x) => x.id === a.id)
      const it = next[i]
      if (!it || it.kind !== 'sets') return null
      next[i] = { ...it, rest: a.patch === null ? undefined : withPatch(it.rest ?? stepOf.rest(), a.patch) }
      return { items: next, meta }
    }
    case 'sets': {
      const i = next.findIndex((x) => x.id === a.id)
      const it = next[i]
      if (!it || it.kind !== 'sets') return null
      next[i] = { ...it, count: a.count }
      return { items: next, meta }
    }
    case 'repeat': {
      const i = next.findIndex((x) => x.id === a.id)
      const it = next[i]
      if (!it || it.kind !== 'repeat') return null
      const r: RepeatItem = { ...it }
      if (a.rounds !== undefined) r.rounds = a.rounds
      if (a.forS === null) r.forS = undefined
      else if (a.forS !== undefined) r.forS = a.forS
      next[i] = r
      return { items: next, meta }
    }
  }
  return null
}

/** withPatch changes a step, keeping what a duration and target imply: a step for reps aims
 * at open, one for time at nothing (as Garmin Connect writes them); no target, no range. */
function withPatch(s: WorkoutStep, p: Partial<WorkoutStep>): WorkoutStep {
  const out = { ...s, ...p }
  if (p.duration && p.duration !== s.duration && s.exercise && (!out.target || out.target === 'open')) {
    out.target = p.duration === 'reps' || p.duration === 'open' ? 'open' : undefined
  }
  if (p.target !== undefined && (!out.target || out.target === 'open')) {
    delete out.target_zone
    delete out.target_low
    delete out.target_high
    delete out.target_pct
  }
  for (const k of Object.keys(out) as (keyof WorkoutStep)[]) if (out[k] === undefined) delete out[k]
  return out
}

const HISTORY = 100

/** reduce runs the builder: edits go into the history (undo, redo); selecting does not. */
export function reduce(st: EditorState, a: Action): EditorState {
  switch (a.type) {
    case 'select':
      return { ...st, selected: a.ids }
    case 'undo': {
      const prev = st.past[st.past.length - 1]
      if (!prev) return st
      return { ...st, ...prev, past: st.past.slice(0, -1), future: [{ meta: st.meta, items: st.items }, ...st.future], selected: [] }
    }
    case 'redo': {
      const next = st.future[0]
      if (!next) return st
      return { ...st, ...next, past: [...st.past, { meta: st.meta, items: st.items }], future: st.future.slice(1), selected: [] }
    }
    case 'replace':
      return { ...st, meta: a.meta, items: a.items, past: [...st.past, { meta: st.meta, items: st.items }].slice(-HISTORY), future: [], selected: [] }
  }
  const done = edit(st.items, st.meta, a)
  if (!done) return st
  return {
    meta: done.meta, items: done.items, selected: done.selected ?? st.selected,
    past: [...st.past, { meta: st.meta, items: st.items }].slice(-HISTORY), future: [],
  }
}

/** A problem with the workout as it stands: what, and where (an item's id) when it is one. */
export interface Problem { id?: string; text: string }

const ZONES: Record<string, number> = { heart_rate: 5, power: 7 }

/** problems are the reasons a watch would not take the workout, as the server checks them. */
export function problems(st: { meta: Meta; items: Item[] }): Problem[] {
  const out: Problem[] = []
  const name = st.meta.name.trim().replace(/\s+/g, ' ')
  if (!name) out.push({ text: 'The workout needs a name.' })
  else if ([...name].length > 40) out.push({ text: 'A watch shows at most 40 characters of a name.' })
  if ([...st.meta.description.trim()].length > 1000) out.push({ text: 'The description holds at most 1,000 characters.' })
  if (!st.items.length) out.push({ text: 'Add a step.' })
  const n = stepCount(st.items)
  if (n > MAX_STEPS) out.push({ text: `A watch takes at most ${MAX_STEPS} steps; this is ${n} (sets count three each).` })
  const step = (id: string, s: WorkoutStep, what: string) => {
    if (s.duration !== 'open' && !((s.duration_value ?? 0) > 0)) out.push({ id, text: `${what}: give it a ${s.duration === 'reps' ? 'number of reps' : s.duration.replace(/_/g, ' ')}.` })
    if ([...(s.notes ?? '')].length > 200) out.push({ id, text: `${what}: notes hold at most 200 characters.` })
    if ([...(s.name ?? '')].length > 40) out.push({ id, text: `${what}: a step name holds at most 40 characters.` })
    if (s.target && s.target !== 'open') {
      if (s.target_zone) {
        if (s.target_zone > (ZONES[s.target] ?? 7)) out.push({ id, text: `${what}: no such zone.` })
      } else if (!((s.target_low ?? 0) > 0 && (s.target_high ?? 0) > 0 && (s.target_low ?? 0) <= (s.target_high ?? 0))) {
        out.push({ id, text: `${what}: the target needs a zone, or a low and a high.` })
      }
    }
    if ((s.weight_kg ?? 0) > 500) out.push({ id, text: `${what}: a weight up to 500 kg.` })
  }
  st.items.forEach((it, i) => {
    const what = `Step ${i + 1}`
    if (it.kind === 'step') step(it.id, it.step, what)
    else if (it.kind === 'sets') {
      step(it.id, it.step, what)
      if (it.rest) step(it.id, it.rest, `${what}, the rest`)
      if (!(it.count >= 1 && it.count <= 99)) out.push({ id: it.id, text: `${what}: 1 to 99 sets.` })
    } else {
      if (!it.items.length) out.push({ id: it.id, text: `${what}: the repeat holds no steps.` })
      if (it.forS !== undefined ? !(it.forS > 0) : !(it.rounds >= 2 && it.rounds <= 99)) {
        out.push({ id: it.id, text: it.forS !== undefined ? `${what}: give the rounds a time.` : `${what}: repeat 2 to 99 times.` })
      }
      it.items.forEach((c, j) => step(c.id, c.step, `${what}.${j + 1}`))
    }
  })
  return out
}
