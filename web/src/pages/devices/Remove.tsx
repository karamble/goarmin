// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Download, Handshake, Trash2, Unlink } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { errorText } from '../../api/client'
import { useDeleteData, useDeviceSummary, useRelease, useUnpair } from '../../api/hooks/devices'
import type { Device } from '../../api/types'
import { watchName } from '../../lib/devices'
import { bytes, int } from '../../lib/format'
import { Confirm } from '../../ui/Sheet'
import { useToast } from '../../ui/Toast'

/** Remove offers what can be done when a watch goes: unpair it, hand it over, delete its data. */
export function Remove({ d, serverSyncs }: { d: Device; serverSyncs: boolean }) {
  const [ask, setAsk] = useState<'unpair' | 'handover' | 'delete' | null>(null)
  const sum = useDeviceSummary(d.id)
  const name = watchName(d)
  // The requests live here, not in the dialogs: a dialog closes at once, and a mutation whose
  // component is gone never reports back.
  const unpair = useUnpair(d.id)
  const release = useRelease(d.id)
  const toast = useToast()
  const navigate = useNavigate()
  const doUnpair = () => unpair.mutate(undefined, {
    onSuccess: () => toast({ text: 'Unpaired. On the watch, remove this computer from its Bluetooth list too (Watch Settings › Connectivity).' }),
    onError: (e) => toast({ tone: 'error', text: errorText(e) }),
  })
  const doRelease = (alsoUnpair: boolean) => release.mutate(alsoUnpair, {
    onSuccess: () => {
      toast({ text: 'Handed over. Whoever adds it next owns it from then on.' })
      navigate('/devices/watches')
    },
    onError: (e) => toast({ tone: 'error', text: errorText(e) }),
  })
  return (
    <div className="grid gap-4">
      {d.current && serverSyncs && d.paired && (
        <Action icon={<Unlink size={18} />} title="Unpair from this computer"
          text="goarmin stops syncing it. The watch stays yours with all its data; add it again any time under Add a watch."
          button="Unpair…" onClick={() => setAsk('unpair')} />
      )}
      {d.current && (
        <Action icon={<Handshake size={18} />} title="Hand it over"
          text="It stops being yours now. What it recorded so far stays in your account; whoever adds it next owns it from then on."
          button="Hand over…" onClick={() => setAsk('handover')} />
      )}
      <Action icon={<Trash2 size={18} />} title="Delete its data" danger
        text={sum.data && sum.data.files > 0
          ? `Everything that came from it into your account: ${int(sum.data.days)} days, ${int(sum.data.nights)} nights, ${int(sum.data.activities)} activities, ${int(sum.data.files)} files (${bytes(sum.data.bytes)}). This cannot be undone.`
          : 'Nothing from it is left in your account.'}
        button="Delete…" disabled={!sum.data || sum.data.files === 0} onClick={() => setAsk('delete')}
        extra={sum.data && sum.data.files > 0 ? (
          <a href={`/api/devices/${d.id}/raw.zip`} download className="inline-flex items-center gap-1.5 text-[13px] text-lime hover:underline">
            <Download size={14} /> Download the original files first
          </a>
        ) : undefined} />
      {ask === 'unpair' && <UnpairDialog d={d} onConfirm={doUnpair} onClose={() => setAsk(null)} />}
      {ask === 'handover' && <HandoverDialog d={d} serverSyncs={serverSyncs} onConfirm={doRelease} onClose={() => setAsk(null)} />}
      {ask === 'delete' && <DeleteDialog d={d} name={name} onClose={() => setAsk(null)} />}
    </div>
  )
}

