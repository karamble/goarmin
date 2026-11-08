// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Laptop, Pencil, Trash2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { errorText } from '../../api/client'
import { useMe } from '../../api/hooks/auth'
import { useApproveNode, useNodes, useRenameNode, useRevokeNode } from '../../api/hooks/nodes'
import type { SyncNode } from '../../api/types'
import { ago } from '../../lib/format'
import { Button, Skeleton } from '../../ui/kit'
import { Confirm } from '../../ui/Sheet'
import { useToast } from '../../ui/Toast'

/** Nodes is Devices › Sync nodes: the computers that sync watches near them for this server. */
export default function Nodes() {
  const q = useNodes()
  const nodes = q.data ?? []
  const live = nodes.filter((n) => !n.revoked)
  const revoked = nodes.filter((n) => n.revoked)
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
      <section className="card p-5 sm:p-6">
        <h2 className="m-0 font-display text-[22px] uppercase">Sync a watch from another computer</h2>
        <p className="m-0 mt-2 max-w-[720px] text-[14px] text-muted">
          A sync node is a computer near your watch (with the watch paired over Bluetooth) that syncs it for this
          server. It hands every file over before the watch may let go of it, and keeps syncing while this server is
          away. On that computer, in the goarmin checkout:
        </p>
        <pre className="mt-3 overflow-x-auto rounded-xl border border-border bg-bg2 px-4 py-3 text-[13px]"><code>{`make node-login SERVER=${location.origin} EMAIL=you@example.com
make node-start`}</code></pre>
        <p className="m-0 mt-2 text-[13px] text-muted">
          Without an e-mail (an account that signs in with passkeys only) the computer shows a code to approve here.
        </p>
        <Approve />
      </section>

      {q.isPending ? <Skeleton className="h-36 rounded-[18px]" /> : live.length === 0 ? (
        <p className="card m-0 px-5 py-4 text-[14px] text-muted">No sync node yet.</p>
      ) : (
        <ul className="m-0 grid list-none gap-3 p-0 md:grid-cols-2">
          {live.map((n) => <NodeCard key={n.id} n={n} />)}
        </ul>
      )}
      {revoked.length > 0 && (
        <details className="card px-5 py-4">
          <summary className="cursor-pointer text-[14px] text-muted">Signed out ({revoked.length})</summary>
          <ul className="m-0 mt-3 grid list-none gap-1 p-0 text-[13px] text-muted">
            {revoked.map((n) => <li key={n.id}>{n.name}{n.hostname && n.hostname !== n.name ? ` (${n.hostname})` : ''}, since {new Date(n.created_at * 1000).toLocaleDateString()}</li>)}
          </ul>
        </details>
      )}
    </div>
  )
}

function Approve() {
  const [code, setCode] = useState('')
  const approve = useApproveNode()
  const toast = useToast()
  const submit = (e: FormEvent) => {
    e.preventDefault()
    approve.mutate(code, {
      onSuccess: (n: SyncNode) => { toast({ text: `${n.name} is signed in` }); setCode('') },
      onError: (err) => toast({ tone: 'error', text: errorText(err) }),
    })
  }
  return (
    <form onSubmit={submit} className="mt-4 flex flex-wrap items-end gap-2">
      <label className="grid gap-1 text-[13px]">
        <span className="text-muted">Approve a code</span>
        <input value={code} onChange={(e) => setCode(e.target.value.toUpperCase())} placeholder="ABCD-EFGH" maxLength={9}
          autoComplete="off" spellCheck={false}
          className="h-10 w-[160px] rounded-xl border border-border bg-bg2 px-3 font-mono tracking-[.15em] text-text outline-none focus:border-lime" />
      </label>
      <Button type="submit" variant="primary" disabled={code.replace('-', '').length !== 8 || approve.isPending}>Approve</Button>
    </form>
  )
}

