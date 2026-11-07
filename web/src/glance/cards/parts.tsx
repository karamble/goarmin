// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { ReactNode } from 'react'
import { micavoss, type Ctx } from '../../charts'
import { hourCycle, int } from '../../lib/format'
import { weekStart } from '../../lib/period'

/** ctx is the chart context for the user's zone. */
export function ctx(tz: string): Ctx {
  return { tokens: micavoss, tz, hourCycle: hourCycle(), firstDay: weekStart() }
}

/** Kpi is a card's big number (mono, tabular) with its unit. */
export function Kpi({ value, unit, big, children }: { value: ReactNode; unit?: string; big?: boolean; children?: ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="flex items-baseline gap-1.5">
        <span className={`num leading-none text-text ${big ? 'text-[44px]' : 'text-[34px]'}`}>{value}</span>
        {unit && <span className="text-[13px] text-muted">{unit}</span>}
      </div>
      {children && <div className="mt-1.5 truncate text-[12px] text-muted">{children}</div>}
    </div>
  )
}

/** Stat is a small labelled value. */
export function Stat({ label, value, unit, tone }: { label: string; value: ReactNode; unit?: string; tone?: 'lime' | 'amber' | 'muted' }) {
  const color = tone === 'lime' ? 'text-lime' : tone === 'amber' ? 'text-amber' : tone === 'muted' ? 'text-muted' : 'text-text'
  return (
    <div className="min-w-0">
      <dt className="caps !text-[10px] !text-faint">{label}</dt>
      <dd className={`num m-0 mt-1 text-[17px] leading-none ${color}`}>
        {value}
        {unit && <span className="ml-1 font-sans text-[11px] text-muted">{unit}</span>}
      </dd>
    </div>
  )
}

/** Meter is a thin 0..max bar. */
export function Meter({ value, max, color = 'var(--lime)', label }: { value: number | null; max: number; color?: string; label: string }) {
  const f = value == null ? 0 : Math.max(0, Math.min(1, value / max))
  return (
    <div role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={max} aria-valuenow={value ?? undefined} className="h-1.5 w-full overflow-hidden rounded-full bg-track">
      <div className="h-full rounded-full transition-[width] duration-500" style={{ width: `${f * 100}%`, background: color }} />
    </div>
  )
}

/** last is the last non-null value. */
export function last(v: (number | null)[]): number | null {
  for (let i = v.length - 1; i >= 0; i--) if (v[i] != null) return v[i]
  return null
}

export const n = (v: number | null | undefined) => (v == null ? '–' : int(v))
