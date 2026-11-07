// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo, useState, type FormEvent } from 'react'
import { errorText } from '../../api/client'
import { useMe, useUpdateProfile } from '../../api/hooks/auth'
import { Button, Card, Field, Notice } from '../../ui/kit'
import { Preferences } from './Preferences'

function zones(): string[] {
  try {
    return (Intl as unknown as { supportedValuesOf(k: string): string[] }).supportedValuesOf('timeZone')
  } catch {
    return []
  }
}

export default function Profile() {
  const me = useMe().data!
  const update = useUpdateProfile()
  const [name, setName] = useState(me.display_name)
  const [tz, setTz] = useState(me.timezone)
  const [units, setUnits] = useState(me.units)
  const browserTz = Intl.DateTimeFormat().resolvedOptions().timeZone
  const all = useMemo(zones, [])
  const submit = (e: FormEvent) => {
    e.preventDefault()
    update.mutate({ display_name: name, timezone: tz, units })
  }
  return (
    <div className="grid gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
      <Card className="p-6 sm:p-8">
        <form onSubmit={submit} className="grid max-w-lg gap-5">
          <Field label="Name" value={name} onChange={(e) => setName(e.target.value)} />
          <Field label="Email" value={me.email} readOnly hint="Your sign-in name; it cannot be changed here." />
          <label className="field-label">
            Time zone
            <select className="field normal-case tracking-normal text-[15px] font-sans" value={tz} onChange={(e) => setTz(e.target.value)}>
              <option value="">The server's time zone</option>
              {all.map((z) => <option key={z} value={z}>{z}</option>)}
            </select>
            <span className="normal-case tracking-normal font-sans text-[13px] text-faint">
              Days and times are shown in this zone{browserTz ? ` (this browser: ${browserTz})` : ''}. Stored days keep the zone the watch had when it recorded them.
            </span>
          </label>
          <fieldset className="grid gap-2 border-0 p-0">
            <legend className="field-label mb-2">Units</legend>
            <div className="flex gap-2">
              {(['metric', 'imperial'] as const).map((u) => (
                <button key={u} type="button" onClick={() => setUnits(u)} aria-pressed={units === u}
                  className={`rounded-full border px-4 py-2 text-sm ${units === u ? 'border-lime-dim bg-lime-fill text-lime' : 'border-border text-muted hover:text-text'}`}>
                  {u === 'metric' ? 'Metric (km, kg)' : 'Imperial (mi, lb)'}
                </button>
              ))}
            </div>
          </fieldset>
          {update.error && <Notice tone="error">{errorText(update.error)}</Notice>}
          {update.isSuccess && <Notice tone="ok">Saved.</Notice>}
          <div><Button variant="primary" type="submit" disabled={update.isPending}>Save</Button></div>
        </form>
      </Card>
      <Card className="p-6 sm:p-8">
        <p className="caps mb-4">This server</p>
        <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-6 gap-y-3 text-sm">
          <dt className="text-muted">Version</dt><dd className="num m-0">{me.server.version || 'dev'}</dd>
          <dt className="text-muted">Watch sync</dt><dd className="m-0">{me.server.sync ? 'on this server' : 'not here (no live view)'}</dd>
          <dt className="text-muted">Map tiles</dt><dd className="m-0">{me.server.tiles ? 'available' : 'not set up (routes without a map)'}</dd>
        </dl>
      </Card>
      <Preferences />
    </div>
  )
}
