// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { TimelineChart, TimelineLegend, useDayTimeline } from '../../day/DayTimeline'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'

export default function YourDay({ day, size, isToday, tz }: CardProps) {
  const t = useDayTimeline(day, tz)
  return (
    <Body q={[...t.queries]} empty={t.empty} isToday={isToday} size={size}>
      {() => (
        <div className="flex h-full flex-col">
          <div className="-mt-1 mb-1"><TimelineLegend /></div>
          <div className="min-h-0 flex-1"><TimelineChart t={t} height="100%" /></div>
        </div>
      )}
    </Body>
  )
}
