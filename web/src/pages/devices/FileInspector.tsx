// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { ArrowLeft, Download } from 'lucide-react'
import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { errorText } from '../../api/client'
import { useFileMessages } from '../../api/hooks/readers'
import type { NamedField } from '../../api/types'
import { useSectionTitle } from '../../app/section/HeaderSlot'
import { bytes } from '../../lib/format'
import { Button, Notice, Skeleton } from '../../ui/kit'

const page = 200

function show(f: NamedField): string {
  const v = Array.isArray(f.value) ? f.value.join(', ') : typeof f.value === 'boolean' ? (f.value ? 'yes' : 'no') : String(f.value)
  return f.units ? `${v} ${f.units}` : v
}

/** FileInspector shows what one stored FIT file holds: every message by name with its count, and
 * the messages with their fields named, scaled and in words. Unknown messages and fields keep
 * their numbers. */
export default function FileInspector() {
  const id = Number(useParams().id)
  const [num, setNum] = useState<number | null>(null)
  const [offset, setOffset] = useState(0)
  const q = useFileMessages(id, num, offset, page)
  const f = q.data?.file
  useSectionTitle(f ? f.type_name : 'File')
  if (q.isPending) return <Skeleton className="h-64 rounded-[18px]" />
  if (q.error) {
    return (
      <div className="grid gap-3">
        <Link to="/devices/files" className="inline-flex w-fit items-center gap-1.5 text-[13px] text-muted hover:text-text"><ArrowLeft size={14} /> All files</Link>
        <Notice tone="error">{errorText(q.error)}</Notice>
      </div>
    )
  }
  const d = q.data!
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
      <Link to="/devices/files" className="inline-flex w-fit items-center gap-1.5 text-[13px] text-muted hover:text-text"><ArrowLeft size={14} /> All files</Link>
      <section className="card flex flex-wrap items-start gap-4 p-5">
        <div className="min-w-0 flex-1">
          <p className="caps m-0 !text-[10px] !text-faint">FIT type {d.file.fit_type} · {d.file.source}</p>
          <p className="m-0 mt-1 break-all font-mono text-[13px]">{d.file.name}</p>
          <p className="m-0 mt-1 text-[13px] text-muted">{bytes(d.file.size)} · {d.counts.reduce((a, c) => a + c.count, 0).toLocaleString()} messages</p>
        </div>
        <a href={`/api/files/${id}/download`} download className="inline-flex items-center gap-1.5 rounded-full border border-border px-3.5 py-1.5 text-[13px] hover:border-lime hover:text-lime">
          <Download size={14} /> Download
        </a>
      </section>
      {d.error && <Notice tone="error">The file breaks off: {d.error}. What came before is shown.</Notice>}
      <section className="card p-5">
        <h2 className="caps m-0 mb-3">Messages</h2>
        <div className="flex flex-wrap gap-2">
          <button type="button" aria-pressed={num == null} onClick={() => { setNum(null); setOffset(0) }}
            className={`rounded-full border px-3 py-1 text-[12px] ${num == null ? 'border-lime-dim bg-lime-fill text-lime' : 'border-border text-muted hover:text-text'}`}>All</button>
          {d.counts.map((c) => (
            <button key={c.num} type="button" aria-pressed={num === c.num} onClick={() => { setNum(c.num); setOffset(0) }}
              title={`message ${c.num}${c.known ? (c.official ? '' : ', from the community profile') : ', unknown'}`}
              className={`rounded-full border px-3 py-1 text-[12px] ${num === c.num ? 'border-lime-dim bg-lime-fill text-lime' : 'border-border text-muted hover:text-text'} ${c.known ? '' : 'italic'}`}>
              {c.name} <span className="num text-text">{c.count}</span>
            </button>
          ))}
        </div>
      </section>
      <ol className="m-0 grid list-none gap-3 p-0" start={offset + 1}>
        {d.messages.map((m, i) => (
          <li key={offset + i} className="card p-4">
            <p className="m-0 mb-2 flex items-baseline gap-2 text-[13px]">
              <span className="num text-faint">#{offset + i + 1}</span>
              <span className={m.known ? 'text-text' : 'italic text-muted'}>{m.name}</span>
              <span className="font-mono text-[11px] text-faint">{m.num}{m.known && !m.official ? ' · community' : ''}</span>
            </p>
            <dl className="m-0 grid grid-cols-[minmax(120px,auto)_minmax(0,1fr)] gap-x-4 gap-y-1 text-[12px]">
              {(m.fields ?? []).map((fl) => (
                <div key={fl.num} className="contents">
                  <dt className={`font-mono ${fl.known ? 'text-muted' : 'italic text-faint'}`}>{fl.name}</dt>
                  <dd className="m-0 min-w-0 break-words font-mono text-text">{show(fl)}</dd>
                </div>
              ))}
            </dl>
            {!m.fields?.length && <p className="m-0 text-[12px] text-faint">No field with a value.</p>}
          </li>
        ))}
      </ol>
      {(offset > 0 || offset + page < d.total) && (
        <div className="flex items-center gap-3">
          <Button small disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - page))}>Earlier</Button>
          <span className="text-[13px] text-muted">{offset + 1}–{Math.min(offset + page, d.total)} of {d.total.toLocaleString()}</span>
          <Button small disabled={offset + page >= d.total} onClick={() => setOffset(offset + page)}>Later</Button>
        </div>
      )}
    </div>
  )
}
