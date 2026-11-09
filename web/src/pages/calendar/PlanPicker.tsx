// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { usePlanActions } from '../../api/hooks/calendar'
import { useWorkouts } from '../../api/hooks/training'
import { sportIcon } from '../../glance/cards/sport'
import { hm } from '../../lib/format'
import { filterLibrary, sportKey } from '../../lib/workoutLibrary'
import { Sheet } from '../../ui/Sheet'
import { useToast } from '../../ui/Toast'
import { Presence } from '../training/Workouts'

const longDay = new Intl.DateTimeFormat(undefined, { weekday: 'long', day: 'numeric', month: 'long', timeZone: 'UTC' })

/** PlanPicker puts a library workout on a day: search, filter by sport, pick. */
export function PlanPicker({ day, today, onClose }: { day: string; today: string; onClose: () => void }) {
  const lib = useWorkouts()
  const { add } = usePlanActions()
  const toast = useToast()
  const [q, setQ] = useState('')
  const [sport, setSport] = useState('')
  const [on, setOn] = useState(day < today ? today : day)
  const all = useMemo(() => lib.data ?? [], [lib.data])
  const sports = [...new Set(all.map(sportKey))].sort()
  const shown = filterLibrary(all, { q, sport, origin: '', where: '' })
  const plan = (id: number, name: string) => add.mutate({ day: on, workout_id: id }, {
    onSuccess: () => {
      toast({ text: `${name} planned for ${longDay.format(new Date(`${on}T00:00:00Z`))}` })
      onClose()
    },
    onError: (e) => toast({ text: `Not planned: ${(e as Error).message}`, tone: 'error' }),
  })
  return (
    <Sheet title="Plan a workout" onClose={onClose}>
      <div className="grid gap-3">
        <label className="grid gap-1 text-[13px] text-muted">
          On
          <input type="date" value={on} min={today} onChange={(e) => setOn(e.target.value)}
            className="w-full max-w-[220px] rounded-lg border border-border bg-bg2 px-3 py-2 text-[14px] text-text focus:border-lime focus:outline-none" />
        </label>
        <label className="relative block">
          <span className="sr-only">Search workouts</span>
          <Search size={15} aria-hidden className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-faint" />
          <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search workouts"
            className="w-full rounded-full border border-border bg-bg2 py-2 pl-9 pr-3 text-[14px] text-text placeholder:text-faint focus:border-lime focus:outline-none" />
        </label>
        {sports.length > 1 && (
          <label className="block">
            <span className="sr-only">Sport</span>
            <select value={sport} onChange={(e) => setSport(e.target.value)}
              className="w-full rounded-full border border-border bg-bg2 px-3 py-2 text-[14px] text-text focus:border-lime focus:outline-none">
              <option value="">All sports</option>
              {sports.map((s) => <option key={s} value={s}>{s}</option>)}
            </select>
          </label>
        )}
        {lib.isPending && <p className="m-0 text-[13px] text-muted">Loading the library…</p>}
        {!lib.isPending && all.length === 0 && <p className="m-0 text-[13px] text-muted">Your workout library is empty: workouts arrive from the watch and from a Garmin Connect export.</p>}
        <ul className="m-0 grid list-none gap-1.5 p-0" aria-label="Workouts to plan">
          {shown.map((w) => {
            const Icon = sportIcon(w.sport)
            return (
              <li key={w.id}>
                <button type="button" disabled={add.isPending} onClick={() => plan(w.id, w.name)}
                  className="flex w-full items-center gap-3 rounded-lg bg-bg2 px-3 py-2 text-left hover:bg-panel2 disabled:opacity-50">
                  <Icon size={16} strokeWidth={1.75} className="shrink-0 text-faint" aria-hidden />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-[14px] text-text">{w.name}</span>
                    <span className="block text-[12px] text-muted">{[sportKey(w), w.estimated_s ? `about ${hm(w.estimated_s)}` : ''].filter(Boolean).join(' · ')}</span>
                  </span>
                  <span className="hidden shrink-0 gap-1 sm:flex">{w.watches.filter((x) => x.state === 'on').slice(0, 1).map((x) => <Presence key={x.device_id} x={x} />)}</span>
                </button>
              </li>
            )
          })}
        </ul>
      </div>
    </Sheet>
  )
}
