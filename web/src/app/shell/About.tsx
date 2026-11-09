// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { ExternalLink, X } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { DISCLAIMER, SOURCE_URL } from '../../lib/disclaimer'
import { Mark } from '../../ui/kit'

/** openAbout opens the About dialog from a button anywhere in the shell. */
export function openAbout() {
  window.dispatchEvent(new Event('goarmin:about'))
}

/** AboutHost renders the About dialog while it is open; the shell mounts it once. */
export function AboutHost({ version }: { version?: string }) {
  const [open, setOpen] = useState(false)
  useEffect(() => {
    const show = () => setOpen(true)
    window.addEventListener('goarmin:about', show)
    return () => window.removeEventListener('goarmin:about', show)
  }, [])
  return open ? <AboutDialog version={version} onClose={() => setOpen(false)} /> : null
}

/**
 * AboutDialog says what goarmin is, links to its source, and carries the disclaimer of liability:
 * use at your own risk, no medical advice, and the exercise figures are simplified illustrations.
 * Esc and the backdrop close it; focus returns to where it was.
 */
export function AboutDialog({ version, onClose }: { version?: string; onClose: () => void }) {
  const box = useRef<HTMLDivElement>(null)
  const back = useRef<HTMLElement | null>(document.activeElement as HTMLElement | null)
  useEffect(() => {
    box.current?.querySelector<HTMLElement>('[data-close]')?.focus()
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        onClose()
      }
    }
    document.addEventListener('keydown', key)
    const ret = back.current
    return () => {
      document.removeEventListener('keydown', key)
      ret?.focus?.()
    }
  }, [onClose])
  return (
    <div className="fixed inset-0 z-[77] grid place-items-center bg-black/60 px-3 py-6" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} role="dialog" aria-modal="true" aria-labelledby="about-title" className="card-glass flex max-h-full w-full max-w-[560px] flex-col overflow-y-auto p-5 sm:p-6">
        <header className="flex items-start gap-3">
          <Mark />
          <div className="min-w-0 flex-1">
            <h2 id="about-title" className="m-0 font-display text-[22px] uppercase leading-tight tracking-[.02em]">About goarmin</h2>
            {version && <p className="m-0 mt-0.5 font-mono text-[12px] text-muted">Version {version}</p>}
          </div>
          <button type="button" data-close onClick={onClose} aria-label="Close" className="grid h-8 w-8 shrink-0 place-items-center rounded-full text-muted hover:text-text">
            <X size={18} />
          </button>
        </header>
        <p className="m-0 mt-4 text-[14px] text-text">
          A self-hosted dashboard for Garmin watches: it syncs them over Bluetooth, imports your history and keeps everything on your own server.
        </p>
        <a href={SOURCE_URL} target="_blank" rel="noopener noreferrer"
          className="mt-3 inline-flex items-center gap-1.5 self-start rounded-full border border-border px-3 py-1.5 text-[13px] text-lime hover:border-lime">
          Source code on GitHub <ExternalLink size={14} aria-hidden />
        </a>
        <section aria-labelledby="about-disclaimer" className="mt-5 rounded-xl border border-border bg-bg2 p-4">
          <h3 id="about-disclaimer" className="m-0 font-mono text-[11px] uppercase tracking-[.14em] text-muted">Disclaimer</h3>
          {DISCLAIMER.map((d) => (
            <p key={d.lead} className="m-0 mt-2 text-[13px] text-text"><strong className="font-semibold">{d.lead}</strong> {d.text}</p>
          ))}
        </section>
        <p className="m-0 mt-4 text-[12px] text-faint">
          ISC license. The exercise figures use a body from Blender Studio’s Human Base Meshes (CC0) and are drawn with three.js (MIT).
          Garmin, Fenix and Garmin Connect are trademarks of Garmin Ltd.; goarmin is not affiliated with or endorsed by Garmin.
        </p>
      </div>
    </div>
  )
}
