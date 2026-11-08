# goarmin

A self-hosted replacement for Garmin Connect. goarmin pulls the files straight
from your Garmin watch over Bluetooth on Linux, keeps them on your own machine,
and shows them in a web dashboard with an account for each person in the
household. No Garmin account, phone or cloud is involved.

It works with one watch so far: a Fenix 8 (47 mm, firmware 23.40), on BlueZ
5.87. The protocol notes in [docs/protocol](docs/protocol) come from a capture
of Garmin Connect syncing that watch, and from Gadgetbridge's open-source Garmin
support. Gadgetbridge was read for reference only; none of its AGPL code is in
goarmin.

## What it does

- **Sync.** Add a watch from the browser (a pairing wizard) or with
  `goarmin pair`. goarmin then connects every 30 minutes, lists what is new and
  pulls it. For each watch you choose what happens afterwards: keep the files
  on the watch, so another app can still sync them, or mark them as synced,
  as Garmin Connect does.
- **Raw files are the source of truth.** They are stored content-addressed, and
  everything else can be rebuilt from them (`goarmin reingest`).
- **Health.** Sleep (score, stages, overnight values), HRV against its baseline,
  heart rate, stress and Body Battery, breathing and movement, each by day,
  week, month and year.
- **Training.**
  - Activities: a map, linked charts, laps, splits and time in zones.
  - Performance: VO2 max, training status, load, race predictions.
  - Garmin Coach's plan.
  - The workouts and courses saved on the watch, and lifetime totals.
- **Live.** Heart rate, stress, Body Battery, breathing, steps, calories and
  floors, as the watch measures them.
- **Devices.** Battery, the sync log, the files fetched from each watch.
  Unpair a watch, hand it over to another account, or delete its data.
- **At a glance.** A dashboard of cards you arrange yourself.
- **Accounts.** Passkeys or passwords, by invitation; the first account is the
  admin. Everyone sees only their own data, and admins don't see other
  people's health data either.
- **Import.** FIT files from a USB copy of the watch (`goarmin import`). Import
  from the browser, from a watch plugged in by USB, and from the Garmin Connect
  data export are being built.

## Privacy

- Your data stays in the data directory (`~/.local/share/goarmin` by default):
  a SQLite database and the raw files.
- The dashboard loads everything from goarmin itself, fonts included, and makes
  no requests to third parties.
- The only requests goarmin makes are for map tiles, and only if you configure
  a tile server. The server fetches them with a regular browser User-Agent,
  caches them, and serves them only to signed-in users, privately cached.
- When the watch asks the phone for its position, goarmin answers that
  location services are off.

## Requirements

- Linux with BlueZ and a Bluetooth LE adapter. The user running goarmin needs
  access to BlueZ over D-Bus; a desktop session has it, and no root is needed.
- Go 1.27.3.
- Node.js and npm, to build the dashboard. Tested with Node 26 and npm 11.
- sqlite3, for backups and the inspection targets (`make list-users`, `make db-shell`, …).
- GNU time (`/usr/bin/time`; on Arch the `time` package), for `make bench-import`.

`make check-tools` checks for all of them.

## Quick start

```sh
make web-deps    # once: the dashboard's npm packages
make start       # build, then start the server with the sync role on http://localhost:8642
make logs        # on the first start, the log shows the setup link for the admin account
```

Open the setup link and create your account. Then go to **Devices › Add a
watch** and follow the steps; the watch shows a code to confirm. `make help`
lists every target, including `stop`, `status`, `backup` and `restore`.

## Configuration

`goarmin serve -h` lists the flags. Each has an environment variable, and
`make` also reads a `.env` file in the checkout (git-ignored).

| Variable | Meaning |
|---|---|
| `GOARMIN_DATA_DIR` | Data directory (default `~/.local/share/goarmin`) |
| `GOARMIN_LISTEN` | Address to listen on (default `127.0.0.1:8642`) |
| `GOARMIN_PUBLIC_URL` | The URL people open. Passkeys are bound to its host and need HTTPS beyond `localhost`. |
| `GOARMIN_ADAPTER` | Bluetooth adapter (default `hci0`) |
| `GOARMIN_TILE_URL` | Map tiles: a preset (`jawg-matrix`, `jawg-dark`) or a URL template with `{z}`, `{x}`, `{y}`, and optionally `{s}` and `{token}`. Without one, routes are drawn without a map. OpenStreetMap's own servers are refused, because they require an identifying User-Agent. |
| `GOARMIN_TILE_TOKEN` | The access token for `{token}` in the tile URL. It is never logged. |
| `GOARMIN_TILE_DARK`, `GOARMIN_TILE_ATTRIBUTION` | For a tile style that isn't a preset: whether it is dark already, and its credit line |
| `GOARMIN_FEATURES` | `0` stops goarmin from telling the watch which features it supports |

## Commands

```
serve       run the API and the dashboard (-with-sync also syncs this computer's watches)
pair        pair a Garmin watch with this computer (interactive)
probe       connect to a paired watch and run the protocol handshake
sync        pull new files from a paired watch into the local spool (-once)
live        stream live values (heart rate, steps, stress, …) from a paired watch
import      store and ingest FIT files (a USB copy of the watch, a sync spool)
usb         list the watches on USB, or copy one's GARMIN folder (read-only)
reingest    extract the health data of the stored files again
days        print the daily summaries of an account
activities  list the activities of an account
user        list the accounts, or set an account's password (-set-password, read from stdin)
version     print the version
```

## Layout

- `cmd/goarmin`: the binary.
- `pkg/`: packages meant to be reused elsewhere.
  - `bluez`: BlueZ over D-Bus.
  - `garmin/...`: the watch protocol (Multi-Link, GFDI, the protobuf services,
    file access, live values) and a fake watch for tests.
  - FIT readers: `fitid`, `fitactivity`, `fitwatch`.
  - Helpers: `btsnoop`, `cobs`, `clock`, `httpx`.
- `internal/`: the application. `api`, `store`, `ingest`, `nodehub` (the sync
  role), `syncnode`, `tiles`, `webui`.
- `web/`: the dashboard (React, Vite, Tailwind, ECharts, Leaflet), built into
  `internal/webui/dist` and embedded in the binary.
- `tools/`: `snoopdecode` (Bluetooth captures), `fitinspect`, and `devseed`
  (made-up data for a scratch server).
- `docs/`: [the API](docs/api.md), [research](docs/research.md) and
  [the protocol](docs/protocol).

## Development

- `make check` runs gofmt, go vet, the Go tests and the dashboard's type check
  and tests. Tests never use fixed dates: every time derives from the clock,
  and a guard test enforces it.
- `make dev-frontend` runs Vite on `:5173` against the server on `:8642`.
- `go.mod` changes only through `go mod tidy`. `make dep MOD=module@version`
  fetches a new module with git and a browser User-Agent.
- To work on the tile cache ([github.com/karamble/tilecache](https://github.com/karamble/tilecache))
  at the same time, a local `go.work` with `use (. ../tilecache)` links your
  checkout; it is git-ignored.

Garmin, Fenix and Garmin Connect are trademarks of Garmin Ltd. goarmin is not
affiliated with or endorsed by Garmin.

## License

ISC, see [LICENSE](LICENSE).
