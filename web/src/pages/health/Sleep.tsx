// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useId } from 'react'
import { useDailyRange, useNightStages, useNights } from '../../api/hooks/data'
import type { Night, NightStages, Series } from '../../api/types'
import { Ring, describeSeries, rangeBars, describeRangeBars, stageBars, describeStageBars, timeSeries, type Ctx } from '../../charts'
import { calendarHeatmap, describeCalendarHeatmap } from '../../charts/builders/calendarHeatmap'
import { describeHypnogram, hypnogram } from '../../charts/builders/hypnogram'
import type { Role } from '../../charts/theme/tokens'
import { hm, timeOfDay } from '../../lib/format'
import { asleep, clockOfEvening, eveningMinutes, stageSegments } from '../../lib/health'
import { addDays, shortDay } from '../../lib/period'
import { Meter } from '../../glance/cards/parts'
import {
  byDay, Card, ChartCard, dayClick, Empty, Kpis, Loaded, nightsByDay, num, rangeDays, StageBar, useHealth,
} from './parts'
import { NapsCard } from './extras'

export default function SleepPage() {
  const { p } = useHealth()
  if (p.period !== 'day') return p.period === 'year' ? <SleepYear /> : <SleepRange />
  return (
    <div className="grid gap-5">
      <SleepDay />
      <NapsCard day={p.day} tz={p.tz} />
    </div>
  )
}

function SleepDay() {
  const { p, ctx } = useHealth()
  const nights = useNights(addDays(p.day, -1), p.day)
  const night = nightsByDay(nights.data).get(p.day)
  const daily = useDailyRange(p.day, p.day)
  const stages = useNightStages(night?.start)
  return (
    <Loaded q={[nights, daily]}>
      {() => {
        if (!night) return <Empty p={p} what="night recorded" />
        const need = daily.data?.[0]?.values.sleep_need
        const slept = asleep(night)
        return (
          <div className="grid gap-5">
            <section className="card grid gap-6 p-5 sm:p-6 lg:grid-cols-[auto_minmax(0,1fr)]">
              <div className="flex items-center gap-6">
                <Ring value={night.score} max={100} size={150} stroke={10} label={`Sleep score ${num(night.score)}`}>
                  <span className="num text-[44px] leading-none">{num(night.score)}</span>
                  <span className="caps mt-1 !text-[10px]">score</span>
                </Ring>
                <div className="grid min-w-[180px] gap-3">
                  {([['Quality', night.quality_score], ['Duration', night.duration_score], ['Recovery', night.recovery_score]] as const).map(([k, v]) => (
                    <div key={k}>
                      <div className="flex justify-between text-[12px] text-muted"><span>{k}</span><span className="num text-text">{num(v)}</span></div>
                      <Meter value={v} max={100} label={`${k} ${num(v)} of 100`} />
                    </div>
                  ))}
                </div>
              </div>
              <div className="grid content-start gap-5">
                <Kpis items={[
                  { label: 'Asleep', value: hm(slept), hint: need ? `need ${hm(need * 60)}` : undefined, tone: slept != null && need && slept >= need * 60 ? 'lime' : undefined },
                  { label: 'Fell asleep', value: timeOfDay(night.start, p.tz) },
                  { label: 'Woke up', value: timeOfDay(night.end, p.tz) },
                  { label: 'Lowest HR', value: num(night.min_hr), unit: 'bpm' },
                  { label: 'Body Battery', value: night.body_battery_end != null && night.body_battery_start != null ? `+${night.body_battery_end - night.body_battery_start}` : '–',
                    hint: night.body_battery_end != null ? `woke at ${night.body_battery_end}` : undefined },
                ]} />
                <StageBar night={night} />
              </div>
            </section>
            <Overnight night={night} data={stages.data} pending={stages.isPending} ctx={ctx} />
          </div>
        )
      }}
    </Loaded>
  )
}

const overnight: { key: keyof Omit<NightStages, 'start' | 'end' | 'sleep_stage'>; label: string; unit: string; role: Role; digits?: number }[] = [
  { key: 'heart_rate', label: 'Heart rate', unit: 'bpm', role: 'primary' },
  { key: 'hrv', label: 'HRV', unit: 'ms', role: 'primary' },
  { key: 'respiration', label: 'Respiration', unit: 'brpm', role: 'primary', digits: 1 },
  { key: 'stress', label: 'Stress', unit: '', role: 'warn' },
  { key: 'body_battery', label: 'Body Battery', unit: '%', role: 'primary' },
]

/** Overnight is the hypnogram and the night's values, linked: one crosshair, one zoom. */
function Overnight({ night, data, pending, ctx }: { night: Night; data?: NightStages; pending: boolean; ctx: Ctx }) {
  const group = useId()
  if (pending) return <Card title="The night"><p className="m-0 text-[13px] text-muted">Loading the night…</p></Card>
  if (!data) return null
  const segs = stageSegments(data.sleep_stage.t, data.sleep_stage.v, data.end)
  const input = { from: data.start, to: data.end, segments: segs }
  const panels = overnight.filter((o) => (data[o.key] as Series).t.length > 0)
  return (
    <div className="grid gap-5">
      {segs.length > 0 ? (
        <ChartCard title="Stages" ariaLabel={describeHypnogram(input)} option={hypnogram(input, ctx)} height={170} group={group}
          table={{ head: ['From', 'To', 'Stage'], rows: segs.map((s) => [timeOfDay(s.from, ctx.tz), timeOfDay(s.to, ctx.tz), ['', 'Awake', 'Light', 'Deep', 'REM'][s.stage]]) }} />
      ) : (
        <Card title="Stages"><p className="m-0 text-[13px] text-muted">The watch kept no stages for this night ({timeOfDay(night.start, ctx.tz)}–{timeOfDay(night.end, ctx.tz)}).</p></Card>
      )}
      <div className="grid gap-5 xl:grid-cols-2">
        {panels.map((o) => {
          const s = data[o.key] as Series
          return (
            <ChartCard key={o.key} title={o.label} height={150} group={group}
              ariaLabel={describeSeries(o.label, s.v, o.unit, o.digits ?? 0)}
              option={timeSeries({ from: data.start, to: data.end, digits: o.digits,
                series: [{ id: o.key, name: o.label, t: s.t, v: s.v, min: s.min, max: s.max, kind: o.key === 'stress' ? 'bar' : 'line', role: o.role, unit: o.unit, gapS: 900 }] }, ctx)}
              table={{ head: ['Time', o.label], rows: s.t.map((t, i) => [timeOfDay(t, ctx.tz), s.v[i].toFixed(o.digits ?? 0)]) }} />
          )
        })}
      </div>
    </div>
  )
}

