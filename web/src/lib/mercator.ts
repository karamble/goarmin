// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/** mercator projects degrees to Web Mercator units in 0..1 (x east, y south), like map tiles. */
export function mercator(lat: number, lon: number): [number, number] {
  const s = Math.sin((Math.max(-85.0511, Math.min(85.0511, lat)) * Math.PI) / 180)
  return [(lon + 180) / 360, 0.5 - Math.log((1 + s) / (1 - s)) / (4 * Math.PI)]
}

/** fitPath projects a track and scales it into w×h with padding, keeping its proportions. */
export function fitPath(pts: [number, number][], w: number, h: number, pad = 8): [number, number][] {
  if (pts.length === 0) return []
  const xy = pts.map(([lat, lon]) => mercator(lat, lon))
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity
  for (const [x, y] of xy) {
    x0 = Math.min(x0, x); x1 = Math.max(x1, x)
    y0 = Math.min(y0, y); y1 = Math.max(y1, y)
  }
  const span = Math.max(x1 - x0, y1 - y0, 1e-9)
  const k = Math.min((w - 2 * pad) / Math.max(x1 - x0, span * 1e-3), (h - 2 * pad) / Math.max(y1 - y0, span * 1e-3))
  const ox = (w - (x1 - x0) * k) / 2
  const oy = (h - (y1 - y0) * k) / 2
  return xy.map(([x, y]) => [ox + (x - x0) * k, oy + (y - y0) * k])
}
