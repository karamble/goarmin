// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useQueryClient } from '@tanstack/react-query'
import { Suspense, useEffect, type ComponentType, type LazyExoticComponent } from 'react'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { onUnauthorized } from '../api/client'
import { useMe, useSetupNeeded } from '../api/hooks/auth'
import { keys } from '../api/keys'
import Invite from '../pages/auth/Invite'
import Login from '../pages/auth/Login'
import Setup from '../pages/auth/Setup'
import NotFound from '../pages/NotFound'
import { Spinner } from '../ui/kit'
import { lazyRoute } from './lazyRoute'
import { visibleSections, type PageDef, type SectionDef } from './routes'
import { SectionIndex, SectionLayout } from './section/SectionLayout'
import { ToastProvider } from '../ui/Toast'
import { PrefsProvider } from './PrefsProvider'
import { Shell } from './shell/Shell'

const lazies = new Map<() => Promise<{ default: ComponentType }>, LazyExoticComponent<ComponentType>>()
function Page({ load }: { load: PageDef['load'] }) {
  let C = lazies.get(load)
  if (!C) {
    C = lazyRoute(load)
    lazies.set(load, C)
  }
  return <C />
}

function Splash() {
  return (
    <div className="grid min-h-dvh place-items-center">
      <Spinner />
    </div>
  )
}

/** ToSignIn sends a signed-out visitor to the setup (first run) or the sign-in page. */
function ToSignIn() {
  const location = useLocation()
  const setup = useSetupNeeded()
  if (setup.isPending) return <Splash />
  if (setup.data?.needed) return <Navigate to={`/setup${location.search}`} replace />
  const next = location.pathname + location.search
  return <Navigate to={next === '/' ? '/login' : `/login?next=${encodeURIComponent(next)}`} replace />
}

function sectionRoutes(s: SectionDef) {
  if (s.pages.length === 0 && s.load) {
    return (
      <Route key={s.id} path={s.path} element={<SectionLayout section={s} />}>
        <Route index element={<Page load={s.load} />} />
      </Route>
    )
  }
  return (
    <Route key={s.id} path={s.path} element={<SectionLayout section={s} />}>
      <Route index element={<SectionIndex section={s} />} />
      {s.pages.map((p) => (
        <Route key={p.path} path={p.path} element={<Page load={p.load} />} />
      ))}
      {s.pages.filter((p) => p.detail).map((p) => (
        <Route key={p.path + '/:id'} path={`${p.path}/:id`} element={<Page load={p.detail!} />} />
      ))}
      <Route path="*" element={<Navigate to={s.path} replace />} />
    </Route>
  )
}

export function App() {
  const me = useMe()
  const qc = useQueryClient()
  // Any 401 means the session ended (expired, signed out elsewhere): back to sign-in.
  useEffect(() => onUnauthorized(() => qc.setQueryData(keys.me, null)), [qc])
  if (me.isPending) return <Splash />
  const user = me.data ?? null
  return (
    <Suspense fallback={<Splash />}>
      <Routes>
        <Route path="/login" element={user ? <Navigate to="/" replace /> : <Login />} />
        <Route path="/setup" element={user ? <Navigate to="/" replace /> : <Setup />} />
        <Route path="/invite/:token" element={user ? <Navigate to="/" replace /> : <Invite />} />
        {user ? (
          <Route element={<ToastProvider><PrefsProvider><Shell me={user} /></PrefsProvider></ToastProvider>}>
            {visibleSections(user).map(sectionRoutes)}
            <Route path="*" element={<NotFound />} />
          </Route>
        ) : (
          <Route path="*" element={<ToSignIn />} />
        )}
      </Routes>
    </Suspense>
  )
}
