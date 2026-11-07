// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { KeyRound } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { errorText } from '../../api/client'
import { usePasskeyLogin, usePasswordLogin } from '../../api/hooks/auth'
import { passkeysAvailable } from '../../api/passkeys'
import { Button, Field, Notice } from '../../ui/kit'
import { AuthLayout } from './AuthLayout'

/** safeNext keeps a redirect inside this site. */
export function safeNext(next: string | null): string {
  return next && next.startsWith('/') && !next.startsWith('//') ? next : '/'
}

export default function Login() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const next = safeNext(params.get('next'))
  const passkey = usePasskeyLogin()
  const password = usePasswordLogin()
  const [email, setEmail] = useState('')
  const [pw, setPw] = useState('')
  const done = { onSuccess: () => navigate(next, { replace: true }) }
  const submit = (e: FormEvent) => {
    e.preventDefault()
    password.mutate({ email, password: pw }, done)
  }
  const error = passkey.error ?? password.error
  return (
    <AuthLayout eyebrow="Welcome back" title="Sign in">
      <div className="grid gap-5">
        {passkeysAvailable() ? (
          <Button variant="primary" onClick={() => passkey.mutate(undefined, done)} disabled={passkey.isPending}>
            <KeyRound size={18} /> {passkey.isPending ? 'Waiting for your passkey…' : 'Sign in with a passkey'}
          </Button>
        ) : (
          <Notice>Passkeys need https (or localhost). Sign in with your password.</Notice>
        )}
        <div className="flex items-center gap-3 text-faint">
          <span className="h-px flex-1 bg-border" />
          <span className="caps text-faint">or with a password</span>
          <span className="h-px flex-1 bg-border" />
        </div>
        <form onSubmit={submit} className="grid gap-4">
          <Field label="Email" type="email" autoComplete="username webauthn" required value={email} onChange={(e) => setEmail(e.target.value)} />
          <Field label="Password" type="password" autoComplete="current-password" required value={pw} onChange={(e) => setPw(e.target.value)} />
          <Button type="submit" disabled={password.isPending}>{password.isPending ? 'Signing in…' : 'Sign in'}</Button>
        </form>
        {error && <Notice tone="error">{errorText(error)}</Notice>}
      </div>
    </AuthLayout>
  )
}
