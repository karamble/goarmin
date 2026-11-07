// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Puzzle } from 'lucide-react'
import { lazy, type ComponentType, type LazyExoticComponent } from 'react'
import type { Me } from '../api/types'
import { CardFrame, Empty } from './CardFrame'
import { optionsFor } from './layout'
import { cardByType } from './registry'
import { nearestSize, type CardSize } from './sizes'
import type { CardDef, CardProps, PlacedCard } from './types'

/** The day the cards show. */
export interface CardView {
  day: string
  today: string
  isToday: boolean
  tz: string
}

const loaded = new Map<string, LazyExoticComponent<ComponentType<CardProps>>>()
function component(def: CardDef) {
  let C = loaded.get(def.type)
  if (!C) {
    C = lazy(def.load)
    loaded.set(def.type, C)
  }
  return C
}

/** cardTitle is a placed card's title (a trend names its value; an unknown card its type). */
export function cardTitle(c: PlacedCard): string {
  const def = cardByType.get(c.type)
  if (!def) return 'Unknown card'
  return def.titleFor ? def.titleFor(optionsFor(def, c)) : def.title
}

/** cardSize is the size a placed card shows at. */
export function cardSize(c: PlacedCard): CardSize {
  const def = cardByType.get(c.type)
  return def ? nearestSize(c.size, def.sizes) : c.size
}

/** GlanceCard renders one placed card with its frame, or says why it cannot. */
export function GlanceCard({ c, view, me, editing }: { c: PlacedCard; view: CardView; me: Me; editing?: boolean }) {
  const def = cardByType.get(c.type)
  const size = cardSize(c)
  if (!def) {
    return (
      <CardFrame def={{ title: 'Unknown card', icon: Puzzle }} size={size} resetKey={view.day} editing={editing}>
        <Empty>A newer goarmin added this card ({c.type}). It is kept as it is.</Empty>
      </CardFrame>
    )
  }
  const title = def.titleFor ? def.titleFor(optionsFor(def, c)) : def.title
  if (def.requires === 'sync' && !me.server.sync) {
    return (
      <CardFrame def={def} size={size} resetKey={view.day} title={title} editing={editing}>
        <Empty>Needs a server that syncs watches (goarmin serve --with-sync).</Empty>
      </CardFrame>
    )
  }
  const C = component(def)
  return (
    <CardFrame def={def} size={size} resetKey={view.day} title={title} editing={editing}>
      <C id={c.id} size={size} options={optionsFor(def, c)} {...view} />
    </CardFrame>
  )
}
