// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Device } from '../api/types'
import { ago, timeOfDay } from './format'

/** watchName is what the person calls the watch: their nickname, else the model. */
export function watchName(d: Pick<Device, 'nickname' | 'model' | 'name'>): string {
  return d.nickname || d.model || d.name || 'Watch'
}

/** The FIT file types a watch hands over, as people know them (the FIT profile's file types plus
 * Garmin's own health types). */
const fitTypes: Record<number, string> = {
  1: 'Device', 2: 'Settings', 3: 'Sport profile', 4: 'Activity', 5: 'Workout', 6: 'Course', 7: 'Schedule',
  8: 'Locations', 9: 'Weight', 10: 'Totals', 11: 'Goals', 14: 'Blood pressure', 15: 'Monitoring', 20: 'Activity summary',
  28: 'Daily totals', 32: 'Monitoring', 34: 'Segment', 35: 'Segments', 38: 'Sleep', 41: 'Totals', 44: 'Training metrics',
  49: 'Sleep', 61: 'Blood pressure', 68: 'HRV', 70: 'Health', 71: 'Health', 72: 'Daily health', 73: 'Sleep summary',
  74: 'Physiology', 77: 'Garmin Coach', 79: 'Health',
}

export function fitTypeName(t: number): string {
  return t < 0 ? 'Other file' : (fitTypes[t] ?? `Type ${t}`)
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
export function syncLine(d: Device, tz = '', now = Date.now(), serverSyncs = true): { text: string; detail: string; tone: Tone } {
  const s = d.sync
  if (!d.current) return { text: 'Handed over', detail: '', tone: 'idle' }
  if (!s) {
    return {
      text: 'Imported files only',
      detail: serverSyncs ? 'Not paired with this computer. Add it under Add a watch to sync it over Bluetooth.'
        : 'This server does not sync watches over Bluetooth.',
      tone: 'idle',
    }
  }
  const next = s.next_sync ? `next ${timeOfDay(s.next_sync, tz)}` : ''
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
  if (s.last_ok && s.last_end) return { text: `Synced ${ago(s.last_end, now)}`, detail: next, tone: 'ok' }
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
    if (!d.current || !s || d.paused) continue
    if (s.failures >= 3) {
      out.push({ device: d, text: `${watchName(d)}: ${s.failures} syncs failed in a row. ${errorHint(s.error_code)}` })
    } else if (s.last_good && now / 1000 - s.last_good > 86400) {
      out.push({ device: d, text: `${watchName(d)} has not synced for ${ago(s.last_good, now).replace(' ago', '')}.` })
    }
  }
  return out
}
