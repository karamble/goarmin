// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useDay } from '../../api/hooks/data'
import { clock, fixed } from '../../lib/format'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { Kpi, Stat } from './parts'

const races = [
  { key: 'race_5k', label: '5K' },
  { key: 'race_10k', label: '10K' },
  { key: 'race_half', label: 'Half' },
  { key: 'race_marathon', label: 'Marathon' },
]

export default function Vo2max({ day, size, isToday, options }: CardProps<{ predictions: boolean }>) {
  const q = useDay(day)
  const v = q.data?.values ?? {}
  const vo2 = v.vo2max ?? null
  const shown = options.predictions && size !== 'S' ? races.filter((r) => v[r.key] != null) : []
  return (
    <Body q={[q]} empty={vo2 == null} isToday={isToday} size={size}>
      {() => (
        <div className="flex h-full flex-col justify-end gap-4">
          <Kpi value={fixed(vo2, 1)} unit="ml/kg/min" big={size !== 'S'}>VO2 max</Kpi>
          {shown.length > 0 && (
            <dl className="m-0 grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-4">
              {shown.map((r) => <Stat key={r.key} label={r.label} value={clock(v[r.key])} />)}
            </dl>
          )}
        </div>
      )}
    </Body>
  )
}
