// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useDay } from '../../api/hooks/data'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { readinessLevel } from '../../lib/training'
import { Kpi, Meter, n } from './parts'

export default function Readiness({ day, size, isToday }: CardProps) {
  const q = useDay(day)
  const v = q.data?.values.training_readiness ?? null
  return (
    <Body q={[q]} empty={v == null} isToday={isToday} size={size}>
      {() => (
        <div className="flex h-full flex-col justify-end gap-3">
          <Kpi value={n(v)}>{readinessLevel(v!)}</Kpi>
          <Meter value={v} max={100} label={`Training readiness ${n(v)} of 100`} />
        </div>
      )}
    </Body>
  )
}
