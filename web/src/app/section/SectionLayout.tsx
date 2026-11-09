// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Navigate, NavLink, Outlet, useLocation } from 'react-router-dom'
import type { PageDef, SectionDef } from '../routes'
import { DateNavigator } from './DateNavigator'
import { HeaderSlotContext, SectionTitleContext } from './HeaderSlot'
import { carriedSearch } from './search'
import { DefaultPeriodContext, FutureContext, PeriodsContext } from './usePeriod'
import { load, save } from '../storage'

/** SectionIndex sends a bare section URL to its last-used sub-section, else the first. */
export function SectionIndex({ section }: { section: SectionDef }) {
  const location = useLocation()
  const last = load('tab.' + section.id, 'session')
  const page = section.pages.find((p) => p.path === last && !p.hidden) ?? section.pages.find((p) => !p.hidden) ?? section.pages[0]
  return <Navigate to={`${section.path}/${page.path}${carriedSearch(location.search, page)}`} replace />
}

/** SectionHeader is the sticky glass header: eyebrow, title, the right-hand slot, the tab strip. */
export function SectionHeader({ section, page, right, slot, titleOverride }: {
  section: SectionDef
  page?: PageDef
  right?: ReactNode
  slot?: (el: HTMLDivElement | null) => void
  titleOverride?: string
}) {
  const location = useLocation()
  const sentinel = useRef<HTMLDivElement>(null)
  const [condensed, setCondensed] = useState(false)
  useEffect(() => {
    const el = sentinel.current
    if (!el || typeof IntersectionObserver === 'undefined') return
    const io = new IntersectionObserver(([e]) => setCondensed(!e.isIntersecting))
    io.observe(el)
    return () => io.disconnect()
  }, [])
  const title = titleOverride || page?.title || page?.label || section.label
  return (
    <>
      <div ref={sentinel} aria-hidden className="h-px" />
      <header className="glass sticky top-0 z-30 border-b border-border">
        <div className={`mx-auto flex max-w-[1480px] flex-wrap items-end justify-between gap-x-6 gap-y-3 px-4 sm:px-8 transition-[padding] duration-200 ${condensed ? 'pt-3' : 'pt-6'}`}>
          <div className="min-w-0">
            <p className={`eyebrow m-0 transition-[margin] ${condensed ? 'mb-1' : 'mb-2'}`}>{section.label}</p>
            <h1 className={`title m-0 truncate transition-[font-size] duration-200 ${condensed ? '!text-[22px]' : ''}`}>{title}</h1>
          </div>
          <div className="flex flex-wrap items-center gap-2 pb-1">
            {right}
            <div ref={slot} className="flex flex-wrap items-center gap-2 empty:hidden" />
          </div>
        </div>
        {section.pages.length > 0 && (
          <nav aria-label={`${section.label} sections`} className="mx-auto max-w-[1480px] px-4 sm:px-8">
            <ul className="scroll-x m-0 flex list-none gap-1 p-0 pt-3 md:gap-6 md:pt-4">
              {section.pages.filter((p) => !p.hidden).map((p) => (
                <li key={p.path} className="shrink-0">
                  <NavLink
                    to={`${section.path}/${p.path}${carriedSearch(location.search, p)}`}
                    onClick={() => save('tab.' + section.id, p.path, 'session')}
                    className={({ isActive }) =>
                      // Phone: pills; desktop: an underline strip (minirack .nav-active).
                      `relative block whitespace-nowrap px-3.5 py-2 text-[14px] font-medium transition-colors rounded-full md:rounded-none md:px-0 md:pb-3 md:pt-1 ${
                        isActive
                          ? 'bg-lime-fill text-lime md:bg-transparent md:text-text md:after:absolute md:after:inset-x-0 md:after:bottom-0 md:after:h-[2px] md:after:bg-lime md:after:shadow-[0_0_10px_var(--lime-glow)]'
                          : 'text-muted hover:text-text'
                      }`
                    }
                  >
                    {p.label}
                  </NavLink>
                </li>
              ))}
            </ul>
          </nav>
        )}
        {section.pages.length === 0 && <div className="h-4" />}
      </header>
    </>
  )
}

/** SectionLayout renders a section's header above its current page. */
export function SectionLayout({ section }: { section: SectionDef }) {
  const location = useLocation()
  const parts = location.pathname.replace(/\/+$/, '').split('/')
  const sub = parts[2]
  const page = section.pages.find((p) => p.path === sub)
  // A page for one item (an activity, a watch) has no date of its own.
  const periods = parts.length > 3 ? [] : (page?.periods ?? section.periods ?? [])
  // While a page is being arranged (?edit=1) the date stays put.
  const editing = new URLSearchParams(location.search).get('edit') === '1'
  const [slot, setSlot] = useState<HTMLDivElement | null>(null)
  const [title, setTitle] = useState('')
  return (
    <PeriodsContext.Provider value={periods}>
      <FutureContext.Provider value={!!section.future}>
      <DefaultPeriodContext.Provider value={page?.defaultPeriod}>
      <SectionHeader section={section} page={page} right={periods.length > 0 && !editing ? <DateNavigator /> : undefined}
        slot={setSlot} titleOverride={title} />
      <HeaderSlotContext.Provider value={slot}>
        <SectionTitleContext.Provider value={setTitle}>
          <div className="mx-auto max-w-[1480px] px-4 py-6 sm:px-8 sm:py-8">
            <Outlet />
          </div>
        </SectionTitleContext.Provider>
      </HeaderSlotContext.Provider>
      </DefaultPeriodContext.Provider>
      </FutureContext.Provider>
    </PeriodsContext.Provider>
  )
}
