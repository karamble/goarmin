// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { SlidersHorizontal } from 'lucide-react'
import { lazy, Suspense } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useMe } from '../api/hooks/auth'
import type { Me } from '../api/types'
import { HeaderActions } from '../app/section/HeaderSlot'
import { usePeriod } from '../app/section/usePeriod'
import { Notice, Skeleton } from '../ui/kit'
import { cardSize, GlanceCard, type CardView } from './CardSlot'
import { sizeClass } from './sizes'
import type { Layout } from './types'
import { GettingStarted } from './GettingStarted'
import { useGlanceLayout } from './useGlanceLayout'

// The editor (and dnd-kit with it) loads only when someone customizes.
const GlanceEditor = lazy(() => import('./edit/GlanceEditor'))

export const gridClass = 'grid auto-rows-[148px] grid-cols-2 gap-3 sm:gap-4 md:grid-cols-6 xl:grid-cols-12'

/** GlanceGrid shows the cards, read-only. */
export function GlanceGrid({ layout, view, me }: { layout: Layout; view: CardView; me: Me }) {
  return (
    <div className={gridClass}>
      {layout.cards.map((c, i) => (
        <div key={c.id} data-card-id={c.id} className={`${sizeClass[cardSize(c)]} reveal min-w-0`} style={{ animationDelay: `${Math.min(i, 12) * 35}ms` }}>
          <GlanceCard c={c} view={view} me={me} />
        </div>
      ))}
    </div>
  )
}

/** GlancePage is the landing page: the user's cards for the day in focus; ?edit=1 arranges them. */
export default function GlancePage() {
  const me = useMe().data!
  const p = usePeriod()
  const g = useGlanceLayout(me)
  const [q, setQ] = useSearchParams()
  const editing = q.get('edit') === '1' && !g.readOnly && !g.loading
  const view: CardView = { day: p.day, today: p.today, isToday: p.isToday, tz: p.tz }
  const setEdit = (on: boolean) =>
    setQ((prev) => {
      const n = new URLSearchParams(prev)
      if (on) n.set('edit', '1')
      else n.delete('edit')
      return n
    })

  if (g.loading) {
    return (
      <div className={gridClass} aria-busy="true">
        {['M', 'M', 'M', 'W'].map((s, i) => <div key={i} className={sizeClass[s as 'M']}><Skeleton className="h-full w-full rounded-[18px]" /></div>)}
      </div>
    )
  }
  return (
    <>
      {!editing && !g.readOnly && (
        <HeaderActions>
          <button type="button" onClick={() => setEdit(true)} title="Arrange, resize, add and remove cards"
            className="inline-flex h-9 items-center gap-2 rounded-full border border-border px-3.5 font-mono text-[11px] uppercase tracking-[.14em] text-muted hover:border-lime hover:text-lime">
            <SlidersHorizontal size={14} /> Customize
          </button>
        </HeaderActions>
      )}
      {g.readOnly && (
        <div className="mb-4">
          <Notice>A newer goarmin saved this layout. It is shown as it is; customizing is off until this server is updated.</Notice>
        </div>
      )}
      {!editing && <GettingStarted me={me} />}
      {editing ? (
        <Suspense fallback={<GlanceGrid layout={g.layout} view={view} me={me} />}>
          <GlanceEditor g={g} me={me} view={view} onDone={() => { g.flush(); setEdit(false) }} />
        </Suspense>
      ) : (
        <GlanceGrid layout={g.layout} view={view} me={me} />
      )}
    </>
  )
}
