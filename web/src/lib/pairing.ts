// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Device, PairComputer, PairState, PairView, PairWatch } from '../api/types'

/** computer names the computer that pairs in a sentence: a sync node by its name, else "this
 * computer" (the server pairs itself). */
export function computer(name?: string | null): string {
  return name || 'this computer'
}

/** defaultComputer is the one Prepare offers first: this server, else a node with Bluetooth on. */
export function defaultComputer(cs: PairComputer[]): PairComputer | undefined {
  return cs.find((c) => c.here) ?? cs.find((c) => c.adapter?.power_state === 'on') ?? cs[0]
}

/** computerLabel is a computer in the Pair with choice. */
export function computerLabel(c: PairComputer): string {
  return c.here ? 'This server' : c.name
}

/** The wizard's steps, in order. */
export const pairSteps = ['Prepare', 'Find', 'Confirm', 'Set up', 'First sync'] as const
export type PairStep = (typeof pairSteps)[number]

/** stepOf is the step a session is at (0-based); null means it has ended (failed, canceled, expired). */
export function stepOf(v: Pick<PairView, 'state'> | null | undefined): number | null {
  if (!v) return 0
  switch (v.state) {
    case 'prepare':
    case 'waiting':
      return 0
    case 'finding':
      return 1
    // Pairing shows on the Confirm step: it comes before the code and again right after the
    // answer, so the page never jumps back to the list (a failure does, with its message).
    case 'pairing':
    case 'confirm':
    case 'enter_passkey':
    case 'show_passkey':
    case 'identifying':
      return 2
    case 'setup':
      return 3
    case 'done':
      return 4
  }
  return null
}

const ended: PairState[] = ['done', 'failed', 'canceled', 'expired']

/** isOver tells whether a session runs no more (done counts: nothing is left to drive). */
export function isOver(v: Pick<PairView, 'state'> | null | undefined): boolean {
  return !!v && ended.includes(v.state)
}

/** signalText is a listed watch's signal for people. */
export function signalText(w: Pick<PairWatch, 'seen_now' | 'signal'>): string {
  if (!w.seen_now || !w.signal) return 'not seen now'
  return { strong: 'strong signal', ok: 'good signal', weak: 'weak signal' }[w.signal]
}

/** signalBars is how many of three bars the signal fills. */
export function signalBars(w: Pick<PairWatch, 'seen_now' | 'signal'>): number {
  if (!w.seen_now || !w.signal) return 0
  return { strong: 3, ok: 2, weak: 1 }[w.signal]
}

export interface Flag {
  text: string
  tone: 'lime' | 'muted' | 'amber'
  /** The watch cannot be chosen. */
  disabled?: boolean
}

/** flagOf says what a listed watch is to the person adding it. */
export function flagOf(w: Pick<PairWatch, 'owner' | 'paired_here'>, name?: string | null): Flag {
  if (w.owner === 'other') return { text: 'Belongs to another account', tone: 'amber', disabled: true }
  if (w.owner === 'yours') return { text: w.paired_here ? `Already yours · paired with ${computer(name)}` : 'Already yours', tone: 'muted' }
  if (w.paired_here) return { text: `Paired with ${computer(name)}`, tone: 'muted' }
  if (w.owner === 'nobody') return { text: 'Ready to add', tone: 'lime' }
  return { text: 'New', tone: 'lime' }
}

/** codeGroups shows a six-digit code as two groups of three ("482 917"). */
export function codeGroups(code: string): string {
  return code.length === 6 ? `${code.slice(0, 3)} ${code.slice(3)}` : code
}

/** codeSpoken is the code for a screen reader, digit by digit. */
export function codeSpoken(code: string): string {
  return code.split('').join(' ')
}

/** pairHint is what a pairing error means and what to do next. */
export function pairHint(code: string | null | undefined, name?: string | null): string {
  const here = computer(name)
  switch (code) {
    case 'mismatch':
      return 'The codes did not match, so the pairing was refused. Choose the watch again; if the codes differ again, it may be another watch nearby.'
    case 'timeout':
      return 'Nobody confirmed the code in time. Choose the watch again and confirm on both sides within 30 seconds.'
    case 'auth_failed':
      return 'The watch refused the pairing or gave up. Open Pair Phone on the watch again, then choose it here.'
    case 'connect_failed':
      return `goarmin could not connect to the watch. Keep it close to ${here} and try again.`
    case 'gone':
      return 'The watch is no longer visible. Open Pair Phone on the watch again; it shows up in the list by itself.'
    case 'in_progress':
      return 'Another pairing with this watch is running. Wait a moment and try again.'
    case 'adapter_off':
      return `Bluetooth is off on ${here}. Turn it on, then scan again.`
    case 'not_bonded':
      return 'The watch paired but kept no pairing. Choose it again with Pair again.'
    case 'identify':
      return 'The watch is paired, but goarmin could not talk to it yet. Choose it again to retry.'
    case 'scan':
      return `Searching for watches failed. Scan again; if it keeps failing, check Bluetooth on ${here}.`
    case 'busy':
      return 'Another goarmin command is using Bluetooth. Try again when it has finished.'
    case 'owned_elsewhere':
      return 'This watch belongs to another account on this server. Its owner can hand it over under Devices › the watch › Remove; then it can be added here.'
    case 'stopping':
      return name ? `The sync node on ${name} is stopping. Start again once it runs (make node-start).` : 'The server is stopping. Start again once it is back.'
    case 'node_offline':
      return `${name ? `${name}, the computer that pairs,` : 'The computer that pairs'} went offline. Start again once its sync node runs (make node-start).`
    case 'node_gone':
      return `${name ? `${name}, the computer that pairs,` : 'The computer that pairs'} no longer has this attempt (it restarted, or was signed out). Start again.`
  }
  return ''
}

/** endTitle and endText describe a session that ended without adding a watch. */
export function endText(v: Pick<PairView, 'state' | 'error_code' | 'error'> & { node?: PairView['node'] }): { title: string; text: string } {
  switch (v.state) {
    case 'canceled':
      return v.error_code === 'stopping'
        ? { title: 'Stopped', text: pairHint('stopping', v.node?.name) }
        : { title: 'Cancelled', text: 'Adding the watch was cancelled. If it was paired already, the next try skips the code.' }
    case 'expired':
      return { title: 'Timed out', text: v.error || 'This took too long, so it stopped. Start again when the watch is ready.' }
  }
  return { title: 'Not added', text: pairHint(v.error_code, v.node?.name) || v.error || 'The watch could not be added.' }
}

/** firstSyncDone tells whether the first sync after the setup has finished (it started after the session did). */
export function firstSyncDone(d: Pick<Device, 'sync'> | undefined, v: Pick<PairView, 'started_at'>): boolean {
  const s = d?.sync
  return !!s && !s.running && !!s.last_start && s.last_start >= v.started_at && !!s.last_end && s.last_end >= s.last_start
}

/** bondText says how the watch came to be paired. */
export function bondText(bond: PairView['bond'], name?: string | null): string {
  switch (bond) {
    case 'existing':
      return `Uses the pairing ${computer(name)} already had with the watch; no new pairing was needed.`
    case 'code':
      return 'Paired just now, with the code confirmed on both sides.'
    case 'no_code':
      return `Paired just now without a code: the watch did not ask for one (it may have known ${computer(name)} already).`
  }
  return ''
}
