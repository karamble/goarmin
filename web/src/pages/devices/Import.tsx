// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { useQueryClient } from '@tanstack/react-query'
import { FileArchive, FolderUp, Upload, Usb, X } from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type DragEvent } from 'react'
import { errorText } from '../../api/client'
import { useCancelImport, useImports, useImportUSB, useSetUSBAuto, useUSB } from '../../api/hooks/imports'
import { keys } from '../../api/keys'
import type { ImportRun, USBWatch } from '../../api/types'
import { ago, bytes, timeOfDay } from '../../lib/format'
import {
  finished, howWords, keptFolders, ownerWords, pickedFrom, reportLine, selectionName, sortPicked, statusWords, usbWatchName, type Picked,
} from '../../lib/imports'
import { droppedFiles, uploadFiles } from '../../lib/upload'
import { Button, Notice, Skeleton, Spinner } from '../../ui/kit'
import { useToast } from '../../ui/Toast'
import { RunningImports } from './ImportProgress'

/** Import brings files in outside a sync: a watch plugged in by USB, or files from this
 * computer (FIT files, a folder, zip archives, the Garmin Connect export). */
export default function ImportPage() {
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5">
      <USBSection />
      <FilesSection />
      <History />
    </div>
  )
}

function Switch({ on, label, onChange, disabled }: { on: boolean; label: string; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <button type="button" role="switch" aria-checked={on} aria-label={label} disabled={disabled} onClick={() => onChange(!on)}
      className={`relative h-6 w-11 shrink-0 rounded-full transition-colors disabled:opacity-50 ${on ? 'bg-lime' : 'bg-track'}`}>
      <span className={`absolute top-0.5 h-5 w-5 rounded-full transition-[left] ${on ? 'left-[22px] bg-bg' : 'left-0.5 bg-text'}`} />
    </button>
  )
}

