// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { allSizes, nearestSize, type CardSize } from './sizes'
import type { CardDef, Layout, PlacedCard } from './types'

/** The layout format this version writes. */
export const LAYOUT_VERSION = 1

/** At most this many cards (the server allows 60, so a newer client has room). */
export const MAX_CARDS = 40

/** newId is a short random card id (getRandomValues works without a secure context). */
export function newId(type: string): string {
  const b = new Uint8Array(4)
  crypto.getRandomValues(b)
  return `${type}-${Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')}`
}

/** createCard places a new card of def with its defaults. */
export function createCard(def: CardDef): PlacedCard {
  return { id: newId(def.type), type: def.type, size: def.defaultSize, options: { ...(def.defaultOptions as object) } }
}

// migrations[v] turns a layout of version v into version v+1. None yet: v1 is the first.
const migrations: Record<number, (l: { v: number; cards: unknown[] }) => { v: number; cards: unknown[] }> = {}

export interface Normalized {
  layout: Layout
  /** Written by a newer goarmin: shown, but never saved over. */
  readOnly: boolean
  /** Normalizing changed something (ids, sizes, too many cards, an older version). */
  changed: boolean
}

const isObject = (x: unknown): x is Record<string, unknown> => typeof x === 'object' && x !== null && !Array.isArray(x)

/**
 * normalizeLayout makes a stored layout safe to show: anything unreadable falls back to the
 * default, older versions are migrated step by step, sizes snap to what the card allows,
 * duplicate or missing ids are replaced, repeated singletons dropped, and cards of unknown
 * types are kept as they are.
 */
export function normalizeLayout(raw: unknown, fallback: () => Layout, defs: Map<string, CardDef>): Normalized {
  if (!isObject(raw) || !Number.isInteger(raw.v) || (raw.v as number) < 1 || !Array.isArray(raw.cards)) {
    return { layout: fallback(), readOnly: false, changed: raw != null }
  }
  let l = { v: raw.v as number, cards: raw.cards as unknown[] }
  let changed = false
  while (l.v < LAYOUT_VERSION) {
    const m = migrations[l.v]
    l = m ? m(l) : { ...l, v: l.v + 1 }
    changed = true
  }
  const readOnly = l.v > LAYOUT_VERSION
  const seen = new Set<string>()
  const singles = new Set<string>()
  const cards: PlacedCard[] = []
  for (const c of l.cards) {
    if (!isObject(c) || typeof c.type !== 'string' || !c.type) {
      changed = true
      continue
    }
    const def = defs.get(c.type)
    if (def?.singleton) {
      if (singles.has(def.type)) {
        changed = true
        continue
      }
      singles.add(def.type)
    }
    let id = typeof c.id === 'string' && c.id ? c.id : ''
    if (!id || seen.has(id)) {
      id = newId(c.type)
      changed = true
    }
    seen.add(id)
    const stored = allSizes.includes(c.size as CardSize) ? (c.size as CardSize) : undefined
    let size: CardSize = stored ?? def?.defaultSize ?? 'M'
    if (def) size = nearestSize(size, def.sizes)
    if (size !== c.size) changed = true
    const options = isObject(c.options) ? c.options : {}
    if (options !== c.options) changed = true
    cards.push({ ...c, id, type: c.type, size, options })
    if (cards.length === MAX_CARDS) {
      changed ||= l.cards.length > MAX_CARDS
      break
    }
  }
  return { layout: { v: readOnly ? l.v : LAYOUT_VERSION, cards }, readOnly, changed }
}

/** optionsFor parses a card's stored options with its definition. */
export function optionsFor(def: CardDef, c: PlacedCard): Record<string, unknown> {
  const defaults = def.defaultOptions as Record<string, unknown>
  return def.parseOptions ? (def.parseOptions({ ...defaults, ...c.options }) as Record<string, unknown>) : { ...defaults, ...c.options }
}
