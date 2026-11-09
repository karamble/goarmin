// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { WorkoutStep } from '../api/types'
import type { Units } from './format'
import { blocks } from './workout'

/** A workout's steps as the editor holds them: steps, and repeat blocks holding steps (one
 * level, as a watch takes them). Every item has a key that stays while it moves. */
export type EditStep = { kind: 'step'; key: string; step: WorkoutStep }
export type EditRepeat = { kind: 'repeat'; key: string; times: number; forS?: number; steps: EditStep[] }
export type EditItem = EditStep | EditRepeat
export type Tree = EditItem[]

/** The editor's whole workout. */
export interface Draft {
  name: string
  sport: string
  sub_sport: string
  description: string
  tree: Tree
}

let seq = 0
/** newKey is a key no item has yet. */
export const newKey = () => `k${++seq}`

/** bare is a step without what the tree decides: its place and repeat fields. */
function bare(s: WorkoutStep): WorkoutStep {
  const { index: _i, from: _f, repeat: _r, ...rest } = s
  return { index: 0, ...rest }
}

/** toTree reads a workout's flat steps into the editor's tree. */
export function toTree(steps: WorkoutStep[]): Tree {
  return blocks(steps).map((b): EditItem => b.kind === 'step'
    ? { kind: 'step', key: newKey(), step: bare(b.step) }
    : {
        kind: 'repeat', key: newKey(), times: b.forS ? 1 : b.times, ...(b.forS ? { forS: b.forS } : {}),
        steps: b.steps.map((s) => ({ kind: 'step' as const, key: newKey(), step: bare(s) })),
      })
}

/** flatten writes the tree as a watch's steps: numbered from 0, a repeat step after its block
 * naming the block's first step. An empty block is left out. */
export function flatten(tree: Tree): WorkoutStep[] {
  const out: WorkoutStep[] = []
  const put = (s: WorkoutStep) => out.push({ ...bare(s), index: out.length })
  for (const it of tree) {
    if (it.kind === 'step') {
      put(it.step)
      continue
    }
    if (it.steps.length === 0) continue
    const from = out.length
    it.steps.forEach((s) => put(s.step))
    out.push(it.forS
      ? { index: out.length, duration: 'repeat_until_time', duration_value: it.forS, from }
      : { index: out.length, duration: 'repeat', from, repeat: it.times })
  }
  return out
}

/** estimate is the workout's time from its timed steps with their repeats, in seconds (as the
 * server's fitwatch.Estimate). */
export function estimate(tree: Tree): number {
  const timed = (s: WorkoutStep) => (s.duration === 'time' ? (s.duration_value ?? 0) : 0)
  let total = 0
  for (const it of tree) {
    if (it.kind === 'step') {
      total += timed(it.step)
      continue
    }
    const round = it.steps.reduce((a, s) => a + timed(s.step), 0)
    total += it.forS ? Math.max(round, it.forS) : round * it.times
  }
  return Math.round(total)
}

/** Where an item is: its list (the tree, or a block's steps) and its place in it. */
function find(tree: Tree, key: string): { list: EditItem[]; i: number; block?: EditRepeat } | null {
  const i = tree.findIndex((x) => x.key === key)
  if (i >= 0) return { list: tree, i }
  for (const b of tree) {
    if (b.kind !== 'repeat') continue
    const j = b.steps.findIndex((x) => x.key === key)
    if (j >= 0) return { list: b.steps, i: j, block: b }
  }
  return null
}

/** clone copies the tree's lists (items are replaced, never changed in place). */
const clone = (tree: Tree): Tree => tree.map((x) => (x.kind === 'repeat' ? { ...x, steps: [...x.steps] } : x))

/** at runs fn on a copy of the tree with the item's place; an unknown key leaves the tree. */
function at(tree: Tree, key: string, fn: (t: Tree, list: EditItem[], i: number, block?: EditRepeat) => void): Tree {
  const t = clone(tree)
  const f = find(t, key)
  if (!f) return tree
  fn(t, f.list, f.i, f.block)
  return t
}

