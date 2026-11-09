// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useEffect, useRef, useState } from 'react'

const box = 'min-w-0 rounded-lg border bg-bg2 px-2.5 py-1.5 text-[14px] text-text focus:border-lime focus:outline-none tabular-nums'

/** ParsedInput is a text field for a value typed in a form ("1:30", "5:15"): the value goes out
 * as soon as the text reads as one, and the field says so on leaving when it does not. While
 * the field is not being typed in, it shows the value as format writes it. */
export function ParsedInput<T>({ label, value, parse, format, onChange, placeholder, className = '' }: {
  label: string
  value: T | undefined
  parse: (s: string) => T | null
  format: (v: T) => string
  onChange: (v: T) => void
  placeholder?: string
  className?: string
}) {
  const shown = value !== undefined && value !== null ? format(value) : ''
  const [text, setText] = useState(shown)
  const [bad, setBad] = useState(false)
  const typing = useRef(false)
  useEffect(() => {
    if (!typing.current) setText(shown)
  }, [shown])
  return (
    <input aria-label={label} value={text} placeholder={placeholder} inputMode="numeric" aria-invalid={bad || undefined}
      className={`${box} ${bad ? 'border-[#c9605f]' : 'border-border'} ${className || 'w-full'}`}
      onFocus={() => { typing.current = true }}
      onChange={(e) => {
        setText(e.target.value)
        const v = parse(e.target.value)
        if (v !== null) {
          setBad(false)
          onChange(v)
        }
      }}
      onBlur={() => {
        typing.current = false
        const v = parse(text)
        setBad(v === null && text.trim() !== '')
        if (v !== null) setText(format(v))
      }} />
  )
}

/** NumberInput is a field for a plain number (reps, bpm, km), in whole numbers unless decimals. */
export function NumberInput({ label, value, onChange, decimals = 0, min = 0, max, className = '' }: {
  label: string
  value: number | undefined
  onChange: (v: number) => void
  decimals?: number
  min?: number
  max?: number
  className?: string
}) {
  const round = (v: number) => Number(v.toFixed(decimals))
  return (
    <ParsedInput label={label} value={value} className={className}
      format={(v) => String(round(v))}
      parse={(s) => {
        const t = s.trim().replace(',', '.')
        if (!(decimals ? /^\d+(\.\d*)?$/ : /^\d+$/).test(t)) return null
        const v = round(Number(t))
        return v < min || (max !== undefined && v > max) ? null : v
      }}
      onChange={onChange} />
  )
}
