// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import {
  closestCenter, DndContext, DragOverlay, KeyboardSensor, MouseSensor, TouchSensor, useDroppable, useSensor, useSensors,
  type Announcements, type DragEndEvent,
} from '@dnd-kit/core'
import { SortableContext, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { GripVertical, MoreHorizontal, Repeat } from 'lucide-react'
import { useState, type KeyboardEvent, type MouseEvent } from 'react'
import type { WorkoutStep } from '../../../api/types'
import { useReducedMotion } from '../../../charts'
import { clock, type Units } from '../../../lib/format'
import { durationText, intensityTone, stepTitle, targetText } from '../../../lib/workout'
import { canHold, find, type Action, type Item, type RepeatItem, type StepItem } from '../../../lib/workoutEdit'
import { Menu, type MenuItem } from '../../../ui/Menu'

const LB = 0.45359237

/** stepLine says what a step does: its duration, target and weight. */
export function stepLine(s: WorkoutStep, u: Units): string {
  const w = s.weight_kg ? (u === 'imperial' ? `${Math.round((s.weight_kg / LB) * 10) / 10} lb` : `${s.weight_kg} kg`) : ''
  return [durationText(s, u), targetText(s, u), w].filter(Boolean).join(' · ')
}

/** itemTitle names an item for menus and announcements. */
export function itemTitle(it: Item): string {
  if (it.kind === 'repeat') return it.forS !== undefined ? `Rounds in ${clock(it.forS)}` : `Repeat ${it.rounds} times`
  return stepTitle(it.step)
}

/** flat is the items in the order the list shows them, blocks' steps after their block. */
export function flat(items: Item[]): Item[] {
  return items.flatMap<Item>((it) => (it.kind === 'repeat' ? [it, ...it.items] : [it]))
}

/** StepList shows the workout's items for arranging: drag by the grip (mouse, a held touch, or
 * Space and the arrow keys), select with a click (Shift extends), and every action in a row's
 * menu. A repeat block takes single steps dropped onto it. */
export function StepList({ items, selected, units, problems, dispatch, onOpen, onHint }: {
  items: Item[]
  selected: string[]
  units: Units
  /** Ids of items with a problem (marked). */
  problems: Set<string>
  dispatch: (a: Action) => void
  onOpen: (id: string) => void
  onHint: (text: string) => void
}) {
  const reduced = useReducedMotion()
  const [active, setActive] = useState<string | null>(null)
  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 6 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 250, tolerance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )
  const order = flat(items)
  const title = (id: string | number) => {
    const at = find(items, String(id))
    return at ? itemTitle(at.item) : 'step'
  }
  const pos = (id: string | number) => order.findIndex((x) => x.id === String(id)) + 1
  const announcements: Announcements = {
    onDragStart: ({ active }) => `Picked up ${title(active.id)}, step ${pos(active.id)} of ${order.length}.`,
    onDragOver: ({ active, over }) => (over ? `${title(active.id)} is over ${String(over.id).startsWith('in:') ? 'the repeat' : `step ${pos(over.id)}`}.` : `${title(active.id)} is outside the list.`),
    onDragEnd: ({ active, over }) => (over ? `${title(active.id)} dropped.` : `${title(active.id)} put back.`),
    onDragCancel: ({ active }) => `Moving ${title(active.id)} was cancelled.`,
  }
  const onDragEnd = (e: DragEndEvent) => {
    setActive(null)
    delete document.body.dataset.dragging
    if (!e.over || e.active.id === e.over.id) return
    const id = String(e.active.id)
    const over = String(e.over.id)
    const it = find(items, id)?.item
    if (!it) return
    let parent: string | null = null
    let index: number
    if (over.startsWith('in:')) {
      parent = over.slice(3)
      index = (items.find((x) => x.id === parent) as RepeatItem | undefined)?.items.length ?? 0
    } else {
      const at = find(items, over)
      if (!at) return
      parent = at.parent?.id ?? null
      index = at.index
    }
    if (parent && !canHold(it)) {
      onHint(it.kind === 'sets' ? 'A repeat holds single steps; sets are repeats themselves.' : 'A repeat cannot hold another repeat.')
      return
    }
    dispatch({ type: 'move', id, parent, index })
  }
  const activeItem = active ? find(items, active)?.item : undefined
  return (
    <DndContext sensors={sensors} collisionDetection={closestCenter}
      onDragStart={(e) => { setActive(String(e.active.id)); document.body.dataset.dragging = '1' }}
      onDragEnd={onDragEnd} onDragCancel={() => { setActive(null); delete document.body.dataset.dragging }}
      accessibility={{ announcements, screenReaderInstructions: { draggable: 'To move a step, press Space, then the arrow keys; Space drops it, Escape cancels.' } }}>
      <SortableContext items={items.map((x) => x.id)} strategy={verticalListSortingStrategy}>
        <ol aria-label="Steps" className="m-0 grid list-none gap-2 p-0">
          {items.map((it, i) => it.kind === 'repeat'
            ? <Block key={it.id} r={it} index={i} items={items} order={order} selected={selected} units={units} problems={problems} dispatch={dispatch} onOpen={onOpen} />
            : <Row key={it.id} it={it} index={i} items={items} order={order} selected={selected} units={units} problems={problems} dispatch={dispatch} onOpen={onOpen} />)}
        </ol>
      </SortableContext>
      <DragOverlay dropAnimation={reduced ? null : undefined}>
        {activeItem && (
          <div className="rounded-lg border border-lime-dim bg-panel px-3 py-2.5 text-[14px] text-text shadow-[0_18px_40px_rgba(0,0,0,.5)]">{itemTitle(activeItem)}</div>
        )}
      </DragOverlay>
    </DndContext>
  )
}

