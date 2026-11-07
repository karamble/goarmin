// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { ChevronLeft, ChevronRight, Download } from 'lucide-react'
import { useCallback, useId, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useMe } from '../../api/hooks/auth'
import { usePrefs } from '../../api/hooks/prefs'
import { useRecentActivities } from '../../api/hooks/data'
import { useActivity, useNeighbours, useStreams, useZones } from '../../api/hooks/training'
import type { ActivityDetail, Streams } from '../../api/types'
import { useHotkeys } from '../../app/hotkeys/useHotkeys'
import { usePaletteActions } from '../../app/palette/actions'
import { HeaderActions, useSectionTitle } from '../../app/section/HeaderSlot'
import { Chart } from '../../charts'
import type { EChartsType } from '../../charts/core/echarts'
import type { Role } from '../../charts/theme/tokens'
import { describeTrackPanel, trackPanel, xLabel } from '../../charts/builders/trackPanel'
import { cadenceFactor, nearestIndex, niceAxis, paceSteps, robustRange, speedSteps, routePoints, speedKind, speedText, speedValue, splits, timeInLimits } from '../../lib/activity'
import { secondsIn, zonesFor, type HrZones } from '../../lib/zones'
import { clock, distance, elevation, hm, timeOfDay, type Units } from '../../lib/format'
import { ActivityMap } from '../../map/ActivityMap'
import { ActivityRoute, ramps } from '../../map/ActivityRoute'
import { Card, chartCtx, Kpis, Loaded, num } from '../health/parts'

export default function ActivityPage() {
  const id = Number(useParams().id)
  const me = useMe().data!
  const units = me.units
  const a = useActivity(id)
  const s = useStreams(id)
  const nb = useNeighbours(id)
  const recent = useRecentActivities(50)
  const navigate = useNavigate()
  useSectionTitle(a.data ? a.data.name || a.data.sport_label : undefined)

  // The palette offers this activity's download and its neighbours while the page is open.
  const paletteItems = useMemo(() => !a.data ? [] : [
    { id: 'activity-fit', label: 'Download this activity as FIT', icon: Download, run: () => { window.location.href = `/api/activities/${id}/fit` } },
    ...(nb.data?.prev ? [{ id: 'activity-prev', label: 'Previous activity', hint: nb.data.prev.name || nb.data.prev.sport_label, icon: ChevronLeft, run: () => navigate(`/training/activities/${nb.data!.prev!.id}`) }] : []),
    ...(nb.data?.next ? [{ id: 'activity-next', label: 'Next activity', hint: nb.data.next.name || nb.data.next.sport_label, icon: ChevronRight, run: () => navigate(`/training/activities/${nb.data!.next!.id}`) }] : []),
  ], [a.data, id, nb.data, navigate])
  usePaletteActions('activity', paletteItems)

  // ← and → step to the previous and next activity (not while typing or in a dialog).
  useHotkeys({
    ArrowLeft: () => nb.data?.prev && navigate(`/training/activities/${nb.data.prev.id}`),
    ArrowRight: () => nb.data?.next && navigate(`/training/activities/${nb.data.next.id}`),
  })

  // Zones: the watch's own for this activity, else its sport profile's or default ones, else an
  // estimate from the highest heart rate across recent activities and this one.
  const zinfo = useZones()
  const prefs = usePrefs()
  const zones = useMemo(() => {
    const seen = [...(recent.data?.items ?? []).map((x) => x.max_hr ?? 0), a.data?.max_hr ?? 0]
    return zonesFor({ activity: a.data?.zones, sport: a.data?.sport, subSport: a.data?.sub_sport, info: zinfo.data, estimateMax: Math.max(...seen, 0) || undefined,
      custom: prefs.zones.source === 'custom' ? prefs.zones : null, prefer: prefs.zones.source })
  }, [recent.data, a.data, zinfo.data, prefs.zones])

  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
      <HeaderActions>
        <nav aria-label="Other activities" className="flex items-center gap-1">
          {nb.data?.prev ? (
            <Link to={`/training/activities/${nb.data.prev.id}`} aria-label="Previous activity" title="Previous activity (←)"
              className="grid h-9 w-9 place-items-center rounded-full border border-border text-muted hover:border-lime hover:text-lime"><ChevronLeft size={16} /></Link>
          ) : <span className="grid h-9 w-9 place-items-center rounded-full border border-border text-faint opacity-40"><ChevronLeft size={16} /></span>}
          {nb.data?.next ? (
            <Link to={`/training/activities/${nb.data.next.id}`} aria-label="Next activity" title="Next activity (→)"
              className="grid h-9 w-9 place-items-center rounded-full border border-border text-muted hover:border-lime hover:text-lime"><ChevronRight size={16} /></Link>
          ) : <span className="grid h-9 w-9 place-items-center rounded-full border border-border text-faint opacity-40"><ChevronRight size={16} /></span>}
        </nav>
        {a.data && (
          <a href={`/api/activities/${id}/fit`} download className="inline-flex h-9 items-center gap-2 rounded-full border border-border px-4 text-[13px] text-muted hover:border-lime hover:text-lime">
            <Download size={14} /> FIT file
          </a>
        )}
      </HeaderActions>
      {!(id > 0) ? <NotFound /> : (
        <Loaded q={[a, s]}>
          {() => !a.data ? <NotFound /> : (
            <Detail a={a.data} s={s.data ?? null} units={units} tz={me.timezone} zones={zones} />
          )}
        </Loaded>
      )}
    </div>
  )
}

