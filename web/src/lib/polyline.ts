// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/** decodePolyline reads Google's encoded polyline format (the server writes 5 decimals). */
export function decodePolyline(s: string, precision = 5): [number, number][] {
  const out: [number, number][] = []
  const k = 10 ** precision
  let i = 0
  let lat = 0
  let lon = 0
  const next = (): number | null => {
    let shift = 0
    let result = 0
    let b: number
    do {
      if (i >= s.length) return null
      b = s.charCodeAt(i++) - 63
      result |= (b & 0x1f) << shift
      shift += 5
    } while (b >= 0x20)
    return result & 1 ? ~(result >> 1) : result >> 1
  }
  while (i < s.length) {
    const a = next()
    const b = next()
    if (a == null || b == null) break
    lat += a
    lon += b
    out.push([lat / k, lon / k])
  }
  return out
}
