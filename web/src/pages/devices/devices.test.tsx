// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { useState, type ReactNode } from 'react'
import { Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Device, LogRun, WatchFile } from '../../api/types'
import { HeaderSlotContext } from '../../app/section/HeaderSlot'
import { fixtureMe, json, renderWith, stubApi } from '../../test/fixtures'
import { ToastProvider } from '../../ui/Toast'
import SyncLog from './SyncLog'
import Watch from './Watch'
import Watches from './Watches'

afterEach(() => vi.unstubAllGlobals())

const now = Math.floor(Date.now() / 1000)
const me = fixtureMe({ server: { version: 'dev', sync: true, tiles: false } })

function device(over: Partial<Device> = {}): Device {
  return {
    id: 1, unit_id: 3912345678, product: 4536, model: 'Fenix 8', name: 'fenix 8 - 47mm', nickname: '', firmware: '23.40',
    address: 'C0:FF:EE:12:34:56', after_fetch: 'keep', paused: false, delete_support: '', battery: 76, battery_at: now - 600,
    current: true, owned_from: 0, owned_to: null,
    sync: { address: 'C0:FF:EE:12:34:56', running: false, live: false, last_ok: false, pulled: 0, imported: 0, failures: 0,
      away: true, away_since: now - 3600, next_sync: now + 120, last_good: now - 7200, listed: 0, done: 0, total: 0, error_code: 'not_in_range' },
    ...over,
  }
}

function run(id: number, over: Partial<LogRun> = {}): LogRun {
  return { id, device_id: 1, started_by: 'schedule', started_at: now - id * 600, finished_at: now - id * 600 + 40, ok: false,
    listed: 0, failed: 0, pulled: 0, archived: 0, deleted: 0, bytes: 0, imported: 0, error: 'gattlink: not in range',
    error_code: 'not_in_range', ...over }
}

/** Page renders a page with the header slot and toasts it needs. */
function Page({ children }: { children: ReactNode }) {
  const [slot, setSlot] = useState<HTMLDivElement | null>(null)
  return (
    <ToastProvider>
      <div ref={setSlot} data-testid="header" />
      <HeaderSlotContext.Provider value={slot}>{children}</HeaderSlotContext.Provider>
    </ToastProvider>
  )
}

/** api answers the device routes; writes are recorded. */
function api(devices: Device[], extra: Record<string, (u: URL) => Response> = {}) {
  const writes: { method: string; path: string; body: unknown }[] = []
  const fn = stubApi({
    '/api/devices': json(devices),
    '/api/bluetooth': json({ available: true, adapter: 'hci0', address: 'AA:BB:CC:00:11:22', powered: false, power_state: 'off' }),
    '/api/sync-runs': json({ items: [] }),
    '/api/devices/1/summary': json({ files: 3, bytes: 4096, by_type: { 32: 2, 4: 1 }, activities: 1, nights: 1, days: 2,
      first_day: '', last_day: '', first_at: 0, last_at: 0 }),
    ...extra,
  })
  const base = fn.getMockImplementation() as (url: string, init?: RequestInit) => Promise<Response>
  fn.mockImplementation(async (url: string, init?: RequestInit) => {
    if (init?.method && init.method !== 'GET') {
      writes.push({ method: init.method, path: new URL(url, 'http://localhost').pathname, body: init.body ? JSON.parse(String(init.body)) : undefined })
      return json(devices[0])()
    }
    return base(url, init)
  })
  return writes
}

describe('Watches', () => {
  it('shows each watch, Bluetooth, and watches you had', async () => {
    const writes = api([device(), device({ id: 2, nickname: 'Old one', current: false, owned_to: now - 86400 * 40, sync: undefined })])
    renderWith(<Page><Watches /></Page>, { me })
    await screen.findByRole('heading', { name: 'Fenix 8' })
    expect(screen.getByText(/Not in range since/)).toBeTruthy()
    expect(screen.getByRole('img', { name: 'Battery 76 percent' })).toBeTruthy()
    expect(screen.getByText('Watches you had before (1)')).toBeTruthy()
    // Not in range is not trouble: no alert.
    expect(screen.queryByRole('alert')).toBeNull()

    fireEvent.click(await screen.findByRole('button', { name: 'Turn on' }))
    fireEvent.click(screen.getByRole('button', { name: /Sync now/ }))
    await waitFor(() => expect(writes.map((w) => `${w.method} ${w.path}`)).toEqual(['POST /api/bluetooth/power', 'POST /api/devices/1/sync']))
    expect(writes[0].body).toEqual({ on: true })
  })

  it('raises an alert after real failures', async () => {
    api([device({ sync: { ...device().sync!, away: false, failures: 3, error_code: 'not_bonded' } })])
    renderWith(<Page><Watches /></Page>, { me })
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toMatch(/3 syncs failed in a row.*no longer paired/)
  })
})