function NodeCard({ n }: { n: SyncNode }) {
  const me = useMe().data
  const [renaming, setRenaming] = useState(false)
  const [name, setName] = useState(n.name)
  const [confirm, setConfirm] = useState(false)
  const rename = useRenameNode()
  const revoke = useRevokeNode()
  const toast = useToast()
  const seen = n.last_seen ? ago(n.last_seen, Date.now()) : 'never'
  const adapter = n.adapter
  return (
    <li className="card grid gap-3 p-5">
      <div className="flex items-start gap-3">
        <Laptop size={22} className="mt-0.5 shrink-0 text-muted" aria-hidden />
        <div className="min-w-0 flex-1">
          {renaming ? (
            <form className="flex gap-2" onSubmit={(e) => {
              e.preventDefault()
              rename.mutate({ id: n.id, name }, { onSuccess: () => setRenaming(false), onError: (err) => toast({ tone: 'error', text: errorText(err) }) })
            }}>
              <input autoFocus value={name} onChange={(e) => setName(e.target.value)} maxLength={60} aria-label="Name"
                className="h-9 min-w-0 flex-1 rounded-lg border border-border bg-bg2 px-2 text-[14px] outline-none focus:border-lime" />
              <Button type="submit" small variant="primary">Save</Button>
            </form>
          ) : (
            <p className="m-0 truncate text-[16px] text-text">{n.name}</p>
          )}
          <p className="m-0 mt-0.5 flex items-center gap-2 text-[13px] text-muted">
            <span className={`inline-block size-2 rounded-full ${n.online ? 'bg-lime' : 'bg-faint'}`} aria-hidden />
            {n.online ? 'Online' : `Offline, last heard from ${seen}`}
            {n.hostname && n.hostname !== n.name ? ` · ${n.hostname}` : ''}
            {n.version ? ` · ${n.version}` : ''}
          </p>
          {me?.is_admin && me.id !== n.user_id && <p className="m-0 mt-0.5 text-[12px] text-faint">Another account's computer</p>}
        </div>
        {!renaming && (
          <button type="button" onClick={() => setRenaming(true)} aria-label={`Rename ${n.name}`} className="rounded-lg p-1.5 text-muted hover:text-lime">
            <Pencil size={15} />
          </button>
        )}
      </div>
      {adapter && (
        <p className="m-0 text-[13px] text-muted">
          Bluetooth {adapter.name}: {adapter.power_state === 'on' || adapter.powered ? 'on' : adapter.power_state || 'off'}
          {adapter.busy ? ` · busy (${adapter.busy.kind})` : ''}
        </p>
      )}
      <div className="text-[13px]">
        <p className="caps m-0 !text-[10px] !text-faint">Watches it syncs</p>
        {n.watches.length === 0 ? <p className="m-0 mt-1 text-muted">{n.online ? 'None yet' : 'Shown once it is online'}</p> : (
          <ul className="m-0 mt-1 grid list-none gap-0.5 p-0">
            {n.watches.map((w) => <li key={w.address}>{w.name || 'Watch'} <span className="font-mono text-[12px] text-faint">{w.address}</span></li>)}
          </ul>
        )}
      </div>
      <div className="flex justify-end">
        <Button small variant="danger" onClick={() => setConfirm(true)}><Trash2 size={14} /> Sign out</Button>
      </div>
      {confirm && (
        <Confirm title={`Sign out ${n.name}?`} confirm="Sign out" danger onClose={() => setConfirm(false)}
          onConfirm={() => revoke.mutate(n.id, {
            onSuccess: () => { setConfirm(false); toast({ text: `${n.name} is signed out` }) },
            onError: (err) => toast({ tone: 'error', text: errorText(err) }),
          })}>
          The computer stops syncing for this server at once. Files it fetched but did not hand over yet stay on it and
          on the watch. To sync again, sign it in anew (make node-login).
        </Confirm>
      )}
    </li>
  )
}
