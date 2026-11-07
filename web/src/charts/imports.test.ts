// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { expect, it } from 'vitest'

// The chart library stays extractable: it imports only react, echarts, zrender and itself.
const sources = import.meta.glob(['./**/*.ts', './**/*.tsx'], { query: '?raw', import: 'default', eager: true }) as Record<string, string>

it('imports only react, echarts, zrender and relative paths', () => {
  const bad: string[] = []
  for (const [file, src] of Object.entries(sources)) {
    if (/\.test\.tsx?$/.test(file)) continue
    for (const m of src.matchAll(/(?:^|\n)\s*(?:import|export)\b[^'"]*?from\s+['"]([^'"]+)['"]/g)) {
      const spec = m[1]
      if (spec.startsWith('.') || /^(react|react-dom|echarts|zrender)(\/|$)/.test(spec)) continue
      bad.push(`${file}: ${spec}`)
    }
  }
  expect(Object.keys(sources).length).toBeGreaterThan(5)
  expect(bad).toEqual([])
})
