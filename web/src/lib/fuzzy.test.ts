// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import { fuzzyFilter, fuzzyScore } from './fuzzy'

describe('fuzzy', () => {
  it('matches characters in order', () => {
    expect(fuzzyScore('slp', 'Health › Sleep')).not.toBeNull()
    expect(fuzzyScore('pls', 'Health › Sleep')).toBeNull()
    expect(fuzzyScore('', 'anything')).toBe(0)
  })

  it('ranks word starts and runs first', () => {
    const items = ['Training › Performance', 'Health › Heart', 'Health › HRV']
    expect(fuzzyFilter('hrv', items, (x) => x)[0]).toBe('Health › HRV')
    expect(fuzzyFilter('heart', items, (x) => x)[0]).toBe('Health › Heart')
    expect(fuzzyFilter('perf', items, (x) => x)).toEqual(['Training › Performance'])
  })
})
