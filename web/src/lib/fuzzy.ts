// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/**
 * fuzzyScore matches the query's characters in order within text (case-insensitive) and scores
 * the match: word starts and runs score higher, gaps lower. null means no match; "" matches all.
 */
export function fuzzyScore(query: string, text: string): number | null {
  const q = query.trim().toLowerCase()
  if (!q) return 0
  const t = text.toLowerCase()
  let score = 0
  let ti = 0
  let prev = -2
  for (const ch of q) {
    if (ch === ' ') continue
    const i = t.indexOf(ch, ti)
    if (i < 0) return null
    const start = i === 0 || /[\s›/\-_&]/.test(t[i - 1])
    score += 1 + (start ? 3 : 0) + (i === prev + 1 ? 2 : 0) - Math.min(i - ti, 5) * 0.2
    prev = i
    ti = i + 1
  }
  return score - t.length * 0.01
}

/** fuzzyFilter keeps the items that match, best first (stable for equal scores). */
export function fuzzyFilter<T>(query: string, items: T[], text: (x: T) => string): T[] {
  return items
    .map((x, i) => ({ x, i, s: fuzzyScore(query, text(x)) }))
    .filter((r) => r.s != null)
    .sort((a, b) => (b.s as number) - (a.s as number) || a.i - b.i)
    .map((r) => r.x)
}
