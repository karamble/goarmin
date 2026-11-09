// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Suspense, useState } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
import type { Me } from '../../api/types'
import { Spinner } from '../../ui/kit'
import { PaletteActionsProvider } from '../palette/actions'
import { CommandPalette } from '../palette/CommandPalette'
import { RouteErrorBoundary } from '../RouteErrorBoundary'
import { visibleSections } from '../routes'
import { load, save } from '../storage'
import { AboutHost } from './About'
import { BottomBar } from './BottomBar'
import { Rail } from './Rail'
import { useSyncRefresh } from './SyncFreshness'

/** The signed-in frame: the rail (desktop) or bottom bar (phone) around the current section. */
export function Shell({ me }: { me: Me }) {
  const [open, setOpen] = useState(() => load('rail') !== 'closed')
  const location = useLocation()
  const sections = visibleSections(me)
  useSyncRefresh()
  const toggle = () => {
    setOpen(!open)
    save('rail', open ? 'closed' : 'open')
  }
  return (
    <PaletteActionsProvider>
      <a href="#main" className="sr-only z-[80] rounded-full bg-lime px-4 py-2 text-[14px] font-bold text-bg focus:not-sr-only focus:fixed focus:left-4 focus:top-4">
        Skip to the content
      </a>
      <div className="min-h-dvh" style={{ ['--rail-w' as string]: open ? 'var(--rail-open)' : 'var(--rail)' }}>
        <Rail sections={sections} me={me} open={open} onToggle={toggle} />
        <main id="main" tabIndex={-1} className="outline-none pb-[calc(76px+env(safe-area-inset-bottom))] md:pb-0 md:pl-[var(--rail-w)] transition-[padding] duration-200">
          <RouteErrorBoundary resetKey={location.pathname}>
            <Suspense fallback={<div className="grid place-items-center py-24"><Spinner /></div>}>
              <Outlet />
            </Suspense>
          </RouteErrorBoundary>
        </main>
        <BottomBar sections={sections} me={me} />
        <CommandPalette sections={sections} />
        <AboutHost version={me.server?.version} />
      </div>
    </PaletteActionsProvider>
  )
}
