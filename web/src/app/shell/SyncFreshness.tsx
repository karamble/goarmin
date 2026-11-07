// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'
import { useDevices } from '../../api/hooks/data'
import { keys } from '../../api/keys'
import { alerts } from '../../lib/devices'
import { ago } from '../../lib/format'

/** useSyncRefresh reloads health and training data whenever a watch finishes a sync. */
export function useSyncRefresh() {
  const qc = useQueryClient()
  const q = useDevices()
  const stamp = q.data ? q.data.map((d) => `${d.id}:${d.sync?.last_end ?? ''}`).join('|') : null
  const prev = useRef<string | null>(null)
  useEffect(() => {
    if (stamp == null) return
    if (prev.current != null && prev.current !== stamp) {
      void qc.invalidateQueries({ queryKey: keys.health })
      void qc.invalidateQueries({ queryKey: keys.training })
    }
    prev.current = stamp
  }, [stamp, qc])
}

/** SyncFreshness is the rail's "synced 12 min ago" line. */
export function SyncFreshness({ open }: { open: boolean }) {
  const q = useDevices()
  const synced = (q.data ?? []).filter((d) => d.current && d.sync)
  if (synced.length === 0) return null
  const running = synced.some((d) => d.sync!.running)
  const failing = synced.some((d) => d.sync!.failures > 0) || alerts(synced).length > 0
  const away = synced.every((d) => d.sync!.away)
  const lastEnd = Math.max(0, ...synced.map((d) => d.sync!.last_end ?? 0)) || undefined
  const lastGood = synced.every((d) => d.sync!.last_ok)
  const text = running ? 'Syncing…'
    : away ? 'Watch not in range'
    : lastEnd && lastGood ? `Synced ${ago(lastEnd)}`
    : lastEnd ? `Last try ${ago(lastEnd)}` : 'Not synced yet'
  const dot = running ? 'bg-lime animate-pulse' : failing ? 'bg-amber' : away ? 'bg-faint' : lastEnd ? 'bg-lime-dim' : 'bg-faint'
  return (
    <Link to="/devices/watches" title={open ? undefined : text}
      className={`flex h-9 items-center gap-3 rounded-xl px-[20px] text-[12px] text-muted hover:bg-panel hover:text-text ${open ? '' : 'justify-center px-0'}`}>
      <span aria-hidden className={`h-2 w-2 shrink-0 rounded-full ${dot}`} />
      {open ? <span className="truncate">{text}</span> : <span className="sr-only">{text}</span>}
    </Link>
  )
}
