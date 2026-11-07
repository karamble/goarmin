// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it, vi } from 'vitest'
import { SSEParser, streamLive, streamPairing, streamSSE, type LiveEnd, type Reading } from './live'

describe('SSEParser', () => {
  it('splits messages across any chunking and line ending', () => {
    const text = ': hello\r\nevent: reading\r\ndata: {"a":1}\r\n\r\nevent: end\ndata: \n\nevent: reading\rdata: x\rdata: y\r\r\n'
    for (const size of [1, 2, 3, 7, text.length]) {
      const p = new SSEParser()
      const got = []
      for (let i = 0; i < text.length; i += size) got.push(...p.push(text.slice(i, i + size)))
      expect(got).toEqual([
        { event: 'reading', data: '{"a":1}' },
        { event: 'end', data: '' },
        { event: 'reading', data: 'x\ny' },
      ])
    }
  })
})

describe('streamSSE', () => {
  const body = (chunks: string[]) => new ReadableStream<Uint8Array>({
    start(c) {
      const enc = new TextEncoder()
      for (const x of chunks) c.enqueue(enc.encode(x))
      c.close()
    },
  })

  it('follows a pairing session until it ends', async () => {
    const views = [{ id: 'a', state: 'finding' }, { id: 'a', state: 'done' }]
    const text = `event: session\ndata: ${JSON.stringify(views[0])}\n\n: ping\n\nevent: session\ndata: ${JSON.stringify(views[1])}\n\nevent: end\ndata: {}\n\nevent: session\ndata: {"late":true}\n\n`
    const chunks = [text.slice(0, 17), text.slice(17, 60), text.slice(60)]
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      expect(url).toBe('/api/pairing/a/events')
      return new Response(body(chunks), { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
    }))
    const got: unknown[] = []
    await streamPairing('a', (v) => got.push(v), new AbortController().signal)
    expect(got).toEqual(views)
    vi.unstubAllGlobals()
  })

  it('reads a live link: hello, readings, and why it ended', async () => {
    const now = Math.floor(Date.now() / 1000)
    const text = `event: hello\ndata: {"since":${now - 60}}\n\nevent: reading\ndata: {"name":"heart_rate","value":71,"unit":"bpm","t":${now}}\n\n` +
      `event: reading\ndata: {broken\n\nevent: end\ndata: {"reason":"error","error_code":"not_in_range"}\n\n`
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      expect(url).toBe('/api/devices/3/live')
      return new Response(body([text]), { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
    }))
    let since = 0
    const got: Reading[] = []
    let end: LiveEnd | null = null
    await streamLive(3, { onHello: (s) => (since = s), onReading: (r) => got.push(r), onEnd: (e) => (end = e) }, new AbortController().signal)
    expect(since).toBe(now - 60)
    expect(got).toEqual([{ name: 'heart_rate', value: 71, unit: 'bpm', t: now }])
    expect(end).toEqual({ reason: 'error', error_code: 'not_in_range' })
    vi.unstubAllGlobals()
  })

  it('turns a refusal into an ApiError with the server text', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: 'no such pairing session' }), { status: 404 })))
    await expect(streamSSE('/api/pairing/x/events', () => {}, new AbortController().signal)).rejects.toMatchObject({ status: 404, message: 'no such pairing session' })
    vi.unstubAllGlobals()
  })
})
