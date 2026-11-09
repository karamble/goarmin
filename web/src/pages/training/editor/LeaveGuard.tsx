// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useContext, useEffect, useRef } from 'react'
import { UNSAFE_DataRouterContext, useBlocker } from 'react-router-dom'
import { Confirm } from '../../../ui/Sheet'

/** LeaveGuard asks before a page with unsaved changes is left: inside the app (a link, Back)
 * through the data router, and when the tab closes through the browser's own question. */
export function LeaveGuard({ when, text }: { when: boolean; text: string }) {
  useEffect(() => {
    if (!when) return
    const ask = (e: BeforeUnloadEvent) => {
      e.preventDefault()
      e.returnValue = ''
    }
    window.addEventListener('beforeunload', ask)
    return () => window.removeEventListener('beforeunload', ask)
  }, [when])
  // useBlocker needs a data router (the app has one; a test may not).
  return useContext(UNSAFE_DataRouterContext) ? <Blocker when={when} text={text} /> : null
}

function Blocker({ when, text }: { when: boolean; text: string }) {
  const blocker = useBlocker(({ currentLocation, nextLocation }) => when && currentLocation.pathname !== nextLocation.pathname)
  const leaving = useRef(false)
  if (blocker.state !== 'blocked') return null
  return (
    <Confirm title="Leave without saving?" confirm="Leave" danger
      onConfirm={() => { leaving.current = true; blocker.proceed?.() }}
      onClose={() => { if (!leaving.current) blocker.reset?.(); leaving.current = false }}>
      {text}
    </Confirm>
  )
}
