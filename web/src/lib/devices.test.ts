// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import type { Device, SyncStatus } from '../api/types'
import { alerts, fitTypeName, subtitle, syncLine, watchName } from './devices'

const now = Date.now()
const sec = (d: number) => Math.floor((now + d) / 1000)

function dev(sync: Partial<SyncStatus> | null, over: Partial<Device> = {}): Device {
  return {
    id: 1, unit_id: 7, product: 4536, kind: 'watch', model: 'Fenix 8', name: 'fenix 8 - 47mm', nickname: '', firmware: '23.40',
    address: 'C0:FF:EE:12:34:56', after_fetch: 'keep', paused: false, usb_auto: true, delete_support: '', battery: null,
    current: true, owned_from: 0, owned_to: null,
    sync: sync === null ? undefined : { address: 'A0', running: false, live: false, last_ok: false, pulled: 0, imported: 0,
      failures: 0, away: false, listed: 0, done: 0, total: 0, ...sync },
    ...over,
  }
}

describe('device lines', () => {
  it('names a watch by nickname, then model', () => {
    expect(watchName(dev(null))).toBe('Fenix 8')
    expect(watchName(dev(null, { nickname: 'Daily' }))).toBe('Daily')
    expect(fitTypeName(4)).toBe('Activity')
    expect(fitTypeName(999)).toBe('Type 999')
    expect(fitTypeName(73)).toBe('Skin temperature')
    expect(fitTypeName(61)).toBe('ECG')
    expect(subtitle(dev(null))).toBe('fenix 8 - 47mm')
    expect(subtitle(dev(null, { nickname: 'Daily' }))).toBe('Fenix 8 · fenix 8 - 47mm')
    expect(subtitle(dev(null, { name: '', model: 'Instinct 2' }))).toBe('Unit 7')
  })

  it('says what a watch is doing', () => {
    expect(syncLine(dev({ running: true, phase: 'fetching', done: 3, total: 9 })).text).toBe('Fetching 3 of 9 files')
    expect(syncLine(dev({ away: true, failures: 0 })).tone).toBe('away')
    expect(syncLine(dev({ failures: 2, error_code: 'timeout' })).text).toBe('Sync failed 2× in a row')
    expect(syncLine(dev({ last_ok: true, last_end: sec(-120_000) }), '', now).text).toBe('Synced 2 min ago')
    expect(syncLine(dev({ last_ok: true }, { paused: true })).tone).toBe('paused')
    expect(syncLine(dev(null)).detail).toMatch(/Not paired with this computer/)
    expect(syncLine(dev(null), '', now, false).detail).toMatch(/does not sync watches/)
    expect(syncLine(dev(null, { current: false })).text).toBe('Handed over')
    // Through a sync node: its name when all is well, offline when it went silent.
    expect(syncLine(dev({ last_ok: true, last_end: sec(-120_000), node: 'Laptop' }), '', now).detail).toMatch(/via Laptop/)
    const off = syncLine(dev({ last_ok: true, node: 'Laptop', offline: true, last_seen: sec(-600_000) }), '', now)
    expect(off.text).toBe('Sync node offline')
    expect(off.tone).toBe('fail')
    expect(off.detail).toMatch(/Laptop .*10 min ago/)
  })

  it('alerts on real trouble only', () => {
    expect(alerts([dev({ away: true, failures: 0, last_good: sec(-3600_000) })], now)).toHaveLength(0)
    expect(alerts([dev({ failures: 3 })], now)).toHaveLength(1)
    expect(alerts([dev({ away: true, last_good: sec(-30 * 3600_000) })], now)).toHaveLength(1)
    expect(alerts([dev({ failures: 5 }, { paused: true })], now)).toHaveLength(0)
  })
})
