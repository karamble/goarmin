// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import type { Me } from '../api/types'
import { findPage, sections, visibleSections } from './routes'
import { carriedSearch } from './section/search'

const me = (over: Partial<Me> = {}): Me => ({
  id: 1, email: 'a@example.invalid', display_name: 'A', timezone: '', units: 'metric', is_admin: false,
  created_at: new Date().toISOString(), server: { version: 'dev', sync: false, tiles: false }, ...over,
})

describe('routes', () => {
  it('hides the sync-only and admin-only parts', () => {
    const plain = visibleSections(me())
    expect(plain.find((s) => s.id === 'live')).toBeUndefined()
    expect(plain.find((s) => s.id === 'devices')?.pages.map((p) => p.path)).toEqual(['watches', 'log', 'import'])
    expect(plain.find((s) => s.id === 'settings')?.pages.map((p) => p.path)).not.toContain('admin')
    const full = visibleSections(me({ is_admin: true, server: { version: 'dev', sync: true, tiles: true } }))
    expect(full.find((s) => s.id === 'live')).toBeDefined()
    expect(full.find((s) => s.id === 'settings')?.pages.map((p) => p.path)).toContain('admin')
  })
  it('resolves paths', () => {
    expect(findPage('/').section?.id).toBe('glance')
    expect(findPage('/health/hrv/').page?.label).toBe('HRV')
    expect(findPage('/nowhere').section).toBeUndefined()
  })
  it('routes Add a watch as a page outside the tab strip, only with the sync role', async () => {
    const add = findPage('/devices/add').page
    expect(add?.label).toBe('Add a watch')
    expect(add?.hidden).toBe(true)
    expect(add?.requires).toBe('sync')
    const mod = await add!.load()
    expect(mod.default.name).toBe('AddWatch')
    const withSync = visibleSections(me({ server: { version: 'dev', sync: true, tiles: false } }))
    expect(withSync.find((s) => s.id === 'devices')?.pages.filter((p) => !p.hidden).map((p) => p.path)).toEqual(['watches', 'log', 'import'])
  })
  it('has unique ids and paths', () => {
    expect(new Set(sections.map((s) => s.id)).size).toBe(sections.length)
    for (const s of sections) expect(new Set(s.pages.map((p) => p.path)).size).toBe(s.pages.length)
  })
})

describe('carriedSearch', () => {
  const sleep = sections.find((s) => s.id === 'health')!.pages[0]
  const coach = sections.find((s) => s.id === 'training')!.pages.find((p) => p.path === 'coach')!
  const day = new Date().toISOString().slice(0, 10)
  it('keeps the date and an allowed period, nothing else', () => {
    expect(carriedSearch(`?d=${day}&p=month&x=1`, sleep)).toBe(`?d=${day}&p=month`)
    expect(carriedSearch(`?d=${day}&p=month`, coach)).toBe(`?d=${day}`)
    expect(carriedSearch('', sleep)).toBe('')
  })
})
