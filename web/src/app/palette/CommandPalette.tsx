// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Activity, CalendarCheck, CalendarSearch, CornerDownLeft, Dumbbell, Info, LayoutGrid, LogOut, Map as MapIcon, Radio, RefreshCw, Search, SlidersHorizontal, Upload, Usb, Watch } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { errorText } from '../../api/client'
import { useLogout, useMe } from '../../api/hooks/auth'
import { useDevices, useRecentActivities } from '../../api/hooks/data'
import { useSyncNow } from '../../api/hooks/devices'
import { useCourses, useWorkouts } from '../../api/hooks/training'
import { deviceName, isWatch, watchName } from '../../lib/devices'
import { fuzzyFilter } from '../../lib/fuzzy'
import { parseDay, shortDay, today as todayIn } from '../../lib/period'
import { openAbout } from '../shell/About'
import { useRegisteredActions, type PaletteItem } from './actions'
import { useToast } from '../../ui/Toast'
import type { SectionDef } from '../routes'
import { carriedSearch } from '../section/search'

type Item = PaletteItem

/** CommandPalette (Ctrl/⌘-K): jump to any page, a day, an activity, a workout or a course; run
 * the app's actions and those the open page offers. */
export function CommandPalette({ sections }: { sections: SectionDef[] }) {
  const [open, setOpen] = useState(false)
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setOpen((o) => !o)
      }
    }
    const show = () => setOpen(true)
    window.addEventListener('keydown', on)
    window.addEventListener('goarmin:palette', show)
    return () => {
      window.removeEventListener('keydown', on)
      window.removeEventListener('goarmin:palette', show)
    }
  }, [])
  return open ? <Palette sections={sections} onClose={() => setOpen(false)} /> : null
}

/** openPalette opens the palette from a button. */
export function openPalette() {
  window.dispatchEvent(new Event('goarmin:palette'))
}

