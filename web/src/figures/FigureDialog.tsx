// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { ChevronLeft, ChevronRight, X } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { FigureView } from './FigureView'

/** FigureItem is one exercise the dialog can show. */
export interface FigureItem {
  category: string
  number: number
  title: string
  detail?: string // e.g. the step's reps, time or weight
}

/**
 * FigureDialog shows exercises' figures one at a time, with previous and next (also ← and →).
 * It holds the page's only WebGL context while open; Esc and the backdrop close it, and focus
 * returns to where it was.
 */
export function FigureDialog({ items, start = 0, onClose }: { items: FigureItem[]; start?: number; onClose: () => void }) {
  const [i, setI] = useState(Math.min(Math.max(start, 0), Math.max(items.length - 1, 0)))
  const box = useRef<HTMLDivElement>(null)
  const back = useRef<HTMLElement | null>(document.activeElement as HTMLElement | null)
  const n = items.length
  const step = useCallback((d: number) => setI((x) => (x + d + n) % n), [n])
  useEffect(() => {
    box.current?.querySelector<HTMLElement>('[data-close]')?.focus()
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        onClose()
      } else if (n > 1 && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) {
        const t = e.target as HTMLElement | null
        if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return
        e.preventDefault()
        step(e.key === 'ArrowLeft' ? -1 : 1)
      }
    }
    document.addEventListener('keydown', key)
    const ret = back.current
    return () => {
      document.removeEventListener('keydown', key)
      ret?.focus?.()
    }
  }, [onClose, step, n])
  const it = items[i]
  if (!it) return null
  return (
    <div className="fixed inset-0 z-[76] grid place-items-center bg-black/60 px-3 py-6" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} role="dialog" aria-modal="true" aria-label={it.title} className="card-glass flex max-h-full w-full max-w-[720px] flex-col overflow-y-auto p-4 sm:p-5">
        <header className="mb-3 flex items-start gap-3">
          <div className="min-w-0 flex-1">
            <h2 className="m-0 font-display text-[22px] uppercase leading-tight tracking-[.02em]">{it.title}</h2>
            {it.detail && <p className="m-0 mt-0.5 text-[13px] text-muted">{it.detail}</p>}
          </div>
          <button type="button" data-close onClick={onClose} aria-label="Close" className="grid h-8 w-8 shrink-0 place-items-center rounded-full text-muted hover:text-text">
            <X size={18} />
          </button>
        </header>
        <FigureView category={it.category} number={it.number} title={it.title} />
        {n > 1 && (
          <nav className="mt-3 flex items-center justify-between gap-2" aria-label="Exercises">
            <button type="button" onClick={() => step(-1)} className="inline-flex items-center gap-1 rounded-full border border-border px-3 py-1.5 text-[13px] text-muted hover:text-text">
              <ChevronLeft size={15} aria-hidden /> Previous
            </button>
            <span className="num text-[12px] text-muted">{i + 1} of {n}</span>
            <button type="button" onClick={() => step(1)} className="inline-flex items-center gap-1 rounded-full border border-border px-3 py-1.5 text-[13px] text-muted hover:text-text">
              Next <ChevronRight size={15} aria-hidden />
            </button>
          </nav>
        )}
      </div>
    </div>
  )
}
