// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMe } from '../../api/hooks/auth'
import { usePrefs } from '../../api/hooks/prefs'
import { useDailyRange, useDaySeries } from '../../api/hooks/data'
import { dailyBars, describeSeries, Ring } from '../../charts'
import { calendarHeatmap, describeCalendarHeatmap } from '../../charts/builders/calendarHeatmap'
import { distance, elevation, int } from '../../lib/format'
import { hourlyTotals, intensityWeek } from '../../lib/health'
import { periodRange } from '../../lib/period'
import { Meter } from '../../glance/cards/parts'
import { byDay, ChartCard, dayClick, Empty, Kpis, Loaded, num, rangeDays, useHealth } from './parts'

export default function MovementPage() {
  const { p } = useHealth()
  return p.period === 'day' ? <MovementDay /> : p.period === 'year' ? <MovementYear /> : <MovementRange />
}


function MovementDay() {
  const intensityGoal = usePrefs().intensityGoal
  const { p, ctx } = useHealth()
  const units = useMe().data?.units ?? 'metric'
  const steps = useDaySeries('steps', p.day, p.day, 288)
  const week = periodRange(p.day, 'week')
  const daily = useDailyRange(week.from, week.to)
  return (
    <Loaded q={[steps, daily]}>
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
        return (
          <div className="grid gap-5">
            <section className="card grid gap-6 p-5 sm:p-6 lg:grid-cols-[auto_minmax(0,1fr)]">
              <div className="flex items-center gap-5">
                <Ring value={total} max={goal || total || 1} size={140} stroke={10} label={`${int(total)} steps${goal ? ` of ${int(goal)}` : ''}`}>
                  <span className="num text-[22px] leading-none">{int(total)}</span>
                  <span className="caps mt-1 !text-[10px]">{goal ? `of ${int(goal)}` : 'steps'}</span>
                </Ring>
              </div>
              <div className="grid content-start gap-5">
                <Kpis items={[
                  { label: 'Distance', value: distance(d?.distance_m, units) },
                  { label: 'Climbed', value: elevation(d?.ascent_m, units) },
                  { label: 'Active kcal', value: num(d?.active_kcal) },
                  { label: 'Intensity minutes', value: `${minutes}`, hint: `this week, of ${intensityGoal}`, tone: minutes >= intensityGoal ? 'lime' : undefined },
                ]} />
                <Meter value={minutes} max={intensityGoal} label={`Intensity minutes this week: ${minutes} of ${intensityGoal}`} />
              </div>
            </section>
            <ChartCard title="Steps by hour" ariaLabel={describeSeries('Steps per hour', hours, 'steps')} height={220}
              option={dailyBars({ labels, v: hours, focus: current, axis: true, yAxis: true, unit: 'steps' }, ctx)}
              table={{ head: ['Hour', 'Steps'], rows: hours.map((v, i) => [`${labels[i]}:00`, int(v)]).filter((r) => r[1] !== '0') }} />
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
        const goals = days.map((d) => m.get(d)?.values.step_goal).filter((x): x is number => x != null)
        const goal = goals.length ? goals[goals.length - 1] : undefined
        const got = v.filter((x): x is number => x != null)
        const dist = days.reduce((a, d) => a + (m.get(d)?.distance_m ?? 0), 0)
        const reached = goal ? got.filter((x) => x >= goal).length : null
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Average', value: int(Math.round(got.reduce((a, b) => a + b, 0) / got.length)), unit: 'steps', tone: 'lime' },
                { label: 'Total', value: int(got.reduce((a, b) => a + b, 0)), unit: 'steps' },
                { label: 'Distance', value: distance(dist, units) },
                { label: 'Goal reached', value: reached == null ? '–' : `${reached}`, hint: reached == null ? undefined : `of ${got.length} days` },
              ]} />
            </section>
            <ChartCard title="Steps by day" ariaLabel={describeSeries('Daily steps', v, 'steps')} height={240}
              option={dailyBars({ labels, v, focus: days.indexOf(p.day), goal, goalLabel: goal ? `goal ${int(goal)}` : undefined, axis: true, yAxis: true, unit: 'steps' }, ctx)}
              onEvents={dayClick(days, p.open)}
              table={{ head: ['Day', 'Steps'], rows: days.map((d, i) => [d, v[i] == null ? '–' : int(v[i])]).filter((r) => r[1] !== '–') }} />
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
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Average', value: int(Math.round(total / got.length)), unit: 'steps', tone: 'lime' },
                { label: 'Total', value: int(total), unit: 'steps' },
                { label: 'Days', value: `${got.length}` },
              ]} />
            </section>
            <ChartCard title="Steps by day" hint="Brighter is more steps. Choose a day to see it."
              ariaLabel={describeCalendarHeatmap(input)} option={calendarHeatmap(input, ctx)} height={190} onEvents={dayClick([], p.open)}
              table={{ head: ['Day', 'Steps'], rows: got.map((d) => [d.day, int(d.steps)]) }} />
          </div>
        )
      }}
    </Loaded>
  )
}
