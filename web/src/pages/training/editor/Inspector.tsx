// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Repeat, Replace } from 'lucide-react'
import type { WorkoutStep } from '../../../api/types'
import { FigureView } from '../../../figures/FigureView'
import type { Units } from '../../../lib/format'
import { stepTitle } from '../../../lib/workout'
import { type Action, type Item } from '../../../lib/workoutEdit'
import { Choice } from '../../../ui/kit'
import { fieldClass, Labelled, NumberInput, PaceInput, Select, TimeInput } from './fields'

const LB = 0.45359237

/** Inspector edits the selected item: an exercise (with its figure), its sets, a rest or an
 * interval, or a repeat block. */
export function Inspector({ item, units, dispatch, onPick, figure }: {
  item: Item
  units: Units
  dispatch: (a: Action) => void
  onPick: () => void
  /** Show the 3D figure (off while the picker shows one: one WebGL context a page). */
  figure: boolean
}) {
  if (item.kind === 'repeat') {
    const timed = item.forS !== undefined
    return (
      <div className="grid gap-4">
        <h2 className="m-0 flex items-center gap-2 text-[16px] text-text"><Repeat size={16} className="text-lime" aria-hidden /> Repeat</h2>
        <Choice legend="Repeat" value={timed ? 'time' : 'rounds'} options={[['rounds', 'A number of times'], ['time', 'As many rounds as fit']]}
          onChange={(v) => dispatch({ type: 'repeat', id: item.id, forS: v === 'time' ? 600 : null, rounds: v === 'time' ? 0 : Math.max(2, item.rounds || 3) })} />
        {timed
          ? <TimeInput label="In" value={item.forS ?? 0} onChange={(s) => dispatch({ type: 'repeat', id: item.id, forS: s })} hint="An AMRAP: as many rounds as you manage in this time." />
          : <NumberInput label="Times" value={item.rounds} min={2} max={99} onChange={(v) => dispatch({ type: 'repeat', id: item.id, rounds: Math.round(v) })} />}
        <p className="m-0 text-[13px] text-muted">A repeat holds single steps. To repeat an exercise with its rest, make it sets.</p>
      </div>
    )
  }
  const s = item.step
  const set = (patch: Partial<WorkoutStep>) => dispatch({ type: 'step', id: item.id, patch })
  return (
    <div className="grid gap-4">
      {s.exercise ? (
        <>
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="m-0 font-mono text-[11px] uppercase tracking-[.14em] text-faint">{s.exercise.category.replace(/_/g, ' ')} · {s.exercise.number}</p>
              <h2 className="m-0 text-[18px] text-text">{stepTitle(s)}</h2>
            </div>
            <button type="button" onClick={onPick}
              className="inline-flex shrink-0 items-center gap-1.5 rounded-full border border-border px-3 py-1.5 text-[13px] text-muted hover:border-lime hover:text-text">
              <Replace size={14} aria-hidden /> Change
            </button>
          </div>
          {figure
            ? <FigureView category={s.exercise.category} number={s.exercise.number} title={stepTitle(s)} />
            : <div className="grid h-40 place-items-center rounded-xl border border-dashed border-border text-[13px] text-faint">The figure shows in the picker</div>}
        </>
      ) : (
        <h2 className="m-0 text-[18px] text-text">{stepTitle(s)}</h2>
      )}
      <StepFields s={s} units={units} set={set} exercise={!!s.exercise} />
      {item.kind === 'sets' && (
        <div className="grid gap-3 rounded-xl border border-border p-3">
          <NumberInput label="Sets" value={item.count} min={1} max={99} onChange={(v) => dispatch({ type: 'sets', id: item.id, count: Math.round(v) })} />
          <Choice legend="Rest between sets" value={item.rest ? (item.rest.duration === 'open' ? 'lap' : 'time') : 'none'}
            options={[['time', 'Timed'], ['lap', 'Until Lap'], ['none', 'None']]}
            onChange={(v) => dispatch({ type: 'rest', id: item.id, patch: v === 'none' ? null : v === 'lap' ? { duration: 'open', duration_value: undefined } : { duration: 'time', duration_value: item.rest?.duration === 'time' ? item.rest.duration_value : 60 } })} />
          {item.rest?.duration === 'time' && (
            <TimeInput label="Rest" value={item.rest.duration_value ?? 0} onChange={(v) => dispatch({ type: 'rest', id: item.id, patch: { duration_value: v } })} />
          )}
        </div>
      )}
    </div>
  )
}

