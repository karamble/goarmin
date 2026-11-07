// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { LucideIcon } from 'lucide-react'
import type { ComponentType } from 'react'
import type { CardSize } from './sizes'

/** What every card gets: the day in focus and how big it is. */
export interface CardProps<O = Record<string, unknown>> {
  id: string
  size: CardSize
  options: O
  day: string
  today: string
  isToday: boolean
  tz: string
}

/** An option a card offers in the editor. */
export type OptionField =
  | { key: string; label: string; kind: 'select'; choices: { value: string; label: string }[] }
  | { key: string; label: string; kind: 'toggle' }

export interface CardDef<O = Record<string, unknown>> {
  type: string
  title: string
  description: string
  icon: LucideIcon
  category: 'health' | 'sleep' | 'training' | 'device'
  sizes: CardSize[]
  defaultSize: CardSize
  defaultOptions: O
  /** The options the editor offers. */
  fields?: OptionField[]
  /** parseOptions turns stored options (anything) into valid ones; default: defaultOptions. */
  parseOptions?: (raw: Record<string, unknown>) => O
  /** The card's title when it depends on its options (a trend names its value). */
  titleFor?: (o: O) => string
  /** Where the card's title links to. */
  to?: string
  requires?: 'sync' | 'tiles'
  singleton?: boolean
  load: () => Promise<{ default: ComponentType<CardProps<O>> }>
}

/**
 * A card placed on the glance. Options stay opaque here; each card parses its own. A card of a
 * type this version does not know keeps every stored field, so saving never loses it.
 */
export interface PlacedCard {
  id: string
  type: string
  size: CardSize
  options: Record<string, unknown>
  [extra: string]: unknown
}

export interface Layout {
  v: number
  cards: PlacedCard[]
}
