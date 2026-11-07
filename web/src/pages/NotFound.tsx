// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Link } from 'react-router-dom'

export default function NotFound() {
  return (
    <div className="mx-auto max-w-xl px-6 py-24 text-center">
      <p className="eyebrow justify-center">404</p>
      <h1 className="title mt-3">Nothing here</h1>
      <Link to="/" className="text-lime">Back to the glance</Link>
    </div>
  )
}
