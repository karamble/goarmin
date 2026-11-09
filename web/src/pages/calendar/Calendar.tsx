// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Check, SlidersHorizontal } from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type DragEvent, type KeyboardEvent } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { useCalendar, usePlanActions } from '../../api/hooks/calendar'
import { usePrefs, useSavePrefs } from '../../api/hooks/prefs'
import type { CalendarDay, CalendarKind } from '../../api/types'
import { HeaderActions } from '../../app/section/HeaderSlot'
import { usePeriod } from '../../app/section/usePeriod'
import { dayEntries, fitBars, kindInfo, kinds, monthCells, planStateText, weekCells, type Bar, type Named } from '../../lib/calendar'
import type { Units } from '../../lib/format'
import { addDays, weekdayNames } from '../../lib/period'
import { Menu } from '../../ui/Menu'
import { Sheet } from '../../ui/Sheet'
import { useToast } from '../../ui/Toast'
import { DayPanel } from './DayPanel'
import { PlanPicker } from './PlanPicker'

const longDay = new Intl.DateTimeFormat(undefined, { weekday: 'long', day: 'numeric', month: 'long', timeZone: 'UTC' })
const dateOf = (d: string) => new Date(`${d}T00:00:00Z`)

/** useWide tells a desktop layout (the day panel beside the grid) from a phone's. */
function useWide(): boolean {
  const query = '(min-width: 1024px)'
  const [wide, setWide] = useState(() => typeof window !== 'undefined' && !!window.matchMedia?.(query).matches)
  useEffect(() => {
    const m = window.matchMedia?.(query)
    if (!m) return
    const on = () => setWide(m.matches)
    m.addEventListener('change', on)
    return () => m.removeEventListener('change', on)
  }, [])
  return wide
}

/**
 * Calendar shows the days by month or week: on each, short bars for the kinds of data recorded
 * (activities and plans one bar each), named on wide screens. A day opens in a panel beside it
 * (a sheet on phones) with its timeline, plans, activities and details. Plans of your own can be
 * added, dragged to another day, moved and removed.
 */
export default function CalendarPage() {
  const p = usePeriod()
  const units = useMe().data?.units ?? 'metric'
  const prefs = usePrefs()
  const save = useSavePrefs()
  const toast = useToast()
  const actions = usePlanActions()
  const wide = useWide()
  const [params, setParams] = useSearchParams()
  const [sheet, setSheet] = useState(false)
  const picking = params.get('plan') === '1'
  const setPicking = (on: boolean) => {
    const n = new URLSearchParams(params)
    if (on) n.set('plan', '1')
    else n.delete('plan')
    setParams(n, { replace: true })
  }
  const hidden = useMemo(() => new Set<CalendarKind>(prefs.raw.calendar_hidden ?? []), [prefs.raw.calendar_hidden])
  const cells = p.period === 'week' ? weekCells(p.day) : monthCells(p.day)
  const q = useCalendar(cells[0], cells[cells.length - 1])
  const byDay = useMemo(() => new Map((q.data?.days ?? []).map((d) => [d.day, d])), [q.data])
  const month = p.day.slice(0, 7)

  const toggle = (k: CalendarKind) => {
    const next = new Set(hidden)
    if (next.has(k)) next.delete(k)
    else next.add(k)
    save.mutate({ next: { ...prefs.raw, v: prefs.raw.v || 1, calendar_hidden: kinds.map(([x]) => x).filter((x) => next.has(x)) }, rev: prefs.rev })
  }
  const pick = (d: string) => {
    p.go(d)
    if (!wide) setSheet(true)
  }
  const move = (id: number, day: string) => {
    actions.move.mutate({ id, day }, {
      onSuccess: () => toast({ text: `Moved to ${longDay.format(dateOf(day))}` }),
      onError: (e) => toast({ text: `Not moved: ${(e as Error).message}`, tone: 'error' }),
    })
  }

  const panel = (
    <DayPanel day={p.day} cd={byDay.get(p.day)} today={p.today} latest={p.latest} tz={p.tz} units={units}
      onDay={(d) => p.go(d)} onPlan={() => setPicking(true)} onMove={move} inSheet={!wide} />
  )
  return (
    <>
      <HeaderActions>
        <Menu label="Show" icon={<SlidersHorizontal size={16} aria-hidden />}
          items={kinds.map(([k, info]) => ({ label: info.label, checked: !hidden.has(k), checkbox: true, run: () => toggle(k) }))} />
      </HeaderActions>
      <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,400px)]">
        <section className="min-w-0" aria-label={p.period === 'week' ? 'Week' : 'Month'} aria-busy={q.isFetching}>
          {p.period === 'week'
            ? <WeekList cells={cells} byDay={byDay} hidden={hidden} units={units} p={p} onPick={pick} />
            : <MonthGrid cells={cells} month={month} byDay={byDay} hidden={hidden} units={units} p={p} onPick={pick} onDropPlan={move} />}
          <Legend hidden={hidden} />
        </section>
        {wide && <aside className="min-w-0 self-start lg:sticky lg:top-[150px]">{panel}</aside>}
      </div>
      {!wide && sheet && <Sheet title={longDay.format(dateOf(p.day))} onClose={() => setSheet(false)}>{panel}</Sheet>}
      {picking && <PlanPicker day={p.day} today={p.today} onClose={() => setPicking(false)} />}
    </>
  )
}

