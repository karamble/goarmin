// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Link } from 'react-router-dom'
import { useEffect } from 'react'
import { errorText } from '../../../api/client'
import { useDevice, useDeviceSummary, useSyncNow } from '../../../api/hooks/devices'
import type { PairView } from '../../../api/types'
import { errorHint, syncLine, toneDot, watchName } from '../../../lib/devices'
import { firstSyncDone } from '../../../lib/pairing'
import { Button, Notice, Spinner } from '../../../ui/kit'
import { useToast } from '../../../ui/Toast'
import { StepHeading } from './parts'

/** FirstSync follows the new watch's first sync, then says what arrived. */
export function FirstSync({ view, tz, onAnother }: { view: PairView; tz: string; onAnother: () => void }) {
  const id = view.device_id!
  const q = useDevice(id)
  const d = q.device
  const done = firstSyncDone(d, view)
  // The sync may wait for Bluetooth before it shows as running: ask often until it has ended.
  const { refetch } = q
  useEffect(() => {
    if (done) return
    const t = setInterval(() => void refetch(), 2000)
    return () => clearInterval(t)
  }, [done, refetch])
  const s = d?.sync
  return (
    <div className="grid gap-5">
      <div>
        <StepHeading>First sync</StepHeading>
        <p className="m-0 mt-2 text-[15px] text-muted">
          {d ? `${watchName(d)} is added.` : 'The watch is added.'} {done ? '' : 'This keeps going if you leave this page.'}
        </p>
      </div>
      {!d ? (
        <section className="card flex items-center gap-3 p-6"><Spinner /> <span className="text-muted">Loading the watch…</span></section>
      ) : done && s?.last_ok ? (
        <Arrived id={id} />
      ) : done ? (
        <Failed id={id} code={s?.error_code} older={!view.former_owner} />
      ) : (
        <Running d={d} tz={tz} />
      )}
      <div className="flex flex-wrap gap-3">
        {done && s?.last_ok && (
          <Link to="/" className="inline-flex items-center rounded-full border-2 border-lime bg-lime px-6 py-3 text-[15px] font-bold text-bg">
            Open your glance
          </Link>
        )}
        <Link to={`/devices/watches/${id}`}
          className="inline-flex items-center rounded-full border-2 border-[rgba(230,237,243,.35)] px-6 py-3 text-[15px] font-bold hover:border-lime hover:text-lime">
          Go to the watch
        </Link>
        <Button variant="quiet" onClick={onAnother}>Add another watch</Button>
      </div>
    </div>
  )
}

function Running({ d, tz }: { d: NonNullable<ReturnType<typeof useDevice>['device']>; tz: string }) {
  const s = d.sync
  const line = s?.running ? syncLine(d, tz) : { text: 'Waiting for Bluetooth to start the sync…', detail: '', tone: 'running' as const }
  return (
    <section className="card p-6" aria-live="polite">
      <p className="m-0 flex items-center gap-2 text-[15px]">
        <span aria-hidden className={`h-2 w-2 shrink-0 rounded-full ${toneDot[line.tone]}`} />
        {line.text}
      </p>
      <div role="progressbar" aria-label="Files fetched" aria-valuemin={0} aria-valuemax={s?.total || 1} aria-valuenow={s?.done ?? 0}
        className="mt-3 h-1.5 overflow-hidden rounded-full bg-track">
        <div className={`h-full rounded-full bg-lime transition-[width] duration-500 ${s?.total ? '' : 'w-1/6 animate-pulse'}`}
          style={s?.total ? { width: `${(s.done / s.total) * 100}%` } : undefined} />
      </div>
      <p className="m-0 mt-3 text-[13px] text-muted">The first sync fetches everything new on the watch; with older activities included it can take a few minutes.</p>
    </section>
  )
}

function Arrived({ id }: { id: number }) {
  const sum = useDeviceSummary(id).data
  return (
    <section className="card p-6" aria-live="polite">
      <p className="caps m-0 mb-3">Imported into your account</p>
      {!sum ? <Spinner /> : (
        <dl className="m-0 grid grid-cols-3 gap-4">
          {[['Activities', sum.activities], ['Days', sum.days], ['Nights', sum.nights]].map(([k, v]) => (
            <div key={k}>
              <dt className="font-mono text-[11px] uppercase tracking-[.14em] text-faint">{k}</dt>
              <dd className="m-0 font-mono text-[28px] text-text">{v}</dd>
            </div>
          ))}
        </dl>
      )}
      {sum?.first_day && <p className="m-0 mt-3 text-[13px] text-muted">Data from {sum.first_day} on.</p>}
    </section>
  )
}

function Failed({ id, code, older }: { id: number; code?: string; older: boolean }) {
  const sync = useSyncNow()
  const toast = useToast()
  return (
    <section className="card grid gap-4 p-6">
      <Notice tone={code === 'not_in_range' ? 'info' : 'error'}>
        {code === 'not_in_range' ? 'The watch was not in range for the first sync.' : 'The first sync did not work.'} {errorHint(code)}
      </Notice>
      <p className="m-0 text-[14px] text-muted">The watch is added anyway; goarmin tries again on its own.</p>
      <div>
        <Button variant="ghost" small disabled={sync.isPending}
          onClick={() => sync.mutate({ id, includeSynced: older }, { onError: (e) => toast({ tone: 'error', text: errorText(e) }) })}>
          Try again
        </Button>
      </div>
    </section>
  )
}
