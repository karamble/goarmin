// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { MAX_CARDS } from './layout'
import type { CardSize } from './sizes'
import type { Layout, PlacedCard } from './types'

export type Action =
  | { type: 'add'; card: PlacedCard; index?: number }
  | { type: 'remove'; id: string }
  | { type: 'move'; id: string; to: number }
  | { type: 'resize'; id: string; size: CardSize }
  | { type: 'setOptions'; id: string; options: Record<string, unknown> }
  | { type: 'reset'; layout: Layout }
  | { type: 'replace'; layout: Layout }

/** reduce applies one edit; it never mutates l and returns l itself when nothing changes. */
export function reduce(l: Layout, a: Action): Layout {
  switch (a.type) {
    case 'add': {
      if (l.cards.length >= MAX_CARDS || l.cards.some((c) => c.id === a.card.id)) return l
      const cards = [...l.cards]
      const at = a.index == null ? cards.length : Math.max(0, Math.min(a.index, cards.length))
      cards.splice(at, 0, a.card)
      return { ...l, cards }
    }
    case 'remove': {
      const cards = l.cards.filter((c) => c.id !== a.id)
      return cards.length === l.cards.length ? l : { ...l, cards }
    }
    case 'move': {
      const from = l.cards.findIndex((c) => c.id === a.id)
      const to = Math.max(0, Math.min(a.to, l.cards.length - 1))
      if (from < 0 || from === to) return l
      const cards = [...l.cards]
      const [c] = cards.splice(from, 1)
      cards.splice(to, 0, c)
      return { ...l, cards }
    }
    case 'resize':
      return update(l, a.id, (c) => (c.size === a.size ? c : { ...c, size: a.size }))
    case 'setOptions':
      return update(l, a.id, (c) => ({ ...c, options: { ...c.options, ...a.options } }))
    case 'reset':
    case 'replace':
      return a.layout
  }
}

function update(l: Layout, id: string, fn: (c: PlacedCard) => PlacedCard): Layout {
  let hit = false
  const cards = l.cards.map((c) => {
    if (c.id !== id) return c
    const n = fn(c)
    hit = n !== c
    return n
  })
  return hit ? { ...l, cards } : l
}
