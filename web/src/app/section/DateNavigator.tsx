// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useQuery } from '@tanstack/react-query'
import { CalendarDays, ChevronLeft, ChevronRight } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { request } from '../../api/client'
import { keys } from '../../api/keys'
import type { Day } from '../../api/types'
import { addDays, addMonths, label, periodRange, weekday, type Period, weekdayNames } from '../../lib/period'
import { useHotkeys } from '../hotkeys/useHotkeys'
import { usePeriod } from './usePeriod'

const short: Record<Period, string> = { day: 'D', week: 'W', month: 'M', year: 'Y', all: 'All' }
const long: Record<Period, string> = { day: 'day', week: 'week', month: 'month', year: 'year', all: 'all time' }

/** DateNavigator: ◀ date ▶, the periods the page allows, and Today. Keys: ←/→, T, D/W/M/Y. */
export function DateNavigator() {
  const p = usePeriod()
  const [open, setOpen] = useState(false)
  const stepping = p.period !== 'all'
  const keyMap: Record<string, () => void> = { t: p.toToday }
  if (stepping) {
    keyMap.ArrowLeft = () => p.step(-1)
    keyMap.ArrowRight = () => p.step(1)
  }
  for (const x of p.periods) if (x !== 'all') keyMap[short[x].toLowerCase()] = () => p.setPeriod(x)
  useHotkeys(keyMap, !open)

  return (
    <div className="flex flex-wrap items-center gap-2">
      {p.periods.length > 1 && (
        <div role="radiogroup" aria-label="Period" className="flex rounded-full border border-border bg-panel p-0.5">
          {p.periods.map((x) => (
            <button
              key={x}
              type="button"
              role="radio"
              aria-checked={p.period === x}
              title={`Show a ${long[x]}${x === 'all' ? '' : ` (${short[x]})`}`}
              onClick={() => p.setPeriod(x)}
              className={`min-w-8 rounded-full px-2.5 py-1 font-mono text-[11px] font-medium uppercase tracking-[.12em] transition-colors ${
                p.period === x ? 'bg-lime text-bg' : 'text-muted hover:text-text'
              }`}
            >
              {short[x]}
            </button>
          ))}
        </div>
      )}
      <div className="relative flex items-center rounded-full border border-border bg-panel">
        {stepping && (
          <button type="button" onClick={() => p.step(-1)} aria-label={`Previous ${long[p.period]}`} title="Previous (←)"
            className="grid h-9 w-9 place-items-center rounded-full text-muted hover:text-lime">
            <ChevronLeft size={18} />
          </button>
        )}
        <button
          type="button"
          onClick={() => setOpen(!open)}
          aria-haspopup="dialog"
          aria-expanded={open}
          className={`flex h-9 items-center gap-2 whitespace-nowrap font-mono text-[12px] uppercase tracking-[.1em] text-text hover:text-lime ${stepping ? 'px-1' : 'px-4'}`}
        >
          <CalendarDays size={15} className="text-faint" aria-hidden />
          <span aria-live="polite">{label(p.day, p.period)}</span>
        </button>
        {stepping && (
          <button type="button" onClick={() => p.step(1)} disabled={!p.canNext} aria-label={`Next ${long[p.period]}`} title="Next (→)"
            className="grid h-9 w-9 place-items-center rounded-full text-muted hover:text-lime disabled:pointer-events-none disabled:opacity-30">
            <ChevronRight size={18} />
          </button>
        )}
        {open && <MonthPopover day={p.day} today={p.today} latest={p.latest} onPick={(d) => { p.go(d); setOpen(false) }} onClose={() => setOpen(false)} />}
      </div>
      <button
        type="button"
        onClick={p.toToday}
        disabled={p.isToday}
        title="Today (T)"
        className="h-9 rounded-full border border-border px-3.5 font-mono text-[11px] uppercase tracking-[.14em] text-muted hover:border-lime hover:text-lime disabled:opacity-35 disabled:pointer-events-none"
      >
        Today
      </button>
    </div>
  )
}