describe('Watch', () => {
  const at = (path: string, m = me) =>
    renderWith(<Page><Routes><Route path="/devices/watches/:id" element={<Watch />} /></Routes></Page>, { path, me: m })

  it('renames the watch and asks before deleting activities', async () => {
    const writes = api([device()])
    at('/devices/watches/1?tab=settings')
    const name = await screen.findByRole('textbox', { name: 'What you call it' })
    fireEvent.change(name, { target: { value: 'Daily' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(writes[0]).toEqual({ method: 'PATCH', path: '/api/devices/1', body: { nickname: 'Daily' } }))

    fireEvent.click(screen.getByRole('radio', { name: /delete activities/ }))
    const dialog = await screen.findByRole('alertdialog')
    expect(writes).toHaveLength(1)
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete activities after fetching' }))
    await waitFor(() => expect(writes[1].body).toEqual({ after_fetch: 'archive_delete' }))
  })

  it("shows what the watch keeps: its settings, goals and bikes", async () => {
    api([device()], {
      '/api/me/watch-profile': json({
        recorded_at: Math.floor(Date.now() / 1000) - 3600, device_id: 1,
        settings: {
          clock: '24h', date_mode: 'day_month', units: { distance: 'metric' }, resting_hr: 52, wake_s: 7 * 3600, sleep_s: 23 * 3600, run_step_m: 1.12,
          zones: [{ max_hr: 183, threshold_hr: 161, ftp: 255, hr_calc: 'percent_max_hr', hr: [92, 110, 128, 146, 165, 183] }],
          bikes: [{ name: 'Road bike', odometer_m: 4821300, enabled: true, weight_kg: 8.4, sensors: ['power'] }],
        },
        goals: [{ type: 'active_minutes', sport: '', target: 150, unit: 'min' }, { type: 'ascent', sport: '', target: 30, unit: 'm' }],
      }),
    })
    at('/devices/watches/1?tab=settings')
    expect(await screen.findByText('From the watch')).toBeTruthy()
    expect(screen.getByText('24-hour')).toBeTruthy()
    expect(screen.getByText('92 / 110 / 128 / 146 / 165 bpm, maximum 183, threshold 161')).toBeTruthy()
    expect(screen.getByText('23:00 – 07:00')).toBeTruthy()
    expect(screen.getByText('150 intensity minutes a week')).toBeTruthy()
    expect(screen.getByText('10 floors a day (30 m)')).toBeTruthy()
    expect(screen.getByText('Road bike')).toBeTruthy()
    expect(screen.getByText(/^4,?821(\.\d+)? km$/)).toBeTruthy()
  })

  it('deletes chosen files from the watch', async () => {
    const files: WatchFile[] = [
      { uid: 'a', type: 'FIT_TYPE_4', fit_type: 4, size: 2048, recorded: now - 3600, fetched: now - 600, state: 'kept', on_watch: true, deletable: true },
      { uid: 'b', type: 'FIT_TYPE_32', fit_type: 32, size: 512, recorded: now - 7200, fetched: now - 600, state: 'archived', on_watch: false, deletable: false },
    ]
    const writes = api([device()], { '/api/devices/1/files': json(files) })
    at('/devices/watches/1?tab=files')
    const box = await screen.findByRole('checkbox', { name: 'Select Activity a' })
    expect((screen.getByRole('checkbox', { name: 'Select Monitoring b' }) as HTMLInputElement).disabled).toBe(true)
    fireEvent.click(box)
    fireEvent.click(screen.getByRole('button', { name: 'Delete from the watch' }))
    fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: 'Delete from the watch' }))
    await waitFor(() => expect(writes[0]).toEqual({ method: 'POST', path: '/api/devices/1/files/delete', body: { uids: ['a'] } }))
    await screen.findByText(/goarmin keeps its copies/)
  })

  it('shows a handed-over watch read-only', async () => {
    api([device({ current: false, owned_to: now - 86400, sync: undefined })])
    at('/devices/watches/1')
    await screen.findByText(/You handed this watch over/)
    expect(screen.queryByRole('button', { name: 'Settings' })).toBeNull()
    expect(screen.queryByRole('button', { name: /Sync now/ })).toBeNull()
    expect(await screen.findByText('Activities')).toBeTruthy()
  })
})

