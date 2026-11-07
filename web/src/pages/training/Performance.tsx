// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo, useState } from 'react'
import { useMe } from '../../api/hooks/auth'
import { useDailyRange } from '../../api/hooks/data'
import { usePeriod } from '../../app/section/usePeriod'
import { Chart, dailyBars } from '../../charts'
import { dailyLines, describeDailyLines } from '../../charts/builders/dailyLines'
import { clock } from '../../lib/format'
import { shortDay } from '../../lib/period'
import { change, latest, loadRatio, optimalBand, races, ratioBand, readinessLevel, statusRuns, toneVar, valuesOf } from '../../lib/training'
import { byDay, Card, chartCtx, ChartCard, Empty, Kpis, Loaded, num, rangeDays } from '../health/parts'
import { Lifetime } from './Lifetime'

const keysShown = ['training_status', 'training_readiness', 'acute_load', 'chronic_load', 'vo2max', 'endurance_score', 'hill_score', ...races.map((r) => r.key)]

export default function Performance() {
  const p = usePeriod()
  const q = useDailyRange(p.range.from, p.range.to)
  const units = useMe().data?.units ?? 'metric'
  const ctx = useMemo(() => chartCtx(p.tz), [p.tz])
  const { days, labels } = useMemo(() => rangeDays(p.range, p.period), [p.range, p.period])
  const s = useMemo(() => {
    const m = byDay(q.data)
    return Object.fromEntries(keysShown.map((k) => [k, valuesOf(days, m, k)])) as Record<string, (number | null)[]>
  }, [q.data, days])
  const has = (k: string) => s[k].some((v) => v != null)
  const [race, setRace] = useState<(typeof races)[number]['key']>('race_5k')

  return (
    <Loaded q={[q]}>
      {() => {
        if (!keysShown.some(has)) {
          return (
            <Empty p={p} what="training values">
              The watch writes training status, readiness, load and race predictions into its metrics files, and VO2 max with
              the daily-health file. None of them reached goarmin for this period.
            </Empty>
          )
        }
        const missing = [
          !has('training_status') && 'training status', !has('training_readiness') && 'readiness', !has('acute_load') && 'training load',
          !has('endurance_score') && 'endurance score', !has('hill_score') && 'hill score', !races.some((r) => has(r.key)) && 'race predictions',
        ].filter(Boolean) as string[]
        const vo2 = latest(s.vo2max)
        const ready = latest(s.training_readiness)
        const acute = latest(s.acute_load)
        const chronic = latest(s.chronic_load)
        const ratio = acute && chronic ? loadRatio(acute.value, chronic.value) : null
        const band = ratio != null ? ratioBand(ratio) : null
        const endurance = latest(s.endurance_score)
        const hill = latest(s.hill_score)
        const vo2Change = change(s.vo2max)
        const runs = statusRuns(days, s.training_status)
        const current = [...runs].reverse().find((r) => r.status)
        const raceShown = has(race) ? race : (races.find((r) => has(r.key))?.key ?? race)
        const raceInfo = races.find((r) => r.key === raceShown)!
        return (
          <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
            <section className="card grid gap-6 p-5 sm:p-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
              <div className="min-w-0">
                <p className="caps m-0 !text-[10px] !text-faint">Training status</p>
                {current?.status ? (
                  <>
                    <p className="m-0 mt-2 flex items-center gap-3 font-display text-[34px] uppercase leading-none tracking-[.02em]">
                      <span aria-hidden className="h-3 w-3 shrink-0 rounded-full" style={{ background: toneVar[current.status.tone] }} />
                      {current.status.label}
                    </p>
                    <p className="m-0 mt-3 text-[13px] text-muted">{current.status.note}</p>
                    <p className="m-0 mt-2 font-mono text-[11px] text-faint">
                      {current.n > 1 ? `since ${shortDay(current.from)}` : shortDay(current.to)}
                      {!current.status.confirmed && ' · label not yet confirmed on a watch'}
                    </p>
                  </>
                ) : (
                  <p className="m-0 mt-2 text-[14px] text-muted">Not recorded in this period.</p>
                )}
              </div>
              <Kpis items={[
                { label: 'Readiness', value: ready ? num(ready.value) : '–', tone: 'lime', hint: ready ? `${readinessLevel(ready.value)} · ${shortDay(days[ready.i])}` : undefined },
                {
                  label: 'VO2 max', value: vo2 ? vo2.value.toFixed(1) : '–',
                  hint: vo2 ? `ml/kg/min${vo2Change != null ? ` · ${vo2Change >= 0 ? '+' : '−'}${Math.abs(vo2Change).toFixed(1)}` : ''}` : undefined,
                },
                { label: 'Acute load', value: acute ? num(acute.value) : '–', tone: band?.tone === 'amber' || band?.tone === 'red' ? 'amber' : undefined, hint: band ? `${band.label} · ratio ${ratio!.toFixed(1)}` : undefined },
                { label: 'Chronic load', value: chronic ? num(chronic.value) : '–' },
                { label: 'Endurance', value: endurance ? num(endurance.value) : '–' },
                { label: 'Hill score', value: hill ? num(hill.value) : '–' },
              ]} />
            </section>

            {has('training_status') && (
              <Card title="Training status by day">
                <ol className="m-0 flex h-7 list-none gap-[2px] overflow-hidden rounded-md p-0" aria-label="Training status by day">
                  {runs.map((r) => (
                    <li key={r.from} style={{ flexGrow: r.n, background: r.status ? toneVar[r.status.tone] : 'var(--track)', opacity: r.status ? 0.85 : 1 }}
                      title={`${r.n > 1 ? `${shortDay(r.from)}–${shortDay(r.to)}` : shortDay(r.from)}: ${r.status?.label ?? 'not recorded'}`}>
                      <span className="sr-only">{r.n > 1 ? `${shortDay(r.from)} to ${shortDay(r.to)}` : shortDay(r.from)}: {r.status?.label ?? 'not recorded'}</span>
                    </li>
                  ))}
                </ol>
                <div className="mt-3 flex flex-wrap gap-x-5 gap-y-1" aria-hidden>
                  {[...new Map(runs.filter((r) => r.status).map((r) => [r.status!.value, r.status!])).values()].map((st) => (
                    <span key={st.value} className="inline-flex items-center gap-1.5 text-[12px] text-muted">
                      <span className="h-2 w-2 rounded-[2px]" style={{ background: toneVar[st.tone] }} />
                      {st.label} <span className="num text-text">{runs.filter((r) => r.status?.value === st.value).reduce((a, r) => a + r.n, 0)} d</span>
                    </span>
                  ))}
                </div>
              </Card>
            )}

            <div className="grid min-w-0 gap-5 xl:grid-cols-2">
              {has('training_readiness') && (
                <ChartCard title="Training readiness" hint="0–100 each morning, from sleep, recovery, HRV, load and stress."
                  option={dailyBars({ labels, v: s.training_readiness, focus: ready?.i, axis: true, yAxis: true, max: 100 }, ctx)}
                  ariaLabel={describeDailyLines({ labels, lines: [{ name: 'Training readiness', v: s.training_readiness }] })}
                  table={{ head: ['Day', 'Readiness'], rows: days.flatMap((d, i) => (s.training_readiness[i] == null ? [] : [[d, s.training_readiness[i]!]])) }} />
              )}
              {has('acute_load') && (() => {
                const opt = optimalBand(s.chronic_load)
                const input = {
                  labels, connect: true,
                  band: has('chronic_load') ? { name: 'Optimal range', ...opt } : undefined,
                  lines: [
                    { name: 'Acute load', v: s.acute_load, role: 'primary' as const, area: true },
                    ...(has('chronic_load') ? [{ name: 'Chronic load', v: s.chronic_load, role: 'context' as const, dashed: true }] : []),
                  ],
                }
                return (
                  <ChartCard title="Training load" hint="Acute load (the last 7 days) against the range your chronic load (about 4 weeks) makes optimal."
                    option={dailyLines(input, ctx)} ariaLabel={describeDailyLines(input)}
                    table={{ head: ['Day', 'Acute', 'Chronic', 'Ratio'], rows: days.flatMap((d, i) => (s.acute_load[i] == null ? [] : [[d, s.acute_load[i]!, s.chronic_load[i] ?? '–', loadRatio(s.acute_load[i], s.chronic_load[i])?.toFixed(2) ?? '–']])) }} />
                )
              })()}
              {has('vo2max') && (() => {
                const input = { labels, connect: true, unit: 'ml/kg/min', minInterval: 0.1, format: (v: number) => v.toFixed(1), lines: [{ name: 'VO2 max', v: s.vo2max, role: 'primary' as const }] }
                return (
                  <ChartCard title="VO2 max" hint="The watch's estimate of your maximal oxygen uptake, updated after runs and rides with heart rate."
                    option={dailyLines(input, ctx)} ariaLabel={describeDailyLines(input)}
                    table={{ head: ['Day', 'VO2 max'], rows: days.flatMap((d, i) => (s.vo2max[i] == null ? [] : [[d, s.vo2max[i]!.toFixed(1)]])) }} />
                )
              })()}
              {races.some((r) => has(r.key)) && (() => {
                const input = { labels, connect: true, inverse: true, minInterval: 5, format: clock, lines: [{ name: raceInfo.label, v: s[raceShown], role: 'primary' as const }] }
                return (
                  <Card title="Race predictions" right={
                    <div role="group" aria-label="Race" className="inline-flex rounded-full border border-border p-0.5 text-[12px]">
                      {races.filter((r) => has(r.key)).map((r) => (
                        <button key={r.key} type="button" aria-pressed={raceShown === r.key} onClick={() => setRace(r.key)}
                          className={`rounded-full px-3 py-1 ${raceShown === r.key ? 'bg-lime-fill text-lime' : 'text-muted hover:text-text'}`}>
                          {r.label === 'Half marathon' ? 'Half' : r.label}
                        </button>
                      ))}
                    </div>
                  }>
                    <ul className="m-0 mb-4 grid list-none grid-cols-2 gap-x-6 gap-y-4 p-0 sm:grid-cols-4">
                      {races.map((r) => {
                        const l = latest(s[r.key])
                        const c = change(s[r.key])
                        const unit = units === 'imperial' ? 1609.344 : 1000
                        return (
                          <li key={r.key} className="min-w-0">
                            <p className="caps m-0 !text-[10px] !text-faint">{r.label}</p>
                            <p className={`num m-0 mt-1.5 text-[22px] leading-none ${r.key === raceShown ? 'text-lime' : 'text-text'}`}>{l ? clock(l.value) : '–'}</p>
                            <p className="m-0 mt-1 truncate text-[12px] text-muted">
                              {l ? `${clock(l.value / (r.metres / unit))} /${units === 'imperial' ? 'mi' : 'km'}` : ''}
                              {c != null && c !== 0 && <span className={c < 0 ? 'text-lime' : ''}> · {c < 0 ? '−' : '+'}{clock(Math.abs(c))}</span>}
                            </p>
                          </li>
                        )
                      })}
                    </ul>
                    <Chart option={dailyLines(input, ctx)} ariaLabel={describeDailyLines(input)} height={200} />
                  </Card>
                )
              })()}
              {has('endurance_score') && (() => {
                const input = { labels, connect: true, lines: [{ name: 'Endurance score', v: s.endurance_score, role: 'primary' as const }] }
                return (
                  <ChartCard title="Endurance score" hint="How well you sustain long efforts, from your VO2 max and the load of your long activities."
                    option={dailyLines(input, ctx)} ariaLabel={describeDailyLines(input)} height={180}
                    table={{ head: ['Day', 'Endurance'], rows: days.flatMap((d, i) => (s.endurance_score[i] == null ? [] : [[d, s.endurance_score[i]!]])) }} />
                )
              })()}
              {has('hill_score') && (() => {
                const input = { labels, connect: true, lines: [{ name: 'Hill score', v: s.hill_score, role: 'primary' as const }] }
                return (
                  <ChartCard title="Hill score" hint="Strength and endurance on climbs, from runs uphill."
                    option={dailyLines(input, ctx)} ariaLabel={describeDailyLines(input)} height={180}
                    table={{ head: ['Day', 'Hill score'], rows: days.flatMap((d, i) => (s.hill_score[i] == null ? [] : [[d, s.hill_score[i]!]])) }} />
                )
              })()}
            </div>
            <Lifetime units={units} />
            {missing.length > 0 && (
              <p className="m-0 text-[13px] text-faint">
                Not recorded in this {p.period}: {missing.join(', ')}. The watch writes them into its metrics files.
              </p>
            )}
          </div>
        )
      }}
    </Loaded>
  )
}
