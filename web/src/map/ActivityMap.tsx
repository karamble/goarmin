// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import { useEffect, useMemo, useRef } from 'react'
import { useMe } from '../api/hooks/auth'
import { buckets } from '../lib/activity'
import type { Ramp } from './ActivityRoute'

/** The OpenStreetMap attribution, kept visible on every map (the server may name another). */
export const osmAttribution = '© <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noreferrer">OpenStreetMap</a> contributors'

/** cssColor resolves a CSS colour (var(--lime), color-mix(…)) for Leaflet, which draws SVG
 * attributes that do not take custom properties everywhere. */
function cssColor(el: HTMLElement, c: string): string {
  if (!c.startsWith('var(') && !c.startsWith('color-mix(')) return c
  const probe = document.createElement('span')
  probe.style.color = c
  el.appendChild(probe)
  const out = getComputedStyle(probe).color || c
  probe.remove()
  return out
}

/**
 * ActivityMap draws a track on the map tiles goarmin caches (/tiles, same origin), coloured by a
 * value in five bands, with the point the charts point at (hover) and named marks. Moving over
 * the map reports the nearest track point (onHover). Without interactive, it is a still picture
 * (the glance card).
 */
export function ActivityMap({ points, values, ramp, hover, onHover, marks, label, height = 440, interactive = true }: {
  points: { i: number; lat: number; lon: number }[]
  values?: (number | null | undefined)[]
  ramp: Ramp
  hover?: number | null
  onHover?: (i: number | null) => void
  marks?: { lat: number; lon: number; label: string }[]
  label: string
  height?: number
  interactive?: boolean
}) {
  const server = useMe().data?.server.map
  const credit = server?.attribution || osmAttribution
  const dark = !!server?.dark
  const box = useRef<HTMLDivElement>(null)
  const map = useRef<L.Map | null>(null)
  const track = useRef<L.LayerGroup | null>(null)
  const dot = useRef<L.CircleMarker | null>(null)
  const hoverCb = useRef(onHover)
  hoverCb.current = onHover
  const idx = useMemo(() => points.map((p) => p.i), [points])

  // The map, once.
  useEffect(() => {
    if (!box.current) return
    const m = L.map(box.current, {
      zoomControl: interactive, attributionControl: true, dragging: interactive, touchZoom: interactive, doubleClickZoom: interactive,
      boxZoom: interactive, keyboard: interactive, scrollWheelZoom: false,
    })
    m.attributionControl.setPrefix(false)
    L.tileLayer('/tiles/{s}/{z}/{x}/{y}.png', { subdomains: 'abc', maxZoom: 19, attribution: credit }).addTo(m)
    map.current = m
    track.current = L.layerGroup().addTo(m)
    // Wheel zoom only with Ctrl/⌘ held, so the page keeps scrolling over the map.
    const wheel = (e: WheelEvent) => {
      if (!(e.ctrlKey || e.metaKey) || !interactive) return
      e.preventDefault()
      m.setZoom(m.getZoom() + (e.deltaY < 0 ? 1 : -1))
    }
    const el = box.current
    el.addEventListener('wheel', wheel, { passive: false })
    return () => {
      el.removeEventListener('wheel', wheel)
      m.remove()
      map.current = null
    }
  }, [interactive, credit])

  // The track, its ends and the marks; the view fits them.
  useEffect(() => {
    const m = map.current
    const g = track.current
    if (!m || !g || !box.current || points.length < 2) return
    g.clearLayers()
    const colors = ramp.map((c) => cssColor(box.current!, c))
    const band = values ? buckets(points.map((p) => values[p.i]), 5).band : points.map(() => 4)
    let run: L.LatLngExpression[] = []
    let cur = band[0] < 0 ? 0 : band[0]
    const flush = () => {
      if (run.length > 1) L.polyline(run, { color: colors[cur], weight: 4, opacity: 0.95, lineCap: 'round', lineJoin: 'round', interactive: false }).addTo(g)
    }
    points.forEach((p, k) => {
      const b = band[k] < 0 ? 0 : band[k]
      if (b !== cur) {
        run.push([p.lat, p.lon]) // join to the next run
        flush()
        run = []
        cur = b
      }
      run.push([p.lat, p.lon])
    })
    flush()
    const first = points[0]
    const last = points[points.length - 1]
    const bg = cssColor(box.current, 'var(--bg)')
    L.circleMarker([first.lat, first.lon], { radius: 6, color: cssColor(box.current, 'var(--grey)'), weight: 2, fillColor: bg, fillOpacity: 1, interactive: false }).addTo(g)
    L.circleMarker([last.lat, last.lon], { radius: 6, color: bg, weight: 2, fillColor: cssColor(box.current, 'var(--lime)'), fillOpacity: 1, interactive: false }).addTo(g)
    const cyan = cssColor(box.current, 'var(--cyan)')
    marks?.forEach((mk, k) => {
      L.circleMarker([mk.lat, mk.lon], { radius: 9, color: cyan, weight: 2, fillColor: bg, fillOpacity: 1 })
        .bindTooltip(`${k + 1}`, { permanent: true, direction: 'center', className: 'goarmin-mark' })
        .bindPopup(`${k + 1}. ${mk.label}`)
        .addTo(g)
    })
    m.fitBounds(L.latLngBounds(points.map((p) => [p.lat, p.lon] as L.LatLngTuple)), { padding: [24, 24] })
  }, [points, values, ramp, marks])

  // Pointing at the map reports the nearest track point within 40 px.
  useEffect(() => {
    const m = map.current
    if (!m || !interactive) return
    const move = (e: L.LeafletMouseEvent) => {
      if (!hoverCb.current) return
      let best = -1
      let bd = Infinity
      const step = Math.max(1, Math.floor(points.length / 2000))
      for (let k = 0; k < points.length; k += step) {
        const q = m.latLngToContainerPoint([points[k].lat, points[k].lon])
        const d = q.distanceTo(e.containerPoint)
        if (d < bd) {
          bd = d
          best = k
        }
      }
      hoverCb.current(best >= 0 && bd < 40 ? idx[best] : null)
    }
    const out = () => hoverCb.current?.(null)
    m.on('mousemove', move)
    m.on('mouseout', out)
    return () => {
      m.off('mousemove', move)
      m.off('mouseout', out)
    }
  }, [points, idx, interactive])

  // The dot follows the charts' crosshair.
  useEffect(() => {
    const m = map.current
    if (!m || !box.current) return
    const k = hover == null ? -1 : idx.indexOf(hover) >= 0 ? idx.indexOf(hover) : nearest(idx, hover)
    if (k < 0) {
      dot.current?.remove()
      dot.current = null
      return
    }
    const ll: L.LatLngTuple = [points[k].lat, points[k].lon]
    if (!dot.current) {
      dot.current = L.circleMarker(ll, { radius: 7, color: cssColor(box.current, 'var(--bg)'), weight: 2, fillColor: cssColor(box.current, 'var(--text)'), fillOpacity: 1, interactive: false }).addTo(m)
    } else dot.current.setLatLng(ll)
  }, [hover, idx, points])

  return (
    <div ref={box} role="img" aria-label={label} className={`goarmin-map w-full${dark ? ' goarmin-map-dark' : ''}`} style={{ height }} />
  )
}

function nearest(xs: number[], x: number): number {
  let best = -1
  let bd = Infinity
  xs.forEach((v, k) => {
    if (Math.abs(v - x) < bd) {
      bd = Math.abs(v - x)
      best = k
    }
  })
  return best
}
