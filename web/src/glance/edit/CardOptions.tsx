// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Sheet } from '../../ui/Sheet'
import { cardTitle } from '../CardSlot'
import { optionsFor } from '../layout'
import type { CardDef, PlacedCard } from '../types'

/** CardOptions edits a card's options; every change shows (and saves) at once. */
export function CardOptions({ c, def, onChange, onClose }: { c: PlacedCard; def: CardDef; onChange: (o: Record<string, unknown>) => void; onClose: () => void }) {
  const o = optionsFor(def, c)
  return (
    <Sheet title={`${cardTitle(c)} options`} onClose={onClose}
      footer={<button type="button" onClick={onClose} className="w-full rounded-full border-2 border-lime bg-lime py-2 text-[14px] font-bold text-bg">Done</button>}>
      <div className="grid gap-5">
        {def.fields?.map((f) =>
          f.kind === 'select' ? (
            <label key={f.key} className="field-label">
              {f.label}
              <select value={String(o[f.key])} onChange={(e) => onChange({ [f.key]: e.target.value })}
                className="field font-sans text-[15px] normal-case tracking-normal text-text">
                {f.choices.map((ch) => <option key={ch.value} value={ch.value}>{ch.label}</option>)}
              </select>
            </label>
          ) : (
            <div key={f.key} className="flex items-center justify-between gap-4">
              <span id={`opt-${f.key}`} className="text-[14px]">{f.label}</span>
              <button type="button" role="switch" aria-checked={!!o[f.key]} aria-labelledby={`opt-${f.key}`} onClick={() => onChange({ [f.key]: !o[f.key] })}
                className={`relative h-6 w-11 rounded-full transition-colors ${o[f.key] ? 'bg-lime' : 'bg-track'}`}>
                <span className={`absolute top-0.5 h-5 w-5 rounded-full bg-text transition-[left] ${o[f.key] ? 'left-[22px] !bg-bg' : 'left-0.5'}`} />
              </button>
            </div>
          ),
        )}
      </div>
    </Sheet>
  )
}
