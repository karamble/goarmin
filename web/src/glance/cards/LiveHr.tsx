// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { HeartPulse, Radio, Square } from 'lucide-react'
import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import { useDevices } from '../../api/hooks/data'
import { useLive } from '../../api/hooks/live'
import { Chart, sparkline } from '../../charts'
import { emptySeries, endText } from '../../lib/live'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'
import { ctx, n } from './parts'

/** LiveHr opens the watch's live link only when tapped (it holds the Bluetooth link). */
export default function LiveHr({ size, tz }: CardProps) {
  const q = useDevices()
  const dev = q.data?.find((d) => d.current && d.sync)
  const live = useLive(dev?.id, { keepS: 120 })
  const hr = live.latest.heart_rate?.value ?? null
  const series = live.series.heart_rate ?? emptySeries
  const now = series.t.at(-1) ?? 0
  const option = useMemo(
    () => sparkline({ v: series.v, t: series.t, from: now - 120, to: now, area: true, lastDot: true }, ctx(tz)),
    [series, now, tz],
  )
  const on = live.state === 'connecting' || live.state === 'live'
  return (
    <Body q={[q]} empty={!dev} size={size} emptyText="No watch syncs through this server">
      {() => (
        <div className="flex h-full flex-col gap-2">
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-baseline gap-1.5" aria-live="polite">
              <HeartPulse size={20} className={live.state === 'live' ? 'animate-pulse text-lime' : 'text-faint'} aria-hidden />
              <span className="num text-[40px] leading-none">{live.state === 'live' ? n(hr) : '–'}</span>
              <span className="text-[13px] text-muted">bpm</span>
            </div>
            <button type="button" onClick={on ? live.stop : live.start}
              className={`inline-flex items-center gap-2 rounded-full border-2 px-3.5 py-1.5 text-[13px] font-bold ${
                on ? 'border-border text-muted hover:text-text' : 'border-lime bg-lime text-bg'}`}>
              {on ? <><Square size={13} /> Stop</> : <><Radio size={14} /> Go live</>}
            </button>
          </div>
          <div className="min-h-0 flex-1">
            {live.state === 'live' && series.v.length > 1 ? (
              <Chart ariaLabel="Heart rate, the last two minutes" height="100%" option={option} lazy={false} />
            ) : (
              <p className="m-0 text-[13px] text-muted">
                {live.state === 'connecting' ? 'Connecting to the watch (about 10 s)…'
                  : live.state === 'error' ? <span className="text-err">{live.error}</span>
                  : live.state === 'ended' ? `${endText(live.end)} Go live to try again.`
                  : <>Streams {dev!.nickname || dev!.name || 'the watch'}'s heart rate while this is open. <Link to={`/live?watch=${dev!.id}`} className="text-lime hover:underline">Everything live</Link></>}
              </p>
            )}
          </div>
        </div>
      )}
    </Body>
  )
}
