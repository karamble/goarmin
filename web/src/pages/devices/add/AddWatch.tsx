// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { errorText } from '../../../api/client'
import { useMe } from '../../../api/hooks/auth'
import {
  useCancelPairing, usePairComputers, usePairConfirm, usePairPasskey, usePairScan, usePairSelect, usePairSetup, usePairing,
  useStartPairing,
} from '../../../api/hooks/pairing'
import { keys } from '../../../api/keys'
import type { PairView } from '../../../api/types'
import { HeaderActions, useSectionTitle } from '../../../app/section/HeaderSlot'
import { defaultComputer, isOver, pairSteps, stepOf } from '../../../lib/pairing'
import { Button, Notice, Spinner } from '../../../ui/kit'
import { Confirm as ConfirmDialog } from '../../../ui/Sheet'
import { useToast } from '../../../ui/Toast'
import { Confirm } from './Confirm'
import { Ended } from './Ended'
import { Find } from './Find'
import { FirstSync } from './FirstSync'
import { StepRail } from './parts'
import { Prepare } from './Prepare'
import { Setup } from './Setup'

/** railStep is where an ended session stopped, for the rail. */
function railStep(v: PairView | null): number {
  const s = stepOf(v)
  if (s != null) return s
  return v?.identified ? 3 : v?.selected ? 2 : 1
}

/**
 * AddWatch is the Add a watch wizard: Prepare, Find, Confirm, Set up, First sync. The session
 * lives on the server, so a reload (or another tab) picks it up where it is. The requests live
 * here, not in the steps: a step unmounts when the session moves on, and its callbacks with it.
 */
export default function AddWatch() {
  const me = useMe().data!
  useSectionTitle('Add a watch')
  const navigate = useNavigate()
  const toast = useToast()
  const qc = useQueryClient()
  const [params] = useSearchParams()
  const q = usePairing()
  const view = q.data ?? null
  const start = useStartPairing()
  const scan = usePairScan()
  const select = usePairSelect()
  const confirm = usePairConfirm()
  const passkey = usePairPasskey()
  const setup = usePairSetup()
  const cancel = useCancelPairing()
  const [owner, setOwner] = useState(() => (me.is_admin ? Number(params.get('for')) || 0 : 0))
  const [node, setNode] = useState<number | null>(null)
  const [asking, setAsking] = useState(false)
  // The computers that can pair for the owner, until a session runs on one of them.
  const computers = usePairComputers(owner || me.id, !q.isPending && !q.data)
  const chosen = computers.data?.find((c) => c.node === node) ?? defaultComputer(computers.data ?? [])

  const step = stepOf(view)
  // Each screen change moves the focus to its heading, so a screen reader hears where it is.
  const screen = view ? (step === 2 ? view.state : `${step}`) : 'none'
  const root = useRef<HTMLDivElement>(null)
  const prev = useRef<string | null>(null)
  useEffect(() => {
    if (q.isPending) return
    if (prev.current !== null && prev.current !== screen && view?.state !== 'enter_passkey') {
      root.current?.querySelector<HTMLElement>('[data-step-heading]')?.focus()
    }
    prev.current = screen
  }, [screen, q.isPending, view?.state])

  const fail = (e: unknown) => toast({ tone: 'error', text: errorText(e) })
  const id = view?.id ?? ''
  const running = !!view && !isOver(view)

  const doCancel = () =>
    cancel.mutate(id, {
      onSuccess: () => {
        toast({ text: 'Adding the watch was cancelled.' })
        navigate('/devices/watches')
      },
      onError: fail,
    })
  const onCancel = () => {
    if (!running) return navigate('/devices/watches')
    if (view?.selected) setAsking(true)
    else doCancel()
  }
  const restart = () => {
    start.reset()
    setup.reset()
    qc.setQueryData(keys.pairing, null)
  }

  if (q.isPending) return <div className="grid place-items-center py-16"><Spinner /></div>
  if (q.isError) {
    return (
      <div className="grid max-w-[560px] gap-3">
        <Notice tone="error">{errorText(q.error)}</Notice>
        <div><Button small onClick={() => void q.refetch()}>Try again</Button></div>
      </div>
    )
  }

  let body
  if (step == null) {
    body = <Ended view={view!} onRestart={restart} />
  } else if (step === 0) {
    body = (
      <Prepare me={me} view={view} owner={owner} setOwner={setOwner} starting={start.isPending}
        computers={view ? undefined : computers.data} chosen={chosen} setNode={setNode}
        startError={start.isError ? errorText(start.error) : ''}
        onStart={() => start.mutate({ owner: owner && owner !== me.id ? owner : undefined,
          node: chosen && !chosen.here ? chosen.node : undefined })} />
    )
  } else if (step === 1) {
    body = (
      <Find view={view!} busy={select.isPending || scan.isPending}
        onScan={() => scan.mutate(id, { onError: fail })}
        onSelect={(address, repair) => select.mutate({ id, address, repair }, { onError: fail })} />
    )
  } else if (step === 2) {
    body = (
      <Confirm view={view!} busy={confirm.isPending || passkey.isPending}
        onAnswer={(match) => confirm.mutate({ id, match }, { onError: fail })}
        onPasskey={(n) => passkey.mutate({ id, passkey: n }, { onError: fail })} />
    )
  } else if (step === 3) {
    body = (
      <Setup me={me} view={view!} busy={setup.isPending} error={setup.isError ? errorText(setup.error) : ''}
        onSetup={(v) => setup.mutate({ id, ...v })} />
    )
  } else {
    body = <FirstSync view={view!} tz={me.timezone} onAnother={restart} />
  }

  return (
    <div ref={root} className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-6 lg:grid-cols-[230px_minmax(0,1fr)] lg:gap-10">
      {(running || !view) && (
        <HeaderActions>
          <button type="button" onClick={onCancel} disabled={cancel.isPending}
            className="inline-flex h-9 items-center rounded-full border border-border px-4 text-[13px] text-muted hover:border-muted hover:text-text disabled:opacity-40">
            Cancel
          </button>
        </HeaderActions>
      )}
      <StepRail step={railStep(view)} />
      <div className="min-w-0 max-w-[820px]">
        <p className="sr-only" aria-live="polite">{step != null ? `Step ${step + 1} of ${pairSteps.length}: ${pairSteps[step]}` : ''}</p>
        {body}
      </div>
      {asking && (
        <ConfirmDialog title="Stop adding the watch?" confirm="Stop" danger onClose={() => setAsking(false)} onConfirm={doCancel}>
          The watch is not added. If it was paired already, the next try skips the code.
        </ConfirmDialog>
      )}
    </div>
  )
}
