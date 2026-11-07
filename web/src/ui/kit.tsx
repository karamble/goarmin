// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode } from 'react'

type Variant = 'primary' | 'ghost' | 'danger' | 'quiet'

const variants: Record<Variant, string> = {
  primary: 'bg-lime text-bg border-lime shadow-[0_0_28px_var(--lime-glow)] hover:shadow-[0_0_44px_var(--lime-glow)] hover:-translate-y-0.5',
  ghost: 'bg-transparent text-text border-[rgba(230,237,243,.35)] hover:border-lime hover:text-lime',
  danger: 'bg-transparent text-err border-red/60 hover:border-err',
  quiet: 'bg-transparent text-muted border-transparent hover:text-text',
}

/** Button in the micavoss pill style (site.css:34-41). */
export function Button({ variant = 'ghost', small, className = '', ...p }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: Variant; small?: boolean }) {
  return (
    <button
      {...p}
      className={`inline-flex items-center justify-center gap-2 rounded-full border-2 font-bold tracking-[.02em] transition-[transform,box-shadow,border-color,color] duration-200 disabled:opacity-45 disabled:pointer-events-none ${
        small ? 'px-4 py-2 text-[13px]' : 'px-6 py-3 text-[15px]'
      } ${variants[variant]} ${className}`}
    />
  )
}

export function Card({ children, className = '', glass }: { children: ReactNode; className?: string; glass?: boolean }) {
  return <section className={`${glass ? 'card-glass' : 'card'} ${className}`}>{children}</section>
}

export function Eyebrow({ children, muted }: { children: ReactNode; muted?: boolean }) {
  return <p className={`eyebrow ${muted ? 'eyebrow-muted' : ''} m-0`}>{children}</p>
}

export function Field({ label, hint, ...p }: InputHTMLAttributes<HTMLInputElement> & { label: string; hint?: ReactNode }) {
  return (
    <label className="field-label">
      {label}
      <input {...p} className="field normal-case tracking-normal text-[15px] font-sans text-text" />
      {hint && <span className="normal-case tracking-normal font-sans text-[13px] text-faint">{hint}</span>}
    </label>
  )
}

export function Notice({ tone = 'info', children }: { tone?: 'info' | 'error' | 'ok'; children: ReactNode }) {
  const tones = {
    info: 'border-border text-muted',
    error: 'border-red/50 text-err bg-red/5',
    ok: 'border-lime-dim text-lime bg-lime-fill',
  }
  return <div role={tone === 'error' ? 'alert' : 'status'} className={`rounded-xl border px-4 py-3 text-sm ${tones[tone]}`}>{children}</div>
}

export function Spinner({ label = 'Loading' }: { label?: string }) {
  return (
    <span role="status" aria-label={label} className="inline-block h-5 w-5 animate-spin rounded-full border-2 border-track border-t-lime" />
  )
}

/** The goarmin mark: a lime ring with a heartbeat. */
export function Mark({ size = 30 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden="true">
      <circle cx="16" cy="16" r="14" fill="#161b22" stroke="var(--lime)" strokeWidth="2" />
      <path d="M6 17h5l2.5-6 4 11 2.5-5H26" fill="none" stroke="var(--lime)" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

/** Skeleton is a pulsing placeholder in the shape of what is coming. */
export function Skeleton({ className = '' }: { className?: string }) {
  return <div aria-hidden className={`animate-pulse rounded-lg bg-panel2 ${className}`} />
}
