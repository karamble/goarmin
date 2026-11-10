// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMe } from '../../api/hooks/auth'
import type { Day } from '../../api/types'
import { usePrefs } from '../../api/hooks/prefs'
import { useDailyRange, useDaySeries } from '../../api/hooks/data'
import { dailyBars, describeSeries, Ring } from '../../charts'
import { calendarHeatmap, describeCalendarHeatmap } from '../../charts/builders/calendarHeatmap'
import { distance, elevation, int } from '../../lib/format'
import { hourlyFloors, hourlyTotals, intensityWeek } from '../../lib/health'
import { periodRange } from '../../lib/period'
import { Meter } from '../../glance/cards/parts'
import { byDay, ChartCard, dayClick, Empty, Kpis, Loaded, num, rangeDays, useHealth } from './parts'

/** reachedOn counts the days whose value met that day's own goal (the watch's goals change). */
function reachedOn(days: string[], m: Map<string, Day>, value: (d: Day) => number | null | undefined, goal: (d: Day) => number | null | undefined) {
  let n = 0, of = 0
  for (const day of days) {
    const d = m.get(day)
    const v = d ? value(d) : null
    const g = d ? goal(d) : null
    if (v == null || !g) continue
    of++
    if (v >= g) n++
  }
  return of ? { n, of } : null
}

const lastOf = (xs: (number | null | undefined)[]) => xs.filter((x): x is number => x != null && x > 0).at(-1)

export default function MovementPage() {
  const { p } = useHealth()
  return p.period === 'day' ? <MovementDay /> : p.period === 'year' ? <MovementYear /> : <MovementRange />
}


function MovementDay() {
  const intensityGoal = usePrefs().intensityGoal
  const { p, ctx } = useHealth()
  const units = useMe().data?.units ?? 'metric'
  const steps = useDaySeries('steps', p.day, p.day, 288)
  const ascent = useDaySeries('ascent', p.day, p.day, 288)
  const week = periodRange(p.day, 'week')
  const daily = useDailyRange(week.from, week.to)
  return (
    <Loaded q={[steps, ascent, daily]}>
      {() => {
        const s = steps.data!
        const d = byDay(daily.data).get(p.day)
        if (!s.t.length && d?.steps == null) return <Empty p={p} what="steps recorded" />
        const hours = hourlyTotals(s)
        const now = Math.floor(Date.now() / 1000)
        const current = p.isToday ? Math.floor((now - s.from) / 3600) : undefined
        const goal = d?.values.step_goal
        const total = d?.steps ?? hours.reduce((a, b) => a + b, 0)
        const minutes = intensityWeek(daily.data ?? [], p.day)
        const labels = hours.map((_, i) => String(i).padStart(2, '0'))
        const floors = d?.floors_up ?? null
        const floorsGoal = d?.floors_goal ?? null
        const floorHours = ascent.data ? hourlyFloors(ascent.data) : []
        return (
          <div className="grid gap-5">
            <section className="card grid gap-6 p-5 sm:p-6 lg:grid-cols-[auto_minmax(0,1fr)]">
              <div className="flex flex-wrap items-end gap-6">
                <figure className="m-0 grid justify-items-center gap-2">
                  <Ring value={total} max={goal || total || 1} size={140} stroke={10} label={`${int(total)} steps${goal ? ` of ${int(goal)}` : ''}`}>
                    <span className="num text-[22px] leading-none">{int(total)}</span>
                    <span className="caps mt-1 !text-[10px]">{goal ? `of ${int(goal)}` : 'steps'}</span>
                  </Ring>
                  <figcaption className="caps !text-[10px] !text-faint">Steps</figcaption>
                </figure>
                {floors != null && (
                  <figure className="m-0 grid justify-items-center gap-2">
                    <Ring value={floors} max={floorsGoal || floors || 1} size={112} stroke={9} color="var(--lime-dim)"
                      label={`${int(floors)} floor${floors === 1 ? '' : 's'} climbed${floorsGoal ? ` of ${int(floorsGoal)}` : ''}`}>
                      <span className="num text-[20px] leading-none">{int(floors)}</span>
                      <span className="caps mt-1 !text-[10px]">{floorsGoal ? `of ${int(floorsGoal)}` : 'floors'}</span>
                    </Ring>
                    <figcaption className="caps !text-[10px] !text-faint">Floors</figcaption>
                  </figure>
                )}
              </div>
              <div className="grid content-start gap-5">
                <Kpis items={[
                  { label: 'Distance', value: distance(d?.distance_m, units) },
                  { label: 'Climbed', value: elevation(d?.ascent_m, units), hint: d?.floors_down != null ? `${int(d.floors_down)} floor${d.floors_down === 1 ? '' : 's'} down` : undefined },
                  { label: 'Active kcal', value: num(d?.active_kcal) },
                  { label: 'Intensity minutes', value: `${minutes}`, hint: `this week, of ${intensityGoal}`, tone: minutes >= intensityGoal ? 'lime' : undefined },
                ]} cols="sm:grid-cols-4" />
                <Meter value={minutes} max={intensityGoal} label={`Intensity minutes this week: ${minutes} of ${intensityGoal}`} />
              </div>
            </section>
            <ChartCard title="Steps by hour" ariaLabel={describeSeries('Steps per hour', hours, 'steps')} height={220}
              option={dailyBars({ labels, v: hours, focus: current, axis: true, yAxis: true, unit: 'steps' }, ctx)}
              table={{ head: ['Hour', 'Steps'], rows: hours.map((v, i) => [`${labels[i]}:00`, int(v)]).filter((r) => r[1] !== '0') }} />
            {floors != null && (
              <ChartCard title="Floors by hour" hint="A floor is 3 m (10 ft) climbed, counted as the watch counts it."
                ariaLabel={describeSeries('Floors climbed per hour', floorHours, 'floors')} height={180}
                option={dailyBars({ labels: floorHours.map((_, i) => String(i).padStart(2, '0')), v: floorHours, focus: current, axis: true, yAxis: true, unit: 'floors', whole: true }, ctx)}
                table={{ head: ['Hour', 'Floors'], rows: floorHours.map((v, i) => [`${String(i).padStart(2, '0')}:00`, int(v)]).filter((r) => r[1] !== '0') }} />
            )}
          </div>
        )
      }}
    </Loaded>
  )
}

