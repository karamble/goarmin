// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, request } from '../client'
import { keys } from '../keys'
import { registerPasskey, signInWithPasskey } from '../passkeys'
import type { CreatedInvite, InviteInfo, Me, PasskeyInfo, SessionInfo, User } from '../types'

async function fetchMe(): Promise<Me | null> {
  try {
    return await request<Me>('GET', '/me', undefined, { quiet401: true })
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return null
    throw e
  }
}

/** useMe is the signed-in account; a 401 resolves to null (signed out), not an error. */
export function useMe() {
  return useQuery({ queryKey: keys.me, queryFn: fetchMe, staleTime: 60_000 })
}

export function useSetupNeeded() {
  return useQuery({ queryKey: keys.setup, queryFn: () => request<{ needed: boolean }>('GET', '/setup'), staleTime: 30_000 })
}

/**
 * useSignedIn drops everything cached for whoever was signed in before and loads the new account.
 * The account query itself stays in the cache (the app watches it), so it must be refetched, not
 * removed; callers navigate only after it has loaded.
 */
function useSignedIn() {
  const qc = useQueryClient()
  return async () => {
    qc.removeQueries({ predicate: (q) => !(q.queryKey.length === 1 && q.queryKey[0] === keys.me[0]) })
    await qc.fetchQuery({ queryKey: keys.me, queryFn: fetchMe, staleTime: 0 })
  }
}

export function usePasswordLogin() {
  const done = useSignedIn()
  return useMutation({
    mutationFn: (v: { email: string; password: string }) => request<User>('POST', '/auth/login', v, { quiet401: true }),
    onSuccess: done,
  })
}

export function usePasskeyLogin() {
  const done = useSignedIn()
  return useMutation({ mutationFn: () => signInWithPasskey<User>(), onSuccess: done })
}

export function useSetup() {
  const done = useSignedIn()
  return useMutation({
    mutationFn: (v: { token: string; email: string; name: string; password: string }) => request<User>('POST', '/setup', v),
    onSuccess: done,
  })
}

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => request<void>('POST', '/auth/logout'),
    onSettled: () => {
      qc.clear()
      qc.setQueryData(keys.me, null)
    },
  })
}

export function useInvite(token: string) {
  return useQuery({ queryKey: keys.invite(token), queryFn: () => request<InviteInfo>('GET', `/invites/${encodeURIComponent(token)}`), retry: false })
}

export function useAcceptInvite(token: string) {
  const done = useSignedIn()
  return useMutation({
    mutationFn: (v: { email: string; name: string; password: string }) =>
      request<User>('POST', `/invites/${encodeURIComponent(token)}/accept`, v),
    onSuccess: done,
  })
}

export function useSessions() {
  return useQuery({ queryKey: keys.sessions, queryFn: () => request<SessionInfo[]>('GET', '/me/sessions') })
}

export function useEndSession() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => request<void>('DELETE', `/me/sessions/${encodeURIComponent(id)}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.sessions }),
  })
}

export function usePasskeys() {
  return useQuery({ queryKey: keys.passkeys, queryFn: () => request<PasskeyInfo[]>('GET', '/me/passkeys') })
}

export function useAddPasskey() {
  const qc = useQueryClient()
  return useMutation({ mutationFn: (name: string) => registerPasskey(name), onSuccess: () => qc.invalidateQueries({ queryKey: keys.passkeys }) })
}

export function useDeletePasskey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => request<void>('DELETE', `/me/passkeys/${encodeURIComponent(id)}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.passkeys }),
  })
}

export function useChangePassword() {
  return useMutation({ mutationFn: (v: { current: string; new: string }) => request<void>('POST', '/me/password', v) })
}

export function useUsers(enabled = true) {
  return useQuery({ queryKey: keys.users, queryFn: () => request<User[]>('GET', '/admin/users'), enabled })
}

export function useCreateInvite() {
  return useMutation({ mutationFn: (email: string) => request<CreatedInvite>('POST', '/admin/invites', { email }) })
}

export function useUpdateProfile() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: Partial<Pick<Me, 'display_name' | 'timezone' | 'units'>>) => request<Me>('PATCH', '/me', v),
    onSuccess: (me) => qc.setQueryData(keys.me, me),
  })
}
