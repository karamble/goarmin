// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { render } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

const inst = { setOption: vi.fn(), on: vi.fn(), off: vi.fn(), dispose: vi.fn(), resize: vi.fn(), group: '' }
vi.mock('./echarts', () => ({
  echarts: { init: vi.fn(() => inst), connect: vi.fn(), disconnect: vi.fn(), registerTheme: vi.fn() },
}))

const { Chart } = await import('./Chart')
const { echarts } = await import('./echarts')

function prefersReduced(on: boolean) {
  window.matchMedia = (q: string) =>
    ({ matches: on && q.includes('reduce'), media: q, addEventListener() {}, removeEventListener() {} }) as unknown as MediaQueryList
}

describe('useECharts', () => {
  afterEach(() => prefersReduced(false))

  it('applies the option as is', () => {
    prefersReduced(false)
    const option = { series: [] }
    render(<Chart ariaLabel="x" option={option} lazy={false} />)
    expect(inst.setOption).toHaveBeenLastCalledWith(option, { notMerge: true, lazyUpdate: true })
  })

  it('turns animation off with reduced motion', () => {
    prefersReduced(true)
    render(<Chart ariaLabel="x" option={{ series: [] }} lazy={false} />)
    expect(inst.setOption).toHaveBeenLastCalledWith({ series: [], animation: false }, expect.anything())
  })

  it('joins and leaves its group, and disposes', () => {
    const { unmount } = render(<Chart ariaLabel="x" option={{}} group="g1" lazy={false} />)
    expect(echarts.connect).toHaveBeenCalledWith('g1')
    unmount()
    expect(echarts.disconnect).toHaveBeenCalledWith('g1')
    expect(inst.dispose).toHaveBeenCalled()
  })

  it('labels the chart for screen readers', () => {
    const { getByRole } = render(<Chart ariaLabel="Heart rate today" option={{}} lazy={false} />)
    expect(getByRole('img').getAttribute('aria-label')).toBe('Heart rate today')
  })
})
