// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import type { WorkoutStep } from '../api/types'
import {
  addRepeat, addStep, body, canMoveInto, duplicate, emptyDraft, estimate, flatten, move, moveInto, moveOut, paceText, parseDuration,
  parsePace, remove, reorder, setRepeat, setStep, stepNumbers, toTree, ungroup, validate, wrap, type Draft, type EditRepeat, type Tree,
} from './workoutEditor'

// Intervals: a warm-up, 6 × (400 m, 90 s recovery), an AMRAP of two exercises for 10 minutes, a
// cool-down: the shapes a watch's workouts have.
const steps: WorkoutStep[] = [
  { index: 0, intensity: 'warmup', duration: 'time', duration_value: 600, target: 'heart_rate', target_zone: 2 },
  { index: 1, intensity: 'active', duration: 'distance', duration_value: 400, target: 'speed', target_low: 4, target_high: 4.3 },
  { index: 2, intensity: 'recovery', duration: 'time', duration_value: 90 },
  { index: 3, duration: 'repeat', from: 1, repeat: 6 },
  { index: 4, intensity: 'active', duration: 'reps', duration_value: 12, exercise: { category: 'squat', name: 'Goblet Squat', number: 37 } },
  { index: 5, intensity: 'active', duration: 'time', duration_value: 45, exercise: { category: 'plank', name: 'Plank', number: 43 } },
  { index: 6, duration: 'repeat_until_time', duration_value: 600, from: 4 },
  { index: 7, intensity: 'cooldown', duration: 'open' },
]

const keys = (t: Tree) => t.map((x) => (x.kind === 'step' ? x.step.intensity ?? x.step.duration : `[${x.steps.map((s) => s.step.intensity).join(',')}]`))

describe('the tree', () => {
  it('reads a workout and writes the same steps back', () => {
    const t = toTree(steps)
    expect(t.map((x) => x.kind)).toEqual(['step', 'repeat', 'repeat', 'step'])
    expect((t[1] as EditRepeat).times).toBe(6)
    expect((t[2] as EditRepeat).forS).toBe(600)
    expect(flatten(t)).toEqual(steps)
    expect(new Set([...t, ...(t[1] as EditRepeat).steps].map((x) => x.key)).size).toBe(6)
  })

  it('estimates as the server does', () => {
    // 600 + 6 × 90 + max(45, 600) + 0
    expect(estimate(toTree(steps))).toBe(600 + 540 + 600)
  })

  it('adds, duplicates, moves and removes', () => {
    let t = toTree(steps)
    const warm = t[0].key
    let k: string
    ;[t, k] = addStep(t, { index: 0, intensity: 'rest', duration: 'time', duration_value: 30 }, warm)
    expect(keys(t).slice(0, 2)).toEqual(['warmup', 'rest'])
    let copy: string
    ;[t, copy] = duplicate(t, k)
    expect(keys(t).slice(0, 3)).toEqual(['warmup', 'rest', 'rest'])
    t = move(t, warm, 1)
    expect(keys(t).slice(0, 3)).toEqual(['rest', 'warmup', 'rest'])
    t = move(t, t[0].key, -1) // the first stays first
    expect(keys(t)[0]).toBe('rest')
    t = remove(t, k)
    t = remove(t, copy)
    expect(flatten(t)).toEqual(steps)
  })

  it('adds a step inside a block after one of its steps', () => {
    let t = toTree(steps)
    const inner = (t[1] as EditRepeat).steps[1].key
    ;[t] = addStep(t, { index: 0, intensity: 'rest', duration: 'time', duration_value: 30 }, inner)
    expect(keys(t)[1]).toBe('[active,recovery,rest]')
    ;[t] = addRepeat(t, { index: 0, intensity: 'active', duration: 'time', duration_value: 60 }, inner) // after the block, not in it
    expect(keys(t).slice(1, 3)).toEqual(['[active,recovery,rest]', '[active]'])
  })

  it('moves steps into and out of blocks, wraps and ungroups', () => {
    let t = toTree(steps)
    const cool = t[3].key
    expect(canMoveInto(t, cool)).toBe(true)
    expect(canMoveInto(t, t[0].key)).toBe(false)
    t = moveInto(t, cool)
    expect(keys(t)).toEqual(['warmup', '[active,recovery]', '[active,active,cooldown]'])
    t = moveOut(t, cool)
    expect(keys(t)).toEqual(['warmup', '[active,recovery]', '[active,active]', 'cooldown'])
    const first = (t[1] as EditRepeat).steps[0].key
    t = moveOut(t, first) // the first step of a block goes before it
    expect(keys(t)).toEqual(['warmup', 'active', '[recovery]', '[active,active]', 'cooldown'])
    t = ungroup(t, t[2].key)
    let b: string
    ;[t, b] = wrap(t, t[1].key, t[2].key)
    expect(keys(t)).toEqual(['warmup', '[active,recovery]', '[active,active]', 'cooldown'])
    expect((t[1] as EditRepeat).times).toBe(2)
    expect(wrap(t, t[0].key, t[1].key)[1]).toBe('') // a block cannot go into a block
    t = setRepeat(t, b, { times: 6 })
    expect(flatten(t)).toEqual(steps)
  })

  it('reorders by drag and drop within a list', () => {
    let t = toTree(steps)
    t = reorder(t, t[3].key, t[0].key)
    expect(keys(t)).toEqual(['cooldown', 'warmup', '[active,recovery]', '[active,active]'])
    const block = t[2] as EditRepeat
    t = reorder(t, block.steps[1].key, block.steps[0].key)
    expect(keys(t)[2]).toBe('[recovery,active]')
  })

  it('numbers the steps as the list shows them', () => {
    const t = toTree(steps)
    const n = stepNumbers(t)
    expect(n.get(t[0].key)).toBe(1)
    expect(n.get((t[2] as EditRepeat).steps[0].key)).toBe(4)
    expect(n.get(t[3].key)).toBe(6)
  })
})

