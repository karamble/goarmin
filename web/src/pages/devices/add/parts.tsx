// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Check } from 'lucide-react'
import { forwardRef, useEffect, useState, type ReactNode } from 'react'
import { Ring } from '../../../charts/svg/Ring'
import { codeGroups, codeSpoken, pairSteps } from '../../../lib/pairing'

/** StepHeading is a step's title; the wizard moves the focus to it when the step changes. */
export const StepHeading = forwardRef<HTMLHeadingElement, { children: ReactNode }>(function StepHeading({ children }, ref) {
  return (
    <h2 ref={ref} tabIndex={-1} data-step-heading
      className="m-0 font-display text-[28px] uppercase leading-tight tracking-[.02em] outline-none sm:text-[34px]">
      {children}
    </h2>
  )
})

/** StepRail lists the five steps (a progress header on phones); step is 0-based. */
export function StepRail({ step }: { step: number }) {
  return (
    <>
      <div className="lg:hidden">
        <p className="m-0 font-mono text-[11px] uppercase tracking-[.16em] text-muted">
          Step {step + 1} of {pairSteps.length} · <span className="text-text">{pairSteps[step]}</span>
        </p>
        <div className="mt-2 h-1 overflow-hidden rounded-full bg-track" aria-hidden>
          <div className="h-full rounded-full bg-lime transition-[width] duration-500 motion-reduce:transition-none"
            style={{ width: `${((step + 1) / pairSteps.length) * 100}%` }} />
        </div>
      </div>
      <nav aria-label="Steps" className="hidden lg:block">
        <ol className="m-0 grid list-none gap-1 p-0">
          {pairSteps.map((name, i) => {
            const done = i < step
            const now = i === step
            return (
              <li key={name} aria-current={now ? 'step' : undefined}
                className={`flex items-center gap-3 rounded-xl px-3 py-2.5 text-[14px] ${now ? 'bg-panel2 text-text' : done ? 'text-muted' : 'text-faint'}`}>
                <span aria-hidden className={`grid h-7 w-7 shrink-0 place-items-center rounded-full border font-mono text-[12px] ${
                  now ? 'border-lime text-lime shadow-[0_0_14px_var(--lime-glow)]' : done ? 'border-lime-dim bg-lime-fill text-lime' : 'border-border'}`}>
                  {done ? <Check size={14} /> : i + 1}
                </span>
                {name}
                {done && <span className="sr-only">(done)</span>}
              </li>
            )
          })}
        </ol>
      </nav>
    </>
  )
}

/** CodeDisplay shows a pairing code big, in lime mono, read digit by digit to screen readers. */
export function CodeDisplay({ code }: { code: string }) {
  return (
    <p role="img" aria-label={`Code ${codeSpoken(code)}`}
      className="m-0 select-all font-mono text-[56px] font-bold leading-none tracking-[.12em] text-lime [text-shadow:0_0_28px_var(--lime-glow)] sm:text-[72px]">
      {codeGroups(code)}
    </p>
  )
}

/** useNow ticks every second while on. */
function useNow(on: boolean) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!on) return
    setNow(Date.now())
    const t = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(t)
  }, [on])
  return now
}

/** Countdown empties a ring towards deadline (Unix seconds), with the seconds left as text;
 * screen readers hear it at 20, 10 and 5 seconds. */
export function Countdown({ deadline, total = 30 }: { deadline: number; total?: number }) {
  const now = useNow(true)
  const left = Math.max(0, Math.ceil(deadline - now / 1000))
  const [said, setSaid] = useState('')
  useEffect(() => {
    if (left === 20 || left === 10 || left === 5) setSaid(`${left} seconds left`)
  }, [left])
  return (
    <div className="flex items-center gap-3">
      <Ring value={left} max={total} size={52} stroke={5} color={left <= 5 ? 'var(--amber)' : 'var(--lime)'} label={`${left} seconds left`}>
        <span className="font-mono text-[15px] tabular-nums">{left}</span>
      </Ring>
      <span className="text-[13px] text-muted">{left > 0 ? `${left} s left to answer` : 'Time is up'}</span>
      <span className="sr-only" aria-live="polite">{said}</span>
    </div>
  )
}

/** SignalBars draws 0–3 bars. */
export function SignalBars({ bars }: { bars: number }) {
  return (
    <svg width="18" height="14" viewBox="0 0 18 14" aria-hidden className="shrink-0">
      {[0, 1, 2].map((i) => (
        <rect key={i} x={i * 6} y={10 - i * 4} width="4" height={4 + i * 4} rx="1"
          fill={i < bars ? 'var(--lime)' : 'var(--track)'} />
      ))}
    </svg>
  )
}
