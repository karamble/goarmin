// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { AlertTriangle, ChevronRight, Plus, RefreshCw } from 'lucide-react'
import { Link } from 'react-router-dom'
import { errorText } from '../../api/client'
import { useMe } from '../../api/hooks/auth'
import { useDevices } from '../../api/hooks/data'
import { useSyncNow } from '../../api/hooks/devices'
import type { Device } from '../../api/types'
import { HeaderActions } from '../../app/section/HeaderSlot'
import { alerts, canPair, deviceName, groupDevices, isWatch, kindWord, subtitle, syncLine, toneDot } from '../../lib/devices'
import { ago } from '../../lib/format'
import { relDay, today } from '../../lib/period'
import { Skeleton } from '../../ui/kit'
import { useToast } from '../../ui/Toast'
import { BluetoothStrip } from './BluetoothStrip'
import { DeviceGlyph } from './DeviceGlyph'

/** Devices lists the person's devices grouped by kind (watches, scales, …), the Bluetooth
 * adapter, and devices they had before. Every device is a data source; only watches sync. */
export default function Watches() {
  const me = useMe().data!
  const q = useDevices()
  const tz = me.timezone
  const list = q.data ?? []
  const current = list.filter((d) => d.current)
  const earlier = list.filter((d) => !d.current)
  const problems = alerts(current)
  const groups = groupDevices(current)
  const watches = current.filter(isWatch)
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
      {canPair(me) && (
        <HeaderActions>
          <Link to="/devices/add"
            className={`inline-flex h-9 items-center gap-2 rounded-full px-4 text-[13px] font-bold ${current.length === 0
              ? 'border-2 border-lime bg-lime text-bg' : 'border border-border text-text hover:border-lime hover:text-lime'}`}>
            <Plus size={15} /> Add a watch
          </Link>
        </HeaderActions>
      )}
      {me.server.sync && <BluetoothStrip tz={tz} />}
      {problems.map((p) => (
        <div key={p.device.id} role="alert" className="card flex items-start gap-3 border-amber/50 px-4 py-3 text-[14px]">
          <AlertTriangle size={18} className="mt-0.5 shrink-0 text-amber" aria-hidden />
          <span className="flex-1">{p.text}</span>
          <Link to={`/devices/watches/${p.device.id}`} className="shrink-0 text-[13px] text-lime hover:underline">Open</Link>
        </div>
      ))}
      {q.isPending ? (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3"><Skeleton className="h-[188px] rounded-[18px]" /></div>
      ) : current.length === 0 ? (
        <div className="card grid place-items-center px-6 py-14 text-center">
          <p className="m-0 font-display text-[24px] uppercase">No watch yet</p>
          <p className="m-0 mt-2 max-w-[460px] text-[14px] text-muted">
            {canPair(me) ? 'Add your Garmin watch: goarmin pairs it with this computer and fetches its data from then on.'
              : me.server.sync ? 'Watches sync through a sync node: pair the watch on the computer near it (goarmin pair), and it appears here with its first sync.'
              : 'This server does not sync watches over Bluetooth. Sign in the computer near your watch as a sync node (Devices › Sync nodes), or import files.'}
          </p>
          {canPair(me) && (
            <Link to="/devices/add" className="mt-5 inline-flex items-center gap-2 rounded-full border-2 border-lime bg-lime px-5 py-2.5 text-[14px] font-bold text-bg">
              <Plus size={16} /> Add a watch
            </Link>
          )}
        </div>
      ) : (
        <>
          {watches.length === 0 && (
            <p className="card m-0 px-4 py-3 text-[14px] text-muted">
              No watch yet.{canPair(me) && <> <Link to="/devices/add" className="text-lime hover:underline">Add a watch</Link> to sync it over Bluetooth.</>}
            </p>
          )}
          {groups.map((g) => (
            <section key={g.kind} aria-labelledby={`group-${g.kind}`} className="grid gap-3">
              {groups.length > 1 && <h2 id={`group-${g.kind}`} className="caps m-0">{g.title} <span className="text-faint">{g.devices.length}</span></h2>}
              <ul aria-label={g.title} className="m-0 grid list-none gap-4 p-0 md:grid-cols-2 xl:grid-cols-3">
                {g.devices.map((d) => isWatch(d)
                  ? <WatchCard key={d.id} d={d} tz={tz} serverSyncs={me.server.sync} />
                  : <DeviceCard key={d.id} d={d} />)}
              </ul>
            </section>
          ))}
        </>
      )}
      {earlier.length > 0 && (
        <details className="card px-4 py-3">
          <summary className="cursor-pointer text-[14px] text-muted">Devices you had before ({earlier.length})</summary>
          <ul className="m-0 mt-3 grid list-none gap-1 p-0">
            {earlier.map((d) => (
              <li key={d.id}>
                <Link to={`/devices/watches/${d.id}`} className="flex items-center gap-3 rounded-xl px-2 py-2 hover:bg-panel2">
                  <span className="flex-1 truncate text-[14px]">{deviceName(d)}</span>
                  {!isWatch(d) && <span className="text-[12px] text-muted">{kindWord(d.kind)}</span>}
                  <span className="font-mono text-[12px] text-faint">
                    {d.owned_from ? relDay(today(tz, new Date(d.owned_from * 1000)), today(tz)) : 'from the start'} –{' '}
                    {d.owned_to ? relDay(today(tz, new Date(d.owned_to * 1000)), today(tz)) : ''}
                  </span>
                  <ChevronRight size={15} className="text-faint" />
                </Link>
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  )
}

function WatchCard({ d, tz, serverSyncs }: { d: Device; tz: string; serverSyncs: boolean }) {
  const sync = useSyncNow()
  const toast = useToast()
  const line = syncLine(d, tz, Date.now(), serverSyncs)
  const s = d.sync
  const running = !!s?.running
  return (
    <li className="card card-hover relative flex flex-col gap-4 p-5">
      <div className="flex items-center gap-4">
        <DeviceGlyph d={d} size={76} />
        <div className="min-w-0 flex-1">
          <Link to={`/devices/watches/${d.id}`} className="after:absolute after:inset-0 after:content-['']">
            <h2 className="m-0 truncate font-display text-[24px] uppercase leading-tight tracking-[.02em]">{deviceName(d)}</h2>
          </Link>
          <p className="m-0 truncate font-mono text-[11px] uppercase tracking-[.12em] text-faint">{subtitle(d)}</p>
          {d.battery_at && <p className="m-0 mt-1 text-[12px] text-muted">battery read {ago(d.battery_at)}</p>}
        </div>
      </div>
      <div>
        <p className="m-0 flex items-center gap-2 text-[14px]">
          <span aria-hidden className={`h-2 w-2 shrink-0 rounded-full ${toneDot[line.tone]}`} />
          {line.text}
        </p>
        {line.detail && <p className="m-0 mt-1 text-[12px] text-muted">{line.detail}</p>}
        {running && s && s.total > 0 && (
          <div role="progressbar" aria-label="Files fetched" aria-valuemin={0} aria-valuemax={s.total} aria-valuenow={s.done}
            className="mt-2 h-1.5 overflow-hidden rounded-full bg-track">
            <div className="h-full rounded-full bg-lime transition-[width] duration-500" style={{ width: `${(s.done / s.total) * 100}%` }} />
          </div>
        )}
      </div>
      {s && (
        <div className="relative z-10 mt-auto flex items-center gap-2">
          <button type="button" disabled={running || sync.isPending}
            onClick={() => sync.mutate({ id: d.id }, { onError: (e) => toast({ tone: 'error', text: errorText(e) }) })}
            className="inline-flex items-center gap-2 rounded-full border border-border px-3.5 py-1.5 text-[13px] text-text hover:border-lime hover:text-lime disabled:opacity-40">
            <RefreshCw size={14} className={running ? 'animate-spin' : ''} /> {running ? 'Syncing' : 'Sync now'}
          </button>
        </div>
      )}
    </li>
  )
}

/** DeviceCard is a device that is no watch: a scale, a sensor, … Its data comes from imports. */
function DeviceCard({ d }: { d: Device }) {
  const line = syncLine(d)
  return (
    <li className="card card-hover relative flex flex-col gap-4 p-5">
      <div className="flex items-center gap-4">
        <DeviceGlyph d={d} size={76} />
        <div className="min-w-0 flex-1">
          <Link to={`/devices/watches/${d.id}`} className="after:absolute after:inset-0 after:content-['']">
            <h2 className="m-0 truncate font-display text-[24px] uppercase leading-tight tracking-[.02em]">{deviceName(d)}</h2>
          </Link>
          <p className="m-0 truncate font-mono text-[11px] uppercase tracking-[.12em] text-faint">{kindWord(d.kind)} · {subtitle(d)}</p>
        </div>
      </div>
      <div>
        <p className="m-0 flex items-center gap-2 text-[14px]">
          <span aria-hidden className={`h-2 w-2 shrink-0 rounded-full ${toneDot[line.tone]}`} />
          {line.text}
        </p>
        {line.detail && <p className="m-0 mt-1 text-[12px] text-muted">{line.detail}</p>}
      </div>
    </li>
  )
}
