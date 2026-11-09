// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useEffect, useId, useState, type ReactNode } from 'react'
import { clock } from '../../../lib/format'

/** The builder's small inputs: they keep what is typed while it is typed, and hand on a value
 * once it reads as one. */

export const fieldClass = 'w-full rounded-lg border border-border bg-bg2 px-3 py-2 text-[14px] text-text focus:border-lime focus:outline-none'

/** Labelled wraps an input with its label and a hint. */
export function Labelled({ label, hint, children }: { label: string; hint?: ReactNode; children: (id: string) => ReactNode }) {
  const id = useId()
  return (
    <div className="grid gap-1">
      <label htmlFor={id} className="text-[13px] text-muted">{label}</label>
      {children(id)}
      {hint && <span className="text-[12px] text-faint">{hint}</span>}
    </div>
  )
}

/** parseClock reads "1:30", "01:30", "90" or "1:02:03" as seconds (null if it does not read). */
export function parseClock(s: string): number | null {
  const t = s.trim()
  if (!t) return null
  if (!/^\d+(:\d{1,2}){0,2}$/.test(t)) return null
  const parts = t.split(':').map(Number)
  if (parts.slice(1).some((p) => p >= 60)) return null
  return parts.reduce((a, p) => a * 60 + p, 0)
}

/** TimeInput edits seconds as m:ss. */
export function TimeInput({ label, value, onChange, hint }: { label: string; value: number; onChange: (s: number) => void; hint?: ReactNode }) {
  const [text, setText] = useState(clock(value))
  useEffect(() => setText(clock(value)), [value])
  const bad = parseClock(text) === null
  return (
    <Labelled label={label} hint={bad ? 'Minutes and seconds, like 1:30' : hint}>
      {(id) => (
        <input id={id} value={text} inputMode="numeric" aria-invalid={bad} className={fieldClass}
          onChange={(e) => {
            setText(e.target.value)
            const s = parseClock(e.target.value)
            if (s !== null) onChange(s)
          }}
          onBlur={() => setText(clock(value))} />
      )}
    </Labelled>
  )
}

/** NumberInput edits a number between min and max (step decimals allowed when step < 1). */
export function NumberInput({ label, value, onChange, min = 0, max, step = 1, unit, hint }: {
  label: string; value: number; onChange: (v: number) => void; min?: number; max?: number; step?: number; unit?: string; hint?: ReactNode
}) {
  const [text, setText] = useState(String(value))
  useEffect(() => setText(String(value)), [value])
  return (
    <Labelled label={unit ? `${label} (${unit})` : label} hint={hint}>
      {(id) => (
        <input id={id} type="number" value={text} min={min} max={max} step={step} inputMode={step < 1 ? 'decimal' : 'numeric'} className={fieldClass}
          onChange={(e) => {
            setText(e.target.value)
            const v = Number(e.target.value)
            if (e.target.value !== '' && Number.isFinite(v) && v >= min && (max === undefined || v <= max)) onChange(v)
          }}
          onBlur={() => setText(String(value))} />
      )}
    </Labelled>
  )
}

/** PaceInput edits a speed (m/s) as a pace, m:ss per km or mile. */
export function PaceInput({ label, speed, onChange, imperial }: { label: string; speed: number; onChange: (mps: number) => void; imperial: boolean }) {
  const per = imperial ? 1609.344 : 1000
  const pace = speed > 0 ? Math.round(per / speed) : 0
  return (
    <TimeInput label={`${label} (per ${imperial ? 'mile' : 'km'})`} value={pace}
      onChange={(s) => s > 0 && onChange(Math.round((per / s) * 1000) / 1000)} />
  )
}

/** Select is a labelled select of value and words. */
export function Select<T extends string>({ label, value, options, onChange }: { label: string; value: T; options: [T, string][]; onChange: (v: T) => void }) {
  return (
    <Labelled label={label}>
      {(id) => (
        <select id={id} value={value} onChange={(e) => onChange(e.target.value as T)} className={fieldClass}>
          {options.map(([v, w]) => <option key={v} value={v}>{w}</option>)}
        </select>
      )}
    </Labelled>
  )
}
