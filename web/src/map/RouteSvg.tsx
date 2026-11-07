// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo } from 'react'
import { fitPath } from '../lib/mercator'
import { decodePolyline } from '../lib/polyline'

/** RouteSvg draws a route from its encoded polyline: no tiles, no requests. */
export function RouteSvg({ polyline, width = 320, height = 200, label, className = '' }: {
  polyline: string
  width?: number
  height?: number
  label: string
  className?: string
}) {
  const pts = useMemo(() => fitPath(decodePolyline(polyline), width, height, 14), [polyline, width, height])
  if (pts.length < 2) return null
  const d = pts.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)} ${y.toFixed(1)}`).join('')
  const [sx, sy] = pts[0]
  const [ex, ey] = pts[pts.length - 1]
  return (
    <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label={label} className={className} preserveAspectRatio="xMidYMid meet">
      <defs>
        <pattern id="route-grid" width="24" height="24" patternUnits="userSpaceOnUse">
          <path d="M24 0H0V24" fill="none" stroke="var(--grid)" strokeWidth="1" />
        </pattern>
      </defs>
      <rect width={width} height={height} fill="url(#route-grid)" />
      <path d={d} fill="none" stroke="var(--lime)" strokeOpacity=".25" strokeWidth="7" strokeLinejoin="round" strokeLinecap="round" />
      <path d={d} fill="none" stroke="var(--lime)" strokeWidth="2.25" strokeLinejoin="round" strokeLinecap="round" />
      <circle cx={sx} cy={sy} r="4.5" fill="var(--bg)" stroke="var(--grey)" strokeWidth="2" />
      <circle cx={ex} cy={ey} r="4.5" fill="var(--lime)" stroke="var(--bg)" strokeWidth="2" />
    </svg>
  )
}
