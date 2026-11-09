// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Plus, Search, Watch } from 'lucide-react'
import { useMemo } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useWorkouts } from '../../api/hooks/training'
import type { Workout, WorkoutOrigin, WorkoutWatch } from '../../api/types'
import { sportIcon } from '../../glance/cards/sport'
import { ago, hm } from '../../lib/format'
import { filterLibrary, originText, originWords, presenceText, sportKey, watchShares, type LibraryFilter } from '../../lib/workoutLibrary'
import { HeaderActions } from '../../app/section/HeaderSlot'
import { Loaded } from '../health/parts'

export { sportWords } from '../../lib/workoutLibrary'

/** The colour of a presence badge's dot. */
const tone: Record<WorkoutWatch['state'], string> = { on: 'bg-lime', removed: 'bg-amber', absent: 'bg-faint', unknown: 'border border-faint' }

/**
 * Workouts is the account's workout library: every workout goarmin keeps, from the watch's files
 * and from a Garmin Connect export, once each, with whether each watch has it now. Search and
 * filters live in the address, so a filtered view can be bookmarked.
 */
export default function Workouts() {
  const q = useWorkouts()
  const [params, setParams] = useSearchParams()
  const f = useMemo((): LibraryFilter => ({
    q: params.get('q') ?? '', sport: params.get('sport') ?? '', where: params.get('where') ?? '',
    origin: (params.get('origin') ?? '') as LibraryFilter['origin'],
  }), [params])
  const set = (k: keyof LibraryFilter, v: string) => {
    const next = new URLSearchParams(params)
    if (v) next.set(k, v)
    else next.delete(k)
    setParams(next, { replace: true })
  }
  const all = useMemo(() => q.data ?? [], [q.data])
  const shown = useMemo(() => filterLibrary(all, f), [all, f])
  return (
    <>
    <HeaderActions>
      <Link to="/training/workouts/new" className="inline-flex items-center gap-1.5 rounded-full border border-border px-3 py-1.5 text-[13px] text-lime hover:border-lime">
        <Plus size={14} aria-hidden /> New workout
      </Link>
    </HeaderActions>
    <Loaded q={[q]}>
      {() => all.length === 0 ? (
        <section className="card grid place-items-center px-6 py-12 text-center">
          <p className="m-0 font-display text-[22px] uppercase">No workouts yet</p>
          <p className="m-0 mt-2 max-w-[520px] text-[14px] text-muted">
            Workouts on your watch (sent from Garmin Connect, or made on the watch) arrive with the next sync. The workouts of your
            Garmin Connect account come with its export, under <Link to="/devices/import" className="text-lime hover:underline">Devices › Import</Link>.
            Or <Link to="/training/workouts/new" className="text-lime hover:underline">make one here</Link>.
          </p>
        </section>
      ) : (
        <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-4">
          <Summary all={all} />
          <Filters all={all} f={f} set={set} />
          <p className="m-0 text-[12px] text-muted" aria-live="polite">
            {shown.length === all.length ? `${all.length} workouts` : `${shown.length} of ${all.length} workouts`}
          </p>
          {shown.length === 0 ? (
            <p className="card m-0 px-6 py-10 text-center text-[14px] text-muted">No workout fits these filters.</p>
          ) : (
            <ul className="m-0 grid list-none grid-cols-[minmax(0,1fr)] gap-4 p-0 sm:grid-cols-2 xl:grid-cols-3" aria-label="Workouts">
              {shown.map((w) => <WorkoutCard key={w.id} w={w} />)}
            </ul>
          )}
        </div>
      )}
    </Loaded>
    </>
  )
}

