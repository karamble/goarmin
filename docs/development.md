# Development

How to build, test and change goarmin. The design is in [architecture.md](architecture.md), the
watch protocol in [docs/protocol](protocol/README.md), the HTTP API in [api.md](api.md).

## Setup

```sh
make check-tools   # go 1.27.3, node, npm, sqlite3, GNU time (optional: Blender, ImageMagick)
make web-deps      # the dashboard's npm packages
make build         # dashboard into internal/webui/dist, then bin/goarmin with it embedded
make check         # gofmt, go vet, Go tests, the dashboard's type check and tests
```

| Command | What it is for |
|---|---|
| `make run` | The server in the foreground on :8642, with the sync role |
| `make dev-frontend` | Vite on :5173 against the server on :8642, with hot reload. Sign in with the password there; passkeys need `goarmin serve -public-url http://localhost:5173`. |
| `make test`, `make race`, `make web-test`, `make vet` | The parts of `make check` |
| `make fuzz` | The fuzz targets (COBS, MTP), 30 s each |
| `make tools` | `snoopdecode` (Bluetooth captures), `fitinspect`, `devseed`, `fitprofilegen`, `wellnessdump` into `bin/` |
| `make docker-image`, `make docker-dev-up` | The container image, tried on a scratch copy |
| `make bench-import FILE=export.zip` | Time an import on a throwaway copy of your data |
| `make figures` | Rebuild the exercise figures with Blender ([figures.md](figures.md)); only needed when you change a pose |

## Rules the code follows

- **Tests never hardcode dates.** No `time.Date(2026, …)`, no date strings or epoch constants in
  tests or fixtures. Take `now := time.Now()` once, derive every value from it, and inject the
  clock (`pkg/clock`, `Store.SetClock`). A guard test fails on literals, the web tests
  included. Fixtures must also hold on every weekday and at every hour: anchor week views to a
  past week, and "today" to the test account's zone (UTC). Ranges such as "the last three days"
  or "six hours ago" otherwise cross a Monday or midnight.
- **goarmin's own HTTP requests never identify us.** They send a regular browser User-Agent
  (`pkg/httpx.BrowserUA`), and `pkg/httpx` is the only place an `http.Client` is made (a guard
  test checks). Developer tools (go, npm, apk, docker, git) keep their own defaults.
- **`go.mod` changes only through `go mod tidy`**, never by hand. Builds run offline
  (`GOPROXY=off`); `make deps` / `make dep MOD=module@version` fetch.
- **Reusable code goes in `pkg/`** (no goarmin knowledge there); `internal/` holds the
  application.
- **Every file starts with the ISC header:**
  `// Copyright (c) 2026 karamble` / `// Use of this source code is governed by an ISC` /
  `// license that can be found in the LICENSE file.`
- **MAC addresses are written in capitals.** Test fixtures use made-up device identifiers,
  never a real watch's.
- **Captures, FIT files and exports never go into the repository.** `.gitignore` keeps them out;
  keep them under `~/goarmin-captures/`.
- **Commits** are signed, carry the repository's own identity and no generated attribution.

## Trying things safely

- **Scratch data.** Every command takes `-data DIR`. Use a copy, never the real directory, for
  experiments:
  - `goarmin backup -out /tmp/x`, then unpack it;
  - or `make docker-dev-up`, which makes the copy for you.
- **Made-up data.** `go run ./tools/devseed -data DIR -email you@example.invalid -days 400`
  fills an account on a scratch server with plausible health data, activities with tracks,
  training values and a coach plan. It refuses the default data directory.
- **No Bluetooth.** `goarmin serve -with-sync -fake-pairer -data DIR` scripts two made-up
  watches for trying *Add a watch* (scratch data only). `goarmin sync -server URL -fake-pairer
  -data DIR` does the same on a scratch node, for *Add a watch* through a node.
- **A node against a server.** `internal/nodeclient`'s end-to-end tests run a real server
  (httptest) and a node: syncing the fake watch with the server refusing uploads for a while,
  and the whole *Add a watch* wizard through the node with the scripted Bluetooth.
- **The fake watch.** `pkg/garmin/fakewatch` answers the protocol in tests: handshake, listing,
  file pipes, MLR with loss injection, live values, battery. The sync, live and pairing tests
  run against it.
- **Capture replay.** `GOARMIN_CAPTURE=~/goarmin-captures/…` replays a real Bluetooth capture
  in the codec tests (opt-in; captures stay out of the repository).
