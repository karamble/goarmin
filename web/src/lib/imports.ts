// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import type { ImportReport, ImportRun, USBWatch } from '../api/types'

/** A file to upload with its path in what was picked ("GARMIN/Activity/x.fit"). */
export interface Picked {
  file: File
  path: string
}

/** isExportPath: the path lies inside a Garmin Connect export (DI_CONNECT/DI-Connect-…). */
export function isExportPath(path: string): boolean {
  return path.split('/').some((p) => p === 'DI_CONNECT' || p.startsWith('DI-Connect-'))
}

/** wanted says whether a picked file is worth sending: FIT files, zip archives, and every file
 * of a Garmin Connect export (its JSON is kept for later). The rest stays on this computer. */
export function wanted(path: string): boolean {
  const lower = path.toLowerCase()
  return lower.endsWith('.fit') || lower.endsWith('.zip') || isExportPath(path)
}

/** sortPicked splits a selection into what goes up and what is left out (counted). */
export function sortPicked(files: Picked[]): { send: Picked[]; left: number; size: number } {
  const send = files.filter((f) => wanted(f.path))
  return { send, left: files.length - send.length, size: send.reduce((n, f) => n + f.file.size, 0) }
}

/** pickedFrom turns an <input type=file> selection into paths (a folder pick keeps them). */
export function pickedFrom(list: FileList | File[]): Picked[] {
  return Array.from(list).map((file) => ({ file, path: file.webkitRelativePath || file.name }))
}

/** selectionName says in words what was picked: one file, or the folder, or a count. */
export function selectionName(files: Picked[]): string {
  if (files.length === 1) return files[0].path.split('/').pop() ?? files[0].path
  const tops = new Set(files.map((f) => f.path.split('/')[0]))
  if (tops.size === 1 && files[0].path.includes('/')) return `${[...tops][0]} (${files.length} files)`
  return `${files.length} files`
}

/** finished: the import has ended. */
export function finished(i: Pick<ImportRun, 'status'>): boolean {
  return i.status === 'done' || i.status === 'failed' || i.status === 'canceled'
}

export const statusWords: Record<ImportRun['status'], string> = {
  uploading: 'Uploading', queued: 'Waiting', running: 'Importing', done: 'Done', failed: 'Failed', canceled: 'Canceled',
}

/** reportLine sums an import up: "5 new, 171 already here, 2 failed". */
export function reportLine(r: ImportReport, running = false): string {
  const parts: string[] = []
  const n = (v: number | undefined) => v ?? 0
  if (running) parts.push(`${n(r.seen) + n(r.kept)} files read`)
  if (n(r.new) || !running) parts.push(`${n(r.new)} new`)
  const here = n(r.known) + n(r.duplicates) + n(r.unchanged)
  if (here) parts.push(`${here} already here`)
  if (n(r.kept)) parts.push(`${n(r.kept)} kept for later`)
  if (n(r.waiting)) parts.push(`${n(r.waiting)} waiting for an owner`)
  if (n(r.failed)) parts.push(`${n(r.failed)} failed`)
  if (!running && !n(r.seen) && !n(r.kept)) return n(r.other) ? `no FIT files, ${n(r.other)} other files left out` : 'nothing to import'
  return parts.join(', ')
}

/** keptFolders lists an export's folders with what was kept from each, largest first. */
export function keptFolders(r: ImportReport): [string, number][] {
  return Object.entries(r.kept_by ?? {}).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
}

/** howWords says how the server reads a watch on USB. */
export function howWords(w: Pick<USBWatch, 'how' | 'mounted'>): string {
  switch (w.how) {
    case 'direct':
      return w.mounted ? 'Read over USB (or through the desktop’s mount while a file manager has it)' : 'Read over USB'
    case 'gvfs':
      return 'Read through the desktop’s mount'
    case 'drive':
      return 'Read as a USB drive'
  }
}

/** usbWatchName is the name to show: yours by its nickname, others by what USB says. */
export function usbWatchName(w: USBWatch): string {
  if (w.device) return w.device.nickname || w.device.model || w.device.name || w.name
  return w.name
}

/** ownerWords explains whose watch it is, and so what importing it does. */
export function ownerWords(w: USBWatch): string {
  switch (w.owner) {
    case 'yours':
      return 'Your watch.'
    case 'nobody':
      return 'Nobody owns this watch now; importing it adds its files to your account.'
    case 'unknown':
      return 'goarmin has not seen this watch’s files yet; importing it adds the watch to your account.'
    case 'other':
      return 'This watch belongs to another account.'
  }
}
