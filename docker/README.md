# goarmin in Docker

Part of the [documentation](../docs/README.md). The choices around it (one computer or a
server, HTTPS, backups, updates) are in [running.md](../docs/running.md).

This directory runs goarmin's **server** in a container: the API, the database and the dashboard.
The **sync role** talks to the watch over Bluetooth, so it stays on a computer the watch is
paired with. Until the sync node arrives (M7), that is `make start` on that computer. Then it
becomes a sync node that uploads to this server.

| File | What it is |
|---|---|
| `Dockerfile` | Three stages: the dashboard (Node), a static binary (Go, no cgo, time zones embedded), and an Alpine runtime with `tzdata` and GNU `time`. It runs as uid 10001. |
| `Dockerfile.dockerignore` | Only sources go into the build: never data, secrets, backups or build output. |
| `compose.yaml` | The app only: no host port, a data and a backup bind mount, an `env_file`, read-only root, all capabilities dropped, a health check, rotated logs. |
| `compose.dev.yaml` | Adds `127.0.0.1:18642` for a local test over plain HTTP. |
| `compose.env.example` | The variables compose needs (host paths, uid, version). |
| `goarmin.env.example` | goarmin's own settings: public URL, map tiles. |

## Quick start

1. **Make two files, outside the repository, from the examples:**
   - `~/.config/goarmin/compose.env`;
   - `~/.config/goarmin/goarmin.env`, mode 600, which holds the map token if you use one.
2. **Create the data and backup directories.** They must be writable by the user the container
   runs as (`GOARMIN_UID`).
3. **Build and start:**

   ```sh
   cd docker
   export GOARMIN_VERSION=$(git describe --tags --always --dirty)
   docker compose --env-file ~/.config/goarmin/compose.env build
   docker compose --env-file ~/.config/goarmin/compose.env up -d
   ```

   The container listens on 8642 on the compose network only. Publish it in an override file of
   your own, in one of two ways:
   - **Behind a reverse proxy with HTTPS.** Needed for passkeys and for anything beyond a
     private network.
   - **Directly over plain HTTP on a private network** (`ports: ["8642:8642"]`). Everyone then
     signs in with a password. `GOARMIN_PUBLIC_URL` must be the
   address people open (`https://host:port`), because:
   - passkeys are bound to its host name, and the origin is its scheme, host and port;
   - `https` makes the session cookies Secure.

   For a test on this machine without a proxy, add `-f compose.yaml -f compose.dev.yaml` and
   open http://localhost:18642. `make docker-dev-up` does that on a scratch copy of your data.
4. **First run on an empty data directory.** The log prints a one-time link that creates the
   admin account:

   ```sh
   docker compose --env-file … logs goarmin | grep setup
   ```

## The settings

| Variable | Where | Meaning |
|---|---|---|
| `GOARMIN_PUBLIC_URL` | goarmin.env | The address people open; sets the passkey RP ID (its host) and origin |
| `GOARMIN_TILE_URL`, `GOARMIN_TILE_TOKEN` | goarmin.env | Map tiles (`jawg-matrix`, `jawg-dark` or a template); optional |
| `GOARMIN_IMPORT_MAX_GB` | goarmin.env | The largest upload (8) |
| `GOARMIN_DATA`, `GOARMIN_BACKUP` | compose.env | Host directories mounted at `/data` and `/backup` |
| `GOARMIN_ENV_FILE` | compose.env | The path of goarmin.env |
| `GOARMIN_UID`, `GOARMIN_GID` | compose.env | The user the container runs as (10001). Under rootless Docker, `0` is the host user that runs Docker, so the files stay theirs. |
| `GOARMIN_VERSION` | compose.env or shell | The image tag and the version the binary reports |

The image itself sets `GOARMIN_DATA_DIR=/data`, `GOARMIN_BACKUP_DIR=/backup` and
`GOARMIN_LISTEN=0.0.0.0:8642`.

## Health and logs

- `GET /healthz` answers `ok`. It needs no session, touches no database and is not logged. It
  suits uptime probes.
- The compose health check runs `goarmin healthcheck`; the image has no curl.
- Logs go to the container output, one line per event, with no tokens or personal data. The
  `local` driver keeps 3 × 10 MB.
- The server sees the proxy's address, not the client's.
- `goarmin` runs as PID 1 and stops cleanly on SIGTERM. A running import gets up to 15 s, and
  `stop_grace_period` is 70 s.

## Backups

`goarmin backup` writes a consistent archive while the server runs:
- a snapshot of the database (`VACUUM INTO`, integrity-checked);
- the raw files;
- the watch spool and `node.json`, when present.

They all go into `goarmin_backup_YYYYMMDD_HHMMSS.tar.gz` (UTC inside the container). Raw files
are content-addressed, written before the database refers to them and never changed. A file the
snapshot names is therefore always in the archive.

```sh
docker compose --env-file … exec -T goarmin goarmin backup -out /backup -keep-days 7 -keep-min 3
```

`-keep-days` removes older archives but always keeps the newest `-keep-min`. On a laptop,
`make backup` writes the same format into `~/goarmin-backups`, and `make restore BACKUP=…` reads
either.

## Moving data in (an exact copy)

The copy carries everything: accounts, settings, devices and their owners, history.

1. **On the old machine:** `make backup`. The server keeps running.
2. **On the new one,** with the container stopped:

   ```sh
   tar -xzf goarmin_backup_….tar.gz --strip-components=1 -C "$GOARMIN_DATA" --wildcards '*/goarmin.db' '*/raw'
   ```

   - Leave out `watches/` and `node.json`: they belong to the computer that syncs the watch.
   - The files must belong to `GOARMIN_UID`. Under rootless Docker with uid 0 they already do.
3. **Start the container.** A newer image upgrades the database by itself.
4. **Sign in with the password,** then add a passkey under Settings › Security. Passkeys made on
   the old address (`localhost`) don't work on the new one. If the account has no password,
   set one:

   ```sh
   docker compose --env-file … exec -T goarmin goarmin user -email EMAIL -set-password < file
   ```

## Updating

```sh
cd docker && git pull
export GOARMIN_VERSION=$(git describe --tags --always --dirty)
docker compose --env-file … up -d --build
```

Migrations run at start. Take a backup first.

## What the build downloads

- The Node, Go and Alpine images from Docker Hub.
- npm packages from the lockfile.
- Go modules through the Go module proxy, checked against `go.sum`.
- `tzdata` and `time` from Alpine's repository.

At runtime goarmin itself fetches nothing but map tiles, and only when a tile server is set.
