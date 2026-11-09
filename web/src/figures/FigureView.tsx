// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { ChevronLeft, ChevronRight } from 'lucide-react'
import { lazy, Suspense, useEffect, useMemo, useState } from 'react'
import { FIGURE_DISCLAIMER } from '../lib/disclaimer'
import { useExercisePose, type ExerciseInfo } from './api'
import type { View } from './FigureCanvas'
import { hasWebGL, useReducedMotion } from './motion'

// three.js loads with the first figure, in its own chunk.
const FigureCanvas = lazy(() => import('./FigureCanvas'))

const VIEW_LABELS: [View, string][] = [['three', '¾'], ['front', 'Front'], ['side', 'Side'], ['back', 'Back'], ['top', 'Top']]

/** qualityText says where an exercise's figure comes from. */
export function qualityText(q: ExerciseInfo['quality'] | undefined): string {
  if (q === 'own') return 'Posed for this exercise'
  if (q === 'family') return 'Shared with similar movements'
  return 'Its category’s figure'
}

/**
 * FigureView shows an exercise's 3D figure: the mannequin in its key poses, a view picker, and with
 * reduced motion a key-pose stepper instead of movement. It needs WebGL; without it, a note.
 */
export function FigureView({ category, number, title }: { category: string; number: number; title?: string }) {
  const q = useExercisePose(category, number)
  const reduced = useReducedMotion()
  const webgl = useMemo(() => hasWebGL(), [])
  const [view, setView] = useState<View>('three')
  const [key, setKey] = useState(0)
  useEffect(() => setKey(0), [category, number])
  const pose = q.data
  const name = title || pose?.exercise.title || ''
  const keys = pose?.keys.length ?? 0
  const label = pose ? `${name}: a 3D figure ${keys > 1 ? (reduced ? `showing key pose ${key + 1} of ${keys}` : `moving between ${keys} key poses`) : 'holding the pose'}` : name

  let body
  if (!webgl) {
    body = <Note>This browser cannot draw the 3D figure (WebGL is off or missing).</Note>
  } else if (q.isPending) {
    body = <div className="aspect-[4/3] w-full animate-pulse rounded-xl bg-bg2" aria-label="Loading the figure" />
  } else if (q.isError) {
    body = <Note>The figure could not be loaded.</Note>
  } else if (!pose) {
    body = <Note>No figure for this exercise.</Note>
  } else {
    body = (
      <Suspense fallback={<div className="aspect-[4/3] w-full animate-pulse rounded-xl bg-bg2" aria-label="Loading the figure" />}>
        <FigureCanvas pose={pose} view={view} reduced={reduced} keyIndex={key} label={label} />
      </Suspense>
    )
  }
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-3">
      {body}
      {webgl && pose && (
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div role="group" aria-label="View" className="flex flex-wrap gap-1">
            {VIEW_LABELS.map(([v, text]) => (
              <button key={v} type="button" aria-pressed={view === v} onClick={() => setView(v)}
                className={`rounded-full border px-3 py-1 text-[12px] ${view === v ? 'border-lime text-lime' : 'border-border text-muted hover:text-text'}`}>
                {text}
              </button>
            ))}
          </div>
          {reduced && keys > 1 && (
            <div className="flex items-center gap-1 text-[12px] text-muted">
              <button type="button" aria-label="Previous key pose" onClick={() => setKey((k) => (k + keys - 1) % keys)} className="grid h-7 w-7 place-items-center rounded-full hover:text-text"><ChevronLeft size={16} /></button>
              <span className="num">Pose {key + 1} of {keys}</span>
              <button type="button" aria-label="Next key pose" onClick={() => setKey((k) => (k + 1) % keys)} className="grid h-7 w-7 place-items-center rounded-full hover:text-text"><ChevronRight size={16} /></button>
            </div>
          )}
        </div>
      )}
      {pose && <p className="m-0 text-[12px] text-faint">{qualityText(pose.exercise.quality)} · drag to turn</p>}
      {pose && <p className="m-0 text-[12px] text-faint">{FIGURE_DISCLAIMER}</p>}
    </div>
  )
}

function Note({ children }: { children: string }) {
  return <p className="m-0 grid aspect-[4/3] w-full place-items-center rounded-xl bg-bg2 px-6 text-center text-[13px] text-muted">{children}</p>
}
