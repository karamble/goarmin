// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fromB64url, toB64url } from './b64url'
import { request } from './client'

/** Passkeys need a secure context (https, or http://localhost). */
export function passkeysAvailable(): boolean {
  return typeof window !== 'undefined' && window.isSecureContext && 'PublicKeyCredential' in window
}

type Json = Record<string, unknown>

// The browsers' own JSON helpers where present, else a manual conversion of the few binary fields.
function creationOptions(pk: Json): PublicKeyCredentialCreationOptions {
  const P = PublicKeyCredential as unknown as { parseCreationOptionsFromJSON?: (j: Json) => PublicKeyCredentialCreationOptions }
  if (P.parseCreationOptionsFromJSON) return P.parseCreationOptionsFromJSON(pk)
  const user = pk.user as Json
  return {
    ...(pk as unknown as PublicKeyCredentialCreationOptions),
    challenge: fromB64url(pk.challenge as string),
    user: { ...(user as unknown as PublicKeyCredentialUserEntity), id: fromB64url(user.id as string) },
    excludeCredentials: ((pk.excludeCredentials as Json[] | undefined) ?? []).map((c) => ({
      ...(c as unknown as PublicKeyCredentialDescriptor),
      id: fromB64url(c.id as string),
    })),
  }
}

function requestOptions(pk: Json): PublicKeyCredentialRequestOptions {
  const P = PublicKeyCredential as unknown as { parseRequestOptionsFromJSON?: (j: Json) => PublicKeyCredentialRequestOptions }
  if (P.parseRequestOptionsFromJSON) return P.parseRequestOptionsFromJSON(pk)
  return {
    ...(pk as unknown as PublicKeyCredentialRequestOptions),
    challenge: fromB64url(pk.challenge as string),
    allowCredentials: ((pk.allowCredentials as Json[] | undefined) ?? []).map((c) => ({
      ...(c as unknown as PublicKeyCredentialDescriptor),
      id: fromB64url(c.id as string),
    })),
  }
}

function credentialJSON(c: PublicKeyCredential): string {
  const withToJSON = c as unknown as { toJSON?: () => Json }
  if (typeof withToJSON.toJSON === 'function') return JSON.stringify(withToJSON.toJSON())
  const r = c.response as AuthenticatorAttestationResponse & AuthenticatorAssertionResponse
  const response: Json = { clientDataJSON: toB64url(r.clientDataJSON) }
  if ('attestationObject' in r && r.attestationObject) {
    response.attestationObject = toB64url(r.attestationObject)
    response.transports = typeof r.getTransports === 'function' ? r.getTransports() : []
  } else {
    response.authenticatorData = toB64url(r.authenticatorData)
    response.signature = toB64url(r.signature)
    if (r.userHandle) response.userHandle = toB64url(r.userHandle)
  }
  return JSON.stringify({
    id: c.id,
    rawId: toB64url(c.rawId),
    type: c.type,
    response,
    clientExtensionResults: c.getClientExtensionResults(),
    authenticatorAttachment: c.authenticatorAttachment ?? undefined,
  })
}

/** registerPasskey adds a passkey to the signed-in account. */
export async function registerPasskey(name: string): Promise<void> {
  const opts = await request<{ publicKey: Json }>('POST', '/me/passkeys/begin', { name })
  const cred = (await navigator.credentials.create({ publicKey: creationOptions(opts.publicKey) })) as PublicKeyCredential | null
  if (!cred) throw new Error('No passkey was created.')
  await request('POST', '/me/passkeys/finish', undefined, { raw: credentialJSON(cred) })
}

/** signInWithPasskey runs a discoverable passkey sign-in (no username). */
export async function signInWithPasskey<T>(): Promise<T> {
  const opts = await request<{ publicKey: Json }>('POST', '/auth/passkey/login/begin', {}, { quiet401: true })
  const cred = (await navigator.credentials.get({ publicKey: requestOptions(opts.publicKey) })) as PublicKeyCredential | null
  if (!cred) throw new Error('No passkey was chosen.')
  return request<T>('POST', '/auth/passkey/login/finish', undefined, { raw: credentialJSON(cred), quiet401: true })
}
