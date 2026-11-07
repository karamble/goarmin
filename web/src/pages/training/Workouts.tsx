// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Link } from 'react-router-dom'
import { useWorkouts } from '../../api/hooks/training'
import { sportIcon } from '../../glance/cards/sport'
import { hm } from '../../lib/format'
import { Loaded } from '../health/parts'

export const sportWords = (sport: string, sub?: string) => {
  const w = (s: string) => s.replace(/_/g, ' ')
  if (sub && sub !== 'generic') return w(sub)[0].toUpperCase() + w(sub).slice(1)
  return sport ? w(sport)[0].toUpperCase() + w(sport).slice(1) : 'Workout'
}

/** Workouts lists the workouts saved on the watch. */
export default function Workouts() {
  const q = useWorkouts()
  return (
    <Loaded q={[q]}>
      {() => (q.data ?? []).length === 0 ? (
        <section className="card grid place-items-center px-6 py-12 text-center">
          <p className="m-0 font-display text-[22px] uppercase">No saved workouts</p>
          <p className="m-0 mt-2 max-w-[480px] text-[14px] text-muted">
            Workouts you save to the watch (from Garmin Connect, or made on the watch) arrive with the next sync.
          </p>
        </section>
      ) : (
        <ul className="m-0 grid list-none gap-4 p-0 sm:grid-cols-2 xl:grid-cols-3" aria-label="Workouts on the watch">
          {q.data!.map((w) => {
            const Icon = sportIcon(w.sport)
            return (
              <li key={w.id}>
                <Link to={`/training/workouts/${w.id}`} className="card card-hover group flex h-full items-start gap-4 p-5">
                  <span className="grid h-11 w-11 shrink-0 place-items-center rounded-full border border-border text-muted group-hover:border-lime-dim group-hover:text-lime">
                    <Icon size={18} strokeWidth={1.75} aria-hidden />
                  </span>
                  <span className="min-w-0">
                    <span className="block truncate text-[16px] font-medium text-text">{w.name}</span>
                    <span className="mt-1 block font-mono text-[11px] text-faint">
                      {sportWords(w.sport, w.sub_sport)} · {w.step_count} steps{w.estimated_s ? ` · about ${hm(w.estimated_s)}` : ''}
                    </span>
                  </span>
                </Link>
              </li>
            )
          })}
        </ul>
      )}
    </Loaded>
  )
}
