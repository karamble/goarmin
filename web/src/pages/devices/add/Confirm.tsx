// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Check } from 'lucide-react'
import { useState } from 'react'
import type { PairView } from '../../../api/types'
import { Button, Spinner } from '../../../ui/kit'
import { CodeDisplay, Countdown, StepHeading } from './parts'

const stepNames = { connected: 'Connected', link: 'Garmin link: model and firmware', battery: 'Battery level' } as const

/** Confirm is everything between choosing the watch and setting it up: connecting, the code,
 * and getting to know the watch. */
export function Confirm({ view, onAnswer, onPasskey, busy }: {
  view: PairView
  onAnswer: (match: boolean) => void
  onPasskey: (n: number) => void
  busy: boolean
}) {
  const name = view.watches.find((w) => w.address === view.selected)?.name ?? 'the watch'
  const p = view.prompt
  if (view.state === 'confirm' && p) {
    return (
      <div className="grid gap-6">
        <div>
          <StepHeading>Check the code</StepHeading>
          <p className="m-0 mt-2 max-w-[560px] text-[15px] text-muted">Your watch shows a code. If both match, confirm on the watch and here.</p>
        </div>
        <section className="card grid justify-items-start gap-5 p-6 sm:p-8">
          <CodeDisplay code={p.passkey} />
          <Countdown deadline={p.deadline} />
          <div className="flex flex-wrap gap-3">
            <Button variant="primary" disabled={busy} onClick={() => onAnswer(true)}>They match</Button>
            <Button variant="ghost" disabled={busy} onClick={() => onAnswer(false)}>They don't match</Button>
          </div>
        </section>
      </div>
    )
  }
  if (view.state === 'enter_passkey' && p) return <EnterPasskey deadline={p.deadline} onPasskey={onPasskey} busy={busy} />
  if (view.state === 'show_passkey' && p) {
    return (
      <div className="grid gap-6">
        <div>
          <StepHeading>Type this code on the watch</StepHeading>
          <p className="m-0 mt-2 text-[15px] text-muted">The watch asks for a code. Enter this one there.</p>
        </div>
        <section className="card grid justify-items-start gap-5 p-6 sm:p-8">
          <CodeDisplay code={p.passkey} />
          <Countdown deadline={p.deadline} />
        </section>
      </div>
    )
  }
  if (view.state === 'identifying') {
    return (
      <div className="grid gap-6">
        <div>
          <StepHeading>Getting to know the watch</StepHeading>
          <p className="m-0 mt-2 text-[15px] text-muted">Paired. goarmin now talks to {name} for the first time.</p>
        </div>
        <section className="card p-6">
          <ul className="m-0 grid list-none gap-3 p-0" aria-label="Progress">
            {view.steps.map((s, i) => {
              const running = !s.done && view.steps.slice(0, i).every((x) => x.done)
              return (
                <li key={s.name} className={`flex items-center gap-3 text-[15px] ${s.done ? 'text-text' : running ? 'text-muted' : 'text-faint'}`}>
                  <span aria-hidden className="grid h-6 w-6 place-items-center">
                    {s.done ? <Check size={18} className="text-lime" /> : running ? <Spinner label={stepNames[s.name]} /> : <span className="h-2 w-2 rounded-full bg-track" />}
                  </span>
                  {stepNames[s.name]}
                  <span className="sr-only">{s.done ? ': done' : running ? ': working' : ': waiting'}</span>
                </li>
              )
            })}
          </ul>
        </section>
      </div>
    )
  }
  // pairing: before the code shows, and right after the answer.
  return (
    <div className="grid gap-6">
      <div>
        <StepHeading>Connecting</StepHeading>
        <p className="m-0 mt-2 text-[15px] text-muted">Pairing with {name}. Keep the watch close; the code shows up on both screens in a moment.</p>
      </div>
      <section className="card flex items-center gap-3 p-6 text-[15px] text-muted">
        <Spinner label="Pairing" /> Waiting for the watch…
      </section>
    </div>
  )
}

function EnterPasskey({ deadline, onPasskey, busy }: { deadline: number; onPasskey: (n: number) => void; busy: boolean }) {
  const [code, setCode] = useState('')
  const ok = /^\d{6}$/.test(code)
  return (
    <div className="grid gap-6">
      <div>
        <StepHeading>Type the code from the watch</StepHeading>
        <p className="m-0 mt-2 text-[15px] text-muted">The watch shows a six-digit code. Type it here.</p>
      </div>
      <form className="card grid justify-items-start gap-5 p-6 sm:p-8" onSubmit={(e) => { e.preventDefault(); if (ok) onPasskey(Number(code)) }}>
        <label className="field-label">
          Code on the watch
          <input className="field w-[220px] font-mono text-[28px] tracking-[.3em] text-lime" inputMode="numeric" autoComplete="off"
            maxLength={6} value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))} autoFocus />
        </label>
        <Countdown deadline={deadline} />
        <Button variant="primary" type="submit" disabled={!ok || busy}>Send the code</Button>
      </form>
    </div>
  )
}
