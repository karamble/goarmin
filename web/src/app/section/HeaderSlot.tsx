// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { createContext, useContext, useEffect, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

/** The element in the section header where a page puts its own actions. */
export const HeaderSlotContext = createContext<HTMLElement | null>(null)

/** HeaderActions renders its children into the section header, beside the date navigator. */
export function HeaderActions({ children }: { children: ReactNode }) {
  const el = useContext(HeaderSlotContext)
  return el ? createPortal(children, el) : null
}

/** Lets a page name the header (a watch's page shows the watch's name). */
export const SectionTitleContext = createContext<(title: string) => void>(() => {})

/** useSectionTitle sets the section header's title while the page is shown. */
export function useSectionTitle(title: string | undefined) {
  const set = useContext(SectionTitleContext)
  useEffect(() => {
    set(title ?? '')
    return () => set('')
  }, [set, title])
}
