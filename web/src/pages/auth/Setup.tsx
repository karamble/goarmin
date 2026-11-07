// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useState, type FormEvent } from 'react'
import { Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { errorText } from '../../api/client'
import { useSetup, useSetupNeeded } from '../../api/hooks/auth'
import { Button, Field, Notice, Spinner } from '../../ui/kit'
import { AuthLayout } from './AuthLayout'

/** Setup creates the first admin with the token goarmin serve printed. */
export default function Setup() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const needed = useSetupNeeded()
  const setup = useSetup()
  const [token, setToken] = useState(params.get('token') ?? '')
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [pw, setPw] = useState('')
  const [pw2, setPw2] = useState('')
  if (needed.isPending) return <div className="grid min-h-dvh place-items-center"><Spinner /></div>
  if (needed.data && !needed.data.needed) return <Navigate to="/login" replace />
  const mismatch = pw2 !== '' && pw !== pw2
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (mismatch) return
    setup.mutate({ token, email, name, password: pw }, { onSuccess: () => navigate('/settings/security?welcome=1', { replace: true }) })
  }
  return (
    <AuthLayout eyebrow="First run" title="Create your account">
      <form onSubmit={submit} className="grid gap-4">
        <p className="m-0 text-sm text-muted">
          This account becomes the admin. Data you already imported with the same email stays yours.
        </p>
        <Field label="Setup token" required value={token} onChange={(e) => setToken(e.target.value)}
          hint="goarmin serve printed a link with it when it started." />
        <Field label="Your name" autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} />
        <Field label="Email" type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
        <Field label="Password" type="password" autoComplete="new-password" required minLength={12} value={pw}
          onChange={(e) => setPw(e.target.value)} hint="At least 12 characters. You can add a passkey right after." />
        <Field label="Password again" type="password" autoComplete="new-password" required value={pw2} onChange={(e) => setPw2(e.target.value)} />
        {mismatch && <Notice tone="error">The passwords differ.</Notice>}
        {setup.error && <Notice tone="error">{errorText(setup.error)}</Notice>}
        <Button variant="primary" type="submit" disabled={setup.isPending || mismatch}>{setup.isPending ? 'Creating…' : 'Create account'}</Button>
      </form>
    </AuthLayout>
  )
}
