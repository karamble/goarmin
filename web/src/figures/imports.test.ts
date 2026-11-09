// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { expect, it } from 'vitest'

// three.js stays in one place: only the figure canvas imports it, so it loads in its own chunk
// with the first figure and nowhere else.
const sources = import.meta.glob(['../**/*.ts', '../**/*.tsx'], { query: '?raw', import: 'default', eager: true }) as Record<string, string>

it('imports three only in figures/FigureCanvas', () => {
  const users: string[] = []
  for (const [file, src] of Object.entries(sources)) {
    if (/\.test\.tsx?$/.test(file)) continue
    if (/(?:^|\n)\s*(?:import|export)\b[^'"]*?from\s+['"]three(?:\/[^'"]*)?['"]/.test(src)) users.push(file)
  }
  expect(Object.keys(sources).length).toBeGreaterThan(50)
  expect(users).toEqual(['./FigureCanvas.tsx'])
})
