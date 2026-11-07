// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Bike as BikeIcon } from 'lucide-react'
import { useMe } from '../../api/hooks/auth'
import { useWatchProfile } from '../../api/hooks/training'
import type { Device, WatchGoal } from '../../api/types'
import { distance } from '../../lib/format'

const hhmm = (s: number) => `${String(Math.floor(s / 3600) % 24).padStart(2, '0')}:${String(Math.floor((s % 3600) / 60)).padStart(2, '0')}`

/** goalText says a watch goal in words. */
export function goalText(g: WatchGoal): string {
  switch (g.type) {
    case 'steps':
      return `${g.target.toLocaleString()} steps a day${g.source === 'auto' ? ' (set by the watch)' : ''}`
    case 'active_minutes':
      return `${g.target} intensity minutes a week`
    case 'ascent':
      return `${Math.round(g.target / 3.048)} floors a day (${g.target} m)`
  }
  return `${g.type.replace(/_/g, ' ')}: ${g.target} ${g.unit}`
}

/** FromWatch shows what the watch itself keeps: its settings, goals and zones, and its bikes. Read
 * only: they change on the watch (or in Garmin Connect) and arrive with the next sync. */
export function FromWatch({ d }: { d: Device }) {
  const q = useWatchProfile()
  const units = useMe().data?.units ?? 'metric'
  const p = q.data
  // A settings file from before goarmin recorded which watch sent it (device 0) still counts.
  if (!p?.settings || (p.device_id && p.device_id !== d.id)) return null
  const s = p.settings
  const z = s.zones?.[0]
  const rows: [string, string][] = [
    ['Clock', s.clock === '12h' ? '12-hour' : s.clock === '24h' ? '24-hour' : '–'],
    ['Date', s.date_mode === 'day_month' ? 'day before month' : s.date_mode === 'month_day' ? 'month before day' : '–'],
    ['Units', s.units.distance === 'statute' ? 'miles and feet' : s.units.distance === 'metric' ? 'kilometres and metres' : '–'],
    ['Resting heart rate', s.resting_hr ? `${s.resting_hr} bpm` : 'measured by the watch'],
    ['Heart-rate zones', z?.hr?.length === 6 ? `${z.hr.slice(0, 5).join(' / ')} bpm, maximum ${z.hr[5]}${z.threshold_hr ? `, threshold ${z.threshold_hr}` : ''}` : '–'],
    ['Zones based on', z?.hr_calc === 'percent_hrr' ? 'heart-rate reserve' : z?.hr_calc === 'percent_lthr' ? 'lactate threshold' : z?.hr_calc === 'percent_max_hr' ? 'maximum heart rate' : '–'],
    ['Functional threshold power', z?.ftp ? `${z.ftp} W` : '–'],
    ['Sleep window', s.sleep_s >= 0 && s.wake_s >= 0 ? `${hhmm(s.sleep_s)} – ${hhmm(s.wake_s)}` : '–'],
    ['Step length', [s.run_step_m && `running ${Math.round(s.run_step_m * 100)} cm`, s.walk_step_m && `walking ${Math.round(s.walk_step_m * 100)} cm`].filter(Boolean).join(', ') || 'measured by the watch'],
    ['Height, weight', [s.height_m && `${s.height_m.toFixed(2)} m`, s.weight_kg && `${s.weight_kg.toFixed(1)} kg`].filter(Boolean).join(', ') || '–'],
    ['Activity level', s.activity_level != null ? `${s.activity_level} of 100${s.athlete ? ', athlete' : ''}` : '–'],
  ]
  return (
    <>
      <section className="card p-5">
        <h2 className="caps m-0">From the watch</h2>
        <p className="m-0 mb-4 mt-1 text-[13px] text-muted">
          As the watch has them{p.recorded_at ? ` (${new Date(p.recorded_at * 1000).toLocaleDateString()})` : ''}. Change them on the watch; they arrive with the next sync.
        </p>
        <dl className="m-0 grid grid-cols-1 gap-x-6 gap-y-2.5 sm:grid-cols-[14rem_minmax(0,1fr)]">
          {rows.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-[13px] text-muted">{k}</dt>
              <dd className="m-0 text-[14px] text-text">{v}</dd>
            </div>
          ))}
        </dl>
        {(p.goals?.length ?? 0) > 0 && (
          <>
            <h3 className="caps m-0 mt-5 !text-[10px] !text-faint">Goals</h3>
            <ul className="m-0 mt-2 grid list-none gap-1 p-0 text-[14px]">
              {p.goals.map((g) => <li key={`${g.type}|${g.sport}`}>{goalText(g)}</li>)}
            </ul>
          </>
        )}
      </section>
      {(s.bikes?.length ?? 0) > 0 && (
        <section className="card p-5">
          <h2 className="caps m-0 mb-3">Gear</h2>
          <ul className="m-0 grid list-none gap-3 p-0 sm:grid-cols-2">
            {s.bikes!.map((b, i) => (
              <li key={i} className="flex items-start gap-3 rounded-xl border border-border p-3.5">
                <BikeIcon size={18} strokeWidth={1.75} className={`mt-0.5 shrink-0 ${b.enabled ? 'text-lime' : 'text-faint'}`} aria-hidden />
                <span className="min-w-0">
                  <span className="block text-[14px] text-text">{b.name || 'Bike'}{!b.enabled && <span className="text-muted"> · not in use</span>}</span>
                  <span className="num mt-1 block text-[20px] leading-none">{b.odometer_m ? distance(b.odometer_m, units) : '–'}</span>
                  <span className="mt-1 block text-[12px] text-muted">
                    {[b.weight_kg && `${b.weight_kg} kg`, b.wheel_mm && `wheel ${b.wheel_mm} mm`, b.crank_mm && `cranks ${b.crank_mm} mm`,
                      b.sensors?.length && `sensors: ${b.sensors.map((x) => x.replace('_', ' and ')).join(', ')}`].filter(Boolean).join(' · ') || 'odometer'}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </>
  )
}
