// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useState } from 'react'
import { useAccountDocument, useAccountDocuments } from '../../api/hooks/readers'
import type { AccountDocumentRow } from '../../api/types'
import { DataView, words } from '../../ui/DataView'
import { Skeleton } from '../../ui/kit'

/** The groups the account's records fall into, by kind (gcexport.Kind). */
const groups: { title: string; kinds: string[] }[] = [
  { title: 'Profile and settings', kinds: ['profile', 'settings', 'social_profile', 'goals', 'reminders', 'suica', 'connect_iq', 'livetrack', 'profile_image'] },
  { title: 'Health and zones', kinds: ['hr_zones', 'power_zones', 'biometrics_latest', 'biometrics_profile'] },
  { title: 'Training', kinds: ['training_plan', 'weekly_report', 'workouts', 'courses', 'gear', 'personal_records', 'golf_clubs', 'golf_club_types'] },
  { title: 'Devices', kinds: ['devices', 'device_backups', 'device_backup_file'] },
  { title: 'Orders, consents and account events', kinds: ['orders', 'consents', 'customer', 'events'] },
  { title: 'inReach', kinds: ['inreach_billing', 'inreach_trials', 'inreach_messages', 'inreach_addressbook', 'inreach_presets', 'inreach_routes', 'inreach_waypoints'] },
  { title: 'Social and media', kinds: ['comments', 'likes', 'media'] },
]

const titles: Record<string, string> = {
  profile: 'Profile', settings: 'Settings', social_profile: 'Social profile', goals: 'Goals', reminders: 'Reminders', suica: 'Suica',
  connect_iq: 'Connect IQ', livetrack: 'LiveTrack settings', profile_image: 'Profile picture', hr_zones: 'Heart-rate zones',
  power_zones: 'Power zones', biometrics_latest: 'Latest thresholds', biometrics_profile: 'Biometric profile', training_plan: 'Training plans',
  weekly_report: 'Weekly training report', workouts: 'Workouts in Garmin Connect', courses: 'Courses in Garmin Connect', gear: 'Gear',
  personal_records: 'Personal records', golf_clubs: 'Golf clubs', golf_club_types: 'Golf club types', devices: 'Devices and content',
  device_backups: 'Device backups', device_backup_file: 'Device backup file', orders: 'Orders', consents: 'Consent history',
  customer: 'Customer record', events: 'Account events', inreach_billing: 'Billing', inreach_trials: 'Free trials',
  inreach_messages: 'Messages', inreach_addressbook: 'Address book', inreach_presets: 'Preset messages', inreach_routes: 'Routes (GPX)',
  inreach_waypoints: 'Waypoints (KMZ)', comments: 'Comments', likes: 'Likes', media: 'Media',
}

/** AccountData shows the account's records from a Garmin Connect export, readably: profile,
 * settings, zones, orders, consents, inReach, social. They are only ever shown to their owner. */
export default function AccountData() {
  const q = useAccountDocuments()
  const [open, setOpen] = useState<number | null>(null)
  if (q.isPending) return <Skeleton className="h-48 rounded-[18px]" />
  const docs = q.data ?? []
  if (!docs.length) {
    return (
      <div className="card px-6 py-10 text-center text-[14px] text-muted">
        Nothing here yet. Import your Garmin Connect export under Devices › Import: its account records (profile, settings,
        orders, consents, inReach and more) appear here, readable, and only for you.
      </div>
    )
  }
  const used = new Set<number>()
  const sections = [...groups, { title: 'Other files', kinds: [] as string[] }].map((g) => {
    const rows = docs.filter((d) => !used.has(d.id) && (g.kinds.includes(d.kind) || g.kinds.length === 0))
    rows.forEach((d) => used.add(d.id))
    return { ...g, rows }
  }).filter((g) => g.rows.length)
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
      <p className="m-0 text-[13px] text-muted">From your Garmin Connect export, as it is. goarmin keeps it on this server only and shows it to nobody but you.</p>
      {sections.map((g) => (
        <section key={g.title} className="card p-5" aria-labelledby={`doc-${g.title}`}>
          <h2 id={`doc-${g.title}`} className="caps m-0 mb-3">{g.title}</h2>
          <ul className="m-0 grid list-none gap-2 p-0">
            {g.rows.map((d) => <Doc key={d.id} d={d} open={open === d.id} onToggle={() => setOpen(open === d.id ? null : d.id)} />)}
          </ul>
        </section>
      ))}
    </div>
  )
}

/** shortName drops what Garmin puts before a file's own name: the account's e-mail address. */
export function shortName(name: string): string {
  return name.replace(/^[^@/]+@[^_\-/]+?[_-](?=[A-Za-z0-9])/, '')
}

function Doc({ d, open, onToggle }: { d: AccountDocumentRow; open: boolean; onToggle: () => void }) {
  const q = useAccountDocument(open && d.kind !== 'profile_image' ? d.id : null)
  return (
    <li className="rounded-xl border border-border">
      <button type="button" aria-expanded={open} onClick={onToggle} className="flex w-full items-center gap-3 px-4 py-3 text-left">
        <span className="flex-1 text-[14px]">{titles[d.kind] ?? words(d.kind)}</span>
        <span className="truncate font-mono text-[11px] text-faint">{shortName(d.name)}</span>
      </button>
      {open && (
        <div className="min-w-0 border-t border-border px-4 py-3">
          {d.kind === 'profile_image' ? (
            <img src={`/api/files/${d.id}/download`} alt="Profile picture from Garmin Connect" className="max-h-40 rounded-xl" />
          ) : q.isPending ? <Skeleton className="h-20" /> : <DataView value={q.data?.data} />}
        </div>
      )}
    </li>
  )
}
