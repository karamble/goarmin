// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Search } from 'lucide-react'
import { useMemo, useState, type KeyboardEvent } from 'react'
import { useExercises, type ExerciseInfo } from '../../../figures/api'
import { FigureView } from '../../../figures/FigureView'
import { Sheet } from '../../../ui/Sheet'
import { Spinner } from '../../../ui/kit'
import { matches } from '../Exercises'
import { fieldClass } from './fields'

/** The most rows the picker lists at once; a search narrows the rest. */
const SHOWN = 200

/** ExercisePicker finds an exercise in Garmin's catalogue (1,846, each with its 3D figure):
 * search and category, this workout's exercises first, the highlighted one's figure. Add closes
 * it; Add and keep picking does not (when adding). */
export function ExercisePicker({ replacing, used, onPick, onClose }: {
  /** Changing a step's exercise rather than adding one. */
  replacing: boolean
  /** The keys (category/number) of the exercises the workout has, shown first. */
  used: string[]
  onPick: (e: ExerciseInfo, keepOpen: boolean) => void
  onClose: () => void
}) {
  const cat = useExercises()
  const [q, setQ] = useState('')
  const [category, setCategory] = useState('')
  const [at, setAt] = useState(0)
  const list = useMemo(() => {
    const all = cat.data?.exercises ?? []
    const hits = all.filter((e) => (!category || e.category === category) && (!q.trim() || matches(e, q)))
    const rank = new Map(used.map((k, i) => [k, i]))
    return hits.slice().sort((a, b) => (rank.get(a.key) ?? 1e9) - (rank.get(b.key) ?? 1e9))
  }, [cat.data, category, q, used])
  const shown = list.slice(0, SHOWN)
  const cur = shown[Math.min(at, shown.length - 1)]
  const keys = (e: KeyboardEvent) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      setAt((i) => Math.max(0, Math.min(shown.length - 1, i + (e.key === 'ArrowDown' ? 1 : -1))))
    } else if (e.key === 'Enter' && cur) {
      e.preventDefault()
      onPick(cur, e.shiftKey && !replacing)
    }
  }
  return (
    <Sheet title={replacing ? 'Change the exercise' : 'Add an exercise'} onClose={onClose}
      footer={
        <div className="flex flex-wrap gap-2">
          {!replacing && (
            <button type="button" disabled={!cur} onClick={() => cur && onPick(cur, true)}
              className="rounded-full border border-border px-4 py-2 text-[14px] text-text hover:border-lime disabled:opacity-50">
              Add and keep picking
            </button>
          )}
          <button type="button" disabled={!cur} onClick={() => cur && onPick(cur, false)}
            className="rounded-full bg-lime px-4 py-2 text-[14px] font-medium text-bg disabled:opacity-50">
            {replacing ? 'Use it' : 'Add'}
          </button>
        </div>
      }>
      {!cat.data ? <Spinner /> : (
        <div className="grid gap-3">
          <label className="relative block">
            <span className="sr-only">Search exercises</span>
            <Search size={15} aria-hidden className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-faint" />
            <input type="search" value={q} autoFocus placeholder="Search 1,846 exercises" onKeyDown={keys}
              onChange={(e) => { setQ(e.target.value); setAt(0) }} className={`${fieldClass} pl-9`}
              role="combobox" aria-expanded aria-controls="picker-list" aria-activedescendant={cur ? `pick-${cur.key}` : undefined} />
          </label>
          <select aria-label="Category" value={category} onChange={(e) => { setCategory(e.target.value); setAt(0) }} className={fieldClass}>
            <option value="">All categories</option>
            {cat.data.categories.map((c) => <option key={c.name} value={c.name}>{c.title} ({c.count})</option>)}
          </select>
          {cur && <FigureView category={cur.category} number={cur.number} title={cur.title} />}
          <p className="m-0 text-[12px] text-faint" aria-live="polite">
            {list.length === 0 ? 'No exercise fits.' : list.length > SHOWN ? `${SHOWN} of ${list.length}: search to narrow them` : `${list.length} exercises`}
            {!replacing && ' · Shift+Enter adds and keeps picking'}
          </p>
          <ul id="picker-list" role="listbox" aria-label="Exercises" className="m-0 grid max-h-[40vh] list-none gap-0.5 overflow-auto p-0">
            {shown.map((e, i) => (
              <li key={e.key} id={`pick-${e.key}`} role="option" aria-selected={e === cur}
                onClick={() => setAt(i)} onDoubleClick={() => onPick(e, false)}
                className={`cursor-pointer rounded-lg px-3 py-2 text-[14px] ${e === cur ? 'bg-lime-fill text-lime' : 'text-text hover:bg-panel2'}`}>
                {e.title}
                <span className="ml-2 text-[12px] text-faint">{e.category.replace(/_/g, ' ')}{used.includes(e.key) ? ' · in this workout' : ''}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </Sheet>
  )
}
