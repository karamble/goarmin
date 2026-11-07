// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { LucideIcon } from 'lucide-react'
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'

/** A palette entry. */
export interface PaletteItem {
  id: string
  label: string
  hint?: string
  icon: LucideIcon
  run: () => void
}

interface Registry {
  items: PaletteItem[]
  set: (owner: string, items: PaletteItem[] | null) => void
}

const Ctx = createContext<Registry>({ items: [], set: () => {} })

/** PaletteActionsProvider holds the actions pages offer while they are open. */
export function PaletteActionsProvider({ children }: { children: ReactNode }) {
  const [byOwner, setByOwner] = useState<Record<string, PaletteItem[]>>({})
  const set = useCallback((owner: string, items: PaletteItem[] | null) => {
    setByOwner((m) => {
      const n = { ...m }
      if (items) n[owner] = items
      else delete n[owner]
      return n
    })
  }, [])
  const value = useMemo(() => ({ items: Object.values(byOwner).flat(), set }), [byOwner, set])
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

/** usePaletteActions offers items in the palette while the calling page is mounted. Pass a
 * stable list (useMemo), keyed by owner. */
export function usePaletteActions(owner: string, items: PaletteItem[]) {
  const { set } = useContext(Ctx)
  useEffect(() => {
    set(owner, items)
    return () => set(owner, null)
  }, [owner, items, set])
}

/** useRegisteredActions is what pages offer now. */
export function useRegisteredActions(): PaletteItem[] {
  return useContext(Ctx).items
}
