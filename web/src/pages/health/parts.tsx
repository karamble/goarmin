// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useId, useState, type ReactNode } from 'react'
import { errorText } from '../../api/client'
import type { Day, Night } from '../../api/types'
import { usePeriod, type PeriodState } from '../../app/section/usePeriod'
import { Chart, micavoss, type Ctx } from '../../charts'
import type { ChartEvents } from '../../charts/core/useECharts'
import type { Option } from '../../charts/core/echarts'
import { hm, hourCycle } from '../../lib/format'
import { asleep, stageInfo } from '../../lib/health'
import { addDays, days as daysOf, shortDay, weekStart, type Range } from '../../lib/period'
import { Button, Skeleton } from '../../ui/kit'

/** chartCtx is the chart context for the user's zone. */
export function chartCtx(tz: string): Ctx {
  return { tokens: micavoss, tz, hourCycle: hourCycle(), firstDay: weekStart() }
}

/** Card is a page section with a title. */
export function Card({ title, hint, right, children, className = '' }: { title: string; hint?: ReactNode; right?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={`card min-w-0 p-5 ${className}`}>
      <header className="mb-3 flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <h2 className="caps m-0">{title}</h2>
        {right}
      </header>
      {hint && <p className="m-0 -mt-1 mb-3 text-[13px] text-muted">{hint}</p>}
      {children}
    </section>
  )
}

export interface Table {
  head: string[]
  rows: (string | number)[][]
}

