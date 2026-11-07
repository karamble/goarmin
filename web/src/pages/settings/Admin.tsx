// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Copy } from 'lucide-react'
import { useState } from 'react'
import { errorText } from '../../api/client'
import { useCreateInvite, useUsers } from '../../api/hooks/auth'
import { useMe } from '../../api/hooks/auth'
import { useAdminWatchAction, useAdminWatches } from '../../api/hooks/devices'
import { useAdminPairing, useEndPairing } from '../../api/hooks/pairing'
import type { AdminWatch } from '../../api/types'
import { ago, timeOfDay } from '../../lib/format'
import { Link } from 'react-router-dom'
import { Button, Card, Field, Notice, Spinner } from '../../ui/kit'
import { Confirm } from '../../ui/Sheet'
import { useToast } from '../../ui/Toast'

export default function Admin() {
  const users = useUsers()
  const invite = useCreateInvite()
  const [email, setEmail] = useState('')
  const [copied, setCopied] = useState(false)
  const copy = async (url: string) => {
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }
  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <Card className="p-6 sm:p-8">
        <p className="caps mb-1">Invite someone</p>
        <p className="mt-0 mb-5 text-sm text-muted">The link works once, for 7 days. Their health data stays theirs: admins never see it.</p>
        <form className="grid gap-4" onSubmit={(e) => { e.preventDefault(); setCopied(false); invite.mutate(email) }}>
          <Field label="Their email (optional)" type="email" value={email} onChange={(e) => setEmail(e.target.value)}
            hint="Bind the link to one address, or leave it empty." />
          <div><Button variant="primary" type="submit" disabled={invite.isPending}>Create link</Button></div>
        </form>
        {invite.data && (
          <div className="mt-5 grid gap-2">
            <Notice tone="ok">Send this link; it is shown only now.</Notice>
            <div className="flex items-center gap-2 rounded-xl border border-border bg-bg2 px-3 py-2">
              <code className="num min-w-0 flex-1 truncate text-[13px]">{invite.data.url}</code>
              <button type="button" onClick={() => copy(invite.data!.url)} aria-label="Copy the link" className="text-muted hover:text-lime"><Copy size={16} /></button>
            </div>
            {copied && <span className="text-[13px] text-lime">Copied.</span>}
          </div>
        )}
        {invite.error && <div className="mt-4"><Notice tone="error">{errorText(invite.error)}</Notice></div>}
      </Card>
      <Card className="p-6 sm:p-8">
        <p className="caps mb-5">Accounts</p>
        {users.isPending ? <Spinner /> : (
          <ul className="m-0 grid list-none gap-2 p-0">
            {users.data?.map((u) => (
              <li key={u.id} className="flex items-center justify-between rounded-xl border border-border bg-bg2 px-4 py-3">
                <div className="min-w-0">
                  <p className="m-0 truncate">{u.display_name || u.email}</p>
                  <p className="m-0 truncate text-[13px] text-faint">{u.email}</p>
                </div>
                {u.is_admin && <span className="badge-cut">Admin</span>}
              </li>
            ))}
          </ul>
        )}
      </Card>
      <AdminWatches />
    </div>
  )
}

/** AdminWatches lists every watch on the server with its owner; an admin can release one (a
 * household member who left) or forget its pairing. No health data is shown. */
