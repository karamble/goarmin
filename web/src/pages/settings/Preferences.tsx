// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useEffect, useState, type FormEvent } from 'react'
import { ApiError, errorText } from '../../api/client'
import { useMe } from '../../api/hooks/auth'
import { useRecentActivities } from '../../api/hooks/data'
import { usePrefs, useSavePrefs, type Prefs } from '../../api/hooks/prefs'
import { useWatchProfile, useZones } from '../../api/hooks/training'
import { zoneShares } from '../../lib/activity'
import { Button, Card, Choice, Notice } from '../../ui/kit'
import { useToast } from '../../ui/Toast'

/** Preferences: week start, clock, intensity goal and heart-rate zones, each "as the watch"
 * unless the user chooses otherwise. */
export function Preferences() {
  const me = useMe().data!
  const eff = usePrefs()
  const watch = useWatchProfile().data
  const zinfo = useZones().data
  const recent = useRecentActivities(50)
  const save = useSavePrefs()
  const toast = useToast()
  const raw = eff.raw
  const [week, setWeek] = useState(raw.week_start ?? 'monday')
  const [clock, setClock] = useState(raw.clock ?? 'watch')
  const [goal, setGoal] = useState(raw.intensity_goal ? String(raw.intensity_goal) : '')
  const [zsrc, setZsrc] = useState(raw.zones?.source ?? 'watch')
  const [maxHr, setMaxHr] = useState(raw.zones?.max_hr ? String(raw.zones.max_hr) : '')
  const [edges, setEdges] = useState<string[]>(raw.zones?.edges?.map(String) ?? [])
  const [done, setDone] = useState('')
  // Another device saved meanwhile: show its version.
  useEffect(() => {
    setWeek(raw.week_start ?? 'monday')
    setClock(raw.clock ?? 'watch')
    setGoal(raw.intensity_goal ? String(raw.intensity_goal) : '')
    setZsrc(raw.zones?.source ?? 'watch')
    setMaxHr(raw.zones?.max_hr ? String(raw.zones.max_hr) : '')
    setEdges(raw.zones?.edges?.map(String) ?? [])
  }, [raw])
  const watchGoal = watch?.goals?.find((g) => g.type === 'active_minutes' && !g.sport)?.target
  const watchZones = zinfo?.default?.hr
  const estimate = Math.max(0, ...(recent.data?.items ?? []).map((a) => a.max_hr ?? 0))
  const max = Number(maxHr) || 0
  const shareEdges = max ? zoneShares.map((z) => Math.round(z * max)) : []
  const watchUnits = watch?.settings?.units.distance
  const unitsDiffer = watchUnits && (watchUnits === 'statute') !== (me.units === 'imperial')

  const submit = (e: FormEvent) => {
    e.preventDefault()
    setDone('')
    const next: Prefs = { v: 1, onboarding_done: raw.onboarding_done || undefined }
    if (week !== 'monday') next.week_start = week
    if (clock !== 'watch') next.clock = clock
    if (goal) next.intensity_goal = Number(goal)
    if (zsrc !== 'watch') {
      next.zones = { source: zsrc }
      if (max) next.zones.max_hr = max
      const typed = edges.map(Number)
      if (zsrc === 'custom' && typed.length === 5 && typed.every((x) => x > 0)) next.zones.edges = typed
    }
    // A new week start or clock remounts the pages, this form included: the toast outlives it.
    save.mutate({ next, rev: eff.rev }, {
      onSuccess: () => toast({ text: 'Preferences saved' }),
      onError: (err) => setDone(err instanceof ApiError && err.status === 412 ? 'Changed on another device meanwhile; showing that version now.' : ''),
    })
  }

  return (
    <Card className="p-6 sm:p-8 lg:col-span-2">
      <p className="caps mb-5">Preferences</p>
      <form onSubmit={submit} className="grid max-w-3xl gap-6">
        <Choice legend="Week starts on" value={week} onChange={setWeek}
          options={[['monday', 'Monday'], ['sunday', 'Sunday'], ['saturday', 'Saturday']]} />
        <Choice legend="Clock" value={clock} onChange={setClock}
          options={[['watch', `As the watch${eff.watchClock ? ` (${eff.watchClock === '12h' ? '12 h' : '24 h'})` : ''}`], ['browser', 'As this browser'], ['24h', '24 h'], ['12h', '12 h']]} />
        <label className="field-label">
          Weekly intensity goal (minutes)
          <input className="field max-w-[12rem] font-sans text-[15px] normal-case tracking-normal" inputMode="numeric" value={goal}
            placeholder={`as the watch: ${watchGoal ?? 150}`} onChange={(e) => setGoal(e.target.value.replace(/\D/g, ''))} />
          <span className="font-sans text-[13px] normal-case tracking-normal text-faint">Empty follows the watch's goal{watchGoal ? ` (${watchGoal} minutes)` : ''}. Vigorous minutes count twice.</span>
        </label>
        <Choice legend="Heart-rate zones" value={zsrc} onChange={setZsrc}
          options={[['watch', 'From the watch'], ['custom', 'My own'], ['estimate', 'Estimate']]}
          hint={zsrc === 'watch' ? (watchZones ? `The watch's zones: ${watchZones.slice(0, 5).join(' / ')} bpm, maximum ${watchZones[5]}. Activities show the zones the watch counted them with.`
            : 'The watch has not sent its zones yet; until then they are estimated.')
            : zsrc === 'estimate' ? (estimate ? `From your highest heart rate in recent activities: ${estimate} bpm.` : 'No activity with heart rate yet.')
            : 'Your maximum heart rate, and optionally where each zone starts.'} />
        {zsrc === 'estimate' && estimate > 0 && (
          <div><Button type="button" small onClick={() => { setZsrc('custom'); setMaxHr(String(estimate)) }}>Use {estimate} bpm as my maximum</Button></div>
        )}
        {zsrc === 'custom' && (
          <div className="grid gap-3">
            <label className="field-label">
              Maximum heart rate (bpm)
              <input className="field max-w-[12rem] font-sans text-[15px] normal-case tracking-normal" inputMode="numeric" value={maxHr}
                onChange={(e) => setMaxHr(e.target.value.replace(/\D/g, ''))} />
            </label>
            <fieldset className="grid gap-2 border-0 p-0">
              <legend className="field-label mb-1">Zones start at (bpm)</legend>
              <div className="flex flex-wrap gap-2">
                {[0, 1, 2, 3, 4].map((i) => (
                  <label key={i} className="flex items-center gap-2 text-[13px] text-muted">
                    Zone {i + 1}
                    <input className="field w-20 py-2 font-sans text-[14px]" inputMode="numeric" aria-label={`Zone ${i + 1} starts at`}
                      value={edges[i] ?? ''} placeholder={shareEdges[i] ? String(shareEdges[i]) : ''}
                      onChange={(e) => setEdges((x) => { const n = [...x]; n[i] = e.target.value.replace(/\D/g, ''); return n })} />
                  </label>
                ))}
              </div>
              <span className="text-[13px] text-faint">Empty: 50, 60, 70, 80 and 90 % of the maximum.</span>
            </fieldset>
          </div>
        )}
        {unitsDiffer && (
          <Notice tone="info">Your watch shows {watchUnits === 'statute' ? 'miles' : 'kilometres'}; goarmin shows {me.units === 'imperial' ? 'miles' : 'kilometres'} (the units above).</Notice>
        )}
        {save.error && !(save.error instanceof ApiError && save.error.status === 412) && <Notice tone="error">{errorText(save.error)}</Notice>}
        {done && <Notice tone="info">{done}</Notice>}
        <div><Button variant="primary" type="submit" disabled={save.isPending}>Save preferences</Button></div>
      </form>
    </Card>
  )
}
