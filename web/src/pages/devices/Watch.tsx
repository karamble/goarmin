// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { ArrowLeft, Radio, RefreshCw, Trash2 } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { errorText } from '../../api/client'
import { useMe } from '../../api/hooks/auth'
import { useDeleteFromWatch, useDevice, useDeviceFiles, useDeviceSummary, useSyncLog, useSyncNow, useUpdateDevice } from '../../api/hooks/devices'
import type { AfterFetch, Device, WatchFile } from '../../api/types'
import { HeaderActions, useSectionTitle } from '../../app/section/HeaderSlot'
import { fitTypeName, subtitle, syncLine, toneDot, watchName } from '../../lib/devices'
import { ago, bytes, int, timeOfDay } from '../../lib/format'
import { relDay, today } from '../../lib/period'
import { Notice, Skeleton } from '../../ui/kit'
import { Confirm } from '../../ui/Sheet'
import { useToast } from '../../ui/Toast'
import { FromWatch } from './FromWatch'
import { Remove } from './Remove'
import { fold, RunRow } from './RunRow'
import { WatchGlyph } from './WatchGlyph'

type Tab = 'overview' | 'files' | 'settings' | 'remove'

/** Watch is one watch: what it is doing, what came from it, its files and its settings. */
export default function Watch() {
  const id = Number(useParams().id)
  const me = useMe().data!
  const { device: d, isPending } = useDevice(id)
  const [q, setQ] = useSearchParams()
  useSectionTitle(d ? watchName(d) : undefined)
  const sync = useSyncNow()
  const toast = useToast()
  const tabs: { id: Tab; label: string }[] = [
    { id: 'overview', label: 'Overview' },
    { id: 'files', label: 'Files' },
    ...(d?.current ? [{ id: 'settings' as Tab, label: 'Settings' }] : []),
    { id: 'remove', label: 'Remove' },
  ]
  const tab = (tabs.find((t) => t.id === q.get('tab'))?.id ?? 'overview') as Tab
  if (isPending) return <Skeleton className="h-[240px] rounded-[18px]" />
  if (!d) {
    return (
      <div className="card px-6 py-10 text-center">
        <p className="m-0 text-muted">No such watch.</p>
        <Link to="/devices/watches" className="mt-3 inline-block text-lime">Back to your watches</Link>
      </div>
    )
  }
  const running = !!d.sync?.running
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
      <HeaderActions>
        {d.current && d.sync && (
          <>
            <button type="button" disabled={running || sync.isPending}
              onClick={() => sync.mutate({ id: d.id }, { onError: (e) => toast({ tone: 'error', text: errorText(e) }) })}
              className="inline-flex h-9 items-center gap-2 rounded-full border-2 border-lime bg-lime px-4 text-[13px] font-bold text-bg disabled:opacity-50">
              <RefreshCw size={14} className={running ? 'animate-spin' : ''} /> {running ? 'Syncing' : 'Sync now'}
            </button>
            {me.server.sync && (
              <Link to={`/live?watch=${d.id}`} className="inline-flex h-9 items-center gap-2 rounded-full border border-border px-3.5 text-[13px] text-text hover:border-lime hover:text-lime">
                <Radio size={14} /> Live
              </Link>
            )}
          </>
        )}
      </HeaderActions>
      <Link to="/devices/watches" className="inline-flex w-fit items-center gap-1.5 text-[13px] text-muted hover:text-text">
        <ArrowLeft size={14} /> All watches
      </Link>
      {!d.current && (
        <Notice>
          You handed this watch over{d.owned_to ? ` ${relDay(today(me.timezone, new Date(d.owned_to * 1000)), today(me.timezone))}` : ''}.
          You still see what came from it while it was yours.
        </Notice>
      )}
      <Hero d={d} tz={me.timezone} serverSyncs={me.server.sync} />
      <nav aria-label="Watch" className="flex gap-1 border-b border-border">
        {tabs.map((t) => (
          <button key={t.id} type="button" aria-current={tab === t.id ? 'page' : undefined}
            onClick={() => setQ((p) => { const n = new URLSearchParams(p); n.set('tab', t.id); return n }, { replace: true })}
            className={`relative px-3 pb-3 pt-1 text-[14px] ${tab === t.id
              ? 'text-text after:absolute after:inset-x-0 after:-bottom-px after:h-[2px] after:bg-lime' : 'text-muted hover:text-text'}`}>
            {t.label}
          </button>
        ))}
      </nav>
      {tab === 'overview' && <Overview d={d} tz={me.timezone} />}
      {tab === 'files' && <Files d={d} tz={me.timezone} />}
      {tab === 'settings' && d.current && <Settings d={d} />}
      {tab === 'remove' && <Remove d={d} serverSyncs={me.server.sync} />}
    </div>
  )
}

