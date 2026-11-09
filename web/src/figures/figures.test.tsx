// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FIGURE_DISCLAIMER } from '../lib/disclaimer'
import { json, renderWith, stubApi } from '../test/fixtures'
import type { ExercisePose } from './api'
import { FigureDialog } from './FigureDialog'
import { qualityText } from './FigureView'

beforeEach(() => {
  // jsdom draws nothing: the view shows its no-WebGL note, and three.js never loads here.
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

const pose = (category: string, number: number, title: string): ExercisePose => ({
  exercise: { key: `${category}/${number}`, category, category_num: 1, number, name: title.toLowerCase(), title, quality: 'own', family: 'x', pose: 'p', mode: 'loop' },
  parts: ['pelvis'], keys: [[0, 1, 0, 0, 0, 0, 1], [0, 1, 0, 0, 0, 0, 1]],
})

describe('FigureDialog', () => {
  it('steps through the exercises with the buttons and the arrow keys, and closes with Esc', async () => {
    stubApi({ '/api/exercises/core/51': json(pose('core', 51, 'Cat Cow')), '/api/exercises/plank/43': json(pose('plank', 43, 'Plank')) })
    const onClose = vi.fn()
    renderWith(<FigureDialog onClose={onClose} items={[
      { category: 'core', number: 51, title: 'Cat Cow', detail: '10 reps' },
      { category: 'plank', number: 43, title: 'Plank', detail: '1:00' },
    ]} />)
    expect(screen.getByRole('dialog', { name: 'Cat Cow' })).toBeTruthy()
    expect(screen.getByText('10 reps')).toBeTruthy()
    expect(screen.getByText('1 of 2')).toBeTruthy()
    expect(screen.getByText(/cannot draw the 3D figure/)).toBeTruthy()
    expect(await screen.findByText(FIGURE_DISCLAIMER)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Next/ }))
    expect(screen.getByRole('dialog', { name: 'Plank' })).toBeTruthy()
    fireEvent.keyDown(document, { key: 'ArrowRight' })
    expect(screen.getByRole('dialog', { name: 'Cat Cow' })).toBeTruthy()
    fireEvent.keyDown(document, { key: 'ArrowLeft' })
    expect(screen.getByText('2 of 2')).toBeTruthy()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('has no stepper for one exercise', () => {
    stubApi({ '/api/exercises/core/51': json(pose('core', 51, 'Cat Cow')) })
    renderWith(<FigureDialog onClose={() => {}} items={[{ category: 'core', number: 51, title: 'Cat Cow' }]} />)
    expect(screen.queryByRole('navigation', { name: 'Exercises' })).toBeNull()
  })

  it('says where a figure comes from', () => {
    expect(qualityText('own')).toBe('Posed for this exercise')
    expect(qualityText('family')).toBe('Shared with similar movements')
    expect(qualityText('category')).toBe('Its category’s figure')
  })
})
