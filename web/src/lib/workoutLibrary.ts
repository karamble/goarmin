// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Workout, WorkoutOrigin, WorkoutWatch } from '../api/types'

/** sportWords names a workout's sport: its sub sport when it has one ("Pilates"), else its sport. */
export function sportWords(sport: string, sub?: string): string {
  const w = (s: string) => {
    const t = s.replace(/_/g, ' ').replace(/\b(hiit|amrap|emom)\b/gi, (x) => x.toUpperCase())
    return t[0].toUpperCase() + t.slice(1)
  }
  if (sub && sub !== 'generic') return w(sub)
  return sport ? w(sport) : 'Workout'
}

export const originWords: Record<WorkoutOrigin, string> = {
  goarmin: 'Edited in goarmin',
  watch: 'From a watch',
  garmin_connect: 'From Garmin Connect',
}

/** originText says where a workout's versions came from. */
export function originText(origins: WorkoutOrigin[]): string {
  const from = origins.includes('watch') && origins.includes('garmin_connect') ? 'From a watch and Garmin Connect'
    : origins.includes('watch') ? originWords.watch
      : origins.includes('garmin_connect') ? originWords.garmin_connect : ''
  if (!origins.includes('goarmin')) return from
  return from ? `${originWords.goarmin}, ${from[0].toLowerCase()}${from.slice(1)}` : originWords.goarmin
}

/** presenceText says where a workout is on one watch: short for a badge among others (the
 * watch named apart), long on its own. */
export function presenceText(w: WorkoutWatch): { short: string; long: string } {
  if (w.pending === 'send') return { short: 'Goes on at the next sync', long: `Goes on ${w.name} at the next sync` }
  if (w.pending === 'remove') return { short: 'Delete it on the watch', long: `Switched off: delete it on ${w.name}` }
  switch (w.state) {
    case 'on':
      return { short: 'On the watch', long: `On ${w.name}` }
    case 'removed':
      return {
        short: 'Removed from the watch',
        long: `Removed from ${w.name}${w.since ? ` (${new Date(w.since * 1000).toLocaleDateString()})` : ''}`,
      }
    case 'absent':
      return { short: 'Not on the watch', long: `Not on ${w.name}` }
  }
  return { short: 'Not checked yet', long: `${w.name} has not been checked yet` }
}

/** The library's filters: words to search, a sport, an origin, and where a workout is
 * ("on:<device>", "none" for on no watch, "" for anywhere). */
export interface LibraryFilter {
  q: string
  sport: string
  origin: '' | WorkoutOrigin
  where: string
}

/** sportKey is what the sport filter groups by: the words a card shows. */
export const sportKey = (w: Workout) => sportWords(w.sport, w.sub_sport)

/** filterLibrary keeps the workouts that fit every filter; a search needs every word in the
 * workout's name or sport. */
export function filterLibrary(list: Workout[], f: LibraryFilter): Workout[] {
  const words = f.q.toLowerCase().split(/\s+/).filter(Boolean)
  return list.filter((w) => {
    if (f.sport && sportKey(w) !== f.sport) return false
    if (f.origin && !w.origins.includes(f.origin)) return false
    if (f.where === 'none' && w.watches.some((x) => x.state === 'on')) return false
    if (f.where.startsWith('on:') && !w.watches.some((x) => `on:${x.device_id}` === f.where && x.state === 'on')) return false
    const text = `${w.name} ${sportKey(w)} ${w.sport}`.toLowerCase()
    return words.every((x) => text.includes(x))
  })
}

/** A watch's share of the library: how many workouts are on it and were removed from it, and
 * when it last listed them. */
export interface WatchShare {
  device_id: number
  name: string
  on: number
  removed: number
  checked_at?: number
}

/** watchShares sums the library up per watch, in the order the server lists the watches. */
export function watchShares(list: Workout[]): WatchShare[] {
  const out = new Map<number, WatchShare>()
  for (const w of list) {
    for (const x of w.watches) {
      const s = out.get(x.device_id) ?? { device_id: x.device_id, name: x.name, on: 0, removed: 0, checked_at: x.checked_at }
      if (x.state === 'on') s.on++
      if (x.state === 'removed') s.removed++
      out.set(x.device_id, s)
    }
  }
  return [...out.values()]
}