function Hero({ d, tz, serverSyncs }: { d: Device; tz: string; serverSyncs: boolean }) {
  const line = syncLine(d, tz, Date.now(), serverSyncs)
  const s = d.sync
  return (
    <section className="card flex flex-wrap items-center gap-6 p-5 sm:p-6">
      <WatchGlyph battery={d.battery} size={104} />
      <div className="min-w-0 flex-1">
        <p className="m-0 font-mono text-[11px] uppercase tracking-[.14em] text-faint">{subtitle(d)}</p>
        <p className="m-0 mt-2 flex items-center gap-2 text-[18px]">
          <span aria-hidden className={`h-2.5 w-2.5 shrink-0 rounded-full ${toneDot[line.tone]}`} />
          {line.text}
        </p>
        {line.detail && <p className="m-0 mt-1 text-[13px] text-muted">{line.detail}</p>}
        {s?.running && s.total > 0 && (
          <div role="progressbar" aria-label="Files fetched" aria-valuemin={0} aria-valuemax={s.total} aria-valuenow={s.done}
            className="mt-3 h-2 max-w-[420px] overflow-hidden rounded-full bg-track">
            <div className="h-full rounded-full bg-lime transition-[width] duration-500" style={{ width: `${(s.done / s.total) * 100}%` }} />
          </div>
        )}
        <dl className="m-0 mt-4 flex flex-wrap gap-x-6 gap-y-2 text-[13px]">
          <Fact label="Battery">{d.battery == null ? 'not reported yet' : `${d.battery} %${d.battery_at ? ` · ${ago(d.battery_at)}` : ''}`}</Fact>
          {s?.last_good && <Fact label="Last good sync">{ago(s.last_good)}</Fact>}
          <Fact label="Firmware">{d.firmware || '–'}</Fact>
          {d.address && <Fact label="Address"><span className="break-all font-mono">{d.address}</span></Fact>}
          <Fact label="Unit">{d.unit_id}</Fact>
        </dl>
      </div>
    </section>
  )
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <dt className="caps !text-[10px] !text-faint">{label}</dt>
      <dd className="m-0 mt-0.5 text-text">{children}</dd>
    </div>
  )
}