/** Summary says what the library holds and what each watch has of it. */
function Summary({ all }: { all: Workout[] }) {
  const shares = watchShares(all)
  const fromWatch = all.filter((w) => w.origins.includes('watch')).length
  const fromGC = all.filter((w) => w.origins.includes('garmin_connect')).length
  return (
    <section className="card p-4 sm:p-5" aria-label="Library">
      <p className="m-0 text-[14px] text-text">
        {all.length} workouts in your library
        <span className="text-muted">
          {fromWatch > 0 && ` · ${fromWatch} from a watch`}{fromGC > 0 && ` · ${fromGC} from Garmin Connect`}
        </span>
      </p>
      {shares.length > 0 && (
        <ul className="m-0 mt-2 grid list-none gap-1 p-0">
          {shares.map((s) => (
            <li key={s.device_id} className="flex flex-wrap items-center gap-x-2 text-[13px] text-muted">
              <Watch size={14} aria-hidden className="text-faint" />
              <span className="text-text">{s.name}</span>
              {s.checked_at ? (
                <span>
                  {s.on} on it{s.removed > 0 && ` · ${s.removed} removed`} · checked {ago(s.checked_at)}
                </span>
              ) : (
                <span>not checked yet: the next sync lists its workouts</span>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function Chips({ label, value, options, onChange }: { label: string; value: string; options: [string, string][]; onChange: (v: string) => void }) {
  return (
    <div role="group" aria-label={label} className="flex flex-wrap gap-2">
      {options.map(([k, text]) => (
        <button key={k || 'all'} type="button" aria-pressed={value === k} onClick={() => onChange(k)}
          className={`rounded-full border px-3.5 py-1.5 text-[13px] ${value === k ? 'border-lime-dim bg-lime-fill text-lime' : 'border-border text-muted hover:text-text'}`}>
          {text}
        </button>
      ))}
    </div>
  )
}

function Filters({ all, f, set }: { all: Workout[]; f: LibraryFilter; set: (k: keyof LibraryFilter, v: string) => void }) {
  const sports = [...new Set(all.map(sportKey))].sort()
  const origins = (['goarmin', 'watch', 'garmin_connect'] as WorkoutOrigin[]).filter((o) => all.some((w) => w.origins.includes(o)))
  const watches = watchShares(all)
  return (
    <section className="grid min-w-0 gap-3" aria-label="Filters">
      <label className="relative block max-w-[420px]">
        <span className="sr-only">Search workouts</span>
        <Search size={15} aria-hidden className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-faint" />
        <input type="search" value={f.q} onChange={(e) => set('q', e.target.value)} placeholder="Search workouts"
          className="w-full rounded-full border border-border bg-bg2 py-2 pl-9 pr-3 text-[14px] text-text placeholder:text-faint focus:border-lime focus:outline-none" />
      </label>
      {sports.length > 1 && <Chips label="Sport" value={f.sport} onChange={(v) => set('sport', v)} options={[['', 'All sports'], ...sports.map((s) => [s, s] as [string, string])]} />}
      {origins.length > 1 && (
        <Chips label="Source" value={f.origin} onChange={(v) => set('origin', v)}
          options={[['', 'Every source'], ...origins.map((o) => [o, originWords[o]] as [string, string])]} />
      )}
      {watches.length > 0 && (
        <Chips label="On a watch" value={f.where} onChange={(v) => set('where', v)}
          options={[['', 'Anywhere'], ...watches.map((w) => [`on:${w.device_id}`, `On ${w.name}`] as [string, string]), ['none', 'On no watch']]} />
      )}
    </section>
  )
}

function WorkoutCard({ w }: { w: Workout }) {
  const Icon = sportIcon(w.sport)
  const meta = [sportKey(w), `${w.step_count} steps`, w.estimated_s ? `about ${hm(w.estimated_s)}` : '', w.exercise_count ? `${w.exercise_count} exercises` : '']
  return (
    <li className="min-w-0">
      <Link to={`/training/workouts/${w.id}`} className="card card-hover group flex h-full min-w-0 items-start gap-4 p-5">
        <span className="grid h-11 w-11 shrink-0 place-items-center rounded-full border border-border text-muted group-hover:border-lime-dim group-hover:text-lime">
          <Icon size={18} strokeWidth={1.75} aria-hidden />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[16px] font-medium text-text">{w.name}</span>
          <span className="mt-1 block font-mono text-[11px] text-faint">{meta.filter(Boolean).join(' · ')}</span>
          <span className="mt-3 flex flex-wrap gap-1.5">
            {w.watches.map((x) => <Presence key={x.device_id} x={x} named={w.watches.length > 1} />)}
            <span className="rounded-full border border-border px-2 py-0.5 text-[11px] text-muted">{originText(w.origins)}</span>
          </span>
        </span>
      </Link>
    </li>
  )
}

/** Presence is one watch's badge on a workout: on it, removed, not on it, or not checked. With
 * one watch the badge leaves its name out (named: several watches; long: the full sentence). */
export function Presence({ x, long, named }: { x: WorkoutWatch; long?: boolean; named?: boolean }) {
  const t = presenceText(x)
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] ${x.state === 'on' ? 'border-lime-dim text-lime' : 'border-border text-muted'}`}
      title={t.long}>
      <span aria-hidden className={`h-1.5 w-1.5 rounded-full ${tone[x.state]}`} />
      {long ? t.long : <><span aria-hidden>{named ? `${x.name}: ${t.short}` : t.short}</span><span className="sr-only">{t.long}</span></>}
    </span>
  )
}
