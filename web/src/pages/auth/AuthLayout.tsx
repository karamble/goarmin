// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { ReactNode } from 'react'
import { Mark } from '../../ui/kit'

/** AuthLayout is the frame of the public pages: the mark, a lime wash, one glass card. */
export function AuthLayout({ eyebrow, title, children }: { eyebrow: string; title: string; children: ReactNode }) {
  return (
    <div className="relative grid min-h-dvh place-items-center overflow-hidden px-4 py-10">
      <div aria-hidden className="pointer-events-none absolute inset-0 bg-[radial-gradient(55%_45%_at_25%_30%,rgba(200,245,66,.07),transparent_70%)]" />
      <div className="relative w-full max-w-[420px]">
        <div className="mb-8 flex items-center gap-3">
          <Mark size={36} />
          <span className="font-display text-[22px] font-semibold uppercase tracking-[.12em]">goarmin</span>
        </div>
        <section className="card-glass p-7 shadow-[0_30px_80px_rgba(0,0,0,.35)] sm:p-9">
          <p className="eyebrow mb-3">{eyebrow}</p>
          <h1 className="title mb-6">{title}</h1>
          {children}
        </section>
      </div>
    </div>
  )
}
