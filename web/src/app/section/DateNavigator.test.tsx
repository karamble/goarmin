// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { addDays, label, today, type Period } from '../../lib/period'
import { json, renderWith, stubApi, where } from '../../test/fixtures'
import { DateNavigator } from './DateNavigator'
import { PeriodsContext } from './usePeriod'

afterEach(() => vi.unstubAllGlobals())

function nav(path: string, periods: Period[] = ['day', 'week', 'month', 'year']) {
  stubApi({ '/api/daily': json([]) })
  return renderWith(
    <PeriodsContext.Provider value={periods}>
      <DateNavigator />
      <input aria-label="typing" />
    </PeriodsContext.Provider>,
    { path },
  )
}

const key = (k: string, target: Element | Window = window) => fireEvent.keyDown(target, { key: k })

describe('DateNavigator', () => {
  const now = today()
  const yesterday = addDays(now, -1)

  it('starts on today and never steps into the future', () => {
    nav('/health/sleep')
    expect(screen.getByText(label(now, 'day'))).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Next day' }) as HTMLButtonElement).disabled).toBe(true)
    expect((screen.getByRole('button', { name: 'Today' }) as HTMLButtonElement).disabled).toBe(true)
    key('ArrowRight')
    expect(where.path).toBe('/health/sleep')
  })

  it('steps with the arrows and the keys, keeping it in the URL', () => {
    nav('/health/sleep')
    key('ArrowLeft')
    expect(where.path).toBe(`/health/sleep?d=${yesterday}`)
    fireEvent.click(screen.getByRole('button', { name: 'Previous day' }))
    expect(where.path).toBe(`/health/sleep?d=${addDays(now, -2)}`)
    key('t')
    expect(where.path).toBe('/health/sleep')
  })

  it('switches periods by key and by button, only to allowed ones', () => {
    nav(`/health/sleep?d=${yesterday}`)
    key('w')
    expect(where.path).toBe(`/health/sleep?d=${yesterday}&p=week`)
    fireEvent.click(screen.getByRole('radio', { name: 'M' }))
    expect(where.path).toBe(`/health/sleep?d=${yesterday}&p=month`)
    fireEvent.click(screen.getByRole('radio', { name: 'D' }))
    expect(where.path).toBe(`/health/sleep?d=${yesterday}`)
  })

  it('ignores keys while typing', () => {
    nav('/health/sleep')
    key('ArrowLeft', screen.getByRole('textbox', { name: 'typing' }))
    key('w', screen.getByRole('textbox', { name: 'typing' }))
    expect(where.path).toBe('/health/sleep')
  })

  it('treats a future or broken date as today', () => {
    nav(`/?d=${addDays(now, 3)}`, ['day'])
    expect(screen.getByText(label(now, 'day'))).toBeTruthy()
    expect(screen.queryByRole('radiogroup')).toBeNull()
  })

  it('picks a day from the month popover', async () => {
    nav('/')
    fireEvent.click(screen.getByText(label(now, 'day')))
    const dialog = await screen.findByRole('dialog', { name: 'Choose a date' })
    // While it is open, the arrows belong to it.
    key('ArrowLeft')
    expect(where.path).toBe('/')
    const first = now.slice(0, 8) + '01'
    fireEvent.click(dialog.querySelector(`[aria-label^="${label(first, 'day')}"]`)!)
    expect(where.path).toBe(first === now ? '/' : `/?d=${first}`)
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
