// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Link } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { useRecentActivities } from '../../api/hooks/data'
import { distance, hm } from '../../lib/format'
import { relDay } from '../../lib/period'
import { decodePolyline } from '../../lib/polyline'
import { ActivityMap } from '../../map/ActivityMap'
import { ramps } from '../../map/ActivityRoute'
import { RouteSvg } from '../../map/RouteSvg'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'

export default function LastActivityMap({ day, today, size, isToday }: CardProps) {
  const q = useRecentActivities(50)
  const me = useMe().data
  const units = me?.units ?? 'metric'
  const tiles = !!me?.server.tiles
  const a = (q.data?.items ?? []).find((x) => x.day <= day && x.polyline)
  return (
    <Body q={[q]} empty={!a} isToday={isToday} size={size} emptyText="No activity with a route yet">
      {() => (
        <Link to={`/training/activities/${a!.id}`} className="group flex h-full flex-col gap-2">
          <div className="min-h-0 flex-1 overflow-hidden rounded-xl border border-border bg-bg2 transition-colors group-hover:border-lime-dim">
            {tiles ? (
              <ActivityMap points={decodePolyline(a!.polyline).map(([lat, lon], i) => ({ i, lat, lon }))} ramp={ramps.speed} interactive={false} height={200}
                label={`Map of ${a!.name || a!.sport_label}`} />
            ) : (
              <RouteSvg polyline={a!.polyline} width={size === 'L' ? 560 : 360} height={200} label={`Route of ${a!.name || a!.sport_label}`} className="h-full w-full" />
            )}
          </div>
          <div className="flex items-baseline justify-between gap-3">
            <span className="min-w-0 truncate text-[14px] font-medium">{a!.name || a!.sport_label}</span>
            <span className="shrink-0 font-mono text-[12px] text-muted">
              {relDay(a!.day, today)} · {distance(a!.distance_m, units)} · {hm(a!.timer_s ?? a!.elapsed_s)}
            </span>
          </div>
        </Link>
      )}
    </Body>
  )
}
