// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useMemo } from 'react'
import { useDaySeries } from '../../api/hooks/data'
import { Arc, Chart, describeSeries, sparkline, stats } from '../../charts'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { ctx, Kpi, last, n, Stat } from './parts'

export default function BodyBattery({ day, size, isToday, tz }: CardProps) {
  const q = useDaySeries('body_battery', day)
  const s0 = q.data
  const v = s0?.v ?? []
  const option = useMemo(
    () => sparkline({ v: s0?.v ?? [], t: s0?.t, from: s0?.from, to: s0?.to, gapS: 900, area: true, lastDot: true, min: 0, max: 100 }, ctx(tz)),
    [s0, tz],
  )
  const now = last(v)
  const s = stats(v)
  let charged = 0
  let drained = 0
  for (let i = 1; i < v.length; i++) {
    const d = v[i] - v[i - 1]
    if (d > 0) charged += d
    else drained -= d
  }
  const aria = describeSeries('Body Battery', v, '%')
  const when = isToday ? 'now' : 'end of day'
  return (
    <Body q={[q]} empty={v.length === 0} isToday={isToday} size={size}>
      {() =>
        size === 'S' ? (
          <div className="flex h-full items-end justify-between gap-3">
            <Kpi value={n(now)} unit="%">{when}</Kpi>
            <div className="w-[45%]"><Chart ariaLabel={aria} height={52} option={option} /></div>
          </div>
        ) : (
          <div className="flex h-full flex-col gap-2">
            <div className="flex flex-1 items-center gap-5">
              <Arc value={now} size={size === 'L' ? 150 : 132} label={`Body Battery ${n(now)} percent ${when}`}>
                <span className="num text-[38px] leading-none">{n(now)}</span>
                <span className="caps mt-1 !text-[10px]">{when}</span>
              </Arc>
              <dl className="m-0 grid flex-1 grid-cols-2 gap-x-4 gap-y-4">
                <Stat label="High" value={n(s?.max)} />
                <Stat label="Low" value={n(s?.min)} />
                <Stat label="Charged" value={'+' + n(charged)} tone="lime" />
                <Stat label="Drained" value={'−' + n(drained)} tone="muted" />
              </dl>
            </div>
            <Chart ariaLabel={aria} height={size === 'L' ? 76 : 58} option={option} />
          </div>
        )
      }
    </Body>
  )
}