describe('validate', () => {
  const draft = (over: Partial<Draft> = {}): Draft => ({ name: 'Intervals', sport: 'running', sub_sport: 'generic', description: '', tree: toTree(steps), ...over })

  it('takes a good workout', () => {
    expect(validate(draft())).toEqual([])
  })

  it('names what keeps a workout from being saved', () => {
    const t = toTree(steps)
    const warm = t[0].key
    let bad = setStep(t, warm, { duration_value: 0 })
    bad = setStep(bad, t[3].key, { target: 'heart_rate' })
    bad = setRepeat(bad, t[1].key, { times: 1 })
    const p = validate(draft({ name: ' ', tree: bad }))
    expect(p.map((x) => x.text)).toEqual([
      'The workout needs a name.',
      'Step 1 needs a time.',
      'The repeat after step 1: repeat 2 to 99 times.',
      'Step 6 needs a zone or a range for its target.',
    ])
    expect(p[1].key).toBe(warm)
    expect(validate(draft({ name: 'x'.repeat(41) }))[0].text).toBe('A watch shows 40 characters of a name.')
    expect(validate(draft({ tree: [] }))[0].text).toBe('Add a step.')
  })
})

describe('inputs', () => {
  it('reads durations', () => {
    expect(parseDuration('90')).toBe(90)
    expect(parseDuration('1:30')).toBe(90)
    expect(parseDuration(' 1:02:00 ')).toBe(3720)
    expect(parseDuration('1:75')).toBeNull()
    expect(parseDuration('0')).toBeNull()
    expect(parseDuration('fast')).toBeNull()
  })

  it('reads and shows paces in the account units', () => {
    const v = parsePace('5:00', 'metric')!
    expect(v).toBeCloseTo(1000 / 300)
    expect(paceText(v, 'metric')).toBe('5:00')
    expect(paceText(parsePace('8:03', 'imperial')!, 'imperial')).toBe('8:03')
    expect(paceText(0, 'metric')).toBe('')
  })
})

describe('a new workout', () => {
  it('starts with a warm-up, a step for its sport and a cool-down, and saves as the server takes it', () => {
    const d = emptyDraft('training', 'strength_training')
    expect(d.tree.map((x) => (x.kind === 'step' ? x.step.duration : ''))).toEqual(['time', 'reps', 'time'])
    d.name = '  Core   20 '
    expect(body(d)).toEqual({ name: 'Core 20', sport: 'training', sub_sport: 'strength_training', description: '', steps: flatten(d.tree) })
    expect(body(d).steps.map((s) => s.index)).toEqual([0, 1, 2])
  })
})
