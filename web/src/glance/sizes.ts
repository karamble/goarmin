// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

export type CardSize = 'S' | 'M' | 'L' | 'W'

export const allSizes: readonly CardSize[] = ['S', 'M', 'L', 'W']

/**
 * The grid has 12/6/2 columns at ≥1280/≥768/smaller with 148 px rows:
 * S = 3/2/1 × 1 row, M = 4/3/2 × 2, L = 6/6/2 × 2, W = 12/6/2 × 2.
 */
export const sizeClass: Record<CardSize, string> = {
  S: 'col-span-1 md:col-span-2 xl:col-span-3 row-span-1',
  M: 'col-span-2 md:col-span-3 xl:col-span-4 row-span-2',
  L: 'col-span-2 md:col-span-6 xl:col-span-6 row-span-2',
  W: 'col-span-2 md:col-span-6 xl:col-span-12 row-span-2',
}

export const sizeName: Record<CardSize, string> = { S: 'Small', M: 'Medium', L: 'Large', W: 'Wide' }

/** nearestSize is the allowed size closest to want (smaller first on a tie). */
export function nearestSize(want: CardSize, allowed: readonly CardSize[]): CardSize {
  if (allowed.includes(want)) return want
  const i = allSizes.indexOf(want)
  for (let d = 1; d < allSizes.length; d++) {
    for (const j of [i - d, i + d]) {
      const s = allSizes[j]
      if (s && allowed.includes(s)) return s
    }
  }
  return allowed[0] ?? 'M'
}
