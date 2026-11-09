// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import type { WorkoutStep } from '../api/types'
import {
  estimate, fromSteps, initState, problems, reduce, REP_SECONDS, stepCount, stepOf, toSteps, type EditorState, type Item,
  type RepeatItem, type SetsItem,
} from './workoutEdit'

const ex = (category: string, number: number, title: string) => ({ category, number, name: title, title })

// Garmin's own layouts: a strength workout of sets (Endless Strokes' shape), a circuit, an
// AMRAP, and plain timed steps.
const sets: WorkoutStep[] = [
  { index: 0, intensity: 'warmup', duration: 'time', duration_value: 300 },
  { index: 1, intensity: 'active', duration: 'reps', duration_value: 12, target: 'open', exercise: ex('plank', 43, 'Plank') },
  { index: 2, intensity: 'rest', duration: 'time', duration_value: 60, target: 'open' },
  { index: 3, duration: 'repeat', from: 1, repeat: 3 },
  { index: 4, intensity: 'active', duration: 'reps', duration_value: 10, target: 'open', exercise: ex('squat', 29, 'Dumbbell Squat') },
  { index: 5, duration: 'repeat', from: 4, repeat: 2 },
]
const circuit: WorkoutStep[] = [
  { index: 0, intensity: 'active', duration: 'time', duration_value: 40, exercise: ex('core', 51, 'Cat Cow') },
  { index: 1, intensity: 'active', duration: 'time', duration_value: 40, exercise: ex('pose', 11, "Child's Pose") },
  { index: 2, intensity: 'rest', duration: 'time', duration_value: 20, target: 'open' },
  { index: 3, duration: 'repeat', from: 0, repeat: 4 },
]
const amrap: WorkoutStep[] = [
  { index: 0, intensity: 'active', duration: 'reps', duration_value: 5, target: 'open', exercise: ex('push_up', 77, 'Push Up') },
  { index: 1, intensity: 'active', duration: 'reps', duration_value: 10, target: 'open', exercise: ex('squat', 61, 'Squat') },
  { index: 2, duration: 'repeat_until_time', from: 0, duration_value: 600 },
]

const state = (items: Item[]): EditorState => initState({ name: 'Test', sport: 'training', sub_sport: 'strength_training', description: '' }, items)

describe('fromSteps and toSteps', () => {
  it('read Garmin layouts as sets, blocks and steps, and write them back the same', () => {
    for (const steps of [sets, circuit, amrap]) expect(toSteps(fromSteps(steps))).toEqual(steps)
    const a = fromSteps(sets)
    expect(a.map((x) => x.kind)).toEqual(['step', 'sets', 'sets'])
    expect((a[1] as SetsItem).count).toBe(3)
    expect((a[1] as SetsItem).rest?.duration_value).toBe(60)
    expect((a[2] as SetsItem).rest).toBeUndefined()
    const c = fromSteps(circuit)
    expect(c.map((x) => x.kind)).toEqual(['repeat'])
    expect((c[0] as RepeatItem).items).toHaveLength(3)
    expect((fromSteps(amrap)[0] as RepeatItem).forS).toBe(600)
  })

  it('writes one set without a repeat and an empty block as nothing', () => {
    const items: Item[] = [
      { kind: 'sets', id: 'a', step: stepOf.exercise({ category: 'plank', number: 43, title: 'Plank' }), rest: stepOf.rest(), count: 1 },
      { kind: 'repeat', id: 'b', items: [], rounds: 3 },
    ]
    expect(toSteps(items).map((s) => s.duration)).toEqual(['reps', 'time'])
    expect(stepCount(fromSteps(sets))).toBe(6)
  })
})

