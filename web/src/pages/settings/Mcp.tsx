// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

import { Check, Copy, Pencil, Plus, Search, Trash2 } from 'lucide-react'
import { useLayoutEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { errorText } from '../../api/client'
import { useCreateMcpToken, useMcp, useRenameMcpToken, useRevokeMcpToken } from '../../api/hooks/mcp'
import type { McpItem, McpScope, McpToken } from '../../api/types'
import { ago } from '../../lib/format'
import { Button, Card, Choice, Field, Notice, Spinner } from '../../ui/kit'
import { Confirm, Sheet } from '../../ui/Sheet'
import { useToast } from '../../ui/Toast'

const levelText: Record<McpScope, string> = { read: 'Read', write: 'Read and write', admin: 'Admin' }
const levelHint: Record<McpScope, string> = {
  read: 'Everything readable: health, sleep, training, the calendar, devices and files.',
  write: 'Also plans, Sync now, watch and profile settings, imports and adding a watch; removals need the watch’s name typed.',
  admin: 'Also people, invites, every watch on the server and the sync nodes.',
}
const expiries: [string, string][] = [['30', '30 days'], ['90', '90 days'], ['365', 'A year'], ['0', 'Never']]

const date = (unix: number) => new Date(unix * 1000).toLocaleDateString(undefined, { dateStyle: 'medium' })
const when = (unix: number) => new Date(unix * 1000).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })

/** copyText copies to the clipboard; on plain HTTP (no clipboard API) it falls back to the
 * selection, which the browser still copies on a click. */
async function copyText(text: string, field?: HTMLInputElement | HTMLTextAreaElement | null): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // fall through to the selection
  }
  if (field) {
    field.focus()
    field.select()
    try {
      return document.execCommand('copy')
    } catch {
      return false
    }
  }
  return false
}

/** CopyField shows a value read-only with a Copy button. */
function CopyField({ label, value, multiline, wrap }: { label: string; value: string; multiline?: boolean; wrap?: boolean }) {
  const ref = useRef<HTMLInputElement & HTMLTextAreaElement>(null)
  const [state, setState] = useState<'' | 'copied' | 'select'>('')
  // A wrapping field grows to its text, however narrow the screen.
  useLayoutEffect(() => {
    const el = ref.current
    if (!wrap || !el) return
    el.style.height = 'auto'
    if (el.scrollHeight > 0) el.style.height = `${el.scrollHeight + 2}px`
  }, [wrap, value])
  const copy = async () => setState((await copyText(value, ref.current)) ? 'copied' : 'select')
  const cls = 'num w-full rounded-xl border border-border bg-bg2 px-3 py-2 text-[13px] text-text'
  return (
    <div className="grid gap-1.5">
      <div className="flex items-center justify-between gap-2">
        <span className="field-label">{label}</span>
        <button type="button" onClick={copy} className="inline-flex items-center gap-1 text-[13px] text-muted hover:text-lime" aria-label={`Copy ${label}`}>
          {state === 'copied' ? <Check size={14} /> : <Copy size={14} />} {state === 'copied' ? 'Copied' : 'Copy'}
        </button>
      </div>
      {multiline || wrap ? (
        // JSON keeps its lines (scrolling sideways on a phone); a long command wraps.
        <textarea ref={ref} readOnly rows={wrap ? 2 : value.split('\n').length + 1} wrap={wrap ? 'soft' : 'off'} value={value} aria-label={label}
          className={`${cls} resize-none ${wrap ? 'break-all' : 'overflow-x-auto'}`} onFocus={(e) => e.target.select()} />
      ) : (
        <input ref={ref} readOnly value={value} aria-label={label} className={cls} onFocus={(e) => e.target.select()} />
      )}
      {state === 'select' && <span className="text-[13px] text-faint">Selected: press Ctrl+C (⌘C) to copy.</span>}
    </div>
  )
}

/** snippets are ready configurations for a new token. */
export function snippets(endpoint: string, token: string) {
  const server = endpoint.replace(/\/mcp$/, '')
  return {
    claude: `claude mcp add --transport http goarmin ${endpoint} --header "Authorization: Bearer ${token}"`,
    http: JSON.stringify({ mcpServers: { goarmin: { type: 'http', url: endpoint, headers: { Authorization: `Bearer ${token}` } } } }, null, 2),
    stdio: JSON.stringify({ mcpServers: { goarmin: { command: 'goarmin', args: ['mcp', '-server', server], env: { GOARMIN_MCP_TOKEN: token } } } }, null, 2),
  }
}