/** addStep puts step after the item with key (at the end without one; into a block when the key
 * is a step of it), and returns the tree and the new step's key. */
export function addStep(tree: Tree, step: WorkoutStep, after?: string): [Tree, string] {
  const item: EditStep = { kind: 'step', key: newKey(), step: bare(step) }
  if (!after) return [[...tree, item], item.key]
  return [at(tree, after, (_t, list, i) => list.splice(i + 1, 0, item)), item.key]
}

/** addInto puts step at the end of the block with key (an empty block too). */
export function addInto(tree: Tree, block: string, step: WorkoutStep): [Tree, string] {
  const item: EditStep = { kind: 'step', key: newKey(), step: bare(step) }
  const t = clone(tree)
  const b = t.find((x) => x.key === block)
  if (!b || b.kind !== 'repeat') return [tree, '']
  b.steps.push(item)
  return [t, item.key]
}

/** addRepeat puts a new block with one step after the item with key (at the end without one). */
export function addRepeat(tree: Tree, step: WorkoutStep, after?: string): [Tree, string] {
  const block: EditRepeat = { kind: 'repeat', key: newKey(), times: 2, steps: [{ kind: 'step', key: newKey(), step: bare(step) }] }
  if (!after) return [[...tree, block], block.key]
  const f = find(tree, after)
  const anchor = f?.block ? f.block.key : after // a block goes after the block, not into it
  return [at(tree, anchor, (t, _list, i) => t.splice(i + 1, 0, block)), block.key]
}

export const remove = (tree: Tree, key: string) => at(tree, key, (_t, list, i) => void list.splice(i, 1))

/** duplicate copies an item (a block with its steps) right after it. */
export function duplicate(tree: Tree, key: string): [Tree, string] {
  let made = ''
  const t = at(tree, key, (_t, list, i) => {
    const x = list[i]
    const copy: EditItem = x.kind === 'step'
      ? { ...x, key: newKey() }
      : { ...x, key: newKey(), steps: x.steps.map((s) => ({ ...s, key: newKey() })) }
    made = copy.key
    list.splice(i + 1, 0, copy)
  })
  return [t, made]
}

/** move shifts an item one place earlier (-1) or later (+1) in its list. */
export const move = (tree: Tree, key: string, by: -1 | 1) => at(tree, key, (_t, list, i) => {
  const j = i + by
  if (j < 0 || j >= list.length) return
  ;[list[i], list[j]] = [list[j], list[i]]
})

/** reorder moves the item key to where over is, within one list (a drag and drop). */
export const reorder = (tree: Tree, key: string, over: string) => at(tree, key, (_t, list, i) => {
  const j = list.findIndex((x) => x.key === over)
  if (j < 0 || j === i) return
  const [x] = list.splice(i, 1)
  list.splice(j, 0, x)
})

/** canMoveInto tells whether a top-level step has a block right before it to join. */
export function canMoveInto(tree: Tree, key: string): boolean {
  const i = tree.findIndex((x) => x.key === key)
  return i > 0 && tree[i].kind === 'step' && tree[i - 1].kind === 'repeat'
}

/** moveInto moves a top-level step to the end of the block right before it. */
export function moveInto(tree: Tree, key: string): Tree {
  if (!canMoveInto(tree, key)) return tree
  const t = clone(tree)
  const i = t.findIndex((x) => x.key === key)
  const [x] = t.splice(i, 1)
  ;(t[i - 1] as EditRepeat).steps.push(x as EditStep)
  return t
}

/** moveOut moves a block's step out of it, right after the block (or before it, when it is the
 * block's first step). */
export function moveOut(tree: Tree, key: string): Tree {
  const f = find(tree, key)
  if (!f?.block) return tree
  const t = clone(tree)
  const bi = t.findIndex((x) => x.key === f.block!.key)
  const block = t[bi] as EditRepeat
  const [x] = block.steps.splice(f.i, 1)
  t.splice(f.i === 0 ? bi : bi + 1, 0, x)
  return t
}

/** wrap puts the top-level steps from key to to (inclusive, in tree order) into a new block
 * repeated twice. */
