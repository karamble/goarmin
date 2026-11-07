# 05 — What our Fenix 8 actually does (captures, 2026-11-07)

Part of the goarmin protocol notes (`docs/protocol/`). Unlike notes 01–04, which come from
reading Gadgetbridge, this note records **observed traffic** between our own Fenix 8 and the
official Garmin Connect app. It also records what a USB copy of the watch showed right after
that sync. Where an observation contradicts or settles something in 01–04, that is called out.

Personal identifiers (the watch's unit ID, its Bluetooth address, FIT serial numbers) are left
out on purpose. The raw captures live outside the repo, in `~/goarmin-captures/`, and must never
be committed.

## How the data was captured

- **Bluetooth:** an Android HCI snoop log from the phone during a normal Garmin Connect sync. It
  was pulled with `adb bugreport` (`~/Desktop/goarmin-pull-btsnoop.sh`) and decoded with
  [`tools/snoopdecode`](../../tools/snoopdecode).
  - The tool unpacks btsnoop records, then HCI ACL, L2CAP, ATT, Multi-Link with MLR, COBS and
    finally GFDI.
  - It decodes every `Smart` protobuf message field by field against Gadgetbridge's `.proto`
    files.
  - With `-out` it inflates and saves every file pulled over a file pipe.
  - Run it as `go run ./tools/snoopdecode -out <dir> <btsnoop_hci.log>`.
  - The phone wrote `btsnoop_hci.log.filtered` files. **No records were truncated**, so
    Android's "filtered" mode does not cut ATT payloads.
  - The full session (connect, handshake, sync) is in `btsnoop_hci.log.filtered.last`. The
    newer `.filtered` file continues the same connection after the log rotated.
- **USB:** `~/Desktop/fenix8-usb-pull.sh` copied the FIT files from the health and activity
  folders over MTP a few minutes after the sync. `fitinspect` then decoded them.

| Item | Value |
|---|---|
| Phone app | Garmin Connect (Multi-Link client ID **1**) |
| Phone | Pixel 8a, Android 17 |
| Watch | fenix 8 – 47 mm, product number **4536** |
| Watch firmware | 23.39 |
| Watch GFDI protocol version | **151** |
| Watch `max_packet_size` | **4000** |

## Findings

### 1. Garmin Connect syncs the Fenix 8 with FileAccess only (new sync)

The whole sync ran through protobuf FileAccess. Message counts:

| Direction | Message | Count |
|---|---|---|
| H→W | `ItemListRequest` | 7 |
| W→H | `ItemListResponse` | 7 (first one 4,176 bytes) |
| H→W | `PullItemDataRequest` | 34 |
| W→H | `PullItemDataResponse` | 34 |
| H→W | `ModifyFlagsRequest` (archive) | 33 |
| W→H | `ModifyFlagsResponse` | 33 |
| W→H | `ItemUpdatedNotification` | 33 |
| W→H | `TransferStatusRequest` | 42 |
| H→W | `TransferStatusResponse` | 42 |
| W→H | `ItemAddedNotification` | 3 |
| W→H | `ResourceUpdateNotification` | 39 |
| H→W | `PushItemDataRequest` | 9 |
| H→W | `PriorityUpdateRequest`, `CancelTransferRequest` | 1 each |

**Not a single legacy file message was sent:** no `FILTER`, no `DOWNLOAD_REQUEST` (not even the
index-0 directory "flush" that Gadgetbridge sends before new sync), no `FILE_TRANSFER_DATA`, no
`SET_FILE_FLAG` and no `SYNCHRONIZATION`.

**Decision this supports:** goarmin implements only the FileAccess sync. The legacy file
transfer (note 03) is reference material; it is not a build target.

### 2. File bytes go over Multi-Link file pipes with MLR

- Garmin Connect registered file-transfer services `0x2018`, `0x4018`, `0xA018` (twice),
  `0xC018` and `0xE018`, all **reliable** (MLR). The watch assigned handles `0x81`–`0x87`.
  This confirms that reliable handles are `0x80 | n`.
- **Pipe configure, pull (read):** the host sends `00 00 <token u16 LE> 00 00` (6 bytes), for
  example `00 00 17 00 00 00`. This is **Gadgetbridge's 6-byte form**, not garmin-proto-lab's
  10-byte form, which settles open question 3 in note 04.
- **Watch reply:** `00 00 00`, then the data.
- **Pull data is zlib-compressed.** The first data bytes are `38 CB`: CMF `0x38` means deflate
  with a 2 KiB window (CINFO 3 → 2^11), and the FLG check passes. Go's `compress/zlib` reads it.
- **Push (write):** the host sends `00 01 <token u16 LE> 00 00` followed by its own zlib data.
  - Garmin Connect pushed about 8 MB to the watch on one pipe and smaller items on others.
  - Out of scope for a pull-only goarmin, but worth decoding later: possibly GPS assist data,
    software or maps.

### 3. Handshake, as Garmin Connect does it

All timings are from the start of the session.

1. `CLOSE_ALL_REQ` (client 1) → `CLOSE_ALL_RESP`, whose trailer is `00 00 01`.
2. **`REGISTER_ML_REQ` REGISTRATION (service 4), non-reliable, first.** Gadgetbridge never does
   this; garmin-proto-lab predicted it.
3. `REGISTER_ML_REQ` GFDI (service 1) with flags `0x02` → handle **`0x80`**, reliable.
4. `REGISTER_ML_REQ` **KEEP_ALIVE (service 22)**, non-reliable → handle `0x31`.
   - Gadgetbridge defines this service but never uses it.
   - It is cheap; goarmin should register it too.
5. The watch sends `DEVICE_INFORMATION`: protocol 151, product 4536, sw 23.39, maxPacket 4000.
   - The bluetooth/device/model names are `"fenix 8 - 47mm"`, `"fenix"` and `"8 - 47mm"`.
   - The tail is `00 00`: no MAC block.
6. Host reply (one RESPONSE, no bare ACK first):
   - protocol `0x0096` = **150**;
   - product `0xFFFF`, unit `0xFFFFFFFF`, sw `0x2DF1`, maxPacket `0xFFFF`;
   - then the phone name and manufacturer strings.
7. The watch sends `CONFIGURATION` (17 bytes, 79 bits). Relevant bits it sets:
   - SYNC, DEVICE_INITIATES_SYNC, HOST_INITIATED_SYNC_REQUESTS, EXPLICIT_ARCHIVE;
   - EXPLORE_SYNC, CURRENT_TIME_REQUEST_SUPPORT, MULTI_LINK_SERVICE, OAUTH_CREDENTIALS;
   - **SYNC_2 (bit 90)**, REALTIME_SETTINGS and FEATURE_CAPABILITIES_EXCHANGE (95).
8. The host ACKs it, then sends its own `CONFIGURATION` (17 bytes, 83 bits).
   - **SYNC_2 = true**; MULTI_LINK_SERVICE, EXPLORE_SYNC and FEATURE_CAPABILITIES_EXCHANGE are
     **not** set.
9. Protobuf traffic: core, ecg, device_status and realtime_settings requests, plus Smart field
   30 (unknown to our labels).
10. `SUPPORTED_FILE_TYPES_REQUEST` → 16 types. FIT 4, 32, 35, 38, 41, 44, 49, 58, 68, 70, 72, 73
    and 74 are among them, and 255/244, 245 and 247 are present.
11. `DEVICE_SETTINGS` → `03 06 01 01 07 01 01 08 01 00`. That is AUTO_UPLOAD=1,
    WEATHER_CONDITIONS=1 and WEATHER_ALERTS=0, the same as Gadgetbridge.
12. `FIT_DEFINITION` / `FIT_DATA` to the watch (a small settings or user record).
13. `SYSTEM_EVENT SYNC_READY` (value `00`).
14. The watch sends `CURRENT_TIME_REQUEST` and `MUSIC_CONTROL_CAPABILITIES`. FileAccess
    `ItemAddedNotification` follows about 7 s in, and the host's first `ItemListRequest` about
    7.4 s in.

### 4. Compact (transaction) GFDI headers are used in both directions

- From `CONFIGURATION` onwards, **every** GFDI message from both sides uses the compact header
  (bit 15 set, sequence in the high byte).
- **Responses carry the sequence number of the message they answer.** Example: the watch's
  CONFIGURATION had seq 1, and the host's RESPONSE to it also has seq 1. Each side numbers its
  own requests independently.
- This confirms garmin-proto-lab's "transaction mode" (note 02 §2.2). Gadgetbridge never sends
  compact frames and still works.
- goarmin should copy Garmin Connect's behaviour: once the watch uses compact headers, use them
  too, and echo the sequence number in responses.

### 5. No Garmin proprietary authentication

- `AUTH_NEGOTIATION` (5101) and the secure-session messages 5102–5112 never appear.
- The link was OS-bonded, and that was enough.
- This matches garmin-proto-lab: with an OS bond, the proprietary auth route isn't used.
- For goarmin, a BlueZ bond (with an `Agent1`) is the only authentication needed.

### 6. GATT layout of the Fenix 8

The Multi-Link service `6a4e2800` exposes:
- characteristic `2803`;
- **four** RX/TX pairs: `2810/2820`, `2811/2821`, `2812/2822` and `2813/2823`;
- characteristics `2830` and `2840`.

The Connect IQ watch-app service `6a4e8022` (`4c80`/`cd28`) is present too.

- **Garmin Connect used the second pair, `2811/2821`.** Gadgetbridge always takes the first
  existing pair, `2810/2820`, and works.
- Hypothesis: each host app uses its own pair, which may be how a watch keeps a Gadgetbridge
  and a Garmin Connect link apart.
- goarmin: start with `2810/2820` and fall back to the next pair if registration fails.
  Record which pair works.

### 7. Health files leave the watch after the sync

The USB copy a few minutes after the sync found:

| Folder | FIT files |
|---|---|
| Monitor | 1 |
| Sleep, HRVStatus, Metrics, SkinTemp, HSA, Device, ComAct | 0 |
| SUMMARY | 4 |
| Activity | 36 |
| Sports | 113 |
| Workouts | 11 |
| others | a few each |

All 176 files decoded without errors.

- The archive flag sent with `ModifyFlagsRequest` (33 times) evidently lets the watch remove
  health files straight away. **Confirmed live** (06, M2 run 3): even a listing without
  `exclude_all_flags` shows no archived health file, while archived activities stay.
- **Consequences:**
  - USB is not a reliable source of health data once any BLE sync has archived it.
  - Only one host should archive. If Garmin Connect (or Gadgetbridge) keeps syncing, goarmin will
    see no health files.
  - goarmin must store every pulled file durably *before* it sends `ModifyFlagsRequest`.
- Activity files stay on the watch after archiving, at least for a while.

### 8. FileAccess field values, exactly as Garmin Connect sends them

Decoded against Gadgetbridge's `gdi_file_access.proto` and `gdi_data_types.proto`. `snoopdecode`
reads the `.proto` files at run time. `0xA5A5` below is a `UUID{most_significant: 0xA5A5,
least_significant: 0xA5A5}`, which serves as the "synced" flag.

**Listing**
1. H→W `ItemListRequest { exclude_all_flags: 0xA5A5, get_flags: 0xA5A5 }`, i.e. "everything not
   yet flagged as synced".
2. W→H `ItemListResponse { status: SUCCESS, session_id: 1, items: [...] }`. `session_id`
   present means more pages follow.
3. H→W `ItemListRequest { session_id: 1 }`, sent while the response carries a `session_id`.
4. The last page has no `session_id` and carries `next_transaction_id`, e.g. 38519.
5. Later listings in the same session send
   `ItemListRequest { transaction_id: <next_transaction_id>, exclude_all_flags: 0xA5A5, get_flags: 0xA5A5 }`.
   This is an incremental listing.
   - `next_transaction_id` rose from 38519 to 38520 to 38558 during the sync.
   - goarmin should persist it per watch.
6. Garmin Connect also lists full GDXML files:
   `ItemListRequest { get_flags: 0xA5A5, included_data_types { format: GDXML_FULL_FILE } }`.
   One device XML file (about 66 KB) was then pulled.

**Items** (`ItemReference` in the list):
- `uid`, then `data_type { format: GDXML_DATA_TYPE, gdxml_string: "FIT_TYPE_32", gdxml_string_key: 0 }`.
- **The type string appears only the first time a key is used; later items carry only
  `gdxml_string_key`.** Keep a per-session key → name table.
- Also present: `data_size`, `requested_flags: 0`, `transaction_id` and `urgency`. Urgency was
  SLOW for listed items and NORMAL for newly added ones.
- 7 pages listed 177 items. By type:

| Type | Count |
|---|---|
| FIT_TYPE_3 (sports) | 113 |
| FIT_TYPE_32 (monitor) | 17 |
| FIT_TYPE_44 (metrics) | 12 |
| FIT_TYPE_5 (workouts) | 11 |
| FIT_TYPE_33 | 2 |
| FIT_TYPE_6 | 2 |
| ErrorShutdownReports | 2 |
| FIT_TYPE 59, 74, 77, 8, 41, 87, 37, 65, 35, 29, 11, 10, 7, 2, 1 and KPI | 1 each |

- Hypothesis about the `uid` layout. It holds for the examples checked but is not confirmed:
  - `most_significant` = `<u32 Garmin timestamp> << 32 | <u32 item number>`;
  - `least_significant` = `0x01 << 56 | <data size>`. In one ItemAddedNotification the low bits
    equal `data_size` (1910 = 0x776).
  - Host-created push items use `{0xFFFFFFFFFFFFFFFF, <random>}`.

**Pulling one item**
1. H→W `PullItemDataRequest { item { uid }, priority: P, supported_transports: [MULTILINK_TRANSPORT_PIPE], offset: 0, requested_compression_window: 15 }`.
   - Only the `uid` is sent in `item`; Gadgetbridge sends the data type string instead.
   - **P is not constant.** The replay test (`pkg/garmin/fileaccess/capture_test.go`) counted
     34 pulls: 32 at priority 34 (the USER_INITIATED range, 30–39), 1 at 24 and 1 at 21.
     Earlier versions of this note said 24 for all of them.
   - goarmin uses 24 (the STANDARD range, 20–29) for its background syncs. It is configurable.
2. W→H `PullItemDataResponse { result: SUCCESS, transport: MULTILINK_TRANSPORT_PIPE, transfer_handle: N, compression_window: 11 }`.
3. The host registers the next free file-transfer service as reliable.
   - Garmin Connect rotates through `0x2018, 0x4018, 0x6018, 0x8018, 0xA018, 0xC018, 0xE018`.
   - **`0x8018` exists** (Gadgetbridge's list lacks it; garmin-proto-lab has it).
   - The watch reuses handles `0x81`–`0x87`, and **MLR sequence numbers restart at 0 on every
     registration**.
4. H→W pipe configure `00 00 <N u16 LE> 00 00`; W→H `00 00 00`; then the zlib data, using the
   window from `compression_window` (11 = 2 KiB).
   - **The watch closes the pipe itself** once the data is through. It sends an unsolicited
     `CLOSE_HANDLE_RESP` for the pipe's handle; the host never sent a `CLOSE_HANDLE_REQ` for it.
   - The end of the data is therefore the end of the zlib stream, or this close.
5. W→H `TransferStatusRequest { uid, transfer_handle }` (no `failure_reason` on success).
   - The host acknowledges it at once. About 0.1–1 s later it answers with
     `TransferStatusResponse { next_transfer_priority }`.
   - The value is the priority of the host's *next* transfer. Across the 42 answers it was:
     24 (35 times), 20 and 21 (twice each), 34 and 44 (once each).
   - Once nothing more was queued, the answer was **empty** (`DA 02 03 AA 01 00`).
   - goarmin sends 24 while pulls remain, and the empty answer after the last one.
6. H→W `ModifyFlagsRequest { uid, set_flags: 0xA5A5 }`; W→H `ModifyFlagsResponse { status: SUCCESS }`;
   W→H `ItemUpdatedNotification { uid, changed_flags_set: 0xA5A5 }`.
7. Interleaved and harmless:
   - `ResourceUpdateNotification { status: CURRENT_PRIORITY_LOWERED, current_priority: 24 }`;
   - `PriorityUpdateRequest { transfer_handle, new_priority: 24 }` → SUCCESS;
   - one `CancelTransferRequest { transfer_handle }` → SUCCESS.

**What was pulled.** 34 transfers, all `SUCCESS`, all inflated with Go's `compress/zlib`:
- 17 × monitoring (FIT 32);
- 12 × metrics (FIT 44);
- 1 each of FIT 41, 74 and 77, and a segment list;
- 1 device XML.

All 33 FIT files decode with `muktihari/fit` without errors. Pull sizes ranged from 206 bytes to
about 66 KB after inflating.

**Pushes (phone → watch, out of scope for goarmin).** 9 transfers:
- FIT files (e.g. FIT_TYPE_41) sent as `PushItemDataRequest { item { uid, data_type, data_size }, priority: 34, … }`;
- one JPEG;
- small binaries starting `0c 11 ee 5e`;
- an ~8 MB item still in flight when the log rotated.

## Open questions this capture did not answer

- Smart field 30 is `event_sharing` (`EventSharingService` in `gdi_smart_proto.proto`):
  subscribe request/response, alert notification, support request/response, i.e. incident and
  alert sharing. It was missing from snoopdecode's summary labels, which now name it. Both sides
  sent a `subscribe_request` (f1) and the host got an answer (f2).
  - Still open: whether the watch expects a `SubscribeResponse` from us. goarmin discards the
    watch's request, as for every service it does not serve.
- The `core` requests and their answers:
  - the watch sends `get_location_request` twice and `location_updated_set_enabled_request` once;
  - Garmin Connect answers "location services disabled" and "not available" (06, M2);
  - Garmin Connect's own `FeatureCapabilitiesRequest` (core f8) carries its installation GUID.
- `garmin_connect_service` (Smart field 47) `CutWellnessFilesRequest` makes the watch write its
  monitoring data into new files (06, M2).
- What the 8 MB push and the `0c 11 ee 5e` blobs are (GPS assist data? maps?).
- Why Garmin Connect pulled only some listed types (monitor, metrics and a few others) and not,
  e.g., the 113 sports files. Presumably it pulls only what it hasn't already got in the cloud.
  goarmin's own rule: pull everything health- and activity-related that isn't flagged as synced.
- Whether the watch requires `REGISTRATION` and `KEEP_ALIVE` registrations. Gadgetbridge skips
  both and works.
- Whether a second host can register on another pair while Garmin Connect is connected.