function SleepRange() {
  const { p, ctx } = useHealth()
  const nights = useNights(p.range.from, p.range.to)
  const daily = useDailyRange(p.range.from, p.range.to)
  const { days, labels } = rangeDays(p.range, p.period)
  return (
    <Loaded q={[nights, daily]}>
      {() => {
        const byNight = nightsByDay(nights.data)
        const list = days.map((d) => byNight.get(d))
        if (!list.some(Boolean)) return <Empty p={p} what="nights recorded" />
        const h = (v: number | null | undefined) => (v == null ? null : v / 3600)
        const stagesIn = { labels, deep: list.map((n) => h(n?.deep_s)), light: list.map((n) => h(n?.light_s)), rem: list.map((n) => h(n?.rem_s)),
          awake: list.map((n) => h(n?.awake_s)), score: list.map((n) => n?.score ?? null), focus: days.indexOf(p.day) }
        const bed = list.map((n) => (n ? eveningMinutes(n.start, p.tz) : null))
        const wake = list.map((n) => (n ? eveningMinutes(n.end, p.tz) : null))
        const got = list.filter((n): n is Night => !!n)
        const avg = (v: (number | null)[]) => { const x = v.filter((y): y is number => y != null); return x.length ? x.reduce((a, b) => a + b, 0) / x.length : null }
        const need = avg(days.map((d) => byDay(daily.data).get(d)?.values.sleep_need ?? null))
        const avgAsleep = avg(got.map((n) => asleep(n)))
        const bedIn = { labels, lo: bed, hi: wake, name: 'In bed', format: clockOfEvening, focus: days.indexOf(p.day) }
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Average score', value: num(avg(got.map((n) => n.score))), tone: 'lime' },
                { label: 'Average asleep', value: hm(avgAsleep), hint: need ? `need about ${hm(need * 60)}` : undefined },
                { label: 'Usual bedtime', value: avg(bed) == null ? '–' : clockOfEvening(avg(bed)!) },
                { label: 'Usual wake', value: avg(wake) == null ? '–' : clockOfEvening(avg(wake)!) },
                { label: 'Nights', value: `${got.length}`, hint: `of ${days.length}` },
              ]} />
            </section>
            <ChartCard title="Stages and score" ariaLabel={describeStageBars(stagesIn)} option={stageBars(stagesIn, ctx)} height={260}
              onEvents={dayClick(days, p.open)} hint="Choose a night to see it in detail."
              table={{ head: ['Night', 'Score', 'Deep', 'Light', 'REM', 'Awake'], rows: got.map((n) => [n.day, num(n.score), hm(n.deep_s), hm(n.light_s), hm(n.rem_s), hm(n.awake_s)]) }} />
            <ChartCard title="Bedtime consistency" ariaLabel={describeRangeBars(bedIn)} option={rangeBars(bedIn, ctx)} height={220}
              onEvents={dayClick(days, p.open)} hint="Each bar runs from going to sleep to waking up."
              table={{ head: ['Night', 'Asleep at', 'Woke at'], rows: got.map((n) => [n.day, timeOfDay(n.start, p.tz), timeOfDay(n.end, p.tz)]) }} />
          </div>
        )
      }}
    </Loaded>
  )
}

function SleepYear() {
  const { p, ctx } = useHealth()
  const nights = useNights(p.range.from, p.range.to)
  return (
    <Loaded q={[nights]}>
      {() => {
        const got = [...nightsByDay(nights.data).values()]
        if (!got.length) return <Empty p={p} what="nights recorded" />
        const input = { from: p.range.from, to: p.range.to, data: got.map((n) => [n.day, n.score] as [string, number | null]), name: 'Sleep score', min: 40, max: 100 }
        const scores = got.map((n) => n.score).filter((x): x is number => x != null)
        const best = got.reduce<Night | null>((a, n) => (n.score != null && (a?.score == null || n.score > a.score) ? n : a), null)
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Average score', value: scores.length ? Math.round(scores.reduce((a, b) => a + b, 0) / scores.length) : '–', tone: 'lime' },
                { label: 'Best night', value: num(best?.score), hint: best ? shortDay(best.day) : undefined },
                { label: 'Nights', value: `${got.length}` },
              ]} />
            </section>
            <ChartCard title="Sleep score by day" ariaLabel={describeCalendarHeatmap(input)} option={calendarHeatmap(input, ctx)} height={190}
              onEvents={dayClick([], p.open)} hint="Brighter is a better night. Choose a day to see it."
              table={{ head: ['Night', 'Score'], rows: got.map((n) => [n.day, num(n.score)]) }} />
          </div>
        )
      }}
    </Loaded>
  )
}