export function wrap(tree: Tree, key: string, to = key): [Tree, string] {
  const a = tree.findIndex((x) => x.key === key)
  const b = tree.findIndex((x) => x.key === to)
  if (a < 0 || b < 0) return [tree, '']
  const [lo, hi] = a <= b ? [a, b] : [b, a]
  const inner = tree.slice(lo, hi + 1)
  if (inner.some((x) => x.kind !== 'step')) return [tree, ''] // repeats do not nest
  const block: EditRepeat = { kind: 'repeat', key: newKey(), times: 2, steps: inner as EditStep[] }
  return [[...tree.slice(0, lo), block, ...tree.slice(hi + 1)], block.key]
}

/** ungroup replaces a block by its steps. */
export function ungroup(tree: Tree, key: string): Tree {
  const i = tree.findIndex((x) => x.key === key)
  const b = tree[i]
  if (!b || b.kind !== 'repeat') return tree
  return [...tree.slice(0, i), ...b.steps, ...tree.slice(i + 1)]
}

/** setStep changes a step's fields. */
export const setStep = (tree: Tree, key: string, patch: Partial<WorkoutStep>) => at(tree, key, (_t, list, i) => {
  const x = list[i]
  if (x.kind === 'step') list[i] = { ...x, step: { ...x.step, ...patch } }
})

/** setRepeat changes how often a block runs: times, or for a time (forS; 0 for none). */
export const setRepeat = (tree: Tree, key: string, patch: { times?: number; forS?: number }) => at(tree, key, (_t, list, i) => {
  const x = list[i]
  if (x.kind !== 'repeat') return
  const next: EditRepeat = { ...x, ...patch }
  if (!next.forS) delete next.forS
  list[i] = next
})

/** The limits a watch sets (fitwatch). */
export const maxName = 40
export const maxNotes = 200
export const watchNotes = 50
export const maxSteps = 100
export const maxDescription = 1000

/** A problem with the draft: on one item (key) or the whole workout. */
export interface Problem {
  key?: string
  text: string
}

const needsAmount: Record<string, string> = { time: 'a time', distance: 'a distance', reps: 'a number of reps', calories: 'calories' }

/** validate lists what keeps the draft from being saved, as the server would refuse it. */
export function validate(d: Draft): Problem[] {
  const out: Problem[] = []
  const name = d.name.trim()
  if (!name) out.push({ text: 'The workout needs a name.' })
  else if ([...name].length > maxName) out.push({ text: `A watch shows ${maxName} characters of a name.` })
  if (!d.sport) out.push({ text: 'Choose a sport.' })
  if ([...d.description.trim()].length > maxDescription) out.push({ text: `The description is longer than ${maxDescription} characters.` })
  const flat = flatten(d.tree)
  if (flat.length === 0) out.push({ text: 'Add a step.' })
  if (flat.length > maxSteps) out.push({ text: `A watch takes ${maxSteps} steps; this has ${flat.length} (repeats count as one).` })
  const step = (x: EditStep, where: string) => {
    const s = x.step
    if (needsAmount[s.duration] && !((s.duration_value ?? 0) > 0)) out.push({ key: x.key, text: `${where} needs ${needsAmount[s.duration]}.` })
    if ((s.duration === 'hr_less_than' || s.duration === 'hr_greater_than') && !((s.duration_value ?? 0) > 0)) {
      out.push({ key: x.key, text: `${where} needs a heart rate.` })
    }
    if (s.target && s.target !== 'open' && !s.target_zone && !((s.target_low ?? 0) > 0 || (s.target_high ?? 0) > 0)) {
      out.push({ key: x.key, text: `${where} needs a zone or a range for its target.` })
    }
    if ((s.target_low ?? 0) > 0 && (s.target_high ?? 0) > 0 && s.target_low! > s.target_high!) {
      out.push({ key: x.key, text: `${where}: the target's low end is above its high end.` })
    }
    if ([...(s.notes ?? '')].length > maxNotes) out.push({ key: x.key, text: `${where}: notes longer than ${maxNotes} characters.` })
  }
  let n = 0
  for (const it of d.tree) {
    if (it.kind === 'step') {
      step(it, `Step ${++n}`)
      continue
    }
    const where = `The repeat after step ${n}`
    if (it.steps.length === 0) out.push({ key: it.key, text: `${where} has no steps.` })
    if (!it.forS && (it.times < 2 || it.times > 99)) out.push({ key: it.key, text: `${where}: repeat 2 to 99 times.` })
    it.steps.forEach((s) => step(s, `Step ${++n}`))
  }
  return out
}

