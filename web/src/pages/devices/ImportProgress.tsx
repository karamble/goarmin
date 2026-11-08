// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { FileArchive, Upload, Usb, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { errorText } from '../../api/client'
import { useCancelImport, useImports } from '../../api/hooks/imports'
import type { ImportRun } from '../../api/types'
import { bytes, int } from '../../lib/format'
import { elapsedWords, finished, importProgress, rateWords, reportLine } from '../../lib/imports'
import { Button } from '../../ui/kit'
import { useToast } from '../../ui/Toast'

/** RunningImports shows each import of a kind (uploaded files, or a watch on USB) that has not
 * ended yet, live: how far it has come, what it found so far and what it is reading, so a long
 * import (a Garmin Connect export) visibly works. When one it showed ends, a toast says how. */
export function RunningImports({ kind }: { kind: ImportRun['kind'] }) {
  const q = useImports(20)
  const toast = useToast()
  const mine = q.items.filter((i) => i.kind === kind)
  const running = mine.filter((i) => !finished(i))
  const watched = useRef(new Set<number>())
  useEffect(() => {
    for (const i of mine) {
      if (!finished(i)) {
        watched.current.add(i.id)
      } else if (watched.current.delete(i.id)) {
        toast(i.status === 'done' ? { text: `Import done: ${reportLine(i.report ?? {})}` }
          : { tone: i.status === 'failed' ? 'error' : undefined, text: `Import ${i.status === 'failed' ? `failed${i.error ? `: ${i.error}` : ''}` : 'stopped'}` })
      }
    }
  }, [mine, toast])
  if (running.length === 0) return null
  return (
    <div className="grid gap-3">
      {running.map((i) => <RunningImport key={i.id} i={i} />)}
    </div>
  )
}

function useClock() {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(t)
  }, [])
  return now
}

function RunningImport({ i }: { i: ImportRun }) {
  const now = useClock()
  const cancel = useCancelImport()
  const toast = useToast()
  const r = i.report ?? {}
  const p = importProgress(r)
  const running = i.status === 'running'
  const secs = i.started_at ? now / 1000 - i.started_at : 0
  const here = (r.known ?? 0) + (r.duplicates ?? 0) + (r.unchanged ?? 0)
  const Icon = i.kind === 'usb' ? Usb : i.source === 'gc_export' ? FileArchive : Upload
  const phase = i.status === 'uploading' ? 'Receiving the files…'
    : i.status === 'queued' ? 'Waiting for the import before it…'
    : p.read === 0 ? 'Opening…' : r.export ? 'Importing the Garmin Connect export' : 'Importing'
  const stats: { label: string; value: string; sub?: string; tone?: string }[] = [
    { label: 'Read', value: int(p.read), sub: p.found > 0 ? `of ${int(p.found)}` : undefined },
    { label: 'New', value: int(r.new ?? 0), tone: 'text-lime' },
    { label: 'Already here', value: int(here) },
    ...((r.kept ?? 0) > 0 ? [{ label: 'Kept for later', value: int(r.kept) }] : []),
    ...((r.failed ?? 0) > 0 ? [{ label: 'Failed', value: int(r.failed), tone: 'text-warn' }] : []),
    { label: 'Data', value: bytes(r.bytes ?? 0) },
  ]
  return (
    <section className="card border-lime-dim/60 p-4 sm:p-5" aria-label={`Import ${i.name || ''} in progress`}>
      <div className="flex items-start gap-3">
        <span className="relative mt-0.5 grid h-9 w-9 shrink-0 place-items-center rounded-full border border-lime-dim text-lime" aria-hidden>
          <Icon size={16} />
          <span className="absolute -right-0.5 -top-0.5 h-2.5 w-2.5 animate-pulse rounded-full bg-lime" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="caps m-0 !text-[10px] !text-lime">{phase}</p>
          <p className="m-0 mt-1 truncate text-[15px] text-text">{i.name || 'Uploaded files'}</p>
        </div>
        <div className="flex shrink-0 items-center gap-3">
          {running && <span className="num text-[13px] text-muted" title="Running for">{elapsedWords(secs)}</span>}
          {(i.status === 'queued' || running) && (
            <Button small onClick={() => cancel.mutate(i.id, { onError: (e) => toast({ tone: 'error', text: errorText(e) }) })}>
              <X size={14} /> Stop
            </Button>
          )}
        </div>
      </div>
      <div className="mt-4 h-2 overflow-hidden rounded-full bg-track" role="progressbar" aria-label="Import progress"
        aria-valuemin={0} aria-valuemax={100} aria-valuenow={p.pct ?? undefined} aria-valuetext={p.pct == null ? 'working' : `${p.pct} %`}>
        {p.pct == null || !running
          ? <div className="progress-unknown h-full rounded-full bg-lime/80" />
          : <div className="h-full rounded-full bg-lime transition-[width] duration-700" style={{ width: `${Math.max(2, p.pct)}%` }} />}
      </div>
      <div className="mt-1.5 flex justify-between gap-3 text-[12px] text-muted">
        <span>{p.pct != null && running ? `${p.pct} %` : ''}</span>
        <span>{running ? rateWords(p.read, secs) : ''}</span>
      </div>
      <dl className="m-0 mt-3 grid grid-cols-2 gap-x-4 gap-y-3 sm:grid-cols-3 lg:grid-cols-6" aria-live="off">
        {stats.map((s) => (
          <div key={s.label} className="min-w-0">
            <dt className="caps !text-[10px] !text-faint">{s.label}</dt>
            <dd className={`num m-0 mt-1 truncate text-[18px] leading-none ${s.tone ?? 'text-text'}`}>{s.value}</dd>
            {s.sub && <dd className="num m-0 mt-1 truncate text-[11px] text-muted">{s.sub}</dd>}
          </div>
        ))}
      </dl>
      {running && r.current && (
        <p className="m-0 mt-3 truncate font-mono text-[11px] text-faint" title={r.current}>{r.current}</p>
      )}
    </section>
  )
}
