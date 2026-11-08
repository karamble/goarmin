// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useUsers } from '../../../api/hooks/auth'
import { useBluetooth } from '../../../api/hooks/devices'
import type { Me, PairComputer, PairView } from '../../../api/types'
import { timeOfDay } from '../../../lib/format'
import { computer, computerLabel, pairHint } from '../../../lib/pairing'
import { Button, Notice, Spinner } from '../../../ui/kit'
import { BluetoothStrip } from '../BluetoothStrip'
import { StepHeading } from './parts'

const busyText = { sync: 'goarmin is syncing a watch', live: 'a live view is open', pairing: 'someone is adding a watch' } as const

/** btLine is a computer's Bluetooth in the Pair with choice. */
function btLine(c: PairComputer): string {
  if (!c.adapter) return 'Bluetooth not reported yet'
  if (c.adapter.power_state !== 'on') return 'Bluetooth is off'
  return c.adapter.busy ? 'Bluetooth on · in use' : 'Bluetooth on'
}

/**
 * Prepare explains how to put the watch in pairing mode and starts the search. The watch pairs
 * with the computer near it: this server, or a sync node; with several, the person picks one.
 */
export function Prepare({ me, view, owner, setOwner, computers, chosen, setNode, onStart, starting, startError }: {
  me: Me
  view: PairView | null
  owner: number
  setOwner: (id: number) => void
  /** The computers that can pair for the owner (undefined while loading). */
  computers: PairComputer[] | undefined
  chosen: PairComputer | undefined
  setNode: (node: number) => void
  onStart: () => void
  starting: boolean
  startError: string
}) {
  // Before a session: the chosen computer; once one runs, the computer it runs on.
  const node = view ? (view.node?.id ?? 0) : chosen && !chosen.here ? chosen.node : 0
  const name = view ? view.node?.name : chosen && !chosen.here ? chosen.name : undefined
  const bt = useBluetooth(true, node).data
  const users = useUsers(me.is_admin)
  const on = !!bt?.available && bt.power_state === 'on'
  const waiting = view?.state === 'waiting'
  const othersPairing = !view && bt?.busy?.kind === 'pairing'
  const others = (users.data ?? []).filter((u) => u.id !== me.id)
  const none = !view && computers !== undefined && computers.length === 0
  const where = computer(name)
  return (
    <div className="grid gap-5">
      <div>
        <StepHeading>Get the watch ready</StepHeading>
        <p className="m-0 mt-2 max-w-[620px] text-[15px] text-muted">
          goarmin pairs the watch with {name ? <strong className="text-text">{name}</strong> : where}, then fetches its data on its own from then on.
        </p>
      </div>
      {!view && computers && computers.length > 1 && (
        <fieldset className="m-0 grid gap-2 border-0 p-0">
          <legend className="field-label mb-2">Pair with</legend>
          <div className="flex flex-wrap gap-2">
            {computers.map((c) => {
              const picked = chosen?.node === c.node
              return (
                <label key={c.node} className={`card flex cursor-pointer items-center gap-3 px-4 py-3 ${picked ? 'border-lime-dim' : ''}`}>
                  <input type="radio" name="computer" className="accent-[var(--lime)]" checked={picked} onChange={() => setNode(c.node)} />
                  <span>
                    <span className="block text-[15px] text-text">{computerLabel(c)}</span>
                    <span className="block text-[12px] text-muted">{btLine(c)}</span>
                  </span>
                </label>
              )
            })}
          </div>
          <p className="m-0 text-[13px] text-muted">The watch pairs with the computer it is near; that one syncs it from then on.</p>
        </fieldset>
      )}
      {none && (
        <Notice tone="error">
          No computer can pair a watch right now. Start the sync node on the computer near the watch (make node-start there), then come back.
        </Notice>
      )}
      {!none && <BluetoothStrip tz={me.timezone} node={node} name={name} />}
      <section className="card p-5 sm:p-6">
        <ol className="m-0 grid gap-4 pl-0 [counter-reset:step] list-none">
          {[
            <>On the watch, hold the <strong className="text-text">middle-left button</strong>, then open <strong className="text-text">Watch Settings › Connectivity › Phone › Pair Phone</strong>.</>,
            <>Keep the watch within a few metres of {name ? <strong className="text-text">{name}</strong> : where} until it is added.</>,
            <>Your phone can stay paired. If Garmin Connect on it still syncs the watch, you say so in a moment.</>,
          ].map((text, i) => (
            <li key={i} className="flex gap-3 text-[15px] text-muted">
              <span aria-hidden className="grid h-7 w-7 shrink-0 place-items-center rounded-full border border-lime-dim font-mono text-[12px] text-lime">{i + 1}</span>
              <span className="pt-0.5">{text}</span>
            </li>
          ))}
        </ol>
      </section>
      {me.is_admin && others.length > 0 && !view && (
        <label className="field-label max-w-[360px]">
          This watch is for
          <select className="field font-sans text-[15px] normal-case tracking-normal text-text" value={owner || me.id}
            onChange={(e) => setOwner(Number(e.target.value))}>
            <option value={me.id}>Me ({me.email})</option>
            {others.map((u) => <option key={u.id} value={u.id}>{u.display_name || u.email}</option>)}
          </select>
        </label>
      )}
      {waiting && (
        <Notice>
          <span className="inline-flex items-center gap-3">
            <Spinner label="Waiting for Bluetooth" />
            Waiting for Bluetooth: {view?.busy ? `${busyText[view.busy.kind]} since ${timeOfDay(view.busy.since, me.timezone)}` : 'it is in use'}.
            This continues by itself.
          </span>
        </Notice>
      )}
      {view?.error && <Notice tone="error">{pairHint(view.error_code, name) || view.error}</Notice>}
      {othersPairing && <Notice>Someone is adding a watch right now. Try again in a few minutes.</Notice>}
      {startError && <Notice tone="error">{startError}</Notice>}
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="primary" onClick={onStart} disabled={!on || none || starting || waiting || othersPairing}>
          {starting || waiting ? <Spinner label="Starting" /> : null}
          My watch shows a pairing screen
        </Button>
        {!on && bt && <span className="text-[13px] text-muted">Bluetooth must be on first.</span>}
      </div>
    </div>
  )
}
