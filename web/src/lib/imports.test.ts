// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { describe, expect, it } from 'vitest'
import type { USBWatch } from '../api/types'
import { finished, howWords, isExportPath, keptFolders, ownerWords, reportLine, selectionName, sortPicked, usbWatchName, wanted, type Picked } from './imports'

const pick = (path: string, size = 10): Picked => ({ file: new File([new Uint8Array(size)], path.split('/').pop()!), path })

describe('what is sent', () => {
  it('sends FIT files, zips and a Garmin Connect export, nothing else', () => {
    expect(wanted('GARMIN/Activity/A.FIT')).toBe(true)
    expect(wanted('export.zip')).toBe(true)
    expect(wanted('x/DI_CONNECT/DI-Connect-Fitness/a.json')).toBe(true)
    expect(wanted('GARMIN/Apps/app.PRG')).toBe(false)
    expect(wanted('notes.json')).toBe(false)
    expect(isExportPath('DI-Connect-User/x.json')).toBe(true)
    expect(isExportPath('DI_CONNECTED/x')).toBe(false)
    const s = sortPicked([pick('GARMIN/Activity/a.fit', 100), pick('GARMIN/Apps/x.PRG', 999), pick('b.zip', 50)])
    expect(s.send.map((f) => f.path)).toEqual(['GARMIN/Activity/a.fit', 'b.zip'])
    expect(s.left).toBe(1)
    expect(s.size).toBe(150)
  })

  it('names a selection', () => {
    expect(selectionName([pick('export.zip')])).toBe('export.zip')
    expect(selectionName([pick('GARMIN/a.fit'), pick('GARMIN/b/c.fit')])).toBe('GARMIN (2 files)')
    expect(selectionName([pick('a.fit'), pick('b.fit')])).toBe('2 files')
  })
})

describe('reports', () => {
  it('sums an import up', () => {
    expect(reportLine({ seen: 176, new: 5, known: 171 })).toBe('5 new, 171 already here')
    expect(reportLine({ seen: 10, new: 0, unchanged: 8, known: 2, failed: 1 })).toBe('0 new, 10 already here, 1 failed')
    expect(reportLine({ seen: 3, new: 1, known: 2, kept: 40, waiting: 1 })).toBe('1 new, 2 already here, 40 kept for later, 1 waiting for an owner')
    expect(reportLine({ seen: 0, other: 12 })).toBe('no FIT files, 12 other files left out')
    expect(reportLine({})).toBe('nothing to import')
    expect(reportLine({ seen: 12, kept: 3, new: 2 }, true)).toBe('15 files read, 2 new, 3 kept for later')
    expect(reportLine({ seen: 4 }, true)).toBe('4 files read')
  })

  it('lists an export’s folders, largest first', () => {
    expect(keptFolders({ kept_by: { 'DI-Connect-User': 2, 'DI-Connect-Fitness': 40, 'DI-Connect-Aggregator': 2 } }))
      .toEqual([['DI-Connect-Fitness', 40], ['DI-Connect-Aggregator', 2], ['DI-Connect-User', 2]])
    expect(keptFolders({})).toEqual([])
  })

  it('knows when an import is over', () => {
    expect(['done', 'failed', 'canceled'].every((s) => finished({ status: s as 'done' }))).toBe(true)
    expect(['uploading', 'queued', 'running'].some((s) => finished({ status: s as 'done' }))).toBe(false)
  })
})

describe('watches on USB', () => {
  const w = (over: Partial<USBWatch> = {}): USBWatch => ({
    id: 'usb-1', name: 'Garmin fenix 8', how: 'direct', port: '3-8', mounted: false, owner: 'unknown', device: null, auto: null,
    importing: null, last: null, ...over,
  })
  it('names them and says whose they are', () => {
    expect(usbWatchName(w())).toBe('Garmin fenix 8')
    expect(usbWatchName(w({ owner: 'yours', device: { id: 1, model: 'Fenix 8', name: 'fenix 8 - 47mm', nickname: 'Daily' } }))).toBe('Daily')
    expect(usbWatchName(w({ owner: 'yours', device: { id: 1, model: 'Fenix 8', name: 'fenix 8 - 47mm', nickname: '' } }))).toBe('Fenix 8')
    for (const owner of ['yours', 'nobody', 'unknown', 'other'] as const) expect(ownerWords(w({ owner }))).not.toBe('')
    expect(howWords(w())).toBe('Read over USB')
    expect(howWords(w({ mounted: true }))).toContain('mount')
    expect(howWords(w({ how: 'drive' }))).toBe('Read as a USB drive')
  })
})