function MovementRange() {
  const { p, ctx } = useHealth()
  const units = useMe().data?.units ?? 'metric'
  const daily = useDailyRange(p.range.from, p.range.to)
  const { days, labels } = rangeDays(p.range, p.period)
  return (
    <Loaded q={[daily]}>
      {() => {
        const m = byDay(daily.data)
        const v = days.map((d) => m.get(d)?.steps ?? null)
        if (!v.some((x) => x != null)) return <Empty p={p} what="steps recorded" />
        const goal = lastOf(days.map((d) => m.get(d)?.values.step_goal))
        const got = v.filter((x): x is number => x != null)
        const dist = days.reduce((a, d) => a + (m.get(d)?.distance_m ?? 0), 0)
        const reached = reachedOn(days, m, (d) => d.steps, (d) => d.values.step_goal)
        const fv = days.map((d) => m.get(d)?.floors_up ?? null)
        const fgot = fv.filter((x): x is number => x != null)
        const fgoal = lastOf(days.map((d) => m.get(d)?.floors_goal))
        const freached = reachedOn(days, m, (d) => d.floors_up, (d) => d.floors_goal)
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Average', value: int(Math.round(got.reduce((a, b) => a + b, 0) / got.length)), unit: 'steps', tone: 'lime' },
                { label: 'Total', value: int(got.reduce((a, b) => a + b, 0)), unit: 'steps' },
                { label: 'Distance', value: distance(dist, units) },
                { label: 'Goal reached', value: reached == null ? '–' : `${reached.n}`, hint: reached == null ? undefined : `of ${reached.of} days` },
                ...(fgot.length ? [
                  { label: 'Floors', value: int(Math.round(fgot.reduce((a, b) => a + b, 0) / fgot.length)), unit: 'a day', hint: `${int(fgot.reduce((a, b) => a + b, 0))} in all` },
                  { label: 'Floors goal reached', value: freached == null ? '–' : `${freached.n}`, hint: freached == null ? undefined : `of ${freached.of} days` },
                ] : []),
              ]} />
            </section>
            <ChartCard title="Steps by day" ariaLabel={describeSeries('Daily steps', v, 'steps')} height={240}
              option={dailyBars({ labels, v, focus: days.indexOf(p.day), goal, goalLabel: goal ? `goal ${int(goal)}` : undefined, axis: true, yAxis: true, unit: 'steps' }, ctx)}
              onEvents={dayClick(days, p.open)}
              table={{ head: ['Day', 'Steps'], rows: days.map((d, i) => [d, v[i] == null ? '–' : int(v[i])]).filter((r) => r[1] !== '–') }} />
            {fgot.length > 0 && (
              <ChartCard title="Floors by day" ariaLabel={describeSeries('Daily floors climbed', fv, 'floors')} height={200}
                option={dailyBars({ labels, v: fv, focus: days.indexOf(p.day), goal: fgoal, goalLabel: fgoal ? `goal ${int(fgoal)}` : undefined, axis: true, yAxis: true, unit: 'floors', whole: true }, ctx)}
                onEvents={dayClick(days, p.open)}
                table={{ head: ['Day', 'Floors', 'Goal'], rows: days.map((d, i) => [d, fv[i] == null ? '–' : int(fv[i]), m.get(d)?.floors_goal ? int(m.get(d)!.floors_goal) : '–']).filter((r) => r[1] !== '–') }} />
            )}
          </div>
        )
      }}
    </Loaded>
  )
}

