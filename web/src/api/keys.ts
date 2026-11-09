// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/**
 * Every query key in one place, so invalidation never misses one. Health and training data sit
 * under the 'health' and 'training' roots, which a finished sync invalidates.
 */
export const keys = {
  me: ['me'] as const,
  setup: ['setup'] as const,
  sessions: ['me', 'sessions'] as const,
  passkeys: ['me', 'passkeys'] as const,
  setting: (key: string) => ['me', 'settings', key] as const,
  invite: (token: string) => ['invite', token] as const,
  users: ['admin', 'users'] as const,

  health: ['health'] as const,
  daily: (from: string, to: string) => ['health', 'daily', from, to] as const,
  series: (metric: string, fromDay: string, toDay: string, maxPoints: number) => ['health', 'series', metric, fromDay, toDay, maxPoints] as const,
  sleep: (from: string, to: string) => ['health', 'sleep', from, to] as const,
  nightStages: (start: number) => ['health', 'stages', start] as const,

  training: ['training'] as const,
  activities: (limit: number) => ['training', 'activities', limit] as const,
  activitiesBefore: (before: number, limit: number) => ['training', 'activities', 'before', before, limit] as const,
  activityPages: (limit: number) => ['training', 'activities', 'pages', limit] as const,
  activitiesIn: (from: string, to: string) => ['training', 'activities', 'in', from, to] as const,
  activity: (id: number) => ['training', 'activity', id] as const,
  streams: (id: number) => ['training', 'activity', id, 'streams'] as const,
  neighbours: (id: number) => ['training', 'activity', id, 'neighbours'] as const,
  coach: (from: string, to: string) => ['training', 'coach', from, to] as const,
  totals: ['training', 'totals'] as const,
  workouts: ['training', 'workouts'] as const,
  calendar: (from: string, to: string) => ['training', 'calendar', from, to] as const,
  calendarAll: ['training', 'calendar'] as const,
  workout: (id: number) => ['training', 'workouts', id] as const,
  courses: ['training', 'courses'] as const,
  course: (id: number) => ['training', 'courses', id] as const,
  courseStreams: (id: number) => ['training', 'courses', id, 'streams'] as const,
  watchProfile: ['training', 'watch-profile'] as const,
  zones: ['training', 'zones'] as const,

  devices: ['devices'] as const,
  deviceSummary: (id: number) => ['devices', id, 'summary'] as const,
  deviceFiles: (id: number) => ['devices', id, 'files'] as const,
  syncLog: (device: number, problems: boolean, limit: number) => ['devices', 'log', device, problems, limit] as const,
  bluetooth: ['devices', 'bluetooth'] as const,
  /** A sync node's Bluetooth (under bluetooth, so invalidating that covers it). */
  bluetoothOf: (node: number) => ['devices', 'bluetooth', node] as const,
  nodes: ['devices', 'nodes'] as const,
  imports: ['imports'] as const,
  importRun: (id: number) => ['imports', 'run', id] as const,
  usb: ['usb'] as const,

  // Not under 'devices': a device change must not refetch (and so reset) the session.
  pairing: ['pairing'] as const,
  pairComputers: (owner: number) => ['pairing-computers', owner] as const,
  adminPairing: ['admin', 'pairing'] as const,
}
