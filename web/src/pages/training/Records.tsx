// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Link } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { useRecords } from '../../api/hooks/readers'
import type { PersonalRecordRow } from '../../api/types'
import { clock, distance, elevation, int } from '../../lib/format'
import { relDay, today } from '../../lib/period'
import { Skeleton } from '../../ui/kit'

/** recordValue shows a record in its unit: Garmin Connect gives times in seconds, distances
 * and climbs in metres, steps and streaks as counts. */
export function recordValue(r: Pick<PersonalRecordRow, 'metric' | 'value'>, units: 'metric' | 'imperial'): string {
  const v = r.value
  if (v == null) return '–'
  const m = r.metric.toLowerCase()
  if (m.startsWith('best ')) return clock(v)
  if (m.startsWith('farthest') || m.startsWith('longest pool')) return distance(v, units)
  if (m.includes('elevation')) return elevation(v, units)
  if (m.includes('streak')) return `${int(v)} days`
  return int(v)
}

const groups: { title: string; test: (m: string) => boolean }[] = [
  { title: 'Running', test: (m) => m.includes('run') },
  { title: 'Swimming', test: (m) => m.includes('swim') },
  { title: 'Cycling', test: (m) => m.includes('cycl') || m.includes('ride') },
  { title: 'Steps and streaks', test: (m) => m.includes('steps') || m.includes('streak') },
  { title: 'Other', test: () => true },
]

/** Records lists the personal records: Garmin Connect's (from an export) and the watch's own. */
export default function Records() {
  const q = useRecords()
  const me = useMe().data!
  const units = me.units === 'imperial' ? 'imperial' : 'metric'
  if (q.isPending) return <Skeleton className="h-48 rounded-[18px]" />
  const all = q.data ?? []
  const named = all.filter((r) => r.source === 'json')
  const watch = all.filter((r) => r.source === 'fit')
  if (!all.length) {
    return (
      <div className="card px-6 py-10 text-center text-[14px] text-muted">
        No personal records yet. A Garmin Connect export brings them, and the watch keeps its own in a records file.
      </div>
    )
  }
  const seen = new Set<PersonalRecordRow>()
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
      {groups.map((g) => {
        const rows = named.filter((r) => !seen.has(r) && g.test(r.metric.toLowerCase()))
        rows.forEach((r) => seen.add(r))
        if (!rows.length) return null
        return (
          <section key={g.title} className="card p-5 sm:p-6" aria-labelledby={`rec-${g.title}`}>
            <h2 id={`rec-${g.title}`} className="caps m-0 mb-4">{g.title}</h2>
            <ul className="m-0 grid list-none gap-x-6 gap-y-5 p-0 sm:grid-cols-2 lg:grid-cols-3">
              {rows.map((r) => (
                <li key={r.metric} className="min-w-0">
                  <p className="caps m-0 !text-[10px] !text-faint">{r.metric}</p>
                  <p className="num m-0 mt-1.5 text-[26px] leading-none text-lime">{recordValue(r, units)}</p>
                  <p className="m-0 mt-1 truncate text-[12px] text-muted">
                    {relDay(new Date(r.ts * 1000).toISOString().slice(0, 10), today(me.timezone))}
                    {r.activity_id != null && <> · <Link to={`/training/activities/${r.activity_id}`} className="text-lime hover:underline">the activity</Link></>}
                  </p>
                </li>
              ))}
            </ul>
          </section>
        )
      })}
      {watch.length > 0 && (
        <details className="card px-5 py-4">
          <summary className="cursor-pointer text-[14px] text-muted">The watch's own records file ({watch.length})</summary>
          <p className="m-0 mt-2 text-[12px] text-faint">Garmin's record kinds by number, values as the watch stores them.</p>
          <table className="mt-2 w-full text-left text-[13px]">
            <thead className="text-faint"><tr><th className="font-normal">Kind</th><th className="font-normal">Sport</th><th className="font-normal">Value</th><th className="font-normal">When</th></tr></thead>
            <tbody>
              {watch.map((r) => (
                <tr key={r.metric + r.sport} className="border-t border-border">
                  <td className="py-1.5 font-mono text-[12px]">{r.metric}</td>
                  <td>{r.sport}</td>
                  <td className="num">{r.value ?? '–'}</td>
                  <td className="text-muted">{relDay(new Date(r.ts * 1000).toISOString().slice(0, 10), today(me.timezone))}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </details>
      )}
    </div>
  )
}
