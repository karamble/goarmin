// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/** A point list for a time axis: [milliseconds, value | null]. null breaks the line. */
export type Points = [number, number | null][]

/**
 * points pairs Unix seconds with values, inserting a null wherever two neighbours are more than
 * gapS apart, so a line never bridges time the watch was not worn.
 */
export function points(t: number[], v: (number | null)[], gapS = 0): Points {
  const out: Points = []
  for (let i = 0; i < t.length; i++) {
    if (gapS > 0 && i > 0 && t[i] - t[i - 1] > gapS) out.push([(t[i - 1] + 1) * 1000, null])
    out.push([t[i] * 1000, v[i] ?? null])
  }
  return out
}

/** stats over the non-null values. */
export function stats(v: (number | null)[]): { min: number; max: number; avg: number; n: number } | null {
  let min = Infinity
  let max = -Infinity
  let sum = 0
  let n = 0
  for (const x of v) {
    if (x == null || !Number.isFinite(x)) continue
    min = Math.min(min, x)
    max = Math.max(max, x)
    sum += x
    n++
  }
  return n ? { min, max, avg: sum / n, n } : null
}
