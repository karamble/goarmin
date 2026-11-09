// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useTotals } from '../../api/hooks/training'
import { sportIcon } from '../../glance/cards/sport'
import { distance, hm, type Units } from '../../lib/format'
import { Card } from '../health/parts'
import { sportWords } from '../../lib/workoutLibrary'

/** Lifetime shows the watches' own lifetime totals per sport (nothing before a watch sent them). */
export function Lifetime({ units, title = 'Since you started, as the watch counts' }: { units: Units; title?: string }) {
  const q = useTotals()
  const sports = q.data?.sports ?? []
  if (!sports.length) return null
  return (
    <Card title={title} hint={q.data!.watches > 1 ? `Summed over ${q.data!.watches} watches.` : undefined}>
      <ul className="m-0 grid list-none grid-cols-1 gap-3 p-0 sm:grid-cols-2 xl:grid-cols-4" aria-label="Lifetime totals">
        {sports.map((t) => {
          const Icon = sportIcon(t.sport)
          return (
            <li key={t.sport} className="flex items-start gap-3 rounded-xl border border-border p-3.5">
              <Icon size={18} strokeWidth={1.75} className="mt-0.5 shrink-0 text-faint" aria-hidden />
              <span className="min-w-0">
                <span className="block text-[14px] text-text">{sportWords(t.sport)}</span>
                <span className="num mt-1 block text-[22px] leading-none text-lime">{t.distance_m ? distance(t.distance_m, units) : hm(t.timer_s)}</span>
                <span className="mt-1 block text-[12px] text-muted">
                  {t.sessions} {t.sessions === 1 ? 'session' : 'sessions'} · {hm(t.timer_s)} · {t.kcal.toLocaleString()} kcal
                </span>
              </span>
            </li>
          )
        })}
      </ul>
    </Card>
  )
}
