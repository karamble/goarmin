// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

export type Units = 'metric' | 'imperial'

/** int formats a whole number with grouping ("12 345"), or "–" for unknown. */
export function int(v: number | null | undefined): string {
  return v == null || !Number.isFinite(v) ? '–' : Math.round(v).toLocaleString()
}

export function fixed(v: number | null | undefined, digits = 1): string {
  return v == null || !Number.isFinite(v) ? '–' : v.toLocaleString(undefined, { minimumFractionDigits: digits, maximumFractionDigits: digits })
}

/** hm formats seconds as "5h 42m" (or "42m"). */
export function hm(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds)) return '–'
  const m = Math.round(seconds / 60)
  const h = Math.floor(m / 60)
  return h > 0 ? `${h}h ${String(m % 60).padStart(2, '0')}m` : `${m}m`
}

/** clock formats a duration as "1:02:03" or "2:03". */
export function clock(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds)) return '–'
  const s = Math.round(seconds)
  const h = Math.floor(s / 3600)
  const mm = String(Math.floor((s % 3600) / 60)).padStart(h ? 2 : 1, '0')
  const ss = String(s % 60).padStart(2, '0')
  return h ? `${h}:${mm}:${ss}` : `${mm}:${ss}`
}

export function distance(m: number | null | undefined, u: Units): string {
  if (m == null || !Number.isFinite(m)) return '–'
  return u === 'imperial' ? `${fixed(m / 1609.344, 2)} mi` : `${fixed(m / 1000, 2)} km`
}

/** pace per km or mile from m/s. */
export function pace(mps: number | null | undefined, u: Units): string {
  if (!mps || mps <= 0) return '–'
  const s = (u === 'imperial' ? 1609.344 : 1000) / mps
  return `${clock(s)} /${u === 'imperial' ? 'mi' : 'km'}`
}

export function elevation(m: number | null | undefined, u: Units): string {
  if (m == null || !Number.isFinite(m)) return '–'
  return u === 'imperial' ? `${int(m * 3.28084)} ft` : `${int(m)} m`
}

/** timeOfDay formats a Unix time in a zone ("" = browser's) as "07:35". */
export function timeOfDay(unix: number, tz = ''): string {
  const o: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit', hourCycle: cycle }
  try {
    return new Intl.DateTimeFormat(undefined, { ...o, timeZone: tz || undefined }).format(unix * 1000)
  } catch {
    return new Intl.DateTimeFormat(undefined, o).format(unix * 1000)
  }
}

// The clock times are shown in: 'h23' (24-hour), 'h12', or undefined for the browser's own.
// The user's preference sets it once at start (setHourCycle).
let cycle: 'h23' | 'h12' | undefined

/** setHourCycle sets the clock for times of day: 'h23', 'h12', or undefined (the browser's). */
export function setHourCycle(c: 'h23' | 'h12' | undefined) {
  cycle = c
}

/** hourCycle is the clock times of day are shown in (undefined: the browser's). */
export function hourCycle(): 'h23' | 'h12' | undefined {
  return cycle
}

/** ago formats a past instant relative to now ("12 min ago"). */
export function ago(iso: string | number, now = Date.now()): string {
  const t = typeof iso === 'number' ? iso * 1000 : Date.parse(iso)
  const s = Math.max(0, (now - t) / 1000)
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.round(s / 60)} min ago`
  if (s < 86400) return `${Math.round(s / 3600)} h ago`
  return `${Math.round(s / 86400)} d ago`
}

/** bytes formats a size: "812 B", "34 KB", "2.4 MB". */
export function bytes(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return '–'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`
  if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`
  return `${(n / 1024 / 1024 / 1024).toFixed(1)} GB`
}