/** stepNumbers numbers the steps as the list shows them (blocks' steps included), by key. */
export function stepNumbers(tree: Tree): Map<string, number> {
  const m = new Map<string, number>()
  let n = 0
  for (const it of tree) {
    if (it.kind === 'step') m.set(it.key, ++n)
    else it.steps.forEach((s) => m.set(s.key, ++n))
  }
  return m
}

/** parseDuration reads "90", "1:30" or "1:02:00" as seconds (null if it is none of these). */
export function parseDuration(s: string): number | null {
  const t = s.trim()
  if (!/^\d+(:\d{1,2}){0,2}$/.test(t)) return null
  const parts = t.split(':').map(Number)
  if (parts.slice(1).some((p) => p >= 60)) return null
  const v = parts.reduce((a, p) => a * 60 + p, 0)
  return v > 0 ? v : null
}

const perMile = 1609.344

/** parsePace reads a pace ("5:15") per km or mile as a speed in m/s (null if none). */
export function parsePace(s: string, u: Units): number | null {
  const sec = parseDuration(s)
  if (!sec) return null
  return (u === 'imperial' ? perMile : 1000) / sec
}

/** paceText shows a speed as a pace per km or mile, to the second ("5:15"). */
export function paceText(mps: number | undefined, u: Units): string {
  if (!mps || mps <= 0) return ''
  const s = Math.round((u === 'imperial' ? perMile : 1000) / mps)
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

/** distanceIn and distanceFrom convert metres to the account's km or miles and back. */
export const distanceIn = (m: number, u: Units) => (u === 'imperial' ? m / perMile : m / 1000)
export const distanceFrom = (v: number, u: Units) => (u === 'imperial' ? v * perMile : v * 1000)
/** weightIn and weightFrom convert kilograms to the account's kg or lb and back. */
export const weightIn = (kg: number, u: Units) => (u === 'imperial' ? kg / 0.45359237 : kg)
export const weightFrom = (v: number, u: Units) => (u === 'imperial' ? v * 0.45359237 : v)

/** Sub sports whose steps are exercises (strength, cardio, yoga, pilates, HIIT, mobility). */
export function usesExercises(sport: string, sub: string): boolean {
  return sport === 'training' || sport === 'hiit' || sport === 'mobility' || sub === 'pilates'
}

/** newStep is a sensible first step for a sport: timed for endurance sports, reps for exercises. */
export function newStep(sport: string, sub: string, intensity = 'active'): WorkoutStep {
  return usesExercises(sport, sub) && intensity === 'active'
    ? { index: 0, intensity, duration: 'reps', duration_value: 10 }
    : { index: 0, intensity, duration: 'time', duration_value: intensity === 'active' ? 300 : 60 }
}

/** emptyDraft starts a new workout. */
export function emptyDraft(sport = 'running', sub = 'generic'): Draft {
  return {
    name: '', sport, sub_sport: sub, description: '',
    tree: [
      { kind: 'step', key: newKey(), step: { index: 0, intensity: 'warmup', duration: 'time', duration_value: 600 } },
      { kind: 'step', key: newKey(), step: newStep(sport, sub) },
      { kind: 'step', key: newKey(), step: { index: 0, intensity: 'cooldown', duration: 'time', duration_value: 300 } },
    ],
  }
}

/** body is the draft as the server takes it. */
export function body(d: Draft) {
  return { name: d.name.trim().replace(/\s+/g, ' '), sport: d.sport, sub_sport: d.sub_sport, description: d.description.trim(), steps: flatten(d.tree) }
}