const monthName = new Intl.DateTimeFormat(undefined, { month: 'long', year: 'numeric', timeZone: 'UTC' })

function hasData(d: Day): boolean {
  return (d.steps ?? 0) > 0 || d.sleep_score != null || Object.keys(d.values).length > 0
}

/** MonthPopover picks a day; days with data carry a dot, days after latest (today, unless the
 * section looks ahead) are off. */
function MonthPopover({ day, today, latest, onPick, onClose }: { day: string; today: string; latest: string; onPick: (d: string) => void; onClose: () => void }) {
  const [month, setMonth] = useState(day.slice(0, 8) + '01')
  const box = useRef<HTMLDivElement>(null)
  const r = periodRange(month, 'month')
  const data = useQuery({
    queryKey: keys.daily(r.from, r.to),
    queryFn: () => request<Day[]>('GET', `/daily?from=${r.from}&to=${r.to}`),
    enabled: r.from <= today,
  })
  const marked = new Set((data.data ?? []).filter(hasData).map((d) => d.day))

  useEffect(() => {
    box.current?.querySelector<HTMLButtonElement>('[aria-current="date"], [data-today]')?.focus()
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        onClose()
      }
    }
    const click = (e: MouseEvent) => {
      if (!box.current?.parentElement?.contains(e.target as Node)) onClose()
    }
    document.addEventListener('keydown', key)
    document.addEventListener('mousedown', click)
    return () => {
      document.removeEventListener('keydown', key)
      document.removeEventListener('mousedown', click)
    }
  }, [onClose])

  const cells: (string | null)[] = Array(weekday(r.from)).fill(null)
  for (let d = r.from; d <= r.to; d = addDays(d, 1)) cells.push(d)
  return (
    <div ref={box} role="dialog" aria-modal="true" aria-label="Choose a date"
      className="card-glass absolute right-0 top-full z-50 mt-2 w-[296px] p-4">
      <div className="mb-3 flex items-center justify-between">
        <button type="button" aria-label="Previous month" onClick={() => setMonth(addMonths(month, -1))}
          className="grid h-8 w-8 place-items-center rounded-full text-muted hover:text-lime"><ChevronLeft size={16} /></button>
        <span className="font-mono text-[12px] uppercase tracking-[.14em]">{monthName.format(new Date(month + 'T00:00:00Z'))}</span>
        <button type="button" aria-label="Next month" disabled={addMonths(month, 1) > latest} onClick={() => setMonth(addMonths(month, 1))}
          className="grid h-8 w-8 place-items-center rounded-full text-muted hover:text-lime disabled:opacity-30"><ChevronRight size={16} /></button>
      </div>
      <div className="grid grid-cols-7 gap-1 text-center">
        {weekdayNames('two').map((w) => <span key={w} className="pb-1 font-mono text-[10px] uppercase text-faint">{w}</span>)}
        {cells.map((d, i) =>
          d == null ? <span key={'e' + i} /> : (
            <button
              key={d}
              type="button"
              disabled={d > latest}
              onClick={() => onPick(d)}
              aria-current={d === day ? 'date' : undefined}
              data-today={d === today ? '' : undefined}
              aria-label={label(d, 'day') + (marked.has(d) ? ', has data' : '')}
              className={`relative grid h-9 place-items-center rounded-lg font-mono text-[12px] transition-colors disabled:opacity-25 ${
                d === day ? 'bg-lime text-bg' : d === today ? 'text-lime ring-1 ring-lime-dim' : 'text-text hover:bg-panel2'
              }`}
            >
              {Number(d.slice(8))}
              {marked.has(d) && <span aria-hidden className={`absolute bottom-1 h-1 w-1 rounded-full ${d === day ? 'bg-bg' : 'bg-lime-dim'}`} />}
            </button>
          ),
        )}
      </div>
    </div>
  )
}