function NotFound() {
  return (
    <section className="card grid place-items-center px-6 py-12 text-center">
      <p className="m-0 font-display text-[22px] uppercase">Activity not found</p>
      <p className="m-0 mt-2 max-w-[480px] text-[14px] text-muted">It was deleted, or it belongs to another account.</p>
      <Link to="/training/activities" className="mt-4 text-[14px] text-lime hover:underline">All activities</Link>
    </section>
  )
}

type ColorBy = 'hr' | 'speed' | 'alt'

function Detail({ a, s, units, tz, zones }: { a: ActivityDetail; s: Streams | null; units: Units; tz: string; zones: HrZones | null }) {
  const kind = speedKind(a.sport)
  const paceWord = kind === 'speed' ? 'Speed' : 'Pace'
  return (
    <>
      <section className="card p-5 sm:p-6">
        <p className="m-0 mb-4 font-mono text-[12px] text-muted">
          {a.sport_label} · {a.day} · {timeOfDay(a.start, tz)}–{timeOfDay(a.end, tz)}
        </p>
        <Kpis items={[
          { label: 'Distance', value: a.distance_m ? distance(a.distance_m, units) : '–', tone: 'lime' },
          { label: 'Time', value: clock(a.timer_s ?? a.elapsed_s), hint: a.elapsed_s && a.timer_s && a.elapsed_s - a.timer_s > 30 ? `${clock(a.elapsed_s)} with pauses` : undefined },
          { label: paceWord, value: speedText(a.avg_speed, a.sport, units), hint: a.max_speed && (!a.avg_speed || a.max_speed <= 3 * a.avg_speed) ? `best ${speedText(a.max_speed, a.sport, units)}` : undefined },
          { label: 'Heart rate', value: num(a.avg_hr), unit: 'bpm', hint: a.max_hr ? `max ${a.max_hr}` : undefined },
          { label: 'Climbed', value: elevation(a.ascent_m, units), hint: a.descent_m ? `down ${elevation(a.descent_m, units)}` : undefined },
          { label: 'Calories', value: num(a.kcal), unit: 'kcal' },
          ...(a.avg_cadence ? [{ label: 'Cadence', value: num(a.avg_cadence * cadenceFactor(a.sport)), unit: kind === 'pace' ? 'spm' : kind === 'swim' ? 'str/min' : 'rpm' }] : []),
          ...(a.avg_power ? [{ label: 'Power', value: num(a.avg_power), unit: 'W' }] : []),
          ...(a.training_effect != null ? [{ label: 'Training effect', value: a.training_effect.toFixed(1), hint: a.anaerobic_effect != null ? `anaerobic ${a.anaerobic_effect.toFixed(1)}` : undefined }] : []),
          ...(a.training_load != null ? [{ label: 'Training load', value: num(a.training_load) }] : []),
        ]} />
      </section>
      {s ? <Track a={a} s={s} units={units} zones={zones} /> : (
        <Card title="Track"><p className="m-0 text-[13px] text-muted">This activity has no recorded track.</p></Card>
      )}
      {a.laps.length > 1 && <Laps a={a} units={units} />}
      {s && <Splits a={a} s={s} units={units} />}
      {zones && (a.zones?.hr_s || s?.hr) && <Zones a={a} s={s} zones={zones} />}
    </>
  )
}

