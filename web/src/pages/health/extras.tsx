// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo, useState } from 'react'
import { useECG, useECGs, useHealthStatus, useNaps } from '../../api/hooks/readers'
import { describeSeries, timeSeries, type Ctx } from '../../charts'
import { hm, timeOfDay } from '../../lib/format'
import { relDay, today } from '../../lib/period'
import { Skeleton } from '../../ui/kit'
import { Card, ChartCard } from './parts'

const classWords: Record<string, string> = {
  sinus_rhythm: 'Sinus rhythm', sinus_normal: 'Sinus rhythm', atrial_fibrillation: 'Signs of AFib', inconclusive: 'Inconclusive',
  inconclusive_high_hr: 'Inconclusive: heart rate high', inconclusive_low_hr: 'Inconclusive: heart rate low',
  inconclusive_poor_recording: 'Inconclusive: poor recording', '0': 'Sinus rhythm',
}

/** EcgCard lists the ECG recordings and draws the one picked. Not a diagnosis: the watch's own
 * classification, as the app shows it. */
export function EcgCard({ tz, ctx }: { tz: string; ctx: Ctx }) {
  const list = useECGs()
  const [picked, setPicked] = useState<number | null>(null)
  const recs = list.data ?? []
  const id = picked ?? recs[0]?.id ?? null
  const one = useECG(id)
  const wave = useMemo(() => {
    const e = one.data
    const v = e?.samples?.raw?.length ? e.samples.raw : e?.samples?.smooth ?? []
    const rate = e?.samples?.raw?.length ? e.raw_rate : e?.smooth_rate
    if (!e || !v.length || !rate) return null
    const t = v.map((_, i) => e.start + i / rate)
    return { t, v, from: e.start, to: e.start + v.length / rate }
  }, [one.data])
  if (list.isPending) return <Skeleton className="h-40 rounded-[18px]" />
  if (!recs.length) return null
  return (
    <Card title="ECG recordings" hint="The watch's own classification of each 30-second recording. It is not a diagnosis.">
      <ul className="m-0 mb-4 flex list-none flex-wrap gap-2 p-0">
        {recs.map((e) => (
          <li key={e.id}>
            <button type="button" aria-pressed={id === e.id} onClick={() => setPicked(e.id)}
              className={`rounded-xl border px-3 py-2 text-left text-[13px] ${id === e.id ? 'border-lime-dim bg-lime-fill' : 'border-border hover:border-muted'}`}>
              <span className="block text-text">{relDay(e.day, today(tz))}, {timeOfDay(e.start, tz)}</span>
              <span className="block text-muted">{classWords[e.class] ?? e.class}{e.avg_hr != null ? ` · ${Math.round(e.avg_hr)} bpm` : ''}{e.rmssd_ms != null ? ` · HRV ${e.rmssd_ms} ms` : ''}</span>
            </button>
          </li>
        ))}
      </ul>
      {one.isPending ? <Skeleton className="h-48" /> : wave ? (
        <ChartCard title="Waveform" className="!border-0 !p-0 !shadow-none" ariaLabel={describeSeries('ECG', wave.v, 'µV')} height={220}
          option={timeSeries({ from: wave.from, to: wave.to, zoom: true, series: [{ id: 'ecg', name: 'ECG', t: wave.t, v: wave.v, kind: 'line', role: 'primary', unit: 'µV' }] }, ctx)} />
      ) : <p className="m-0 text-[13px] text-muted">This recording has no samples.</p>}
    </Card>
  )
}

/** NapsCard lists the day's naps. */
export function NapsCard({ day, tz }: { day: string; tz: string }) {
  const q = useNaps(day, day)
  const naps = q.data ?? []
  if (!naps.length) return null
  return (
    <Card title="Naps">
      <ul className="m-0 grid list-none gap-1 p-0 text-[14px]">
        {naps.map((n) => (
          <li key={n.start} className="flex justify-between gap-3">
            <span>{timeOfDay(n.start, tz)}–{timeOfDay(n.end, tz)}</span>
            <span className="num text-muted">{hm(n.end - n.start)}{n.how === 1 ? ' · logged by hand' : ''}</span>
          </li>
        ))}
      </ul>
    </Card>
  )
}

const statusMetrics: Record<string, string> = {
  hrv: 'HRV', hr: 'Resting heart rate', respiration: 'Respiration', skin_temp_c: 'Skin temperature', spo2: 'Pulse ox', skin_temp: 'Skin temperature',
}

/** HealthStatusCard shows Garmin Connect's health status of the latest day in [from, to]: each
 * overnight metric against its personal baseline. */
export function HealthStatusCard({ from, to }: { from: string; to: string }) {
  const q = useHealthStatus(from, to)
  const rows = q.data ?? []
  if (!rows.length) return null
  const last = rows[rows.length - 1].day
  const day = rows.filter((r) => r.day === last)
  return (
    <Card title="Health status" hint={`Overnight values against your baseline, ${last} (from Garmin Connect).`}>
      <ul className="m-0 grid list-none gap-3 p-0 sm:grid-cols-2 lg:grid-cols-3">
        {day.map((r) => {
          const out = r.status !== '' && r.status !== 'normal' && r.status !== 'in_range'
          return (
            <li key={r.metric} className="rounded-xl border border-border px-4 py-3">
              <p className="caps m-0 !text-[10px] !text-faint">{statusMetrics[r.metric] ?? r.metric}</p>
              <p className={`num m-0 mt-1 text-[22px] leading-none ${out ? 'text-amber' : 'text-text'}`}>{r.value == null ? '–' : r.value.toFixed(r.value % 1 ? 1 : 0)}</p>
              <p className="m-0 mt-1 text-[12px] text-muted">
                {r.low != null && r.high != null ? `baseline ${r.low.toFixed(1)}–${r.high.toFixed(1)}` : 'no baseline yet'}{r.status ? ` · ${r.status.replaceAll('_', ' ')}` : ''}
              </p>
            </li>
          )
        })}
      </ul>
    </Card>
  )
}