describe('SyncLog', () => {
  it('folds not-in-range runs and pages', async () => {
    let calls = 0
    api([device()], {
      '/api/sync-runs': (u) => {
        calls++
        if (u.searchParams.get('limit') === '200') return json({ items: [run(1, { ok: true, error: '', error_code: '' })] })()
        if (u.searchParams.get('before')) return json({ items: [run(9, { ok: false, error_code: 'timeout', error: 'pull: deadline' })] })()
        return json({ items: [run(1, { ok: true, error: '', error_code: '', listed: 5, pulled: 2, imported: 2 }), run(2), run(3), run(4)], next_before: 4 })()
      },
    })
    renderWith(<Page><SyncLog /></Page>, { me })
    await screen.findByText('Away ×3')
    expect(screen.getByText('OK')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Load more' }))
    await screen.findByText('Failed')
    fireEvent.click(screen.getByRole('button', { name: 'Why it failed' }))
    await screen.findByText('pull: deadline') // the raw error, under the hint
    expect(calls).toBeGreaterThanOrEqual(3)
  })
})

describe('Remove', () => {
  const at = (path: string, m = me) =>
    renderWith(<Page><Routes>
      <Route path="/devices/watches/:id" element={<Watch />} />
      <Route path="/devices/watches" element={<p>the list</p>} />
    </Routes></Page>, { path, me: m })

  it('deletes the data only once the name is typed', async () => {
    const writes = api([device({ paired: true })])
    at('/devices/watches/1?tab=remove')
    const del = await screen.findByRole('button', { name: 'Delete…' }) as HTMLButtonElement
    await waitFor(() => expect(del.disabled).toBe(false)) // once the counts are in
    fireEvent.click(del)
    const dialog = await screen.findByRole('alertdialog', { name: 'Delete the data of Fenix 8' })
    const go = within(dialog).getByRole('button', { name: 'Delete for good' }) as HTMLButtonElement
    expect(go.disabled).toBe(true)
    fireEvent.change(within(dialog).getByRole('textbox'), { target: { value: 'fenix 8 ' } })
    expect(go.disabled).toBe(false)
    fireEvent.click(go)
    await waitFor(() => expect(writes[0]).toEqual({ method: 'POST', path: '/api/devices/1/delete-data', body: { confirm: 'fenix 8 ' } }))
    expect(screen.getByRole('link', { name: /Download the original files/ }).getAttribute('href')).toBe('/api/devices/1/raw.zip')
  })

  it('hands the watch over, unpairing it if asked, and goes back to the list', async () => {
    const writes = api([device({ paired: true })])
    at('/devices/watches/1?tab=remove')
    fireEvent.click(await screen.findByRole('button', { name: 'Hand over…' }))
    const dialog = await screen.findByRole('alertdialog')
    fireEvent.click(within(dialog).getByRole('checkbox', { name: /Also unpair/ }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Hand over' }))
    await waitFor(() => expect(writes[0]).toEqual({ method: 'POST', path: '/api/devices/1/release', body: { unpair: true } }))
    await screen.findByText('the list')
  })

  it('offers a former owner only the delete', async () => {
    api([device({ current: false, owned_to: now - 86400, sync: undefined })])
    at('/devices/watches/1?tab=remove')
    await screen.findByRole('button', { name: 'Delete…' })
    expect(screen.queryByRole('button', { name: 'Hand over…' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Unpair…' })).toBeNull()
  })
})
