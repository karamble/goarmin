// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { Device } from '../../api/types'
import { kindWord } from '../../lib/devices'
import { WatchGlyph } from './WatchGlyph'

/** DeviceGlyph draws a device by its kind: a watch with its battery ring, a scale, a bike
 * computer, a sensor, a handheld, or a generic device. */
export function DeviceGlyph({ d, size = 72 }: { d: Pick<Device, 'kind' | 'battery'>; size?: number }) {
  if ((d.kind ?? 'watch') === 'watch') return <WatchGlyph battery={d.battery} size={size} />
  return (
    <svg viewBox="0 0 100 100" width={size} height={size} role="img" aria-label={kindWord(d.kind, true)} className="shrink-0">
      {shape(d.kind)}
    </svg>
  )
}

const line = { stroke: 'var(--border)', strokeWidth: 2 } as const
const glow = { filter: 'drop-shadow(0 0 3px var(--lime-glow))' }

function shape(kind: Device['kind']) {
  switch (kind) {
    case 'scale':
      return (
        <>
          <rect x="10" y="10" width="80" height="80" rx="16" fill="var(--bg2)" {...line} />
          <rect x="16" y="16" width="68" height="68" rx="11" fill="none" stroke="var(--track)" strokeWidth="1.5" />
          <rect x="34" y="24" width="32" height="15" rx="4" fill="var(--panel2)" stroke="var(--lime-dim)" />
          <text x="50" y="32" textAnchor="middle" dominantBaseline="central" fill="var(--lime)" style={{ font: '600 9px var(--mono)', ...glow }}>kg</text>
          <circle cx="30" cy="66" r="3" fill="var(--track)" />
          <circle cx="70" cy="66" r="3" fill="var(--track)" />
        </>
      )
    case 'bike_computer':
      return (
        <>
          <rect x="22" y="8" width="56" height="84" rx="12" fill="var(--bg2)" {...line} />
          <rect x="30" y="18" width="40" height="46" rx="5" fill="var(--panel2)" stroke="var(--track)" />
          <polyline points="34,52 42,44 50,48 58,34 66,40" fill="none" stroke="var(--lime)" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" style={glow} />
          <rect x="38" y="72" width="24" height="8" rx="4" fill="var(--track)" />
        </>
      )
    case 'sensor':
      return (
        <>
          <circle cx="50" cy="50" r="38" fill="var(--bg2)" {...line} />
          <polyline points="22,52 36,52 42,38 50,66 57,44 62,52 78,52" fill="none" stroke="var(--lime)" strokeWidth="3"
            strokeLinecap="round" strokeLinejoin="round" style={glow} />
        </>
      )
    case 'handheld':
      return (
        <>
          <rect x="64" y="4" width="6" height="20" rx="3" fill="var(--border)" />
          <rect x="26" y="18" width="48" height="78" rx="12" fill="var(--bg2)" {...line} />
          <rect x="33" y="26" width="34" height="34" rx="5" fill="var(--panel2)" stroke="var(--track)" />
          <circle cx="50" cy="43" r="7" fill="none" stroke="var(--lime)" strokeWidth="2.5" style={glow} />
          <circle cx="50" cy="76" r="7" fill="var(--track)" />
        </>
      )
    default:
      return (
        <>
          <rect x="18" y="18" width="64" height="64" rx="12" fill="var(--bg2)" {...line} />
          {[30, 50, 70].map((p) => (
            <g key={p}>
              <rect x={p - 3} y="6" width="6" height="12" rx="2" fill="var(--border)" />
              <rect x={p - 3} y="82" width="6" height="12" rx="2" fill="var(--border)" />
            </g>
          ))}
          <rect x="36" y="36" width="28" height="28" rx="5" fill="none" stroke="var(--lime)" strokeWidth="2.5" style={glow} />
        </>
      )
  }
}
