// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { ArrowUpRight, RotateCw } from 'lucide-react'
import { Component, Suspense, type ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { ApiError } from '../api/client'
import { Skeleton } from '../ui/kit'
import type { CardSize } from './sizes'
import type { CardDef } from './types'

/** CardFrame is a card's chrome: title, link, an error boundary and a sized skeleton. */
export function CardFrame({ def, size, resetKey, title, editing, children }: {
  def: Pick<CardDef, 'title' | 'icon' | 'to'>
  size: CardSize
  resetKey: string
  title?: string
  editing?: boolean
  children: ReactNode
}) {
  const location = useLocation()
  const q = new URLSearchParams(location.search)
  const d = q.get('d')
  return (
    <article aria-label={title ?? def.title} className={`card flex h-full min-h-0 flex-col overflow-hidden p-4 sm:p-[18px] ${editing ? '' : 'card-hover'}`}>
      <header className="mb-3 flex min-w-0 items-center gap-2">
        <def.icon size={15} strokeWidth={1.75} className="shrink-0 text-faint" aria-hidden />
        <h2 className="caps m-0 min-w-0 flex-1 truncate">{title ?? def.title}</h2>
        {def.to && !editing && (
          <Link to={def.to + (d ? `?d=${d}` : '')} aria-label={`Open ${title ?? def.title}`}
            className="-m-1 grid h-7 w-7 shrink-0 place-items-center rounded-full text-faint transition-colors hover:text-lime">
            <ArrowUpRight size={16} />
          </Link>
        )}
      </header>
      <div className="relative min-h-0 flex-1">
        <Boundary resetKey={resetKey}>
          <Suspense fallback={<Loading size={size} />}>{children}</Suspense>
        </Boundary>
      </div>
    </article>
  )
}

/** Loading is a skeleton shaped like a card of this size. */
export function Loading({ size }: { size: CardSize }) {
  if (size === 'S') {
    return (
      <div className="flex h-full flex-col justify-end gap-2">
        <Skeleton className="h-9 w-24" />
        <Skeleton className="h-3 w-16" />
      </div>
    )
  }
  return (
    <div className="flex h-full flex-col gap-3">
      <div className="flex flex-1 items-center gap-5">
        <Skeleton className="h-28 w-28 rounded-full" />
        <div className="grid flex-1 gap-3">
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-4 w-1/2" />
          <Skeleton className="h-4 w-2/3" />
        </div>
      </div>
      <Skeleton className="h-12 w-full" />
    </div>
  )
}

/** Empty says why there is nothing to show. */
export function Empty({ children, isToday }: { children?: ReactNode; isToday?: boolean }) {
  return (
    <div className="grid h-full place-items-center text-center">
      <div>
        <p className="m-0 text-[14px] text-muted">{children ?? 'No data for this day'}</p>
        {isToday && <p className="m-0 mt-1 text-[12px] text-faint">It appears after the next sync of your watch.</p>}
      </div>
    </div>
  )
}

/** Failed shows an error with Retry. */
export function Failed({ error, retry }: { error: unknown; retry?: () => void }) {
  const msg = error instanceof ApiError ? error.message : 'Could not load this card'
  return (
    <div role="alert" className="grid h-full place-items-center text-center">
      <div>
        <p className="m-0 text-[13px] text-err">{msg}</p>
        {retry && (
          <button type="button" onClick={retry} className="mt-2 inline-flex items-center gap-1.5 font-mono text-[11px] uppercase tracking-[.14em] text-muted hover:text-lime">
            <RotateCw size={12} /> Retry
          </button>
        )}
      </div>
    </div>
  )
}

interface QueryLike {
  isPending: boolean
  isError: boolean
  error: unknown
  data?: unknown
  refetch: () => unknown
}

/** Body picks the state: loading until every query has data, the error, empty, or the content. */
export function Body({ q, empty, isToday, size, emptyText, children }: {
  q: QueryLike[]
  empty: boolean
  isToday?: boolean
  size: CardSize
  emptyText?: ReactNode
  children: () => ReactNode
}) {
  const failed = q.find((x) => x.isError && x.data === undefined)
  if (failed) return <Failed error={failed.error} retry={() => q.forEach((x) => x.isError && x.refetch())} />
  if (q.some((x) => x.isPending)) return <Loading size={size} />
  if (empty) return <Empty isToday={isToday}>{emptyText}</Empty>
  return <>{children()}</>
}

class Boundary extends Component<{ resetKey: string; children: ReactNode }, { error: unknown; key: string }> {
  state = { error: null as unknown, key: this.props.resetKey }
  static getDerivedStateFromError(error: unknown) {
    return { error }
  }
  static getDerivedStateFromProps(p: { resetKey: string }, s: { error: unknown; key: string }) {
    return p.resetKey !== s.key ? { error: null, key: p.resetKey } : null
  }
  render() {
    if (this.state.error) return <Failed error={this.state.error} retry={() => this.setState({ error: null })} />
    return this.props.children
  }
}
