// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMe } from '../../api/hooks/auth'
import { useDay } from '../../api/hooks/data'
import { Ring } from '../../charts'
import { distance } from '../../lib/format'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { Kpi, n, Stat } from './parts'

export default function Steps({ day, size, isToday }: CardProps) {
  const q = useDay(day)
  const units = useMe().data?.units ?? 'metric'
  const d = q.data
  const steps = d?.steps ?? null
  const goal = d?.values.step_goal ?? null
  const pct = steps != null && goal ? Math.round((steps / goal) * 100) : null
  const floors = d?.floors_up ?? null
  const floorsGoal = d?.floors_goal ?? null
  const ring = (px: number) => (
    <Ring value={steps ?? 0} max={goal ?? Math.max(steps ?? 0, 1)} size={px} stroke={px > 100 ? 9 : 7} label={`${n(steps)} steps${goal ? ` of ${n(goal)}` : ''}`}>
      <span className={`num leading-none ${px > 100 ? 'text-[22px]' : 'text-[15px]'}`}>{pct != null ? `${pct}%` : '–'}</span>
    </Ring>
  )
  return (
    <Body q={[q]} empty={steps == null} isToday={isToday} size={size}>
      {() =>
        size === 'S' ? (
          <div className="flex h-full items-end justify-between gap-2">
            <Kpi value={n(steps)}>{goal ? `of ${n(goal)}` : 'steps'}</Kpi>
            <div className="-mb-1 shrink-0">{ring(74)}</div>
          </div>
        ) : (
          <div className="flex h-full items-center gap-5">
            {ring(size === 'L' ? 150 : 124)}
            <dl className="m-0 grid gap-4">
              <Kpi value={n(steps)} unit="steps">{goal ? `goal ${n(goal)}` : ''}</Kpi>
              {floors != null && (
                <Stat label="Floors" value={n(floors)} unit={floorsGoal ? `of ${n(floorsGoal)}` : undefined}
                  tone={floorsGoal && floors >= floorsGoal ? 'lime' : undefined} />
              )}
              <Stat label="Distance" value={distance(d?.distance_m, units)} />
              {size === 'L' && <Stat label="Active" value={n(d?.active_kcal)} unit="kcal" />}
            </dl>
          </div>
        )
      }
    </Body>
  )
}
