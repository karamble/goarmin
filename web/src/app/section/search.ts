// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { PageDef } from '../routes'

/** The query parameters a sub-section link keeps: the date and, if the target allows it, the period. */
export function carriedSearch(search: string, target: PageDef): string {
  const q = new URLSearchParams(search)
  const out = new URLSearchParams()
  const d = q.get('d')
  const p = q.get('p')
  if (d) out.set('d', d)
  if (p && target.periods?.includes(p as never)) out.set('p', p)
  const s = out.toString()
  return s ? '?' + s : ''
}
