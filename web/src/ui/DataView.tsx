// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useState } from 'react'

/** words turns a key into words: "lastUpdateDateTime" and "last_update" read as people write. */
export function words(k: string): string {
  const s = k.replace(/_/g, ' ').replace(/([a-z0-9])([A-Z])/g, '$1 $2').replace(/\s+/g, ' ').trim()
  return s ? s[0].toUpperCase() + s.slice(1).toLowerCase() : k
}

const isObj = (v: unknown): v is Record<string, unknown> => v != null && typeof v === 'object' && !Array.isArray(v)

function scalar(v: unknown): string {
  if (v == null) return '–'
  if (typeof v === 'boolean') return v ? 'yes' : 'no'
  if (typeof v === 'number') return Number.isInteger(v) ? v.toLocaleString() : String(Math.round(v * 1000) / 1000)
  return String(v)
}

/** DataView shows any JSON value readably: an object as names and values, a list of objects as a
 * table, a list of plain values as a line, nested parts folded. depth limits how far it opens. */
export function DataView({ value, depth = 0 }: { value: unknown; depth?: number }) {
  if (Array.isArray(value)) {
    if (value.length === 0) return <span className="text-faint">none</span>
    if (value.every(isObj)) return <Table rows={value as Record<string, unknown>[]} depth={depth} />
    return <span className="break-words">{value.map(scalar).join(', ')}</span>
  }
  if (isObj(value)) {
    const entries = Object.entries(value)
    if (!entries.length) return <span className="text-faint">empty</span>
    return (
      <dl className="m-0 grid grid-cols-[minmax(110px,auto)_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-[13px]">
        {entries.map(([k, v]) => (
          <div key={k} className="contents">
            <dt className="text-muted">{words(k)}</dt>
            <dd className="m-0 min-w-0 break-words text-text">
              {isObj(v) || (Array.isArray(v) && v.some((x) => x != null && typeof x === 'object'))
                ? <Fold label={Array.isArray(v) ? `${v.length} ${v.length === 1 ? 'entry' : 'entries'}` : `${Object.keys(v as object).length} values`} open={depth < 1}>
                    <DataView value={v} depth={depth + 1} />
                  </Fold>
                : <DataView value={v} depth={depth + 1} />}
            </dd>
          </div>
        ))}
      </dl>
    )
  }
  return <span>{scalar(value)}</span>
}

function Fold({ label, open, children }: { label: string; open: boolean; children: React.ReactNode }) {
  return (
    <details open={open}>
      <summary className="cursor-pointer text-[12px] text-muted">{label}</summary>
      <div className="mt-1.5 border-l border-border pl-3">{children}</div>
    </details>
  )
}

const pageRows = 100

/** Table shows a list of objects: the keys they have as columns, a page of rows at a time. */
function Table({ rows, depth }: { rows: Record<string, unknown>[]; depth: number }) {
  const [shown, setShown] = useState(pageRows)
  const cols = [...new Set(rows.flatMap((r) => Object.keys(r)))]
  return (
    <div className="min-w-0">
      <div className="scroll-x max-w-full overflow-x-auto">
        <table className="w-max min-w-full border-collapse text-left text-[12px]">
          <thead>
            <tr>{cols.map((c) => <th key={c} className="whitespace-nowrap border-b border-border px-2 py-1.5 font-normal text-faint">{words(c)}</th>)}</tr>
          </thead>
          <tbody>
            {rows.slice(0, shown).map((r, i) => (
              <tr key={i} className="border-b border-border/60 align-top">
                {cols.map((c) => (
                  <td key={c} className="max-w-[320px] px-2 py-1.5">
                    {r[c] != null && typeof r[c] === 'object' ? <Fold label={Array.isArray(r[c]) ? `${(r[c] as unknown[]).length}` : '…'} open={false}><DataView value={r[c]} depth={depth + 1} /></Fold> : scalar(r[c])}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {rows.length > shown && (
        <button type="button" onClick={() => setShown((n) => n + pageRows)} className="mt-2 text-[12px] text-lime hover:underline">
          Show {Math.min(pageRows, rows.length - shown)} more of {rows.length}
        </button>
      )}
    </div>
  )
}
