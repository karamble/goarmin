// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, render, screen, within } from '@testing-library/react'
import { Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Body as BodyData, FileMessages, WeighIn } from '../api/types'
import { PeriodsContext } from '../app/section/usePeriod'
import { addDays, today } from '../lib/period'
import { fixtureMe, json, renderWith, stubApi } from '../test/fixtures'
import { DataView } from '../ui/DataView'
import FileInspector from './devices/FileInspector'
import Body from './health/Body'
import AccountData, { shortName } from './settings/AccountData'
import { recordValue } from './training/Records'

afterEach(() => vi.unstubAllGlobals())

const day = today('UTC')
const now = Math.floor(Date.now() / 1000)

function weighIn(over: Partial<WeighIn> = {}): WeighIn {
  return {
    ts: now - 86400 * 3, day: addDays(day, -3), weight_kg: 92.4, fat_pct: 21.5, hydration_pct: null, bone_kg: null, muscle_kg: 38.2,
    visceral_fat_kg: null, bmi: 27.1, bmr_kcal: 1900, active_mr_kcal: null, metabolic_age: null, visceral_rating: null, source: 'fit', ...over,
  }
}

describe('Body', () => {
  const periods = ['day', 'week', 'month', 'year'] as const
  const page = (path: string) => renderWith(<PeriodsContext.Provider value={[...periods]}><Body /></PeriodsContext.Provider>,
    { path, me: fixtureMe({ timezone: 'UTC' }) })

  it('shows the last weigh-in up to the day, with its composition', async () => {
    const body: BodyData = { weigh_ins: [weighIn({ weight_kg: 93.1, day: addDays(day, -10), ts: now - 86400 * 10 }), weighIn()],
      skin_temp: [{ day, ts: now, nightly_c: 33.4, deviation_c: -0.3, deviation_7d_c: 0.1, calibrated_days: 30 }] }
    stubApi({ '/api/body': json(body), '/api/daily': json([{ day, values: { hydration: 1500, fitness_age: 38 } }]) })
    page('/health/body')
    expect(await screen.findByText('92.4')).toBeTruthy()
    expect(screen.getByText('21.5')).toBeTruthy()
    expect(screen.getByText(/^Weigh-in /).textContent).not.toContain('this day')
    expect(screen.getByText('−0.3')).toBeTruthy()
    expect(screen.getByText('1500')).toBeTruthy()
  })

  it('draws weight and body composition on one chart, each measure switched on and off', async () => {
    const store = new Map<string, string>()
    vi.stubGlobal('localStorage', { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v), removeItem: (k: string) => void store.delete(k) })
    const body: BodyData = { weigh_ins: [weighIn({ weight_kg: 93.1, day: addDays(day, -400), ts: now - 86400 * 400 }), weighIn()], skin_temp: [] }
    stubApi({ '/api/body': json(body), '/api/daily': json([]) })
    page('/health/body?p=month')
    expect(await screen.findByText('Weight and body composition')).toBeTruthy()
    const group = screen.getByRole('group', { name: 'Measures on the chart' })
    const chip = (name: RegExp) => within(group).getByRole('button', { name })
    // Only what the weigh-ins have: weight, muscle, body fat and BMI (no bone or water here); BMI starts off.
    expect(within(group).getAllByRole('button').map((b) => b.textContent)).toEqual(['Weight (kg)', 'Muscle mass (kg)', 'Body fat (%)', 'BMI'])
    expect(chip(/BMI/).getAttribute('aria-pressed')).toBe('false')
    expect(chip(/Weight/).getAttribute('aria-pressed')).toBe('true')
    fireEvent.click(chip(/Muscle mass/))
    expect(chip(/Muscle mass/).getAttribute('aria-pressed')).toBe('false')
    expect(store.get('goarmin.body.history.series')).toBe(JSON.stringify(['weight_kg', 'fat_pct']))
    // The table lists every weigh-in of the history, newest first, with each measure.
    fireEvent.click(within(screen.getByText('Weight and body composition').closest('section')!).getByRole('button', { name: 'Table' }))
    const rows = screen.getAllByRole('row')
    expect(rows[1].textContent).toContain('92.4')
    expect(rows[2].textContent).toContain('93.1')
  })

  it('says what brings body data when there is none', async () => {
    stubApi({ '/api/body': json({ weigh_ins: [], skin_temp: [] }), '/api/daily': json([]) })
    page('/health/body?p=month')
    expect(await screen.findByText(/smart scale/)).toBeTruthy()
  })
})

