// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ImportRun, USBWatch } from '../../api/types'
import { fixtureMe, json, renderWith, stubApi } from '../../test/fixtures'
import { ToastProvider } from '../../ui/Toast'
import ImportPage from './Import'

const upload = vi.hoisted(() => ({ calls: [] as { paths: string[] }[] }))
vi.mock('../../lib/upload', () => ({
  uploadFiles: vi.fn(async (files: { path: string }[], onProgress: (s: number, t: number) => void) => {
    upload.calls.push({ paths: files.map((f) => f.path) })
    onProgress(5, 10)
    return { id: 9, status: 'queued' }
  }),
  droppedFiles: vi.fn(async () => []),
}))

afterEach(() => {
  vi.unstubAllGlobals()
  upload.calls = []
})

const now = Math.floor(Date.now() / 1000)

function run(over: Partial<ImportRun> = {}): ImportRun {
  return {
    id: 1, kind: 'upload', source: 'upload', name: 'GARMIN (176 files)', device_id: null, started_by: 'manual', status: 'done',
    created_at: now - 600, started_at: now - 590, finished_at: now - 580, report: { seen: 176, new: 5, known: 171 }, error: '', ...over,
  }
}

function watch(over: Partial<USBWatch> = {}): USBWatch {
  return {
    id: 'usb-3912345678', name: 'Garmin fenix 8', how: 'direct', port: '3-8', mounted: false, owner: 'yours',
    device: { id: 4, model: 'Fenix 8', name: 'fenix 8 - 47mm', nickname: 'Daily' }, auto: true, importing: null,
    last: run({ id: 2, kind: 'usb', source: 'usb', name: 'Daily', device_id: 4, started_by: 'plugged', report: { seen: 176, new: 0, unchanged: 176 } }),
    ...over,
  }
}

function api(usb: USBWatch[], imports: ImportRun[]) {
  const writes: { method: string; path: string; body: unknown }[] = []
  const fn = stubApi({ '/api/usb': json(usb), '/api/imports': json({ items: imports, next_before: null }) })
  const base = fn.getMockImplementation() as (url: string, init?: RequestInit) => Promise<Response>
  fn.mockImplementation(async (url: string, init?: RequestInit) => {
    if (init?.method && init.method !== 'GET') {
      writes.push({ method: init.method, path: new URL(url, 'http://localhost').pathname, body: init.body ? JSON.parse(String(init.body)) : undefined })
      return json(run({ id: 3, status: 'queued', kind: 'usb' }), 202)()
    }
    return base(url, init)
  })
  return writes
}

const Page = ({ children }: { children: ReactNode }) => <ToastProvider>{children}</ToastProvider>

