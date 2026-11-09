# Garmin watch protocol notes

These notes describe how a host (normally the Garmin Connect app, or Gadgetbridge) talks to a
Garmin watch over Bluetooth Low Energy. They are the basis for goarmin's from-scratch Go
implementation, which pulls files directly from the watch on Linux.

- **Source:** Gadgetbridge at commit `eb3550358d` (Codeberg, cloned at
  `~/go/src/codeberg.org/Freeyourgadget/Gadgetbridge`). Every claim cites a file and line there.
- **Cross-checks:** garmin-proto-lab, garmin-ble, garmin-bridge, braycarlson/gfdi and GarminHRM600,
  all cloned under `~/go/src/github.com/`. Agreements and disagreements are noted inline.
- **Written:** 2026-11-07, from the sources, before goarmin's own code talked to a watch. What
  was then observed and verified on a real Fenix 8 is in [05](05-fenix8-capture.md) (Garmin
  Connect's own sync, captured) and [06](06-linux-live.md) (goarmin's sync on Linux); where they
  differ from the notes below, 05 and 06 win. How goarmin is built on top is in
  [architecture.md](../architecture.md).
- **Example bytes** in these notes were computed by the research agents. Re-derive them in Go unit
  tests from the CRC and COBS implementations rather than copying them.

## Layers

From the radio up:

| Layer | What it does | Notes |
|---|---|---|
| BLE GATT | One service `6a4e2800-…`, one RX/TX characteristic pair | [01](01-ble-transport.md) §1 |
| Multi-Link (ML) | Handle byte 0 = control; the host registers services and gets handles | [01](01-ble-transport.md) §2 |
| MLR (optional per handle) | Reliable byte stream: 2-byte header, sequence/ack numbers, retransmission | [01](01-ble-transport.md) §3 |
| COBS | Frames the GFDI byte stream with `00` delimiters | [01](01-ble-transport.md) §4 |
| GFDI | `[len][msg ID][payload][CRC-16]` messages, handshake, ACK rules | [02](02-gfdi-session.md) |
| Legacy file sync | Directory (file 0), chunked downloads, archive flag | [03](03-legacy-file-sync.md) |
| Protobuf + FileAccess | `Smart` protobuf over GFDI 5043/5044; new sync; file bytes over an ML "pipe" | [04](04-protobuf-fileaccess-http.md) |
| HTTP bridge | Watch web requests (weather, AGPS, OAuth) tunnelled through the host | [04](04-protobuf-fileaccess-http.md) §5 |

## Files

| File | Contents |
|---|---|
| [01-ble-transport.md](01-ble-transport.md) | GATT discovery, MTU, writes, Multi-Link control messages, MLR, COBS, CRC, connection lifecycle, Linux/BlueZ implications |
| [02-gfdi-session.md](02-gfdi-session.md) | GFDI frame format, RESPONSE/status rules, session start sequence, every handshake message, time handling, minimal pull-only client checklist |
| [03-legacy-file-sync.md](03-legacy-file-sync.md) | Sync triggers, DOWNLOAD_REQUEST, directory format, FileType table, chunk transfer and CRC chaining, archiving, one complete sync with example frames |
| [04-protobuf-fileaccess-http.md](04-protobuf-fileaccess-http.md) | Protobuf framing and chunking, `Smart` envelope, FileAccess new sync, ML file pipe, ExploreSync, HTTP bridge, proto schema reference |
| [05-fenix8-capture.md](05-fenix8-capture.md) | **Observed** traffic between our Fenix 8 and Garmin Connect (HCI snoop capture), plus a USB copy right after the sync. Settles the sync method (FileAccess only) and several open questions. |
| [06-linux-live.md](06-linux-live.md) | **Live** results of goarmin's own code against the Fenix 8 on Linux: BlueZ pairing (numeric comparison), Multi-Link with pair 0 / client 2, the full GFDI handshake in 1.5 s, no proprietary auth (M1a). The first syncs (M2): 179 files over MLR without one retransmission, all FIT valid and byte-identical to USB where USB has them, an incremental cursor, 0.54 s per file against Garmin Connect's 1.83 s, archived health files deleted by the watch at once, what the watch asks for and how it takes goarmin's answers. |
| [07-fit-health.md](07-fit-health.md) | The health data in the Fenix 8's FIT files, as `pkg/garmin/wellness` reads it: monitoring running totals and `timestamp_16`, stress and Body Battery, respiration, the daily file with ~100 nights of sleep, VO2max, readiness, race predictions; checked against our own files. |
| [08-workout-transfer.md](08-workout-transfer.md) | Sending workouts and schedules to the watch as Garmin Connect does (FileAccess push, from three captures): the push sequence, the workout and schedule files, the schedule as the whole calendar, and why workouts cannot be removed over Bluetooth. |

