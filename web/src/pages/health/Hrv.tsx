// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useDailyRange, useNightStages, useNights } from '../../api/hooks/data'
import { hrvStatus } from '../../api/types'
import { describeSeries, timeSeries } from '../../charts'
import { describeHrvBaseline, hrvBaseline } from '../../charts/builders/hrvBaseline'
import { timeOfDay } from '../../lib/format'
import { hrvOf, rolling } from '../../lib/health'
import { addDays } from '../../lib/period'
import { byDay, Card, ChartCard, dayClick, Empty, Kpis, Loaded, nightsByDay, num, rangeDays, useHealth, withLead } from './parts'

export default function HrvPage() {
  const { p } = useHealth()
  return p.period === 'day' ? <HrvDay /> : <HrvRange />
}

/** Band shows last night's value on a scale with the balanced range marked. */
function Band({ value, low, high }: { value: number; low: number | null; high: number | null }) {
  const lo = Math.min(value, low ?? value) * 0.75
  const hi = Math.max(value, high ?? value) * 1.2
  const at = (v: number) => `${((v - lo) / (hi - lo)) * 100}%`
  const inside = low == null || high == null || (value >= low && value <= high)
  return (
    <div className="relative mt-6 h-2.5 rounded-full bg-track" role="img"
      aria-label={`Last night ${Math.round(value)} ms${low != null && high != null ? `, balanced range ${Math.round(low)} to ${Math.round(high)} ms` : ''}`}>
      {low != null && high != null && (
        <div className="absolute inset-y-0 rounded-full bg-lime-fill outline outline-1 outline-lime-dim" style={{ left: at(low), width: `calc(${at(high)} - ${at(low)})` }} />
      )}
      <div className={`absolute top-1/2 h-5 w-5 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-bg ${inside ? 'bg-lime shadow-[0_0_12px_var(--lime-glow)]' : 'bg-amber'}`}
        style={{ left: at(value) }} />
      {low != null && high != null && (
        <div className="absolute top-4 flex w-full justify-between font-mono text-[10px] text-faint" aria-hidden>
          <span style={{ position: 'absolute', left: at(low), transform: 'translateX(-50%)' }}>{Math.round(low)}</span>
          <span style={{ position: 'absolute', left: at(high), transform: 'translateX(-50%)' }}>{Math.round(high)}</span>
        </div>
      )}
    </div>
  )
}

function HrvDay() {
  const { p, ctx } = useHealth()
  const daily = useDailyRange(addDays(p.day, -6), p.day)
  const nights = useNights(addDays(p.day, -1), p.day)
  const night = nightsByDay(nights.data).get(p.day)
  const stages = useNightStages(night?.start)
  return (
    <Loaded q={[daily, nights]}>
      {() => {
        const week = (daily.data ?? []).map(hrvOf)
        const h = hrvOf(byDay(daily.data).get(p.day))
        const s = stages.data?.hrv
        if (h.value == null && !s?.t.length) return <Empty p={p} what="HRV recorded" />
        const values = week.map((x) => x.value).filter((x): x is number => x != null)
        const avg7 = h.weekly ?? (values.length ? values.reduce((a, b) => a + b, 0) / values.length : null)
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Last night', value: num(h.value), unit: 'ms', tone: h.value != null && h.low != null && h.high != null && (h.value < h.low || h.value > h.high) ? 'amber' : 'lime' },
                { label: 'Status', value: h.status != null ? hrvStatus[h.status] ?? '–' : '–' },
                { label: '7-day average', value: num(avg7), unit: 'ms' },
                { label: 'Balanced range', value: h.low != null && h.high != null ? `${Math.round(h.low)}–${Math.round(h.high)}` : '–', unit: 'ms' },
              ]} />
              {h.value != null && <Band value={h.value} low={h.low} high={h.high} />}
              <div className="h-4" />
            </section>
            {s && s.t.length > 0 && stages.data && (
              <ChartCard title="Through the night" hint="The watch measures HRV in 5-minute steps while you sleep."
                ariaLabel={describeSeries('HRV', s.v, 'ms')} height={200}
                option={timeSeries({ from: stages.data.start, to: stages.data.end, series: [{ id: 'hrv', name: 'HRV', t: s.t, v: s.v, kind: 'line', role: 'primary', unit: 'ms', gapS: 900 }] }, ctx)}
                table={{ head: ['Time', 'HRV (ms)'], rows: s.t.map((t, i) => [timeOfDay(t, p.tz), s.v[i].toFixed(0)]) }} />
            )}
            {night && !s?.t.length && <Card title="Through the night"><p className="m-0 text-[13px] text-muted">No 5-minute values for this night.</p></Card>}
          </div>
        )
      }}
    </Loaded>
  )
}

function HrvRange() {
  const { p, ctx } = useHealth()
  const lead = withLead(p.range, 6)
  const daily = useDailyRange(lead.from, lead.to)
  const { days, labels } = rangeDays(p.range, p.period)
  const allDays = rangeDays(lead, p.period).days
  return (
    <Loaded q={[daily]}>
      {() => {
        const m = byDay(daily.data)
        const all = allDays.map((d) => hrvOf(m.get(d)))
        const avgAll = rolling(all.map((x) => x.value), 7)
        const off = allDays.length - days.length
        const hv = all.slice(off)
        if (!hv.some((x) => x.value != null)) return <Empty p={p} what="HRV recorded" />
        // The average stops at today: it would carry the last nights into days to come.
        const avg = avgAll.slice(off).map((v, i) => (days[i] > p.today ? null : v))
        const input = { labels, value: hv.map((x) => x.value), low: hv.map((x) => x.low), high: hv.map((x) => x.high), avg, focus: days.indexOf(p.day) }
        const nights = hv.filter((x) => x.value != null)
        const inBand = nights.filter((x) => x.low == null || x.high == null || (x.value! >= x.low && x.value! <= x.high)).length
        const latest = [...hv].reverse().find((x) => x.value != null)
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Average', value: num(nights.reduce((a, x) => a + x.value!, 0) / nights.length), unit: 'ms', tone: 'lime' },
                { label: 'In balance', value: `${inBand}`, hint: `of ${nights.length} nights` },
                { label: 'Latest status', value: latest?.status != null ? hrvStatus[latest.status] ?? '–' : '–' },
              ]} />
            </section>
            <ChartCard title="Nightly HRV" hint="Dots are nights (amber outside your balanced range); the line is the 7-day average."
              ariaLabel={describeHrvBaseline(input)} option={hrvBaseline(input, ctx)} height={260} onEvents={dayClick(days, p.open)}
              table={{ head: ['Night', 'HRV (ms)', 'Balanced (ms)'], rows: days.map((d, i) => [d, num(hv[i].value), hv[i].low != null ? `${Math.round(hv[i].low!)}–${Math.round(hv[i].high!)}` : '–']).filter((r) => r[1] !== '–') }} />
          </div>
        )
      }}
    </Loaded>
  )
}
