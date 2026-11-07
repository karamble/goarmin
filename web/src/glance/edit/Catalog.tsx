// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Search } from 'lucide-react'
import { useState } from 'react'
import type { Me } from '../../api/types'
import { fuzzyFilter } from '../../lib/fuzzy'
import { Sheet } from '../../ui/Sheet'
import { MAX_CARDS } from '../layout'
import { cards } from '../registry'
import { sizeName } from '../sizes'
import type { CardDef, Layout } from '../types'

const groups: { key: CardDef['category']; label: string }[] = [
  { key: 'health', label: 'Health' },
  { key: 'sleep', label: 'Sleep' },
  { key: 'training', label: 'Training' },
  { key: 'device', label: 'Watch' },
]

/** Catalog lists the cards that can be added, searchable and grouped. */
export function Catalog({ me, layout, onAdd, onClose }: { me: Me; layout: Layout; onAdd: (d: CardDef) => void; onClose: () => void }) {
  const [query, setQuery] = useState('')
  const placed = new Set(layout.cards.map((c) => c.type))
  const full = layout.cards.length >= MAX_CARDS
  const shown = fuzzyFilter(query, cards, (d) => `${d.title} ${d.description}`)
  const why = (d: CardDef) =>
    full ? `The glance holds at most ${MAX_CARDS} cards` :
    d.singleton && placed.has(d.type) ? 'Already on your glance' :
    d.requires === 'sync' && !me.server.sync ? 'Needs a server that syncs watches' : ''
  return (
    <Sheet title="Add a card" onClose={onClose}>
      <label className="relative mb-4 block">
        <span className="sr-only">Search cards</span>
        <Search size={16} className="absolute left-3.5 top-1/2 -translate-y-1/2 text-faint" aria-hidden />
        <input autoFocus value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search cards…" className="field !pl-10 text-[15px]" />
      </label>
      {groups.map((g) => {
        const list = shown.filter((d) => d.category === g.key)
        if (!list.length) return null
        return (
          <section key={g.key} className="mb-5">
            <h3 className="caps m-0 mb-2 !text-faint">{g.label}</h3>
            <ul className="m-0 grid list-none gap-2 p-0">
              {list.map((d) => {
                const no = why(d)
                return (
                  <li key={d.type} className="flex items-center gap-3 rounded-xl border border-border bg-panel px-3 py-2.5">
                    <d.icon size={18} strokeWidth={1.75} className="shrink-0 text-muted" aria-hidden />
                    <div className="min-w-0 flex-1">
                      <p className="m-0 text-[14px] font-medium">{d.title}</p>
                      <p className="m-0 text-[12px] text-muted">{no || d.description}</p>
                      <p className="m-0 mt-1 font-mono text-[10px] uppercase tracking-[.12em] text-faint">{d.sizes.map((s) => sizeName[s]).join(' · ')}</p>
                    </div>
                    <button type="button" disabled={!!no} onClick={() => onAdd(d)} aria-label={`Add ${d.title}`}
                      className="shrink-0 rounded-full border border-lime-dim px-3 py-1.5 text-[12px] font-bold text-lime hover:bg-lime hover:text-bg disabled:border-border disabled:text-faint disabled:hover:bg-transparent">
                      Add
                    </button>
                  </li>
                )
              })}
            </ul>
          </section>
        )
      })}
      {shown.length === 0 && <p className="m-0 py-6 text-center text-[14px] text-faint">No card matches “{query}”</p>}
    </Sheet>
  )
}
