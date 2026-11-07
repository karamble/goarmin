// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useEffect, useRef, useState, type ReactNode } from 'react'

export interface MenuItem {
  label: string
  run: () => void
  disabled?: boolean
  checked?: boolean
  danger?: boolean
  /** Starts a new group with this heading. */
  group?: string
}

/** Menu is a button with a small list of actions; arrows move, Esc closes. */
export function Menu({ label, icon, items, className = '' }: { label: string; icon: ReactNode; items: MenuItem[]; className?: string }) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!open) return
    const first = box.current?.querySelector<HTMLButtonElement>('[role^="menuitem"]:not(:disabled)')
    first?.focus()
    const click = (e: MouseEvent) => !box.current?.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', click)
    return () => document.removeEventListener('mousedown', click)
  }, [open])
  const onKey = (e: React.KeyboardEvent) => {
    const all = Array.from(box.current?.querySelectorAll<HTMLButtonElement>('[role^="menuitem"]:not(:disabled)') ?? [])
    const i = all.indexOf(document.activeElement as HTMLButtonElement)
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      setOpen(false)
      button.current?.focus()
    } else if (e.key === 'ArrowDown') {
      e.preventDefault()
      all[(i + 1) % all.length]?.focus()
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      all[(i - 1 + all.length) % all.length]?.focus()
    }
  }
  return (
    <div ref={box} className={`relative ${className}`} onKeyDown={onKey}>
      <button ref={button} type="button" aria-label={label} title={label} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)}
        className="grid h-7 w-7 place-items-center rounded-lg text-muted hover:bg-panel2 hover:text-text">
        {icon}
      </button>
      {open && (
        <div role="menu" aria-label={label} className="card-glass absolute right-0 top-full z-40 mt-1 min-w-[190px] p-1.5">
          {items.map((it, i) => (
            <div key={i}>
              {it.group && <p className="caps m-0 px-2.5 pb-1 pt-2 !text-[10px] !text-faint">{it.group}</p>}
              <button type="button" role={it.checked != null ? 'menuitemradio' : 'menuitem'} aria-checked={it.checked} disabled={it.disabled}
                onClick={() => { setOpen(false); it.run() }}
                className={`flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-[13px] disabled:opacity-35 ${
                  it.danger ? 'text-err hover:bg-red/10' : 'text-text hover:bg-panel2'} ${it.checked ? 'text-lime' : ''}`}>
                <span className="w-3 text-lime">{it.checked ? '•' : ''}</span>
                {it.label}
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
