// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Info, LogOut, MoreHorizontal, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { useLogout } from '../../api/hooks/auth'
import type { Me } from '../../api/types'
import type { SectionDef } from '../routes'
import { openAbout } from './About'

/** The phone bar: the main sections plus More (a sheet with the rest and sign out). */
export function BottomBar({ sections, me }: { sections: SectionDef[]; me: Me }) {
  const [more, setMore] = useState(false)
  const location = useLocation()
  const logout = useLogout()
  useEffect(() => setMore(false), [location.pathname])
  const bar = sections.filter((s) => s.bar).slice(0, 4)
  const rest = sections.filter((s) => !bar.includes(s))
  const restActive = rest.some((s) => location.pathname.startsWith(s.path))
  const item = 'flex min-h-12 flex-col items-center justify-center gap-1 rounded-xl text-[11px] font-medium'
  return (
    <>
      {more && (
        <div className="fixed inset-0 z-40 bg-black/60 md:hidden" onClick={() => setMore(false)}>
          <div
            role="dialog"
            aria-label="More sections"
            onClick={(e) => e.stopPropagation()}
            className="absolute inset-x-0 bottom-[calc(68px+env(safe-area-inset-bottom))] rounded-t-[18px] border border-border bg-panel p-4 shadow-[0_-12px_48px_#0008]"
          >
            <div className="mb-3 flex items-center justify-between">
              <span className="caps">{me.display_name || me.email}</span>
              <button type="button" aria-label="Close" onClick={() => setMore(false)} className="text-muted"><X size={18} /></button>
            </div>
            <ul className="m-0 grid list-none gap-1 p-0">
              {rest.map((s) => (
                <li key={s.id}>
                  <NavLink to={s.path} className={({ isActive }) => `flex h-12 items-center gap-3 rounded-xl px-3 ${isActive ? 'bg-lime-fill text-lime' : 'text-text'}`}>
                    <s.icon size={20} strokeWidth={1.75} /> {s.label}
                  </NavLink>
                </li>
              ))}
              <li>
                <button type="button" onClick={() => { setMore(false); openAbout() }} className="flex h-12 w-full items-center gap-3 rounded-xl px-3 text-muted">
                  <Info size={20} strokeWidth={1.75} /> About
                </button>
              </li>
              <li>
                <button type="button" onClick={() => logout.mutate()} className="flex h-12 w-full items-center gap-3 rounded-xl px-3 text-muted">
                  <LogOut size={20} strokeWidth={1.75} /> Sign out
                </button>
              </li>
            </ul>
          </div>
        </div>
      )}
      <nav
        aria-label="Sections"
        className="fixed inset-x-0 bottom-0 z-50 grid grid-cols-5 gap-1 border-t border-border bg-bg2/95 px-2 pt-2 pb-[calc(8px+env(safe-area-inset-bottom))] shadow-[0_-4px_24px_#0005] backdrop-blur md:hidden"
        style={{ gridTemplateColumns: `repeat(${bar.length + 1}, minmax(0, 1fr))` }}
      >
        {bar.map((s) => (
          <NavLink key={s.id} to={s.path} end={s.path === '/'} className={({ isActive }) => `${item} ${isActive ? 'bg-lime-fill text-lime' : 'text-muted'}`}>
            <s.icon size={20} strokeWidth={1.75} />
            <span className="max-w-full truncate">{s.id === 'glance' ? 'Glance' : s.label}</span>
          </NavLink>
        ))}
        <button type="button" onClick={() => setMore(!more)} aria-expanded={more} className={`${item} ${more || restActive ? 'text-lime' : 'text-muted'}`}>
          <MoreHorizontal size={20} strokeWidth={1.75} />
          More
        </button>
      </nav>
    </>
  )
}
