// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/** ChartTokens are the colours and fonts charts use; the app passes its design tokens. */
export interface ChartTokens {
  lime: string
  limeDim: string
  limeFill: string
  grey: string
  grey2: string
  cyan: string
  pink: string
  amber: string
  red: string
  track: string
  text: string
  muted: string
  faint: string
  border: string
  grid: string
  panel: string
  mono: string
  sans: string
}

/** Colour roles: lime marks the one thing that matters, greys are context. */
export type Role = 'primary' | 'context' | 'context2' | 'extra' | 'extra2' | 'warn' | 'crit' | 'track'

export function roleColor(t: ChartTokens, r: Role): string {
  switch (r) {
    case 'primary':
      return t.lime
    case 'context':
      return t.grey
    case 'context2':
      return t.grey2
    case 'extra':
      return t.cyan
    case 'extra2':
      return t.pink
    case 'warn':
      return t.amber
    case 'crit':
      return t.red
    default:
      return t.track
  }
}

/** micavoss is the default palette (rack-film-website site.css, rack-film theme.ts). */
export const micavoss: ChartTokens = {
  lime: '#c8f542',
  limeDim: '#8da92e',
  limeFill: 'rgba(200, 245, 66, 0.12)',
  grey: '#c9ced4',
  grey2: '#9aa1a9',
  cyan: '#3fd5ff',
  pink: '#ff5fb8',
  amber: '#ffb020',
  red: '#c9605f',
  track: '#2a3038',
  text: '#e6edf3',
  muted: '#8b949e',
  faint: '#5b636d',
  border: '#262c35',
  grid: '#1d232b',
  panel: '#161b22',
  mono: '"JetBrains Mono", ui-monospace, monospace',
  sans: '"DM Sans", system-ui, sans-serif',
}

/** withAlpha turns "#rrggbb" into rgba(…, a). */
export function withAlpha(hex: string, a: number): string {
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex)
  if (!m) return hex
  return `rgba(${parseInt(m[1], 16)}, ${parseInt(m[2], 16)}, ${parseInt(m[3], 16)}, ${a})`
}
