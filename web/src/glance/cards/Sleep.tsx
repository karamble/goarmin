// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useDay, useNight } from '../../api/hooks/data'
import { Ring } from '../../charts'
import { hm, timeOfDay } from '../../lib/format'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { Kpi, n } from './parts'

const stages = [
  { key: 'deep_s', label: 'Deep', color: 'var(--cyan)' },
  { key: 'light_s', label: 'Light', color: 'var(--lime-dim)' },
  { key: 'rem_s', label: 'REM', color: 'var(--pink)' },
  { key: 'awake_s', label: 'Awake', color: 'var(--amber)' },
] as const

export default function Sleep({ day, size, isToday, tz }: CardProps) {
  const q = useNight(day)
  const d = useDay(day)
  const night = q.night
  const asleep = night ? (night.deep_s ?? 0) + (night.light_s ?? 0) + (night.rem_s ?? 0) : null
  const needMin = d.data?.values.sleep_need
  const total = night ? stages.reduce((a, s) => a + (night[s.key] ?? 0), 0) : 0
  return (
    <Body q={[q]} empty={!night} isToday={isToday} size={size} emptyText="No night recorded">
      {() =>
        size === 'S' ? (
          <div className="flex h-full items-end justify-between gap-3">
            <Kpi value={n(night!.score)}>score</Kpi>
            <div className="text-right">
              <div className="num text-[17px]">{hm(asleep)}</div>
              <div className="text-[12px] text-muted">asleep</div>
            </div>
          </div>
        ) : (
          <div className="flex h-full flex-col gap-3">
            <div className="flex flex-1 items-center gap-5">
              <Ring value={night!.score} max={100} size={size === 'L' ? 140 : 120} stroke={9} label={`Sleep score ${n(night!.score)}`}>
                <span className="num text-[36px] leading-none">{n(night!.score)}</span>
                <span className="caps mt-1 !text-[10px]">score</span>
              </Ring>
              <div className="min-w-0 flex-1">
                <div className="num text-[26px] leading-none">{hm(asleep)}</div>
                <div className="mt-1.5 text-[12px] text-muted">
                  {needMin ? <>need {hm(needMin * 60)}</> : 'asleep'}
                </div>
                <div className="mt-3 font-mono text-[12px] text-muted">
                  {timeOfDay(night!.start, tz)} <span className="text-faint">→</span> {timeOfDay(night!.end, tz)}
                </div>
              </div>
            </div>
            {total > 0 && (
              <div>
                <div className="flex h-2 w-full gap-[2px] overflow-hidden rounded-full" role="img"
                  aria-label={stages.map((s) => `${s.label} ${hm(night![s.key])}`).join(', ')}>
                  {stages.map((s) => (night![s.key] ?? 0) > 0 && (
                    <span key={s.key} style={{ flexGrow: night![s.key]!, background: s.color }} />
                  ))}
                </div>
                <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1">
                  {stages.map((s) => (
                    <span key={s.key} className="inline-flex items-center gap-1.5 text-[11px] text-muted">
                      <span aria-hidden className="h-2 w-2 rounded-[2px]" style={{ background: s.color }} />
                      {s.label} <span className="num text-text">{hm(night![s.key])}</span>
                    </span>
                  ))}
                </div>
              </div>
            )}
          </div>
        )
      }
    </Body>
  )
}
