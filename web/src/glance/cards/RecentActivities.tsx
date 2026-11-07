// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Link } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { useRecentActivities } from '../../api/hooks/data'
import { distance, hm, timeOfDay } from '../../lib/format'
import { relDay } from '../../lib/period'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { n } from './parts'
import { sportIcon } from './sport'

export default function RecentActivities({ day, today, size, isToday, tz, options }: CardProps<{ count?: number }>) {
  const q = useRecentActivities(50)
  const units = useMe().data?.units ?? 'metric'
  const rows = size === 'M' ? 3 : Math.min(Math.max(Number(options.count) || 4, 1), 6)
  const list = (q.data?.items ?? []).filter((a) => a.day <= day).slice(0, rows)
  return (
    <Body q={[q]} empty={list.length === 0} isToday={isToday} size={size} emptyText="No activities yet">
      {() => (
        <ul className="m-0 -mx-2 flex h-full list-none flex-col gap-1 overflow-hidden p-0">
          {list.map((a) => {
            const Icon = sportIcon(a.sport)
            return (
              <li key={a.id}>
                <Link to={`/training/activities/${a.id}`}
                  className="group flex items-center gap-3 rounded-xl px-2 py-2 transition-colors hover:bg-panel2">
                  <span className="grid h-9 w-9 shrink-0 place-items-center rounded-full border border-border text-muted group-hover:border-lime-dim group-hover:text-lime">
                    <Icon size={17} strokeWidth={1.75} />
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[14px] font-medium text-text">{a.name || a.sport_label}</span>
                    <span className="block truncate font-mono text-[11px] text-faint">
                      {relDay(a.day, today)} · {timeOfDay(a.start, tz)}{a.name && a.name.toLowerCase() !== a.sport_label.toLowerCase() ? ` · ${a.sport_label}` : ''}
                    </span>
                  </span>
                  <span className="grid shrink-0 grid-cols-[auto_auto] items-baseline gap-x-4 text-right">
                    <span className="num text-[14px]">{a.distance_m ? distance(a.distance_m, units) : '–'}</span>
                    <span className="num text-[14px]">{hm(a.timer_s ?? a.elapsed_s)}</span>
                    {size !== 'M' && (
                      <span className="col-span-2 font-mono text-[11px] text-faint">{a.avg_hr ? `${n(a.avg_hr)} bpm avg` : ''}</span>
                    )}
                  </span>
                </Link>
              </li>
            )
          })}
        </ul>
      )}
    </Body>
  )
}
