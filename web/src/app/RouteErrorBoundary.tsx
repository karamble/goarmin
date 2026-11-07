// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Component, type ReactNode } from 'react'

/** Catches a page that throws, so the rail and header keep working; resets when resetKey changes. */
export class RouteErrorBoundary extends Component<{ resetKey: string; children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  componentDidUpdate(prev: { resetKey: string }) {
    if (prev.resetKey !== this.props.resetKey && this.state.error) this.setState({ error: null })
  }
  render() {
    if (!this.state.error) return this.props.children
    return (
      <div role="alert" className="card m-6 p-6 text-err">
        <p className="caps mb-2 text-err">This page broke</p>
        <p className="m-0 text-sm">{this.state.error.message}</p>
      </div>
    )
  }
}