interface Panel {
  key: string
  name: string
  unit: string
  v: (number | null)[]
  role: Role
  kind: 'line' | 'area'
  inverse?: boolean
  format?: (v: number) => string
  zones?: number[]
  /** A fixed axis (pace: without GPS jumps and standing still). */
  range?: { min: number; max: number; interval: number } | null
}

/** Track is the route and the linked panels: one crosshair across all, mirrored on the route. */
function Track({ a, s, units, zones }: { a: ActivityDetail; s: Streams; units: Units; zones: HrZones | null }) {
  const tiles = !!useMe().data?.server.tiles
  const ctx = useMemo(() => chartCtx(''), [])
  const group = useId()
  const kind = speedKind(a.sport)
  const hasDist = !!s.dist?.some((d) => d != null && d > 0)
  const [xKind, setXKind] = useState<'time' | 'distance'>('time')
  const [colorBy, setColorBy] = useState<ColorBy>(s.hr ? 'hr' : 'speed')
  const [hover, setHover] = useState<number | null>(null)
  const charts = useRef(new Map<string, EChartsType>())
  const imperial = units === 'imperial'
  const x = useMemo(() => {
    if (xKind === 'distance' && s.dist) {
      let last = 0
      return s.dist.map((d) => (last = d ?? last))
    }
    return s.t.map((t) => t - s.t[0])
  }, [xKind, s])
  const points = useMemo(() => routePoints(s), [s])
  const speedSeries = useMemo(() => s.speed?.map((v) => speedValue(v, kind, units)), [s, kind, units])
  const panels = useMemo<Panel[]>(() => {
    const out: Panel[] = []
    const has = (v?: (number | null)[]) => !!v && v.some((x) => x != null && x !== 0)
    if (has(s.speed)) {
      out.push({
        key: 'speed', name: kind === 'speed' ? 'Speed' : 'Pace', role: 'extra', kind: 'line', v: speedSeries ?? [],
        unit: kind === 'pace' ? (imperial ? '/mi' : '/km') : kind === 'swim' ? (imperial ? '/100yd' : '/100m') : imperial ? 'mph' : 'km/h',
        inverse: kind !== 'speed', format: kind === 'speed' ? (v) => v.toFixed(1) : (v) => clock(v),
        range: (() => {
          const r = robustRange(speedSeries ?? [])
          return r && niceAxis({ min: Math.max(0, r.min), max: r.max }, kind === 'speed' ? speedSteps : paceSteps)
        })(),
      })
    }
    if (has(s.hr)) {
      out.push({ key: 'hr', name: 'Heart rate', unit: 'bpm', role: 'primary', kind: 'line', v: s.hr!, zones: zones ? zones.limits.slice(0, 5) : undefined })
    }
    if (has(s.alt)) {
      out.push({ key: 'alt', name: 'Elevation', unit: imperial ? 'ft' : 'm', role: 'context', kind: 'area',
        v: imperial ? s.alt!.map((v) => (v == null ? null : v * 3.28084)) : s.alt! })
    }
    if (has(s.cad)) {
      const f = cadenceFactor(a.sport)
      out.push({ key: 'cad', name: 'Cadence', unit: kind === 'pace' ? 'spm' : kind === 'swim' ? 'str/min' : 'rpm', role: 'context2', kind: 'line',
        v: f === 1 ? s.cad! : s.cad!.map((c) => (c == null ? null : c * f)) })
    }
    if (has(s.power)) out.push({ key: 'power', name: 'Power', unit: 'W', role: 'extra2', kind: 'line', v: s.power! })
    if (has(s.temp)) out.push({ key: 'temp', name: 'Temperature', unit: '°C', role: 'context2', kind: 'line', v: s.temp! })
    return out
  }, [s, kind, speedSeries, imperial, zones, a.sport])

  // The crosshair moved: find the point it points at, for the route's dot.
  // While the route is pointed at, the charts follow it; their own "pointer gone" updates
  // (the mouse is not over them) must not clear the point.
  const fromRoute = useRef(false)
  const onPointer = useMemo(() => ({
    updateAxisPointer: (e: unknown) => {
      const v = (e as { axesInfo?: { value: number }[] }).axesInfo?.[0]?.value
      if (v == null) {
        if (!fromRoute.current) setHover(null)
        return
      }
      if (!fromRoute.current) setHover(nearestIndex(x, v))
    },
  }), [x])
  // The route was hovered: move the charts' crosshair there.
  const onRouteHover = useCallback((i: number | null) => {
    fromRoute.current = i != null
    setHover(i)
    const first = charts.current.values().next().value
    if (!first) return
    if (i == null) first.dispatchAction({ type: 'hideTip' })
    else first.dispatchAction({ type: 'showTip', seriesIndex: 0, dataIndex: i })
  }, [])
  const chartRefs = useMemo(() => Object.fromEntries(panels.map((p) => [p.key, (c: EChartsType | null) => {
    if (c) charts.current.set(p.key, c)
    else charts.current.delete(p.key)
  }])), [panels])

  const colorValues = colorBy === 'hr' ? s.hr : colorBy === 'alt' ? s.alt : s.speed
  const hoverText = hover != null ? panels.map((p) => {
    const v = p.v[hover]
    return v == null ? null : `${p.name} ${p.format ? p.format(v) : Math.round(v)}${p.unit && !p.format ? ` ${p.unit}` : ''}`
  }).filter(Boolean).join(' · ') : ''

  return (
    <>
      {points.length > 1 && (
        <Card title="Route" right={
          <div role="group" aria-label="Colour the route by" className="inline-flex rounded-full border border-border p-0.5 text-[12px]">
            {([['hr', 'Heart rate', !!s.hr], ['speed', kind === 'speed' ? 'Speed' : 'Pace', !!s.speed], ['alt', 'Elevation', !!s.alt]] as const)
              .filter(([, , ok]) => ok).map(([k, label]) => (
                <button key={k} type="button" aria-pressed={colorBy === k} onClick={() => setColorBy(k)}
                  className={`rounded-full px-3 py-1 ${colorBy === k ? 'bg-lime-fill text-lime' : 'text-muted hover:text-text'}`}>{label}</button>
              ))}
          </div>
        }>
          <div className="overflow-hidden rounded-xl border border-border bg-bg2">
            {tiles ? (
              <ActivityMap points={points} values={colorValues ?? undefined} ramp={ramps[colorBy]} hover={hover} onHover={onRouteHover}
                label={`Map of ${a.name || a.sport_label}, the route coloured by ${colorBy === 'hr' ? 'heart rate' : colorBy === 'alt' ? 'elevation' : 'speed'}`} />
            ) : (
              <ActivityRoute points={points} values={colorValues ?? undefined} ramp={ramps[colorBy]} hover={hover} onHover={onRouteHover}
                label={`Route of ${a.name || a.sport_label}, coloured by ${colorBy === 'hr' ? 'heart rate' : colorBy === 'alt' ? 'elevation' : 'speed'}`} />
            )}
          </div>
          <div className="mt-3 flex flex-wrap items-center justify-between gap-3 text-[12px] text-muted">
            <span className="flex items-center gap-2" aria-hidden>
              low {ramps[colorBy].map((c) => <span key={c} className="h-2 w-6 rounded-full" style={{ background: c }} />)} high
            </span>
            <span className="font-mono" aria-live="polite">
              {hover != null ? `${xLabel(x[hover], xKind, imperial)} · ${hoverText}` : 'Point at the charts or the route.'}
            </span>
          </div>
          {!tiles && <p className="m-0 mt-2 text-[12px] text-faint">This server has no map tiles: the route is drawn on its own.</p>}
        </Card>
      )}
      {panels.length > 0 && (
        <Card title="Along the way" right={hasDist ? (
          <div role="group" aria-label="Horizontal axis" className="inline-flex rounded-full border border-border p-0.5 text-[12px]">
            {([['time', 'Time'], ['distance', 'Distance']] as const).map(([k, label]) => (
              <button key={k} type="button" aria-pressed={xKind === k} onClick={() => setXKind(k)}
                className={`rounded-full px-3 py-1 ${xKind === k ? 'bg-lime-fill text-lime' : 'text-muted hover:text-text'}`}>{label}</button>
            ))}
          </div>
        ) : undefined}>
          <div className="grid gap-1">
            {panels.map((p) => {
              const input = { x, xKind, imperial, v: p.v, name: p.name, unit: p.unit, kind: p.kind, role: p.role, inverse: p.inverse, format: p.format, zones: p.zones, min: p.range?.min, max: p.range?.max, interval: p.range?.interval }
              return (
                <div key={p.key}>
                  <p className="m-0 font-mono text-[10px] uppercase tracking-[.14em] text-faint">{p.name}{p.unit ? ` · ${p.unit}` : ''}</p>
                  <Chart option={trackPanel(input, ctx)} ariaLabel={describeTrackPanel(input)} height={130} group={group}
                    onEvents={onPointer} onChart={chartRefs[p.key]} />
                </div>
              )
            })}
          </div>
        </Card>
      )}
    </>
  )
}

