// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import type { Device, PairState } from '../api/types'
import { bondText, codeGroups, codeSpoken, endText, firstSyncDone, flagOf, isOver, pairHint, signalBars, signalText, stepOf } from './pairing'

describe('pairing', () => {
  it('maps every state to its step', () => {
    const want: Record<PairState, number | null> = {
      prepare: 0, waiting: 0, finding: 1, pairing: 2, confirm: 2, enter_passkey: 2, show_passkey: 2, identifying: 2,
      setup: 3, done: 4, failed: null, canceled: null, expired: null,
    }
    for (const [state, step] of Object.entries(want)) expect(stepOf({ state: state as PairState })).toBe(step)
    expect(stepOf(null)).toBe(0)
    expect(isOver({ state: 'done' })).toBe(true)
    expect(isOver({ state: 'setup' })).toBe(false)
    expect(isOver(null)).toBe(false)
  })

  it('names signals and flags', () => {
    expect(signalText({ seen_now: true, signal: 'strong' })).toBe('strong signal')
    expect(signalText({ seen_now: false, signal: null })).toBe('not seen now')
    expect(signalBars({ seen_now: true, signal: 'weak' })).toBe(1)
    expect(signalBars({ seen_now: false, signal: 'strong' })).toBe(0)
    expect(flagOf({ owner: 'other', paired_here: true })).toEqual({ text: 'Belongs to another account', tone: 'amber', disabled: true })
    expect(flagOf({ owner: 'yours', paired_here: true }).text).toBe('Already yours · paired with this computer')
    expect(flagOf({ owner: 'new', paired_here: true }).text).toBe('Paired with this computer')
    expect(flagOf({ owner: 'nobody', paired_here: false }).text).toBe('Ready to add')
    expect(flagOf({ owner: 'new', paired_here: false })).toEqual({ text: 'New', tone: 'lime' })
  })

  it('shows codes in groups and digit by digit', () => {
    expect(codeGroups('482917')).toBe('482 917')
    expect(codeGroups('12')).toBe('12')
    expect(codeSpoken('482917')).toBe('4 8 2 9 1 7')
  })

  it('has a hint for every error code', () => {
    for (const c of ['mismatch', 'timeout', 'auth_failed', 'connect_failed', 'gone', 'in_progress', 'adapter_off', 'not_bonded',
      'identify', 'scan', 'busy', 'owned_elsewhere', 'stopping']) {
      expect(pairHint(c), c).not.toBe('')
    }
    expect(pairHint('nonsense')).toBe('')
    expect(pairHint(null)).toBe('')
    expect(endText({ state: 'failed', error_code: 'owned_elsewhere', error: 'x' }).text).toMatch(/another account/)
    expect(endText({ state: 'canceled', error_code: null, error: null }).title).toBe('Cancelled')
    expect(endText({ state: 'canceled', error_code: 'stopping', error: null }).title).toBe('Stopped')
    expect(endText({ state: 'expired', error_code: null, error: 'Nobody looked.' }).text).toBe('Nobody looked.')
  })

  it('says how the watch is paired', () => {
    expect(bondText('existing')).toMatch(/already had/)
    expect(bondText('code')).toMatch(/code confirmed/)
    expect(bondText('no_code')).toMatch(/without a code/)
    expect(bondText(null)).toBe('')
  })

  it('knows when the first sync has finished', () => {
    const now = Math.floor(Date.now() / 1000)
    const v = { started_at: now - 120 }
    const sync = (over: Partial<NonNullable<Device['sync']>>) => ({
      sync: { address: '', running: false, live: false, last_ok: true, pulled: 0, imported: 0, failures: 0, away: false, listed: 0, done: 0, total: 0, ...over },
    })
    expect(firstSyncDone(undefined, v)).toBe(false)
    expect(firstSyncDone(sync({ last_start: now - 3600, last_end: now - 3500 }), v)).toBe(false) // an older sync
    expect(firstSyncDone(sync({ running: true, last_start: now - 30 }), v)).toBe(false)
    expect(firstSyncDone(sync({ last_start: now - 30, last_end: now - 5 }), v)).toBe(true)
    expect(firstSyncDone(sync({ last_ok: false, last_start: now - 30, last_end: now - 5 }), v)).toBe(true)
  })
})
