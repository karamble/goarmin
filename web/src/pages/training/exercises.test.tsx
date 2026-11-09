// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ExerciseInfo } from '../../figures/api'
import { json, renderWith, stubApi } from '../../test/fixtures'
import Exercises, { matches } from './Exercises'

beforeEach(() => {
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue(null)
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

const ex = (category: string, number: number, title: string, quality: ExerciseInfo['quality'] = 'family'): ExerciseInfo => ({
  key: `${category}/${number}`, category, category_num: 1, number, name: title.toLowerCase().replace(/ /g, '_'), title, quality, family: 'f', pose: 'p', mode: 'hold',
})
const catalog = {
  categories: [{ name: 'core', number: 5, title: 'Core', count: 2 }, { name: 'plank', number: 19, title: 'Plank', count: 1 }],
  exercises: [ex('core', 51, 'Cat Cow', 'own'), ex('core', 72, 'The Hundred'), ex('plank', 43, 'Plank', 'own')],
}

describe('Exercises', () => {
  it('lists the catalogue by category, searches it and shows the picked exercise', async () => {
    stubApi({ '/api/exercises': json(catalog), '/api/exercises/plank/43': json({ exercise: catalog.exercises[2], parts: [], keys: [[0, 1, 0]] }) })
    renderWith(<Exercises />, { path: '/training/exercises' })
    expect(await screen.findByText('3 exercises')).toBeTruthy()
    expect(screen.getByRole('heading', { level: 3, name: 'Core' })).toBeTruthy()
    expect(screen.getByRole('heading', { level: 2, name: 'Cat Cow' })).toBeTruthy() // the first one until one is picked
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search exercises' }), { target: { value: 'plank' } })
    expect(screen.getByText('1 of 3')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /^Plank/ }))
    expect(screen.getByRole('heading', { level: 2, name: 'Plank' })).toBeTruthy()
    expect(screen.getByRole('button', { name: /^Plank/ }).getAttribute('aria-current')).toBe('true')
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search exercises' }), { target: { value: 'nothing like this' } })
    expect(screen.getByText('No exercise matches.')).toBeTruthy()
  })

  it('filters by category', async () => {
    stubApi({ '/api/exercises': json(catalog) })
    renderWith(<Exercises />, { path: '/training/exercises?category=plank' })
    expect(await screen.findByText('1 of 3')).toBeTruthy()
    const list = screen.getByRole('list')
    expect(within(list).getAllByRole('button')).toHaveLength(1)
    expect(within(list).getByRole('button', { name: 'Plank, posed for this exercise' })).toBeTruthy()
  })

  it('matches every word of a search', () => {
    expect(matches(catalog.exercises[1], 'hundred core')).toBe(true)
    expect(matches(catalog.exercises[1], 'hundred plank')).toBe(false)
  })
})