function NewTokenSheet({ endpoint, token, item, onClose }: { endpoint: string; token: string; item: McpToken; onClose: () => void }) {
  const s = snippets(endpoint, token)
  return (
    <Sheet title="Your new token" onClose={onClose} footer={<Button variant="primary" onClick={onClose}>Done</Button>}>
      <div className="grid gap-5">
        <Notice tone="ok">“{item.name}” ({levelText[item.scope]}) is ready. Copy it now: goarmin shows it only this once and keeps only a fingerprint of it.</Notice>
        <CopyField label="Token" value={token} />
        <CopyField label="Claude Code" value={s.claude} wrap />
        <CopyField label="An MCP client over HTTP" value={s.http} multiline />
        <CopyField label="The stdio bridge (goarmin mcp)" value={s.stdio} multiline />
        <p className="m-0 text-[13px] text-faint">Treat it like a password: anyone holding it can do what its access allows, as you.</p>
      </div>
    </Sheet>
  )
}

function NewToken({ scopes, endpoint }: { scopes: McpScope[]; endpoint: string }) {
  const create = useCreateMcpToken()
  const [name, setName] = useState('')
  const [scope, setScope] = useState<McpScope>('read')
  const [expiry, setExpiry] = useState('90')
  const [shown, setShown] = useState<{ token: string; item: McpToken } | null>(null)
  const submit = (e: FormEvent) => {
    e.preventDefault()
    create.mutate({ name: name.trim(), scope, expires_days: Number(expiry) }, {
      onSuccess: (r) => { setShown(r); setName('') },
    })
  }
  return (
    <section aria-label="New token"><Card className="p-6 sm:p-8">
      <p className="caps mb-1">New token</p>
      <p className="mt-0 mb-5 text-sm text-muted">One token per assistant or computer, so you can revoke each on its own.</p>
      <form className="grid gap-5" onSubmit={submit}>
        <Field label="Name" placeholder="e.g. Claude on my laptop" value={name} maxLength={60} required onChange={(e) => setName(e.target.value)} />
        <Choice legend="Access" value={scope} options={scopes.map((s) => [s, levelText[s]])} onChange={setScope} hint={levelHint[scope]} />
        <Choice legend="Expires" value={expiry} options={expiries} onChange={setExpiry} />
        {create.error && <Notice tone="error">{errorText(create.error)}</Notice>}
        <div><Button variant="primary" type="submit" disabled={create.isPending || !name.trim()}><Plus size={18} /> Make token</Button></div>
      </form>
      {shown && <NewTokenSheet endpoint={endpoint} token={shown.token} item={shown.item} onClose={() => setShown(null)} />}
    </Card></section>
  )
}

function TokenRow({ t }: { t: McpToken }) {
  const rename = useRenameMcpToken()
  const revoke = useRevokeMcpToken()
  const toast = useToast()
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(t.name)
  const [asking, setAsking] = useState(false)
  const expires = t.expires_at ? (t.live ? `expires ${date(t.expires_at)}` : `expired ${date(t.expires_at)}`) : 'never expires'
  return (
    <li className={`rounded-xl border border-border bg-bg2 px-4 py-3 ${t.live ? '' : 'opacity-60'}`}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0 flex-1">
          {editing ? (
            <form className="flex flex-wrap items-end gap-2" onSubmit={(e) => {
              e.preventDefault()
              rename.mutate({ id: t.id, name: name.trim() }, { onSuccess: () => setEditing(false) })
            }}>
              <div className="min-w-[180px] flex-1"><Field label="Name" value={name} maxLength={60} required autoFocus onChange={(e) => setName(e.target.value)} /></div>
              <Button small type="submit" disabled={rename.isPending || !name.trim()}>Save</Button>
              <Button small variant="quiet" type="button" onClick={() => { setEditing(false); setName(t.name) }}>Cancel</Button>
            </form>
          ) : (
            <p className="m-0 flex flex-wrap items-center gap-2">
              <span className="truncate font-medium">{t.name}</span>
              <span className="badge-cut">{levelText[t.scope]}</span>
              <span className="num text-[13px] text-faint">{t.prefix}…</span>
            </p>
          )}
          <p className="m-0 mt-1 text-[13px] text-faint">
            made {date(t.created_at)} · {t.last_used_at ? `used ${ago(t.last_used_at)}` : 'not used yet'} ·{' '}
            {t.revoked_at ? `revoked ${date(t.revoked_at)}` : expires}
            {t.calls_24h > 0 && <> · <span className="num">{t.calls_24h}</span> calls in 24 h</>}
          </p>
        </div>
        {t.live && !editing && (
          <div className="flex items-center gap-1">
            <button type="button" aria-label={`Rename ${t.name}`} onClick={() => setEditing(true)} className="grid h-9 w-9 place-items-center text-muted hover:text-text">
              <Pencil size={17} />
            </button>
            <button type="button" aria-label={`Revoke ${t.name}`} onClick={() => setAsking(true)} className="grid h-9 w-9 place-items-center text-muted hover:text-err">
              <Trash2 size={18} />
            </button>
          </div>
        )}
      </div>
      {(rename.error ?? revoke.error) && <div className="mt-3"><Notice tone="error">{errorText(rename.error ?? revoke.error)}</Notice></div>}
      {asking && (
        <Confirm title="Revoke this token?" confirm="Revoke" danger onClose={() => setAsking(false)}
          onConfirm={() => revoke.mutate(t.id, { onSuccess: () => toast({ text: `“${t.name}” is revoked` }) })}>
          “{t.name}” stops working at once. An assistant using it needs a new token.
        </Confirm>
      )}
    </li>
  )
}

