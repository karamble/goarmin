// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { cleanup, configure } from '@testing-library/react'
import { afterEach } from 'vitest'

// Browser APIs jsdom lacks, as inert stand-ins.
class Inert {
  observe() {}
  unobserve() {}
  disconnect() {}
}
const g = globalThis as Record<string, unknown>
g.ResizeObserver ??= Inert
g.IntersectionObserver ??= Inert
if (typeof window !== 'undefined' && !window.matchMedia) {
  window.matchMedia = (q: string) =>
    ({ matches: false, media: q, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, onchange: null, dispatchEvent: () => false }) as MediaQueryList
}

// Testing Library unmounts after each test only with vitest globals; do it explicitly.
afterEach(() => cleanup())

// Lazy chunks load slower when every test file runs at once on a busy laptop; findBy* waits up
// to 10 s (the dom tests' own timeout is 15 s, so a missing element is still reported as such).
configure({ asyncUtilTimeout: 10_000 })
