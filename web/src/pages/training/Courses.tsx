// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Link } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { useCourses } from '../../api/hooks/training'
import { sportIcon } from '../../glance/cards/sport'
import { distance, elevation } from '../../lib/format'
import { RouteSvg } from '../../map/RouteSvg'
import { Loaded } from '../health/parts'

/** Courses lists the routes saved on the watch. */
export default function Courses() {
  const q = useCourses()
  const units = useMe().data?.units ?? 'metric'
  return (
    <Loaded q={[q]}>
      {() => (q.data ?? []).length === 0 ? (
        <section className="card grid place-items-center px-6 py-12 text-center">
          <p className="m-0 font-display text-[22px] uppercase">No saved courses</p>
          <p className="m-0 mt-2 max-w-[480px] text-[14px] text-muted">Courses you save to the watch arrive with the next sync.</p>
        </section>
      ) : (
        <ul className="m-0 grid list-none gap-4 p-0 sm:grid-cols-2 xl:grid-cols-3" aria-label="Courses on the watch">
          {q.data!.map((c) => {
            const Icon = sportIcon(c.sport)
            return (
              <li key={c.id}>
                <Link to={`/training/courses/${c.id}`} className="card card-hover group flex h-full flex-col overflow-hidden">
                  <div className="h-[150px] border-b border-border bg-bg2">
                    {c.polyline ? <RouteSvg polyline={c.polyline} width={420} height={150} label={`Route of ${c.name}`} className="h-full w-full" />
                      : <div className="grid h-full place-items-center text-faint"><Icon size={34} strokeWidth={1.25} aria-hidden /></div>}
                  </div>
                  <div className="flex flex-col gap-2 p-4">
                    <span className="truncate text-[15px] font-medium text-text">{c.name}</span>
                    <span className="font-mono text-[11px] text-faint">
                      {distance(c.distance_m, units)} · +{elevation(c.ascent_m, units)}{c.point_count ? ` · ${c.point_count} points` : ''}
                    </span>
                  </div>
                </Link>
              </li>
            )
          })}
        </ul>
      )}
    </Loaded>
  )
}
