// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { X } from 'lucide-react'
import { useEffect, useRef, type ReactNode } from 'react'

/**
 * Sheet is a modal panel: a drawer on the right on wide screens, a bottom sheet on phones. Esc
 * and the backdrop close it; focus moves in and returns to where it was.
 */
export function Sheet({ title, onClose, children, footer }: { title: string; onClose: () => void; children: ReactNode; footer?: ReactNode }) {
  const panel = useRef<HTMLDivElement>(null)
  const back = useRef<HTMLElement | null>(document.activeElement as HTMLElement | null)
  useEffect(() => {
    const el = panel.current
    el?.querySelector<HTMLElement>('[autofocus], input, select, button:not([data-close])')?.focus()
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
    <div className="fixed inset-0 z-[75] bg-black/50 backdrop-blur-[2px]" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={panel} role="dialog" aria-modal="true" aria-label={title}
        className="card-glass absolute inset-x-0 bottom-0 flex max-h-[85dvh] flex-col rounded-b-none md:inset-y-3 md:left-auto md:right-3 md:max-h-none md:w-[420px] md:rounded-[18px]">
        <header className="flex items-center gap-3 border-b border-border px-5 py-4">
          <h2 className="m-0 flex-1 font-display text-[20px] uppercase tracking-[.02em]">{title}</h2>
          <button type="button" data-close onClick={onClose} aria-label="Close" className="grid h-8 w-8 place-items-center rounded-full text-muted hover:text-text">
            <X size={18} />
          </button>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
        {footer && <footer className="border-t border-border px-5 py-3">{footer}</footer>}
      </div>
    </div>
  )
}

/** Confirm asks a yes/no question in a small modal. */
export function Confirm({ title, children, confirm, danger, onConfirm, onClose }: {
  title: string
  children: ReactNode
  confirm: string
  danger?: boolean
  onConfirm: () => void
  onClose: () => void
}) {
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    box.current?.querySelector<HTMLButtonElement>('[data-cancel]')?.focus()
    const key = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    document.addEventListener('keydown', key)
    return () => document.removeEventListener('keydown', key)
  }, [onClose])
  return (
    <div className="fixed inset-0 z-[78] grid place-items-center bg-black/55 px-4" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={box} role="alertdialog" aria-modal="true" aria-label={title} className="card-glass w-full max-w-[400px] p-6">
        <h2 className="m-0 font-display text-[22px] uppercase tracking-[.02em]">{title}</h2>
        <div className="mt-2 text-[14px] text-muted">{children}</div>
        <div className="mt-6 flex justify-end gap-2">
          <button type="button" data-cancel onClick={onClose} className="rounded-full px-4 py-2 text-[14px] text-muted hover:text-text">Cancel</button>
          <button type="button" onClick={() => { onConfirm(); onClose() }}
            className={`rounded-full border-2 px-4 py-2 text-[14px] font-bold ${danger ? 'border-red/60 text-err hover:border-err' : 'border-lime bg-lime text-bg'}`}>
            {confirm}
          </button>
        </div>
      </div>
    </div>
  )
}
