// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { ChartTokens } from '../theme/tokens'

/** Ctx is what every builder gets besides its data. */
export interface Ctx {
  tokens: ChartTokens
  /** IANA zone for axis labels and tooltips ("" = the browser's). */
  tz?: string
  /** The clock for times of day: 'h23', 'h12', or undefined (the browser's). */
  hourCycle?: 'h23' | 'h12'
  /** The week's first day as Date.getUTCDay counts (1 Monday, 0 Sunday, 6 Saturday). */
  firstDay?: number
}

/** tzFormat formats milliseconds in the context's zone. */
export function tzFormat(ctx: Ctx, o: Intl.DateTimeFormatOptions): (ms: number) => string {
  let f: Intl.DateTimeFormat
  try {
    f = new Intl.DateTimeFormat(undefined, { ...o, hourCycle: o.hour ? ctx.hourCycle : undefined, timeZone: ctx.tz || undefined })
  } catch {
    f = new Intl.DateTimeFormat(undefined, { ...o, hourCycle: o.hour ? ctx.hourCycle : undefined })
  }
  return (ms) => f.format(ms)
}

/** timeLabels picks axis labels for a span: hours within two days, else dates. */
export function timeLabels(ctx: Ctx, spanS: number): (ms: number) => string {
  return spanS <= 2 * 86400 ? tzFormat(ctx, { hour: '2-digit', minute: '2-digit' }) : tzFormat(ctx, { day: 'numeric', month: 'short' })
}
