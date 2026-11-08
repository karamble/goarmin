// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Device, DeviceKind, Me } from '../api/types'
import { ago, timeOfDay } from './format'
import { fitTypeTitles } from './fitTypes.gen'

/** deviceName is what the person calls the device: their nickname, else the model. */
export function deviceName(d: Pick<Device, 'nickname' | 'model' | 'name'> & { kind?: DeviceKind }): string {
  return d.nickname || d.model || d.name || kindWord(d.kind ?? 'watch', true)
}

/** watchName is deviceName (most devices are watches). */
export const watchName = deviceName

/** isWatch: only a watch syncs over Bluetooth, streams live values and keeps files to fetch. */
export function isWatch(d: Pick<Device, 'kind'>): boolean {
  return (d.kind ?? 'watch') === 'watch'
}

const kindWords: Record<DeviceKind, [string, string]> = {
  watch: ['watch', 'Watches'],
  scale: ['scale', 'Scales'],
  bike_computer: ['bike computer', 'Bike computers'],
  sensor: ['sensor', 'Sensors'],
  handheld: ['handheld', 'Handhelds'],
  app: ['app', 'Apps'],
  other: ['device', 'Other devices'],
}

/** kindWord is a kind in words: "scale", or with capital "Scale". */
export function kindWord(k: DeviceKind, capital = false): string {
  const w = (kindWords[k] ?? kindWords.other)[0]
  return capital ? w[0].toUpperCase() + w.slice(1) : w
}

/** The order the Devices page shows its groups in; apps join Other devices. */
const groupOrder: DeviceKind[] = ['watch', 'scale', 'bike_computer', 'sensor', 'handheld', 'other']

/** groupDevices groups devices by kind, in groupOrder, leaving out empty groups. */
export function groupDevices(list: Device[]): { kind: DeviceKind; title: string; devices: Device[] }[] {
  const by = new Map<DeviceKind, Device[]>()
  for (const d of list) {
    const k: DeviceKind = d.kind === 'app' || !groupOrder.includes(d.kind) ? 'other' : d.kind
    by.set(k, [...(by.get(k) ?? []), d])
  }
  return groupOrder.filter((k) => by.has(k)).map((k) => ({ kind: k, title: kindWords[k][1], devices: by.get(k)! }))
}

/** The FIT file types a watch hands over, as people know them (the FIT profile's file types plus
 * Garmin's own health types). */

/** fitTypeName is a FIT file type in words, from the generated FIT profile (pkg/fitprofile). */
export function fitTypeName(t: number): string {
  return t < 0 ? 'Other file' : (fitTypeTitles[t] ?? `Type ${t}`)
}

/** What a failed sync's code means, in words, and what happens next. */
export function errorHint(code: string | undefined): string {
  switch (code) {
    case 'not_in_range':
      return 'The watch was out of range or its Bluetooth was off. goarmin tries again on its own.'
    case 'not_bonded':
      return 'This computer is no longer paired with the watch. Add it again under Add a watch.'
    case 'adapter_off':
      return 'Bluetooth is off on this computer.'
    case 'timeout':
      return 'The watch stopped answering during the sync. goarmin tries again on its own.'
    case 'no_owner':
      return 'Nobody owns this watch on this server; it waits until someone adds it.'
    case 'import':
      return 'The files arrived, but not all could be imported. They stay in the raw store.'
    case 'transfer':
      return 'The connection broke off during the sync. goarmin tries again on its own.'
    default:
      return ''
  }
}

export type Tone = 'running' | 'ok' | 'away' | 'fail' | 'paused' | 'idle'

/** subtitle is the line under a watch's name: the model and its own name where they add something. */
export function subtitle(d: Pick<Device, 'nickname' | 'model' | 'name' | 'unit_id'>): string {
  const shown = watchName(d).toLowerCase()
  const parts = [d.model, d.name].filter((x, i, a) => x && x.toLowerCase() !== shown && a.indexOf(x) === i)
  return parts.length ? parts.join(' · ') : `Unit ${d.unit_id}`
}

