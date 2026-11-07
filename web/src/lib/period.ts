// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Local calendar days as "YYYY-MM-DD" strings. All math runs on UTC dates, so a DST change
// never moves a day; the server resolves days to instants with the watch's own offsets.

export type Period = 'day' | 'week' | 'month' | 'year' | 'all'

const re = /^(\d{4})-(\d{2})-(\d{2})$/

export function isDay(s: string | null | undefined): s is string {
  if (!s || !re.test(s)) return false
  const d = toDate(s)
  return fromDate(d) === s
}

function toDate(day: string): Date {
  const [, y, m, d] = re.exec(day)!
  return new Date(Date.UTC(+y, +m - 1, +d))
}

function fromDate(d: Date): string {
  return d.toISOString().slice(0, 10)
}

/** addDays shifts a day by n days. */
export function addDays(day: string, n: number): string {
  const d = toDate(day)
  d.setUTCDate(d.getUTCDate() + n)
  return fromDate(d)
}

/** addMonths shifts by n months, keeping the day of the month where it exists (31 Jan + 1 = 28/29 Feb). */
export function addMonths(day: string, n: number): string {
  const d = toDate(day)
  const target = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + n, 1))
  const last = new Date(Date.UTC(target.getUTCFullYear(), target.getUTCMonth() + 1, 0)).getUTCDate()
  target.setUTCDate(Math.min(d.getUTCDate(), last))
  return fromDate(target)
}

// The week's first day as Date.getUTCDay counts: 1 Monday (the default), 0 Sunday, 6 Saturday.
// The user's preference sets it once at start (setWeekStart); everything below follows it.
let firstDay = 1

/** setWeekStart sets the first day of the week: 1 Monday, 0 Sunday, 6 Saturday. */
export function setWeekStart(d: number) {
  firstDay = d === 0 || d === 6 ? d : 1
}

/** weekStart is the first day of the week as Date.getUTCDay counts (1 Monday, 0 Sunday, 6 Saturday). */
export function weekStart(): number {
  return firstDay
}

/** weekday: 0 = the week's first day … 6 = its last. */
export function weekday(day: string): number {
  return (toDate(day).getUTCDay() - firstDay + 7) % 7
}

/** weekdayNames are the days' names in the week's order: narrow ("M"), short ("Mon") or two
 * letters ("Mo"). */
export function weekdayNames(style: 'narrow' | 'short' | 'two'): string[] {
  const f = new Intl.DateTimeFormat(undefined, { weekday: style === 'narrow' ? 'narrow' : 'short', timeZone: 'UTC' })
  // 2026-01-04 was a Sunday (getUTCDay 0).
  return Array.from({ length: 7 }, (_, i) => {
    const n = f.format(new Date(Date.UTC(2026, 0, 4 + ((firstDay + i) % 7))))
    return style === 'two' ? n.slice(0, 2) : n
  })
}

/** today is the current date in an IANA zone ("" = the browser's). */
export function today(tz = '', now: Date = new Date()): string {
  try {
    return new Intl.DateTimeFormat('en-CA', { timeZone: tz || undefined, year: 'numeric', month: '2-digit', day: '2-digit' }).format(now)
  } catch {
    return new Intl.DateTimeFormat('en-CA', { year: 'numeric', month: '2-digit', day: '2-digit' }).format(now)
  }
}

export interface Range {
  from: string
  to: string
}

/** periodRange is the span of the period that contains day (weeks start on weekStart()). */
export function periodRange(day: string, p: Period): Range {
  switch (p) {
    case 'week': {
      const from = addDays(day, -weekday(day))
      return { from, to: addDays(from, 6) }
    }
    case 'month': {
      const from = day.slice(0, 8) + '01'
      return { from, to: addDays(addMonths(from, 1), -1) }
    }
    case 'year':
      return { from: day.slice(0, 5) + '01-01', to: day.slice(0, 5) + '12-31' }
    default:
      return { from: day, to: day }
  }
}

/** step moves day by n periods. */
export function step(day: string, p: Period, n: number): string {
  switch (p) {
    case 'week':
      return addDays(day, 7 * n)
    case 'month':
      return addMonths(day, n)
    case 'year':
      return addMonths(day, 12 * n)
    default:
      return addDays(day, n)
  }
}

/** days lists every day from..to, inclusive. */
export function days(r: Range): string[] {
  const out: string[] = []
  for (let d = r.from; d <= r.to && out.length < 4000; d = addDays(d, 1)) out.push(d)
  return out
}

const fmt = (o: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat(undefined, { ...o, timeZone: 'UTC' })
const fDay = fmt({ weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' })
const fDayShort = fmt({ day: 'numeric', month: 'short' })
const fMonth = fmt({ month: 'long', year: 'numeric' })

/** label names the period that contains day, for the date navigator. */
export function label(day: string, p: Period): string {
  const r = periodRange(day, p)
  switch (p) {
    case 'week':
      return `${fDayShort.format(toDate(r.from))} – ${fDayShort.format(toDate(r.to))} ${r.to.slice(0, 4)}`
    case 'month':
      return fMonth.format(toDate(day))
    case 'year':
      return day.slice(0, 4)
    case 'all':
      return 'All time'
    default:
      return fDay.format(toDate(day))
  }
}

/** shortDay is "7 Nov" for axis labels and lists. */
export function shortDay(day: string): string {
  return fDayShort.format(toDate(day))
}

const fDayYear = fmt({ day: 'numeric', month: 'short', year: 'numeric' })
const fWeekday = fmt({ weekday: 'short', day: 'numeric', month: 'short' })

/** relDay names a day for lists: "Today", "Yesterday", "Thu 5 Nov" within a week, else "29 Dec 2025" across years. */
export function relDay(day: string, today: string): string {
  if (day === today) return 'Today'
  if (day === addDays(today, -1)) return 'Yesterday'
  if (day > addDays(today, -7) && day < today) return fWeekday.format(toDate(day))
  return day.slice(0, 4) === today.slice(0, 4) ? fDayShort.format(toDate(day)) : fDayYear.format(toDate(day))
}

const dayWords = ['sunday', 'monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday']

/** parseDay reads a typed day relative to today: "2026-10-03", "today", "yesterday",
 * "3 days ago", "2 weeks ago", "monday" or "last monday" (the latest one before today). It is
 * null for anything else and for days after today. */
export function parseDay(text: string, todayDay: string): string | null {
  const q = text.trim().toLowerCase().replace(/\s+/g, ' ')
  let d: string | null = null
  if (isDay(q)) d = q
  else if (q === 'today') d = todayDay
  else if (q === 'yesterday') d = addDays(todayDay, -1)
  else {
    const ago = /^(\d{1,4}) (day|days|week|weeks) ago$/.exec(q)
    if (ago) d = addDays(todayDay, -Number(ago[1]) * (ago[2].startsWith('week') ? 7 : 1))
    const wd = /^(?:last )?([a-z]+)$/.exec(q)
    if (!d && wd) {
      const i = dayWords.findIndex((w) => w.startsWith(wd[1]) && wd[1].length >= 3)
      if (i >= 0) {
        const back = (toDate(todayDay).getUTCDay() - i + 7) % 7 || 7
        d = addDays(todayDay, -back)
      }
    }
  }
  return d && d <= todayDay ? d : null
}
