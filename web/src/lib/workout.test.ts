// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import type { WorkoutStep } from '../api/types'
import { blocks, durationText, prettyExercise, stepTitle, strip, targetText } from './workout'

const steps: WorkoutStep[] = [
  { index: 0, intensity: 'warmup', duration: 'time', duration_value: 600, target: 'heart_rate', target_zone: 2 },
  { index: 1, intensity: 'active', duration: 'distance', duration_value: 400, target: 'speed', target_low: 4, target_high: 4.3 },
  { index: 2, intensity: 'recovery', duration: 'time', duration_value: 90 },
  { index: 3, duration: 'repeat', from: 1, repeat: 6 },
  { index: 4, intensity: 'cooldown', duration: 'open' },
]

describe('workout', () => {
  it('folds repeats into blocks', () => {
    const b = blocks(steps)
    expect(b.map((x) => x.kind)).toEqual(['step', 'repeat', 'step'])
    expect(b[1]).toMatchObject({ kind: 'repeat', times: 6 })
    expect(b[1].kind === 'repeat' && b[1].steps.map((s) => s.index)).toEqual([1, 2])
  })

  it('says durations and targets the way a runner reads them', () => {
    expect(durationText(steps[0], 'metric')).toBe('10:00')
    expect(durationText(steps[1], 'metric')).toMatch(/^0\.4\d* km$|^400 m$/)
    expect(durationText(steps[4], 'metric')).toBe('until Lap')
    expect(durationText({ index: 0, duration: 'hr_less_than', duration_value: 120 }, 'metric')).toBe('until HR < 120 bpm')
    expect(targetText(steps[0], 'metric')).toBe('HR zone 2')
    expect(targetText(steps[1], 'metric')).toBe('3:53–4:10 /km')
    expect(targetText({ index: 0, duration: 'time', target: 'power', target_low: 90, target_high: 105, target_pct: true }, 'metric')).toBe('90–105 % FTP')
    expect(targetText({ index: 0, duration: 'time', target: 'open' }, 'metric')).toBe('')
  })

  it('names steps and unrolls the intensity strip', () => {
    expect(stepTitle(steps[0])).toBe('Warm up')
    expect(stepTitle({ index: 0, duration: 'reps', exercise: { category: 'squat', name: 'goblet_squat', number: 37 } })).toBe('Goblet squat')
    expect(prettyExercise('Child’s Pose')).toBe('Child’s Pose')
    const s = strip(steps)
    expect(s).toHaveLength(1 + 6 * 2 + 1)
    expect(s[0]).toEqual({ intensity: 'warmup', s: 600 })
    expect(s[2]).toEqual({ intensity: 'recovery', s: 90 })
  })
})