interface GridProps {
  cells: string[]
  byDay: Map<string, CalendarDay>
  hidden: Set<CalendarKind>
  units: Units
  p: ReturnType<typeof usePeriod>
  onPick: (d: string) => void
}

/** gridKeys moves the selected day with the arrow keys (a week up or down), Home and End to
 * the week's ends; the global ←/→ period keys don't see them. */
function gridKeys(e: KeyboardEvent, day: string, onPick: (d: string) => void, latest: string) {
  const moves: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }
  if (!(e.key in moves)) return
  e.preventDefault()
  e.stopPropagation()
  const d = addDays(day, moves[e.key])
  if (d <= latest) onPick(d)
}

function MonthGrid({ cells, month, byDay, hidden, units, p, onPick, onDropPlan }: GridProps & { month: string; onDropPlan: (id: number, day: string) => void }) {
  const names = weekdayNames('short')
  const grid = useRef<HTMLDivElement>(null)
  // Keep focus on the selected day while the arrow keys move it (it may scroll into a new month).
  useEffect(() => {
    if (grid.current?.contains(document.activeElement)) grid.current.querySelector<HTMLButtonElement>('[aria-current="date"], [data-selected]')?.focus()
  }, [p.day])
  return (
    <div className="card overflow-hidden p-0">
      <div className="grid grid-cols-7 border-b border-border" aria-hidden>
        {names.map((n) => <span key={n} className="px-2 py-2 text-center font-mono text-[10px] uppercase tracking-[.14em] text-faint">{n}</span>)}
      </div>
      <div ref={grid} className="grid grid-cols-7" role="group" aria-label="Days">
        {cells.map((d) => (
          <DayCell key={d} d={d} cd={byDay.get(d)} hidden={hidden} units={units} out={d.slice(0, 7) !== month} p={p}
            onPick={onPick} onDropPlan={onDropPlan} />
        ))}
      </div>
    </div>
  )
}

