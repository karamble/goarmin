// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Fragment, useMemo, type ReactNode } from 'react'
import { effective, PrefsContext, usePrefsSetting } from '../api/hooks/prefs'
import { useWatchProfile } from '../api/hooks/training'
import { setHourCycle } from '../lib/format'
import { setWeekStart } from '../lib/period'

/** PrefsProvider applies the user's preferences (with the watch's settings as defaults). The
 * week start and the clock live in lib/period and lib/format, which pages read directly, so the
 * pages remount when either changes; the rest reaches them through usePrefs. */
export function PrefsProvider({ children }: { children: ReactNode }) {
  const q = usePrefsSetting()
  const w = useWatchProfile()
  const eff = useMemo(() => effective(q.data?.value ?? null, q.data?.rev ?? 0, w.data, !q.isPending), [q.data, q.isPending, w.data])
  // Wait for both (local and quick), so the pages do not mount with the defaults first and
  // then again with the user's week and clock. A failed request falls back to the defaults.
  if (q.isPending || w.isPending) return null
  // Set before the children render, so they read the new values in this same pass.
  setWeekStart(eff.weekStart)
  setHourCycle(eff.hourCycle)
  return (
    <PrefsContext.Provider value={eff}>
      <Fragment key={`${eff.weekStart}|${eff.hourCycle ?? ''}`}>{children}</Fragment>
    </PrefsContext.Provider>
  )
}
