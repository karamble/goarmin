// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useDay } from '../../api/hooks/data'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { Kpi, Meter, n, Stat } from './parts'

/** Garmin's stress bands. */
export function stressLevel(v: number): string {
  return v <= 25 ? 'Rest' : v <= 50 ? 'Low' : v <= 75 ? 'Medium' : 'High'
}

export default function Stress({ day, size, isToday }: CardProps) {
  const q = useDay(day)
  const avg = q.data?.stress_avg ?? null
  const max = q.data?.stress_max ?? null
  return (
    <Body q={[q]} empty={avg == null} isToday={isToday} size={size}>
      {() => (
        <div className="flex h-full flex-col justify-end gap-3">
          <div className="flex items-end justify-between gap-3">
            <Kpi value={n(avg)}>{stressLevel(avg!)} · average</Kpi>
            {max != null && <dl className="m-0 text-right"><Stat label="Peak" value={n(max)} /></dl>}
          </div>
          <Meter value={avg} max={100} color="var(--amber)" label={`Average stress ${n(avg)} of 100`} />
        </div>
      )}
    </Body>
  )
}
