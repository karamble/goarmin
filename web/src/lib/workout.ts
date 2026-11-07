// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { WorkoutStep } from '../api/types'
import { clock, distance, type Units } from './format'

/** A workout as the page lays it out: steps, and repeat blocks holding their steps. */
export type Block = { kind: 'step'; step: WorkoutStep } | { kind: 'repeat'; times: number; steps: WorkoutStep[] }

/** blocks folds repeat steps into blocks: a repeat step runs the steps from its From up to
 * itself Repeat times in all, so those steps move into the block. */
export function blocks(steps: WorkoutStep[]): Block[] {
  const out: Block[] = []
  for (const s of steps) {
    if (s.duration !== 'repeat') {
      out.push({ kind: 'step', step: s })
      continue
    }
    const from = s.from ?? 0
    const inner: WorkoutStep[] = []
    while (out.length) {
      const last = out[out.length - 1]
      if (last.kind !== 'step' || last.step.index < from) break
      inner.unshift(last.step)
      out.pop()
    }
    out.push({ kind: 'repeat', times: Math.max(1, s.repeat ?? 1), steps: inner })
  }
  return out
}

const words = (s: string) => s.replace(/_/g, ' ')

/** durationText says how long a step lasts. */
export function durationText(s: WorkoutStep, u: Units): string {
  const v = s.duration_value ?? 0
  switch (s.duration) {
    case 'time':
      return clock(v)
    case 'distance':
      return distance(v, u)
    case 'reps':
      return `${v} reps`
    case 'calories':
      return `${v} kcal`
    case 'open':
      return 'until Lap'
    case 'hr_less_than':
      return `until HR < ${v}${s.target_pct ? ' %' : ' bpm'}`
    case 'hr_greater_than':
      return `until HR > ${v}${s.target_pct ? ' %' : ' bpm'}`
    case 'power_less_than':
      return `until power < ${v}${s.target_pct ? ' % FTP' : ' W'}`
    case 'power_greater_than':
      return `until power > ${v}${s.target_pct ? ' % FTP' : ' W'}`
  }
  return words(s.duration)
}

/** targetText says what a step aims at ('' for none). */
export function targetText(s: WorkoutStep, u: Units): string {
  if (!s.target || s.target === 'open') return ''
  if (s.target_zone) return `${s.target === 'heart_rate' ? 'HR' : words(s.target)} zone ${s.target_zone}`
  const lo = s.target_low ?? 0
  const hi = s.target_high ?? 0
  if (!lo && !hi) return ''
  switch (s.target) {
    case 'heart_rate':
      return `${lo}–${hi}${s.target_pct ? ' % max HR' : ' bpm'}`
    case 'power':
      return `${lo}–${hi}${s.target_pct ? ' % FTP' : ' W'}`
    case 'cadence':
      return `${lo}–${hi} rpm`
    case 'speed': {
      // Pace for the runner: the faster bound first.
      const per = u === 'imperial' ? 1609.344 : 1000
      return `${clock(per / hi)}–${clock(per / lo)} /${u === 'imperial' ? 'mi' : 'km'}`
    }
  }
  return `${words(s.target)} ${lo}–${hi}`
}

/** stepTitle names a step: its exercise, its own name, or its intensity. */
export function stepTitle(s: WorkoutStep): string {
  if (s.exercise?.name) return prettyExercise(s.exercise.name)
  if (s.name) return s.name
  const i = s.intensity
  return i === 'warmup' ? 'Warm up' : i === 'cooldown' ? 'Cool down' : i ? i[0].toUpperCase() + i.slice(1) : 'Step'
}

/** prettyExercise turns the FIT profile's "goblet_squat" into "Goblet squat"; a watch's own
 * title stays as it is. */
export function prettyExercise(n: string): string {
  if (!/^[a-z0-9_]+$/.test(n)) return n
  const w = words(n)
  return w[0].toUpperCase() + w.slice(1)
}

export interface Segment {
  intensity: string
  /** Seconds, or 0 when the step is not timed (shown with a nominal width). */
  s: number
}

/** strip is the workout as a strip of intensities in order, repeats unrolled. */
export function strip(steps: WorkoutStep[]): Segment[] {
  const out: Segment[] = []
  const add = (s: WorkoutStep) => out.push({ intensity: s.intensity || 'active', s: s.duration === 'time' ? (s.duration_value ?? 0) : 0 })
  for (const b of blocks(steps)) {
    if (b.kind === 'step') add(b.step)
    else for (let i = 0; i < b.times; i++) b.steps.forEach(add)
  }
  return out
}

/** intensityTone colours a step: warm-up and cool-down grey, work lime, recovery and rest cyan. */
export function intensityTone(i?: string): string {
  switch (i) {
    case 'warmup':
    case 'cooldown':
      return 'var(--grey2)'
    case 'recovery':
    case 'rest':
      return 'var(--cyan)'
    case 'interval':
      return 'var(--amber)'
  }
  return 'var(--lime)'
}
