# Architecture

How goarmin is put together: its roles, how a sync and an import travel through it, how it
stores data, and how the API and the dashboard sit on top. For running it see
[running.md](running.md); for the data it reads see [data.md](data.md).

## One binary, several roles

`goarmin` is one Go program (no cgo). Its subcommands are the roles:

| Role | Command | What it does |
|---|---|---|
| Server | `goarmin serve` | The API, the SQLite database, the raw file store, imports, and the dashboard (embedded) |
| Sync role | `goarmin serve -with-sync` | The server plus the Bluetooth side: syncs this computer's watches on a schedule, streams live values, pairs new watches |
| Tools | `pair`, `sync`, `probe`, `live`, `import`, `usb`, `reingest`, `backup`, `user`, `days`, `activities`, `healthcheck` | One-off work on the same data directory |

The sync role needs the computer the watch is paired with: Bluetooth bonds live in BlueZ on
that machine. The server alone can run anywhere, for example in a container
([docker/README.md](../docker/README.md)), while a **sync node** (`goarmin sync -server`) syncs
the watch on the computer near it and hands the files to the server ([below](#sync-nodes)).

```mermaid
flowchart LR
  watch(["Garmin watch"])
  usb(["Watch on USB"])
  browser(["Browser or phone"])
  tiles(["Map tile server<br/>(optional)"])

  subgraph host["Linux computer with BlueZ"]
    direction TB
    hub["Sync role<br/>(nodehub, syncnode)"]
    api["API and dashboard<br/>(internal/api, webui)"]
    ingest["Ingest<br/>(internal/ingest)"]
    store[("SQLite<br/>goarmin.db")]
    raw[("Raw files<br/>raw/ab/cd/sha256")]
  end

  watch <-->|"Bluetooth LE"| hub
  usb -->|"MTP, read-only"| ingest
  hub -->|"pulled files"| ingest
  browser <-->|"HTTPS: /api, SSE, /tiles"| api
  api -->|"uploads, export zips"| ingest
  ingest --> raw
  ingest --> store
  api --> store
  api -->|"tiles, cached"| tiles
```

## Code layout

- `cmd/goarmin`: the subcommands.
- `pkg/`: reusable packages with no goarmin knowledge.
  - `bluez`: BlueZ over D-Bus (discovery, pairing agent, GATT).
  - `garmin/...`: the watch protocol, bottom up:
    - `multilink`, `gfdi`, `smart` (protobuf);
    - `watch` (connect and handshake), `fileaccess` (list, pull, archive), `realtime`;
    - `wellness` and `gcexport` (readers);
    - `product`;
    - `fakewatch`, a watch in a test.
  - FIT: `fitid`, `fitactivity`, `fitwatch`, `fitnamed`, `fitprofile`.
  - Storage and files: `sqlitex`, `blobstore`, `zipwalk`, `mtp`.
  - Small helpers: `btsnoop`, `cobs`, `clock`, `httpx`, `auth`, `geo`.
- `internal/`: the application.
  - `store` holds the database and its queries.
  - `ingest` turns files into rows.
  - `importer` runs imports and the USB watcher.
  - `nodehub` is the sync role: schedule, Bluetooth lane, live links, pairing sessions.
  - `syncnode` is one sync, plus the spool.
  - `api` serves HTTP; `webui` is the embedded dashboard.
  - `figures` holds the exercise figures' catalogue and poses, built into the binary.
  - `tiles` mounts the tile cache; `backup` writes archives; `usbsource` finds watches on USB;
    `config` reads settings.
- `web/` is the dashboard source (React); `web/src/figures` draws the exercise figures.
- `tools/` holds developer tools; `tools/figures` is the Blender pipeline for the figures.

## The sync

A sync is one Bluetooth connection: connect, handshake, list what is new, pull it, store it,
then tell the watch it may let go of it. The protocol is documented layer by layer in
[docs/protocol](protocol/README.md); what goarmin does on the real Fenix 8 is in
[06-linux-live.md](protocol/06-linux-live.md).

```mermaid
flowchart TB
  gatt["BLE GATT: one RX/TX characteristic pair"] --> ml["Multi-Link: services on handles"]
  ml --> mlr["MLR: reliable stream (sequence, ack, retransmit)"]
  mlr --> cobs["COBS framing"]
  cobs --> gfdi["GFDI messages: handshake, ACKs, responders"]
  gfdi --> smart["Protobuf Smart services"]
  smart --> fa["FileAccess: list, pull, flag as synced"]
  ml --> pipe["File pipe: zlib file bytes"]
  fa -. "opens" .-> pipe
```

```mermaid
sequenceDiagram
  autonumber
  participant Hub as nodehub (schedule, lane)
  participant Node as syncnode
  participant Watch as Watch
  participant Spool as Spool (watches/unit)
  participant Raw as Raw store and ingest
  Hub->>Hub: take the Bluetooth lane (ble.lock)
  Hub->>Node: Sync(watch, owner's time zone, after-fetch mode)
  Node->>Watch: connect, Multi-Link, GFDI handshake
  Watch-->>Node: device info, configuration
  Node->>Watch: cut wellness files, battery request
  Node->>Watch: list items (from the stored cursor)
  Watch-->>Node: new files
  loop each file, health first
    Node->>Watch: pull over a file pipe
    Watch-->>Node: zlib bytes
    Node->>Spool: verify size and FIT CRC, fsync
    Node->>Raw: store by sha256, ingest
    Node->>Watch: flag as synced (only after the file is stored)
  end
  Node->>Watch: sync complete, disconnect
  Node-->>Hub: result (counts, battery, errors)
  Hub->>Raw: record the sync run, rebuild the touched days
```

- **Only after a durable store.** A file is flagged as synced only once it is in the raw
  store. A flag lets the watch delete health files, so a crash between pull and store loses
  nothing: the next sync pulls the file again.
- **After fetching** is a per-watch setting:
  - `keep` never flags, so another app can still sync the files;
  - `archive` flags like Garmin Connect;
  - `archive_delete` also deletes activities from the watch.
- **Schedule.** Every 30 minutes by default. A sync that finds the watch out of range backs off
  (1 to 30 minutes) and doesn't count as a failure. One Bluetooth operation runs at a time (the
  lane): a scheduled sync, a requested one, a live link, a pairing session, or a CLI command
  holding `ble.lock`.
- **Live values.** Opened on demand. Heart rate, stress, Body Battery, respiration, steps,
  calories and floors stream to every viewer over Server-Sent Events, with the last 10 minutes
  replayed. The link closes 30 seconds after the last viewer leaves, and after 30 minutes at
  most.
- **Pairing.** A session on the computer with Bluetooth (this server, or a sync node relayed
  by the server) that the browser follows over SSE. Ownership is decided on the server before
  anything is saved: a watch that belongs to another account is refused without naming it.

```mermaid
stateDiagram-v2
  [*] --> prepare
  prepare --> waiting: scan
  waiting --> finding: Bluetooth free
  finding --> pairing: select a watch
  pairing --> confirm: numeric comparison
  pairing --> enter_passkey
  pairing --> show_passkey
  pairing --> identifying: already paired
  confirm --> identifying: codes match
  enter_passkey --> identifying
  show_passkey --> identifying
  confirm --> finding: mismatch or timeout
  identifying --> setup: model, firmware, battery
  setup --> done: nickname, after fetching, first sync
  identifying --> failed: owned by another account
  finding --> canceled
  setup --> expired: 10 minutes in all
  done --> [*]
  failed --> [*]
  canceled --> [*]
  expired --> [*]
```

## Sync nodes

A sync node runs the same hub as `serve -with-sync` (schedule, Bluetooth lane, live links); only
where the hub keeps things differs. The hub talks to a **Backend**:

- `nodehub.Local` in the server: the database and ingest, in-process.
- `nodeclient.Remote` on a node: the server over HTTP (`internal/nodeproto`, routes under
  `/api/node/v1`, the node's token as Bearer). It caches the server's config for each watch, so
  syncs go on while the server is away.

```mermaid
sequenceDiagram
  autonumber
  participant Watch
  participant Node as Sync node (hub, Remote)
  participant Server as Server (node API, registry)
  participant UI as Dashboard
  Node->>Server: hello (watches, adapter) → each watch's config
  loop each pulled file
    Watch-->>Node: file over a pipe, into the spool
    Node->>Server: PUT /files/{sha256}
    Server-->>Node: 200 (stored and ingested)
    Node->>Watch: flag as synced (only now)
  end
  Node->>Server: POST /runs (the sync log), POST /progress
  UI->>Server: Sync now / Live
  Server-->>Node: command (long poll)
  Node-->>Server: result, live readings each second
```

- **Archive after the server's answer.** `syncnode.Options.Hand` gives each file to the server
  right after the spool has it; the file-access engine flags a file only when
  `ArchiveAllowed` says the server took it. A file the server didn't take stays pending in the
  spool and on the watch. The next sync hands it over first and then flags it, without pulling it
  again.
- **The registry** (`internal/nodes`) is the API's sync role. It knows which node syncs which
  watch, keeps what each reported (status, Bluetooth), queues commands (sync now, live, the file
  list, unpair, purge, power on) and relays live readings with the same 10-minute replay. The
  in-process hub of `serve -with-sync` is one more member. A node silent for 75 s is offline, and
  the dashboard says so.
- **Uploads** go through the same import as a sync's spool (`ingest.ImportWatchFile`): the
  account that owned the watch at recording time, dedupe by content. A node may upload only for
  watches its account owns now (any watch for an admin's node).
- **Sign-in.** `goarmin node login` with the account's password, or a code approved in the
  dashboard, gives the node a token (stored as SHA-256 in `sync_nodes`); signing a node out
  revokes it at once.
- **Add a watch through a node.** The pairing session runs on the node, which holds the
  Bluetooth; its three touches of the database (whose the listed watches are, whose the
  identified watch is, adding it to an account) go through a `PairBackend`: `nodehub.Local`
  in the server, `nodeclient.Remote` on a node (the node API's pairing routes, answered only
  within a session the node runs). The registry keeps a mirror of the session for the browser:
  each step becomes a command, the node pushes each change (versioned, so an older copy never
  replaces a newer one), and a node that goes silent ends the mirror as failed. The claim's
  answer carries the watch's sync config, so the first sync runs at once on the node.

```mermaid
sequenceDiagram
  autonumber
  participant UI as Dashboard
  participant Server as Server (registry, node API)
  participant Node as Sync node (hub, Remote)
  participant Watch
  UI->>Server: POST /api/pairing {node}
  Server-->>Node: pair_start (long poll)
  Node-->>Server: the session, then each change → POST /pairing
  UI->>Server: scan, select
  Server-->>Node: pair_scan, pair_select
  Node->>Server: POST /pairing/owners (whose the listed watches are)
  Node->>Watch: pair (the code shows in the browser)
  UI->>Server: confirm
  Server-->>Node: pair_confirm
  Node->>Server: POST /pairing/identified
  UI->>Server: setup
  Server-->>Node: pair_setup
  Node->>Server: POST /pairing/claim → device, sync config
  Node->>Watch: node.json, first sync
```

## Imports

Files also arrive without Bluetooth:
- **Uploads** through the dashboard: FIT files, folders, zips, the Garmin Connect export.
- **A watch on USB:** read over MTP, read-only.
- **`goarmin import`** on the command line.

All of them go through the same path as a sync's files.

```mermaid
flowchart LR
  src["Folder, zip, nested zips,<br/>Garmin Connect export,<br/>watch on USB (MTP)"] --> walk["zipwalk / folder walk<br/>(limits: entries, size, ratio, depth)"]
  walk --> workers["Decode on every core<br/>(FIT, export JSON)"]
  workers --> ordered["One writer, in walk order"]
  ordered --> dedupe{"sha256 or FIT identity<br/>already stored?"}
  dedupe -- "yes" --> known["count as known"]
  dedupe -- "no" --> rawstore["raw store + raw_files row"]
  rawstore --> rows["samples, nights, activities,<br/>daily values, readers' tables"]
  rows --> days["rebuild each touched day once<br/>(and every minute on long runs)"]
```

- **Raw files are the source of truth.** Every file is stored by content (`raw/ab/cd/<sha256>`)
  before anything is extracted, and `goarmin reingest` rebuilds every derived row from them.
- **Handlers have versions** (`fit` and `gcjson`). Bumping one re-reads only the files an older
  version read.
- **Duplicates collapse** by sha256 and by FIT identity (serial, type, time created, number).
  A watch's own file wins over the export's copy.
- **Ownership by recording time.** Each file belongs to whoever owned the watch when it was
  recorded, not when it was imported (see below).
- One import runs at a time, in the background, with live progress in the dashboard.

## Storage

Everything lives in one data directory (default `~/.local/share/goarmin`):

| Path | What |
|---|---|
| `goarmin.db` | SQLite in WAL mode: one writer connection, a read-only pool, append-only migrations |
| `raw/` | Raw files by sha256: FIT files and the export's other files |
| `watches/<unit>/` | The sync spool: files on their way into the raw store, and what was pulled (`state.json`) |
| `node.json` | The watches this computer syncs, with how they connect (sync role only) |
| `tiles/` | The map tile cache |
| `imports/` | Uploads while they are imported |
| `ble.lock`, `node.lock`, `usb.lock` | Locks shared by the server and the CLI |

The core of the schema:

```mermaid
erDiagram
  users ||--o{ device_assignments : "owns, over periods"
  devices ||--o{ device_assignments : "is owned"
  users ||--o{ raw_files : "holds"
  devices |o--o{ raw_files : "recorded"
  raw_files ||--o{ ingest_state : "read by handler, version"
  raw_files ||--o{ samples : "wrote"
  raw_files ||--o{ sleep_parts : "wrote"
  sleep_parts }o--|| sleep_sessions : "merge into a night"
  raw_files ||--o{ activities : "wrote"
  activities ||--o{ laps : "has"
  activities ||--|| activity_streams : "track"
  users ||--o{ daily_values : "per day and kind"
  users ||--o{ daily_summary : "per day (rebuilt)"
  devices ||--o{ sync_runs : "synced"
  users ||--o{ sessions : "signed in"
  users ||--o{ webauthn_credentials : "passkeys"
```

- **`samples`** holds every time series (heart rate, stress, Body Battery, steps, …), keyed by
  user, kind and second. Running totals are stored as deltas.
- **Derived tables can be rebuilt:**
  - `daily_summary`, rebuilt per touched day;
  - `sleep_sessions`, merged from `sleep_parts`;
  - the readers' tables (weigh-ins, ECG, records, gear, …).
- **Times** are Unix seconds (UTC). Local days come from the watch's own time zone offsets
  (`tz_offsets`), so a day ends where the wearer's day ended.

## Ownership over time

A watch can change hands. `device_assignments` records who owned it when; files and syncs belong
to the owner at the time of recording. The next owner's period starts where the last one ended,
so nothing recorded in between is lost or lands with the wrong person. A former owner keeps read
access to their own period only.

```mermaid
flowchart LR
  anna["Anna's period<br/>from adding the watch<br/>until the hand-over"] -->|"Anna hands it over;<br/>later Ben adds it"| ben["Ben's period<br/>from the hand-over on"]
  f1(["File recorded<br/>before the hand-over"]) -.->|"belongs to"| anna
  f2(["File recorded after it,<br/>even before Ben added the watch"]) -.->|"belongs to"| ben
```

## API and dashboard

- **HTTP.** The standard library's `ServeMux`. Middleware runs in this order:
  - security headers;
  - gzip (not for event streams);
  - session lookup;
  - per route, `needUser` / `needAdmin`.
- **Sessions are server-side.** The database keeps only a token's sha256. Passwords are
  argon2id; passkeys use WebAuthn. Accounts come by invitation, and the first account is the
  admin.
- **Isolation.** Every data route takes the user from the session; another account's ids
  answer 404. Admins manage accounts but see no one's health data.
- **Writes** need a same-origin `Origin`: the public URL's host, or the request's own host
  (a reverse proxy passes it through).
- **Live values and pairing** stream as Server-Sent Events.
- **The dashboard** is React with TanStack Query, Tailwind, Apache ECharts (through goarmin's
  own chart library, `web/src/charts`) and Leaflet. Vite builds it into
  `internal/webui/dist`, which is embedded in the binary. Only the dashboard's own routes get
  `index.html`.
- [api.md](api.md) lists every route.

## Exercise figures

Each exercise of the FIT profile has a 3D figure: a see-through mannequin in one key pose, or
looping through several.

- **Made offline.** The figures are built in Blender (`make figures`, the scripts in
  `tools/figures`). The server embeds the result (`internal/figures`: catalogue and poses), and
  the dashboard embeds the mesh (`web/src/figures/assets/mannequin.glb`).
- **Served.** `/api/exercises` lists the catalogue, and `/api/exercises/{category}/{number}`
  gives one exercise's key poses. Both are cached privately and revalidated by ETag, because the
  data changes only with a new binary.
- **Drawn.**
  - three.js loads in its own chunk with the first figure. It sets each part's rotation from the
    key poses and blends between them.
  - A dialog holds the page's only WebGL context while it is open and gives it back on close; the
    figure pauses off screen.
- Workout steps carry the exercise's category and number, which select its figure.

Details, the pose language and how to change a figure: [figures.md](figures.md).

## What leaves the machine

- Nothing, unless a map tile server is configured. Then the server fetches tiles, caches them,
  and serves them to signed-in users only.
- The browser loads everything from goarmin itself (CSP `default-src 'self'`).
- When the watch asks for its position, goarmin answers that location services are off.
