// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Activity, Footprints, Flame, HeartPulse, Mountain, Radio, Square, Wind, Zap } from 'lucide-react'
import { useEffect, useId, useMemo, useState, type ReactNode } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { useDevices, useRecentActivities } from '../../api/hooks/data'
import { useLive, type LiveState } from '../../api/hooks/live'
import { usePrefs } from '../../api/hooks/prefs'
import { useZones } from '../../api/hooks/training'
import type { Device } from '../../api/types'
import { usePaletteActions } from '../../app/palette/actions'
import { HeaderActions } from '../../app/section/HeaderSlot'
import { Chart } from '../../charts'
import { describeLiveLine, liveLine, type LiveLineInput } from '../../charts/builders/liveLine'
import { zonesFor } from '../../lib/zones'
import { watchName } from '../../lib/devices'
import { timeOfDay } from '../../lib/format'
import { emptySeries, endText, gained, rmssd, stressLevel, summary, type Series } from '../../lib/live'
import { toneVar } from '../../lib/training'
import { Card, chartCtx, num } from '../health/parts'

/** Live shows everything the watch streams, as it arrives. The link opens with the page. */
export default function LivePage() {
  const me = useMe().data!
  const devs = useDevices()
  const [q, setQ] = useSearchParams()
  const watches = (devs.data ?? []).filter((d) => d.current && d.sync)
  const picked = watches.find((d) => String(d.id) === q.get('watch')) ?? watches[0]
  if (!me.server.sync) {
    return (
      <Notice title="Live needs the sync role">
        This server does not talk to watches itself. Run it with <code className="font-mono">goarmin serve --with-sync</code> on the
        computer near your watch.
      </Notice>
    )
  }
  if (devs.isPending) return null
  if (!picked) {
    return (
      <Notice title="No watch to watch">
        Add your watch first; then its heart rate, stress, Body Battery and more stream here.
        <Link to="/devices/add" className="mt-4 block text-lime hover:underline">Add a watch</Link>
      </Notice>
    )
  }
  return <Watching key={picked.id} dev={picked} watches={watches} pick={(id) => setQ({ watch: String(id) }, { replace: true })} tz={me.timezone} />
}

function Notice({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="card grid place-items-center px-6 py-12 text-center">
      <p className="m-0 font-display text-[22px] uppercase">{title}</p>
      <div className="m-0 mt-2 max-w-[520px] text-[14px] text-muted">{children}</div>
    </section>
  )
}

/** useSecond ticks once a second while on, for the scrolling charts and ages. */
function useSecond(on: boolean): number {
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000))
  useEffect(() => {
    if (!on) return
    const id = setInterval(() => setNow(Math.floor(Date.now() / 1000)), 1000)
    return () => clearInterval(id)
  }, [on])
  return now
}