function Action({ icon, title, text, button, onClick, danger, disabled, extra }: {
  icon: ReactNode
  title: string
  text: string
  button: string
  onClick: () => void
  danger?: boolean
  disabled?: boolean
  extra?: ReactNode
}) {
  return (
    <section className={`card flex flex-wrap items-start gap-4 p-5 ${danger ? 'border-red/40' : ''}`}>
      <span className={`mt-0.5 ${danger ? 'text-err' : 'text-muted'}`} aria-hidden>{icon}</span>
      <div className="min-w-[220px] flex-1">
        <h2 className="m-0 text-[15px] font-medium">{title}</h2>
        <p className="m-0 mt-1 text-[13px] text-muted">{text}</p>
        {extra && <div className="mt-2">{extra}</div>}
      </div>
      <button type="button" onClick={onClick} disabled={disabled}
        className={`shrink-0 rounded-full border px-4 py-2 text-[13px] disabled:opacity-35 ${danger
          ? 'border-red/60 text-err hover:border-err' : 'border-border text-text hover:border-lime hover:text-lime'}`}>
        {button}
      </button>
    </section>
  )
}

function UnpairDialog({ d, onConfirm, onClose }: { d: Device; onConfirm: () => void; onClose: () => void }) {
  return (
    <Confirm title={`Unpair ${watchName(d)}?`} confirm="Unpair" onClose={onClose} onConfirm={onConfirm}>
      This computer forgets the watch and stops syncing it. Nothing is deleted.
    </Confirm>
  )
}

function HandoverDialog({ d, serverSyncs, onConfirm, onClose }: {
  d: Device
  serverSyncs: boolean
  onConfirm: (unpair: boolean) => void
  onClose: () => void
}) {
  const [unpair, setUnpair] = useState(false)
  return (
    <Confirm title={`Hand over ${watchName(d)}?`} confirm="Hand over" danger onClose={onClose}
      onConfirm={() => onConfirm(serverSyncs && unpair)}>
      <span className="block">It stops being yours now. What it recorded until now stays in your account, and you can still look at it.</span>
      {serverSyncs && d.paired && (
        <label className="mt-3 flex items-center gap-2 text-text">
          <input type="checkbox" checked={unpair} onChange={(e) => setUnpair(e.target.checked)} className="accent-[var(--lime)]" />
          Also unpair it from this computer
        </label>
      )}
    </Confirm>
  )
}

function DeleteDialog({ d, name, onClose }: { d: Device; name: string; onClose: () => void }) {
  const del = useDeleteData(d.id)
  const toast = useToast()
  const [typed, setTyped] = useState('')
  const ok = typed.trim().toLowerCase() === name.toLowerCase()
  return (
    <div className="fixed inset-0 z-[78] grid place-items-center bg-black/55 px-4" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <form role="alertdialog" aria-modal="true" aria-label={`Delete the data of ${name}`} className="card-glass w-full max-w-[440px] p-6"
        onSubmit={(e) => {
          e.preventDefault()
          if (!ok) return
          del.mutate(typed, {
            onSuccess: (g) => {
              toast({ text: `Deleted ${g.files} files: ${g.days} days, ${g.nights} nights, ${g.activities} activities.` })
              onClose()
            },
            onError: (e) => toast({ tone: 'error', text: errorText(e) }),
          })
        }}>
        <h2 className="m-0 font-display text-[22px] uppercase tracking-[.02em]">Delete its data?</h2>
        <p className="mt-2 text-[14px] text-muted">
          Everything that came from {name} into your account is deleted for good. Another account's data is not touched.
        </p>
        <label className="field-label mt-4">
          Type {name} to confirm
          <input autoFocus className="field font-sans text-[15px] normal-case tracking-normal text-text" value={typed}
            onChange={(e) => setTyped(e.target.value)} onKeyDown={(e) => e.key === 'Escape' && onClose()} />
        </label>
        <div className="mt-6 flex justify-end gap-2">
          <button type="button" onClick={onClose} className="rounded-full px-4 py-2 text-[14px] text-muted hover:text-text">Cancel</button>
          <button type="submit" disabled={!ok || del.isPending}
            className="rounded-full border-2 border-red/60 px-4 py-2 text-[14px] font-bold text-err hover:border-err disabled:opacity-35">
            Delete for good
          </button>
        </div>
      </form>
    </div>
  )
}
