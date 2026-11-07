// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useDevices } from '../../api/hooks/data'
import { syncLine, toneDot } from '../../lib/devices'
import { Body } from '../CardFrame'
import type { CardProps } from '../types'

export default function DeviceStatus({ size, tz }: CardProps) {
  const q = useDevices()
  const d = q.data?.find((x) => x.current)
  const line = d ? syncLine(d, tz) : null
  return (
    <Body q={[q]} empty={!d} size={size} emptyText="No watch yet">
      {() => (
        <div className="flex h-full flex-col justify-end gap-1">
          <p className="m-0 flex items-baseline justify-between gap-2">
            <span className="truncate font-display text-[20px] uppercase leading-tight tracking-[.02em]">{d!.nickname || d!.model || d!.name || 'Watch'}</span>
            {d!.battery != null && <span className={`num shrink-0 text-[13px] ${d!.battery < 20 ? 'text-amber' : 'text-muted'}`}>{d!.battery}%</span>}
          </p>
          <p className="m-0 flex items-center gap-2 text-[13px] text-muted">
            <span aria-hidden className={`h-2 w-2 shrink-0 rounded-full ${toneDot[line!.tone]}`} />
            <span className="truncate">{line!.text}</span>
          </p>
          {line!.detail && <p className="m-0 truncate text-[12px] text-faint" title={d!.sync?.last_error}>{line!.detail}</p>}
        </div>
      )}
    </Body>
  )
}
