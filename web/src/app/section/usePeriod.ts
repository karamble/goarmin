// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { isDay, periodRange, step as stepDay, today as todayIn, type Period, type Range } from '../../lib/period'

/** useToday is the current day in tz; it rolls over at midnight. */
export function useToday(tz: string): string {
  const [day, setDay] = useState(() => todayIn(tz))
  useEffect(() => {
    setDay(todayIn(tz))
    const id = setInterval(() => setDay(todayIn(tz)), 30_000)
    return () => clearInterval(id)
  }, [tz])
  return day
}

/** The periods the current page allows; the first is its default. */
export const PeriodsContext = createContext<Period[]>([])

/** DefaultPeriodContext is the page's period when the URL names none (else its first one). */
export const DefaultPeriodContext = createContext<Period | undefined>(undefined)

export interface PeriodState {
  /** The day in focus (?d, absent = today), never after today. */
  day: string
  period: Period
  periods: Period[]
  range: Range
  today: string
  isToday: boolean
  /** The user's zone ("" = the browser's). */
  tz: string
  canNext: boolean
  go: (day: string) => void
  setPeriod: (p: Period) => void
  step: (n: number) => void
  toToday: () => void
  /** open shows one day in the Day view (from a week, month or year), as a new history entry so
   * Back returns to the range. */
  open: (day: string) => void
}

/** usePeriod reads and moves the date and period kept in the URL (?d=YYYY-MM-DD&p=week). */
export function usePeriod(): PeriodState {
  const periods = useContext(PeriodsContext)
  const preferred = useContext(DefaultPeriodContext)
  const tz = useMe().data?.timezone ?? ''
  const today = useToday(tz)
  const [q, setQ] = useSearchParams()
  const raw = q.get('d')
  const day = isDay(raw) && raw < today ? raw : today
  const def = preferred && periods.includes(preferred) ? preferred : (periods[0] ?? 'day')
  const rawP = q.get('p') as Period | null
  const period = rawP && periods.includes(rawP) ? rawP : def

  const write = useCallback(
    (d: string, p: Period, push = false) => {
      setQ(
        (prev) => {
          const n = new URLSearchParams(prev)
          if (d >= today) n.delete('d')
          else n.set('d', d)
          if (p === def) n.delete('p')
          else n.set('p', p)
          return n
        },
        { replace: !push },
      )
    },
    [setQ, today, def],
  )

  return useMemo(() => {
    const next = stepDay(day, period, 1)
    const canNext = period !== 'all' && periodRange(next, period).from <= today
    return {
      day, period, periods, range: periodRange(day, period), today, isToday: day === today, tz, canNext,
      go: (d) => write(d > today ? today : d, period),
      setPeriod: (p) => periods.includes(p) && write(day, p),
      step: (n) => {
        if (period === 'all') return
        let d = stepDay(day, period, n)
        if (d > today) {
          if (periodRange(d, period).from > today) return
          d = today
        }
        write(d, period)
      },
      toToday: () => write(today, period),
      open: (d) => periods.includes('day') && write(d > today ? today : d, 'day', true),
    }
  }, [day, period, periods, today, tz, write])
}