describe('Records', () => {
  it('writes each record in its unit', () => {
    expect(recordValue({ metric: 'Best 5km Run', value: 2684.15 }, 'metric')).toBe('44:44')
    expect(recordValue({ metric: 'Farthest Run', value: 5022.27 }, 'metric')).toMatch(/^5\.0\d? km$/)
    expect(recordValue({ metric: 'Most Steps in a Day', value: 23114 }, 'metric')).toBe('23,114')
    expect(recordValue({ metric: 'Longest Goal Streak', value: 8 }, 'metric')).toBe('8 days')
    expect(recordValue({ metric: 'Max Elevation Gain', value: 133 }, 'metric')).toBe('133 m')
  })
})

describe('DataView', () => {
  it('shows an object by names and a list of objects as a table', () => {
    render(<DataView value={{ preferredLocale: 'de', orders: [{ orderNumber: 1, orderTotal: 9.5 }, { orderNumber: 2, orderTotal: 12 }], flag: true }} />)
    expect(screen.getByText('Preferred locale')).toBeTruthy()
    expect(screen.getByText('yes')).toBeTruthy()
    const table = screen.getByRole('table')
    expect(within(table).getByText('Order total')).toBeTruthy()
    expect(within(table).getByText('9.5')).toBeTruthy()
  })
})

describe('Account data', () => {
  it('groups the export’s records and opens one', async () => {
    stubApi({
      '/api/account/documents': json([{ id: 5, kind: 'orders', name: 'orders.json', recorded_at: now }, { id: 6, kind: 'settings', name: 'user_settings.json', recorded_at: now }]),
      '/api/account/documents/5': json({ id: 5, kind: 'orders', name: 'orders.json', recorded_at: now, data: [{ orderNumber: 7, orderStatus: 'SHIPPED' }] }),
    })
    renderWith(<AccountData />)
    expect(await screen.findByRole('heading', { name: 'Orders, consents and account events' })).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Profile and settings' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Orders/ }))
    expect(await screen.findByText('SHIPPED')).toBeTruthy()
  })
})

describe('Account file names', () => {
  it('drop the e-mail address Garmin puts before them', () => {
    expect(shortName('someone@example.invalid-profile-image-large.png')).toBe('profile-image-large.png')
    expect(shortName('someone@example.invalid_gear.json')).toBe('gear.json')
    expect(shortName('orders.json')).toBe('orders.json')
  })
})

describe('File inspector', () => {
  it('names the messages and their fields, and filters by message', async () => {
    const data: FileMessages = {
      file: { id: 3, name: 'W.fit', source: 'usb', size: 120, received_at: now, device_id: null, fit_type: 9, type_name: 'Weight', created: now },
      counts: [{ num: 0, name: 'file_id', count: 1, known: true, official: true }, { num: 30, name: 'weight_scale', count: 1, known: true, official: true },
        { num: 64999, name: 'message_64999', count: 1, known: false, official: false }],
      messages: [{ num: 30, name: 'weight_scale', known: true, official: true, fields: [{ num: 0, name: 'weight', units: 'kg', value: 75.2, raw: 7520, known: true }] }],
      total: 1, offset: 0,
    }
    const fn = stubApi({ '/api/files/3/messages': json(data) })
    renderWith(<Routes><Route path="/devices/files/:id" element={<FileInspector />} /></Routes>, { path: '/devices/files/3' })
    expect(await screen.findByText('75.2 kg')).toBeTruthy()
    expect(screen.getByRole('button', { name: /message_64999/ })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /weight_scale/ }))
    expect(fn.mock.calls.some(([u]) => String(u).includes('num=30'))).toBe(true)
  })
})
