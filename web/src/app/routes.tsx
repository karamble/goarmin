// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { canPair } from '../lib/devices'
import {
  Activity, HeartPulse, LayoutGrid, Radio, Settings, Watch, type LucideIcon,
} from 'lucide-react'
import type { ComponentType } from 'react'
import type { Me } from '../api/types'
import type { Period } from '../lib/period'

export type { Period }

export interface PageDef {
  path: string
  label: string
  /** The header title when it differs from the tab label. */
  title?: string
  periods?: Period[]
  /** The period when the URL names none (default: the first of periods). */
  defaultPeriod?: Period
  requires?: 'sync' | 'pair' | 'admin'
  load: () => Promise<{ default: ComponentType }>
  /** A page for one item under it (`path/:id`), e.g. a watch under Watches. */
  detail?: () => Promise<{ default: ComponentType }>
  /** Routed but not in the tab strip (reached from a button: Add a watch). */
  hidden?: boolean
}

export interface SectionDef {
  id: string
  path: string
  label: string
  icon: LucideIcon
  requires?: 'sync' | 'pair' | 'admin'
  /** Sub-sections shown as the tab strip; a section without pages renders `load` itself. */
  pages: PageDef[]
  load?: () => Promise<{ default: ComponentType }>
  /** The periods of a section without pages (its date navigator). */
  periods?: Period[]
  /** On the phone's bottom bar (the rest go to More). */
  bar?: boolean
}

const health: Period[] = ['day', 'week', 'month', 'year']

/** The single registry: rail, bottom bar, tab strips, the router and the palette read it. */
export const sections: SectionDef[] = [
  { id: 'glance', path: '/', label: 'At a glance', icon: LayoutGrid, pages: [], load: () => import('../glance/GlancePage'), periods: ['day'], bar: true },
  {
    id: 'health', path: '/health', label: 'Health', icon: HeartPulse, bar: true,
    pages: [
      { path: 'sleep', label: 'Sleep', periods: health, load: () => import('../pages/health/Sleep') },
      { path: 'hrv', label: 'HRV', periods: health, load: () => import('../pages/health/Hrv') },
      { path: 'heart', label: 'Heart', periods: health, load: () => import('../pages/health/Heart') },
      { path: 'energy', label: 'Energy', title: 'Stress & Body Battery', periods: health, load: () => import('../pages/health/Energy') },
      { path: 'breathing', label: 'Breathing', periods: health, load: () => import('../pages/health/Breathing') },
      { path: 'movement', label: 'Movement', periods: health, load: () => import('../pages/health/Movement') },
      { path: 'body', label: 'Body', periods: health, load: () => import('../pages/health/Body') },
    ],
  },
  {
    id: 'training', path: '/training', label: 'Training', icon: Activity, bar: true,
    pages: [
      {
        path: 'activities', label: 'Activities', periods: ['all', 'week', 'month', 'year'],
        load: () => import('../pages/training/Activities'), detail: () => import('../pages/training/Activity'),
      },
      { path: 'performance', label: 'Performance', periods: ['all', 'week', 'month', 'year'], defaultPeriod: 'month', load: () => import('../pages/training/Performance') },
      { path: 'coach', label: 'Coach', periods: ['week'], load: () => import('../pages/training/Coach') },
      { path: 'workouts', label: 'Workouts', load: () => import('../pages/training/Workouts'), detail: () => import('../pages/training/Workout') },
      { path: 'courses', label: 'Courses', load: () => import('../pages/training/Courses'), detail: () => import('../pages/training/Course') },
      { path: 'records', label: 'Records', load: () => import('../pages/training/Records') },
      { path: 'gear', label: 'Gear', load: () => import('../pages/training/Gear') },
    ],
  },
  { id: 'live', path: '/live', label: 'Live', icon: Radio, requires: 'sync', pages: [], load: () => import('../pages/live/Live'), bar: true },
  {
    id: 'devices', path: '/devices', label: 'Devices', icon: Watch,
    pages: [
      { path: 'watches', label: 'Devices', load: () => import('../pages/devices/Watches'), detail: () => import('../pages/devices/Watch') },
      { path: 'log', label: 'Sync log', load: () => import('../pages/devices/SyncLog') },
      { path: 'import', label: 'Import', load: () => import('../pages/devices/Import') },
      { path: 'files', label: 'Files', load: () => import('../pages/devices/Files'), detail: () => import('../pages/devices/FileInspector') },
      { path: 'nodes', label: 'Sync nodes', load: () => import('../pages/devices/Nodes') },
      { path: 'add', label: 'Add a watch', requires: 'pair', hidden: true, load: () => import('../pages/devices/add/AddWatch') },
    ],
  },
  {
    id: 'settings', path: '/settings', label: 'Settings', icon: Settings,
    pages: [
      { path: 'profile', label: 'Profile', load: () => import('../pages/settings/Profile') },
      { path: 'security', label: 'Security', load: () => import('../pages/settings/Security') },
      { path: 'account', label: 'Account data', load: () => import('../pages/settings/AccountData') },
      { path: 'admin', label: 'People', title: 'People & invites', requires: 'admin', load: () => import('../pages/settings/Admin') },
    ],
  },
]


/** allowed tells whether the signed-in account may see a section or page. */
export function allowed(req: 'sync' | 'pair' | 'admin' | undefined, me: Me): boolean {
  if (req === 'sync') return me.server.sync
  if (req === 'pair') return canPair(me)
  if (req === 'admin') return me.is_admin
  return true
}

/** visibleSections are the sections (and their pages) this account may use. */
export function visibleSections(me: Me): SectionDef[] {
  return sections
    .filter((s) => allowed(s.requires, me))
    .map((s) => ({ ...s, pages: s.pages.filter((p) => allowed(p.requires, me)) }))
}

/** findPage resolves a pathname to its section and page. */
export function findPage(pathname: string, list: SectionDef[] = sections): { section?: SectionDef; page?: PageDef } {
  const parts = pathname.replace(/\/+$/, '').split('/').filter(Boolean)
  const section = parts.length === 0 ? list.find((s) => s.path === '/') : list.find((s) => s.path === '/' + parts[0])
  const page = section?.pages.find((p) => p.path === parts[1])
  return { section, page }
}