/** The intensities a step can have, in words. */
const intensities: [string, string][] = [['active', 'Work'], ['warmup', 'Warm-up'], ['cooldown', 'Cool-down'], ['recovery', 'Recovery'], ['rest', 'Rest'], ['interval', 'Interval']]

function StepFields({ s, units, set, exercise }: { s: WorkoutStep; units: Units; set: (p: Partial<WorkoutStep>) => void; exercise: boolean }) {
  const imperial = units === 'imperial'
  const durations: [string, string][] = exercise
    ? [['reps', 'Reps'], ['time', 'Time'], ['open', 'Until Lap']]
    : [['time', 'Time'], ['distance', 'Distance'], ['open', 'Until Lap'], ['calories', 'Calories'], ['hr_greater_than', 'Until heart rate above'],
      ['hr_less_than', 'Until heart rate below'], ['power_greater_than', 'Until power above'], ['power_less_than', 'Until power below']]
  const v = s.duration_value ?? 0
  const defaults: Record<string, number> = { reps: 10, time: 60, distance: 1000, calories: 50, hr_greater_than: 150, hr_less_than: 120, power_greater_than: 250, power_less_than: 150 }
  const target = s.target && s.target !== 'open' ? s.target : 'none'
  return (
    <div className="grid gap-4">
      {exercise
        ? <Choice legend="Duration" value={s.duration} options={durations}
          onChange={(d) => set({ duration: d, duration_value: d === 'open' ? undefined : d === s.duration ? v : defaults[d] })} />
        : <Select label="Duration" value={s.duration} options={durations}
          onChange={(d) => set({ duration: d, duration_value: d === 'open' ? undefined : defaults[d] })} />}
      {s.duration === 'time' && <TimeInput label="Time" value={v} onChange={(x) => set({ duration_value: x })} />}
      {s.duration === 'reps' && <NumberInput label="Reps" value={v} min={1} max={9999} onChange={(x) => set({ duration_value: Math.round(x) })} />}
      {s.duration === 'distance' && (
        <NumberInput label="Distance" unit={imperial ? 'mi' : 'km'} step={0.01} value={Math.round((v / (imperial ? 1609.344 : 1000)) * 100) / 100}
          onChange={(x) => set({ duration_value: Math.round(x * (imperial ? 1609.344 : 1000)) })} />
      )}
      {s.duration === 'calories' && <NumberInput label="Calories" unit="kcal" value={v} min={1} onChange={(x) => set({ duration_value: Math.round(x) })} />}
      {(s.duration.startsWith('hr_') || s.duration.startsWith('power_')) && (
        <NumberInput label={s.duration.startsWith('hr_') ? 'Heart rate' : 'Power'} unit={s.target_pct ? (s.duration.startsWith('hr_') ? '% max' : '% FTP') : s.duration.startsWith('hr_') ? 'bpm' : 'W'}
          value={v} min={1} onChange={(x) => set({ duration_value: Math.round(x) })} />
      )}
      {s.duration === 'open' && <p className="m-0 -mt-2 text-[13px] text-muted">The step lasts until you press Lap on the watch.</p>}
      {exercise && (
        <NumberInput label="Weight" unit={imperial ? 'lb' : 'kg'} step={0.5} max={imperial ? 1100 : 500}
          value={Math.round(((s.weight_kg ?? 0) / (imperial ? LB : 1)) * 10) / 10}
          onChange={(x) => set({ weight_kg: x > 0 ? Math.round(x * (imperial ? LB : 1) * 100) / 100 : undefined })} hint="0 for body weight" />
      )}
      <Select label="Intensity" value={s.intensity || 'active'} options={intensities} onChange={(i) => set({ intensity: i })} />
      {!exercise && (
        <>
          <Select label="Target" value={target}
            options={[['none', 'None'], ['heart_rate', 'Heart rate'], ['speed', imperial ? 'Pace (per mile)' : 'Pace (per km)'], ['power', 'Power'], ['cadence', 'Cadence']]}
            onChange={(t) => set(t === 'none' ? { target: s.intensity === 'rest' ? 'open' : undefined } : { target: t, target_zone: t === 'heart_rate' || t === 'power' ? 2 : undefined, target_low: t === 'speed' ? 2.5 : t === 'cadence' ? 80 : undefined, target_high: t === 'speed' ? 3 : t === 'cadence' ? 90 : undefined })} />
          {target !== 'none' && <TargetFields s={s} set={set} imperial={imperial} />}
        </>
      )}
      <Labelled label="Notes" hint={`${[...(s.notes ?? '')].length} of 200 characters`}>
        {(id) => <textarea id={id} rows={2} maxLength={200} value={s.notes ?? ''} onChange={(e) => set({ notes: e.target.value || undefined })} className={fieldClass} />}
      </Labelled>
      {!exercise && (
        <Labelled label="Step name" hint="Optional; the watch shows it during the step">
          {(id) => <input id={id} maxLength={40} value={s.name ?? ''} onChange={(e) => set({ name: e.target.value || undefined })} className={fieldClass} />}
        </Labelled>
      )}
    </div>
  )
}

