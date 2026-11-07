// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { X } from 'lucide-react'
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'

export interface ToastSpec {
  text: string
  tone?: 'info' | 'error'
  action?: { label: string; run: () => void }
  /** How long it stays, ms (default 5000; errors 8000). */
  ms?: number
}

interface Live extends ToastSpec {
  id: number
}

const Ctx = createContext<(t: ToastSpec) => void>(() => {})

/** useToast shows a short message at the bottom, optionally with one action (Undo, Retry). */
export function useToast() {
  return useContext(Ctx)
}

/** ToastProvider renders the toasts in a polite live region. */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [list, setList] = useState<Live[]>([])
  const next = useRef(1)
  const close = useCallback((id: number) => setList((l) => l.filter((t) => t.id !== id)), [])
  const show = useCallback((t: ToastSpec) => {
    const id = next.current++
    setList((l) => [...l.slice(-2), { ...t, id }])
  }, [])
  const value = useMemo(() => show, [show])
  return (
    <Ctx.Provider value={value}>
      {children}
      <div aria-live="polite" role="status"
        className="pointer-events-none fixed inset-x-0 bottom-[calc(88px+env(safe-area-inset-bottom))] z-[80] flex flex-col items-center gap-2 px-4 md:bottom-6 md:items-end md:pr-6">
        {list.map((t) => <ToastItem key={t.id} t={t} onClose={() => close(t.id)} />)}
      </div>
    </Ctx.Provider>
  )
}

function ToastItem({ t, onClose }: { t: Live; onClose: () => void }) {
  useEffect(() => {
    const id = setTimeout(onClose, t.ms ?? (t.tone === 'error' ? 8000 : 5000))
    return () => clearTimeout(id)
  }, [t, onClose])
  return (
    <div className={`card-glass pointer-events-auto flex w-full max-w-[420px] items-center gap-3 px-4 py-3 text-[14px] shadow-[0_16px_40px_rgba(0,0,0,.45)] ${
      t.tone === 'error' ? 'border-red/60 text-err' : 'text-text'}`}>
      <span className="flex-1">{t.text}</span>
      {t.action && (
        <button type="button" onClick={() => { t.action!.run(); onClose() }}
          className="font-mono text-[12px] font-medium uppercase tracking-[.14em] text-lime hover:underline">
          {t.action.label}
        </button>
      )}
      <button type="button" onClick={onClose} aria-label="Dismiss" className="text-faint hover:text-text"><X size={15} /></button>
    </div>
  )
}
