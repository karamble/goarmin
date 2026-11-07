// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useCallback, useEffect, useRef, useState } from 'react'
import { addReadings, type Series } from '../../lib/live'
import { ApiError, errorText } from '../client'
import { streamLive, type LiveEnd, type Reading } from '../live'

export type LiveState = 'idle' | 'connecting' | 'live' | 'ended' | 'error' | 'paused'

export interface LiveOptions {
  /** Seconds of history kept per value (10 minutes). */
  keepS?: number
  /** Open the link when mounted (and when the watch changes). */
  auto?: boolean
  /** Stop after this long in a hidden tab, so a forgotten page lets the watch sync (2 minutes;
   * 0 = never). */
  pauseHiddenS?: number
}

/** useLive follows a watch's live link: the latest value of everything the watch sends, a
 * history of the tracked values, the beat intervals, and why the link ended. Readings are
 * applied in batches, so a replay of ten minutes costs one render, not hundreds. */
export function useLive(deviceId: number | undefined, { keepS = 600, auto = false, pauseHiddenS = 120 }: LiveOptions = {}) {
  const [state, setState] = useState<LiveState>('idle')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [end, setEnd] = useState<LiveEnd | null>(null)
  const [since, setSince] = useState<number | null>(null)
  const [latest, setLatest] = useState<Record<string, Reading>>({})
  const [series, setSeries] = useState<Record<string, Series>>({})
  const [beats, setBeats] = useState<number[]>([])
  const ctl = useRef<AbortController | null>(null)
  const buf = useRef<Reading[]>([])
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const flush = useCallback(() => {
    timer.current = null
    const rs = buf.current
    buf.current = []
    if (!rs.length) return
    setLatest((l) => {
      const n = { ...l }
      for (const r of rs) if (!n[r.name] || n[r.name].t <= r.t) n[r.name] = r
      return n
    })
    setSeries((h) => addReadings(h, rs, keepS))
    const rr = rs.filter((r) => r.name === 'rr_interval').map((r) => r.value)
    if (rr.length) setBeats((b) => [...b, ...rr].slice(-300))
    setState((s) => (s === 'connecting' ? 'live' : s))
  }, [keepS])

  const stop = useCallback(() => {
    ctl.current?.abort()
    ctl.current = null
    setState((s) => (s === 'error' || s === 'ended' ? s : 'idle'))
  }, [])

  const start = useCallback(() => {
    if (deviceId == null) return
    ctl.current?.abort()
    const c = new AbortController()
    ctl.current = c
    buf.current = []
    setState('connecting')
    setError('')
    setBusy(false)
    setEnd(null)
    setSince(null)
    setLatest({})
    setSeries({})
    setBeats([])
    streamLive(deviceId, {
      onHello: setSince,
      onReading: (r) => {
        buf.current.push(r)
        if (timer.current == null) timer.current = setTimeout(flush, 150)
      },
      onEnd: setEnd,
    }, c.signal)
      .then(() => {
        if (c.signal.aborted) return
        flush()
        ctl.current = null
        setState('ended')
      })
      .catch((e) => {
        if (c.signal.aborted) return
        ctl.current = null
        setState('error')
        setBusy(e instanceof ApiError && e.status === 409)
        setError(errorText(e))
      })
  }, [deviceId, flush])

  // Open by itself, and close on unmount (the server keeps the link 30 s for a quick return).
  useEffect(() => {
    if (auto && deviceId != null) start()
    return () => {
      ctl.current?.abort()
      ctl.current = null
      if (timer.current != null) clearTimeout(timer.current)
      timer.current = null
    }
  }, [auto, deviceId, start])

  // Leaving the page lets go at once: a page kept in the back-forward cache never unmounts, and
  // its open stream would hold the watch until the 30-minute limit.
  useEffect(() => {
    const onHide = () => {
      if (!ctl.current) return
      ctl.current.abort()
      ctl.current = null
      setState('paused')
    }
    window.addEventListener('pagehide', onHide)
    return () => window.removeEventListener('pagehide', onHide)
  }, [])

  // A hidden tab lets go after a while.
  useEffect(() => {
    if (!pauseHiddenS || typeof document === 'undefined') return
    let t: ReturnType<typeof setTimeout> | undefined
    const on = () => {
      if (document.hidden) {
        t = setTimeout(() => {
          if (!ctl.current) return
          ctl.current.abort()
          ctl.current = null
          setState('paused')
        }, pauseHiddenS * 1000)
      } else if (t) {
        clearTimeout(t)
        t = undefined
      }
    }
    document.addEventListener('visibilitychange', on)
    return () => {
      document.removeEventListener('visibilitychange', on)
      if (t) clearTimeout(t)
    }
  }, [pauseHiddenS])

  return { state, error, busy, end, since, latest, series, beats, start, stop }
}
