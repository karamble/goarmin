// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { useState, type ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Device, PairView, PairWatch } from '../../../api/types'
import { HeaderSlotContext } from '../../../app/section/HeaderSlot'
import { fixtureMe, renderWith, where } from '../../../test/fixtures'
import { ToastProvider } from '../../../ui/Toast'
import AddWatch from './AddWatch'

afterEach(() => vi.unstubAllGlobals())

const now = Math.floor(Date.now() / 1000)
/** The first day of data in the summary: two weeks ago. */
const firstDay = new Date((now - 14 * 86400) * 1000).toISOString().slice(0, 10)
const me = fixtureMe({ server: { version: 'dev', sync: true, tiles: false } })

const fenix: PairWatch = { address: 'C0:FF:EE:12:34:56', name: 'fenix 8 - 47mm', rssi: -55, signal: 'strong', seen_now: true, paired_here: false, owner: 'new' }
const others: PairWatch = { address: 'AA:BB:CC:DD:EE:02', name: 'Forerunner 965', rssi: -80, signal: 'weak', seen_now: true, paired_here: false, owner: 'other' }

function view(over: Partial<PairView> = {}): PairView {
  return {
    id: 's1', state: 'prepare', started_at: now - 10, expires_at: now + 590, owner: { id: 1, email: 'a@example.invalid', name: 'Ada' },
    error: null, error_code: null, retry: false, discovering: false, watches: [], selected: null, prompt: null, steps: [],
    identified: null, ownership: null, former_owner: false, bond: null, device_id: null,
    adapter: { name: 'hci0', powered: true, power_state: 'on', pairable: true }, busy: null, ...over,
  }
}

const identified = { unit: 3912345678, product: 4536, model: 'Fenix 8', name: 'fenix 8 - 47mm', firmware: '23.40', battery: 91 }

function device(over: Partial<Device> = {}): Device {
  return {
    id: 7, unit_id: 3912345678, product: 4536, model: 'Fenix 8', name: 'fenix 8 - 47mm', nickname: 'Trail', firmware: '23.40',
    address: fenix.address, after_fetch: 'archive', paused: false, usb_auto: true, delete_support: '', battery: 91, battery_at: now, current: true,
    owned_from: 0, owned_to: null, paired: true,
    sync: { address: fenix.address, running: true, live: false, last_ok: false, pulled: 0, imported: 0, failures: 0, away: false,
      phase: 'fetching', listed: 10, done: 3, total: 10, last_start: now - 5 },
    ...over,
  }
}

type Handler = (body: unknown) => { status?: number; json?: unknown } | undefined

/**
 * server fakes the API: the session (with an event stream the test pushes views into), the
 * actions (recorded; each answers with the view handlers[path] gives, else the current one),
 * Bluetooth, devices, accounts.
 */
function server(opt: { session?: PairView | null; bluetooth?: object; devices?: Device[]; handlers?: Record<string, Handler> } = {}) {
  const s = {
    session: opt.session ?? null,
    devices: opt.devices ?? [],
    writes: [] as { method: string; path: string; body: unknown }[],
    streams: [] as ReadableStreamDefaultController<Uint8Array>[],
    push(v: PairView) {
      s.session = v
      const msg = new TextEncoder().encode(`event: session\ndata: ${JSON.stringify(v)}\n\n`)
      for (const c of s.streams) {
        try {
          c.enqueue(msg)
        } catch {
          /* closed */
        }
      }
    },
  }
  const reply = (v: unknown, status = 200) => new Response(v === undefined ? null : JSON.stringify(v), { status, headers: { 'Content-Type': 'application/json' } })
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    const path = new URL(url, 'http://localhost').pathname
    const method = init?.method ?? 'GET'
    if (method !== 'GET') {
      const body = init?.body ? JSON.parse(String(init.body)) : undefined
      s.writes.push({ method, path, body })
      const h = opt.handlers?.[`${method} ${path}`]?.(body)
      if (h) {
        if (h.json && (h.status ?? 200) < 300) s.session = h.json as PairView
        return reply(h.json, h.status ?? 200)
      }
      if (method === 'DELETE') return new Response(null, { status: 204 })
      return reply(s.session)
    }
    if (path === '/api/pairing') return s.session ? reply(s.session) : reply({ error: 'no pairing session' }, 404)
    if (path === '/api/pairing/s1/events') {
      const stream = new ReadableStream<Uint8Array>({
        start(c) {
          s.streams.push(c)
          if (s.session) c.enqueue(new TextEncoder().encode(`event: session\ndata: ${JSON.stringify(s.session)}\n\n`))
          init?.signal?.addEventListener('abort', () => c.error(new DOMException('aborted', 'AbortError')))
        },
      })
      return new Response(stream, { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
    }
    if (path === '/api/pairing/s1') return reply(s.session)
    if (path === '/api/bluetooth') {
      return reply(opt.bluetooth ?? { available: true, adapter: 'hci0', address: 'AA:BB:CC:00:11:22', powered: true, power_state: 'on', busy: null })
    }
    if (path === '/api/devices') return reply(s.devices)
    if (path === '/api/devices/7/summary') {
      return reply({ files: 40, bytes: 1, by_type: {}, activities: 12, nights: 9, days: 14, first_day: firstDay, last_day: '', first_at: 0, last_at: 0 })
    }
    if (path === '/api/admin/users') {
      return reply([{ id: 1, email: 'a@example.invalid', display_name: 'Ada' }, { id: 2, email: 'b@example.invalid', display_name: 'Ben' }])
    }
    return reply({ error: 'no route' }, 404)
  }))
  return s
}