interface RowProps {
  index: number
  items: Item[]
  order: Item[]
  selected: string[]
  units: Units
  problems: Set<string>
  dispatch: (a: Action) => void
  onOpen: (id: string) => void
}

/** menuOf is everything a row's menu offers for an item. */
function menuOf(it: Item, items: Item[], dispatch: (a: Action) => void): MenuItem[] {
  const at = find(items, it.id)!
  const top = at.parent ? items.indexOf(at.parent) : at.index
  const near = (d: -1 | 1) => items[top + d]
  const out: MenuItem[] = [
    { label: 'Duplicate', run: () => dispatch({ type: 'duplicate', id: it.id }) },
    { label: 'Move up', run: () => dispatch({ type: 'moveBy', id: it.id, delta: -1 }), disabled: !at.parent && at.index === 0, group: 'Order' },
    { label: 'Move down', run: () => dispatch({ type: 'moveBy', id: it.id, delta: 1 }), disabled: !at.parent && at.index === items.length - 1 },
  ]
  if (at.parent) out.push({ label: 'Take out of the repeat', run: () => dispatch({ type: 'out', id: it.id }) })
  else if (canHold(it)) {
    const up = near(-1)
    const down = near(1)
    if (up?.kind === 'repeat') out.push({ label: 'Into the repeat above', run: () => dispatch({ type: 'into', id: it.id, repeat: up.id }) })
    if (down?.kind === 'repeat') out.push({ label: 'Into the repeat below', run: () => dispatch({ type: 'into', id: it.id, repeat: down.id }) })
    out.push({ label: 'Wrap in a repeat', run: () => dispatch({ type: 'wrap', ids: [it.id] }) })
  }
  if (it.kind === 'step' && it.step.exercise && !at.parent) out.push({ label: 'Make sets', run: () => dispatch({ type: 'toSets', id: it.id }), group: 'Shape' })
  if (it.kind === 'sets') out.push({ label: 'Make single steps', run: () => dispatch({ type: 'toSingle', id: it.id }), group: 'Shape' })
  if (it.kind === 'repeat') out.push({ label: 'Unwrap the repeat', run: () => dispatch({ type: 'unwrap', id: it.id }), group: 'Shape' })
  out.push({ label: it.kind === 'repeat' ? 'Delete the repeat and its steps' : 'Delete', danger: true, run: () => dispatch({ type: 'remove', id: it.id }) })
  return out
}

