// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { LayoutGrid, List } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { useActivitiesIn, useActivityPages } from '../../api/hooks/training'
import type { Activity } from '../../api/types'
import { HeaderActions } from '../../app/section/HeaderSlot'
import { usePeriod } from '../../app/section/usePeriod'
import { load, save } from '../../app/storage'
import { sportIcon } from '../../glance/cards/sport'
import { speedText, sportGroup } from '../../lib/activity'
import { distance, elevation, hm, timeOfDay, type Units } from '../../lib/format'
import { relDay } from '../../lib/period'
import { RouteSvg } from '../../map/RouteSvg'
import { Button } from '../../ui/kit'
import { Empty, Kpis, Loaded } from '../health/parts'
import { Lifetime } from './Lifetime'

type View = 'cards' | 'table'

export default function Activities() {
  const p = usePeriod()
  const all = p.period === 'all'
  const pages = useActivityPages(30)
  const inRange = useActivitiesIn(p.range.from, p.range.to, !all)
  const [sport, setSport] = useState('')
  const [view, setViewState] = useState<View>(() => (load('activities.view') === 'table' ? 'table' : 'cards'))
  const setView = (v: View) => {
    setViewState(v)
    save('activities.view', v)
  }
  const units = useMe().data?.units ?? 'metric'
  const list = useMemo(() => (all ? (pages.data?.pages ?? []).flatMap((x) => x.items) : (inRange.data ?? [])), [all, pages.data, inRange.data])
  const sports = useMemo(() => [...new Map(list.map((a) => [a.sport, sportGroup(a)])).entries()], [list])
  const shown = sport ? list.filter((a) => a.sport === sport) : list
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
      <HeaderActions>
        <div role="group" aria-label="View" className="inline-flex rounded-full border border-border p-0.5">
          {([['cards', LayoutGrid, 'Cards'], ['table', List, 'Table']] as const).map(([v, Icon, label]) => (
            <button key={v} type="button" aria-pressed={view === v} aria-label={label} onClick={() => setView(v)}
              className={`grid h-8 w-9 place-items-center rounded-full ${view === v ? 'bg-lime-fill text-lime' : 'text-muted hover:text-text'}`}>
              <Icon size={15} />
            </button>
          ))}
        </div>
      </HeaderActions>
      <Loaded q={[all ? pages : inRange]}>
        {() => list.length === 0 ? <Empty p={p} what="activities" /> : (
          <>
            <Totals list={shown} units={units} label={all ? 'Loaded so far' : undefined} />
            {all && !sport && <Lifetime units={units} />}
            {sports.length > 1 && (
              <div role="group" aria-label="Sport" className="flex flex-wrap gap-2">
                {[['', 'All'] as [string, string], ...sports].map(([k, label]) => (
                  <button key={k || 'all'} type="button" aria-pressed={sport === k} onClick={() => setSport(k)}
                    className={`rounded-full border px-3.5 py-1.5 text-[13px] ${sport === k ? 'border-lime-dim bg-lime-fill text-lime' : 'border-border text-muted hover:text-text'}`}>
                    {label}
                  </button>
                ))}
              </div>
            )}
            {view === 'cards' ? (
              <ul className="m-0 grid list-none gap-4 p-0 sm:grid-cols-2 xl:grid-cols-3">
                {shown.map((a) => <Card key={a.id} a={a} units={units} today={p.today} tz={p.tz} />)}
              </ul>
            ) : (
              <Table list={shown} units={units} tz={p.tz} />
            )}
            {all && pages.hasNextPage && (
              <div className="flex justify-center">
                <Button small disabled={pages.isFetchingNextPage} onClick={() => void pages.fetchNextPage()}>
                  {pages.isFetchingNextPage ? 'Loading…' : 'Load more'}
                </Button>
              </div>
            )}
          </>
        )}
      </Loaded>
    </div>
  )
}