function Page({ children }: { children: ReactNode }) {
  const [slot, setSlot] = useState<HTMLDivElement | null>(null)
  return (
    <ToastProvider>
      <div ref={setSlot} data-testid="header" />
      <HeaderSlotContext.Provider value={slot}>{children}</HeaderSlotContext.Provider>
    </ToastProvider>
  )
}

const show = (path = '/devices/add', who = me) => renderWith(<Page><AddWatch /></Page>, { me: who, path })

describe('Add a watch', () => {
  it('starts on Prepare and searches once the watch is ready', async () => {
    const s = server({
      handlers: {
        'POST /api/pairing': () => ({ status: 201, json: view() }),
        'POST /api/pairing/s1/scan': () => ({ json: view({ state: 'finding', discovering: true, watches: [fenix] }) }),
      },
    })
    show()
    await screen.findByRole('heading', { name: 'Get the watch ready' })
    expect(screen.getByText(/Watch Settings › Connectivity › Phone › Pair Phone/)).toBeTruthy()
    const go = await screen.findByRole('button', { name: 'My watch shows a pairing screen' })
    await waitFor(() => expect((go as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(go)
    const heading = await screen.findByRole('heading', { name: 'Choose your watch' })
    expect(s.writes.map((w) => `${w.method} ${w.path}`)).toEqual(['POST /api/pairing', 'POST /api/pairing/s1/scan'])
    expect(s.writes[0].body).toBeUndefined()
    await waitFor(() => expect(document.activeElement).toBe(heading))
    expect(screen.getByText('Step 2 of 5: Find')).toBeTruthy()
  })

  it('waits for Bluetooth to be on, and says when someone else is adding a watch', async () => {
    server({ bluetooth: { available: true, adapter: 'hci0', powered: false, power_state: 'off' } })
    show()
    await screen.findByRole('button', { name: 'Turn on' })
    expect((screen.getByRole('button', { name: 'My watch shows a pairing screen' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('shows the refusal when another session runs', async () => {
    const s = server({ handlers: { 'POST /api/pairing': () => ({ status: 409, json: { error: 'Someone is adding a watch right now. Try again in a few minutes.' } }) } })
    show()
    const go = await screen.findByRole('button', { name: 'My watch shows a pairing screen' })
    await waitFor(() => expect((go as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(go)
    await screen.findByText('Someone is adding a watch right now. Try again in a few minutes.')
    expect(s.writes).toHaveLength(1)
  })

  it('resumes on Find: another account’s watch cannot be chosen, Pair sends the address', async () => {
    const s = server({ session: view({ state: 'finding', discovering: true, watches: [fenix, others] }) })
    show()
    await screen.findByRole('heading', { name: 'Choose your watch' })
    const radios = screen.getAllByRole('radio') as HTMLInputElement[]
    expect(radios.map((r) => r.value)).toEqual([fenix.address, others.address])
    expect(radios[1].disabled).toBe(true)
    expect(screen.getByText('Belongs to another account')).toBeTruthy()
    expect(radios[0].checked).toBe(true) // the only one to choose from
    expect(screen.getByText('Searching for watches…')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Pair' }))
    await waitFor(() => expect(s.writes).toHaveLength(1))
    expect(s.writes[0]).toEqual({ method: 'POST', path: '/api/pairing/s1/select', body: { address: fenix.address, repair: false } })
  })

  it('picks the only watch for you, but not once a second one shows up', async () => {
    const paired = { ...others, owner: 'new' as const, paired_here: true }
    const s = server({ session: view({ state: 'finding', discovering: true, watches: [paired] }) })
    show()
    await screen.findByRole('heading', { name: 'Choose your watch' })
    expect((screen.getByRole('radio') as HTMLInputElement).checked).toBe(true)
    s.push(view({ state: 'finding', discovering: true, watches: [fenix, paired] }))
    await waitFor(() => expect(screen.getAllByRole('radio')).toHaveLength(2))
    expect((screen.getAllByRole('radio') as HTMLInputElement[]).some((r) => r.checked)).toBe(false)
    expect((screen.getByRole('button', { name: 'Pair' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.click(screen.getAllByRole('radio')[0])
    expect((screen.getByRole('button', { name: 'Pair' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('offers Pair again for a watch paired here, and shows a failed try above the list', async () => {
    const s = server({
      session: view({ state: 'finding', watches: [{ ...fenix, paired_here: true }], error: 'The codes did not match.', error_code: 'mismatch', retry: true }),
    })
    show()
    await screen.findByRole('heading', { name: 'Choose your watch' })
    expect(screen.getByRole('alert').textContent).toMatch(/codes did not match, so the pairing was refused/)
    expect(screen.getByRole('button', { name: 'Use this watch' })).toBeTruthy()
    expect(screen.getByText('The search has paused.')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Pair again' }))
    await waitFor(() => expect(s.writes).toHaveLength(1))
    expect(s.writes[0].body).toEqual({ address: fenix.address, repair: true })
    fireEvent.click(screen.getByRole('button', { name: /Scan again/ }))
    await waitFor(() => expect(s.writes.map((w) => w.path)).toContain('/api/pairing/s1/scan'))
  })

  it('follows the stream: the code, the answer, identifying, setup', async () => {
    const s = server({ session: view({ state: 'pairing', selected: fenix.address, watches: [fenix] }) })
    show()
    await screen.findByRole('heading', { name: 'Connecting' })
    s.push(view({ state: 'confirm', selected: fenix.address, watches: [fenix], prompt: { kind: 'confirm', passkey: '482917', deadline: now + 30 } }))
    const heading = await screen.findByRole('heading', { name: 'Check the code' })
    await waitFor(() => expect(document.activeElement).toBe(heading))
    expect(screen.getByRole('img', { name: 'Code 4 8 2 9 1 7' }).textContent).toBe('482 917')
    expect(screen.getByText(/s left to answer/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'They match' }))
    await waitFor(() => expect(s.writes).toHaveLength(1))
    expect(s.writes[0]).toEqual({ method: 'POST', path: '/api/pairing/s1/confirm', body: { match: true } })
    s.push(view({ state: 'identifying', selected: fenix.address, watches: [fenix],
      steps: [{ name: 'connected', done: true }, { name: 'link', done: false }, { name: 'battery', done: false }] }))
    await screen.findByRole('heading', { name: 'Getting to know the watch' })
    expect(screen.getByText(': done')).toBeTruthy()
    expect(screen.getByText(': working')).toBeTruthy()
    s.push(view({ state: 'setup', identified, ownership: 'new' }))
    await screen.findByRole('heading', { name: 'Set up your watch' })
    expect(screen.getByText('Step 4 of 5: Set up')).toBeTruthy()
  })

  it('asks for the code a watch shows when it wants one typed', async () => {
    const s = server({ session: view({ state: 'enter_passkey', selected: fenix.address, prompt: { kind: 'enter', passkey: '', deadline: now + 30 } }) })
    show()
    const input = await screen.findByLabelText('Code on the watch')
    const send = screen.getByRole('button', { name: 'Send the code' }) as HTMLButtonElement
    fireEvent.change(input, { target: { value: '04a2917' } })
    expect((input as HTMLInputElement).value).toBe('042917')
    expect(send.disabled).toBe(false)
    fireEvent.click(send)
    await waitFor(() => expect(s.writes).toHaveLength(1))
    expect(s.writes[0].body).toEqual({ passkey: 42917 })
  })

  it('sets the watch up only once the Garmin Connect question is answered', async () => {
    const s = server({
      session: view({ state: 'setup', identified, ownership: 'nobody', former_owner: true }),
      handlers: { 'POST /api/pairing/s1/setup': () => ({ json: view({ state: 'done', device_id: 7 }) }) },
      devices: [device()],
    })
    show()
    await screen.findByRole('heading', { name: 'Set up your watch' })
    expect(screen.queryByText('Also fetch older activities still on the watch')).toBeNull() // someone had it before
    expect(screen.getByText(/This watch will be yours \(a@example.invalid\)/)).toBeTruthy()
    const start = screen.getByRole('button', { name: 'Start the first sync' }) as HTMLButtonElement
    expect(start.disabled).toBe(true)
    fireEvent.change(screen.getByLabelText(/What you call it/), { target: { value: ' Trail ' } })
    fireEvent.click(screen.getByLabelText(/No, goarmin is the only one/))
    expect(start.disabled).toBe(false)
    fireEvent.click(start)
    await waitFor(() => expect(s.writes).toHaveLength(1))
    expect(s.writes[0].body).toEqual({ nickname: 'Trail', garmin_connect: false, include_synced: false })
    await screen.findByRole('heading', { name: 'First sync' })
  })

  it('fetches older activities by default and keeps the settings of a watch that is yours', async () => {
    const s = server({ session: view({ state: 'setup', identified, ownership: 'yours', nickname: 'Old name', after_fetch: 'keep', bond: 'existing' }) })
    show()
    await screen.findByRole('heading', { name: 'Set up your watch' })
    expect(screen.getByText(/Uses the pairing this computer already had/)).toBeTruthy()
    expect((screen.getByLabelText(/What you call it/) as HTMLInputElement).value).toBe('Old name')
    expect((screen.getByLabelText(/Yes, Garmin Connect still syncs it/) as HTMLInputElement).checked).toBe(true)
    expect((screen.getByLabelText(/Also fetch older activities/) as HTMLInputElement).checked).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Start the first sync' }))
    await waitFor(() => expect(s.writes).toHaveLength(1))
    expect(s.writes[0].body).toEqual({ nickname: 'Old name', garmin_connect: true, include_synced: true })
  })

  it('follows the first sync and then says what arrived', async () => {
    const s = server({ session: view({ state: 'done', device_id: 7 }), devices: [device()] })
    show()
    await screen.findByRole('heading', { name: 'First sync' })
    await screen.findByText('Fetching 3 of 10 files')
    expect(screen.getByText(/keeps going if you leave this page/)).toBeTruthy()
    s.devices = [device({ sync: { ...device().sync!, running: false, last_ok: true, last_end: now, done: 10, phase: undefined } })]
    await screen.findByText('Imported into your account', {}, { timeout: 5000 })
    await screen.findByText('12')
    expect(screen.getByRole('link', { name: 'Open your glance' }).getAttribute('href')).toBe('/')
    expect(screen.getByRole('link', { name: 'Go to the watch' }).getAttribute('href')).toBe('/devices/watches/7')
    fireEvent.click(screen.getByRole('button', { name: 'Add another watch' }))
    await screen.findByRole('heading', { name: 'Get the watch ready' })
  })

  it('ends with a reason for another account’s watch, and starts again', async () => {
    server({ session: view({ state: 'failed', error_code: 'owned_elsewhere', error: 'belongs to another account' }) })
    show()
    await screen.findByRole('heading', { name: 'Not added' })
    expect(screen.getByText(/Its owner can hand it over/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Start again' }))
    await screen.findByRole('heading', { name: 'Get the watch ready' })
  })

  it('asks before cancelling once a watch is chosen, then goes back to the watches', async () => {
    const s = server({ session: view({ state: 'pairing', selected: fenix.address, watches: [fenix] }) })
    show()
    await screen.findByRole('heading', { name: 'Connecting' })
    fireEvent.click(within(screen.getByTestId('header')).getByRole('button', { name: 'Cancel' }))
    const dialog = await screen.findByRole('alertdialog', { name: 'Stop adding the watch?' })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Stop' }))
    await waitFor(() => expect(where.path).toBe('/devices/watches'))
    expect(s.writes.at(-1)).toEqual({ method: 'DELETE', path: '/api/pairing/s1', body: undefined })
  })

  it('lets an admin add a watch for someone else', async () => {
    const s = server({ handlers: { 'POST /api/pairing': () => ({ status: 201, json: view({ owner: { id: 2, email: 'b@example.invalid' } }) }) } })
    show('/devices/add?for=2')
    const select = await screen.findByLabelText('This watch is for') as HTMLSelectElement
    await waitFor(() => expect(select.value).toBe('2'))
    const go = screen.getByRole('button', { name: 'My watch shows a pairing screen' })
    await waitFor(() => expect((go as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(go)
    await waitFor(() => expect(s.writes.length).toBeGreaterThan(0))
    expect(s.writes[0]).toEqual({ method: 'POST', path: '/api/pairing', body: { owner: 2 } })
  })
})