function Watching({ dev, watches, pick, tz }: { dev: Device; watches: Device[]; pick: (id: number) => void; tz: string }) {
  const live = useLive(dev.id, { auto: true })
  const on = live.state === 'connecting' || live.state === 'live'
  const now = useSecond(on)
  const recent = useRecentActivities(50)
  const zinfo = useZones()
  const prefs = usePrefs()
  const maxHr = Math.max(0, ...(recent.data?.items ?? []).map((a) => a.max_hr ?? 0))
  const zones = zonesFor({ info: zinfo.data, estimateMax: maxHr || undefined, custom: prefs.zones.source === 'custom' ? prefs.zones : null, prefer: prefs.zones.source })
    ?.limits.slice(0, 5)
  const ctx = useMemo(() => chartCtx(tz), [tz])
  const group = useId()
  const s = (name: string): Series => live.series[name] ?? emptySeries
  const v = (name: string) => live.latest[name]?.value ?? null
  const hrv = rmssd(live.beats)
  const hrSum = summary(s('heart_rate'))
  const stress = v('stress')
  const level = stress != null ? stressLevel(stress) : null
  const spo2 = live.latest.spo2
  const paletteItems = useMemo(() => [on
    ? { id: 'live-stop', label: 'Stop live', icon: Square, run: live.stop }
    : { id: 'live-start', label: 'Go live now', icon: Radio, run: live.start }], [on, live.stop, live.start])
  usePaletteActions('live', paletteItems)
  const [announce, setAnnounce] = useState(false)
  const [spoken, setSpoken] = useState('')
  useEffect(() => {
    if (!announce || live.state !== 'live') return
    const say = () => {
      const hr = live.latest.heart_rate?.value
      if (hr) setSpoken(`Heart rate ${hr}`)
    }
    say()
    const id = setInterval(say, 30_000)
    return () => clearInterval(id)
  }, [announce, live.state]) // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
      <HeaderActions>
        {watches.length > 1 && (
          <label className="flex items-center gap-2 text-[13px] text-muted">
            <span className="sr-only">Watch</span>
            <select value={dev.id} onChange={(e) => pick(Number(e.target.value))}
              className="h-9 rounded-full border border-border bg-bg2 px-3 text-[13px] text-text">
              {watches.map((w) => <option key={w.id} value={w.id}>{watchName(w)}</option>)}
            </select>
          </label>
        )}
        <button type="button" onClick={on ? live.stop : live.start}
          className={`inline-flex h-9 items-center gap-2 rounded-full border-2 px-4 text-[13px] font-bold ${
            on ? 'border-border text-muted hover:text-text' : 'border-lime bg-lime text-bg'}`}>
          {on ? <><Square size={13} aria-hidden /> Stop</> : <><Radio size={14} aria-hidden /> Go live</>}
        </button>
      </HeaderActions>

      <StatusLine state={live.state} since={live.since} error={live.error} busy={live.busy} end={live.end} name={watchName(dev)} tz={tz}
        retry={live.start} />

      <ul className="m-0 grid list-none grid-cols-2 gap-3 p-0 md:grid-cols-3 xl:grid-cols-5" aria-label="Live values">
        <Tile icon={HeartPulse} label="Heart rate" value={num(v('heart_rate'))} unit="bpm" big tone="lime" live={live.state === 'live'}
          hint={[v('resting_hr') != null && `resting ${v('resting_hr')}`, hrSum && `${Math.round(hrSum.min)}–${Math.round(hrSum.max)}, avg ${Math.round(hrSum.avg)}`].filter(Boolean).join(' · ') || undefined} />
        <Tile icon={Zap} label="Stress" value={num(stress)} hint={level?.label} dot={level ? toneVar[level.tone] : undefined} />
        <Tile icon={Activity} label="Body Battery" value={num(v('body_battery'))} unit="%" />
        <Tile icon={Wind} label="Respiration" value={num(v('respiration'))} unit="brpm" />
        <Tile icon={HeartPulse} label="HRV now" value={hrv != null ? Math.round(hrv).toString() : '–'} unit={hrv != null ? 'ms' : undefined}
          hint={hrv != null ? `RMSSD of the last ${Math.min(60, live.beats.length)} beats` : live.state === 'live' ? 'beat intervals not sent' : undefined} />
        <Tile icon={Activity} label="SpO2" value={num(spo2?.value ?? null)} unit={spo2 ? '%' : undefined}
          hint={spo2 ? `spot reading at ${timeOfDay(spo2.t, tz)}` : live.state === 'live' ? 'no spot reading yet' : undefined} />
        <Tile icon={Footprints} label="Steps today" value={v('steps') != null ? v('steps')!.toLocaleString() : '–'}
          meter={v('steps') != null && v('step_goal') ? v('steps')! / v('step_goal')! : undefined}
          hint={[v('step_goal') && `goal ${v('step_goal')!.toLocaleString()}`, gained(s('steps')) ? `+${gained(s('steps'))} while live` : ''].filter(Boolean).join(' · ') || undefined} />
        <Tile icon={Flame} label="Calories today" value={v('calories') != null ? v('calories')!.toLocaleString() : '–'} unit={v('calories') != null ? 'kcal' : undefined}
          hint={v('active_calories') != null ? `${v('active_calories')} active` : undefined} />
        <Tile icon={Mountain} label="Floors today" value={num(v('floors_up'))}
          meter={v('floors_up') != null && v('floors_goal') ? v('floors_up')! / v('floors_goal')! : undefined}
          hint={[v('floors_goal') != null && `goal ${v('floors_goal')}`, v('floors_down') != null && `${v('floors_down')} down`].filter(Boolean).join(' · ') || undefined} />
        <Tile icon={Zap} label="Intensity goal" value={num(v('intensity_goal'))} unit={v('intensity_goal') != null ? 'min / week' : undefined} />
      </ul>

      <Card title="The last ten minutes" right={
        <label className="flex items-center gap-2 text-[12px] text-muted">
          <input type="checkbox" checked={announce} onChange={(e) => setAnnounce(e.target.checked)} className="accent-[var(--lime)]" />
          Announce heart rate every 30 s
        </label>
      }>
        <span className="sr-only" aria-live="polite">{announce ? spoken : ''}</span>
        <div className="grid gap-1">
          {([
            { name: 'Heart rate', key: 'heart_rate', unit: 'bpm', kind: 'area', zones },
            { name: 'Stress', key: 'stress', unit: '', role: 'warn', kind: 'step', min: 0, max: 100 },
            { name: 'Body Battery', key: 'body_battery', unit: '%', role: 'extra', kind: 'line', min: 0, max: 100 },
            { name: 'Respiration', key: 'respiration', unit: 'brpm', role: 'context', kind: 'line' },
          ] as const).map((p) => {
            const ser = s(p.key)
            const input: LiveLineInput = {
              t: ser.t, v: ser.v, now, name: p.name, unit: p.unit, kind: p.kind,
              role: 'role' in p ? p.role : 'primary', min: 'min' in p ? p.min : undefined, max: 'max' in p ? p.max : undefined,
              zones: 'zones' in p ? p.zones : undefined,
            }
            return (
              <div key={p.key}>
                <p className="m-0 font-mono text-[10px] uppercase tracking-[.14em] text-faint">{p.name}{p.unit ? ` · ${p.unit}` : ''}</p>
                {ser.v.length ? (
                  <Chart option={liveLine(input, ctx)} ariaLabel={describeLiveLine(input)} height={p.key === 'heart_rate' ? 170 : 110} group={group} />
                ) : (
                  <p className="m-0 py-6 text-[13px] text-faint">{live.state === 'live' ? 'Nothing sent yet: some values only come when they change.' : '–'}</p>
                )}
              </div>
            )
          })}
        </div>
      </Card>
    </div>
  )
}

