// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useDailyRange, useDaySeries, useNights } from '../../api/hooks/data'
import { describeRangeBars, describeSeries, rangeBars, timeSeries } from '../../charts'
import { calendarHeatmap, describeCalendarHeatmap } from '../../charts/builders/calendarHeatmap'
import { hm, timeOfDay } from '../../lib/format'
import { chargeDrain, stressLevels, stressTime } from '../../lib/health'
import { addDays } from '../../lib/period'
import { byDay, Card, ChartCard, dayClick, Empty, Kpis, Loaded, num, rangeDays, useHealth } from './parts'

export default function EnergyPage() {
  const { p } = useHealth()
  return p.period === 'day' ? <EnergyDay /> : p.period === 'year' ? <EnergyYear /> : <EnergyRange />
}

function EnergyDay() {
  const { p, ctx } = useHealth()
  const bb = useDaySeries('body_battery', p.day, p.day, 288)
  const stress = useDaySeries('stress', p.day, p.day, 288)
  const daily = useDailyRange(p.day, p.day)
  const nights = useNights(p.day, addDays(p.day, 1))
  return (
    <Loaded q={[bb, stress, daily]}>
      {() => {
        const b = bb.data!
        const s = stress.data!
        if (!b.t.length && !s.t.length) return <Empty p={p} what="Body Battery or stress recorded" />
        const d = byDay(daily.data).get(p.day)
        const { charged, drained } = chargeDrain(b.v)
        const st = stressTime(s.t, s.v, s.to, Math.max(300, s.bucket_s ?? 0))
        const total = st.rest + st.low + st.medium + st.high
        const sleep = (nights.data ?? []).filter((n) => n.end > b.from && n.start < b.to)
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Body Battery high', value: num(d?.body_battery_max), tone: 'lime' },
                { label: 'Low', value: num(d?.body_battery_min) },
                { label: 'Charged', value: `+${charged}` },
                { label: 'Drained', value: `−${drained}` },
                { label: 'Average stress', value: num(d?.stress_avg) },
                { label: 'Highest stress', value: num(d?.stress_max), tone: d?.stress_max != null && d.stress_max > 75 ? 'amber' : undefined },
              ]} />
            </section>
            <ChartCard title="Body Battery and stress" hint={sleep.length ? 'The shaded part is your sleep.' : undefined}
              ariaLabel={`${describeSeries('Body Battery', b.v, '%')}. ${describeSeries('Stress', s.v)}`} height={280}
              option={timeSeries({
                from: b.from || s.from, to: b.to || s.to, zoom: true,
                y: [{ min: 0, max: 100 }, { min: 0, max: 100 }],
                series: [
                  { id: 'bb', name: 'Body Battery', t: b.t, v: b.v, kind: 'area', role: 'primary', unit: '%', gapS: 900 },
                  { id: 'stress', name: 'Stress', t: s.t, v: s.v.map((x) => (x < 0 ? null : x)), kind: 'bar', role: 'warn', yAxis: 1, gapS: 900 },
                ],
                markAreas: sleep.map((n) => ({ from: n.start, to: n.end, role: 'extra' as const, label: 'sleep' })),
              }, ctx)}
              table={{ head: ['Time', 'Body Battery', 'Stress'], rows: b.t.map((t, i) => [timeOfDay(t, p.tz), b.v[i].toFixed(0), s.v[s.t.indexOf(t)]?.toFixed(0) ?? '']) }} />
            {total > 0 && (
              <Card title="Stress levels" hint="How long your stress was at each level, while the watch measured it.">
                <div className="flex h-3 w-full gap-[2px] overflow-hidden rounded-full" role="img"
                  aria-label={stressLevels.map((l) => `${l.label} ${hm(st[l.key])}`).join(', ')}>
                  {stressLevels.map((l) => st[l.key] > 0 && <span key={l.key} style={{ flexGrow: st[l.key], background: l.color }} />)}
                </div>
                <div className="mt-3 flex flex-wrap gap-x-6 gap-y-1">
                  {stressLevels.map((l) => (
                    <span key={l.key} className="inline-flex items-center gap-1.5 text-[12px] text-muted">
                      <span aria-hidden className="h-2 w-2 rounded-[2px]" style={{ background: l.color }} />
                      {l.label} <span className="num text-text">{hm(st[l.key])}</span>
                      <span className="text-faint">{Math.round((st[l.key] / total) * 100)} %</span>
                    </span>
                  ))}
                </div>
              </Card>
            )}
          </div>
        )
      }}
    </Loaded>
  )
}

