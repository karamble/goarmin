// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/** WatchGlyph is a round watch whose bezel shows the battery: lime, amber below 20 %. */
export function WatchGlyph({ battery, size = 72 }: { battery: number | null; size?: number }) {
  const r = 30
  const c = 2 * Math.PI * r
  const f = battery == null ? 0 : Math.max(0, Math.min(1, battery / 100))
  const color = battery != null && battery < 20 ? 'var(--amber)' : 'var(--lime)'
  return (
    <svg viewBox="0 0 100 100" width={size} height={size} role="img" className="shrink-0"
      aria-label={battery == null ? 'Battery not reported yet' : `Battery ${battery} percent`}>
      <rect x="35" y="1" width="30" height="18" rx="5" fill="var(--panel2)" stroke="var(--border)" />
      <rect x="35" y="81" width="30" height="18" rx="5" fill="var(--panel2)" stroke="var(--border)" />
      <rect x="86" y="36" width="5" height="9" rx="2" fill="var(--border)" />
      <rect x="86" y="55" width="5" height="9" rx="2" fill="var(--border)" />
      <rect x="9" y="30" width="5" height="9" rx="2" fill="var(--border)" />
      <rect x="9" y="45" width="5" height="9" rx="2" fill="var(--border)" />
      <rect x="9" y="60" width="5" height="9" rx="2" fill="var(--border)" />
      <circle cx="50" cy="50" r="38" fill="var(--bg2)" stroke="var(--border)" strokeWidth="2" />
      <circle cx="50" cy="50" r={r} fill="none" stroke="var(--track)" strokeWidth="6" />
      {f > 0 && (
        <circle cx="50" cy="50" r={r} fill="none" stroke={color} strokeWidth="6" strokeLinecap="round"
          strokeDasharray={`${c * f} ${c}`} transform="rotate(-90 50 50)"
          style={{ filter: battery != null && battery >= 20 ? 'drop-shadow(0 0 3px var(--lime-glow))' : undefined }} />
      )}
      <text x="50" y="50" textAnchor="middle" dominantBaseline="central" fill="var(--text)"
        style={{ font: '600 17px var(--mono)' }}>{battery == null ? '–' : `${battery}`}</text>
      {battery != null && (
        <text x="50" y="66" textAnchor="middle" fill="var(--muted)" style={{ font: '500 8px var(--mono)', letterSpacing: '.1em' }}>%</text>
      )}
    </svg>
  )
}
