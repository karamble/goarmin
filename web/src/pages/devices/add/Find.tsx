// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { RefreshCw } from 'lucide-react'
import { useEffect, useState } from 'react'
import type { PairView, PairWatch } from '../../../api/types'
import { flagOf, pairHint, signalBars, signalText } from '../../../lib/pairing'
import { Button, Notice, Spinner } from '../../../ui/kit'
import { SignalBars, StepHeading } from './parts'

const toneText = { lime: 'text-lime', muted: 'text-muted', amber: 'text-amber' } as const

/** Find lists the watches discovery sees; the person picks theirs. */
export function Find({ view, onSelect, onScan, busy }: {
  view: PairView
  onSelect: (address: string, repair: boolean) => void
  onScan: () => void
  busy: boolean
}) {
  const choosable = view.watches.filter((w) => !flagOf(w).disabled)
  // The person's own choice (after a failed try: the watch they tried). Until they choose, the
  // only watch that can be chosen is picked for them, and nothing once more show up.
  const [picked, setPicked] = useState<string | null>(view.selected)
  const chosen = picked ?? (choosable.length === 1 ? choosable[0].address : '')
  const pick = view.watches.find((w) => w.address === chosen && !flagOf(w).disabled)
  const slow = useSlow(view.discovering && view.watches.length === 0, 15_000)
  return (
    <div className="grid gap-5">
      <div>
        <StepHeading>Choose your watch</StepHeading>
        <p className="m-0 mt-2 text-[15px] text-muted">The watch in pairing mode shows up here within a few seconds.</p>
      </div>
      {view.error && <Notice tone="error">{pairHint(view.error_code) || view.error}</Notice>}
      <div className="flex flex-wrap items-center gap-3 text-[14px]" aria-live="polite">
        {view.discovering ? (
          <><Spinner label="Searching" /><span className="text-muted">Searching for watches…</span></>
        ) : (
          <>
            <span className="text-muted">The search has paused.</span>
            <button type="button" onClick={onScan} disabled={busy}
              className="inline-flex items-center gap-2 rounded-full border border-border px-3.5 py-1.5 text-[13px] hover:border-lime hover:text-lime disabled:opacity-40">
              <RefreshCw size={14} /> Scan again
            </button>
          </>
        )}
      </div>
      {view.watches.length === 0 ? (
        <section className="card px-5 py-8 text-center">
          <p className="m-0 text-[15px] text-muted">No Garmin watch seen yet.</p>
          {slow && (
            <ul className="mx-auto mt-4 grid max-w-[460px] list-disc gap-1 pl-5 text-left text-[14px] text-muted">
              <li>Is Pair Phone open on the watch? It closes after a while; open it again.</li>
              <li>Bring the watch closer to this computer.</li>
              <li>If a phone is next to it, close Garmin Connect there for a moment.</li>
            </ul>
          )}
        </section>
      ) : (
        <fieldset className="m-0 grid gap-2 border-0 p-0">
          <legend className="sr-only">Watches nearby</legend>
          {view.watches.map((w) => <WatchRow key={w.address} w={w} checked={w.address === chosen} onPick={setPicked} />)}
        </fieldset>
      )}
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="primary" disabled={!pick || busy} onClick={() => pick && onSelect(pick.address, false)}>
          {pick?.paired_here ? 'Use this watch' : 'Pair'}
        </Button>
        {pick?.paired_here && (
          <Button variant="ghost" small disabled={busy} onClick={() => onSelect(pick.address, true)}>Pair again</Button>
        )}
        {pick?.paired_here && <span className="text-[13px] text-faint">Pair again removes this computer's pairing first. Use it only when the pairing no longer works: the watch keeps its own record of this computer and offers no way to remove it.</span>}
      </div>
    </div>
  )
}

function WatchRow({ w, checked, onPick }: { w: PairWatch; checked: boolean; onPick: (a: string) => void }) {
  const flag = flagOf(w)
  const id = `watch-${w.address.replaceAll(':', '')}`
  return (
    <label htmlFor={id}
      className={`flex items-center gap-4 rounded-xl border px-4 py-3 ${flag.disabled ? 'cursor-not-allowed border-border opacity-60'
        : checked ? 'cursor-pointer border-lime-dim bg-lime-fill' : 'cursor-pointer border-border hover:border-muted'} has-[:focus-visible]:outline has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-lime`}>
      <input id={id} type="radio" name="watch" value={w.address} checked={checked} disabled={flag.disabled}
        onChange={() => onPick(w.address)} className="sr-only" />
      <span aria-hidden className={`grid h-5 w-5 shrink-0 place-items-center rounded-full border-2 ${checked ? 'border-lime' : 'border-border'}`}>
        {checked && <span className="h-2.5 w-2.5 rounded-full bg-lime" />}
      </span>
      <span className="min-w-0 flex-1">
        <span className={`block truncate text-[15px] ${checked ? 'text-lime' : 'text-text'}`}>{w.name}</span>
        <span className="block font-mono text-[11px] tracking-[.08em] text-faint">{w.address}</span>
      </span>
      <span className="sr-only items-center gap-2 text-[12px] text-muted sm:not-sr-only sm:flex">
        <SignalBars bars={signalBars(w)} />
        {signalText(w)}
      </span>
      <span className={`shrink-0 text-right text-[12px] ${toneText[flag.tone]}`}>{flag.text}</span>
    </label>
  )
}

/** useSlow turns true after ms of on (and false again when on ends). */
function useSlow(on: boolean, ms: number) {
  const [slow, setSlow] = useState(false)
  useEffect(() => {
    if (!on) {
      setSlow(false)
      return
    }
    const t = setTimeout(() => setSlow(true), ms)
    return () => clearTimeout(t)
  }, [on, ms])
  return slow
}