function DayCell({ d, cd, hidden, units, out, p, onPick, onDropPlan }: {
  d: string; cd?: CalendarDay; hidden: Set<CalendarKind>; units: Units; out: boolean; p: GridProps['p']
  onPick: (d: string) => void; onDropPlan: (id: number, day: string) => void
}) {
  const [over, setOver] = useState(false)
  const e = cd ? dayEntries(cd, hidden, units) : { named: [], bars: [] }
  const selected = d === p.day
  const isToday = d === p.today
  const future = d > p.today
  const labelBars = e.bars.filter((b) => b.kind !== 'activity' && b.kind !== 'plan')
  const wideBars = fitBars(labelBars, 4)
  const narrow = fitBars(e.bars, 5)
  const spoken = [longDay.format(dateOf(d)), isToday ? 'today' : '', ...e.named.map((n) => n.kind === 'plan' ? `${n.text} ${planStateText(n.state)}` : n.text),
    ...labelBars.map((b) => b.text)].filter(Boolean).join(', ')
  const drop = (ev: DragEvent) => {
    ev.preventDefault()
    setOver(false)
    const id = Number(ev.dataTransfer.getData('text/x-goarmin-plan'))
    if (id > 0 && d <= p.latest) onDropPlan(id, d)
  }
  return (
    <button type="button" onClick={() => onPick(d)} onKeyDown={(ev) => gridKeys(ev, d, onPick, p.latest)}
      aria-label={spoken} aria-current={selected ? 'date' : undefined} data-selected={selected ? '' : undefined} tabIndex={selected ? 0 : -1}
      disabled={d > p.latest}
      onDragOver={(ev) => { if (ev.dataTransfer.types.includes('text/x-goarmin-plan')) { ev.preventDefault(); setOver(true) } }}
      onDragLeave={() => setOver(false)} onDrop={drop}
      className={`relative flex min-h-[92px] min-w-0 flex-col gap-1 border-b border-r border-border p-1.5 text-left align-top transition-colors sm:min-h-[118px] sm:p-2 ${
        out ? 'bg-bg2/60' : ''} ${selected ? 'z-[1] outline outline-2 -outline-offset-2 outline-lime' : 'hover:bg-panel2'} ${over ? 'bg-lime-fill' : ''}`}>
      <span className={`inline-grid h-6 w-6 place-items-center rounded-full font-mono text-[12px] ${isToday ? 'ring-1 ring-lime text-lime' : out ? 'text-faint' : future ? 'text-muted' : 'text-text'}`}>
        {Number(d.slice(8))}
      </span>
      {/* Wide: names, then labelled bars. */}
      <span className="hidden min-w-0 flex-col gap-0.5 lg:flex">
        {e.named.slice(0, 3).map((n) => <NamedLine key={n.key} n={n} />)}
        {e.named.length > 3 && <span className="text-[11px] text-muted">+{e.named.length - 3} more</span>}
        {wideBars.shown.map((b) => <BarLine key={b.key} b={b} label />)}
        {wideBars.more > 0 && <span className="text-[11px] text-muted">+{wideBars.more}</span>}
      </span>
      {/* Narrow: bars only. */}
      <span className="flex min-w-0 flex-col gap-[3px] lg:hidden">
        {narrow.shown.map((b) => <BarLine key={b.key} b={b} />)}
        {narrow.more > 0 && <span className="font-mono text-[10px] leading-none text-lime">+{narrow.more}</span>}
      </span>
    </button>
  )
}

const hatch = (tone: string) => `repeating-linear-gradient(135deg, ${tone} 0 3px, transparent 3px 6px)`

/** BarLine is one bar: a short coloured line, with its words on wide cells. */
function BarLine({ b, label }: { b: Bar; label?: boolean }) {
  const tone = kindInfo[b.kind].tone
  const bar = (
    <span aria-hidden className={`block h-[5px] shrink-0 rounded-[2px] ${label ? 'w-3' : 'w-full'} ${b.faint || b.state === 'missed' ? 'opacity-40' : ''}`}
      style={{ background: b.hatched ? hatch(tone) : tone }} />
  )
  if (!label) return bar
  return (
    <span className="flex min-w-0 items-center gap-1.5 text-[11px] text-muted">
      {bar}
      <span className="truncate">{b.text}</span>
    </span>
  )
}

