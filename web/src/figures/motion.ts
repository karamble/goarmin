// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useEffect, useState } from 'react'

/** Seconds a loop takes to move from one key pose to the next, and how long it rests on each. */
export const MOVE = 0.9
export const DWELL = 0.55

/**
 * blendAt says which two key poses to show at time t (seconds) and how far between them (0..1,
 * eased). One key is a hold; several loop in order and back to the first.
 */
export function blendAt(count: number, t: number, move = MOVE, dwell = DWELL): { a: number; b: number; u: number } {
  if (count <= 1) return { a: 0, b: 0, u: 0 }
  const seg = move + dwell
  const x = ((t % (seg * count)) + seg * count) % (seg * count)
  const a = Math.min(count - 1, Math.floor(x / seg))
  const r = x - a * seg
  const lin = r < dwell ? 0 : Math.min(1, (r - dwell) / move)
  return { a, b: (a + 1) % count, u: lin * lin * (3 - 2 * lin) }
}

const query = '(prefers-reduced-motion: reduce)'

/** useReducedMotion follows the system setting live. */
export function useReducedMotion(): boolean {
  const [reduced, setReduced] = useState(() => typeof window !== 'undefined' && !!window.matchMedia?.(query).matches)
  useEffect(() => {
    const mq = window.matchMedia?.(query)
    if (!mq) return
    const on = () => setReduced(mq.matches)
    mq.addEventListener?.('change', on)
    return () => mq.removeEventListener?.('change', on)
  }, [])
  return reduced
}

/** hasWebGL tells whether this browser can draw the figure. */
export function hasWebGL(): boolean {
  try {
    const c = document.createElement('canvas')
    return !!(c.getContext('webgl2') || c.getContext('webgl'))
  } catch {
    return false
  }
}
