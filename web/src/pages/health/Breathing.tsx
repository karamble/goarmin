// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useDailyRange, useDaySeries } from '../../api/hooks/data'
import { dailyBars, describeSeries, stats, timeSeries } from '../../charts'
import { timeOfDay } from '../../lib/format'
import { byDay, ChartCard, dayClick, Empty, Kpis, Loaded, num, rangeDays, useHealth } from './parts'

export default function BreathingPage() {
  const { p } = useHealth()
  return p.period === 'day' ? <BreathingDay /> : <BreathingRange />
}

function BreathingDay() {
  const { p, ctx } = useHealth()
  const resp = useDaySeries('respiration', p.day, p.day, 288)
  const spo2 = useDaySeries('spo2', p.day, p.day, 288)
  const daily = useDailyRange(p.day, p.day)
  return (
    <Loaded q={[resp, spo2, daily]}>
      {() => {
        const r = resp.data!
        const o = spo2.data!
        if (!r.t.length && !o.t.length) return <Empty p={p} what="breathing recorded" />
        const d = byDay(daily.data).get(p.day)
        const os = stats(o.min?.length ? o.min : o.v)
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Respiration', value: num(d?.respiration_avg, 1), unit: 'brpm', tone: 'lime', hint: 'average' },
                { label: 'SpO2', value: num(d?.spo2_avg), unit: '%', hint: 'average' },
                { label: 'Lowest SpO2', value: os ? Math.round(os.min) : '–', unit: '%', tone: os && os.min < 90 ? 'amber' : undefined },
              ]} />
            </section>
            {r.t.length > 0 && (
              <ChartCard title="Respiration" hint="Breaths per minute; the band is each 5 minutes’ lowest to highest."
                ariaLabel={describeSeries('Respiration', r.v, 'breaths per minute', 1)} height={240}
                option={timeSeries({ from: r.from, to: r.to, digits: 1, series: [{ id: 'resp', name: 'Respiration', t: r.t, v: r.v, min: r.min, max: r.max, kind: 'line', role: 'primary', unit: 'brpm', gapS: 900 }] }, ctx)}
                table={{ head: ['Time', 'Breaths/min'], rows: r.t.map((t, i) => [timeOfDay(t, p.tz), r.v[i].toFixed(1)]) }} />
            )}
            {o.t.length > 0 && (
              <ChartCard title="Blood oxygen (SpO2)" hint="The watch measures SpO2 while you sleep."
                ariaLabel={describeSeries('SpO2', o.v, '%')} height={200}
                option={timeSeries({ from: o.from, to: o.to, y: [{ min: 80, max: 100 }], series: [{ id: 'spo2', name: 'SpO2', t: o.t, v: o.v, kind: 'line', role: 'extra', unit: '%', gapS: 1800 }] }, ctx)}
                table={{ head: ['Time', 'SpO2 (%)'], rows: o.t.map((t, i) => [timeOfDay(t, p.tz), o.v[i].toFixed(0)]) }} />
            )}
          </div>
        )
      }}
    </Loaded>
  )
}

function BreathingRange() {
  const { p, ctx } = useHealth()
  const daily = useDailyRange(p.range.from, p.range.to)
  const { days, labels } = rangeDays(p.range, p.period)
  return (
    <Loaded q={[daily]}>
      {() => {
        const m = byDay(daily.data)
        const resp = days.map((d) => m.get(d)?.respiration_avg ?? null)
        const spo2 = days.map((d) => m.get(d)?.spo2_avg ?? null)
        if (!resp.some((x) => x != null) && !spo2.some((x) => x != null)) return <Empty p={p} what="breathing recorded" />
        const rs = stats(resp)
        const os = stats(spo2)
        const focus = days.indexOf(p.day)
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Respiration', value: rs ? rs.avg.toFixed(1) : '–', unit: 'brpm', tone: 'lime', hint: rs ? `${rs.min.toFixed(1)}–${rs.max.toFixed(1)}` : undefined },
                { label: 'SpO2', value: os ? Math.round(os.avg) : '–', unit: '%', hint: os ? `lowest day ${Math.round(os.min)} %` : undefined },
              ]} />
            </section>
            <ChartCard title="Respiration by day" ariaLabel={describeSeries('Daily respiration', resp, 'breaths per minute', 1)} height={200}
              option={dailyBars({ labels, v: resp, focus, kind: 'line', axis: true, yAxis: true, unit: 'brpm', digits: 1 }, ctx)} onEvents={dayClick(days, p.open)}
              table={{ head: ['Day', 'Breaths/min'], rows: days.map((d, i) => [d, num(resp[i], 1)]).filter((r) => r[1] !== '–') }} />
            <ChartCard title="SpO2 by day" ariaLabel={describeSeries('Daily SpO2', spo2, '%')} height={200}
              option={dailyBars({ labels, v: spo2, focus, kind: 'line', axis: true, yAxis: true, unit: '%' }, ctx)} onEvents={dayClick(days, p.open)}
              table={{ head: ['Day', 'SpO2 (%)'], rows: days.map((d, i) => [d, num(spo2[i])]).filter((r) => r[1] !== '–') }} />
          </div>
        )
      }}
    </Loaded>
  )
}
