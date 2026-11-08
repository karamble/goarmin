// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo } from 'react'
import { useDeviceDocuments, useSettingsChanges, useSolar } from '../../api/hooks/readers'
import type { Device } from '../../api/types'
import { describeSeries, timeSeries } from '../../charts'
import { timeOfDay } from '../../lib/format'
import { relDay, today } from '../../lib/period'
import { Button } from '../../ui/kit'
import { DataView, words } from '../../ui/DataView'
import { chartCtx, ChartCard } from '../health/parts'

/** SolarCard draws the watch's solar charge over its last 7 days with data, hour by hour (only
 * for a watch with a solar lens: others have no data and show nothing). */
export function SolarCard({ d, tz }: { d: Device; tz: string }) {
  const to = today(tz)
  const q = useSolar(d.id)
  const ctx = useMemo(() => chartCtx(tz), [tz])
  const s = q.data
  if (!s || !s.solar.t.length) return null
  const gainH = s.solar_gain.v.reduce((a, v) => a + v, 0) / 3_600_000
  return (
    <ChartCard title="Solar charge" hint={`Sun on the lens over its last 7 days with data, hourly${gainH > 0.05 ? `; it added about ${gainH.toFixed(1)} h of battery` : ''}.`}
      ariaLabel={describeSeries('Solar intensity', s.solar.v, '%')} height={200}
      option={timeSeries({ from: s.from, to: s.to, y: [{ min: 0 }], series: [{ id: 'solar', name: 'Solar intensity', t: s.solar.t, v: s.solar.v, kind: 'area', role: 'primary', unit: '%', gapS: 7200 }] }, ctx)}
      table={{ head: ['Hour', 'Intensity (%)'], rows: s.solar.t.map((t, i) => [`${relDay(today(tz, new Date(t * 1000)), to)} ${timeOfDay(t, tz)}`, s.solar.v[i].toFixed(1)]) }} />
  )
}

/** ChangesCard lists the watch's settings change log: what was changed on it, when. */
export function ChangesCard({ d, tz }: { d: Device; tz: string }) {
  const q = useSettingsChanges(d.id)
  const items = q.data?.pages.flatMap((p) => p.items) ?? []
  if (!items.length) return null
  return (
    <section className="card p-5">
      <h2 className="caps m-0">Settings history</h2>
      <p className="m-0 mb-3 mt-1 text-[13px] text-muted">What was changed on the watch, as its change log tells.</p>
      <ul className="m-0 list-none p-0">
        {items.map((c, i) => (
          <li key={`${c.ts}-${i}`} className="grid grid-cols-[110px_minmax(0,1fr)] gap-3 border-b border-border py-2 text-[13px] last:border-b-0">
            <span className="text-faint">{relDay(today(tz, new Date(c.ts * 1000)), today(tz))} {timeOfDay(c.ts, tz)}</span>
            <span className="min-w-0 break-words">
              <span className="text-muted">{words(c.message)} › </span>{words(c.field)}
              {c.value && <> → <span className="font-mono text-[12px] text-text">{c.value}</span></>}
              {c.sport && <span className="text-faint"> · {c.sport}</span>}
            </span>
          </li>
        ))}
      </ul>
      {q.hasNextPage && <div className="mt-3"><Button small onClick={() => void q.fetchNextPage()} disabled={q.isFetchingNextPage}>Load more</Button></div>}
    </section>
  )
}

const docTitles: Record<string, string> = { settings: 'Everything else in its settings', device: 'Its device file', multisport: 'Multisport settings' }

/** DocumentsCard shows what the watch's settings, device and multisport files hold beyond what
 * the pages read, message by message. */
export function DocumentsCard({ d }: { d: Device }) {
  const q = useDeviceDocuments(d.id)
  const docs = q.data ?? []
  if (!docs.length) return null
  return (
    <section className="card p-5">
      <h2 className="caps m-0">What else the watch keeps</h2>
      <p className="m-0 mb-3 mt-1 text-[13px] text-muted">Read from its files, named from the FIT profile.</p>
      <div className="grid gap-2">
        {docs.map((doc) => (
          <details key={doc.kind} className="rounded-xl border border-border px-4 py-3">
            <summary className="cursor-pointer text-[14px]">{docTitles[doc.kind] ?? words(doc.kind)} <span className="text-[12px] text-faint">· {Object.keys(doc.data).length} kinds of message</span></summary>
            <div className="mt-3 grid gap-3">
              {Object.entries(doc.data).map(([name, msgs]) => (
                <div key={name}>
                  <p className="m-0 mb-1 text-[13px] text-muted">{words(name)}</p>
                  <DataView value={msgs.length === 1 ? msgs[0] : msgs} depth={1} />
                </div>
              ))}
            </div>
          </details>
        ))}
      </div>
    </section>
  )
}
