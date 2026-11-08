// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useActivitiesOn, useDailyRange, useDaySeries } from '../../api/hooks/data'
import { describeRangeBars, describeSeries, rangeBars, timeSeries } from '../../charts'
import { timeOfDay } from '../../lib/format'
import { rolling } from '../../lib/health'
import { EcgCard } from './extras'
import { byDay, ChartCard, dayClick, Empty, Kpis, Loaded, num, rangeDays, useHealth, withLead } from './parts'

export default function HeartPage() {
  const { p, ctx } = useHealth()
  return (
    <div className="grid gap-5">
      {p.period === 'day' ? <HeartDay /> : <HeartRange />}
      <EcgCard tz={p.tz} ctx={ctx} />
    </div>
  )
}

function HeartDay() {
  const { p, ctx } = useHealth()
  const hr = useDaySeries('heart_rate', p.day, p.day, 288)
  const daily = useDailyRange(p.day, p.day)
  const acts = useActivitiesOn(p.day, hr.data?.to)
  return (
    <Loaded q={[hr, daily]}>
      {() => {
        const s = hr.data!
        const d = byDay(daily.data).get(p.day)
        if (!s.t.length) return <Empty p={p} what="heart rate recorded" />
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Resting', value: num(d?.resting_hr ?? d?.values.resting_hr), unit: 'bpm', tone: 'lime' },
                { label: 'Lowest', value: num(d?.hr_min), unit: 'bpm' },
                { label: 'Highest', value: num(d?.hr_max), unit: 'bpm' },
                { label: 'Average', value: num(d?.hr_avg), unit: 'bpm' },
                { label: 'Activities', value: `${acts.activities.length}` },
              ]} />
            </section>
            <ChartCard title="Through the day" hint={acts.activities.length ? 'Shaded: your activities. The band is each 5 minutes’ lowest to highest.' : 'The band is each 5 minutes’ lowest to highest.'}
              ariaLabel={describeSeries('Heart rate', s.v, 'bpm')} height={280}
              option={timeSeries({
                from: s.from, to: s.to, zoom: true,
                series: [{ id: 'hr', name: 'Heart rate', t: s.t, v: s.v, min: s.min, max: s.max, kind: 'line', role: 'primary', unit: 'bpm', gapS: 900 }],
                markLines: d?.resting_hr != null ? [{ value: d.resting_hr, label: `resting ${d.resting_hr}` }] : undefined,
                markAreas: acts.activities.map((a) => ({ from: a.start, to: a.end, role: 'extra' as const, label: a.sport_label })),
              }, ctx)}
              table={{ head: ['Time', 'Average', 'Low', 'High'], rows: s.t.map((t, i) => [timeOfDay(t, p.tz), s.v[i].toFixed(0), s.min?.[i]?.toFixed(0) ?? '', s.max?.[i]?.toFixed(0) ?? '']) }} />
          </div>
        )
      }}
    </Loaded>
  )
}

function HeartRange() {
  const { p, ctx } = useHealth()
  const lead = withLead(p.range, 6)
  const daily = useDailyRange(lead.from, lead.to)
  const { days, labels } = rangeDays(p.range, p.period)
  const allDays = rangeDays(lead, p.period).days
  return (
    <Loaded q={[daily]}>
      {() => {
        const m = byDay(daily.data)
        const rest = allDays.map((d) => m.get(d)?.resting_hr ?? m.get(d)?.values.resting_hr ?? null)
        const avg = rolling(rest, 7).slice(allDays.length - days.length).map((v, i) => (days[i] > p.today ? null : v))
        const list = days.map((d) => m.get(d))
        if (!list.some((d) => d?.hr_max != null || d?.resting_hr != null)) return <Empty p={p} what="heart rate recorded" />
        const restIn = rest.slice(allDays.length - days.length)
        const input = { labels, lo: list.map((d) => d?.hr_min ?? null), hi: list.map((d) => d?.hr_max ?? null), name: 'Heart rate', unit: 'bpm',
          line: { name: 'Resting', v: restIn }, avg: { name: 'Resting, 7-day average', v: avg }, focus: days.indexOf(p.day) }
        const r = restIn.filter((x): x is number => x != null)
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Average resting', value: r.length ? Math.round(r.reduce((a, b) => a + b, 0) / r.length) : '–', unit: 'bpm', tone: 'lime' },
                { label: 'Lowest resting', value: r.length ? Math.min(...r) : '–', unit: 'bpm' },
                { label: 'Highest', value: num(list.reduce<number | null>((a, d) => (d?.hr_max != null && (a == null || d.hr_max > a) ? d.hr_max : a), null)), unit: 'bpm' },
              ]} />
            </section>
            <ChartCard title="Daily range" hint="Bars run from each day’s lowest to highest heart rate; the line is your resting heart rate."
              ariaLabel={describeRangeBars(input)} option={rangeBars(input, ctx)} height={260} onEvents={dayClick(days, p.open)}
              table={{ head: ['Day', 'Lowest', 'Highest', 'Resting'], rows: days.map((d, i) => [d, num(list[i]?.hr_min), num(list[i]?.hr_max), num(restIn[i])]).filter((x) => x.slice(1).some((v) => v !== '–')) }} />
          </div>
        )
      }}
    </Loaded>
  )
}
