// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { ReactNode } from 'react'

/** Ring is a progress ring (score, goal) drawn as SVG; the value goes in children. */
export function Ring({ value, max, size = 120, stroke = 10, color = 'var(--lime)', track = 'var(--track)', label, children }: {
  value: number | null
  max: number
  size?: number
  stroke?: number
  color?: string
  track?: string
  label: string
  children?: ReactNode
}) {
  const r = (size - stroke) / 2
  const c = 2 * Math.PI * r
  const f = value == null ? 0 : Math.max(0, Math.min(1, value / max))
  return (
    <div className="relative grid place-items-center" style={{ width: size, height: size }} role="img" aria-label={label}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} className="-rotate-90">
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke={track} strokeWidth={stroke} />
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke={color} strokeWidth={stroke} strokeLinecap="round"
          strokeDasharray={`${c * f} ${c}`} style={{ filter: f > 0 ? `drop-shadow(0 0 6px ${color === 'var(--lime)' ? 'var(--lime-glow)' : 'transparent'})` : undefined, transition: 'stroke-dasharray .6s ease' }} />
      </svg>
      <div className="absolute inset-0 grid place-items-center text-center">{children}</div>
    </div>
  )
}

/** Arc is a 240° gauge (Body Battery, readiness), lime filled; the value goes in children. */
export function Arc({ value, min = 0, max = 100, size = 140, stroke = 10, color = 'var(--lime)', label, children }: {
  value: number | null
  min?: number
  max?: number
  size?: number
  stroke?: number
  color?: string
  label: string
  children?: ReactNode
}) {
  const r = (size - stroke) / 2
  const cx = size / 2
  const cy = size / 2
  const sweep = 240
  const startA = 150 // degrees, clockwise from the x axis: the gap is at the bottom
  const pt = (a: number) => [cx + r * Math.cos((a * Math.PI) / 180), cy + r * Math.sin((a * Math.PI) / 180)]
  const arc = (from: number, to: number) => {
    const [x1, y1] = pt(from)
    const [x2, y2] = pt(to)
    return `M ${x1} ${y1} A ${r} ${r} 0 ${to - from > 180 ? 1 : 0} 1 ${x2} ${y2}`
  }
  const f = value == null ? 0 : Math.max(0, Math.min(1, (value - min) / (max - min)))
  return (
    <div className="relative grid place-items-center" style={{ width: size, height: size * 0.86 }} role="img" aria-label={label}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} className="absolute top-0">
        <path d={arc(startA, startA + sweep)} fill="none" stroke="var(--track)" strokeWidth={stroke} strokeLinecap="round" />
        {f > 0 && <path d={arc(startA, startA + sweep * Math.max(f, 0.01))} fill="none" stroke={color} strokeWidth={stroke} strokeLinecap="round"
          style={{ filter: 'drop-shadow(0 0 6px var(--lime-glow))' }} />}
      </svg>
      <div className="relative grid place-items-center pt-2 text-center">{children}</div>
    </div>
  )
}
