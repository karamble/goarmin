// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import { fromB64url, toB64url } from './b64url'

describe('b64url', () => {
  it('round-trips every length without padding', () => {
    for (let n = 0; n < 40; n++) {
      const a = new Uint8Array(n).map((_, i) => (i * 37 + n) & 0xff)
      const s = toB64url(a)
      expect(s).not.toMatch(/[+/=]/)
      expect(new Uint8Array(fromB64url(s))).toEqual(a)
    }
  })
  it('matches a known vector', () => {
    expect(toB64url(new Uint8Array([0xfb, 0xff, 0xbf]))).toBe('-_-_')
  })
})
