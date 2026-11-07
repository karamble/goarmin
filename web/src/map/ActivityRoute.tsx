// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo, useRef } from 'react'
import { buckets, nearestIndex } from '../lib/activity'
import { fitPath } from '../lib/mercator'

/** A colour ramp of five bands, low to high. */
export type Ramp = readonly [string, string, string, string, string]

export const ramps = {
  hr: ['var(--grey2)', 'var(--grey)', 'var(--lime)', 'var(--amber)', 'var(--red)'],
  speed: ['var(--faint)', 'var(--grey2)', 'var(--lime-dim)', 'color-mix(in srgb, var(--lime) 75%, var(--lime-dim))', 'var(--lime)'],
  alt: ['var(--cyan)', 'color-mix(in srgb, var(--cyan) 60%, var(--lime))', 'var(--lime-dim)', 'color-mix(in srgb, var(--lime) 60%, var(--amber))', 'var(--amber)'],
} as const satisfies Record<string, Ramp>

/**
 * ActivityRoute draws a track as SVG (no map tiles), coloured by a value in five bands. A dot
 * marks the point the charts point at (hover), and moving over the route reports the nearest
 * point (onHover) so the charts can follow.
 */
export function ActivityRoute({ points, values, ramp, hover, onHover, width = 720, height = 440, label, marks }: {
  /** The track's positions with their point index. */
  points: { i: number; lat: number; lon: number }[]
  /** The value per point index that colours the route (undefined: one colour). */
  values?: (number | null | undefined)[]
  ramp: Ramp
  hover: number | null
  onHover?: (i: number | null) => void
  width?: number
  height?: number
  label: string
  /** Named points along the route (a course's water, summit, turns), numbered on the map. */
  marks?: { lat: number; lon: number; label: string }[]
}) {
  const svg = useRef<SVGSVGElement>(null)
  // The marks are projected with the track, so they sit where they belong on it.
  const all = useMemo(() => fitPath([...points.map((p) => [p.lat, p.lon] as [number, number]), ...(marks ?? []).map((m) => [m.lat, m.lon] as [number, number])],
    width, height, 16), [points, marks, width, height])
  const xy = useMemo(() => all.slice(0, points.length), [all, points.length])
  const mxy = useMemo(() => all.slice(points.length), [all, points.length])
  const idx = useMemo(() => points.map((p) => p.i), [points])
  const runs = useMemo(() => {
    const band = values ? buckets(points.map((p) => values[p.i]), 5).band : points.map(() => 4)
    const out: { band: number; d: string }[] = []
    let cur = -2
    let d = ''
    for (let k = 0; k < xy.length; k++) {
      const b = band[k] < 0 ? 0 : band[k]
      const [x, y] = xy[k]
      if (b !== cur) {
        if (d) {
          out.push({ band: cur, d: `${d}L${x.toFixed(1)} ${y.toFixed(1)}` }) // join to the next run
        }
        cur = b
        d = `M${x.toFixed(1)} ${y.toFixed(1)}`
      } else {
        d += `L${x.toFixed(1)} ${y.toFixed(1)}`
      }
    }
    if (d) out.push({ band: cur, d })
    return out
  }, [xy, values, points])
  if (xy.length < 2) return null
  const at = hover == null ? -1 : nearestIndex(idx, hover)
  const dot = at >= 0 ? xy[at] : null
  const move = (e: React.PointerEvent<SVGSVGElement>) => {
    if (!onHover || !svg.current) return
    const r = svg.current.getBoundingClientRect()
    const k = Math.min(r.width / width, r.height / height)
    const px = (e.clientX - r.left - (r.width - width * k) / 2) / k
    const py = (e.clientY - r.top - (r.height - height * k) / 2) / k
    let best = -1
    let bd = Infinity
    for (let j = 0; j < xy.length; j += Math.max(1, Math.floor(xy.length / 2000))) {
      const dd = (xy[j][0] - px) ** 2 + (xy[j][1] - py) ** 2
      if (dd < bd) {
        bd = dd
        best = j
      }
    }
    onHover(best >= 0 && bd < 40 * 40 ? idx[best] : null)
  }
  const [sx, sy] = xy[0]
  const [ex, ey] = xy[xy.length - 1]
  return (
    <svg ref={svg} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={label} className="h-auto w-full touch-none"
      preserveAspectRatio="xMidYMid meet" onPointerMove={move} onPointerLeave={() => onHover?.(null)}>
      <defs>
        <pattern id="activity-grid" width="32" height="32" patternUnits="userSpaceOnUse">
          <path d="M32 0H0V32" fill="none" stroke="var(--grid)" strokeWidth="1" />
        </pattern>
      </defs>
      <rect width={width} height={height} fill="url(#activity-grid)" />
      {runs.map((r, k) => (
        <path key={k} d={r.d} fill="none" stroke={ramp[r.band]} strokeWidth="3" strokeLinejoin="round" strokeLinecap="round" />
      ))}
      <circle cx={sx} cy={sy} r="5" fill="var(--bg)" stroke="var(--grey)" strokeWidth="2" />
      <circle cx={ex} cy={ey} r="5" fill="var(--lime)" stroke="var(--bg)" strokeWidth="2" />
      {marks?.map((m, k) => mxy[k] && (
        <g key={k}>
          <title>{`${k + 1}. ${m.label}`}</title>
          <circle cx={mxy[k][0]} cy={mxy[k][1]} r="8" fill="var(--bg)" stroke="var(--cyan)" strokeWidth="2" />
          <text x={mxy[k][0]} y={mxy[k][1] + 3.5} textAnchor="middle" fontSize="9" fontFamily="var(--mono)" fill="var(--cyan)">{k + 1}</text>
        </g>
      ))}
      {dot && (
        <g aria-hidden>
          <circle cx={dot[0]} cy={dot[1]} r="11" fill="var(--lime-glow)" />
          <circle cx={dot[0]} cy={dot[1]} r="5.5" fill="var(--text)" stroke="var(--bg)" strokeWidth="2" />
        </g>
      )}
    </svg>
  )
}