describe('the builder', () => {
  it('inserts after the selection, into a block only what it may hold', () => {
    let st = state(fromSteps(circuit))
    const block = st.items[0] as RepeatItem
    st = reduce(st, { type: 'insert', item: { kind: 'step', id: 'r', step: stepOf.rest() }, after: block.items[0].id })
    expect((st.items[0] as RepeatItem).items.map((c) => c.id)[1]).toBe('r')
    st = reduce(st, { type: 'insert', item: { kind: 'sets', id: 's', step: stepOf.exercise({ category: 'plank', number: 43, title: 'Plank' }), count: 3 }, after: 'r' })
    expect(st.items.map((x) => x.id)).toEqual([block.id, 's'])
    expect(st.selected).toEqual(['s'])
  })

  it('moves, and takes a step out of a block at its edge', () => {
    let st = state(fromSteps(circuit))
    const block = st.items[0] as RepeatItem
    const first = block.items[0].id
    st = reduce(st, { type: 'moveBy', id: first, delta: -1 })
    expect(st.items.map((x) => x.kind)).toEqual(['step', 'repeat'])
    st = reduce(st, { type: 'into', id: first, repeat: block.id })
    expect((st.items[0] as RepeatItem).items[0].id).toBe(first)
    st = reduce(st, { type: 'move', id: first, parent: null, index: 1 })
    expect(st.items.map((x) => x.kind)).toEqual(['repeat', 'step'])
    // Sets never go into a block.
    st = reduce(st, { type: 'insert', item: { kind: 'sets', id: 's', step: stepOf.exercise({ category: 'plank', number: 43, title: 'Plank' }), count: 2 } })
    const before = st
    expect(reduce(st, { type: 'move', id: 's', parent: block.id, index: 0 })).toBe(before)
  })

  it('wraps steps in a block and unwraps it; makes sets and single steps', () => {
    let st = state(fromSteps(amrap.slice(0, 2)))
    const ids = st.items.map((x) => x.id)
    st = reduce(st, { type: 'wrap', ids })
    expect(st.items).toHaveLength(1)
    expect((st.items[0] as RepeatItem).rounds).toBe(3)
    st = reduce(st, { type: 'unwrap', id: st.items[0].id })
    expect(st.items.map((x) => x.id)).toEqual(ids)
    st = reduce(st, { type: 'toSets', id: ids[0] })
    expect(st.items[0].kind).toBe('sets')
    expect(toSteps(st.items).map((s) => s.duration)).toEqual(['reps', 'time', 'repeat', 'reps'])
    st = reduce(st, { type: 'toSingle', id: ids[0] })
    expect(st.items.map((x) => x.kind)).toEqual(['step', 'step', 'step'])
  })

  it('edits a step as Garmin writes it, and undoes and redoes', () => {
    let st = state([{ kind: 'step', id: 'a', step: stepOf.exercise({ category: 'plank', number: 43, title: 'Plank' }) }])
    st = reduce(st, { type: 'step', id: 'a', patch: { duration: 'time', duration_value: 45 } })
    const s = (st.items[0] as { step: WorkoutStep }).step
    expect(s.duration).toBe('time')
    expect(s.target).toBeUndefined() // a timed exercise aims at nothing
    st = reduce(st, { type: 'step', id: 'a', patch: { duration: 'reps', duration_value: 8 } })
    expect((st.items[0] as { step: WorkoutStep }).step.target).toBe('open')
    st = reduce(st, { type: 'meta', patch: { name: 'Changed' } })
    st = reduce(st, { type: 'undo' })
    expect(st.meta.name).toBe('Test')
    st = reduce(st, { type: 'undo' })
    expect((st.items[0] as { step: WorkoutStep }).step.duration).toBe('time')
    st = reduce(st, { type: 'redo' })
    st = reduce(st, { type: 'redo' })
    expect(st.meta.name).toBe('Changed')
    expect(reduce(st, { type: 'select', ids: ['a'] }).past).toHaveLength(st.past.length)
  })

  it('tells what a watch would not take, and how long it takes', () => {
    const st = state(fromSteps(sets))
    expect(problems(st)).toEqual([])
    expect(estimate(st.items)).toBe(300 + (12 * REP_SECONDS + 60) * 3 + 10 * REP_SECONDS * 2)
    expect(estimate(fromSteps(amrap))).toBe(600)
    const bad = reduce(reduce(st, { type: 'meta', patch: { name: ' ' } }), { type: 'step', id: st.items[0].id, patch: { duration_value: 0 } })
    expect(problems(bad).map((p) => p.text)).toEqual(['The workout needs a name.', 'Step 1: give it a time.'])
    const many = state(Array.from({ length: 34 }, (_, i) => ({ kind: 'sets', id: `s${i}`, step: stepOf.exercise({ category: 'plank', number: 43, title: 'Plank' }), rest: stepOf.rest(), count: 2 }) as Item))
    expect(problems(many)[0].text).toMatch(/at most 100 steps; this is 102/)
  })
})
