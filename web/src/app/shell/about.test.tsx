// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { fireEvent, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DISCLAIMER, FIGURE_DISCLAIMER, SOURCE_URL } from '../../lib/disclaimer'
import { fixtureMe, renderWith } from '../../test/fixtures'
import { AboutHost } from './About'
import { BottomBar } from './BottomBar'
import { Rail } from './Rail'

afterEach(() => vi.unstubAllGlobals())

describe('About', () => {
  it('opens from the rail, right after Go to, with the source link and the disclaimer', () => {
    const me = fixtureMe()
    renderWith(<><Rail sections={[]} me={me} open onToggle={() => {}} /><AboutHost version="1.2.3" /></>)
    const rail = screen.getByRole('navigation', { name: 'Sections' })
    const buttons = within(rail).getAllByRole('button').map((b) => b.textContent ?? '')
    expect(buttons.findIndex((t) => t.startsWith('About'))).toBe(buttons.findIndex((t) => t.startsWith('Go to')) + 1)
    fireEvent.click(within(rail).getByRole('button', { name: 'About' }))
    const dialog = screen.getByRole('dialog', { name: 'About goarmin' })
    expect(within(dialog).getByText('Version 1.2.3')).toBeTruthy()
    const link = within(dialog).getByRole('link', { name: /Source code on GitHub/ })
    expect(link.getAttribute('href')).toBe(SOURCE_URL)
    expect(link.getAttribute('rel')).toBe('noopener noreferrer')
    expect(within(dialog).getByRole('heading', { name: 'Disclaimer' })).toBeTruthy()
    for (const d of DISCLAIMER) expect(within(dialog).getByText(d.lead)).toBeTruthy()
    expect(dialog.textContent).toContain('accept no liability')
    expect(dialog.textContent).toContain(FIGURE_DISCLAIMER)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: 'About goarmin' })).toBeNull()
  })

  it('opens from More on a phone', () => {
    renderWith(<><BottomBar sections={[]} me={fixtureMe()} /><AboutHost /></>)
    fireEvent.click(screen.getByRole('button', { name: 'More' }))
    fireEvent.click(screen.getByRole('button', { name: 'About' }))
    expect(screen.getByRole('dialog', { name: 'About goarmin' })).toBeTruthy()
    expect(screen.queryByRole('dialog', { name: 'More sections' })).toBeNull()
  })
})
