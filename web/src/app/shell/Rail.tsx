// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { LogOut, PanelLeftClose, PanelLeftOpen, Search } from 'lucide-react'
import { NavLink } from 'react-router-dom'
import { useLogout } from '../../api/hooks/auth'
import type { Me } from '../../api/types'
import { Mark } from '../../ui/kit'
import { openPalette } from '../palette/CommandPalette'
import type { SectionDef } from '../routes'
import { SyncFreshness } from './SyncFreshness'

/** The desktop rail: brand, sections (icon + label, collapsible to icons), the account. */
export function Rail({ sections, me, open, onToggle }: { sections: SectionDef[]; me: Me; open: boolean; onToggle: () => void }) {
  const logout = useLogout()
  return (
    <nav
      aria-label="Sections"
      className="fixed inset-y-0 left-0 z-40 hidden md:flex w-[var(--rail-w)] flex-col border-r border-border bg-bg2/95 transition-[width] duration-200"
    >
      <div className="flex h-[72px] items-center gap-3 px-[26px]">
        <Mark />
        {open && <span className="font-display text-[19px] font-semibold uppercase tracking-[.12em]">goarmin</span>}
      </div>
      <ul className="m-0 flex flex-1 list-none flex-col gap-1 p-3">
        {sections.map((s) => (
          <li key={s.id}>
            <NavLink
              to={s.path}
              end={s.path === '/'}
              title={open ? undefined : s.label}
              className={({ isActive }) =>
                `group relative flex h-11 items-center gap-3 rounded-xl px-[17px] text-[14px] font-medium transition-colors ${
                  isActive ? 'bg-lime-fill text-lime' : 'text-muted hover:bg-panel hover:text-text'
                }`
              }
            >
              {({ isActive }) => (
                <>
                  {isActive && <span aria-hidden className="absolute left-0 top-2 bottom-2 w-[3px] rounded-r bg-lime shadow-[0_0_12px_var(--lime)]" />}
                  <s.icon size={20} strokeWidth={1.75} className="shrink-0" />
                  {open && <span className="truncate">{s.label}</span>}
                </>
              )}
            </NavLink>
          </li>
        ))}
      </ul>
      <div className="flex flex-col gap-1 p-3 pt-0">
        <button
          type="button"
          onClick={openPalette}
          title={open ? undefined : 'Go to… (Ctrl K)'}
          className={`flex h-10 items-center gap-3 rounded-xl px-[17px] text-[13px] text-muted hover:bg-panel hover:text-text`}
        >
          <Search size={18} strokeWidth={1.75} className="shrink-0" />
          {open && (
            <>
              <span className="flex-1 text-left">Go to…</span>
              <kbd className="rounded border border-border px-1.5 py-0.5 font-mono text-[10px] text-faint">{navigator.platform.startsWith('Mac') ? '⌘' : 'Ctrl'} K</kbd>
            </>
          )}
        </button>
        <SyncFreshness open={open} />
      </div>
      <div className="flex flex-col gap-1 border-t border-border p-3">
        <div className={`flex items-center gap-3 px-[13px] py-2 ${open ? '' : 'justify-center px-0'}`} title={me.email}>
          <span className="grid h-7 w-7 shrink-0 place-items-center rounded-full border border-lime-dim font-mono text-[12px] text-lime">
            {(me.display_name || me.email).slice(0, 1).toUpperCase()}
          </span>
          {open && <span className="min-w-0 truncate text-[13px] text-muted">{me.display_name || me.email}</span>}
        </div>
        <button
          type="button"
          onClick={() => logout.mutate()}
          className="flex h-10 items-center gap-3 rounded-xl px-[17px] text-[13px] text-muted hover:bg-panel hover:text-text"
          title="Sign out"
        >
          <LogOut size={18} strokeWidth={1.75} /> {open && 'Sign out'}
        </button>
        <button
          type="button"
          onClick={onToggle}
          aria-label={open ? 'Collapse the rail' : 'Expand the rail'}
          className="flex h-10 items-center gap-3 rounded-xl px-[17px] text-[13px] text-faint hover:bg-panel hover:text-text"
        >
          {open ? <PanelLeftClose size={18} strokeWidth={1.75} /> : <PanelLeftOpen size={18} strokeWidth={1.75} />}
          {open && 'Collapse'}
        </button>
      </div>
    </nav>
  )
}
