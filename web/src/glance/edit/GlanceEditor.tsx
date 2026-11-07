// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import {
  closestCenter, DndContext, DragOverlay, KeyboardSensor, MouseSensor, TouchSensor, useSensor, useSensors,
  type Announcements, type DragEndEvent, type DragStartEvent,
} from '@dnd-kit/core'
import { rectSortingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { GripVertical, MoreHorizontal, Plus, RotateCcw, Settings2, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import type { Me } from '../../api/types'
import { HeaderActions } from '../../app/section/HeaderSlot'
import { useReducedMotion } from '../../charts'
import { Menu, type MenuItem } from '../../ui/Menu'
import { Confirm } from '../../ui/Sheet'
import { useToast } from '../../ui/Toast'
import { cardSize, cardTitle, GlanceCard, type CardView } from '../CardSlot'
import { gridClass } from '../GlancePage'
import { createCard } from '../layout'
import { cardByType, defaultLayout } from '../registry'
import { sizeClass, sizeName, type CardSize } from '../sizes'
import type { CardDef, PlacedCard } from '../types'
import type { GlanceLayout } from '../useGlanceLayout'
import { Catalog } from './Catalog'
import { CardOptions } from './CardOptions'

const saveText = { saved: 'Saved', pending: 'Saving…', saving: 'Saving…', error: 'Not saved' } as const

/** GlanceEditor arranges the glance: drag (mouse, touch, keyboard), resize, options, add, remove. */
export default function GlanceEditor({ g, me, view, onDone }: { g: GlanceLayout; me: Me; view: CardView; onDone: () => void }) {
  const toast = useToast()
  const reduced = useReducedMotion()
  const [catalog, setCatalog] = useState(false)
  const [resetting, setResetting] = useState(false)
  const [options, setOptions] = useState<string | null>(null)
  const [active, setActive] = useState<string | null>(null)
  const focusNext = useRef<string | null>(null)
  const cards = g.layout.cards
  const ids = cards.map((c) => c.id)

  // Page hotkeys (←/→, T, D…) stay off while arranging.
  useEffect(() => {
    document.body.dataset.editing = '1'
    return () => {
      delete document.body.dataset.editing
      delete document.body.dataset.dragging
    }
  }, [])

  // A card just added: bring it into view and put the keyboard on its grip.
  useEffect(() => {
    const id = focusNext.current
    if (!id) return
    const el = Array.from(document.querySelectorAll<HTMLElement>('[data-card-id]')).find((x) => x.dataset.cardId === id)
    if (!el) return
    focusNext.current = null
    el.scrollIntoView?.({ block: 'nearest', behavior: reduced ? 'auto' : 'smooth' })
    el.querySelector<HTMLElement>('[data-grip]')?.focus()
  }, [cards, reduced])

  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 6 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 250, tolerance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )

  const name = (id: string | number) => {
    const c = cards.find((x) => x.id === id)
    return c ? cardTitle(c) : 'card'
  }
  const pos = (id: string | number) => ids.indexOf(String(id)) + 1
  const announcements: Announcements = {
    onDragStart: ({ active }) => `Picked up ${name(active.id)}, position ${pos(active.id)} of ${ids.length}.`,
    onDragOver: ({ active, over }) => (over ? `${name(active.id)} is over position ${pos(over.id)} of ${ids.length}.` : `${name(active.id)} is outside the cards.`),
    onDragEnd: ({ active, over }) => (over ? `${name(active.id)} dropped at position ${pos(over.id)} of ${ids.length}.` : `${name(active.id)} dropped.`),
    onDragCancel: ({ active }) => `Moving ${name(active.id)} was cancelled.`,
  }

  const onDragStart = (e: DragStartEvent) => {
    setActive(String(e.active.id))
    document.body.dataset.dragging = '1'
  }
  const onDragEnd = (e: DragEndEvent) => {
    setActive(null)
    delete document.body.dataset.dragging
    if (e.over && e.active.id !== e.over.id) g.dispatch({ type: 'move', id: String(e.active.id), to: ids.indexOf(String(e.over.id)) })
  }

  const remove = (c: PlacedCard) => {
    const at = cards.findIndex((x) => x.id === c.id)
    g.dispatch({ type: 'remove', id: c.id })
    toast({ text: `Removed ${cardTitle(c)}`, action: { label: 'Undo', run: () => g.dispatch({ type: 'add', card: c, index: at }) } })
  }
  const add = (def: CardDef) => {
    const c = createCard(def)
    focusNext.current = c.id
    g.dispatch({ type: 'add', card: c })
    setCatalog(false)
  }
  const reset = () => {
    const before = g.layout
    g.dispatch({ type: 'reset', layout: defaultLayout(me) })
    toast({ text: 'The glance is back to the default cards', action: { label: 'Undo', run: () => g.dispatch({ type: 'replace', layout: before }) } })
  }

  const activeCard = active ? cards.find((c) => c.id === active) : undefined
  const optionsCard = options ? cards.find((c) => c.id === options) : undefined
  return (
    <>
      <HeaderActions>
        <span aria-live="polite" className={`font-mono text-[11px] uppercase tracking-[.14em] ${g.state === 'error' ? 'text-err' : 'text-faint'}`}>
          {saveText[g.state]}
        </span>
        <button type="button" onClick={() => setCatalog(true)}
          className="inline-flex h-9 items-center gap-2 rounded-full border border-border px-3.5 text-[13px] text-text hover:border-lime hover:text-lime">
          <Plus size={15} /> Add card
        </button>
        <button type="button" onClick={() => setResetting(true)}
          className="inline-flex h-9 items-center gap-2 rounded-full px-3 text-[13px] text-muted hover:text-text">
          <RotateCcw size={14} /> Reset
        </button>
        <button type="button" onClick={onDone}
          className="inline-flex h-9 items-center rounded-full border-2 border-lime bg-lime px-4 text-[13px] font-bold text-bg shadow-[0_0_24px_var(--lime-glow)]">
          Done
        </button>
      </HeaderActions>

      <p className="m-0 mb-4 text-[13px] text-muted">
        Drag a card by its grip (on a phone, hold it first), or focus the grip and use Space and the arrow keys. Changes save as you go.
      </p>

      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragStart={onDragStart} onDragEnd={onDragEnd}
        onDragCancel={() => { setActive(null); delete document.body.dataset.dragging }}
        accessibility={{
          announcements,
          screenReaderInstructions: { draggable: 'To move a card, press Space, then the arrow keys; Space drops it, Escape cancels.' },
        }}>
        <SortableContext items={ids} strategy={rectSortingStrategy}>
          <div className={gridClass}>
            {cards.map((c, i) => (
              <SortableCard key={c.id} c={c} index={i} count={cards.length} view={view} me={me}
                onMove={(to) => g.dispatch({ type: 'move', id: c.id, to })}
                onResize={(size) => g.dispatch({ type: 'resize', id: c.id, size })}
                onOptions={() => setOptions(c.id)} onRemove={() => remove(c)} />
            ))}
            <button type="button" onClick={() => setCatalog(true)}
              className="col-span-1 row-span-1 grid place-items-center rounded-[18px] border-2 border-dashed border-border text-muted transition-colors hover:border-lime-dim hover:text-lime md:col-span-2 xl:col-span-3">
              <span className="inline-flex items-center gap-2 text-[14px]"><Plus size={16} /> Add a card</span>
            </button>
          </div>
        </SortableContext>
        <DragOverlay dropAnimation={reduced ? null : undefined}>
          {activeCard && <DragPreview c={activeCard} />}
        </DragOverlay>
      </DndContext>

      {catalog && <Catalog me={me} layout={g.layout} onAdd={add} onClose={() => setCatalog(false)} />}
      {optionsCard && cardByType.get(optionsCard.type) && (
        <CardOptions c={optionsCard} def={cardByType.get(optionsCard.type)!}
          onChange={(o) => g.dispatch({ type: 'setOptions', id: optionsCard.id, options: o })} onClose={() => setOptions(null)} />
      )}
      {resetting && (
        <Confirm title="Reset the glance?" confirm="Reset" danger onConfirm={reset} onClose={() => setResetting(false)}>
          Your cards go back to the default set and order. You can undo it right after.
        </Confirm>
      )}
    </>
  )
}

function SortableCard({ c, index, count, view, me, onMove, onResize, onOptions, onRemove }: {
  c: PlacedCard
  index: number
  count: number
  view: CardView
  me: Me
  onMove: (to: number) => void
  onResize: (s: CardSize) => void
  onOptions: () => void
  onRemove: () => void
}) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id: c.id })
  const def = cardByType.get(c.type)
  const size = cardSize(c)
  const title = cardTitle(c)
  const sizes = def?.sizes ?? []
  const items: MenuItem[] = [
    ...(sizes.length > 1 ? sizes.map((s, i) => ({ label: sizeName[s], checked: s === size, run: () => onResize(s), group: i === 0 ? 'Size' : undefined })) : []),
    { label: 'Move to the top', run: () => onMove(0), disabled: index === 0, group: 'Order' },
    { label: 'Move earlier', run: () => onMove(index - 1), disabled: index === 0 },
    { label: 'Move later', run: () => onMove(index + 1), disabled: index === count - 1 },
  ]
  return (
    <div ref={setNodeRef} data-card-id={c.id} style={{ transform: CSS.Translate.toString(transform), transition }}
      className={`${sizeClass[size]} relative min-w-0 ${isDragging ? 'opacity-30' : ''}`}>
      <div inert className="pointer-events-none h-full opacity-55 [&_.card]:border-dashed">
        <GlanceCard c={c} view={view} me={me} editing />
      </div>
      <div className="absolute inset-x-2 top-2 z-10 flex items-center gap-0.5 rounded-xl border border-border bg-bg2/95 px-1 py-1 shadow-[0_8px_20px_rgba(0,0,0,.35)]">
        <button ref={setActivatorNodeRef} {...attributes} {...listeners} type="button" data-grip aria-label={`Move ${title}`}
          className="grid h-7 w-7 shrink-0 cursor-grab touch-none place-items-center rounded-lg text-muted hover:bg-panel2 hover:text-lime active:cursor-grabbing">
          <GripVertical size={16} />
        </button>
        <span className="caps min-w-0 flex-1 truncate !text-[10px] !text-text">{title}</span>
        {size !== 'S' && sizes.length > 1 && (
          <div role="radiogroup" aria-label={`Size of ${title}`} className="hidden shrink-0 rounded-lg bg-panel p-0.5 sm:flex">
            {sizes.map((s) => (
              <button key={s} type="button" role="radio" aria-checked={s === size} aria-label={sizeName[s]} title={sizeName[s]} onClick={() => onResize(s)}
                className={`h-6 min-w-6 rounded-md px-1 font-mono text-[10px] ${s === size ? 'bg-lime text-bg' : 'text-muted hover:text-text'}`}>
                {s}
              </button>
            ))}
          </div>
        )}
        {def?.fields && (
          <button type="button" onClick={onOptions} aria-label={`Options of ${title}`} title="Options"
            className="grid h-7 w-7 shrink-0 place-items-center rounded-lg text-muted hover:bg-panel2 hover:text-text">
            <Settings2 size={15} />
          </button>
        )}
        <Menu label={`More for ${title}`} icon={<MoreHorizontal size={16} />} items={items} />
        <button type="button" onClick={onRemove} aria-label={`Remove ${title}`} title="Remove"
          className="grid h-7 w-7 shrink-0 place-items-center rounded-lg text-muted hover:bg-red/10 hover:text-err">
          <X size={15} />
        </button>
      </div>
    </div>
  )
}

/** DragPreview follows the pointer while a card is moved. */
function DragPreview({ c }: { c: PlacedCard }) {
  const def = cardByType.get(c.type)
  const Icon = def?.icon
  return (
    <div className="card grid h-full w-full place-items-center border-lime-dim shadow-[0_24px_60px_rgba(0,0,0,.55),0_0_40px_rgba(200,245,66,.15)]">
      <span className="inline-flex items-center gap-2 caps !text-text">
        {Icon && <Icon size={16} className="text-lime" />} {cardTitle(c)}
      </span>
    </div>
  )
}