/** Totals sums a list of activities: count, distance, time, climb, then per sport. */
function Totals({ list, units, label }: { list: Activity[]; units: Units; label?: string }) {
  const sum = (k: 'distance_m' | 'timer_s' | 'ascent_m' | 'kcal') => list.reduce((a, x) => a + (x[k] ?? (k === 'timer_s' ? x.elapsed_s ?? 0 : 0)), 0)
  const bySport = [...list.reduce((m, a) => m.set(a.sport_label, (m.get(a.sport_label) ?? 0) + 1), new Map<string, number>()).entries()]
    .sort((a, b) => b[1] - a[1])
  return (
    <section className="card p-5 sm:p-6">
      <Kpis items={[
        { label: 'Activities', value: `${list.length}`, tone: 'lime', hint: label },
        { label: 'Distance', value: distance(sum('distance_m'), units) },
        { label: 'Time', value: hm(sum('timer_s')) },
        { label: 'Climbed', value: elevation(sum('ascent_m'), units) },
        { label: 'Calories', value: `${Math.round(sum('kcal'))}`, unit: 'kcal' },
      ]} />
      {bySport.length > 1 && (
        <p className="m-0 mt-4 text-[13px] text-muted">{bySport.map(([s, n]) => `${n} × ${s}`).join(' · ')}</p>
      )}
    </section>
  )
}

function Card({ a, units, today, tz }: { a: Activity; units: Units; today: string; tz: string }) {
  const Icon = sportIcon(a.sport)
  return (
    <li>
      <Link to={`/training/activities/${a.id}`} className="card card-hover group flex h-full flex-col overflow-hidden">
        <div className="h-[150px] border-b border-border bg-bg2">
          {a.polyline ? (
            <RouteSvg polyline={a.polyline} width={420} height={150} label={`Route of ${a.name || a.sport_label}`} className="h-full w-full" />
          ) : (
            <div className="grid h-full place-items-center text-faint"><Icon size={34} strokeWidth={1.25} aria-hidden /></div>
          )}
        </div>
        <div className="flex flex-1 flex-col gap-3 p-4">
          <div className="flex items-center gap-3">
            <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full border border-border text-muted group-hover:border-lime-dim group-hover:text-lime">
              <Icon size={15} strokeWidth={1.75} aria-hidden />
            </span>
            <span className="min-w-0">
              <span className="block truncate text-[15px] font-medium text-text">{a.name || a.sport_label}</span>
              <span className="block truncate font-mono text-[11px] text-faint">{relDay(a.day, today)} · {timeOfDay(a.start, tz)} · {a.sport_label}</span>
            </span>
          </div>
          <dl className="m-0 mt-auto grid grid-cols-3 gap-2">
            {[
              ['Distance', a.distance_m ? distance(a.distance_m, units) : '–'],
              ['Time', hm(a.timer_s ?? a.elapsed_s)],
              [a.avg_speed ? 'Pace' : 'Avg HR', a.avg_speed ? speedText(a.avg_speed, a.sport, units) : a.avg_hr ? `${a.avg_hr} bpm` : '–'],
            ].map(([k, v]) => (
              <div key={k} className="min-w-0">
                <dt className="font-mono text-[10px] uppercase tracking-[.12em] text-faint">{k}</dt>
                <dd className="num m-0 truncate text-[14px]">{v}</dd>
              </div>
            ))}
          </dl>
        </div>
      </Link>
    </li>
  )
}

function Table({ list, units, tz }: { list: Activity[]; units: Units; tz: string }) {
  return (
    <div className="card overflow-x-auto">
      <table className="w-full min-w-[720px] border-collapse text-left text-[13px]">
        <thead>
          <tr className="font-mono text-[10px] uppercase tracking-[.12em] text-faint">
            {['Day', 'Activity', 'Distance', 'Time', 'Pace / speed', 'Avg HR', 'Climbed'].map((h) => <th key={h} className="px-3 py-2.5 font-normal">{h}</th>)}
          </tr>
        </thead>
        <tbody>
          {list.map((a) => (
            <tr key={a.id} className="border-t border-border hover:bg-panel2">
              <td className="whitespace-nowrap px-3 py-2 text-muted">{a.day} <span className="text-faint">{timeOfDay(a.start, tz)}</span></td>
              <td className="px-3 py-2"><Link to={`/training/activities/${a.id}`} className="text-text hover:text-lime">{a.name || a.sport_label}</Link></td>
              <td className="num px-3 py-2">{a.distance_m ? distance(a.distance_m, units) : '–'}</td>
              <td className="num px-3 py-2">{hm(a.timer_s ?? a.elapsed_s)}</td>
              <td className="num px-3 py-2">{speedText(a.avg_speed, a.sport, units)}</td>
              <td className="num px-3 py-2">{a.avg_hr ?? '–'}</td>
              <td className="num px-3 py-2">{elevation(a.ascent_m, units)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
