// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import { LAYOUT_VERSION, MAX_CARDS, normalizeLayout, optionsFor } from './layout'
import { reduce } from './reducer'
import { cardByType, defaultLayout } from './registry'
import type { Layout } from './types'

const me = { server: { sync: true, tiles: false, version: 'dev' } } as Parameters<typeof defaultLayout>[0]
const fallback = () => defaultLayout(me)
const norm = (raw: unknown) => normalizeLayout(raw, fallback, cardByType)

describe('normalizeLayout', () => {
  it('falls back to the default for nothing or garbage', () => {
    expect(norm(null).layout).toEqual(fallback())
    expect(norm(null).changed).toBe(false)
    expect(norm({ v: 'x' }).layout.cards.length).toBe(fallback().cards.length)
    expect(norm([1, 2]).changed).toBe(true)
  })

  it('keeps unknown card types verbatim', () => {
    const odd = { id: 'w1', type: 'weather-radar', size: 'L', options: { city: 'x' }, colour: 'teal' }
    const n = norm({ v: 1, cards: [odd] })
    expect(n.layout.cards[0]).toEqual(odd)
    expect(n.changed).toBe(false)
  })

  it('snaps sizes, replaces duplicate ids, drops repeated singletons and broken entries', () => {
    const n = norm({ v: 1, cards: [
      { id: 'a', type: 'resting-hr', size: 'W', options: {} },
      { id: 'a', type: 'steps', size: 'S', options: [] },
      { id: 'd1', type: 'your-day', size: 'W', options: {} },
      { id: 'd2', type: 'your-day', size: 'W', options: {} },
      { nope: true },
    ] })
    const [rhr, steps, day] = n.layout.cards
    expect(rhr.size).toBe('M') // resting-hr allows S and M
    expect(steps.id).not.toBe('a')
    expect(steps.options).toEqual({})
    expect(day.id).toBe('d1')
    expect(n.layout.cards).toHaveLength(3)
    expect(n.changed).toBe(true)
  })

  it('shows a newer layout read-only and caps the card count', () => {
    expect(norm({ v: LAYOUT_VERSION + 1, cards: [] }).readOnly).toBe(true)
    const many = Array.from({ length: MAX_CARDS + 5 }, (_, i) => ({ id: 'c' + i, type: 'steps', size: 'S', options: {} }))
    expect(norm({ v: 1, cards: many }).layout.cards).toHaveLength(MAX_CARDS)
  })

  it('parses options with the card definition', () => {
    const def = cardByType.get('recent-activities')!
    expect(optionsFor(def, { id: 'r', type: 'recent-activities', size: 'L', options: { count: 99 } })).toEqual({ count: 6 })
  })
})

describe('reduce', () => {
  const base: Layout = { v: 1, cards: ['a', 'b', 'c'].map((id) => ({ id, type: 'steps', size: 'S' as const, options: {} })) }
  const ids = (l: Layout) => l.cards.map((c) => c.id).join('')

  it('moves, adds, removes, resizes and sets options without mutating', () => {
    expect(ids(reduce(base, { type: 'move', id: 'a', to: 2 }))).toBe('bca')
    expect(ids(reduce(base, { type: 'move', id: 'c', to: 0 }))).toBe('cab')
    expect(ids(reduce(base, { type: 'add', card: { id: 'd', type: 'sleep', size: 'M', options: {} }, index: 1 }))).toBe('adbc')
    expect(ids(reduce(base, { type: 'remove', id: 'b' }))).toBe('ac')
    expect(reduce(base, { type: 'resize', id: 'b', size: 'M' }).cards[1].size).toBe('M')
    expect(reduce(base, { type: 'setOptions', id: 'c', options: { n: 1 } }).cards[2].options).toEqual({ n: 1 })
    expect(ids(base)).toBe('abc')
  })

  it('returns the same layout when nothing changes', () => {
    expect(reduce(base, { type: 'move', id: 'a', to: 0 })).toBe(base)
    expect(reduce(base, { type: 'remove', id: 'zz' })).toBe(base)
    expect(reduce(base, { type: 'resize', id: 'a', size: 'S' })).toBe(base)
  })
})