function Tokens({ tokens }: { tokens: McpToken[] }) {
  return (
    <Card className="p-6 sm:p-8">
      <p className="caps mb-5">Your tokens</p>
      {tokens.length === 0 ? <p className="m-0 text-sm text-faint">No token yet. Make one, then add goarmin to your assistant with it.</p> : (
        <ul className="m-0 grid list-none gap-2 p-0" aria-label="Tokens">
          {tokens.map((t) => <TokenRow key={t.id} t={t} />)}
        </ul>
      )}
    </Card>
  )
}

function Calls({ data }: { data: NonNullable<ReturnType<typeof useMcp>['data']> }) {
  return (
    <Card className="p-6 sm:p-8">
      <p className="caps mb-1">Recent calls</p>
      <p className="mt-0 mb-5 text-sm text-muted">What assistants did with your tokens: the tool, never what they sent. Kept {data.retention_days} days.</p>
      {data.calls.length === 0 ? <p className="m-0 text-sm text-faint">No calls yet.</p> : (
        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-sm">
            <thead>
              <tr className="text-left text-[12px] uppercase tracking-[.12em] text-faint">
                <th className="py-2 pr-3 font-normal">When</th><th className="py-2 pr-3 font-normal">Tool</th>
                <th className="py-2 pr-3 font-normal">Token</th><th className="py-2 pr-3 font-normal">Result</th>
                <th className="py-2 text-right font-normal">Time</th>
              </tr>
            </thead>
            <tbody>
              {data.calls.map((c) => (
                <tr key={c.id} className="border-t border-border align-top">
                  <td className="py-2 pr-3 whitespace-nowrap text-muted">{when(c.at)}</td>
                  <td className="num py-2 pr-3">{c.tool}</td>
                  <td className="py-2 pr-3 whitespace-nowrap text-muted">{c.token}</td>
                  <td className={`py-2 pr-3 ${c.ok ? 'text-lime' : 'text-err'}`}>{c.ok ? 'OK' : c.error || 'failed'}</td>
                  <td className="num py-2 text-right whitespace-nowrap text-muted">{c.ms} ms</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  )
}

const areaOrder = ['Account', 'Health', 'Training', 'Calendar', 'Devices', 'Imports', 'Pairing', 'Files', 'Admin', 'Prompts']
const rank: Record<McpScope, number> = { read: 0, write: 1, admin: 2 }

/** Catalogue lists what MCP offers, by area, for the chosen access. */
function Catalogue({ items, scopes }: { items: McpItem[]; scopes: McpScope[] }) {
  const [level, setLevel] = useState<McpScope>(scopes.includes('write') ? 'write' : 'read')
  const [q, setQ] = useState('')
  const groups = useMemo(() => {
    const needle = q.trim().toLowerCase()
    const shown = items.filter((i) => rank[i.level] <= rank[level] &&
      (!needle || `${i.name} ${i.title} ${i.description}`.toLowerCase().includes(needle)))
    const by = new Map<string, McpItem[]>()
    for (const i of shown) by.set(i.area, [...(by.get(i.area) ?? []), i])
    return [...by.entries()].sort((a, b) => areaOrder.indexOf(a[0]) - areaOrder.indexOf(b[0]))
  }, [items, level, q])
  const count = groups.reduce((n, [, l]) => n + l.length, 0)
  return (
    <section aria-label="What MCP can do"><Card className="p-6 sm:p-8">
      <p className="caps mb-1">What MCP can do</p>
      <p className="mt-0 mb-5 text-sm text-muted">Every part of goarmin, as tools, files (resources) and ready-made prompts. A token sees only what its access allows.</p>
      <div className="mb-5 grid gap-4 sm:flex sm:items-end sm:justify-between">
        <Choice legend="With access" value={level} options={scopes.map((s) => [s, levelText[s]])} onChange={setLevel} />
        <label className="relative block sm:w-[260px]">
          <span className="sr-only">Search the catalogue</span>
          <Search size={16} className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-faint" />
          <input type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search tools"
            className="w-full rounded-full border border-border bg-bg2 py-2 pr-3 pl-9 text-sm" />
        </label>
      </div>
      <p className="mt-0 mb-4 text-[13px] text-faint"><span className="num">{count}</span> {count === 1 ? 'item' : 'items'}</p>
      <div className="grid gap-6">
        {groups.map(([area, list]) => (
          <section key={area} aria-label={area}>
            <h3 className="caps m-0 mb-2 text-faint">{area}</h3>
            <ul className="m-0 grid list-none gap-1 p-0">
              {list.map((i) => (
                <li key={`${i.kind}:${i.name}`} className="grid gap-0.5 rounded-lg px-2 py-1.5 hover:bg-bg2 sm:grid-cols-[minmax(0,240px)_1fr] sm:gap-4">
                  <span className="num truncate text-[13px] text-text">
                    {i.kind === 'resource' ? i.uri : i.name}
                    {i.kind === 'prompt' && <span className="ml-2 text-faint">prompt</span>}
                  </span>
                  <span className="text-[13px] text-muted">
                    {i.description}
                    {i.level !== 'read' && <span className="ml-2 text-faint">· {levelText[i.level]}</span>}
                    {i.destructive && <span className="ml-2 text-amber">· removes data</span>}
                  </span>
                </li>
              ))}
            </ul>
          </section>
        ))}
      </div>
    </Card></section>
  )
}

export default function Mcp() {
  const q = useMcp()
  if (q.isPending) return <Spinner />
  if (q.error || !q.data) return <Notice tone="error">{errorText(q.error)}</Notice>
  const d = q.data
  return (
    // minmax(0, 1fr) columns: the calls table scrolls inside its card instead of widening the page.
    <div className="grid grid-cols-1 gap-6">
      <Card className="p-6 sm:p-8">
        <p className="caps mb-1">AI assistants</p>
        <p className="mt-0 mb-5 max-w-[70ch] text-sm text-muted">
          goarmin speaks MCP, the Model Context Protocol: an assistant such as Claude can read your data and, with a token that allows
          it, act for you (plan workouts, sync, add a watch). It sees only your account, as you do here.
        </p>
        {!d.enabled ? (
          <Notice>MCP is off on this server (it runs with <span className="num">-no-mcp</span>). Ask whoever runs it to turn it on.</Notice>
        ) : (
          <div className="grid gap-3">
            <CopyField label="Endpoint" value={d.endpoint} />
            {!d.secure && (
              <Notice>This server is plain HTTP, so tokens travel unencrypted. That is fine on a private network such as a VPN mesh; elsewhere, put HTTPS in front.</Notice>
            )}
            <p className="m-0 text-[13px] text-faint">
              Each token makes at most {d.limits.calls_per_minute} calls a minute, {d.limits.destructive_per_minute} of them removals.
            </p>
          </div>
        )}
      </Card>
      {d.enabled && (
        <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <NewToken scopes={d.scopes} endpoint={d.endpoint} />
          <Tokens tokens={d.tokens} />
        </div>
      )}
      {!d.enabled && d.tokens.length > 0 && <Tokens tokens={d.tokens} />}
      <Calls data={d} />
      <Catalogue items={d.catalogue} scopes={d.scopes} />
    </div>
  )
}