**Decision (2026-11-07):** goarmin implements only the FileAccess ("new") sync. The capture in
note 05 shows that Garmin Connect syncs the Fenix 8 that way, with no legacy file messages at all.
Note 03 stays as reference material.

## Tools

- [`tools/snoopdecode`](../../tools/snoopdecode) decodes an Android Bluetooth HCI snoop log of a
  Garmin sync, layer by layer, or a trace of goarmin's own sync (`.jsonl`, from
  `goarmin sync -trace`) in the same way.
  - It shows Multi-Link control, MLR, COBS, GFDI messages and `Smart` protobuf fields (named via
    Gadgetbridge's `.proto` files, found automatically in the Codeberg clone).
  - With `-out DIR` it also inflates and saves every file pulled over a file pipe.
  - Usage: `go run ./tools/snoopdecode [-v] [-proto DIR] [-out DIR] <btsnoop_hci.log | trace.jsonl>`.
  - Its building blocks are reusable packages under `pkg/`:
    - `pkg/btsnoop`: HCI snoop logs and ATT extraction;
    - `pkg/cobs`: encode, decode and stream decoder; matches Gadgetbridge's vector byte for byte
      and is fuzzed;
    - `pkg/garmin/gfdi`: CRC-16/ARC, checked against the standard check value, a bitwise
      reference and a Gadgetbridge frame;
    - `pkg/garmin/multilink`: Multi-Link control codec, the mux (host and device side) and
      MLR (see 01 §3.5a); `memlink` is an in-memory link with fault injection for tests;
    - `pkg/garmin/capture`: rebuilds Multi-Link traffic from a snoop log (frames, protobuf
      messages, pipe transfers);
    - `pkg/garmin/mltrace`: writes and reads goarmin's JSONL traces of a live connection, also
      as ATT values for the decoders above;
    - `pkg/protoschema`: the `.proto` reader and renderer.
- [`tools/wellnessdump`](../../tools/wellnessdump) prints what `pkg/garmin/wellness` extracts from
  FIT files (sample counts and ranges, offsets, daily values, sleep nights), for checking the
  extraction against the watch.
- The data side, from M3 on:
  - `pkg/garmin/wellness`: health data from any Garmin FIT file (07);
  - `pkg/sqlitex`: SQLite (modernc, no cgo) with one writer, a read pool, WAL and append-only
    migrations;
  - `pkg/blobstore`: immutable files under their SHA-256;
  - `pkg/fitactivity`: sessions, laps and tracks of activity files, from any device;
  - `pkg/geo`: track bounds, Ramer–Douglas–Peucker simplification, encoded polylines;
  - `pkg/garmin/realtime`: live values over the realtime Multi-Link services (`goarmin live`);
  - `internal/store` and `internal/ingest`: goarmin's schema, `goarmin import`, `reingest`,
    `days` and `activities`.
- The sync stack, from M1b on, each package with its own tests:
  - `pkg/garmin/gfdi` session: compact sequence numbers, request/response matching, protobuf
    chunking and reassembly;
  - `pkg/garmin/smart`: the Smart and FileAccess protobuf codecs;
  - `pkg/garmin/watch`: handshake and always-on responders;
  - `pkg/garmin/fileaccess`: list, pull and archive;
  - `pkg/garmin/fakewatch`: a fake Fenix 8 for end-to-end tests.
- **Capture replay.** Run `GOARMIN_CAPTURE=<snoop log> go test ./pkg/garmin/fileaccess -run CaptureReplay -v`
  to decode a real Garmin Connect sync with goarmin's stack. On ours:
  - 0 frame errors and 34 of 34 pulls inflated;
  - 177 items listed with 21 type names;
  - 114 host FileAccess messages identical to Garmin Connect's, byte for byte.

    Each has its own tests: `make test`.
- [`tools/fitinspect`](../../tools/fitinspect) decodes every FIT file under the given paths with
  `github.com/muktihari/fit`.
  - It reports devices (`file_id`), folders and file types, every message and field.
  - Undocumented fields come with value ranges.
  - Usage: `go run ./tools/fitinspect <dir-or-file>...`.
  - `~/Desktop/fenix8-usb-pull.sh` builds it from the repo to inspect a USB copy of the watch.
- Captures and pulled files hold personal data. Keep them in `~/goarmin-captures/`, never in the
  repo.
- **Fetching Go dependencies.** The `go` command sends `GoCommand/1 (+https://go.dev/cmd/go)` as
  its User-Agent, which our no-identifying-User-Agent rule forbids. With this setup
  (`GOPROXY=direct`), modules come from GitHub through `git`, so dependencies are fetched like
  this:
  1. Download the module with `git` sending a regular browser User-Agent, set per command
     through `GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=http.userAgent GIT_CONFIG_VALUE_0="<browser UA>"`,
     then run `GOPROXY=direct go mod download -x <module>@<version>`. The `-x` log must show only
     `git` commands, no `# get https://…`.
  2. Let the toolchain update the module files from the local cache only:
     `GOPROXY="file://$(go env GOMODCACHE)/cache/download" go mod tidy`.
  - Never edit `go.mod`/`go.sum` by hand.
  - Vanity import paths (`modernc.org/…`) cannot be fetched this way: the `go` command must ask
    the vanity host itself. If the module is already in the cache (another project fetched it),
    tidy against a symlinked copy of the cache whose `@v/list` for that module names only the
    versions that have a `.zip`. Otherwise `go mod tidy` picks a newer version that is listed
    but not cached. This is how `modernc.org/sqlite v1.59.0` was added (M3).

## What a minimal goarmin pull client needs

1. BlueZ pairing agent (`org.bluez.Agent1`) and an OS-level bond with the watch.
2. GATT: service `6a4e2800`, first RX/TX pair, notifications on, MTU read.
3. Multi-Link control: CLOSE_ALL, then register GFDI (reliable flag set).
4. MLR, COBS stream decoder (split on `00`, not one frame per notification), GFDI frames with CRC.
5. GFDI handshake: answer DEVICE_INFORMATION and CONFIGURATION, send the post-handshake messages,
   answer CURRENT_TIME_REQUEST, protobuf status for every protobuf message, bare ACKs for the rest.
6. Legacy file sync first: FILTER(3), directory, download each wanted file, archive it,
   SYNC_COMPLETE. Then FileAccess new sync, chosen by the watch's SYNC_2 capability bit (90).
7. Our own timeouts everywhere (Gadgetbridge has almost none), and the Gadgetbridge bugs listed in
   each note avoided.

## First live test: questions only the real Fenix 8 can answer

**Status after the 2026-11-07 capture (note 05):**
- Question 1 is partly answered: no proprietary auth on a bonded link. Pairing with BlueZ is
  still untested.
- Question 2 is answered: four RX/TX pairs; Garmin Connect uses `2811/2821`.
- Question 3 is answered: protocol 151, maxPacket 4000, SYNC_2 and bit 95 set, compact headers
  used.
- Question 5 is answered: FileAccess only.
- Questions 4 and 6 are still open.

These come from the open-question lists in each note, ordered by how much they decide:

1. **Pairing:** does the Fenix 8 bond with BlueZ, and with which method (passkey entry or numeric
   comparison)? Does it answer CLOSE_ALL/REGISTER before bonding? Does AUTH_NEGOTIATION (5101)
   still arrive on a bonded link? ([01](01-ble-transport.md) §10.9, [02](02-gfdi-session.md) §13.5)
2. **GATT shape:** which RX/TX pairs exist, their write/notify properties, and the MTU.
   ([01](01-ble-transport.md) §10.1–10.3)
3. **Handshake values:** protocol_version, max_packet_size, and whether the watch sets capability
   bits 90 (SYNC_2) and 95. Whether it uses compact (transaction) GFDI headers.
   ([02](02-gfdi-session.md) §13.1–13.3)
4. **Directory:** a raw hexdump of file index 0. Does it start with a 16-byte header? Does it list
   everything the USB `GARMIN/` tree shows? ([03](03-legacy-file-sync.md) §12.1, §12.13)
5. **Sync method:** does legacy sync miss files on the Fenix 8 (issue #5021 risk)? If so,
   FileAccess is needed from the start.
6. **One host at a time:** can the Fenix 8 stay connected to a phone while goarmin syncs?
   ([01](01-ble-transport.md) §6.6)

A Bluetooth HCI snoop log of one sync between the Fenix 8 and its current phone app would answer
most of 2–5 without touching the watch's pairing. A direct Linux pairing probe answers 1 but will
probably unpair the watch from its phone, so it needs an explicit go-ahead.

## Project rules that apply to code built from these notes

- Tests never hardcode dates: derive every timestamp (including Garmin-epoch values,
  Unix − 631065600) from `time.Now()` at run time and inject the clock.
- Any HTTP request goarmin makes to a third party (e.g. if the HTTP bridge ever forwards
  anything) sends a current, regular desktop-browser User-Agent from a shared constant and nothing
  that identifies us. Never forward to Garmin domains.
- MAC addresses are written in capitals (`AA:BB:CC:DD:EE:FF`).
