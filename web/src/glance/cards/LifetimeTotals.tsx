// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMe } from '../../api/hooks/auth'
import { useTotals } from '../../api/hooks/training'
import { distance, hm } from '../../lib/format'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { sportIcon } from './sport'

/** LifetimeTotals is the watches' lifetime totals per sport. */
export default function LifetimeTotals({ size, isToday }: CardProps) {
  const q = useTotals()
  const units = useMe().data?.units ?? 'metric'
  const sports = q.data?.sports ?? []
  return (
    <Body q={[q]} empty={!sports.length} isToday={isToday} size={size} emptyText="The watch has not sent its totals yet">
      {() => (
        <ul className="m-0 grid list-none gap-2 p-0">
          {sports.slice(0, size === 'S' ? 2 : 4).map((t) => {
            const Icon = sportIcon(t.sport)
            return (
              <li key={t.sport} className="flex items-baseline gap-2.5 text-[13px]">
                <Icon size={15} strokeWidth={1.75} className="shrink-0 self-center text-faint" aria-hidden />
                <span className="num text-[18px] leading-none text-text">{t.distance_m ? distance(t.distance_m, units) : hm(t.timer_s)}</span>
                <span className="truncate text-muted">{t.sessions} × {t.sport.replace(/_/g, ' ')}</span>
              </li>
            )
          })}
        </ul>
      )}
    </Body>
  )
}