function Palette({ sections, onClose }: { sections: SectionDef[]; onClose: () => void }) {
  const navigate = useNavigate()
  const location = useLocation()
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const input = useRef<HTMLInputElement>(null)
  const back = useRef<HTMLElement | null>(document.activeElement as HTMLElement | null)
  const devices = useDevices().data
  const syncNow = useSyncNow()
  const toast = useToast()
  const me = useMe().data
  const logout = useLogout()
  const registered = useRegisteredActions()
  // Loaded only while the palette is open (this component exists only then).
  const recent = useRecentActivities(100).data?.items
  const workouts = useWorkouts().data
  const courses = useCourses().data

  const items = useMemo<Item[]>(() => {
    const out: Item[] = []
    for (const s of sections) {
      if (s.pages.length === 0) {
        out.push({ id: s.id, label: s.label, icon: s.icon, run: () => navigate(s.path) })
        continue
      }
      for (const p of s.pages) {
        out.push({
          id: `${s.id}/${p.path}`, label: `${s.label} › ${p.label}`, hint: p.title, icon: s.icon,
          run: () => navigate(`${s.path}/${p.path}${carriedSearch(location.search, p)}`),
        })
      }
    }
    for (const d of devices ?? []) {
      if (!d.current) continue
      const name = deviceName(d)
      if (d.sync && isWatch(d)) {
        out.push({
          id: `sync-${d.id}`, label: `Sync ${name} now`, icon: RefreshCw,
          run: () => syncNow.mutate({ id: d.id }, {
            onSuccess: () => toast({ text: `Syncing ${name}…` }),
            onError: (e) => toast({ tone: 'error', text: errorText(e) }),
          }),
        })
      }
      out.push({ id: `watch-${d.id}`, label: `Open ${name}`, hint: 'Devices', icon: Watch, run: () => navigate(`/devices/watches/${d.id}`) })
    }
    const syncing = (devices ?? []).filter((d) => d.current && d.sync)
    if (me?.server.sync) {
      for (const d of syncing) {
        out.push({ id: `live-${d.id}`, label: `Go live with ${watchName(d)}`, icon: Radio, run: () => navigate(`/live?watch=${d.id}`) })
      }
      if (syncing.length > 1) {
        out.push({
          id: 'sync-all', label: 'Sync all watches now', icon: RefreshCw,
          run: () => syncing.forEach((d) => syncNow.mutate({ id: d.id }, { onError: (e) => toast({ tone: 'error', text: errorText(e) }) })),
        })
      }
    }
    out.push({ id: 'import-files', label: 'Import files', hint: 'Devices › Import', icon: Upload, run: () => navigate('/devices/import') })
    out.push({ id: 'import-usb', label: 'Import from the watch on USB', hint: 'Devices › Import', icon: Usb, run: () => navigate('/devices/import#usb') })
    out.push({ id: 'customize', label: 'Customize the glance', icon: LayoutGrid, run: () => navigate('/?edit=1') })
    out.push({ id: 'plan', label: 'Plan a workout', hint: 'Calendar', icon: CalendarCheck, run: () => navigate('/calendar?plan=1') })
    out.push({ id: 'prefs', label: 'Preferences', hint: 'Settings › Profile', icon: SlidersHorizontal, run: () => navigate('/settings/profile') })
    out.push({ id: 'about', label: 'About goarmin', hint: 'source and disclaimer', icon: Info, run: openAbout })
    out.push({ id: 'logout', label: 'Sign out', icon: LogOut, run: () => logout.mutate() })
    out.push(...registered)
    for (const a of recent ?? []) {
      out.push({ id: `act-${a.id}`, label: a.name || a.sport_label, hint: `${a.sport_label} · ${shortDay(a.day)}`, icon: Activity, run: () => navigate(`/training/activities/${a.id}`) })
    }
    for (const w of workouts ?? []) {
      out.push({ id: `wkt-${w.id}`, label: w.name, hint: 'Workout', icon: Dumbbell, run: () => navigate(`/training/workouts/${w.id}`) })
    }
    for (const c of courses ?? []) {
      out.push({ id: `crs-${c.id}`, label: c.name, hint: 'Course', icon: MapIcon, run: () => navigate(`/training/courses/${c.id}`) })
    }
    out.push({
      id: 'today', label: 'Go to today', icon: CalendarCheck,
      run: () => {
        const q = new URLSearchParams(location.search)
        q.delete('d')
        const s = q.toString()
        navigate(location.pathname + (s ? '?' + s : ''))
      },
    })
    return out
  }, [sections, navigate, location, devices, syncNow, toast, me, logout, registered, recent, workouts, courses])

  // A typed day ("yesterday", "3 days ago", "2026-10-03") goes first: that day on this page,
  // or on the glance for a page without days.
  const day = parseDay(query, todayIn(me?.timezone ?? ''))
  const dayItem: Item[] = day ? [{
    id: 'goto-day', label: `Go to ${shortDay(day)}`, hint: day, icon: CalendarSearch,
    run: () => {
      // This page if it has days (its periods), else the glance.
      const seg = location.pathname.replace(/\/+$/, '').split('/')
      const section = sections.find((x) => x.path === '/' + (seg[1] ?? ''))
      const page = section?.pages.find((x) => x.path === seg[2])
      const periods = seg.length > 3 ? [] : (page?.periods ?? section?.periods ?? [])
      const dated = periods.some((x) => x !== 'all')
      const q = new URLSearchParams(dated ? location.search : '')
      q.set('d', day)
      navigate((dated ? location.pathname : '/') + '?' + q.toString())
    },
  }] : []
  const shown = [...dayItem, ...fuzzyFilter(query, items, (x) => x.label + ' ' + (x.hint ?? ''))].slice(0, 12)
  const sel = Math.min(active, Math.max(shown.length - 1, 0))

  const close = () => {
    onClose()
    back.current?.focus?.()
  }
  const run = (x: Item | undefined) => {
    if (!x) return
    onClose()
    x.run()
  }

  useEffect(() => input.current?.focus(), [])

  return (
    <div className="fixed inset-0 z-[70] grid items-start justify-items-center bg-black/55 px-4 pt-[12vh] backdrop-blur-[2px]" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div role="dialog" aria-modal="true" aria-label="Go to" className="card-glass w-full max-w-[560px] overflow-hidden p-0">
        <div className="flex items-center gap-3 border-b border-border px-4">
          <Search size={18} className="shrink-0 text-faint" aria-hidden />
          <input
            ref={input}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setActive(0)
            }}
            onKeyDown={(e) => {
              if (e.key === 'ArrowDown') {
                e.preventDefault()
                setActive((sel + 1) % Math.max(shown.length, 1))
              } else if (e.key === 'ArrowUp') {
                e.preventDefault()
                setActive((sel - 1 + shown.length) % Math.max(shown.length, 1))
              } else if (e.key === 'Enter') {
                e.preventDefault()
                run(shown[sel])
              } else if (e.key === 'Escape') {
                e.preventDefault()
                close()
              }
            }}
            role="combobox"
            aria-expanded="true"
            aria-controls="palette-list"
            aria-activedescendant={shown[sel] ? 'palette-' + shown[sel].id : undefined}
            placeholder="Go to a page, a day, an activity…"
            className="h-14 w-full bg-transparent text-[16px] text-text outline-none placeholder:text-faint"
          />
          <kbd className="font-mono text-[10px] uppercase tracking-[.14em] text-faint">Esc</kbd>
        </div>
        <ul id="palette-list" role="listbox" aria-label="Pages" className="m-0 max-h-[50vh] list-none overflow-y-auto p-2">
          {shown.length === 0 && <li className="px-3 py-6 text-center text-sm text-faint">Nothing matches “{query}”</li>}
          {shown.map((x, i) => (
            <li
              key={x.id}
              id={'palette-' + x.id}
              role="option"
              aria-selected={i === sel}
              onMouseMove={() => setActive(i)}
              onClick={() => run(x)}
              className={`flex cursor-pointer items-center gap-3 rounded-xl px-3 py-2.5 text-[14px] ${i === sel ? 'bg-lime-fill text-lime' : 'text-text'}`}
            >
              <x.icon size={17} strokeWidth={1.75} className={i === sel ? 'text-lime' : 'text-faint'} aria-hidden />
              <span className="flex-1 truncate">{x.label}</span>
              {x.hint && <span className="truncate text-[12px] text-faint">{x.hint}</span>}
              {i === sel && <CornerDownLeft size={14} aria-hidden />}
            </li>
          ))}
        </ul>
      </div>
    </div>
  )
}