function EnergyRange() {
  const { p, ctx } = useHealth()
  const daily = useDailyRange(p.range.from, p.range.to)
  const { days, labels } = rangeDays(p.range, p.period)
  return (
    <Loaded q={[daily]}>
      {() => {
        const m = byDay(daily.data)
        const list = days.map((d) => m.get(d))
        if (!list.some((d) => d?.body_battery_max != null || d?.stress_avg != null)) return <Empty p={p} what="Body Battery or stress recorded" />
        const input = { labels, lo: list.map((d) => d?.body_battery_min ?? null), hi: list.map((d) => d?.body_battery_max ?? null), name: 'Body Battery',
          min: 0, max: 100, line: { name: 'Average stress', v: list.map((d) => d?.stress_avg ?? null) }, focus: days.indexOf(p.day) }
        const vals = (k: 'body_battery_max' | 'body_battery_min' | 'stress_avg') => list.map((d) => d?.[k]).filter((x): x is number => x != null)
        const avg = (v: number[]) => (v.length ? Math.round(v.reduce((a, b) => a + b, 0) / v.length) : null)
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Average high', value: num(avg(vals('body_battery_max'))) },
                { label: 'Average low', value: num(avg(vals('body_battery_min'))) },
                { label: 'Average stress', value: num(avg(vals('stress_avg'))), tone: 'lime' },
              ]} />
            </section>
            <ChartCard title="Body Battery and stress by day" hint="Bars run from each day’s lowest to highest Body Battery; the line is the day’s average stress."
              ariaLabel={describeRangeBars(input)} option={rangeBars(input, ctx)} height={260} onEvents={dayClick(days, p.open)}
              table={{ head: ['Day', 'Body Battery', 'Average stress'], rows: days.map((d, i) => [d, list[i]?.body_battery_min != null ? `${list[i]!.body_battery_min}–${list[i]!.body_battery_max}` : '–', num(list[i]?.stress_avg)]).filter((r) => r[1] !== '–' || r[2] !== '–') }} />
          </div>
        )
      }}
    </Loaded>
  )
}

function EnergyYear() {
  const { p, ctx } = useHealth()
  const daily = useDailyRange(p.range.from, p.range.to)
  return (
    <Loaded q={[daily]}>
      {() => {
        const got = (daily.data ?? []).filter((d) => d.stress_avg != null)
        if (!got.length) return <Empty p={p} what="stress recorded" />
        const input = { from: p.range.from, to: p.range.to, data: got.map((d) => [d.day, d.stress_avg ?? null] as [string, number | null]), name: 'Average stress', worse: true }
        const avg = Math.round(got.reduce((a, d) => a + d.stress_avg!, 0) / got.length)
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[{ label: 'Average stress', value: `${avg}`, tone: 'lime' }, { label: 'Days', value: `${got.length}` }]} />
            </section>
            <ChartCard title="Stress by day" hint="Brighter amber is a more stressful day. Choose a day to see it."
              ariaLabel={describeCalendarHeatmap(input)} option={calendarHeatmap(input, ctx)} height={190} onEvents={dayClick([], p.open)}
              table={{ head: ['Day', 'Average stress'], rows: got.map((d) => [d.day, num(d.stress_avg)]) }} />
          </div>
        )
      }}
    </Loaded>
  )
}
