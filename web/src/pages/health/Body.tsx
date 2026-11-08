// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMe } from '../../api/hooks/auth'
import { useDailyRange } from '../../api/hooks/data'
import { useBody } from '../../api/hooks/readers'
import type { WeighIn } from '../../api/types'
import { dailyBars, describeSeries, stats, timeSeries } from '../../charts'
import { addDays, relDay, today } from '../../lib/period'
import { byDay, ChartCard, dayClick, Empty, Kpis, Loaded, num, rangeDays, useHealth } from './parts'

/** weight in the account's units: kg, or lb for imperial. */
function weightOf(kg: number | null | undefined, imperial: boolean): number | null {
  return kg == null ? null : imperial ? kg * 2.20462 : kg
}

export default function BodyPage() {
  const { p } = useHealth()
  return p.period === 'day' ? <BodyDay /> : <BodyRange />
}

/** BodyDay: the last weighing up to the day, the night's skin temperature, the day's drinks. */
function BodyDay() {
  const { p } = useHealth()
  const imperial = useMe().data?.units === 'imperial'
  const unit = imperial ? 'lb' : 'kg'
  const body = useBody(addDays(p.day, -365), p.day)
  const daily = useDailyRange(p.day, p.day)
  return (
    <Loaded q={[body, daily]}>
      {() => {
        const b = body.data!
        const last: WeighIn | undefined = b.weigh_ins.filter((w) => w.day <= p.day).at(-1)
        const skin = b.skin_temp.find((n) => n.day === p.day)
        const v = byDay(daily.data).get(p.day)?.values ?? {}
        if (!last && !skin && v.hydration == null && v.fitness_age == null) {
          return <Empty p={p} what="body measurements">A scale (an Index smart scale) or a Garmin Connect export brings weigh-ins; the watch measures skin temperature overnight.</Empty>
        }
        return (
          <div className="grid gap-5">
            {last && (
              <section className="card p-5 sm:p-6">
                <p className="caps m-0 mb-4 !text-[10px] !text-faint">
                  Weigh-in {last.day === p.day ? 'this day' : relDay(last.day, today(p.tz))}{last.source === 'json' ? ' · from Garmin Connect' : ''}
                </p>
                <Kpis items={[
                  { label: 'Weight', value: num(weightOf(last.weight_kg, imperial), 1), unit, tone: 'lime' },
                  { label: 'Body fat', value: num(last.fat_pct, 1), unit: '%' },
                  { label: 'BMI', value: num(last.bmi, 1) },
                  { label: 'Muscle', value: num(weightOf(last.muscle_kg, imperial), 1), unit },
                  { label: 'Bone', value: num(weightOf(last.bone_kg, imperial), 1), unit },
                  { label: 'Body water', value: num(last.hydration_pct, 1), unit: '%' },
                ]} />
                {(last.bmr_kcal != null || last.metabolic_age != null) && (
                  <p className="m-0 mt-4 text-[13px] text-muted">
                    {last.bmr_kcal != null && <>Resting metabolic rate {last.bmr_kcal} kcal/day. </>}
                    {last.metabolic_age != null && <>Metabolic age {last.metabolic_age}. </>}
                    {last.visceral_rating != null && <>Visceral fat rating {last.visceral_rating}.</>}
                  </p>
                )}
              </section>
            )}
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Skin temperature', value: skin?.deviation_c != null ? `${skin.deviation_c >= 0 ? '+' : '−'}${Math.abs(skin.deviation_c).toFixed(1)}` : '–', unit: '°C', hint: 'from your baseline, overnight' },
                { label: 'Hydration', value: v.hydration != null ? num(v.hydration) : '–', unit: 'ml', hint: 'logged this day' },
                { label: 'Calorie goal', value: v.calorie_goal != null ? num(v.calorie_goal) : '–', unit: 'kcal' },
                { label: 'Fitness age', value: v.fitness_age != null ? num(v.fitness_age) : '–', unit: 'years' },
              ]} />
            </section>
          </div>
        )
      }}
    </Loaded>
  )
}