function AdminWatches() {
  const me = useMe().data!
  const q = useAdminWatches()
  const act = useAdminWatchAction()
  const users = useUsers()
  const pairing = useAdminPairing(me.server.sync)
  const end = useEndPairing()
  const toast = useToast()
  const [ask, setAsk] = useState<{ w: AdminWatch; action: 'release' | 'forget' } | null>(null)
  const [forWho, setForWho] = useState(0)
  const p = pairing.data
  return (
    <Card className="p-6 sm:p-8 lg:col-span-2">
      <p className="caps mb-1">Watches on this server</p>
      <p className="mt-0 mb-5 text-sm text-muted">Who owns which watch. Releasing one ends its owner's time with it; their data stays theirs.</p>
      {p && (
        <div role="status" className="mb-4 flex flex-wrap items-center gap-3 rounded-xl border border-lime-dim bg-lime-fill px-4 py-3 text-[14px]">
          <span className="flex-1">
            {p.starter.name || p.starter.email} is adding a watch{p.owner.id !== p.starter.id ? ` for ${p.owner.name || p.owner.email}` : ''}
            <span className="text-muted"> · {p.state.replace('_', ' ')} · since {timeOfDay(p.started_at, me.timezone)}</span>
          </span>
          <button type="button" disabled={end.isPending}
            onClick={() => end.mutate(undefined, { onSuccess: () => toast({ text: 'The pairing session was ended.' }), onError: (e) => toast({ tone: 'error', text: errorText(e) }) })}
            className="rounded-full border border-red/50 px-3 py-1.5 text-[13px] text-err hover:border-err disabled:opacity-40">End it</button>
        </div>
      )}
      {me.server.sync && (users.data ?? []).length > 1 && (
        <div className="mb-5 flex flex-wrap items-end gap-3">
          <label className="field-label min-w-[220px]">
            Add a watch for
            <select className="field font-sans text-[15px] normal-case tracking-normal text-text" value={forWho}
              onChange={(e) => setForWho(Number(e.target.value))}>
              <option value={0}>Choose an account…</option>
              {(users.data ?? []).map((u) => <option key={u.id} value={u.id}>{u.display_name || u.email}{u.id === me.id ? ' (you)' : ''}</option>)}
            </select>
          </label>
          <Link to={forWho ? `/devices/add?for=${forWho}` : '#'} aria-disabled={!forWho}
            onClick={(e) => !forWho && e.preventDefault()}
            className={`rounded-full border px-4 py-2.5 text-[14px] ${forWho ? 'border-lime text-lime hover:bg-lime-fill' : 'pointer-events-none border-border text-faint'}`}>
            Open Add a watch
          </Link>
        </div>
      )}
      {q.isPending ? <Spinner /> : (q.data ?? []).length === 0 ? <p className="m-0 text-sm text-muted">No watches yet.</p> : (
        <ul className="m-0 grid list-none gap-2 p-0">
          {q.data!.map((w) => (
            <li key={w.id} className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl border border-border bg-bg2 px-4 py-3">
              <div className="min-w-[180px] flex-1">
                <p className="m-0">{w.model}{w.name && w.name.toLowerCase() !== w.model.toLowerCase() ? <span className="text-faint"> · {w.name}</span> : ''}</p>
                <p className="m-0 text-[13px] text-faint">{w.owner ? (w.owner.name || w.owner.email) : 'Nobody: waiting to be added'}</p>
              </div>
              <span className="text-[12px] text-muted">
                {w.paired ? 'paired here' : 'not paired here'}{w.last_end ? ` · last sync ${ago(w.last_end)}` : ''}
              </span>
              <div className="flex gap-2">
                {w.owner && (
                  <button type="button" onClick={() => setAsk({ w, action: 'release' })}
                    className="rounded-full border border-border px-3 py-1.5 text-[13px] hover:border-lime hover:text-lime">Release</button>
                )}
                {w.paired && (
                  <button type="button" onClick={() => setAsk({ w, action: 'forget' })}
                    className="rounded-full border border-red/50 px-3 py-1.5 text-[13px] text-err hover:border-err">Forget pairing</button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}
      {ask && (
        <Confirm title={ask.action === 'release' ? `Release ${ask.w.model}?` : `Forget the pairing with ${ask.w.model}?`}
          confirm={ask.action === 'release' ? 'Release' : 'Forget pairing'} danger onClose={() => setAsk(null)}
          onConfirm={() => act.mutate({ id: ask.w.id, action: ask.action }, {
            onSuccess: () => toast({ text: ask.action === 'release' ? 'Released. Whoever adds it next owns it.' : 'This computer forgot the watch.' }),
            onError: (e) => toast({ tone: 'error', text: errorText(e) }),
          })}>
          {ask.action === 'release'
            ? `${ask.w.owner?.name || ask.w.owner?.email} no longer owns it from now on; what it recorded stays theirs.`
            : 'This computer stops syncing it until someone adds it again.'}
        </Confirm>
      )}
    </Card>
  )
}
