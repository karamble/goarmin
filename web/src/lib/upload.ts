// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { ApiError } from '../api/client'
import type { ImportRun } from '../api/types'
import type { Picked } from './imports'

/**
 * uploadFiles sends picked files to POST /api/imports, each part named by its path, and
 * reports the bytes sent. It uses XMLHttpRequest, which (unlike fetch) reports upload
 * progress. The answer is the queued import.
 */
export function uploadFiles(files: Picked[], onProgress: (sent: number, total: number) => void, signal?: AbortSignal): Promise<ImportRun> {
  return new Promise((resolve, reject) => {
    const fd = new FormData()
    for (const f of files) fd.append('file', f.file, f.path)
    const xhr = new XMLHttpRequest()
    xhr.open('POST', '/api/imports')
    xhr.setRequestHeader('Accept', 'application/json')
    xhr.withCredentials = true
    xhr.upload.onprogress = (e) => onProgress(e.loaded, e.lengthComputable ? e.total : 0)
    xhr.onload = () => {
      let body: { error?: string } & Partial<ImportRun> = {}
      try {
        body = JSON.parse(xhr.responseText)
      } catch {
        /* not JSON */
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve(body as ImportRun)
      else reject(new ApiError(xhr.status, body.error || `goarmin answered ${xhr.status}`))
    }
    xhr.onerror = () => reject(new ApiError(0, 'The upload stopped: goarmin is not reachable.'))
    xhr.onabort = () => reject(new DOMException('The upload was stopped.', 'AbortError'))
    signal?.addEventListener('abort', () => xhr.abort(), { once: true })
    xhr.send(fd)
  })
}

/** droppedFiles reads what was dropped, folders included (walked with their paths). */
export async function droppedFiles(dt: DataTransfer): Promise<Picked[]> {
  const entries = Array.from(dt.items ?? [])
    .map((it) => (it.kind === 'file' ? it.webkitGetAsEntry?.() : null))
    .filter((e): e is FileSystemEntry => !!e)
  if (entries.length === 0) return Array.from(dt.files).map((file) => ({ file, path: file.name }))
  const out: Picked[] = []
  for (const e of entries) await walkEntry(e, '', out)
  return out
}

async function walkEntry(e: FileSystemEntry, prefix: string, out: Picked[]): Promise<void> {
  const path = prefix ? `${prefix}/${e.name}` : e.name
  if (e.isFile) {
    const file = await new Promise<File>((res, rej) => (e as FileSystemFileEntry).file(res, rej))
    out.push({ file, path })
    return
  }
  if (!e.isDirectory) return
  const reader = (e as FileSystemDirectoryEntry).createReader()
  // readEntries hands out a folder in batches until it returns none.
  for (;;) {
    const batch = await new Promise<FileSystemEntry[]>((res, rej) => reader.readEntries(res, rej))
    if (batch.length === 0) break
    for (const c of batch) await walkEntry(c, path, out)
  }
}