function Overview({ d, tz }: { d: Device; tz: string }) {
  const sum = useDeviceSummary(d.id)
  const log = useSyncLog(d.id, false, 6)
  const runs = log.data?.pages[0]?.items ?? []
  const s = sum.data
  const types = s ? Object.entries(s.by_type).map(([t, n]) => ({ t: Number(t), n })).sort((a, b) => b.n - a.n) : []
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5 lg:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
      <section className="card p-5">
        <h2 className="caps m-0 mb-4">What came from this watch</h2>
        {!s ? <Skeleton className="h-24" /> : s.files === 0 ? (
          <p className="m-0 text-[14px] text-muted">Nothing yet. Its first sync brings it in.</p>
        ) : (
          <>
            <dl className="m-0 grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-4">
              <Big label="Days" value={int(s.days)} />
              <Big label="Nights" value={int(s.nights)} />
              <Big label="Activities" value={int(s.activities)} />
              <Big label="Files" value={int(s.files)} sub={bytes(s.bytes)} />
            </dl>
            {s.first_day && (
              <p className="m-0 mt-4 text-[13px] text-muted">
                From <span className="text-text">{relDay(s.first_day, today(tz))}</span> to <span className="text-text">{relDay(s.last_day, today(tz))}</span>
              </p>
            )}
            <ul className="m-0 mt-4 flex list-none flex-wrap gap-2 p-0">
              {types.map((x) => (
                <li key={x.t} className="rounded-full border border-border px-3 py-1 text-[12px] text-muted">
                  {fitTypeName(x.t)} <span className="num text-text">{x.n}</span>
                </li>
              ))}
            </ul>
          </>
        )}
      </section>
      <section className="card p-5">
        <div className="mb-2 flex items-center justify-between">
          <h2 className="caps m-0">Recent syncs</h2>
          <Link to={`/devices/log?watch=${d.id}`} className="text-[13px] text-lime hover:underline">Sync log</Link>
        </div>
        {log.isPending ? <Skeleton className="h-24" /> : runs.length === 0 ? (
          <p className="m-0 text-[14px] text-muted">No syncs yet.</p>
        ) : (
          <ul className="m-0 list-none p-0">{fold(runs).map((e) => <RunRow key={e.run.id} entry={e} tz={tz} />)}</ul>
        )}
      </section>
    </div>
  )
}

function Big({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div>
      <dt className="caps !text-[10px] !text-faint">{label}</dt>
      <dd className="num m-0 mt-1 text-[28px] leading-none">{value}</dd>
      {sub && <dd className="m-0 mt-1 text-[12px] text-muted">{sub}</dd>}
    </div>
  )
}

const stateText: Record<WatchFile['state'], string> = {
  pending: 'on the watch, to mark later',
  kept: 'on the watch',
  archived: 'marked as synced',
  deleted: 'deleted by goarmin',
  gone: 'gone from the watch',
}