/** NamedLine names an activity or a plan on a wide cell; an own plan can be dragged to another day. */
function NamedLine({ n }: { n: Named }) {
  const tone = kindInfo[n.kind].tone
  const draggable = n.plan != null
  return (
    <span draggable={draggable} onDragStart={draggable ? (e) => { e.stopPropagation(); e.dataTransfer.setData('text/x-goarmin-plan', String(n.plan)); e.dataTransfer.effectAllowed = 'move' } : undefined}
      className={`flex min-w-0 items-center gap-1.5 rounded px-1 py-[1px] text-[12px] ${n.kind === 'plan' ? 'border border-dashed border-lime-dim/60' : ''} ${
        n.state === 'missed' ? 'text-faint' : 'text-text'} ${draggable ? 'cursor-grab' : ''}`}
      title={draggable ? 'Drag to another day' : undefined}>
      {n.kind === 'activity' && <span aria-hidden className="h-1.5 w-1.5 shrink-0 rounded-full" style={{ background: tone }} />}
      {n.state === 'done' && <Check size={11} aria-hidden className="shrink-0 text-lime" />}
      <span className="truncate">{n.text}</span>
    </span>
  )
}

/** WeekList is the week as rows, an agenda: handier for planning on a phone. */
function WeekList({ cells, byDay, hidden, units, p, onPick }: GridProps) {
  return (
    <ol className="m-0 grid list-none gap-2 p-0" aria-label="Days">
      {cells.map((d) => {
        const cd = byDay.get(d)
        const e = cd ? dayEntries(cd, hidden, units) : { named: [], bars: [] }
        const bars = e.bars.filter((b) => b.kind !== 'activity' && b.kind !== 'plan')
        const selected = d === p.day
        return (
          <li key={d}>
            <button type="button" onClick={() => onPick(d)} onKeyDown={(ev) => gridKeys(ev, d, onPick, p.latest)} disabled={d > p.latest}
              aria-current={selected ? 'date' : undefined} data-selected={selected ? '' : undefined} tabIndex={selected ? 0 : -1}
              className={`card flex w-full min-w-0 flex-col gap-2 p-3 text-left sm:flex-row sm:items-start sm:gap-4 ${selected ? 'outline outline-2 -outline-offset-2 outline-lime' : 'hover:bg-panel2'}`}>
              <span className={`shrink-0 font-mono text-[12px] uppercase tracking-[.1em] sm:w-[150px] ${d === p.today ? 'text-lime' : 'text-muted'}`}>
                {longDay.format(dateOf(d))}{d === p.today ? ' · today' : ''}
              </span>
              <span className="flex min-w-0 flex-1 flex-col gap-1">
                {e.named.map((n) => <NamedLine key={n.key} n={n} />)}
                <span className="flex min-w-0 flex-wrap gap-x-4 gap-y-1">
                  {bars.map((b) => <BarLine key={b.key} b={b} label />)}
                </span>
                {e.named.length === 0 && bars.length === 0 && <span className="text-[12px] text-faint">Nothing recorded or planned</span>}
              </span>
            </button>
          </li>
        )
      })}
    </ol>
  )
}

/** Legend names the bars' colours (the hidden kinds left out). */
function Legend({ hidden }: { hidden: Set<CalendarKind> }) {
  const groups: [string, string, boolean?][] = [
    ['Activity', kindInfo.activity.tone], ['Planned', kindInfo.plan.tone, true], ['Steps', kindInfo.steps.tone],
    ['Sleep, HRV, naps', kindInfo.sleep.tone], ['Heart rate, ECG', kindInfo.heart.tone], ['Stress, Body Battery', kindInfo.stress.tone],
    ['Weight, drinks', kindInfo.weigh_in.tone],
  ]
  return (
    <ul className="m-0 mt-3 flex list-none flex-wrap gap-x-4 gap-y-1 p-0" aria-label="Legend">
      {groups.map(([label, tone, hatched]) => (
        <li key={label} className="inline-flex items-center gap-1.5 text-[12px] text-muted">
          <span aria-hidden className="h-[5px] w-4 rounded-[2px]" style={{ background: hatched ? hatch(tone) : tone }} />{label}
        </li>
      ))}
      {hidden.size > 0 && <li className="text-[12px] text-faint">{hidden.size} hidden (Show)</li>}
    </ul>
  )
}
