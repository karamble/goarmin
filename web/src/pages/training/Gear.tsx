// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMe } from '../../api/hooks/auth'
import { useGear } from '../../api/hooks/readers'
import type { GearRow } from '../../api/types'
import { distance } from '../../lib/format'
import { Skeleton } from '../../ui/kit'

const typeWords: Record<string, string> = { shoes: 'Shoes', bike: 'Bike', other: 'Other', '1': 'Shoes', '2': 'Bike' }

/** Gear lists shoes, bikes and other gear: Garmin Connect's (with the distance of the activities
 * it was used in) and what the watch keeps. */
export default function Gear() {
  const q = useGear()
  const units = useMe().data?.units === 'imperial' ? 'imperial' : 'metric'
  if (q.isPending) return <Skeleton className="h-48 rounded-[18px]" />
  const all = q.data ?? []
  if (!all.length) {
    return <div className="card px-6 py-10 text-center text-[14px] text-muted">No gear yet. A Garmin Connect export brings it, and the watch keeps a gear file.</div>
  }
  // The same item from Garmin Connect and from the watch: one card, Garmin Connect's first.
  const byName = new Map<string, GearRow[]>()
  for (const g of [...all].sort((a, b) => (a.source === b.source ? 0 : a.source === 'json' ? -1 : 1))) {
    const k = g.name.trim().toLowerCase() || g.uuid
    byName.set(k, [...(byName.get(k) ?? []), g])
  }
  return (
    <ul className="m-0 grid list-none gap-4 p-0 md:grid-cols-2 xl:grid-cols-3">
      {[...byName.values()].map((items) => {
        const g = items[0]
        const d = g.data as { status?: string; begin?: string; max_meters?: number; default_for?: string[] }
        const used = g.distance_m ?? 0
        const max = d.max_meters ?? 0
        const retired = d.status === 'retired'
        return (
          <li key={g.uuid + g.source} className={`card flex flex-col gap-3 p-5 ${retired ? 'opacity-70' : ''}`}>
            <div>
              <p className="caps m-0 !text-[10px] !text-faint">{typeWords[g.type] ?? (g.type || 'Gear')}{retired ? ' · retired' : ''}</p>
              <h2 className="m-0 mt-1 truncate font-display text-[22px] uppercase leading-tight tracking-[.02em]">{g.name || 'Unnamed'}</h2>
              {(g.model || g.brand) && <p className="m-0 truncate text-[13px] text-muted">{[g.brand, g.model].filter(Boolean).join(' ')}</p>}
            </div>
            {g.uses != null && (
              <div>
                <p className="m-0 flex items-baseline justify-between gap-3">
                  <span className="num text-[24px] leading-none text-lime">{distance(used, units)}</span>
                  <span className="text-[12px] text-muted">{g.uses} activities{max > 0 ? ` · of ${distance(max, units)}` : ''}</span>
                </p>
                {max > 0 && (
                  <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-track" role="progressbar" aria-label="Distance used"
                    aria-valuemin={0} aria-valuemax={max} aria-valuenow={Math.min(used, max)}>
                    <div className={`h-full rounded-full ${used >= max ? 'bg-amber' : 'bg-lime'}`} style={{ width: `${Math.min(100, (used / max) * 100)}%` }} />
                  </div>
                )}
              </div>
            )}
            <p className="m-0 mt-auto text-[12px] text-faint">
              {d.begin ? `Since ${d.begin}` : ''}{d.default_for?.length ? ` · default for ${d.default_for.map((x) => x.replaceAll('_', ' ')).join(', ')}` : ''}
              {items.some((x) => x.source === 'fit') ? ' · on the watch' : ''}
            </p>
          </li>
        )
      })}
    </ul>
  )
}
