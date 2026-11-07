// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { ApiError } from './client'

export interface SSEMessage {
  event: string
  data: string
}

/**
 * SSEParser splits a Server-Sent Events stream into messages, whatever the chunking: lines end
 * with \n, \r\n or \r, a blank line ends a message, comments (":") are skipped.
 */
export class SSEParser {
  private buf = ''
  private event = ''
  private data: string[] = []

  push(chunk: string): SSEMessage[] {
    this.buf += chunk
    const out: SSEMessage[] = []
    for (;;) {
      const m = /\r\n|\r|\n/.exec(this.buf)
      // A lone \r at the end may be the first half of \r\n: wait for more.
      if (!m || (m[0] === '\r' && m.index === this.buf.length - 1)) break
      const line = this.buf.slice(0, m.index)
      this.buf = this.buf.slice(m.index + m[0].length)
      if (line === '') {
        if (this.data.length) out.push({ event: this.event || 'message', data: this.data.join('\n') })
        this.event = ''
        this.data = []
        continue
      }
      if (line.startsWith(':')) continue
      const i = line.indexOf(':')
      const field = i < 0 ? line : line.slice(0, i)
      const value = i < 0 ? '' : line.slice(i + 1).replace(/^ /, '')
      if (field === 'event') this.event = value
      else if (field === 'data') this.data.push(value)
    }
    return out
  }
}

export interface Reading {
  name: string
  value: number
  unit: string
  /** Unix seconds. */
  t: number
}

/**
 * streamSSE reads the Server-Sent Events at path (absolute, e.g. /api/…) and hands each message
 * to onMessage until the stream ends, onMessage returns false, or signal aborts. fetch rather
 * than EventSource, so a 409 or 503 reaches the caller as an ApiError with the server's text.
 */
export async function streamSSE(path: string, onMessage: (m: SSEMessage) => boolean | void, signal: AbortSignal): Promise<void> {
  const res = await fetch(path, { headers: { Accept: 'text/event-stream' }, credentials: 'same-origin', signal })
  if (!res.ok || !res.body) {
    let msg = `goarmin answered ${res.status}`
    try {
      msg = ((await res.json()) as { error?: string }).error ?? msg
    } catch {
      /* not JSON */
    }
    throw new ApiError(res.status, msg)
  }
  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
  const p = new SSEParser()
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) return
      for (const m of p.push(value)) {
        if (onMessage(m) === false) return
      }
    }
  } finally {
    reader.cancel().catch(() => {})
  }
}

/** Why a live link ended (event: end). */
export interface LiveEnd {
  reason: 'left' | 'time_limit' | 'stopping' | 'closed' | 'error'
  /** With reason error: not_in_range, not_bonded, adapter_off, timeout, transfer. */
  error_code?: string
  error?: string
}

export interface LiveHandlers {
  /** The link is open (or opening) since then, Unix seconds. */
  onHello?: (since: number) => void
  /** A value; the first ones are the link's last ten minutes. */
  onReading: (r: Reading) => void
  onEnd?: (e: LiveEnd) => void
}

/** streamLive reads a watch's live values (GET /api/devices/{id}/live); it resolves when the
 * link ends. A busy watch is an ApiError 409 with the server's sentence. */
export function streamLive(deviceId: number, h: LiveHandlers, signal: AbortSignal): Promise<void> {
  return streamSSE(`/api/devices/${deviceId}/live`, (m) => {
    try {
      if (m.event === 'end') {
        h.onEnd?.(m.data ? (JSON.parse(m.data) as LiveEnd) : { reason: 'closed' })
        return false
      }
      if (m.event === 'hello') h.onHello?.((JSON.parse(m.data) as { since: number }).since)
      if (m.event === 'reading') h.onReading(JSON.parse(m.data) as Reading)
    } catch {
      /* a damaged message: skip it */
    }
  }, signal)
}

/** streamPairing follows a pairing session (GET /api/pairing/{id}/events) until it is over. */
export function streamPairing<V>(id: string, onView: (v: V) => void, signal: AbortSignal): Promise<void> {
  return streamSSE(`/api/pairing/${encodeURIComponent(id)}/events`, (m) => {
    if (m.event === 'end') return false
    if (m.event === 'session') {
      try {
        onView(JSON.parse(m.data) as V)
      } catch {
        /* a damaged message: skip it */
      }
    }
  }, signal)
}