function USBSection() {
  const usb = useUSB()
  const start = useImportUSB()
  const auto = useSetUSBAuto()
  const toast = useToast()
  const list = usb.data ?? []
  const run = (w: USBWatch) => start.mutate(w.id, {
    onSuccess: () => toast({ text: `Importing ${usbWatchName(w)}` }),
    onError: (e) => toast({ tone: 'error', text: errorText(e) }),
  })
  return (
    <section id="usb" className="card p-5 sm:p-6" aria-labelledby="usb-title">
      <h2 id="usb-title" className="caps m-0">From your watch by USB</h2>
      <p className="m-0 mt-1 max-w-3xl text-[13px] text-muted">
        goarmin reads the watch’s files and changes nothing on it: Bluetooth syncs go on as before. Your watch is imported by itself
        whenever you plug it in, for example to charge it.
      </p>
      {usb.isPending ? (
        <Skeleton className="mt-4 h-20" />
      ) : usb.error ? (
        <div className="mt-4"><Notice tone="error">{errorText(usb.error)}</Notice></div>
      ) : list.length === 0 ? (
        <div className="mt-4 flex items-start gap-3 rounded-xl border border-dashed border-border p-4 text-[14px] text-muted">
          <Usb size={18} className="mt-0.5 shrink-0 text-faint" aria-hidden />
          <p className="m-0">
            No watch on USB. Plug the watch into this computer with its charging cable; it shows up here within a few seconds.
          </p>
        </div>
      ) : (
        <ul className="m-0 mt-4 grid list-none gap-3 p-0">
          {list.map((w) => (
            <li key={w.id} className="grid gap-3 rounded-xl border border-border p-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
              <div className="flex min-w-0 items-start gap-3">
                <span className="grid h-10 w-10 shrink-0 place-items-center rounded-full border border-border text-lime" aria-hidden><Usb size={18} /></span>
                <div className="min-w-0">
                  <p className="m-0 truncate text-[15px] text-text">{usbWatchName(w)}</p>
                  <p className="m-0 mt-0.5 text-[12px] text-muted">{howWords(w)}{w.port ? ` · port ${w.port}` : ''}</p>
                  <p className={`m-0 mt-1 text-[13px] ${w.owner === 'other' ? 'text-warn' : 'text-muted'}`}>{ownerWords(w)}</p>
                  {w.importing && !finished(w.importing) ? (
                    <p className="m-0 mt-2 flex items-center gap-2 text-[13px] text-text" aria-live="polite">
                      <Spinner label="Importing" />
                      {w.importing.status === 'queued' ? 'Waiting for the import before it…' : `Importing: ${reportLine(w.importing.report, true)}`}
                    </p>
                  ) : w.last ? (
                    <p className="m-0 mt-2 text-[13px] text-muted">
                      Last import {ago(w.last.finished_at ?? w.last.created_at)}
                      {w.last.status === 'done' ? `: ${reportLine(w.last.report)}` : ` ${statusWords[w.last.status].toLowerCase()}${w.last.error ? `: ${w.last.error}` : ''}`}
                    </p>
                  ) : null}
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-4 sm:justify-end">
                {w.owner === 'yours' && w.device && (
                  <label className="flex items-center gap-2 text-[13px] text-muted">
                    <Switch on={!!w.auto} label="Import when plugged in" disabled={auto.isPending}
                      onChange={(on) => auto.mutate({ device: w.device!.id, on }, { onError: (e) => toast({ tone: 'error', text: errorText(e) }) })} />
                    Import when plugged in
                  </label>
                )}
                <Button variant="primary" disabled={w.owner === 'other' || (!!w.importing && !finished(w.importing)) || start.isPending}
                  onClick={() => run(w)}>
                  Import now
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}
      <div className="mt-4 empty:hidden"><RunningImports kind="usb" /></div>
    </section>
  )
}

function FilesSection() {
  const qc = useQueryClient()
  const toast = useToast()
  const [picked, setPicked] = useState<Picked[]>([])
  const [drag, setDrag] = useState(false)
  const [progress, setProgress] = useState<{ sent: number; total: number } | null>(null)
  const [error, setError] = useState('')
  const abort = useRef<AbortController | null>(null)
  const files = useRef<HTMLInputElement>(null)
  const folder = useRef<HTMLInputElement>(null)
  const sel = useMemo(() => sortPicked(picked), [picked])
  useEffect(() => {
    // Not a React attribute: the folder picker.
    folder.current?.setAttribute('webkitdirectory', '')
  }, [])
  const add = (more: Picked[]) => {
    setError('')
    setPicked((p) => {
      const seen = new Set(p.map((f) => f.path))
      return [...p, ...more.filter((f) => !seen.has(f.path))]
    })
  }
  const onDrop = (e: DragEvent) => {
    e.preventDefault()
    setDrag(false)
    void droppedFiles(e.dataTransfer).then(add, (err) => setError(errorText(err)))
  }
  const start = async () => {
    if (sel.send.length === 0) return
    setError('')
    const ac = new AbortController()
    abort.current = ac
    setProgress({ sent: 0, total: sel.size })
    try {
      await uploadFiles(sel.send, (sent, total) => setProgress({ sent, total: total || sel.size }), ac.signal)
      toast({ text: `Importing ${selectionName(sel.send)}` })
      setPicked([])
      void qc.invalidateQueries({ queryKey: keys.imports })
    } catch (e) {
      if ((e as Error).name !== 'AbortError') setError(errorText(e))
    } finally {
      setProgress(null)
      abort.current = null
    }
  }
  const pct = progress && progress.total ? Math.min(100, Math.round((progress.sent / progress.total) * 100)) : 0
  return (
    <section className="card p-5 sm:p-6" aria-labelledby="files-title">
      <h2 id="files-title" className="caps m-0">From files</h2>
      <p className="m-0 mt-1 max-w-3xl text-[13px] text-muted">
        FIT files, a copy of the watch’s GARMIN folder, zip archives of either, or your Garmin Connect data export as it arrives (a zip).
        Files goarmin has already are recognised and left as they are.
      </p>
      <div onDragOver={(e) => { e.preventDefault(); setDrag(true) }} onDragLeave={() => setDrag(false)} onDrop={onDrop}
        className={`mt-4 grid place-items-center gap-3 rounded-xl border border-dashed p-6 text-center transition-colors ${drag ? 'border-lime bg-lime-fill' : 'border-border'}`}>
        <Upload size={22} className={drag ? 'text-lime' : 'text-faint'} aria-hidden />
        <p className="m-0 text-[14px] text-muted">Drop files or a folder here, or</p>
        <div className="flex flex-wrap justify-center gap-2">
          <Button small onClick={() => files.current?.click()} disabled={!!progress}><FileArchive size={14} /> Choose files</Button>
          <Button small onClick={() => folder.current?.click()} disabled={!!progress}><FolderUp size={14} /> Choose a folder</Button>
        </div>
        <input ref={files} type="file" multiple accept=".fit,.FIT,.zip" className="hidden" aria-label="Choose files"
          onChange={(e) => { if (e.target.files) add(pickedFrom(e.target.files)); e.target.value = '' }} />
        <input ref={folder} type="file" multiple className="hidden" aria-label="Choose a folder"
          onChange={(e) => { if (e.target.files) add(pickedFrom(e.target.files)); e.target.value = '' }} />
      </div>
      {picked.length > 0 && (
        <div className="mt-4 flex flex-wrap items-center gap-3">
          <p className="m-0 flex-1 text-[14px] text-text">
            {sel.send.length === 0 ? 'Nothing to import in this selection' : `${selectionName(sel.send)}, ${bytes(sel.size)}`}
            {sel.left > 0 && <span className="text-muted"> · {sel.left} other files stay on this computer</span>}
          </p>
          {progress ? (
            <Button small onClick={() => abort.current?.abort()}><X size={14} /> Stop the upload</Button>
          ) : (
            <>
              <Button small onClick={() => setPicked([])}>Clear</Button>
              <Button variant="primary" onClick={() => void start()} disabled={sel.send.length === 0}>Import</Button>
            </>
          )}
        </div>
      )}
      {progress && (
        <div className="mt-3" aria-live="polite">
          <div className="h-1.5 overflow-hidden rounded-full bg-track" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct} aria-label="Upload">
            <div className="h-full bg-lime transition-[width]" style={{ width: `${pct}%` }} />
          </div>
          <p className="m-0 mt-1.5 text-[12px] text-muted">
            Uploading {bytes(progress.sent)} of {bytes(progress.total)}. Keep this page open until it is done; the import then runs on by itself.
          </p>
        </div>
      )}
      {error && <div className="mt-3"><Notice tone="error">{error}</Notice></div>}
      <div className="mt-4 empty:hidden"><RunningImports kind="upload" /></div>
      <details className="mt-4 text-[13px] text-muted">
        <summary className="cursor-pointer text-text">The Garmin Connect export</summary>
        <p className="m-0 mt-2 max-w-3xl">
          Garmin Connect sends your whole account as a zip when you ask for it on Garmin’s website (Account › Data Management › Export
          your data); the link arrives by email, often after a few days. Drop that zip here as it is. Its activities and other FIT
          files come in like any import, and devices you had before (watches, a scale) become devices of yours with their
          history. goarmin also reads its other files: daily summaries, sleep, training metrics, weight, health status,
          hydration, ECG, gear, personal records, and your account records (Settings › Account data). Every file is kept
          unchanged as well.
        </p>
      </details>
    </section>
  )
}

const kindWords = (i: ImportRun) => i.kind === 'usb' ? 'From the watch by USB' : i.source === 'gc_export' ? 'Garmin Connect export' : 'Uploaded files'

function History() {
  const q = useImports(20)
  const cancel = useCancelImport()
  const toast = useToast()
  return (
    <section className="card p-5 sm:p-6" aria-labelledby="history-title">
      <h2 id="history-title" className="caps m-0">Imports</h2>
      {q.isPending ? (
        <Skeleton className="mt-4 h-24" />
      ) : q.items.length === 0 ? (
        <p className="m-0 mt-2 text-[14px] text-muted">Nothing imported yet.</p>
      ) : (
        <ul className="m-0 mt-3 grid list-none gap-2 p-0">
          {q.items.map((i) => (
            <ImportRow key={i.id} i={i} onCancel={() => cancel.mutate(i.id, { onError: (e) => toast({ tone: 'error', text: errorText(e) }) })} />
          ))}
        </ul>
      )}
      {q.hasNextPage && (
        <div className="mt-3"><Button small onClick={() => void q.fetchNextPage()} disabled={q.isFetchingNextPage}>Load more</Button></div>
      )}
    </section>
  )
}

function ImportRow({ i, onCancel }: { i: ImportRun; onCancel: () => void }) {
  const r = i.report ?? {}
  const done = finished(i)
  const tone = i.status === 'done' ? '' : i.status === 'failed' ? '!bg-red !text-text' : done ? '!bg-track !text-muted' : '!bg-cyan'
  const folders = keptFolders(r)
  const hasDetails = (r.errors?.length ?? 0) > 0 || folders.length > 0 || !!i.error || (r.other ?? 0) > 0 || (r.archives ?? 0) > 0
  const Icon = i.kind === 'usb' ? Usb : i.source === 'gc_export' ? FileArchive : Upload
  const head = (
    <div className="grid grid-cols-[1.75rem_minmax(0,1fr)_auto] items-start gap-3">
      <Icon size={16} className="mt-0.5 text-muted" aria-hidden />
      <div className="min-w-0">
        <p className="m-0 truncate text-[14px] text-text">{i.name || kindWords(i)}</p>
        <p className="m-0 mt-0.5 text-[12px] text-muted">
          {kindWords(i)} · {ago(i.created_at)}, {timeOfDay(i.created_at)}
          {i.started_by === 'plugged' ? ' · when it was plugged in' : ''}
        </p>
        <p className="m-0 mt-1 text-[13px] text-text">
          {done ? (i.status === 'done' ? reportLine(r) : i.error || statusWords[i.status]) : i.status === 'uploading' ? 'The files are arriving…' : i.status === 'queued' ? 'Waiting for the import before it…' : reportLine(r, true)}
        </p>
      </div>
      <div className="flex flex-col items-end gap-2">
        <span className={`badge-cut ${tone}`}>{statusWords[i.status]}</span>
        {(i.status === 'queued' || i.status === 'running') && <Button small onClick={onCancel}>Stop</Button>}
      </div>
    </div>
  )
  if (!hasDetails) return <li className="rounded-xl border border-border p-3.5">{head}</li>
  return (
    <li className="rounded-xl border border-border p-3.5">
      {head}
      <details className="mt-2 pl-[2.5rem] text-[13px] text-muted">
        <summary className="cursor-pointer">Details</summary>
        <ul className="m-0 mt-2 grid list-none gap-1 p-0">
          {(r.archives ?? 0) > 0 && <li>{r.archives} zip archives opened{r.bytes ? `, ${bytes(r.bytes)} read` : ''}</li>}
          {(r.other ?? 0) > 0 && <li>{r.other} files were neither FIT nor part of an export and were left out</li>}
          {folders.length > 0 && (
            <li>
              Kept for later from the export:
              <ul className="m-0 mt-1 grid list-none gap-0.5 pl-3">
                {folders.map(([f, n]) => <li key={f}><span className="font-mono text-[12px]">{f}</span> · {n}</li>)}
              </ul>
            </li>
          )}
          {(r.errors ?? []).map((e, k) => <li key={k} className="break-all font-mono text-[12px] text-err">{e}</li>)}
          {(r.errors_more ?? 0) > 0 && <li>… and {r.errors_more} more problems</li>}
        </ul>
      </details>
    </li>
  )
}
