// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Check, X } from 'lucide-react'
import { Link } from 'react-router-dom'
import { usePasskeys } from '../api/hooks/auth'
import { useDailyWindow, useDevices } from '../api/hooks/data'
import { usePrefs, useSavePrefs } from '../api/hooks/prefs'
import type { Me } from '../api/types'
import { today } from '../lib/period'

interface Step {
  key: string
  done: boolean
  title: string
  text: string
  to?: string
  action?: string
}

/** GettingStarted guides a new account: a passkey, a watch (or files), the first sync, and the
 * preferences. It goes once every step is done, or when dismissed. */
export function GettingStarted({ me }: { me: Me }) {
  const prefs = usePrefs()
  const save = useSavePrefs()
  const keys = usePasskeys()
  const devs = useDevices()
  const days = useDailyWindow(today(me.timezone), 14)
  if (!prefs.loaded || prefs.onboardingDone || keys.isPending || devs.isPending || days.isPending) return null
  const current = (devs.data ?? []).filter((d) => d.current)
  const hasData = (days.data ?? []).some((d) => (d.steps ?? 0) > 0 || d.sleep_score != null)
  const steps: Step[] = [
    { key: 'passkey', done: (keys.data ?? []).length > 0, title: 'Add a passkey', text: 'Sign in with your fingerprint, face or security key instead of the password.', to: '/settings/security', action: 'Add a passkey' },
    me.server.sync
      ? { key: 'watch', done: current.length > 0, title: 'Add your watch', text: 'Pair it with this computer; it then syncs every 30 minutes.', to: '/devices/add', action: 'Add a watch' }
      : { key: 'watch', done: current.length > 0 || hasData, title: 'Bring in your data', text: 'This server does not talk to watches itself: copy the watch’s GARMIN folder and run goarmin import, or run the server with --with-sync near the watch.' },
    { key: 'sync', done: hasData || current.some((d) => d.sync?.last_good), title: 'First sync', text: 'Health data and activities appear here once the watch has synced.', to: current[0] ? `/devices/watches/${current[0].id}` : undefined, action: current[0] ? 'Open the watch' : undefined },
    { key: 'prefs', done: !!prefs.raw.week_start || !!prefs.raw.clock || !!prefs.raw.intensity_goal || !!prefs.raw.zones, title: 'Your preferences', text: 'Week start, clock, goals and heart-rate zones follow the watch until you choose.', to: '/settings/profile', action: 'Look at them' },
  ]
  if (steps.slice(0, 3).every((s) => s.done)) return null
  const dismiss = () => save.mutate({ next: { ...prefs.raw, v: 1, onboarding_done: true }, rev: prefs.rev })
  return (
    <section className="card mb-5 p-5 sm:p-6" aria-labelledby="getting-started">
      <header className="mb-4 flex items-start justify-between gap-4">
        <div>
          <h2 id="getting-started" className="m-0 font-display text-[22px] uppercase tracking-[.02em]">Getting started</h2>
          <p className="m-0 mt-1 text-[13px] text-muted">{steps.filter((s) => s.done).length} of {steps.length} done</p>
        </div>
        <button type="button" onClick={dismiss} disabled={save.isPending} aria-label="Hide getting started"
          className="grid h-8 w-8 place-items-center rounded-full border border-border text-muted hover:border-lime hover:text-lime">
          <X size={14} aria-hidden />
        </button>
      </header>
      <ol className="m-0 grid list-none gap-3 p-0 md:grid-cols-2 xl:grid-cols-4">
        {steps.map((s, i) => (
          <li key={s.key} className={`rounded-xl border p-4 ${s.done ? 'border-lime-dim bg-lime-fill' : 'border-border'}`}>
            <p className="m-0 flex items-center gap-2 text-[14px] text-text">
              <span aria-hidden className={`grid h-6 w-6 shrink-0 place-items-center rounded-full border font-mono text-[11px] ${s.done ? 'border-lime bg-lime text-bg' : 'border-border text-muted'}`}>
                {s.done ? <Check size={13} /> : i + 1}
              </span>
              {s.title}
              {s.done && <span className="sr-only">(done)</span>}
            </p>
            <p className="m-0 mt-2 text-[13px] text-muted">{s.text}</p>
            {!s.done && s.to && s.action && <Link to={s.to} className="mt-3 inline-block text-[13px] text-lime hover:underline">{s.action}</Link>}
          </li>
        ))}
      </ol>
    </section>
  )
}
