// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Per-browser conveniences only (rail state, last tab). Storage may be blocked: never rely on it.
export function load(key: string, area: 'local' | 'session' = 'local'): string | null {
  try {
    return (area === 'local' ? localStorage : sessionStorage).getItem('goarmin.' + key)
  } catch {
    return null
  }
}

export function save(key: string, value: string, area: 'local' | 'session' = 'local'): void {
  try {
    ;(area === 'local' ? localStorage : sessionStorage).setItem('goarmin.' + key, value)
  } catch {
    /* blocked */
  }
}

export function drop(key: string, area: 'local' | 'session' = 'local'): void {
  try {
    ;(area === 'local' ? localStorage : sessionStorage).removeItem('goarmin.' + key)
  } catch {
    /* blocked */
  }
}
