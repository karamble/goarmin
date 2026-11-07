// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import { addDays, addMonths, days, isDay, parseDay, periodRange, relDay, setWeekStart, step, today, weekday, weekdayNames, weekStart } from './period'

// Every date is derived from the clock; the properties hold for any day.
const now = new Date()
const d0 = today('UTC', now)

describe('period', () => {
  it('validates days', () => {
    expect(isDay(d0)).toBe(true)
    expect(isDay(d0.slice(0, 8) + '32')).toBe(false)
    expect(isDay('7.11.')).toBe(false)
  })
  it('adds days across months and years', () => {
    for (let n = -400; n <= 400; n += 37) {
      expect(addDays(addDays(d0, n), -n)).toBe(d0)
    }
    expect(addDays(d0.slice(0, 5) + '12-31', 1)).toBe(String(+d0.slice(0, 4) + 1) + '-01-01')
  })
  it('clamps the day of the month when adding months', () => {
    const y = d0.slice(0, 4)
    const leap = +y % 4 === 0 && (+y % 100 !== 0 || +y % 400 === 0)
    expect(addMonths(`${y}-01-31`, 1)).toBe(`${y}-02-${leap ? 29 : 28}`)
    expect(addMonths(`${y}-03-15`, -2)).toBe(`${y}-01-15`)
  })
  it('spans weeks from Monday to Sunday', () => {
    for (let n = 0; n < 14; n++) {
      const d = addDays(d0, n)
      const r = periodRange(d, 'week')
      expect(weekday(r.from)).toBe(0)
      expect(weekday(r.to)).toBe(6)
      expect(r.from <= d && d <= r.to).toBe(true)
      expect(days(r)).toHaveLength(7)
    }
  })
  it('spans months and years', () => {
    const m = periodRange(d0, 'month')
    expect(m.from.endsWith('-01')).toBe(true)
    expect(addDays(m.to, 1).endsWith('-01')).toBe(true)
    expect(periodRange(d0, 'year')).toEqual({ from: d0.slice(0, 4) + '-01-01', to: d0.slice(0, 4) + '-12-31' })
  })
  it('steps by periods and back', () => {
    for (const p of ['day', 'week', 'year'] as const) expect(step(step(d0, p, 3), p, -3)).toBe(d0)
    expect(periodRange(step(d0, 'month', 1), 'month').from).toBe(addMonths(periodRange(d0, 'month').from, 1))
  })
  it('knows today in a zone', () => {
    // Somewhere it is already tomorrow or still yesterday relative to UTC, never further.
    const ahead = today('Pacific/Kiritimati', now)
    const behind = today('Pacific/Pago_Pago', now)
    expect([d0, addDays(d0, 1)]).toContain(ahead)
    expect([d0, addDays(d0, -1)]).toContain(behind)
  })
})

describe('relDay', () => {
  const t = today()
  it('names recent days and adds the year across years', () => {
    expect(relDay(t, t)).toBe('Today')
    expect(relDay(addDays(t, -1), t)).toBe('Yesterday')
    expect(relDay(addDays(t, -3), t)).not.toMatch(/\d{4}/)
    expect(relDay(addMonths(t, -13), t)).toContain(addMonths(t, -13).slice(0, 4))
  })
})

describe('week start', () => {
  // A Wednesday, derived from today: today's weekday, moved to Wednesday.
  const t = today()
  const wed = addDays(t, (3 - new Date(t + 'T12:00:00Z').getUTCDay() + 7) % 7)

  it('follows the chosen first day', () => {
    try {
      setWeekStart(0) // Sunday
      expect(weekStart()).toBe(0)
      expect(weekday(wed)).toBe(3)
      expect(periodRange(wed, 'week')).toEqual({ from: addDays(wed, -3), to: addDays(wed, 3) })
      expect(weekdayNames('narrow')).toHaveLength(7)
      expect(weekdayNames('short')[0]).toBe(new Intl.DateTimeFormat(undefined, { weekday: 'short', timeZone: 'UTC' }).format(new Date(addDays(wed, -3) + 'T12:00:00Z')))
      setWeekStart(6) // Saturday
      expect(weekday(wed)).toBe(4)
      expect(periodRange(wed, 'week').from).toBe(addDays(wed, -4))
      setWeekStart(5) // not a week start: Monday
      expect(weekStart()).toBe(1)
    } finally {
      setWeekStart(1)
    }
    expect(weekday(wed)).toBe(2)
    expect(periodRange(wed, 'week')).toEqual({ from: addDays(wed, -2), to: addDays(wed, 4) })
  })
})

describe('parseDay', () => {
  const t = today()
  it('reads typed days, never one after today', () => {
    expect(parseDay('today', t)).toBe(t)
    expect(parseDay(' Yesterday ', t)).toBe(addDays(t, -1))
    expect(parseDay('3 days ago', t)).toBe(addDays(t, -3))
    expect(parseDay('2 weeks ago', t)).toBe(addDays(t, -14))
    expect(parseDay(addDays(t, -40), t)).toBe(addDays(t, -40))
    expect(parseDay(addDays(t, 1), t)).toBeNull()
    expect(parseDay('sleep', t)).toBeNull()
    expect(parseDay('mo', t)).toBeNull() // too short to be a day name
    const mon = parseDay('last monday', t)!
    expect(new Date(mon + 'T12:00:00Z').getUTCDay()).toBe(1)
    expect(mon < t && mon >= addDays(t, -7)).toBe(true)
    expect(parseDay('mon', t)).toBe(mon)
  })
})