/** ChartCard is a chart in a card, with its values as a table for those who cannot see it. */
export function ChartCard({ title, hint, right, ariaLabel, option, height = 220, onEvents, group, table, className }: {
  title: string
  hint?: ReactNode
  right?: ReactNode
  ariaLabel: string
  option: Option
  height?: number
  onEvents?: ChartEvents
  group?: string
  table?: Table
  className?: string
}) {
  const [showTable, setShowTable] = useState(false)
  const tid = useId()
  return (
    <Card title={title} hint={hint} className={className} right={
      <span className="flex items-center gap-3">
        {right}
        {table && table.rows.length > 0 && (
          <button type="button" aria-expanded={showTable} aria-controls={tid} onClick={() => setShowTable((s) => !s)}
            className="font-mono text-[10px] uppercase tracking-[.14em] text-faint hover:text-text">
            {showTable ? 'Chart' : 'Table'}
          </button>
        )}
      </span>
    }>
      <div hidden={showTable}>
        <Chart option={option} ariaLabel={ariaLabel} height={height} onEvents={onEvents} group={group} />
      </div>
      {table && showTable && (
        <div id={tid} className="max-h-[320px] overflow-auto">
          <table className="w-full border-collapse text-left text-[13px]">
            <thead>
              <tr>{table.head.map((h) => <th key={h} className="sticky top-0 bg-panel px-2 py-1.5 font-mono text-[10px] uppercase tracking-[.12em] text-faint">{h}</th>)}</tr>
            </thead>
            <tbody>
              {table.rows.map((r, i) => (
                <tr key={i} className="border-t border-border">
                  {r.map((c, j) => <td key={j} className={`px-2 py-1.5 ${j ? 'num' : 'text-muted'}`}>{c}</td>)}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  )
}

export interface KpiItem {
  label: string
  value: ReactNode
  unit?: string
  hint?: ReactNode
  tone?: 'lime' | 'amber'
}

/** Kpis is a row of the period's key numbers. */
export function Kpis({ items, cols = 'sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6' }: { items: KpiItem[]; cols?: string }) {
  return (
    <dl className={`m-0 grid grid-cols-2 gap-x-6 gap-y-5 ${cols}`}>
      {items.map((k) => (
        <div key={k.label} className="min-w-0">
          <dt className="caps !text-[10px] !text-faint">{k.label}</dt>
          <dd className={`num m-0 mt-1.5 text-[26px] leading-none ${k.tone === 'lime' ? 'text-lime' : k.tone === 'amber' ? 'text-amber' : 'text-text'}`}>
            {k.value}
            {k.unit && <span className="ml-1 font-sans text-[13px] text-muted">{k.unit}</span>}
          </dd>
          {k.hint && <p className="m-0 mt-1 truncate text-[12px] text-muted">{k.hint}</p>}
        </div>
      ))}
    </dl>
  )
}

const periodWord = { day: 'on this day', week: 'in this week', month: 'in this month', year: 'in this year', all: 'yet' } as const

/** Empty says a period has nothing to show, calmly; today it says the watch has not synced yet. */
export function Empty({ p, what, children }: { p: Pick<PeriodState, 'period' | 'isToday'>; what: string; children?: ReactNode }) {
  return (
    <section className="card grid place-items-center px-6 py-12 text-center">
      <p className="m-0 font-display text-[22px] uppercase">No {what} {periodWord[p.period]}</p>
      <p className="m-0 mt-2 max-w-[480px] text-[14px] text-muted">
        {children ?? (p.isToday && p.period === 'day'
          ? 'The watch has not synced it yet, or did not record it.'
          : 'The watch did not record it, or it is from before goarmin fetched data. Older history arrives with the Garmin Connect import.')}
      </p>
    </section>
  )
}

interface QueryLike {
  isPending: boolean
  isError: boolean
  error: unknown
  refetch: () => unknown
  data?: unknown
}

/** Loaded shows a skeleton while the queries load, an error with Retry if one failed. */
export function Loaded({ q, children }: { q: QueryLike[]; children: () => ReactNode }) {
  const failed = q.find((x) => x.isError && x.data === undefined)
  if (failed) {
    return (
      <section role="alert" className="card grid gap-3 p-6">
        <p className="m-0 text-[14px] text-err">{errorText(failed.error)}</p>
        <div><Button small onClick={() => q.forEach((x) => x.isError && x.refetch())}>Try again</Button></div>
      </section>
    )
  }
  if (q.some((x) => x.isPending)) {
    return (
      <div className="grid gap-5" aria-busy="true">
        <Skeleton className="h-[120px] rounded-[18px]" />
        <Skeleton className="h-[260px] rounded-[18px]" />
      </div>
    )
  }
  return <>{children()}</>
}

/** StageBar is a night's stages as one segmented bar with a legend. */
export function StageBar({ night }: { night: Night }) {
  const total = stageInfo.reduce((a, s) => a + (night[s.key] ?? 0), 0)
  if (!total) return null
  const order = [...stageInfo].reverse() // deep, light, REM, awake
  return (
    <div>
      <div className="flex h-2.5 w-full gap-[2px] overflow-hidden rounded-full" role="img"
        aria-label={order.map((s) => `${s.label} ${hm(night[s.key])}`).join(', ')}>
        {order.map((s) => (night[s.key] ?? 0) > 0 && <span key={s.key} style={{ flexGrow: night[s.key]!, background: s.color }} />)}
      </div>
      <div className="mt-2.5 flex flex-wrap gap-x-5 gap-y-1">
        {order.map((s) => (
          <span key={s.key} className="inline-flex items-center gap-1.5 text-[12px] text-muted">
            <span aria-hidden className="h-2 w-2 rounded-[2px]" style={{ background: s.color }} />
            {s.label} <span className="num text-text">{hm(night[s.key])}</span>
          </span>
        ))}
      </div>
    </div>
  )
}

/** Health is what every health page reads: the period and the chart context. */
export function useHealth() {
  const p = usePeriod()
  return { p, ctx: chartCtx(p.tz) }
}

/** rangeDays is the period's days with their labels for a category axis. */
export function rangeDays(r: Range, period: PeriodState['period']): { days: string[]; labels: string[] } {
  const days = daysOf(r)
  const fmt = period === 'week'
    ? (d: string) => new Intl.DateTimeFormat(undefined, { weekday: 'short', day: 'numeric', timeZone: 'UTC' }).format(new Date(d + 'T12:00:00Z'))
    : period === 'month' ? (d: string) => String(Number(d.slice(8)))
    : shortDay
  return { days, labels: days.map(fmt) }
}

/** byDay indexes days by their date. */
export function byDay<T extends { day: string }>(list: T[] | undefined): Map<string, T> {
  return new Map((list ?? []).map((x) => [x.day, x]))
}

/** nightOn picks the night that ended on day: the longest, if the watch saw several. */
export function nightsByDay(list: Night[] | undefined): Map<string, Night> {
  const out = new Map<string, Night>()
  for (const n of list ?? []) {
    const have = out.get(n.day)
    if (!have || n.end - n.start > have.end - have.start) out.set(n.day, n)
  }
  return out
}

/** dayClick opens the clicked day of a category chart (or a heatmap cell) in the Day view. */
export function dayClick(days: string[], open: (day: string) => void): ChartEvents {
  return {
    click: (params: unknown) => {
      const p = params as { dataIndex?: number; value?: unknown; data?: unknown }
      const v = Array.isArray(p.value) ? p.value : Array.isArray(p.data) ? p.data : null
      if (v && typeof v[0] === 'string') return open(v[0])
      if (p.dataIndex != null && days[p.dataIndex]) open(days[p.dataIndex])
    },
  }
}

/** withLead widens a range by n days before it (for averages that look back). */
export function withLead(r: Range, n: number): Range {
  return { from: addDays(r.from, -n), to: r.to }
}

export const num = (v: number | null | undefined, digits = 0) => (v == null ? '–' : v.toFixed(digits))
export const isDayIn = (d: Day | undefined, key: keyof Day) => d != null && d[key] != null

export { asleep }