/** rowKeys: ↑↓ select, Alt+↑↓ move, Enter opens, Delete removes, Ctrl+D duplicates. */
function rowKeys(e: KeyboardEvent, it: Item, order: Item[], dispatch: (a: Action) => void, onOpen: (id: string) => void) {
  if (e.target !== e.currentTarget) return
  const i = order.findIndex((x) => x.id === it.id)
  const focus = (id: string) => document.querySelector<HTMLElement>(`[data-item="${id}"]`)?.focus()
  if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && e.altKey) {
    e.preventDefault()
    dispatch({ type: 'moveBy', id: it.id, delta: e.key === 'ArrowUp' ? -1 : 1 })
    requestAnimationFrame(() => focus(it.id))
  } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    e.preventDefault()
    const next = order[i + (e.key === 'ArrowUp' ? -1 : 1)]
    if (next) {
      dispatch({ type: 'select', ids: [next.id] })
      focus(next.id)
    }
  } else if (e.key === 'Enter') {
    e.preventDefault()
    onOpen(it.id)
  } else if (e.key === 'Delete' || e.key === 'Backspace') {
    e.preventDefault()
    const next = order[i + 1] ?? order[i - 1]
    dispatch({ type: 'remove', id: it.id })
    if (next) requestAnimationFrame(() => focus(next.id))
  } else if (e.key === 'd' && (e.ctrlKey || e.metaKey)) {
    e.preventDefault()
    dispatch({ type: 'duplicate', id: it.id })
  }
}

/** select picks an item; Shift picks the range of top-level items to it (to wrap them). */
function select(e: MouseEvent, it: Item, items: Item[], selected: string[], dispatch: (a: Action) => void) {
  const first = selected[0]
  if (e.shiftKey && first && first !== it.id) {
    const a = items.findIndex((x) => x.id === first)
    const b = items.findIndex((x) => x.id === it.id)
    if (a >= 0 && b >= 0) {
      const [lo, hi] = a < b ? [a, b] : [b, a]
      dispatch({ type: 'select', ids: [first, ...items.slice(lo, hi + 1).map((x) => x.id).filter((x) => x !== first)] })
      return
    }
  }
  dispatch({ type: 'select', ids: [it.id] })
}

function Row({ it, index, items, order, selected, units, problems, dispatch, onOpen }: RowProps & { it: StepItem | Exclude<Item, RepeatItem> }) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id: it.id })
  const s = it.step
  const on = selected.includes(it.id)
  const line = it.kind === 'sets'
    ? [`${it.count} ${it.count === 1 ? 'set' : 'sets'} × ${stepLine(s, units)}`, it.rest ? `rest ${it.rest.duration === 'time' ? clock(it.rest.duration_value ?? 0) : 'until Lap'}` : ''].filter(Boolean).join(' · ')
    : stepLine(s, units)
  return (
    <li ref={setNodeRef} style={{ transform: CSS.Translate.toString(transform), transition }} className={isDragging ? 'opacity-30' : ''}>
      <div data-item={it.id} tabIndex={0} role="button" aria-pressed={on} aria-label={`${index + 1}. ${itemTitle(it)}, ${line}`}
        onClick={(e) => { select(e, it, items, selected, dispatch); if (!e.shiftKey) onOpen(it.id) }}
        onKeyDown={(e) => rowKeys(e, it, order, dispatch, onOpen)}
        className={`flex cursor-pointer items-center gap-2 rounded-lg border px-2 py-2 outline-none focus-visible:ring-2 focus-visible:ring-lime ${on ? 'border-lime-dim bg-lime-fill' : 'border-transparent bg-bg2 hover:border-border'} ${problems.has(it.id) ? '!border-red/60' : ''}`}>
        <button ref={setActivatorNodeRef} {...attributes} {...listeners} type="button" aria-label={`Move ${itemTitle(it)}`} onClick={(e) => e.stopPropagation()}
          className="grid h-8 w-7 shrink-0 cursor-grab touch-none place-items-center rounded-md text-faint hover:text-lime active:cursor-grabbing">
          <GripVertical size={16} />
        </button>
        <span aria-hidden className="h-2.5 w-2.5 shrink-0 rounded-full" style={{ background: intensityTone(s.intensity) }} />
        <div className="min-w-0 flex-1">
          <p className="m-0 truncate text-[14px] text-text">{itemTitle(it)}</p>
          <p className="m-0 truncate text-[12px] text-muted">{line}{s.notes ? ` · ${s.notes}` : ''}</p>
        </div>
        <span onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
          <Menu label={`More for ${itemTitle(it)}`} icon={<MoreHorizontal size={16} />} items={menuOf(it, items, dispatch)} />
        </span>
      </div>
    </li>
  )
}

