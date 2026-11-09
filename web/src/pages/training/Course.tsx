// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Download } from 'lucide-react'
import { useCallback, useMemo, useRef, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { useCourse, useCourseStreams } from '../../api/hooks/training'
import { HeaderActions, useSectionTitle } from '../../app/section/HeaderSlot'
import { Chart } from '../../charts'
import type { EChartsType } from '../../charts/core/echarts'
import { describeTrackPanel, trackPanel, xLabel } from '../../charts/builders/trackPanel'
import { nearestIndex, routePoints } from '../../lib/activity'
import { distance, elevation } from '../../lib/format'
import { ActivityMap } from '../../map/ActivityMap'
import { ActivityRoute, ramps } from '../../map/ActivityRoute'
import { Card, chartCtx, Kpis, Loaded } from '../health/parts'
import { sportWords } from '../../lib/workoutLibrary'

const pointWords: Record<string, string> = {
  left: 'Turn left', right: 'Turn right', slight_left: 'Bear left', slight_right: 'Bear right', sharp_left: 'Sharp left', sharp_right: 'Sharp right',
  straight: 'Straight on', u_turn: 'Turn around', water: 'Water', food: 'Food', summit: 'Summit', valley: 'Valley', danger: 'Danger',
  first_aid: 'First aid', generic: 'Point', sprint: 'Sprint', segment_start: 'Segment start', segment_end: 'Segment end',
}

/** Course shows a saved route: its points numbered on the map, and its elevation along the way. */
export default function CoursePage() {
  const id = Number(useParams().id)
  const c = useCourse(id)
  const s = useCourseStreams(id)
  const me = useMe().data
  const units = me?.units ?? 'metric'
  const tiles = !!me?.server.tiles
  const imperial = units === 'imperial'
  useSectionTitle(c.data?.name)
  const [hover, setHover] = useState<number | null>(null)
  const chart = useRef<EChartsType | null>(null)
  const ctx = useMemo(() => chartCtx(''), [])
  const points = useMemo(() => (s.data ? routePoints(s.data) : []), [s.data])
  const x = useMemo(() => {
    let last = 0
    return (s.data?.dist ?? []).map((d) => (last = d ?? last))
  }, [s.data])
  const alt = useMemo(() => (s.data?.alt ?? []).map((v) => (v == null ? null : imperial ? v * 3.28084 : v)), [s.data, imperial])
  const marks = useMemo(() => (c.data?.points ?? []).map((p) => ({ lat: p.lat, lon: p.lon, label: p.name || pointWords[p.type] || p.type })), [c.data])
  const onPointer = useMemo(() => ({
    updateAxisPointer: (e: unknown) => {
      const v = (e as { axesInfo?: { value: number }[] }).axesInfo?.[0]?.value
      if (v != null) setHover(nearestIndex(x, v))
    },
  }), [x])
  const onRouteHover = useCallback((i: number | null) => {
    setHover(i)
    if (i == null) chart.current?.dispatchAction({ type: 'hideTip' })
    else chart.current?.dispatchAction({ type: 'showTip', seriesIndex: 0, dataIndex: i })
  }, [])
  const onChart = useCallback((ch: EChartsType | null) => { chart.current = ch }, [])
  if (!(id > 0)) return <NotFound />
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
      <HeaderActions>
        {c.data && (
          <a href={`/api/courses/${id}/fit`} download className="inline-flex h-9 items-center gap-2 rounded-full border border-border px-4 text-[13px] text-muted hover:border-lime hover:text-lime">
            <Download size={14} /> FIT file
          </a>
        )}
      </HeaderActions>
      <Loaded q={[c, s]}>
        {() => {
          const co = c.data
          if (!co) return <NotFound />
          const input = { x, xKind: 'distance' as const, imperial, v: alt, name: 'Elevation', unit: imperial ? 'ft' : 'm', kind: 'area' as const, role: 'context' as const }
          return (
            <>
              <section className="card p-5 sm:p-6">
                <Kpis items={[
                  { label: 'Distance', value: distance(co.distance_m, units), tone: 'lime' },
                  { label: 'Climb', value: elevation(co.ascent_m, units) },
                  { label: 'Descent', value: elevation(co.descent_m, units) },
                  { label: 'Sport', value: sportWords(co.sport) },
                  { label: 'Points', value: `${co.point_count}` },
                ]} />
              </section>
              {points.length > 1 && (
                <Card title="Route">
                  <div className="overflow-hidden rounded-xl border border-border bg-bg2">
                    {tiles ? (
                      <ActivityMap points={points} values={s.data?.alt ?? undefined} ramp={ramps.alt} hover={hover} onHover={onRouteHover}
                        marks={marks} label={`Map of ${co.name}, the route coloured by elevation, with ${marks.length} points`} />
                    ) : (
                      <ActivityRoute points={points} values={s.data?.alt ?? undefined} ramp={ramps.alt} hover={hover} onHover={onRouteHover}
                        marks={marks} label={`Route of ${co.name}, coloured by elevation, with ${marks.length} points`} />
                    )}
                  </div>
                  <p className="m-0 mt-3 font-mono text-[12px] text-muted" aria-live="polite">
                    {hover != null && x[hover] != null ? `${xLabel(x[hover], 'distance', imperial)} · ${alt[hover] != null ? `${Math.round(alt[hover]!)} ${imperial ? 'ft' : 'm'}` : ''}`
                      : 'Point at the route or the profile.'}
                  </p>
                </Card>
              )}
              {alt.some((v) => v != null) && (
                <Card title="Elevation">
                  <Chart option={trackPanel(input, ctx)} ariaLabel={describeTrackPanel(input)} height={180} onEvents={onPointer} onChart={onChart} />
                </Card>
              )}
              {co.points && co.points.length > 0 && (
                <Card title={`Points (${co.points.length})`}>
                  <ol className="m-0 grid list-none gap-1.5 p-0">
                    {co.points.map((p, i) => (
                      <li key={i} className="grid grid-cols-[2rem_minmax(0,1fr)_6rem] items-center gap-3 text-[13px]">
                        <span className="grid h-6 w-6 place-items-center rounded-full border border-cyan font-mono text-[10px] text-cyan">{i + 1}</span>
                        <span className="truncate text-text">{p.name || pointWords[p.type] || p.type}{p.name && pointWords[p.type] ? <span className="text-muted"> · {pointWords[p.type]}</span> : null}</span>
                        <span className="num text-right text-muted">{distance(p.distance_m, units)}</span>
                      </li>
                    ))}
                  </ol>
                </Card>
              )}
            </>
          )
        }}
      </Loaded>
    </div>
  )
}

function NotFound() {
  return (
    <section className="card grid place-items-center px-6 py-12 text-center">
      <p className="m-0 font-display text-[22px] uppercase">Course not found</p>
      <Link to="/training/courses" className="mt-4 text-[14px] text-lime hover:underline">All courses</Link>
    </section>
  )
}