function StatusLine({ state, since, error, busy, end, name, tz, retry }: {
  state: LiveState; since: number | null; error: string; busy: boolean; end: ReturnType<typeof useLive>['end']; name: string; tz: string; retry: () => void
}) {
  const text: Record<LiveState, ReactNode> = {
    idle: <>Live is off. <b className="text-text">Go live</b> streams {name} here.</>,
    connecting: <>Connecting to {name}… this takes about 10 s.</>,
    live: <>Live from {name}{since ? ` since ${timeOfDay(since, tz)}` : ''}. Values arrive every few seconds; some only when they change.</>,
    ended: <>{endText(end)}</>,
    error: <span className={busy ? '' : 'text-err'}>{error}</span>,
    paused: <>Paused while this tab was hidden, so the watch can sync.</>,
  }
  const again = state === 'ended' || state === 'error' || state === 'paused'
  return (
    <section className="card flex flex-wrap items-center justify-between gap-3 px-5 py-3.5" aria-live="polite">
      <p className="m-0 flex items-center gap-3 text-[14px] text-muted">
        <span aria-hidden className={`h-2.5 w-2.5 shrink-0 rounded-full ${
          state === 'live' ? 'animate-pulse bg-lime' : state === 'connecting' ? 'animate-pulse bg-grey2' : state === 'error' && !busy ? 'bg-red' : 'bg-faint'}`} />
        <span>{text[state]}</span>
      </p>
      {again && (
        <button type="button" onClick={retry} className="rounded-full border border-border px-4 py-1.5 text-[13px] text-muted hover:border-lime hover:text-lime">
          {state === 'paused' ? 'Resume' : end?.reason === 'time_limit' ? 'Keep going' : 'Try again'}
        </button>
      )}
    </section>
  )
}

function Tile({ icon: Icon, label, value, unit, hint, big, tone, live, dot, meter }: {
  icon: typeof HeartPulse; label: string; value: string; unit?: string; hint?: string; big?: boolean; tone?: 'lime'; live?: boolean; dot?: string; meter?: number
}) {
  return (
    <li className={`card flex min-w-0 flex-col gap-2 p-4 ${big ? 'col-span-2 md:col-span-1' : ''}`}>
      <span className="flex items-center gap-2 font-mono text-[10px] uppercase tracking-[.14em] text-faint">
        <Icon size={13} aria-hidden className={live ? 'animate-pulse text-lime' : ''} /> {label}
      </span>
      <span className={`num leading-none ${big ? 'text-[44px]' : 'text-[28px]'} ${tone === 'lime' ? 'text-lime' : 'text-text'}`}>
        {dot && <span aria-hidden className="mr-2 inline-block h-2.5 w-2.5 rounded-full align-middle" style={{ background: dot }} />}
        {value}{unit && <span className="ml-1 font-sans text-[13px] text-muted">{unit}</span>}
      </span>
      {meter != null && (
        <span className="h-1.5 overflow-hidden rounded-full bg-track" aria-hidden>
          <span className="block h-full rounded-full bg-lime" style={{ width: `${Math.min(100, meter * 100)}%` }} />
        </span>
      )}
      {hint && <span className="truncate text-[12px] text-muted">{hint}</span>}
    </li>
  )
}