function TargetFields({ s, set, imperial }: { s: WorkoutStep; set: (p: Partial<WorkoutStep>) => void; imperial: boolean }) {
  const zoned = s.target === 'heart_rate' || s.target === 'power'
  const byZone = zoned && (s.target_zone ?? 0) > 0
  const lo = s.target_low ?? 0
  const hi = s.target_high ?? 0
  return (
    <div className="grid gap-3 rounded-xl border border-border p-3">
      {zoned && (
        <Choice legend="Aim for" value={byZone ? 'zone' : 'range'} options={[['zone', 'A zone'], ['range', 'A range']]}
          onChange={(v) => set(v === 'zone' ? { target_zone: 2, target_low: undefined, target_high: undefined }
            : { target_zone: undefined, target_low: s.target === 'heart_rate' ? 130 : 200, target_high: s.target === 'heart_rate' ? 150 : 250 })} />
      )}
      {byZone ? (
        <NumberInput label="Zone" value={s.target_zone ?? 1} min={1} max={s.target === 'heart_rate' ? 5 : 7} onChange={(z) => set({ target_zone: Math.round(z) })} />
      ) : s.target === 'speed' ? (
        <div className="grid grid-cols-2 gap-3">
          {/* The slower pace is the low speed. */}
          <PaceInput label="From" speed={lo} imperial={imperial} onChange={(x) => set({ target_low: x })} />
          <PaceInput label="To" speed={hi} imperial={imperial} onChange={(x) => set({ target_high: x })} />
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-3">
          <NumberInput label="Low" unit={unitOf(s)} value={lo} min={1} onChange={(x) => set({ target_low: Math.round(x) })} />
          <NumberInput label="High" unit={unitOf(s)} value={hi} min={1} onChange={(x) => set({ target_high: Math.round(x) })} />
        </div>
      )}
      {zoned && !byZone && (
        <label className="flex items-center gap-2 text-[13px] text-muted">
          <input type="checkbox" checked={!!s.target_pct} onChange={(e) => set({ target_pct: e.target.checked || undefined })} />
          {s.target === 'heart_rate' ? 'In % of maximum heart rate' : 'In % of FTP'}
        </label>
      )}
    </div>
  )
}

const unitOf = (s: WorkoutStep) =>
  s.target === 'heart_rate' ? (s.target_pct ? '% max' : 'bpm') : s.target === 'power' ? (s.target_pct ? '% FTP' : 'W') : s.target === 'cadence' ? 'rpm' : ''