/** syncLine is a watch's state in one line, with its tone (the dot's colour). serverSyncs says
 * whether this server syncs watches over Bluetooth at all. */
/** canPair: Add a watch works on this server (it pairs watches itself, not only through a sync node). */
export function canPair(me: Me): boolean {
  return me.server.pair ?? me.server.sync
}

export function syncLine(d: Device, tz = '', now = Date.now(), serverSyncs = true): { text: string; detail: string; tone: Tone } {
  const s = d.sync
  if (!d.current) return { text: 'Handed over', detail: '', tone: 'idle' }
  if (!isWatch(d)) {
    return {
      text: 'From imports',
      detail: d.kind === 'scale' ? 'The scale sends its weigh-ins to Garmin Connect; they come in with your Garmin Connect export.'
        : `goarmin reads this ${kindWord(d.kind)}'s files from imports.`,
      tone: 'idle',
    }
  }
  if (!s) {
    return {
      text: 'Imported files only',
      detail: serverSyncs ? 'Not paired with this computer. Add it under Add a watch to sync it over Bluetooth.'
        : 'This server does not sync watches over Bluetooth.',
      tone: 'idle',
    }
  }
  const next = s.next_sync ? `next ${timeOfDay(s.next_sync, tz)}` : ''
  if (s.offline) {
    const seen = s.last_seen ? `, last heard from ${ago(s.last_seen, now)}` : ''
    return { text: 'Sync node offline', detail: `${s.node ?? 'The computer that syncs this watch'} is not running or not reachable${seen}. The watch keeps its files until it is back.`, tone: 'fail' }
  }
  if (s.running) {
    const phase = s.phase === 'listing' ? 'Reading the file list…'
      : s.phase === 'fetching' && s.total > 0 ? `Fetching ${s.done} of ${s.total} files`
      : s.phase === 'importing' ? 'Importing…' : 'Connecting…'
    return { text: phase, detail: '', tone: 'running' }
  }
  if (d.paused) return { text: 'Scheduled syncs paused', detail: 'Sync now still works.', tone: 'paused' }
  if (s.away) {
    const since = s.away_since ? ` since ${timeOfDay(s.away_since, tz)}` : ''
    return { text: `Not in range${since}`, detail: next ? `Trying again, ${next}` : '', tone: 'away' }
  }
  if (s.failures > 0) {
    return { text: `Sync failed${s.failures > 1 ? ` ${s.failures}× in a row` : ''}`, detail: [errorHint(s.error_code), next].filter(Boolean).join(' '), tone: 'fail' }
  }
  if (s.last_ok && s.last_end) return { text: `Synced ${ago(s.last_end, now)}`, detail: [next, s.node ? `via ${s.node}` : ''].filter(Boolean).join(' · '), tone: 'ok' }
  if (s.error_code === 'no_owner') return { text: 'Waiting for an owner', detail: errorHint('no_owner'), tone: 'idle' }
  return { text: 'Not synced yet', detail: next, tone: 'idle' }
}

export const toneDot: Record<Tone, string> = {
  running: 'bg-lime animate-pulse',
  ok: 'bg-lime',
  away: 'bg-faint',
  fail: 'bg-amber',
  paused: 'bg-grey',
  idle: 'bg-faint',
}

/** alerts are the watches that need attention: no good sync for a day, or real failures in a row. */
export function alerts(list: Device[], now = Date.now()): { device: Device; text: string }[] {
  const out: { device: Device; text: string }[] = []
  for (const d of list) {
    const s = d.sync
    if (!d.current || !s || d.paused || !isWatch(d)) continue
    if (s.failures >= 3) {
      out.push({ device: d, text: `${watchName(d)}: ${s.failures} syncs failed in a row. ${errorHint(s.error_code)}` })
    } else if (s.last_good && now / 1000 - s.last_good > 86400) {
      out.push({ device: d, text: `${watchName(d)} has not synced for ${ago(s.last_good, now).replace(' ago', '')}.` })
    }
  }
  return out
}
