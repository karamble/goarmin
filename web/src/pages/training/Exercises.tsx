// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Search } from 'lucide-react'
import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useExercises, type ExerciseInfo } from '../../figures/api'
import { FigureView } from '../../figures/FigureView'
import { Loaded } from '../health/parts'

/** matches tells whether an exercise fits a search: every word in its title, name or category. */
export function matches(e: ExerciseInfo, q: string): boolean {
  const hay = `${e.title} ${e.name.replace(/_/g, ' ')} ${e.category.replace(/_/g, ' ')}`.toLowerCase()
  return q.toLowerCase().split(/\s+/).filter(Boolean).every((w) => hay.includes(w))
}

/**
 * Exercises is Garmin's exercise catalogue (every exercise a watch workout can name) with each
 * one's 3D figure: search, filter by category, pick one to see it move.
 */
export default function Exercises() {
  const cat = useExercises()
  const [params, setParams] = useSearchParams()
  const q = params.get('q') ?? ''
  const category = params.get('category') ?? ''
  const selected = params.get('ex') ?? ''
  const set = (k: string, v: string) => {
    const next = new URLSearchParams(params)
    if (v) next.set(k, v)
    else next.delete(k)
    setParams(next, { replace: true })
  }
  const list = useMemo(() => {
    const all = cat.data?.exercises ?? []
    return all.filter((e) => (!category || e.category === category) && (!q || matches(e, q)))
  }, [cat.data, category, q])
  return (
    <Loaded q={[cat]}>
      {() => {
        const data = cat.data!
        const titles = Object.fromEntries(data.categories.map((c) => [c.name, c.title]))
        const current = data.exercises.find((e) => e.key === selected) ?? list[0]
        const groups: [string, ExerciseInfo[]][] = []
        for (const e of list) {
          const g = groups[groups.length - 1]
          if (g && g[0] === e.category) g[1].push(e)
          else groups.push([e.category, [e]])
        }
        return (
          <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5 lg:grid-cols-[minmax(0,360px)_minmax(0,1fr)]">
            <section className="card order-2 flex min-w-0 flex-col p-4 lg:order-1 lg:max-h-[calc(100dvh-180px)]">
              <label className="relative block">
                <span className="sr-only">Search exercises</span>
                <Search size={15} aria-hidden className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-faint" />
                <input type="search" value={q} onChange={(e) => set('q', e.target.value)} placeholder="Search exercises"
                  className="w-full rounded-full border border-border bg-bg2 py-2 pl-9 pr-3 text-[14px] text-text placeholder:text-faint focus:border-lime focus:outline-none" />
              </label>
              <label className="mt-2 block">
                <span className="sr-only">Category</span>
                <select value={category} onChange={(e) => set('category', e.target.value)}
                  className="w-full rounded-full border border-border bg-bg2 px-3 py-2 text-[14px] text-text focus:border-lime focus:outline-none">
                  <option value="">All categories ({data.exercises.length.toLocaleString()})</option>
                  {data.categories.map((c) => <option key={c.name} value={c.name}>{c.title} ({c.count})</option>)}
                </select>
              </label>
              <p className="m-0 mt-3 text-[12px] text-muted" aria-live="polite">
                {list.length === data.exercises.length ? `${list.length.toLocaleString()} exercises` : `${list.length.toLocaleString()} of ${data.exercises.length.toLocaleString()}`}
              </p>
              <div className="mt-2 min-h-0 flex-1 overflow-y-auto pr-1">
                {list.length === 0 && <p className="m-0 py-6 text-center text-[13px] text-muted">No exercise matches.</p>}
                {groups.map(([c, items]) => (
                  <div key={c} className="mb-3">
                    {!category && <h3 className="m-0 mb-1 font-mono text-[11px] uppercase tracking-[.14em] text-faint">{titles[c] ?? c}</h3>}
                    <ul className="m-0 grid list-none gap-0.5 p-0">
                      {items.map((e) => (
                        <li key={e.key}>
                          <button type="button" onClick={() => set('ex', e.key)} aria-current={current?.key === e.key ? 'true' : undefined}
                            className={`flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-[14px] ${current?.key === e.key ? 'bg-bg2 text-lime' : 'text-text hover:bg-bg2'}`}>
                            <span className="min-w-0 flex-1 truncate">{e.title}</span>
                            {e.quality === 'own' && <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-lime" title="Posed for this exercise"><span className="sr-only">, posed for this exercise</span></span>}
                          </button>
                        </li>
                      ))}
                    </ul>
                  </div>
                ))}
              </div>
            </section>
            <section className="card order-1 min-w-0 self-start p-4 sm:p-5 lg:sticky lg:top-[150px] lg:order-2">
              {current ? (
                <>
                  <p className="m-0 font-mono text-[11px] uppercase tracking-[.14em] text-muted">{titles[current.category] ?? current.category} · {current.number}</p>
                  <h2 className="m-0 mb-3 mt-1 font-display text-[24px] uppercase leading-tight tracking-[.02em]">{current.title}</h2>
                  <FigureView category={current.category} number={current.number} title={current.title} />
                </>
              ) : (
                <p className="m-0 py-10 text-center text-[13px] text-muted">Pick an exercise to see its figure.</p>
              )}
            </section>
          </div>
        )
      }}
    </Loaded>
  )
}
