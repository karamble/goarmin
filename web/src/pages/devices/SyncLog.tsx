// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { useDevices } from '../../api/hooks/data'
import { useSyncLog } from '../../api/hooks/devices'
import type { LogRun } from '../../api/types'
import { watchName } from '../../lib/devices'
import { timeOfDay } from '../../lib/format'
import { addDays, shortDay, today } from '../../lib/period'
import { Skeleton, Spinner } from '../../ui/kit'
import { fold, RunRow } from './RunRow'

/** SyncLog is every sync of the person's watches, newest first. */
export default function SyncLog() {
  const me = useMe().data!
  const tz = me.timezone
  const [q, setQ] = useSearchParams()
  const watch = Number(q.get('watch')) || 0
  const problems = q.get('problems') === '1'
  const devices = useDevices().data ?? []
  const names = new Map(devices.map((d) => [d.id, watchName(d)]))
  const log = useSyncLog(watch, problems, 50)
  const strip = useSyncLog(watch, false, 200)
  const runs = log.data?.pages.flatMap((p) => p.items) ?? []
  const set = (k: string, v: string) => setQ((p) => { const n = new URLSearchParams(p); if (v) n.set(k, v); else n.delete(k); return n }, { replace: true })
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
      <div className="flex flex-wrap items-center gap-3">
        {devices.length > 1 && (
          <label className="field-label !flex-row items-center gap-2">
            Watch
            <select value={watch} onChange={(e) => set('watch', e.target.value === '0' ? '' : e.target.value)}
              className="field !w-auto !py-2 font-sans text-[14px] normal-case tracking-normal text-text">
              <option value={0}>All</option>
              {devices.map((d) => <option key={d.id} value={d.id}>{watchName(d)}</option>)}
            </select>
          </label>
        )}
        <label className="inline-flex items-center gap-2 text-[13px] text-muted">
          <input type="checkbox" checked={problems} onChange={(e) => set('problems', e.target.checked ? '1' : '')} className="accent-[var(--lime)]" />
          Problems only
        </label>
      </div>
      <section className="card p-4 sm:p-5">
        <h2 className="caps m-0 mb-3">The last 14 days</h2>
        {strip.isPending ? <Skeleton className="h-10" /> : <HealthStrip runs={strip.data?.pages[0]?.items ?? []} tz={tz} />}
      </section>
      <section className="card px-4 py-1 sm:px-5">
        {log.isPending ? <div className="py-4"><Skeleton className="h-24" /></div> : runs.length === 0 ? (
          <p className="m-0 py-8 text-center text-[14px] text-muted">{problems ? 'No problems. Every sync went through.' : 'No syncs yet.'}</p>
        ) : (
          <ul className="m-0 list-none p-0">
            {fold(runs).map((e) => <RunRow key={e.run.id} entry={e} watch={devices.length > 1 ? names.get(e.run.device_id) : undefined} tz={tz} />)}
          </ul>
        )}
        {log.hasNextPage && (
          <div className="py-3 text-center">
            <button type="button" onClick={() => void log.fetchNextPage()} disabled={log.isFetchingNextPage}
              className="rounded-full border border-border px-4 py-2 text-[13px] hover:border-lime hover:text-lime">
              {log.isFetchingNextPage ? <Spinner label="Loading" /> : 'Load more'}
            </button>
          </div>
        )}
      </section>
    </div>
  )
}

/** HealthStrip draws the last 14 days of syncs: lime went through, red failed, grey not in range. */
function HealthStrip({ runs, tz }: { runs: LogRun[]; tz: string }) {
  const days = useMemo(() => Array.from({ length: 14 }, (_, i) => addDays(today(tz), i - 13)), [tz])
  const byDay = new Map<string, LogRun[]>()
  for (const r of runs) {
    const d = today(tz, new Date(r.started_at * 1000))
    byDay.set(d, [...(byDay.get(d) ?? []), r])
  }
  return (
    <ol className="m-0 grid list-none grid-cols-7 gap-1.5 p-0 sm:grid-cols-14" aria-label="Syncs per day, the last 14 days">
      {days.map((d) => {
        const list = (byDay.get(d) ?? []).slice().reverse()
        const ok = list.filter((r) => r.ok).length
        const failed = list.filter((r) => !r.ok && r.error_code !== 'not_in_range').length
        const away = list.length - ok - failed
        return (
          <li key={d} className="rounded-lg border border-border bg-bg2 px-1.5 py-1.5"
            aria-label={`${shortDay(d)}: ${ok} went through, ${failed} failed, ${away} not in range`}>
            <span className="block font-mono text-[10px] text-faint">{shortDay(d)}</span>
            <span className="mt-1 flex min-h-[10px] flex-wrap gap-[3px]">
              {list.slice(-24).map((r) => (
                <span key={r.id} title={`${timeOfDay(r.started_at, tz)} ${r.ok ? 'OK' : r.error_code === 'not_in_range' ? 'not in range' : 'failed'}`}
                  className={`h-1.5 w-1.5 rounded-full ${r.ok ? 'bg-lime' : r.error_code === 'not_in_range' ? 'bg-faint' : 'bg-red'}`} />
              ))}
            </span>
          </li>
        )
      })}
    </ol>
  )
}