function MovementYear() {
  const { p, ctx } = useHealth()
  const daily = useDailyRange(p.range.from, p.range.to)
  return (
    <Loaded q={[daily]}>
      {() => {
        const got = (daily.data ?? []).filter((d) => d.steps != null)
        if (!got.length) return <Empty p={p} what="steps recorded" />
        const input = { from: p.range.from, to: p.range.to, data: got.map((d) => [d.day, d.steps ?? null] as [string, number | null]), name: 'Steps', unit: 'steps' }
        const total = got.reduce((a, d) => a + d.steps!, 0)
        const fdays = (daily.data ?? []).filter((d) => d.floors_up != null)
        const ftotal = fdays.reduce((a, d) => a + d.floors_up!, 0)
        const finput = { from: p.range.from, to: p.range.to, data: fdays.map((d) => [d.day, d.floors_up ?? null] as [string, number | null]), name: 'Floors', unit: 'floors' }
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Average', value: int(Math.round(total / got.length)), unit: 'steps', tone: 'lime' },
                { label: 'Total', value: int(total), unit: 'steps' },
                { label: 'Days', value: `${got.length}` },
                ...(fdays.length ? [{ label: 'Floors', value: int(Math.round(ftotal / fdays.length)), unit: 'a day', hint: `${int(ftotal)} in all` }] : []),
              ]} />
            </section>
            <ChartCard title="Steps by day" hint="Brighter is more steps. Choose a day to see it."
              ariaLabel={describeCalendarHeatmap(input)} option={calendarHeatmap(input, ctx)} height={190} onEvents={dayClick([], p.open)}
              table={{ head: ['Day', 'Steps'], rows: got.map((d) => [d.day, int(d.steps)]) }} />
            {fdays.length > 0 && (
              <ChartCard title="Floors by day" hint="Brighter is more floors climbed. Choose a day to see it."
                ariaLabel={describeCalendarHeatmap(finput)} option={calendarHeatmap(finput, ctx)} height={190} onEvents={dayClick([], p.open)}
                table={{ head: ['Day', 'Floors'], rows: fdays.map((d) => [d.day, int(d.floors_up)]) }} />
            )}
          </div>
        )
      }}
    </Loaded>
  )
}
