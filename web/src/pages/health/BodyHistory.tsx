// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo, useState } from 'react'
import { useMe } from '../../api/hooks/auth'
import { useBody } from '../../api/hooks/readers'
import type { WeighIn } from '../../api/types'
import { describeSeries, timeSeries, type SeriesSpec } from '../../charts'
import type { Role } from '../../charts/theme/tokens'
import { addDays, today } from '../../lib/period'
import { Skeleton } from '../../ui/kit'
import { ChartCard, num, useHealth } from './parts'

/** How much history the card loads: every weigh-in of the last ten years (the API's limit). */
const historyDays = 3650

type Field = 'weight_kg' | 'muscle_kg' | 'bone_kg' | 'fat_pct' | 'hydration_pct' | 'bmi'

interface Measure {
  field: Field
  label: string
  mass: boolean // kg, shown in lb for imperial; on the left axis
  unit?: string // for the rest
  role: Role
  on: boolean // shown before the user picks
}

/** The measures on the chart: masses on the left axis, percentages and BMI on the right. */
const measures: Measure[] = [
  { field: 'weight_kg', label: 'Weight', mass: true, role: 'primary', on: true },
  { field: 'muscle_kg', label: 'Muscle mass', mass: true, role: 'context', on: true },
  { field: 'bone_kg', label: 'Bone mass', mass: true, role: 'context2', on: false },
  { field: 'fat_pct', label: 'Body fat', mass: false, unit: '%', role: 'extra', on: true },
  { field: 'hydration_pct', label: 'Body water', mass: false, unit: '%', role: 'extra2', on: false },
  { field: 'bmi', label: 'BMI', mass: false, role: 'warn', on: false },
]

const storageKey = 'goarmin.body.history.series'

function loadPicked(): Field[] | null {
  try {
    const v = JSON.parse(localStorage.getItem(storageKey) ?? 'null')
    return Array.isArray(v) ? (v.filter((f) => measures.some((m) => m.field === f)) as Field[]) : null
  } catch {
    return null
  }
}

function savePicked(fs: Field[]) {
  try {
    localStorage.setItem(storageKey, JSON.stringify(fs))
  } catch {
    // a private window: the choice lasts until the page closes
  }
}

/** BodyHistoryCard draws every weigh-in on one chart: weight with muscle and bone mass (kg or lb)
 * against body fat, body water and BMI on a second axis. It opens on [focusFrom, focusTo] (Unix
 * seconds); zooming out shows the whole history. Each measure can be switched on and off. */
export function BodyHistoryCard({ focusFrom, focusTo }: { focusFrom: number; focusTo: number }) {
  const { p, ctx } = useHealth()
  const imperial = useMe().data?.units === 'imperial'
  const massUnit = imperial ? 'lb' : 'kg'
  const end = today(p.tz)
  const q = useBody(addDays(end, -historyDays), end)
  const [picked, setPicked] = useState<Field[]>(() => loadPicked() ?? measures.filter((m) => m.on).map((m) => m.field))
  const ws: WeighIn[] = useMemo(() => q.data?.weigh_ins ?? [], [q.data])

  const value = (w: WeighIn, m: Measure): number | null => {
    const v = w[m.field]
    if (v == null) return null
    return m.mass && imperial ? v * 2.20462 : v
  }
  // Only the measures some weigh-in has (a basic scale has no body fat).
  const available = measures.filter((m) => ws.some((w) => w[m.field] != null))
  if (q.isPending) return <Skeleton className="h-[320px] rounded-[18px]" />
  if (ws.length === 0) return null

  const shown = available.filter((m) => picked.includes(m.field))
  const t = ws.map((w) => w.ts)
  const series: SeriesSpec[] = shown.map((m) => ({
    id: m.field, name: m.label, t, v: ws.map((w) => value(w, m)), kind: 'line', role: m.role,
    unit: m.mass ? massUnit : m.unit, yAxis: m.mass ? 0 : 1, dots: true,
  }))
  const hasRight = shown.some((m) => !m.mass)
  const hasLeft = shown.some((m) => m.mass)
  // An axis without series would draw an empty scale: a right-only choice moves to the left.
  if (!hasLeft && hasRight) for (const s of series) s.yAxis = 0
  const from = t[0] - 86400
  const to = Math.max(t[t.length - 1] + 86400, Date.now() / 1000)
  const toggle = (f: Field) => {
    const next = picked.includes(f) ? picked.filter((x) => x !== f) : [...picked, f]
    setPicked(next)
    savePicked(next)
  }
  const weight = measures[0]
  const chips = (
    <span className="mt-2 flex flex-wrap gap-1.5" role="group" aria-label="Measures on the chart">
      {available.map((m) => {
        const on = picked.includes(m.field)
        return (
          <button key={m.field} type="button" aria-pressed={on} onClick={() => toggle(m.field)}
            className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[12px] ${on
              ? 'border-border bg-panel2 text-text' : 'border-border/60 text-faint hover:text-muted'}`}>
            <span className={`inline-block size-2 rounded-full ${on ? roleDot[m.role] : 'bg-faint'}`} aria-hidden />
            {m.label}{m.mass ? ` (${massUnit})` : m.unit ? ` (${m.unit})` : ''}
          </button>
        )
      })}
    </span>
  )
  return (
    <ChartCard title="Weight and body composition"
      hint={<>Every weigh-in since {new Date(ws[0].ts * 1000).getFullYear()}: drag the slider or Ctrl+scroll to zoom out to the whole history.{chips}</>}
      ariaLabel={shown.map((m) => describeSeries(m.label, ws.map((w) => value(w, m)), m.mass ? massUnit : m.unit ?? '', 1)).join('. ') || describeSeries(weight.label, ws.map((w) => value(w, weight)), massUnit, 1)}
      height={300}
      option={timeSeries({
        from, to, zoom: true, zoomFrom: focusFrom, zoomTo: focusTo, digits: 1, series,
        y: hasLeft && hasRight ? [{ unit: massUnit }, { unit: '%' }] : [{}],
      }, ctx)}
      table={{
        head: ['Day', ...available.map((m) => `${m.label}${m.mass ? ` (${massUnit})` : m.unit ? ` (${m.unit})` : ''}`)],
        rows: [...ws].reverse().map((w) => [w.day, ...available.map((m) => num(value(w, m), 1))]),
      }} />
  )
}

/** The chip dot of each role, in the chart's colours. */
const roleDot: Record<Role, string> = {
  primary: 'bg-lime', context: 'bg-[#c9ced4]', context2: 'bg-[#9aa1a9]', extra: 'bg-cyan', extra2: 'bg-pink',
  warn: 'bg-amber', crit: 'bg-crit', track: 'bg-faint',
}
