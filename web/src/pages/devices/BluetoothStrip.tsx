// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Bluetooth, BluetoothOff } from 'lucide-react'
import { useBluetooth, usePowerOn } from '../../api/hooks/devices'
import { errorText } from '../../api/client'
import { timeOfDay } from '../../lib/format'

const busyText = { sync: 'syncing a watch', live: 'streaming live values', pairing: 'adding a watch' } as const

/** BluetoothStrip is this computer's adapter: on, off (Turn on) or blocked by the system. */
export function BluetoothStrip({ tz }: { tz: string }) {
  const q = useBluetooth(true)
  const power = usePowerOn()
  const b = q.data
  if (!b) return null
  const on = b.available && b.power_state === 'on'
  const blocked = b.power_state === 'off-blocked'
  return (
    <section aria-label="Bluetooth on this computer"
      className={`card flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 ${on ? '' : 'border-amber/50'}`}>
      {on ? <Bluetooth size={18} className="text-lime" aria-hidden /> : <BluetoothOff size={18} className="text-amber" aria-hidden />}
      <div className="min-w-0 flex-1">
        <p className="m-0 text-[14px]">
          {!b.available ? 'Bluetooth is not reachable on this computer'
            : on ? 'Bluetooth is on' : blocked ? 'Bluetooth is blocked on this computer' : 'Bluetooth is off'}
          {b.adapter && <span className="ml-2 font-mono text-[12px] text-faint">{b.adapter} · {b.address}</span>}
        </p>
        <p className="m-0 text-[12px] text-muted">
          {!b.available ? (b.error ?? 'Is bluetoothd running?')
            : blocked ? 'The system blocks the radio (rfkill). Unblock it in the system settings; goarmin cannot.'
            : !on ? 'Watches cannot sync until it is on.'
            : b.busy ? `In use since ${timeOfDay(b.busy.since, tz)}: ${busyText[b.busy.kind]}.` : 'Free.'}
        </p>
        {power.isError && <p role="alert" className="m-0 mt-1 text-[12px] text-err">{errorText(power.error)}</p>}
      </div>
      {b.available && !on && !blocked && (
        <button type="button" onClick={() => power.mutate()} disabled={power.isPending}
          className="rounded-full border-2 border-lime bg-lime px-4 py-1.5 text-[13px] font-bold text-bg disabled:opacity-50">
          Turn on
        </button>
      )}
    </section>
  )
}
