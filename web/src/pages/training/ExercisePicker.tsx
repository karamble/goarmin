// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useExercises, type ExerciseInfo } from '../../figures/api'
import { FigureView } from '../../figures/FigureView'
import { Sheet } from '../../ui/Sheet'
import { Spinner } from '../../ui/kit'
import { matches } from './Exercises'

/** The most rows the list shows at once; a search narrows it. */
const shownMax = 150

/** ExercisePicker chooses an exercise from Garmin's catalogue for a workout step: search,
 * a category, and the chosen exercise's figure before it is used. */
export function ExercisePicker({ current, onPick, onClose }: {
  current?: { category: string; number: number }
  onPick: (e: ExerciseInfo) => void
  onClose: () => void
}) {
  const cat = useExercises()
  const [q, setQ] = useState('')
  const [category, setCategory] = useState(current?.category ?? '')
  const [chosen, setChosen] = useState<ExerciseInfo | null>(null)
  const list = useMemo(() => (cat.data?.exercises ?? []).filter((e) => (!category || e.category === category) && matches(e, q)), [cat.data, category, q])
  const pick = chosen ?? (current ? cat.data?.exercises.find((e) => e.category === current.category && e.number === current.number) ?? null : null)
  return (
    <Sheet title="Choose an exercise" onClose={onClose}
      footer={<button type="button" disabled={!pick} onClick={() => pick && onPick(pick)}
        className="rounded-full bg-lime px-4 py-2 text-[14px] font-medium text-bg disabled:opacity-50">
        {pick ? `Use ${pick.title}` : 'Choose one'}
      </button>}>
      {!cat.data ? (
        <Spinner label="Loading the exercises" />
      ) : (
        <div className="grid gap-3">
          <label className="relative block">
            <span className="sr-only">Search exercises</span>
            <Search size={15} aria-hidden className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-faint" />
            <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search exercises" autoFocus
              className="w-full rounded-full border border-border bg-bg2 py-2 pl-9 pr-3 text-[14px] text-text placeholder:text-faint focus:border-lime focus:outline-none" />
          </label>
          <label className="grid gap-1 text-[13px] text-muted">
            Category
            <select value={category} onChange={(e) => setCategory(e.target.value)}
              className="rounded-lg border border-border bg-bg2 px-2.5 py-1.5 text-[14px] text-text focus:border-lime focus:outline-none">
              <option value="">Every category</option>
              {cat.data.categories.map((c) => <option key={c.name} value={c.name}>{c.title} ({c.count})</option>)}
            </select>
          </label>
          {pick && (
            <div className="rounded-xl border border-border p-2">
              <FigureView category={pick.category} number={pick.number} title={pick.title} />
            </div>
          )}
          <p className="m-0 text-[12px] text-faint" aria-live="polite">
            {list.length === 0 ? 'No exercise fits.' : list.length > shownMax ? `${shownMax} of ${list.length}; search to narrow it.` : `${list.length} exercises`}
          </p>
          <ul role="listbox" aria-label="Exercises" className="m-0 grid max-h-[40vh] list-none gap-1 overflow-y-auto p-0">
            {list.slice(0, shownMax).map((e) => {
              const on = pick?.key === e.key
              return (
                <li key={e.key} role="option" aria-selected={on}>
                  <button type="button" onClick={() => setChosen(e)}
                    className={`flex w-full items-baseline justify-between gap-3 rounded-lg px-3 py-2 text-left text-[14px] ${on ? 'bg-lime-fill text-lime' : 'text-text hover:bg-panel2'}`}>
                    <span className="min-w-0 truncate">{e.title}</span>
                    <span className="shrink-0 text-[12px] text-faint">{cat.data.categories.find((c) => c.name === e.category)?.title ?? e.category}</span>
                  </button>
                </li>
              )
            })}
          </ul>
        </div>
      )}
    </Sheet>
  )
}
