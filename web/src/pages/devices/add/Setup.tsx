// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useState } from 'react'
import type { PairSetup } from '../../../api/hooks/pairing'
import type { Me, PairView } from '../../../api/types'
import { Button, Notice } from '../../../ui/kit'
import { bondText } from '../../../lib/pairing'
import { WatchGlyph } from '../WatchGlyph'
import { StepHeading } from './parts'

const answers = [
  {
    value: true, title: 'Yes, Garmin Connect still syncs it',
    text: 'Files stay on the watch, so both apps get them. Whichever app syncs first makes the watch delete its health files, and the other one misses those. Activities are fetched either way.',
  },
  {
    value: false, title: 'No, goarmin is the only one',
    text: 'goarmin marks files as synced once it has stored them, as Garmin Connect does: the watch then deletes its health files and keeps your activities.',
  },
] as const

/** Setup names the watch, asks who else syncs it, and starts the first sync. */
export function Setup({ me, view, onSetup, busy, error }: {
  me: Me
  view: PairView
  onSetup: (v: PairSetup) => void
  busy: boolean
  error: string
}) {
  const id = view.identified!
  const yours = view.ownership === 'yours'
  const [name, setName] = useState(yours ? (view.nickname ?? '') : '')
  const [gc, setGc] = useState<boolean | null>(yours && view.after_fetch ? view.after_fetch === 'keep' : null)
  const [older, setOlder] = useState(true)
  const forOther = view.owner.id !== me.id
  const tooLong = [...name.trim()].length > 60
  return (
    <form className="grid gap-5" onSubmit={(e) => {
      e.preventDefault()
      if (gc == null || tooLong) return
      onSetup({ nickname: name.trim(), garmin_connect: gc, include_synced: older && !view.former_owner })
    }}>
      <div>
        <StepHeading>Set up your watch</StepHeading>
        <p className="m-0 mt-2 text-[15px] text-muted">{yours ? 'This watch is yours already; check its settings.' : 'Almost done.'}</p>
      </div>
      <section className="card flex items-center gap-5 p-5">
        <WatchGlyph battery={id.battery} size={72} />
        <div className="min-w-0">
          <p className="m-0 truncate font-display text-[24px] uppercase leading-tight">{id.model}</p>
          <p className="m-0 font-mono text-[11px] uppercase tracking-[.12em] text-faint">
            {id.name} · firmware {id.firmware} · battery {id.battery == null ? '—' : `${id.battery} %`}
          </p>
          {view.bond && <p className="m-0 mt-2 text-[13px] text-muted">{bondText(view.bond, view.node?.name)}</p>}
        </div>
      </section>
      <section className="card grid gap-4 p-5">
        <label className="field-label max-w-[420px]">
          What you call it
          <input className="field font-sans text-[15px] normal-case tracking-normal text-text" value={name} maxLength={60}
            placeholder={id.model} onChange={(e) => setName(e.target.value)} />
          <span className="font-sans text-[13px] normal-case tracking-normal text-faint">Empty uses the model name.</span>
        </label>
      </section>
      <section className="card p-5">
        <fieldset className="m-0 border-0 p-0">
          <legend className="m-0 p-0 text-[16px] text-text">Does Garmin Connect on a phone still sync this watch?</legend>
          <p className="m-0 mb-4 mt-1 text-[13px] text-muted">You can change this later in the watch's settings.</p>
          <div className="grid gap-2">
            {answers.map((a) => {
              const checked = gc === a.value
              const rid = `gc-${a.value}`
              return (
                <label key={rid} htmlFor={rid}
                  className={`flex cursor-pointer gap-3 rounded-xl border px-4 py-3 has-[:focus-visible]:outline has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-lime ${
                    checked ? 'border-lime-dim bg-lime-fill' : 'border-border hover:border-muted'}`}>
                  <input id={rid} type="radio" name="garmin-connect" className="sr-only" checked={checked} onChange={() => setGc(a.value)} />
                  <span aria-hidden className={`mt-0.5 grid h-5 w-5 shrink-0 place-items-center rounded-full border-2 ${checked ? 'border-lime' : 'border-border'}`}>
                    {checked && <span className="h-2.5 w-2.5 rounded-full bg-lime" />}
                  </span>
                  <span>
                    <span className={`block text-[14px] ${checked ? 'text-lime' : 'text-text'}`}>{a.title}</span>
                    <span className="mt-0.5 block text-[13px] text-muted">{a.text}</span>
                  </span>
                </label>
              )
            })}
          </div>
        </fieldset>
      </section>
      {!view.former_owner && (
        <label className="card flex cursor-pointer items-start gap-3 p-5 has-[:focus-visible]:outline has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-lime">
          <input type="checkbox" className="mt-1 h-4 w-4 accent-[var(--lime)]" checked={older} onChange={(e) => setOlder(e.target.checked)} />
          <span>
            <span className="block text-[14px] text-text">Also fetch older activities still on the watch</span>
            <span className="mt-0.5 block text-[13px] text-muted">Activities another app synced before. They stay on the watch.</span>
          </span>
        </label>
      )}
      <p className="m-0 text-[14px] text-muted">
        {forOther ? <>This watch will be <span className="text-text">{view.owner.name || view.owner.email}</span>'s.</>
          : <>This watch will be yours ({me.email}).</>}
      </p>
      {tooLong && <Notice tone="error">The name must be at most 60 characters.</Notice>}
      {error && <Notice tone="error">{error}</Notice>}
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="primary" type="submit" disabled={gc == null || tooLong || busy}>Start the first sync</Button>
        {gc == null && <span className="text-[13px] text-faint">Answer the Garmin Connect question first.</span>}
      </div>
    </form>
  )
}
