// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Link } from 'react-router-dom'
import type { PairView } from '../../../api/types'
import { endText } from '../../../lib/pairing'
import { Button } from '../../../ui/kit'
import { StepHeading } from './parts'

/** Ended is a session that stopped without adding a watch: failed, cancelled or timed out. */
export function Ended({ view, onRestart }: { view: PairView; onRestart: () => void }) {
  const { title, text } = endText(view)
  return (
    <div className="grid gap-5">
      <StepHeading>{title}</StepHeading>
      <section className={`card p-6 text-[15px] ${view.state === 'failed' ? 'border-amber/50' : ''}`}>
        <p className="m-0 max-w-[620px] text-muted">{text}</p>
      </section>
      <div className="flex flex-wrap gap-3">
        <Button variant="primary" onClick={onRestart}>Start again</Button>
        <Link to="/devices/watches"
          className="inline-flex items-center rounded-full border-2 border-[rgba(230,237,243,.35)] px-6 py-3 text-[15px] font-bold hover:border-lime hover:text-lime">
          Back to watches
        </Link>
      </div>
    </div>
  )
}
