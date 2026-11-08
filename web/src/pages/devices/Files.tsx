// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { ChevronRight } from 'lucide-react'
import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { useFiles } from '../../api/hooks/readers'
import { fitTypeName } from '../../lib/devices'
import { bytes, timeOfDay } from '../../lib/format'
import { relDay, today } from '../../lib/period'
import { Button, Skeleton } from '../../ui/kit'

/** The file types worth a filter, in the order people look for them. */
const typeFilter = [4, 32, 49, 68, 44, 72, 73, 79, 9, 61, 58, 41, 2, 3, 5, 6]

/** Files lists every file goarmin keeps for the account (from syncs, USB, uploads and exports),
 * newest first; each opens in the inspector. */
export default function Files() {
  const me = useMe().data!
  const [type, setType] = useState('')
  const q = useFiles(type)
  const items = q.data?.pages.flatMap((p) => p.items) ?? []
  return (
    <section className="card p-4 sm:p-5" aria-labelledby="files-title">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <h2 id="files-title" className="caps m-0 mr-auto">Stored files</h2>
        <label className="text-[13px] text-muted">
          <span className="sr-only">Type</span>
          <select value={type} onChange={(e) => setType(e.target.value)} className="field !w-auto !py-1.5 text-[13px]">
            <option value="">All types</option>
            {typeFilter.map((t) => <option key={t} value={t}>{fitTypeName(t)}</option>)}
            <option value="other">Not FIT (export files)</option>
          </select>
        </label>
      </div>
      <p className="m-0 mb-3 text-[13px] text-muted">
        Every file as it arrived, kept unchanged. Open one to see what it holds, message by message, named from the FIT profile.
      </p>
      {q.isPending ? <Skeleton className="h-40" /> : items.length === 0 ? (
        <p className="m-0 py-6 text-center text-[14px] text-muted">No files of this type.</p>
      ) : (
        <ul className="m-0 list-none p-0">
          {items.map((f) => (
            <li key={f.id}>
              <Link to={`/devices/files/${f.id}`} className="flex items-center gap-3 border-b border-border px-1 py-2.5 last:border-b-0 hover:bg-panel2">
                <span className="w-[150px] shrink-0 truncate text-[13px]">{f.fit_type != null ? fitTypeName(f.fit_type) : 'Export file'}</span>
                <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-muted" title={f.name}>{f.name.split('/').pop()}</span>
                <span className="hidden w-[150px] shrink-0 text-[12px] text-faint sm:block">
                  {f.created ? `${relDay(today(me.timezone, new Date(f.created * 1000)), today(me.timezone))} ${timeOfDay(f.created, me.timezone)}` : f.source}
                </span>
                <span className="num w-[64px] shrink-0 text-right text-[12px] text-muted">{bytes(f.size)}</span>
                <ChevronRight size={14} className="shrink-0 text-faint" />
              </Link>
            </li>
          ))}
        </ul>
      )}
      {q.hasNextPage && <div className="mt-3"><Button small onClick={() => void q.fetchNextPage()} disabled={q.isFetchingNextPage}>Load more</Button></div>}
    </section>
  )
}