function Laps({ a, units }: { a: ActivityDetail; units: Units }) {
  return (
    <Card title={`Laps (${a.laps.length})`}>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[620px] border-collapse text-left text-[13px]">
          <thead>
            <tr className="font-mono text-[10px] uppercase tracking-[.12em] text-faint">
              {['Lap', 'Time', 'Distance', speedKind(a.sport) === 'speed' ? 'Speed' : 'Pace', 'Avg HR', 'Max HR', 'Climbed'].map((h) => <th key={h} className="px-2 py-2 font-normal">{h}</th>)}
            </tr>
          </thead>
          <tbody>
            {a.laps.map((l) => (
              <tr key={l.index} className="border-t border-border">
                <td className="px-2 py-1.5 text-muted">{l.index + 1}</td>
                <td className="num px-2 py-1.5">{clock(l.timer_s ?? l.elapsed_s)}</td>
                <td className="num px-2 py-1.5">{l.distance_m ? distance(l.distance_m, units) : '–'}</td>
                <td className="num px-2 py-1.5">{speedText(l.avg_speed, a.sport, units)}</td>
                <td className="num px-2 py-1.5">{num(l.avg_hr)}</td>
                <td className="num px-2 py-1.5">{num(l.max_hr)}</td>
                <td className="num px-2 py-1.5">{elevation(l.ascent_m, units)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  )
}

function Splits({ a, s, units }: { a: ActivityDetail; s: Streams; units: Units }) {
  const list = useMemo(() => splits(s, units), [s, units])
  if (list.length < 2) return null
  // The best split is a whole one: a short last split (or a GPS jump in it) does not count.
  const whole = list.filter((x) => x.distance >= (units === 'imperial' ? 1609.344 : 1000) - 1)
  const best = Math.max(...(whole.length ? whole : list).map((x) => x.speed))
  // Bars between a bit below the slowest whole split and the best, so a few seconds show.
  const slow = Math.min(...(whole.length ? whole : list).map((x) => x.speed)) * 0.97
  const width = (v: number) => `${Math.max(4, Math.min(100, 12 + (88 * (v - slow)) / Math.max(1e-6, best - slow)))}%`
  return (
    <Card title={`Splits per ${units === 'imperial' ? 'mile' : 'kilometre'}`}>
      <ul className="m-0 grid list-none gap-1.5 p-0">
        {list.map((x) => (
          <li key={x.n} className="grid grid-cols-[1.25rem_5.25rem_minmax(0,1fr)_2.25rem_3.5rem] items-center gap-2 text-[13px] sm:grid-cols-[2.5rem_6rem_minmax(0,1fr)_4rem_4rem] sm:gap-3">
            <span className="font-mono text-faint">{x.n}</span>
            <span className="num">{speedText(x.speed, a.sport, units)}</span>
            <span className="h-2 overflow-hidden rounded-full bg-track" aria-hidden>
              <span className={`block h-full rounded-full ${x.speed === best ? 'bg-lime' : 'bg-grey2'}`} style={{ width: width(x.speed) }} />
            </span>
            <span className="num text-right text-muted">{x.hr != null ? Math.round(x.hr) : '–'}</span>
            <span className="num text-right text-muted">{x.distance < (units === 'imperial' ? 1609 : 1000) - 1 ? distance(x.distance, units) : `+${Math.round(x.ascent)} m`}</span>
          </li>
        ))}
      </ul>
      <p className="m-0 mt-3 text-[12px] text-faint">Columns: split, {speedKind(a.sport) === 'speed' ? 'speed' : 'pace'}, average heart rate, climb (or the last split's length).</p>
    </Card>
  )
}

const zoneColors = ['var(--grey2)', 'var(--grey)', 'var(--lime)', 'var(--amber)', 'var(--red)']

function Zones({ a, s, zones }: { a: ActivityDetail; s: Streams | null; zones: HrZones }) {
  // The watch's own seconds when it counted them with these limits; else counted from the track.
  const z = useMemo(() => (zones.source === 'activity' && a.zones && secondsIn(a.zones)) || (s ? timeInLimits(s.t, s.hr, zones.limits) : [0, 0, 0, 0, 0]),
    [a.zones, s, zones])
  const total = z.reduce((x, y) => x + y, 0)
  if (!total) return null
  return (
    <Card title="Time in heart-rate zones" hint={`Zones from ${zones.label}.`}>
      <ul className="m-0 grid list-none gap-2 p-0">
        {z.map((sec, i) => (
          <li key={i} className="grid grid-cols-[6.5rem_minmax(0,1fr)_4.5rem_3rem] items-center gap-3 text-[13px]">
            <span className="text-muted">Zone {i + 1} <span className="font-mono text-[11px] text-faint">{zones.limits[i]}–{zones.limits[i + 1]}</span></span>
            <span className="h-2.5 overflow-hidden rounded-full bg-track" aria-hidden>
              <span className="block h-full rounded-full" style={{ width: `${(sec / total) * 100}%`, background: zoneColors[i] }} />
            </span>
            <span className="num text-right">{hm(sec)}</span>
            <span className="num text-right text-faint">{Math.round((sec / total) * 100)} %</span>
          </li>
        ))}
      </ul>
    </Card>
  )
}
