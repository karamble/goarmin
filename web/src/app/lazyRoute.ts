// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { lazy, type ComponentType } from 'react'

const flag = 'goarmin.chunkReload'

/**
 * lazyRoute loads a page module on first use. Chunks are content-hashed and replaced on upgrade,
 * so a long-open tab can ask for one that no longer exists (the server answers 404 for missing
 * assets): reload once to pick up the new build.
 */
export function lazyRoute(load: () => Promise<{ default: ComponentType }>) {
  return lazy(() =>
    load().then(
      (m) => {
        try { sessionStorage.removeItem(flag) } catch { /* storage blocked */ }
        return m
      },
      (err) => {
        let reloaded = false
        try { reloaded = sessionStorage.getItem(flag) === '1'; sessionStorage.setItem(flag, '1') } catch { reloaded = true }
        if (!reloaded) window.location.reload()
        throw err
      },
    ),
  )
}