/** BodyRange: weight and body fat over the period, nightly skin temperature, daily hydration. */
function BodyRange() {
  const { p, ctx } = useHealth()
  const imperial = useMe().data?.units === 'imperial'
  const unit = imperial ? 'lb' : 'kg'
  const body = useBody(p.range.from, p.range.to)
  const daily = useDailyRange(p.range.from, p.range.to)
  const { days, labels } = rangeDays(p.range, p.period)
  return (
    <Loaded q={[body, daily]}>
      {() => {
        const b = body.data!
        const m = byDay(daily.data)
        const skin = new Map(b.skin_temp.map((n) => [n.day, n.deviation_c]))
        const skinV = days.map((d) => skin.get(d) ?? null)
        const water = days.map((d) => m.get(d)?.values.hydration ?? null)
        const ws = b.weigh_ins
        if (!ws.length && !skinV.some((x) => x != null) && !water.some((x) => x != null)) {
          return <Empty p={p} what="body measurements">A scale (an Index smart scale) or a Garmin Connect export brings weigh-ins; the watch measures skin temperature overnight.</Empty>
        }
        const kg = ws.map((w) => weightOf(w.weight_kg, imperial))
        const st = stats(kg)
        const delta = kg.length > 1 ? kg[kg.length - 1]! - kg[0]! : null
        const fat = ws.map((w) => w.fat_pct)
        const fs = stats(fat)
        const t = ws.map((w) => w.ts)
        const focus = days.indexOf(p.day)
        const [from, to] = [Date.parse(p.range.from + 'T00:00:00Z') / 1000, Date.parse(addDays(p.range.to, 1) + 'T00:00:00Z') / 1000]
        return (
          <div className="grid gap-5">
            <section className="card p-5 sm:p-6">
              <Kpis items={[
                { label: 'Weight', value: kg.length ? num(kg[kg.length - 1], 1) : '–', unit, tone: 'lime', hint: ws.length ? `last weigh-in ${relDay(ws[ws.length - 1].day, today(p.tz))}` : 'no weigh-in' },
                { label: 'Change', value: delta != null ? `${delta >= 0 ? '+' : '−'}${Math.abs(delta).toFixed(1)}` : '–', unit, hint: ws.length > 1 ? `${ws.length} weigh-ins` : undefined },
                { label: 'Range', value: st ? `${st.min.toFixed(1)}–${st.max.toFixed(1)}` : '–', unit },
                { label: 'Body fat', value: fs ? fs.avg.toFixed(1) : '–', unit: '%', hint: 'average' },
              ]} />
            </section>
            {ws.length > 0 && (
              <ChartCard title="Weight" hint="Each weigh-in; the line joins them across days without one."
                ariaLabel={describeSeries('Weight', kg, unit, 1)} height={220}
                option={timeSeries({ from, to, digits: 1, series: [{ id: 'kg', name: 'Weight', t, v: kg, kind: 'line', role: 'primary', unit }] }, ctx)}
                table={{ head: ['Day', `Weight (${unit})`, 'Body fat (%)', 'BMI'], rows: ws.map((w, i) => [w.day, num(kg[i], 1), num(w.fat_pct, 1), num(w.bmi, 1)]) }} />
            )}
            {fs && (
              <ChartCard title="Body fat" ariaLabel={describeSeries('Body fat', fat, '%', 1)} height={180}
                option={timeSeries({ from, to, digits: 1, series: [{ id: 'fat', name: 'Body fat', t, v: fat, kind: 'line', role: 'extra', unit: '%' }] }, ctx)}
                table={{ head: ['Day', 'Body fat (%)'], rows: ws.flatMap((w) => (w.fat_pct == null ? [] : [[w.day, w.fat_pct.toFixed(1)]])) }} />
            )}
            {skinV.some((x) => x != null) && (
              <ChartCard title="Skin temperature" hint="Each night's average against your baseline (°C)."
                ariaLabel={describeSeries('Skin temperature deviation', skinV, '°C', 1)} height={180}
                option={dailyBars({ labels, v: skinV, focus, axis: true, yAxis: true, unit: '°C', digits: 1 }, ctx)} onEvents={dayClick(days, p.open)}
                table={{ head: ['Night', 'Deviation (°C)'], rows: days.flatMap((d, i) => (skinV[i] == null ? [] : [[d, skinV[i]!.toFixed(1)]])) }} />
            )}
            {water.some((x) => x != null) && (
              <ChartCard title="Hydration" hint="What you logged drinking each day (ml)."
                ariaLabel={describeSeries('Hydration', water, 'ml')} height={180}
                option={dailyBars({ labels, v: water, focus, axis: true, yAxis: true, unit: 'ml' }, ctx)} onEvents={dayClick(days, p.open)}
                table={{ head: ['Day', 'ml'], rows: days.flatMap((d, i) => (water[i] == null ? [] : [[d, num(water[i])]])) }} />
            )}
          </div>
        )
      }}
    </Loaded>
  )
}
