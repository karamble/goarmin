// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, errorText, request } from '../api/client'
import { keys } from '../api/keys'
import type { Me, Setting } from '../api/types'
import { useToast } from '../ui/Toast'
import { normalizeLayout } from './layout'
import { reduce, type Action } from './reducer'
import { cardByType, defaultLayout } from './registry'
import type { Layout } from './types'

const KEY = 'glance.layout'
const PATH = `/me/settings/${KEY}`
/** Edits are saved this long after the last one. */
export const SAVE_DELAY = 800

export type SaveState = 'saved' | 'pending' | 'saving' | 'error'

export interface GlanceLayout {
  layout: Layout
  /** Still loading the stored layout. */
  loading: boolean
  /** Written by a newer goarmin: shown, never saved over. */
  readOnly: boolean
  state: SaveState
  dispatch: (a: Action) => void
  /** Save now (leaving the editor). */
  flush: () => void
}

/**
 * useGlanceLayout is the user's glance, stored on the server so it follows them to every
 * browser. Edits show at once and are saved after a pause, one request at a time, against the
 * revision last seen: if another device saved in between, its version wins and is shown.
 */
export function useGlanceLayout(me: Me): GlanceLayout {
  const qc = useQueryClient()
  const toast = useToast()
  const q = useQuery({ queryKey: keys.setting(KEY), queryFn: () => request<Setting>('GET', PATH) })
  const fallback = useCallback(() => defaultLayout(me), [me])
  const server = useMemo(() => normalizeLayout(q.data?.value ?? null, fallback, cardByType), [q.data, fallback])

  // local holds edits the server has not confirmed yet; without it the server copy shows.
  const [local, setLocal] = useState<Layout | null>(null)
  const [state, setState] = useState<SaveState>('saved')
  const shown = local ?? server.layout
  const r = useRef({ shown, pending: null as Layout | null, inFlight: false, timer: 0, rev: q.data?.rev ?? 0 })
  r.current.shown = shown
  if (!r.current.inFlight && q.data) r.current.rev = q.data.rev

  const save = useCallback(async () => {
    const s = r.current
    clearTimeout(s.timer)
    s.timer = 0
    if (s.inFlight || !s.pending) return
    const body = s.pending
    s.pending = null
    s.inFlight = true
    setState('saving')
    try {
      const res = await request<Setting>('PUT', PATH, undefined, { raw: JSON.stringify(body), headers: { 'If-Match': `"${s.rev}"` } })
      s.rev = res.rev
      qc.setQueryData<Setting>(keys.setting(KEY), { value: body, rev: res.rev })
      s.inFlight = false
      if (s.pending) {
        void save()
      } else {
        setLocal(null)
        setState('saved')
      }
    } catch (e) {
      s.inFlight = false
      s.pending = null
      setLocal(null)
      if (e instanceof ApiError && e.status === 412) {
        setState('saved')
        toast({ text: 'Your glance was changed on another device; this is that version now.' })
        void qc.invalidateQueries({ queryKey: keys.setting(KEY) })
        return
      }
      setState('error')
      toast({
        tone: 'error',
        text: `The glance was not saved: ${errorText(e)}`,
        action: { label: 'Retry', run: () => { s.pending = body; setLocal(body); void save() } },
      })
    }
  }, [qc, toast])

  const dispatch = useCallback(
    (a: Action) => {
      if (server.readOnly) return
      const s = r.current
      const next = reduce(s.shown, a)
      if (next === s.shown) return
      s.shown = next
      s.pending = next
      setLocal(next)
      setState('pending')
      clearTimeout(s.timer)
      s.timer = window.setTimeout(() => void save(), SAVE_DELAY)
    },
    [save, server.readOnly],
  )

  const flush = useCallback(() => void save(), [save])

  // Closing the tab with a save still waiting: send it with keepalive, which outlives the page.
  // Leaving the glance for another page: save as usual (the request finishes in the background).
  useEffect(() => {
    const away = () => {
      const s = r.current
      if (!s.pending || s.inFlight) return
      clearTimeout(s.timer)
      void fetch(`/api${PATH}`, {
        method: 'PUT', keepalive: true, credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'If-Match': `"${s.rev}"` }, body: JSON.stringify(s.pending),
      }).catch(() => {})
      s.pending = null
    }
    window.addEventListener('pagehide', away)
    return () => {
      window.removeEventListener('pagehide', away)
      void save()
    }
  }, [save])

  return { layout: shown, loading: q.isPending, readOnly: server.readOnly, state, dispatch, flush }
}
