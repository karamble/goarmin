// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useEffect, useRef } from 'react'

/**
 * ignoreKey tells whether a plain key press belongs to something else: typing, an open dialog,
 * a drag, a widget that uses arrows, or a held modifier.
 */
export function ignoreKey(e: KeyboardEvent): boolean {
  if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return true
  const t = e.target as HTMLElement | null
  if (t?.closest?.('input, textarea, select, [contenteditable=""], [contenteditable="true"], [role="slider"], [role="listbox"], [role="menu"]')) return true
  if (document.querySelector('dialog[open], [aria-modal="true"]')) return true
  if (document.body.dataset.dragging || document.body.dataset.editing) return true
  return false
}

/** useHotkeys binds single keys (KeyboardEvent.key, case-insensitive for letters) while mounted. */
export function useHotkeys(map: Record<string, () => void>, enabled = true) {
  const ref = useRef(map)
  ref.current = map
  useEffect(() => {
    if (!enabled) return
    const on = (e: KeyboardEvent) => {
      if (e.repeat && e.key.length === 1) return
      if (ignoreKey(e)) return
      const fn = ref.current[e.key] ?? ref.current[e.key.toLowerCase()]
      if (!fn) return
      e.preventDefault()
      fn()
    }
    window.addEventListener('keydown', on)
    return () => window.removeEventListener('keydown', on)
  }, [enabled])
}