function Files({ d, tz }: { d: Device; tz: string }) {
  const q = useDeviceFiles(d.id)
  const del = useDeleteFromWatch(d.id)
  const toast = useToast()
  const [type, setType] = useState<number | 'all'>('all')
  const [onWatch, setOnWatch] = useState(false)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [confirm, setConfirm] = useState(false)
  const files = q.data ?? []
  const types = useMemo(() => [...new Set(files.map((f) => f.fit_type))].sort((a, b) => a - b), [files])
  const shown = files.filter((f) => (type === 'all' || f.fit_type === type) && (!onWatch || f.on_watch))
  const toggle = (uid: string) => setPicked((p) => { const n = new Set(p); if (n.has(uid)) n.delete(uid); else n.add(uid); return n })
  const deletable = shown.filter((f) => f.deletable)
  if (q.isPending) return <Skeleton className="h-40 rounded-[18px]" />
  if (files.length === 0) {
    return <div className="card px-5 py-8 text-center text-[14px] text-muted">{d.sync ? 'No files fetched from this watch yet.' : 'This server keeps no file list for imported watches.'}</div>
  }
  return (
    <section className="card p-4 sm:p-5">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        {(['all', ...types] as const).map((t) => (
          <button key={t} type="button" aria-pressed={type === t} onClick={() => setType(t)}
            className={`rounded-full border px-3 py-1 text-[12px] ${type === t ? 'border-lime-dim bg-lime-fill text-lime' : 'border-border text-muted hover:text-text'}`}>
            {t === 'all' ? `All (${files.length})`
              : types.some((u) => u !== t && fitTypeName(u) === fitTypeName(t)) ? `${fitTypeName(t)} (${t})` : fitTypeName(t)}
          </button>
        ))}
        <label className="ml-auto inline-flex items-center gap-2 text-[13px] text-muted">
          <input type="checkbox" checked={onWatch} onChange={(e) => setOnWatch(e.target.checked)} className="accent-[var(--lime)]" />
          Still on the watch
        </label>
      </div>
      {d.current && d.delete_support === 'no' && <Notice>This watch refused to delete files on request, so deleting is off.</Notice>}
      {d.current && d.delete_support !== 'no' && <p className="m-0 mb-2 text-[12px] text-faint">Only activities can be deleted from the watch; its other files configure it.</p>}
      {picked.size > 0 && (
        <div className="sticky top-[132px] z-10 mb-2 flex items-center gap-3 rounded-xl border border-border bg-bg2 px-3 py-2">
          <span className="flex-1 text-[13px]">{picked.size} selected</span>
          <button type="button" onClick={() => setPicked(new Set())} className="text-[13px] text-muted hover:text-text">Clear</button>
          <button type="button" onClick={() => setConfirm(true)}
            className="inline-flex items-center gap-1.5 rounded-full border border-red/60 px-3 py-1.5 text-[13px] text-err hover:border-err">
            <Trash2 size={14} /> Delete from the watch
          </button>
        </div>
      )}
      {deletable.length > 0 && (
        <button type="button" className="mb-1 text-[12px] text-muted hover:text-text"
          onClick={() => setPicked(new Set(deletable.map((f) => f.uid)))}>Select all {deletable.length} that can be deleted</button>
      )}
      <ul className="m-0 list-none p-0">
        {shown.map((f) => (
          <li key={f.uid} className="flex items-center gap-3 border-b border-border py-2.5 last:border-b-0">
            <input type="checkbox" aria-label={`Select ${fitTypeName(f.fit_type)} ${f.uid}`} disabled={!f.deletable}
              checked={picked.has(f.uid)} onChange={() => toggle(f.uid)} className="accent-[var(--lime)] disabled:opacity-20" />
            <span className="w-[130px] shrink-0 truncate text-[13px]">{fitTypeName(f.fit_type)}</span>
            <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-muted"
              title={f.recorded ? 'recorded' : 'fetched (the file has no recording time)'}>
              {f.recorded || f.fetched
                ? `${relDay(today(tz, new Date((f.recorded || f.fetched) * 1000)), today(tz))} ${timeOfDay(f.recorded || f.fetched, tz)}`
                : '–'}
            </span>
            <span className="hidden w-[150px] shrink-0 text-[12px] text-faint sm:block">{stateText[f.state]}</span>
            <span className="num w-[64px] shrink-0 text-right text-[12px] text-muted">{bytes(f.size)}</span>
          </li>
        ))}
      </ul>
      {confirm && (
        <Confirm title={`Delete ${picked.size} file${picked.size === 1 ? '' : 's'} from the watch?`} confirm="Delete from the watch" danger
          onClose={() => setConfirm(false)}
          onConfirm={() => del.mutate([...picked], {
            onSuccess: () => {
              toast({ text: 'Deleting from the watch at the next connection, which starts now. goarmin keeps its copies.' })
              setPicked(new Set())
            },
            onError: (e) => toast({ tone: 'error', text: errorText(e) }),
          })}>
          They leave the watch and free its space. goarmin keeps every one of them.
          {d.delete_support === '' && ' This watch has not deleted a file for goarmin yet; the first try shows whether it does.'}
        </Confirm>
      )}
    </section>
  )
}

const afterFetch: { value: AfterFetch; title: string; text: string }[] = [
  { value: 'keep', title: 'Keep on the watch', text: 'Nothing is marked as synced. Another app (Garmin Connect) can still sync the same files.' },
  { value: 'archive', title: 'Mark as synced', text: 'The watch deletes its health files once goarmin has them, as it does for Garmin Connect. Activities stay.' },
  { value: 'archive_delete', title: 'Mark as synced and delete activities', text: 'Also deletes activities from the watch once goarmin has them.' },
]

