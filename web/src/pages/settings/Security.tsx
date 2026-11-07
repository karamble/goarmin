// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { KeyRound, Trash2 } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { errorText } from '../../api/client'
import {
  useAddPasskey, useChangePassword, useDeletePasskey, useEndSession, usePasskeys, useSessions,
} from '../../api/hooks/auth'
import { passkeysAvailable } from '../../api/passkeys'
import { Button, Card, Field, Notice, Spinner } from '../../ui/kit'

const date = (s: string) => new Date(s).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })

function Passkeys() {
  const list = usePasskeys()
  const add = useAddPasskey()
  const del = useDeletePasskey()
  const [name, setName] = useState('')
  return (
    <Card className="p-6 sm:p-8">
      <p className="caps mb-1">Passkeys</p>
      <p className="mt-0 mb-5 text-sm text-muted">Sign in with your fingerprint, face or a security key, without a password.</p>
      {list.isPending ? <Spinner /> : (
        <ul className="m-0 mb-5 grid list-none gap-2 p-0">
          {list.data?.map((p) => (
            <li key={p.id} className="flex items-center justify-between gap-3 rounded-xl border border-border bg-bg2 px-4 py-3">
              <div className="min-w-0">
                <p className="m-0 truncate font-medium">{p.name}</p>
                <p className="m-0 text-[13px] text-faint">added {date(p.created_at)}{p.last_used_at ? ` · used ${date(p.last_used_at)}` : ''}</p>
              </div>
              <button type="button" aria-label={`Remove ${p.name}`} onClick={() => del.mutate(p.id)} className="text-muted hover:text-err">
                <Trash2 size={18} />
              </button>
            </li>
          ))}
          {list.data?.length === 0 && <li className="text-sm text-faint">No passkey yet.</li>}
        </ul>
      )}
      {passkeysAvailable() ? (
        <form className="flex flex-wrap items-end gap-3" onSubmit={(e) => { e.preventDefault(); add.mutate(name || 'passkey', { onSuccess: () => setName('') }) }}>
          <div className="min-w-[200px] flex-1"><Field label="Name" placeholder="e.g. laptop" value={name} onChange={(e) => setName(e.target.value)} /></div>
          <Button variant="primary" type="submit" disabled={add.isPending}><KeyRound size={18} /> Add a passkey</Button>
        </form>
      ) : (
        <Notice>Passkeys need https (or localhost) in the address bar.</Notice>
      )}
      {(add.error ?? del.error) && <div className="mt-4"><Notice tone="error">{errorText(add.error ?? del.error)}</Notice></div>}
    </Card>
  )
}

function Password() {
  const change = useChangePassword()
  const [cur, setCur] = useState('')
  const [pw, setPw] = useState('')
  const submit = (e: FormEvent) => {
    e.preventDefault()
    change.mutate({ current: cur, new: pw }, { onSuccess: () => { setCur(''); setPw('') } })
  }
  return (
    <Card className="p-6 sm:p-8">
      <p className="caps mb-5">Password</p>
      <form onSubmit={submit} className="grid gap-4">
        <Field label="Current password" type="password" autoComplete="current-password" value={cur} onChange={(e) => setCur(e.target.value)} />
        <Field label="New password" type="password" autoComplete="new-password" minLength={12} required value={pw} onChange={(e) => setPw(e.target.value)} />
        {change.error && <Notice tone="error">{errorText(change.error)}</Notice>}
        {change.isSuccess && <Notice tone="ok">Password changed.</Notice>}
        <div><Button type="submit" disabled={change.isPending}>Change password</Button></div>
      </form>
    </Card>
  )
}

function Sessions() {
  const list = useSessions()
  const end = useEndSession()
  return (
    <Card className="p-6 sm:p-8">
      <p className="caps mb-5">Signed-in browsers</p>
      {list.isPending ? <Spinner /> : (
        <ul className="m-0 grid list-none gap-2 p-0">
          {list.data?.map((s) => (
            <li key={s.id} className="flex items-center justify-between gap-3 rounded-xl border border-border bg-bg2 px-4 py-3">
              <div className="min-w-0">
                <p className="m-0 truncate text-sm">{s.user_agent || 'unknown browser'}</p>
                <p className="m-0 text-[13px] text-faint"><span className="num">{s.ip}</span> · last seen {date(s.last_seen)}</p>
              </div>
              {s.current ? <span className="badge-cut">This one</span> : (
                <Button small variant="danger" onClick={() => end.mutate(s.id)}>Sign out</Button>
              )}
            </li>
          ))}
        </ul>
      )}
    </Card>
  )
}

export default function Security() {
  const welcome = useSearchParams()[0].get('welcome')
  return (
    <div className="grid gap-6">
      {welcome && (
        <Notice tone="ok">
          Your account is ready. Add a passkey to sign in without a password next time, or <Link to="/" className="text-lime underline">continue to your glance</Link>.
        </Notice>
      )}
      <div className="grid gap-6 lg:grid-cols-2">
        <Passkeys />
        <Password />
      </div>
      <Sessions />
    </div>
  )
}