- **Exercise figures in tests.** jsdom draws nothing, so the figure tests mock
  `HTMLCanvasElement.getContext` and check the dashboard's no-WebGL note. three.js never loads in
  a test; a guard keeps it to `web/src/figures/FigureCanvas.tsx`. devseed's strength workout
  names real exercises, so *View pose* shows their figures on a scratch server.
- **Inspecting files.** `go run ./tools/fitinspect FILE…` prints every message of a FIT file;
  **Devices › Files › Inspect** does the same in the dashboard.

## Common changes

**A new FIT reader.**
1. Read the messages in the matching package:
   - health and daily values in `pkg/garmin/wellness` (message-driven: any file that carries
     them);
   - device files in `pkg/fitwatch`;
   - activities in `pkg/fitactivity`.
2. Store them in `internal/store`: a migration and the writer, and make `deleteFileData` cover
   the new table.
3. Bump `ingest.Version`, so `goarmin reingest` re-reads the stored files.
4. Test against a file built at run time with `pkg/fitid/fittest`, with times from the clock.

**A new export reader.** Name the file's kind in `pkg/garmin/gcexport.Kind`, read it in `Read`,
and write it in `store.WriteExportFile`. Bump `ingest.ExportVersion`.

**A migration.** Append to `migrations` in `internal/store/schema.go`. Never edit a shipped
entry: each entry is one schema version, applied once in its own transaction.

**An API route.** Register it in `internal/api` (`server.go` or the topic's file) behind
`needUser`, taking the user from the session. Add an isolation test (another account gets 404),
and document it in [api.md](api.md).

**A dashboard page or glance card.**
- Pages are registered in `web/src/app/routes.tsx` (one registry for the rail, the tabs, the
  router and the palette).
- Cards are registered in `web/src/glance/registry.ts`.
- Charts are pure builders in `web/src/charts/builders` ([charts/README](../web/src/charts/README.md)):
  at most one lime series, with a table and an aria description.
- A new top-level section also goes into `internal/webui.Routes` (a test checks they match).

**An exercise figure.** Pose one exercise in `tools/figures/blender/own.py`, change a family or a
rule in `families.py`, check it in Blender with `tools/figures/blender/session.py`, then
`make figures` and commit the scripts with the generated files. See [figures.md](figures.md).

**The FIT profile.** `make fitprofile` regenerates `pkg/fitprofile` from the official profile
(through `github.com/muktihari/fit`) and Gadgetbridge's community profile
(`GADGETBRIDGE=path/to/clone`).

## Layout

```
cmd/goarmin/       the subcommands
internal/
  api/             HTTP API, auth, SSE (live, pairing)
  store/           SQLite schema, migrations, queries, writers
  ingest/          FIT and export files → rows; the ordered parallel pipeline
  importer/        background imports, USB watcher
  nodehub/         the sync role: schedule, Bluetooth lane, live links, pairing sessions;
                   its Backend (Local: this server's database)
  syncnode/        one sync, the spool, pairing and identifying a watch
  nodes/           the server's sync nodes: who syncs which watch, commands, live relay
  nodeclient/      a sync node: sign-in, the Remote backend, the daemon (goarmin sync -server)
  nodeproto/       what a node and the server send each other
  backup/          backup archives
  figures/         the exercise figures' catalogue and poses (generated, embedded)
  guard/           repository-wide checks (no fixed dates in tests, no raw HTTP clients, …)
  tiles/, webui/, usbsource/, config/
pkg/
  garmin/          gattlink, multilink, gfdi, smart, watch, fileaccess, realtime, wellness,
                   gcexport, product, fakewatch, capture, mltrace
  bluez/           BlueZ over D-Bus
  fitid/ fitactivity/ fitwatch/ fitnamed/ fitprofile/   FIT
  sqlitex/ blobstore/ zipwalk/ mtp/                     storage and files
  auth/ clock/ httpx/ btsnoop/ cobs/ geo/ protoschema/  helpers
web/src/
  app/             shell, routes, section header, date navigator, palette
  api/             typed client, TanStack Query hooks
  charts/          the chart library (ECharts)
  figures/         the exercise figures (three.js, the mannequin mesh)
  glance/          the customizable board
  pages/           health, training, live, devices, settings, auth
  map/, ui/, lib/, theme/
tools/             snoopdecode, fitinspect, devseed, fitprofilegen, wellnessdump;
                   figures/ (the Blender pipeline for the exercise figures)
docs/              these documents, the API, the protocol notes
docker/            the server image and compose files
```