function Settings({ d }: { d: Device }) {
  const up = useUpdateDevice(d.id)
  const toast = useToast()
  const [name, setName] = useState(d.nickname)
  const [ask, setAsk] = useState<AfterFetch | null>(null)
  const save = (v: Parameters<typeof up.mutate>[0], ok?: string) =>
    up.mutate(v, { onSuccess: () => ok && toast({ text: ok }), onError: (e) => toast({ tone: 'error', text: errorText(e) }) })
  return (
    <div className="grid gap-5">
      <section className="card p-5">
        <h2 className="caps m-0 mb-3">Name</h2>
        <form className="flex flex-wrap items-end gap-3" onSubmit={(e) => { e.preventDefault(); save({ nickname: name }, 'Saved') }}>
          <label className="field-label min-w-[240px] flex-1">
            What you call it
            <input className="field font-sans text-[15px] normal-case tracking-normal text-text" value={name} maxLength={60}
              placeholder={d.model} onChange={(e) => setName(e.target.value)} />
          </label>
          <button type="submit" disabled={name === d.nickname || up.isPending}
            className="rounded-full border border-border px-4 py-2.5 text-[14px] hover:border-lime hover:text-lime disabled:opacity-40">Save</button>
        </form>
      </section>
      <section className="card p-5">
        <h2 className="caps m-0">After fetching</h2>
        <p className="m-0 mb-4 mt-1 text-[13px] text-muted">What goarmin does on the watch with a file it has stored safely.</p>
        <div role="radiogroup" aria-label="After fetching" className="grid gap-2">
          {afterFetch.map((o) => (
            <button key={o.value} type="button" role="radio" aria-checked={d.after_fetch === o.value}
              onClick={() => (o.value === 'archive_delete' ? setAsk(o.value) : save({ after_fetch: o.value }, `After fetching: ${o.title.toLowerCase()}`))}
              className={`rounded-xl border px-4 py-3 text-left ${d.after_fetch === o.value ? 'border-lime-dim bg-lime-fill' : 'border-border hover:border-muted'}`}>
              <span className={`block text-[14px] ${d.after_fetch === o.value ? 'text-lime' : 'text-text'}`}>{o.title}</span>
              <span className="mt-0.5 block text-[13px] text-muted">{o.text}</span>
            </button>
          ))}
        </div>
      </section>
      <section className="card flex items-center gap-4 p-5">
        <div className="flex-1">
          <h2 className="caps m-0">Scheduled syncs</h2>
          <p className="m-0 mt-1 text-[13px] text-muted">{d.paused ? 'Paused. Sync now still works.' : 'Every 30 minutes while the watch is in range.'}</p>
        </div>
        <button type="button" role="switch" aria-checked={!d.paused} aria-label="Scheduled syncs"
          onClick={() => save({ paused: !d.paused }, d.paused ? 'Scheduled syncs are on' : 'Scheduled syncs are paused')}
          className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${!d.paused ? 'bg-lime' : 'bg-track'}`}>
          <span className={`absolute top-0.5 h-5 w-5 rounded-full transition-[left] ${!d.paused ? 'left-[22px] bg-bg' : 'left-0.5 bg-text'}`} />
        </button>
      </section>
      <FromWatch d={d} />
      <section className="card p-5">
        <h2 className="caps m-0">Coming later</h2>
        <ul className="m-0 mt-2 grid gap-1 pl-5 text-[13px] text-muted">
          <li>An approximate position the watch can use for its location requests</li>
          <li>Satellite data (GPS assistance) from your Garmin account</li>
          <li>Firmware updates, installed only when you say so</li>
        </ul>
      </section>
      {ask && (
        <Confirm title="Also delete activities from the watch?" confirm="Delete activities after fetching" danger onClose={() => setAsk(null)}
          onConfirm={() => save({ after_fetch: ask }, 'Activities are deleted from the watch once goarmin has them')}>
          Once goarmin has stored an activity, it is deleted from the watch, which frees its space. goarmin keeps every one.
          {d.delete_support !== 'yes' && ' This watch has not deleted a file for goarmin yet; if it refuses, its activities stay.'}
        </Confirm>
      )}
    </div>
  )
}