function Block({ r, index, items, order, selected, units, problems, dispatch, onOpen }: RowProps & { r: RepeatItem }) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id: r.id })
  const drop = useDroppable({ id: `in:${r.id}` })
  const on = selected.includes(r.id)
  return (
    <li ref={setNodeRef} style={{ transform: CSS.Translate.toString(transform), transition }} className={isDragging ? 'opacity-30' : ''}>
      <div className={`rounded-xl border p-2 ${on ? 'border-lime-dim' : 'border-border'} ${problems.has(r.id) ? '!border-red/60' : ''}`}>
        <div data-item={r.id} tabIndex={0} role="button" aria-pressed={on} aria-label={`${index + 1}. ${itemTitle(r)}, ${r.items.length} steps`}
          onClick={(e) => { select(e, r, items, selected, dispatch); if (!e.shiftKey) onOpen(r.id) }}
          onKeyDown={(e) => rowKeys(e, r, order, dispatch, onOpen)}
          className="flex cursor-pointer items-center gap-2 rounded-lg px-1 py-1 outline-none focus-visible:ring-2 focus-visible:ring-lime">
          <button ref={setActivatorNodeRef} {...attributes} {...listeners} type="button" aria-label={`Move ${itemTitle(r)}`} onClick={(e) => e.stopPropagation()}
            className="grid h-8 w-7 shrink-0 cursor-grab touch-none place-items-center rounded-md text-faint hover:text-lime active:cursor-grabbing">
            <GripVertical size={16} />
          </button>
          <Repeat size={14} aria-hidden className="text-lime" />
          <span className="flex-1 font-mono text-[11px] uppercase tracking-[.14em] text-lime">{itemTitle(r)}</span>
          <span onClick={(e) => e.stopPropagation()} onKeyDown={(e) => e.stopPropagation()}>
            <Menu label={`More for ${itemTitle(r)}`} icon={<MoreHorizontal size={16} />} items={menuOf(r, items, dispatch)} />
          </span>
        </div>
        <SortableContext items={r.items.map((c) => c.id)} strategy={verticalListSortingStrategy}>
          <ol aria-label={itemTitle(r)} ref={drop.setNodeRef}
            className={`m-0 mt-1 grid min-h-10 list-none gap-2 rounded-lg p-0 ${drop.isOver ? 'bg-lime-fill' : ''}`}>
            {r.items.map((c, i) => <Row key={c.id} it={c} index={i} items={items} order={order} selected={selected} units={units} problems={problems} dispatch={dispatch} onOpen={onOpen} />)}
            {r.items.length === 0 && <li className="grid place-items-center rounded-lg border border-dashed border-border py-3 text-[13px] text-faint">Drop steps here</li>}
          </ol>
        </SortableContext>
      </div>
    </li>
  )
}
