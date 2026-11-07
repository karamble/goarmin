// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/** An API failure. status 0 means the server could not be reached. */
export class ApiError extends Error {
  readonly status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export type Num = number | null

type Listener = () => void
const unauthorized = new Set<Listener>()

/** onUnauthorized registers a callback for any 401 (the session ended): the app returns to sign-in. */
export function onUnauthorized(fn: Listener): () => void {
  unauthorized.add(fn)
  return () => unauthorized.delete(fn)
}

export interface RequestOptions {
  /** A raw body (already JSON text) instead of JSON-encoding `body`. */
  raw?: string
  headers?: Record<string, string>
  signal?: AbortSignal
  /** Do not report a 401 to onUnauthorized (sign-in attempts). */
  quiet401?: boolean
}

/**
 * request calls the API at path (relative to /api). Errors become ApiError with the server's
 * {"error": "…"} text. The session cookie travels on its own (same origin).
 */
export async function request<T>(method: string, path: string, body?: unknown, opt: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json', ...opt.headers }
  let payload: string | undefined
  if (opt.raw !== undefined) {
    payload = opt.raw
    headers['Content-Type'] = 'application/json'
  } else if (body !== undefined) {
    payload = JSON.stringify(body)
    headers['Content-Type'] = 'application/json'
  }
  let res: Response
  try {
    res = await fetch(`/api${path}`, { method, headers, body: payload, signal: opt.signal, credentials: 'same-origin' })
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e
    throw new ApiError(0, 'goarmin is not reachable. Check that the server is running.')
  }
  if (res.status === 401 && !opt.quiet401) unauthorized.forEach((fn) => fn())
  if (!res.ok) {
    let msg = `goarmin answered ${res.status} ${res.statusText}`
    try {
      const b = (await res.json()) as { error?: string }
      if (b.error) msg = b.error
    } catch {
      /* not JSON */
    }
    throw new ApiError(res.status, msg)
  }
  if (res.status === 204) return undefined as T
  const text = await res.text()
  return (text ? JSON.parse(text) : undefined) as T
}

export function errorText(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}
