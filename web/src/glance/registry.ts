// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import {
  Activity, Dumbbell, Trophy, BatteryCharging, Flame, Footprints, Gauge, HeartPulse, LineChart, Map as MapIcon, Moon, Radio, Target, TrendingUp, Waves, Watch, Wind, Zap,
} from 'lucide-react'
import type { Me } from '../api/types'
import { dailyMetrics, metricByKey } from './metrics'
import type { CardDef, Layout } from './types'

const pick = <T extends string>(v: unknown, allowed: readonly T[], def: T): T => (allowed.includes(v as T) ? (v as T) : def)
const trendDays = ['14', '30', '90', '365'] as const
const trendCharts = ['line', 'bar'] as const

const def = <O,>(d: CardDef<O>) => d as unknown as CardDef

/** The card catalog. A card's code loads with its first use. */
export const cards: CardDef[] = [
  def({ type: 'body-battery', title: 'Body Battery', description: 'Energy now, charged and drained today', icon: BatteryCharging,
    category: 'health', sizes: ['S', 'M', 'L'], defaultSize: 'M', defaultOptions: {}, to: '/health/energy',
    load: () => import('./cards/BodyBattery') }),
  def({ type: 'sleep', title: 'Sleep', description: 'Last night: score, duration and stages', icon: Moon,
    category: 'sleep', sizes: ['S', 'M', 'L'], defaultSize: 'M', defaultOptions: {}, to: '/health/sleep',
    load: () => import('./cards/Sleep') }),
  def({ type: 'hrv', title: 'HRV', description: 'Last night against your normal range', icon: Waves,
    category: 'health', sizes: ['S', 'M', 'L'], defaultSize: 'M', defaultOptions: {}, to: '/health/hrv',
    load: () => import('./cards/Hrv') }),
  def({ type: 'your-day', title: 'Your day', description: 'Heart rate, stress and Body Battery on one timeline', icon: LineChart,
    category: 'health', sizes: ['L', 'W'], defaultSize: 'W', defaultOptions: {}, singleton: true, to: '/health/energy',
    load: () => import('./cards/YourDay') }),
  def({ type: 'steps', title: 'Steps', description: 'Steps against your goal', icon: Footprints,
    category: 'health', sizes: ['S', 'M', 'L'], defaultSize: 'S', defaultOptions: {}, to: '/health/movement',
    load: () => import('./cards/Steps') }),
  def({ type: 'resting-hr', title: 'Resting HR', description: 'Resting heart rate and its trend', icon: HeartPulse,
    category: 'health', sizes: ['S', 'M'], defaultSize: 'S', defaultOptions: {}, to: '/health/heart',
    load: () => import('./cards/RestingHr') }),
  def({ type: 'stress', title: 'Stress', description: 'Average and peak stress', icon: Zap,
    category: 'health', sizes: ['S', 'M', 'L'], defaultSize: 'S', defaultOptions: {}, to: '/health/energy',
    load: () => import('./cards/Stress') }),
  def({ type: 'readiness', title: 'Training readiness', description: 'How ready you are for a hard session', icon: Gauge,
    category: 'training', sizes: ['S', 'M'], defaultSize: 'S', defaultOptions: {}, to: '/training/performance',
    load: () => import('./cards/Readiness') }),
  def({ type: 'recent-activities', title: 'Recent activities', description: 'Your latest workouts', icon: Activity,
    category: 'training', sizes: ['M', 'L', 'W'], defaultSize: 'L', defaultOptions: { count: 4 }, to: '/training/activities',
    fields: [{ key: 'count', label: 'Show', kind: 'select', choices: [3, 4, 5, 6].map((x) => ({ value: String(x), label: `${x} activities` })) }],
    parseOptions: (o) => ({ count: Math.min(Math.max(Math.round(Number(o.count)) || 4, 3), 6) }),
    load: () => import('./cards/RecentActivities') }),
  def({ type: 'coach', title: 'Garmin Coach', description: 'Today’s suggested workout and the week ahead', icon: Target,
    category: 'training', sizes: ['M', 'L'], defaultSize: 'L', defaultOptions: {}, to: '/training/coach',
    load: () => import('./cards/Coach') }),
  def({ type: 'vo2max', title: 'VO2 max', description: 'Aerobic fitness and race predictions', icon: TrendingUp,
    category: 'training', sizes: ['S', 'M', 'L'], defaultSize: 'M', defaultOptions: { predictions: true }, to: '/training/performance',
    fields: [{ key: 'predictions', label: 'Race predictions', kind: 'toggle' }],
    parseOptions: (o) => ({ predictions: o.predictions !== false }),
    load: () => import('./cards/Vo2max') }),
  def({ type: 'breathing', title: 'Breathing', description: 'Respiration and blood oxygen', icon: Wind,
    category: 'health', sizes: ['S', 'M'], defaultSize: 'S', defaultOptions: {}, to: '/health/breathing',
    load: () => import('./cards/Breathing') }),
  def({ type: 'intensity-minutes', title: 'Intensity minutes', description: 'This week against your weekly goal', icon: Flame,
    category: 'training', sizes: ['S', 'M'], defaultSize: 'S', defaultOptions: {}, to: '/health/movement',
    load: () => import('./cards/IntensityMinutes') }),
  def({ type: 'metric-trend', title: 'Trend', description: 'Any daily value over weeks or a year, as a line or bars', icon: LineChart,
    category: 'health', sizes: ['M', 'L', 'W'], defaultSize: 'M', defaultOptions: { metric: 'resting_hr', days: '30', chart: 'line' },
    fields: [
      { key: 'metric', label: 'Value', kind: 'select', choices: dailyMetrics.map((m) => ({ value: m.key, label: m.label })) },
      { key: 'days', label: 'Range', kind: 'select', choices: trendDays.map((d) => ({ value: d, label: d === '365' ? 'A year' : `${d} days` })) },
      { key: 'chart', label: 'Chart', kind: 'select', choices: [{ value: 'line', label: 'Line' }, { value: 'bar', label: 'Bars' }] },
    ],
    parseOptions: (o) => ({
      metric: metricByKey.has(o.metric as string) ? (o.metric as string) : 'resting_hr',
      days: pick(String(o.days), trendDays, '30'),
      chart: pick(o.chart, trendCharts, 'line'),
    }),
    titleFor: (o) => `${metricByKey.get(o.metric)?.label ?? 'Daily value'} trend`,
    load: () => import('./cards/MetricTrend') }),
  def({ type: 'last-activity-map', title: 'Last route', description: 'Where your latest activity went', icon: MapIcon,
    category: 'training', sizes: ['M', 'L'], defaultSize: 'M', defaultOptions: {}, to: '/training/activities',
    load: () => import('./cards/LastActivityMap') }),
  def({ type: 'workout-today', title: 'Workout for today', description: "Garmin Coach's workout, or the workouts saved on the watch", icon: Dumbbell,
    category: 'training', sizes: ['M', 'L'], defaultSize: 'M', defaultOptions: {}, to: '/training/workouts',
    load: () => import('./cards/WorkoutToday') }),
  def({ type: 'lifetime', title: 'Lifetime', description: 'Distance and sessions per sport, as the watch counts them', icon: Trophy,
    category: 'training', sizes: ['S', 'M'], defaultSize: 'S', defaultOptions: {}, to: '/training/performance',
    load: () => import('./cards/LifetimeTotals') }),
  def({ type: 'live-hr', title: 'Live heart rate', description: 'Your heart rate now, straight from the watch (on tap)', icon: Radio,
    category: 'device', sizes: ['M', 'L'], defaultSize: 'M', defaultOptions: {}, requires: 'sync', singleton: true, to: '/live',
    load: () => import('./cards/LiveHr') }),
  def({ type: 'device-status', title: 'Watch', description: 'Last and next sync', icon: Watch,
    category: 'device', sizes: ['S', 'M'], defaultSize: 'S', defaultOptions: {}, requires: 'sync', to: '/devices/watches',
    load: () => import('./cards/DeviceStatus') }),
]

export const cardByType = new Map(cards.map((c) => [c.type, c]))

/** defaultLayout is the glance a new account starts with. */
export function defaultLayout(me: Me): Layout {
  const order = ['body-battery', 'sleep', 'hrv', 'your-day', 'steps', 'resting-hr', 'stress', 'readiness', 'recent-activities', 'coach', 'device-status']
  return {
    v: 1,
    cards: order
      .map((t) => cardByType.get(t)!)
      .filter((c) => c.requires !== 'sync' || me.server.sync)
      .map((c) => ({ id: c.type, type: c.type, size: c.defaultSize, options: { ...(c.defaultOptions as object) } })),
  }
}