describe('Import', () => {
  it('imports your watch on USB and turns importing on plug-in off', async () => {
    const writes = api([watch()], [])
    renderWith(<Page><ImportPage /></Page>, { me: fixtureMe() })
    const row = (await screen.findByText('Daily')).closest('li')!
    expect(within(row).getByText('Your watch.')).toBeTruthy()
    expect(within(row).getByText(/Last import .*: 0 new, 176 already here/)).toBeTruthy()
    fireEvent.click(within(row).getByRole('button', { name: 'Import now' }))
    fireEvent.click(within(row).getByRole('switch', { name: 'Import when plugged in' }))
    await waitFor(() => expect(writes.map((w) => `${w.method} ${w.path}`)).toEqual(['POST /api/usb/usb-3912345678/import', 'PATCH /api/devices/4']))
    expect(writes[1].body).toEqual({ usb_auto: false })
  })

  it('shows another account’s watch without importing it, and the empty state', async () => {
    api([watch({ owner: 'other', device: null, auto: null, last: null, name: 'Garmin forerunner' })], [])
    const { unmount } = renderWith(<Page><ImportPage /></Page>, { me: fixtureMe() })
    const row = (await screen.findByText('Garmin forerunner')).closest('li')!
    expect(within(row).getByText('This watch belongs to another account.')).toBeTruthy()
    expect((within(row).getByRole('button', { name: 'Import now' }) as HTMLButtonElement).disabled).toBe(true)
    expect(within(row).queryByRole('switch')).toBeNull()
    unmount()
    vi.unstubAllGlobals()
    api([], [])
    renderWith(<Page><ImportPage /></Page>, { me: fixtureMe() })
    expect(await screen.findByText(/No watch on USB/)).toBeTruthy()
    expect(await screen.findByText('Nothing imported yet.')).toBeTruthy()
  })

  it('shows a running import of the watch', async () => {
    api([watch({ importing: run({ id: 5, kind: 'usb', status: 'running', finished_at: null, report: { seen: 12, new: 3 } }) })], [])
    renderWith(<Page><ImportPage /></Page>, { me: fixtureMe() })
    expect(await screen.findByText(/Importing: 12 files read, 3 new/)).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Import now' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('uploads what is worth sending from a folder', async () => {
    api([], [])
    renderWith(<Page><ImportPage /></Page>, { me: fixtureMe() })
    const input = await screen.findByLabelText('Choose a folder')
    const file = (path: string) => Object.defineProperty(new File([new Uint8Array(20)], path.split('/').pop()!), 'webkitRelativePath', { value: path })
    fireEvent.change(input, { target: { files: [file('GARMIN/Activity/A.FIT'), file('GARMIN/Monitor/M.FIT'), file('GARMIN/Apps/x.PRG')] } })
    expect(await screen.findByText(/GARMIN \(2 files\), 40 B/)).toBeTruthy()
    expect(screen.getByText(/1 other files stay on this computer/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Import' }))
    await waitFor(() => expect(upload.calls).toEqual([{ paths: ['GARMIN/Activity/A.FIT', 'GARMIN/Monitor/M.FIT'] }]))
    expect(await screen.findByText('Importing GARMIN (2 files)')).toBeTruthy()
  })

  it('shows a running export live: how far, what so far, what it reads', async () => {
    const writes = api([], [run({ id: 8, source: 'gc_export', name: 'export.zip', status: 'running', started_at: now - 65, finished_at: null,
      report: { export: true, read: 1200, found: 12281, seen: 1100, new: 950, known: 150, kept: 100, bytes: 9_500_000,
        current: 'export.zip/DI_CONNECT/DI-Connect-Uploaded-Files/UploadedFiles_0-_Part1.zip/x_1.fit' } })])
    renderWith(<Page><ImportPage /></Page>, { me: fixtureMe() })
    const panel = await screen.findByRole('region', { name: /Import export.zip in progress/ })
    expect(within(panel).getByText('Importing the Garmin Connect export')).toBeTruthy()
    expect(within(panel).getByText('1,200')).toBeTruthy()
    expect(within(panel).getByText('of 12,281')).toBeTruthy()
    expect(within(panel).getByText('950')).toBeTruthy()
    expect(within(panel).getByText('9 %')).toBeTruthy()
    expect(within(panel).getByRole('progressbar').getAttribute('aria-valuenow')).toBe('9')
    expect(within(panel).getByText(/UploadedFiles_0-_Part1\.zip\/x_1\.fit/)).toBeTruthy()
    expect(within(panel).getByText(/files\/s/)).toBeTruthy()
    expect(within(panel).getByText(/^1 min/)).toBeTruthy()
    fireEvent.click(within(panel).getByRole('button', { name: 'Stop' }))
    await waitFor(() => expect(writes).toEqual([{ method: 'DELETE', path: '/api/imports/8', body: undefined }]))
  })

  it('lists imports with what an export kept and what went wrong', async () => {
    const writes = api([], [
      run({ id: 7, status: 'running', finished_at: null, report: { seen: 40, new: 10, kept: 5 } }),
      run({ id: 6, source: 'gc_export', name: 'export.zip', report: { seen: 900, new: 850, known: 50, kept: 120, kept_by: { 'DI-Connect-Fitness': 100, 'DI-Connect-User': 20 }, archives: 3, failed: 1, errors: ['DI_CONNECT/x.zip: zipwalk: not a zip archive'] } }),
      run({ id: 5, status: 'failed', error: 'that watch is not plugged in', kind: 'usb', name: 'Daily', report: {} }),
    ])
    renderWith(<Page><ImportPage /></Page>, { me: fixtureMe() })
    const exp = (await screen.findByText('export.zip')).closest('li')!
    expect(within(exp).getByText('Garmin Connect export', { exact: false })).toBeTruthy()
    expect(within(exp).getByText('850 new, 50 already here, 120 kept for later, 1 failed')).toBeTruthy()
    expect(within(exp).getByText('DI-Connect-Fitness')).toBeTruthy()
    expect(within(exp).getByText(/not a zip archive/)).toBeTruthy()
    expect(screen.getByText('that watch is not plugged in')).toBeTruthy()
    const running = screen.getByText(/45 files read, 10 new, 5 kept for later/).closest('li')!
    fireEvent.click(within(running).getByRole('button', { name: 'Stop' }))
    await waitFor(() => expect(writes).toEqual([{ method: 'DELETE', path: '/api/imports/7', body: undefined }]))
  })
})
