// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { ChevronDown } from 'lucide-react'
import { useState } from 'react'
import type { LogRun } from '../../api/types'
import { errorHint } from '../../lib/devices'
import { bytes, timeOfDay } from '../../lib/format'
import { relDay, today as todayIn } from '../../lib/period'

const by = { schedule: 'scheduled', manual: 'Sync now', wizard: 'first sync', delete: 'delete', library: 'workout sent' } as const

/** A run, or several not-in-range runs folded into one. */
export interface LogEntry {
  run: LogRun
  /** Runs folded into this one (not in range in a row), oldest last. */
  folded: LogRun[]
}

/** fold folds consecutive not-in-range runs of the same watch into one entry. */
export function fold(runs: LogRun[]): LogEntry[] {
  const out: LogEntry[] = []
  for (const r of runs) {
    const prev = out.at(-1)
    if (prev && r.error_code === 'not_in_range' && prev.run.error_code === 'not_in_range' && prev.run.device_id === r.device_id) {
      prev.folded.push(r)
      continue
    }
    out.push({ run: r, folded: [] })
  }
  return out
}

function dayOf(unix: number, tz: string): string {
  return todayIn(tz, new Date(unix * 1000))
}

/** RunRow is one line of the sync log; a failed one opens to its reason. */
export function RunRow({ entry, watch, tz }: { entry: LogEntry; watch?: string; tz: string }) {
  const [open, setOpen] = useState(false)
  const r = entry.run
  const away = r.error_code === 'not_in_range'
  const n = entry.folded.length + 1
  const oldest = entry.folded.at(-1) ?? r
  const today = todayIn(tz)
  const when = `${relDay(dayOf(r.started_at, tz), today)} ${timeOfDay(r.started_at, tz)}`
  const span = n > 1 ? `${timeOfDay(oldest.started_at, tz)}–${timeOfDay(r.started_at, tz)}` : ''
  const failed = !r.ok && !away
  return (
    <li className="border-b border-border last:border-b-0">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 px-1 py-3">
        <span className={`badge-cut shrink-0 ${r.ok ? '' : away ? '!bg-track !text-muted' : '!bg-red !text-text'}`}>
          {r.ok ? 'OK' : away ? (n > 1 ? `Away ×${n}` : 'Away') : 'Failed'}
        </span>
        <span className="min-w-[150px] font-mono text-[12px] text-text">{n > 1 ? `${relDay(dayOf(r.started_at, tz), today)} ${span}` : when}</span>
        {watch && <span className="min-w-0 truncate text-[13px] text-muted">{watch}</span>}
        <span className="font-mono text-[11px] uppercase tracking-[.12em] text-faint">{by[r.started_by] ?? r.started_by}</span>
        <span className="ml-auto flex flex-wrap items-center gap-x-4 font-mono text-[12px] text-muted">
          {r.ok || r.pulled > 0 ? (
            <>
              <span title="files the watch listed → fetched → new in goarmin">{r.listed} → {r.pulled} → {r.imported}</span>
              {(r.archived > 0 || r.deleted > 0) && <span title="marked as synced · deleted on the watch">{r.archived} · {r.deleted}</span>}
              {r.bytes > 0 && <span>{bytes(r.bytes)}</span>}
              <span className="text-faint">{Math.max(0, r.finished_at - r.started_at)} s</span>
            </>
          ) : (
            <span className="text-faint">{away ? 'not seen in the scan' : errorHint(r.error_code).split('.')[0]}</span>
          )}
        </span>
        {failed && (
          <button type="button" onClick={() => setOpen(!open)} aria-expanded={open} aria-label="Why it failed"
            className="grid h-7 w-7 place-items-center rounded-lg text-muted hover:bg-panel2 hover:text-text">
            <ChevronDown size={15} className={`transition-transform ${open ? 'rotate-180' : ''}`} />
          </button>
        )}
      </div>
      {failed && open && (
        <div className="mb-3 rounded-xl bg-bg2 px-4 py-3">
          <p className="m-0 text-[13px] text-text">{errorHint(r.error_code) || 'The sync did not finish.'}</p>
          <p className="m-0 mt-2 break-words font-mono text-[11px] text-faint">{r.error}</p>
        </div>
      )}
    </li>
  )
}
