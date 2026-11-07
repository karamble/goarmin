// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useState, type FormEvent } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { errorText } from '../../api/client'
import { useAcceptInvite, useInvite } from '../../api/hooks/auth'
import { Button, Field, Notice, Spinner } from '../../ui/kit'
import { AuthLayout } from './AuthLayout'

export default function Invite() {
  const token = useParams().token ?? ''
  const navigate = useNavigate()
  const invite = useInvite(token)
  const accept = useAcceptInvite(token)
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [pw, setPw] = useState('')
  if (invite.isPending) return <div className="grid min-h-dvh place-items-center"><Spinner /></div>
  if (invite.isError) {
    return (
      <AuthLayout eyebrow="Invitation" title="Link not valid">
        <Notice tone="error">{errorText(invite.error)}</Notice>
      </AuthLayout>
    )
  }
  const bound = invite.data.email
  const submit = (e: FormEvent) => {
    e.preventDefault()
    accept.mutate({ email: bound || email, name, password: pw }, { onSuccess: () => navigate('/settings/security?welcome=1', { replace: true }) })
  }
  return (
    <AuthLayout eyebrow="Invitation" title="Join goarmin">
      <form onSubmit={submit} className="grid gap-4">
        <Field label="Your name" autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} />
        <Field label="Email" type="email" autoComplete="username" required value={bound || email} readOnly={!!bound}
          onChange={(e) => setEmail(e.target.value)} hint={bound ? 'This invitation is for this address.' : undefined} />
        <Field label="Password" type="password" autoComplete="new-password" required minLength={12} value={pw} onChange={(e) => setPw(e.target.value)}
          hint="At least 12 characters. You can add a passkey right after." />
        {accept.error && <Notice tone="error">{errorText(accept.error)}</Notice>}
        <Button variant="primary" type="submit" disabled={accept.isPending}>{accept.isPending ? 'Creating…' : 'Create account'}</Button>
      </form>
    </AuthLayout>
  )
}
