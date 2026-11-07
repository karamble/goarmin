# goarmin research notes

Status: explore and research phase, nothing implemented yet. Last updated 2026-11-06.

goarmin is a self-hosted Go application that replaces Garmin Connect. It pulls data
directly from Garmin watches over Bluetooth Low Energy (BLE) on Linux, decodes the FIT
files, and serves the data to several users. Garmin's cloud and the phone are not part of
the data path.

Facts marked **(verified)** were checked against source code or primary sources on the
date above. Facts marked **(unverified)** still need confirmation, preferably on real
hardware.

---

## 1. Goal and decisions so far

| Decision | Detail |
|---|---|
| Replace Garmin Connect | No cloud dependency for data. The watch's own Wi-Fi upload to Garmin is not used. |
| Port Gadgetbridge's Garmin support to Go | The Go app talks to the watch itself on Linux. Gadgetbridge on a phone is not in the data path. |
| Recode from scratch | Gadgetbridge is used as the protocol reference. goarmin's code is written from scratch. |
| Target device | **Garmin Fenix 8** |
| Multi-user | Several people, each with their own watch(es), served by one system. |
| Language | Go. Dev host has Go 1.27.3. |

---

## 2. Key conclusions

1. **A direct-BLE Go port is feasible.** File sync needs only part of Gadgetbridge's
   Garmin code: the transport, the message framing, the handshake, and the two file-sync
   protocols. FIT decoding comes from an existing Go library.
2. **Nobody has done this in Go yet.** There is no existing Go implementation of Garmin's
   BLE protocol ("GFDI"). Other-language references exist (section 4).
3. **Pairing is the first hard problem on Linux.** Garmin watches need a bonded BLE link,
   and `tinygo-org/bluetooth` has no pairing API on Linux. goarmin must register a BlueZ
   pairing agent itself over D-Bus.
4. **The Fenix 8 can use either of two file-sync protocols.** Gadgetbridge defaults it to
   the legacy one, with the reliable transport (MLR) on. The newer protobuf-based
   FileAccess sync is opt-in. goarmin should support both, legacy first.
5. **Use `muktihari/fit` for FIT decoding, not `tormoder/fit`.** It is current and keeps
   undocumented fields. Body Battery lives in one of those fields.
6. **Multi-user means two roles.** BLE range is about 10 m, so the part that syncs watches
   must run physically near them. The server can run anywhere, e.g. on Proxmox.
7. **The Gadgetbridge GitHub mirror is stale.** It stopped updating on 2024-12-22. Use the
   Codeberg repository only.

---

## 3. Development environment (verified)

| Item | Value |
|---|---|
| Bluetooth adapter | `hci0`, Intel USB (`btusb`, product `8087/26`) |
| BlueZ | 5.87, service active |
| Go | 1.27.3 |
| Reference repos | Cloned under `~/go/src/<host>/<owner>/<repo>` (section 4) |

---

## 4. Reference sources (cloned locally)

| Path under `~/go/src/` | Language | License | HEAD at clone | Use |
|---|---|---|---|---|
| `codeberg.org/Freeyourgadget/Gadgetbridge` | Java/Kotlin | AGPL-3.0 | 2026-11-06 `eb3550358d` | **Primary protocol reference** |
| `github.com/muktihari/fit` | Go | BSD-3 | 2026-10-19 `5aae3d5` (latest tag v0.28.5) | FIT decoding |
| `github.com/tinygo-org/bluetooth` | Go | BSD-3 | 2026-10-28 `77493f1` (tag v0.17.0) | BLE GATT client over BlueZ D-Bus |
| `github.com/muka/go-bluetooth` | Go | Apache-2.0 | 2024-07-01 `04c4f09` | Alternative BlueZ wrapper. Stale. |
| `github.com/wh1le/garmin-bridge` | Python | AGPL-3.0 | 2026-10-02 `15a5060` | Linux/BlueZ pairing and handshake. Tested on Fenix 6 Pro. |
| `github.com/gwerneckp/garmin-ble` | Python | AGPL-3.0 | 2026-10-23 `6c1fb86` | GFDI v2 stack. File transfer only planned (v0.6.0). |
| `github.com/seofernando25/garmin-proto-lab` | Python | none stated | 2026-09-14 `e029827` | `spec/PROTOCOL.md` (1,144 lines). Not tested on hardware. |
| `github.com/braycarlson/gfdi` | Zig | MIT | 2026-10-24 `a7d92da` | Pulls FIT files over BLE on BlueZ |
| `github.com/mskerwiderski/GarminHRM600` | Python | MIT | 2026-08-30 `c48b98e` | File sync from a Garmin heart-rate strap |
| `github.com/TomaszBojanowski/openExpress` | Python | GPL-3.0 | 2026-11-06 `e20edd5` | Linux Garmin Express replacement over gvfs/MTP. Developer-checked on a Fenix 8 47 mm Solar. Documents the USB startup sequence (section 10.1). |
| `github.com/hanwen/go-mtpfs` | Go | BSD | 2026-07-20 `a759730` | MTP FUSE mount plus an `mtp` package. **Not pure Go:** USB goes through `github.com/hanwen/usb` (cgo, `libusb-1.0`, last commit 2014). |

The Gadgetbridge GitHub mirror (`github.com/Freeyourgadget/Gadgetbridge`) last changed on
2024-12-22. 673 commits touching the Garmin code have landed on Codeberg since then.

---

## 5. Gadgetbridge's Garmin implementation (snapshot `eb3550358d`)

Paths below are relative to
`app/src/main/java/nodomain/freeyourgadget/gadgetbridge/` unless noted otherwise.

### 5.1 Size map

`service/devices/garmin/` (protocol and device logic):

| Subdir | Files | Lines | Relevance for goarmin |
|---|---|---|---|
| top level | 21 | 7,240 | High: `GarminSupport` (2,430), `FileTransferHandler` (647), `FileSyncServiceHandler.kt` (309), `ProtocolBufferHandler` (797), `FileType` (284) |
| `fit/` | 49 | 6,517 | Low: replaced by `muktihari/fit`. Still useful for undocumented fields and raw-sleep handling. |
| `messages/` | 48 | 3,543 | High: GFDI message encode/decode |
| `http/` | 14 | 3,129 | Later: HTTP-over-BLE bridge for weather, AGPS, OAuth |
| `communicator/` | 10 | 2,364 | High: `v2/CommunicatorV2` (894), `v2/MlrCommunicator` (417), `CobsCoDec` (160) |
| `watchapps/` | 3 | 416 | No |
| `deviceevents/` | 8 | 180 | Low |
| `agps/` | 3 | 145 | Later |

`devices/garmin/` (coordinators, preferences, settings UI, database sample providers):
15,098 lines. These are mostly Android-specific. The parts that matter are device
detection and default preferences.

Protobuf schemas: `GBProtobufGenerator/src/main/proto/garmin/`, 38 `.proto` files,
5,942 lines in total. Examples: `gdi_core`, `gdi_smart_proto`, `gdi_file_access`,
`gdi_data_types`, `gdi_http`, `gdi_explore_sync`, `gdi_realtime_settings`.

### 5.2 Protocol layers (bottom-up)

**1. BLE link and bonding**
- Garmin watches need a bonded, encrypted BLE link. On Android, Gadgetbridge uses the
  default `BONDING_STYLE_ASK` and lets the OS do the bonding.
- On Linux, BlueZ does the bonding. The application must register an
  `org.bluez.Agent1` that answers `RequestPasskey` and `RequestConfirmation`.
  `garmin-bridge` registers its agent with capability `KeyboardDisplay`
  (`src/bluetooth.py`).
- Watch-side pairing mode on the Fenix 8, per Garmin's manual: hold the middle-left
  button, then **Watch Settings > Connectivity > Phone > Pair Phone**.

**2. Transport: "V2 / multi-link" GATT** (`communicator/v2/CommunicatorV2.java`, verified)
- Service: `6a4e2800-667b-11e3-949a-0800200c9a66`.
- Five receive/send characteristic pairs: receive `6a4e2810` … `6a4e2814`, send
  `6a4e2820` … `6a4e2824`. All share the same `-667b-11e3-949a-0800200c9a66` suffix.
- Multiplexing:
  - Handle `0x00` is the control channel. Its request types are `REGISTER_ML_REQ/RESP`,
    `CLOSE_HANDLE_REQ/RESP`, `CLOSE_ALL_REQ/RESP`.
  - The host registers each logical service and gets a one-byte handle back, plus a
    "reliable" flag.
  - Every later packet starts with its handle byte.
- Service IDs that can be registered include `GFDI(1)`, `REALTIME_HR(6)`, `REALTIME_HRV(12)`,
  `REALTIME_BODY_BATTERY(20)`, `KEEP_ALIVE(22)`, `FILE_TRANSFER_A(0xa018)` and
  `FILE_TRANSFER_C(0xc018)`.
- MLR (multi-link reliable) adds acknowledgements and retransmission
  (`communicator/v2/MlrCommunicator.java`). Gadgetbridge turns it on by default
  (`PREF_GARMIN_MLR`, default `true`).
- Older watches use V0/V1 services instead (`communicator/v1/CommunicatorV1.java`:
  `9b012401-…`, `6a4e2401-…`). Some Edge devices serve files over Bluetooth Classic RFCOMM
  (`GarminRfcommListener.java`). Neither is expected on the Fenix 8 (unverified).
- The send MTU is negotiated: `maxWriteSize = mtu - 3`. `tinygo-org/bluetooth` exposes
  `GetMTU()`.

**3. GFDI framing**
- A GFDI frame is `[length u16][message ID u16][payload][CRC-16 u16]`. The CRC-16/ARC
  (`ChecksumCalculator.java`) belongs to the GFDI frame, and the whole frame including the CRC
  is then COBS-encoded (`communicator/CobsCoDec.java`) for the transport.
- The length counts the whole frame, including itself and the CRC.
- Responses use `RESPONSE(5000)`, which carries the original message ID and a status.
- Message IDs defined in `messages/GFDIMessage.java` (verified):

| Range | Messages |
|---|---|
| Files | `DOWNLOAD_REQUEST 5002`, `UPLOAD_REQUEST 5003`, `FILE_TRANSFER_DATA 5004`, `CREATE_FILE 5005`, `DELETE_FILE 5006`, `FILTER 5007`, `SET_FILE_FLAG 5008`, `FILE_AVAILABLE 5009`, `CANCEL_FILE_TRANSFER 5022`, `QUEUED_DOWNLOAD 5027`, `COMPRESSED_FILE_DATA 5054` |
| Sync | `SUPPORTED_FILE_TYPES_REQUEST 5031`, `SYNCHRONIZATION 5037` |
| Device | `DEVICE_INFORMATION 5024`, `DEVICE_SETTINGS 5026`, `BATTERY_STATUS 5023`, `SYSTEM_EVENT 5030`, `CONFIGURATION 5050`, `CURRENT_TIME_REQUEST 5052` |
| FIT over GFDI | `FIT_DEFINITION 5011`, `FIT_DATA 5012` |
| Protobuf | `PROTOBUF_REQUEST 5043`, `PROTOBUF_RESPONSE 5044`, `CANCEL_PROTOBUF 5045` |
| Weather / GPS | `WEATHER_REQUEST 5014`, `WEATHER_ALERT 5015`, `EPHEMERIS_* 5019/5020/5028/5029` |
| Notifications / music / phone | `5033–5036`, `5039–5042`, `5049` |
| LiveTrack | `5016`, `5025`, `5046–5048` |
| Auth / secure session | `AUTH_NEGOTIATION 5101`, `LTK_RECONNECT 5102`, `STK_BEGIN_GENERATION 5103`, `CONFIRM_NUMBER 5104`, `STK_RANDOM_NUMBER 5105`, `STK_GENERATION_STATUS 5106`, `LTK_KEY_DISTRIBUTION 5107`, `SESSION_SKD_DISTRIBUTION 5108`, `SESSION_KEY_VERIFICATION 5109`, `PASSKEY_REDISPLAY 5110`, `SECURE_SESSION 5111`, `OUT_OF_BAND_PASSKEY_DATA 5112` |

**4. Handshake and auth**
- After the GFDI channel is registered, the watch sends `DEVICE_INFORMATION` and
  `CONFIGURATION` and expects answers.
- `AUTH_NEGOTIATION (5101)`: Gadgetbridge replies `ACK` with status `GUESS_OK` and **echoes the
  watch's byte 0 and flag mask** (since commit cd46e24819; corrected 2026-11-07, an earlier
  version of this note said "empty flag set"). Details in `protocol/02-gfdi-session.md` §5.3.
- The secure-session messages `5102–5112` are only listed as IDs. Gadgetbridge has no
  handler classes for them. In other words, Gadgetbridge relies entirely on BLE bonding
  and does not do Garmin's application-level key exchange.
- **Risk:** a future Fenix 8 firmware that requires the secure session would break
  Gadgetbridge and goarmin alike. `garmin-proto-lab` documents messages 5101–5112 and
  could serve as a reference if that happens.

**5. File sync, two protocols**
- *Legacy GFDI sync* (`FileTransferHandler.java`, `GarminSupport.java`):
  1. The watch signals pending data with `SYNCHRONIZATION`, or the host sends `FILTER`.
  2. The host downloads the directory listing as file index 0 (`DOWNLOAD_REQUEST`, then
     `FILE_TRANSFER_DATA` chunks).
  3. The host downloads each listed file the same way.
  4. The host marks each file archived with `SET_FILE_FLAG`, so it isn't offered again.
- *New sync, protobuf FileAccess* (`FileSyncServiceHandler.kt`, schemas
  `gdi_file_access.proto` and `gdi_data_types.proto`):
  - Uses `ItemListRequest` / `ItemListResponse` with paging (`cursor_id`), plus
    `ModifyFlagsRequest`, `DeleteItemRequest` and `GetItemChecksumRequest`, all carried over
    `PROTOBUF_REQUEST/RESPONSE`.
  - The code contains workarounds for watches that leave out the next page ID (#5461). It
    also keeps pulling pages within a cursor, because otherwise "anything past the first
    ~100 items … is never seen".
- Selection (`devices/garmin/GarminCoordinator.java`):
  - `PREF_NEW_SYNC_PROTOCOL` defaults to `false`.
  - `defaultNewSyncProtocol()` returns `true` only for Venu 4, Venu X1, Vivoactive 6 and
    Cirqa. Source comments say "#5021 - Some new devices like Venu X1 misses a lot of files
    without the new sync protocol", with a TODO to detect `GarminCapability.SYNC_2`
    instead.
  - Another comment says "new sync protocol without MLR fails to sync large workouts",
    with a TODO to check `DeviceInformationMessage.protocolVersion >= 150`.
- goarmin approach: build the legacy sync first, then FileAccess. Pick the protocol from
  the watch's advertised capabilities, not from the model name.

**6. Requests the watch makes of the "phone"**
- `CURRENT_TIME_REQUEST`, `SYSTEM_EVENT`, `BATTERY_STATUS` and similar must be answered,
  or the session misbehaves.
- An HTTP-over-protobuf bridge (`http/`, `gdi_http.proto`) carries the watch's web
  requests. Gadgetbridge intercepts these for weather, AGPS (`AgpsInterceptor`), OAuth
  (`OauthInterceptor`), contacts, images and hydration, and blocks the rest
  (`FirewallInterceptor`). goarmin can skip this at first and add AGPS and weather later.

**7. ExploreSync** (`ExploreSyncHandler.java`, `gdi_explore_sync.proto`)
- Walks the watch's historical activity catalog and fetches routes as polylines.
  Gadgetbridge turns it on by default (`defaultExploreSync() = true`).
- Not needed for a FIT-first pipeline.

### 5.3 Fenix 8 specifics (verified)

- `devices/garmin/watches/fenix/GarminFenix8Coordinator.java` extends
  `GarminWatchCoordinator` and only matches the BLE name `^fenix 8( - [\d+]+mm)?$`. There
  is no Fenix-8-specific protocol handling.
- Defaults: legacy sync, MLR on, ExploreSync on.
- The Gadgetbridge docs list Fenix 8 support as recent and still experimental. They also
  note that some newer devices only sync everything with "New sync protocol" and
  "Multi-link Reliable" turned on.
- Unverified, needs a test on the real watch:
  - Whether pairing to a Linux host works.
  - Whether the watch advertises `SYNC_2` / FileAccess.
  - Which auth flags it requests in `AUTH_NEGOTIATION`.
  - Whether a Fenix 8 can stay paired to a second "phone" at the same time. Garmin's
    manual describes pairing one phone at a time.

### 5.4 Gadgetbridge storage and export (relevant only for the fallback path)

- Raw files pulled from the watch are saved to `<externalFilesDir>/<device MAC>/`
  (`devices/AbstractDeviceCoordinator.getWritableExportDirectory`).
- The zip auto-export includes that whole external files folder, plus the SQLite
  database and preferences (`util/backup/ZipBackupExportJob.java`).
- File naming changed between Gadgetbridge versions (`getFileName` vs
  `getLegacyFileName`), so identify files by FIT `file_id`, never by file name.

---

## 6. Go building blocks

### 6.1 FIT decoding: `github.com/muktihari/fit` (verified)

| | `muktihari/fit` | `tormoder/fit` |
|---|---|---|
| Latest release | v0.28.5, 2026-10-17 | v0.15.0, 2023-10-01 |
| FIT profile | 21.218 | 21.115 (per README copies) |
| Go | `go 1.26.0` in go.mod | — |
| License | BSD-3 (profile and test data under the FIT SDK license) | — |
| Undocumented fields | Kept in `UnknownFields` on typed messages | Developer fields dropped |
| Encode | Yes, fast | Yes, slow (project's own benchmark) |

### 6.2 BLE on Linux

- **`tinygo-org/bluetooth` v0.17.0** uses BlueZ over D-Bus through `godbus/dbus/v5`. Its
  Linux client API has `WriteWithoutResponse`, `Write`, `EnableNotifications`, `Read` and
  `GetMTU`. It has **no pairing or bonding API and no agent support** (checked in
  `gattc_linux.go` and `gap_linux.go`).
- **Gap to fill:** implement an `org.bluez.Agent1` (`RequestPasskey`, `RequestConfirmation`,
  …) and call `org.bluez.Device1.Pair()` directly with `godbus/dbus/v5`. That is the same
  D-Bus connection BlueZ already uses. `garmin-bridge/src/bluetooth.py` is a compact
  reference.
- **`muka/go-bluetooth`** wraps more of BlueZ, including an `agent` profile package, but
  has had no commits since 2024-07.
- Raw HCI libraries such as `go-ble` were not evaluated. They bypass BlueZ and need extra
  privileges.

### 6.3 Protobuf

The FileAccess sync and the HTTP bridge need protobuf message types for
`gdi_smart_proto`, `gdi_file_access`, `gdi_data_types` and, later, `gdi_http` and
`gdi_core`. The schemas are documented in Gadgetbridge's `.proto` files (section 5.1).
The Go types need to be produced, either with `protoc-gen-go` or by hand for the subset
used. Undecided.

---

## 7. Where health data lives in FIT files

Message numbers below are from the `muktihari/fit` v0.28.5 profile (`MesgNum*`). The
Body Battery field comes from Gadgetbridge's importer.

| Data | FIT message (number) | In official profile? | Notes |
|---|---|---|---|
| Steps, distance, intensity | `monitoring` (55), `monitoring_info` (103) | yes | Daily monitoring files |
| Resting HR | `monitoring_hr_data` (211) | yes | |
| Stress | `stress_level` (227), field 0 | yes | |
| **Body Battery** | `stress_level` (227), **field 3** (sint8, "body_energy") | **no** | Read from `UnknownFields` |
| SpO2 | `spo2_data` (269) | yes | |
| Sleep stages | `sleep_level` (275) | yes | |
| Sleep score | `sleep_assessment` (346) | yes | |
| Respiration | `respiration_rate` (297) | yes | |
| HRV status / values | `hrv_status_summary` (370), `hrv_value` (371) | yes | |
| Skin temperature | `skin_temp_overnight` (398) | yes | |
| `hsa_*` streams | 302–315, 376, 389, 409 | yes | Accelerometer, gyroscope, step, stress, respiration, HR, Body Battery and wrist-temperature streams |
| Raw sleep samples | 273 / 274 | no | Only on watches that don't compute sleep stages themselves. Gadgetbridge `fit/RawSleepStream.kt` (#4048). The Fenix 8 is expected to compute stages on the watch (unverified). |

The device decides which of these messages are present. The real list for the Fenix 8
has to come from its own files (section 11).

---

## 8. What still depends on Garmin

| Function | Status | Options |
|---|---|---|
| Weather on watch | The watch needs valid Garmin OAuth credentials, refreshed every 90 days (Gadgetbridge docs) | Gadgetbridge has "Send fake OAuth responses". Its docs warn the credentials then become invalid and a factory reset may be needed to use the official app again. goarmin could answer weather itself through the HTTP bridge. |
| AGPS (satellite assist data) | Same credential requirement. The watch fetches it over HTTP via the phone. | Intercept the request and serve a file, as Gadgetbridge's `AgpsInterceptor` does. |
| Firmware updates | Garmin Express runs only on Windows and macOS | Community-reported: copy `GUPDATE.GCD` to `/GARMIN/` over USB. Firmware source must be trusted. |
| Connect IQ store, coaching, LiveTrack, cloud analytics | Lost | Out of scope |

---

## 9. Multi-user architecture implications

- **One binary, two roles.**
  - `sync` runs on a machine with a Bluetooth adapter within BLE range of the watches. It
    pulls files and pushes them to the server.
  - `serve` runs centrally, e.g. on Proxmox. It stores, decodes, has user accounts, and
    serves the API and UI.
  - Running `sync` on Proxmox requires passing the Bluetooth adapter through to a VM.
  - Several `sync` nodes can feed one server.
- **Several watches per adapter.** BlueZ can hold several bonded LE devices. Syncing them
  one at a time keeps the state machine simple.
- **Watch identity.** Map watch to user by device serial. It is available from
  `DEVICE_INFORMATION` and in every FIT `file_id`.
- **Raw files are the source of truth.** Store every pulled file unchanged, keyed by
  `file_id` (serial, time_created, type, number) or by content hash, so they can be
  re-decoded as decoding improves.
- **Phone coexistence.** If a watch can only pair with one "phone", a user synced by
  goarmin loses phone notifications and the Garmin app on that watch (unverified,
  section 5.3).

---

## 10. Alternative and fallback data paths

| Path | Phone | Notes |
|---|---|---|
| USB (MTP) | no | Fenix 6/7 transfer files over MTP on Linux (forum reports). Fenix 8 expected the same (unverified). Good for backfill and as a fallback, and to inspect files without touching pairing. |
| Gadgetbridge zip export → Syncthing → goarmin | yes | Works today. Raw FIT files are included (section 5.4). |
| Garmin account data export | — | One-time import of history. FIT files reportedly sit under `DI_CONNECT/DI-Connect-Uploaded-Files` (unverified). |

### 10.1 USB findings on the real Fenix 8 (2026-11-06)

**USB identities.** The watch shows up under two USB product IDs:

| USB ID | Mode | Interfaces |
|---|---|---|
| `091e:0003` | Transitional / "Garmin USB mode" | One vendor-specific interface (class 255): bulk IN, interrupt IN, bulk OUT. No product or manufacturer strings. |
| `091e:51b8` | MTP | Seen by gvfs as `mtp://091e_51b8_<serial>/`. openExpress reports `091e:51b4` for a Fenix 8 Solar. |

**USB mode switching.**
- The Fenix 8 has **no USB mode setting**, unlike the Fenix 6, which offers MTP or Garmin.
- Normal sequence according to openExpress (`openexpress/device/gio_storage.py`), which only reads `/sys` and sends nothing to the device:
  1. After the cable goes in, the watch sits on `0003` for about 19 s.
  2. It drops off the bus for about 0.3 s.
  3. It comes back as an MTP device. gvfs needs a few more seconds before it offers the files.
- openExpress also notes that after an eject the watch comes back as `0003` and stays there until replugged (seen on a Forerunner 55). Don't press eject on the watch while testing.

**Kernel driver `garmin_gps`.**
- It binds to `091e:0003`. At probe time (`garmin_port_probe` → `garmin_init_session`) it sends the Garmin USB protocol `Pid_Start_Session` three times: transport layer 0, packet ID 5, 12-byte header.
- The watch replies `Pid_Session_Started` (ID 6) with its 4-byte unit ID.
- While the driver was bound, the watch showed a full-screen USB connection screen with an eject button.
- We blocked the driver for testing at 23:42 (`/etc/modprobe.d/goarmin-blacklist-garmin_gps.conf`). At the user's request it was **reverted at 23:58**: the blacklist file was removed and the driver reloaded, so the system is back to its default.
- Whether the driver helps or hurts the switch to MTP is **unproven**. After the block, no USB link came up at all (see below), so there was nothing to compare.

**What was observed.**
- Plugged in at 23:26: eight cycles of `0003` within one minute, then a stable `0003` phase.
- After about 40 s the watch switched to `51b8` MTP, and gvfs mounted it.
- About 19 s later the link dropped: the watch rebooted to install a staged firmware update.
- After the update it stayed on `0003` for more than 3 minutes without switching.

**Physical link.**
- Later attempts failed at the electrical level: `usb3-port4: disabled by hub (EMI?)`, `attempt power cycle`, `unable to enumerate USB device`.
- The watch still charged. A hub plus a different cable showed nothing on USB, not even the hub.
- The likely suspects are the watch's charging contacts, the clip, the cable and the port.

**GARMIN folder layout observed on the Fenix 8** (top level of `Internal Storage/GARMIN`):
`Activity Adjstmts Apps Audio Benchmark Calendar ChngLog Clubs ComAct Courses Device DivePlans FbtBackup Goals HMD HRVStatus HSA Location Metrics MltSport Monitor PaceBands PowerGuide Records RunningTrack SCORCRDS Schedule Seg_List Segments Sleep SkinTemp SUMMARY Totals Workouts`.
- More entries appeared after the link dropped, so they may not be real: `Backup Coach EXPRESS FbtPtdBackup Gear Golf HFD http inReach JetLag NewFiles PointCache Quarantine SID SlpDisr Temp TempFIT TMP UpdateManager WALLET Weather WIFI`.
- `Audio/Test` holds about 25 MB of test tones. A full-device walk is slow and pointless, so copy only the data folders.

**MTP from Go.**
- MTP access is exclusive per device, and gvfs or KDE's `kiod6` can hold it.
- A single-binary goarmin needs either cgo plus `libusb-1.0` (`go-mtpfs`), or its own MTP (PTP over bulk endpoints) on Linux usbfs with `golang.org/x/sys/unix`.
- Raw USB access needs a udev rule granting the user access to `091e` devices.

---

## 11. Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Fenix 8 won't bond with BlueZ, or rejects the "phone" role from Linux | Blocks direct pull | Test early (section 12). Fall back to USB/MTP or the Gadgetbridge export. |
| Firmware starts requiring the GFDI secure session (5102–5112) | Breaks sync | Watch Gadgetbridge and `garmin-proto-lab`. Capture real traffic. |
| Legacy sync misses files on the Fenix 8 | Data gaps | Implement FileAccess sync. Choose by capability. |
| Protocol changes across firmware versions | Breakage | Keep tests based on recorded traffic. Track Gadgetbridge commits touching `service/devices/garmin/`. |
| Pairing to Linux unpairs the user's phone | Lost notifications | Document it. Decide per user. |

---

## 12. Open questions

1. Can a Fenix 8 stay paired with a phone (for notifications) and a Linux host (for sync)
   at the same time?
2. Which sync protocol and capabilities does the Fenix 8 advertise (`SYNC_2`,
   `protocolVersion`)?
3. Which auth flags does it request in `AUTH_NEGOTIATION`?
4. Which FIT messages does the Fenix 8 actually write (section 7)?
5. Is weather or AGPS on the watch wanted? This decides whether the HTTP bridge and the
   OAuth handling are in scope.
6. Where will the `sync` role run: a laptop, a Raspberry Pi, or a Proxmox VM with USB
   passthrough?
7. Protobuf types: generate with `protoc-gen-go`, or hand-write the subset?

---

## 13. Next research steps (no implementation yet)

1. **Inspect files over USB, without touching pairing.** Connect the Fenix 8 over USB,
   copy its `GARMIN/` folders over MTP, and list every message and field with
   `muktihari/fit`. This answers question 4.
2. **Capture real BLE traffic.** On the Android phone paired to the Fenix 8, enable the
   developer option "Enable Bluetooth HCI snoop log". Run one sync, then open the
   `btsnoop` file in Wireshark. This shows real Fenix 8 handshake, auth flags and sync
   traffic (questions 2 and 3), and the captures double as test fixtures.
3. **Pairing probe from Linux.** This step re-pairs the watch and will likely unpair it
   from its current phone, so run it only with explicit go-ahead. Write a minimal Go
   program that:
   - registers a BlueZ agent and bonds,
   - registers the GFDI multi-link service,
   - completes the handshake,
   - lists files without downloading them.

   This answers questions 1 to 3 on the dev host's adapter.

---

## 14. Detailed protocol notes

The byte-level protocol notes derived from Gadgetbridge live in `docs/protocol/`:
`01-ble-transport.md`, `02-gfdi-session.md`, `03-legacy-file-sync.md` and
`04-protobuf-fileaccess-http.md`, with an overview in `docs/protocol/README.md`.

---

## 15. Sources

- Gadgetbridge source, Codeberg: <https://codeberg.org/Freeyourgadget/Gadgetbridge>
  (commit `eb3550358d`)
- Gadgetbridge Garmin docs: <https://gadgetbridge.org/basics/topics/garmin/>
- Gadgetbridge auto export: <https://gadgetbridge.org/internals/automations/auto-export/>
- Gadgetbridge data management: <https://gadgetbridge.org/internals/development/data-management/>
- muktihari/fit: <https://github.com/muktihari/fit>
- tormoder/fit: <https://github.com/tormoder/fit>
- tinygo-org/bluetooth: <https://github.com/tinygo-org/bluetooth>
- garmin-bridge: <https://github.com/wh1le/garmin-bridge>
- garmin-ble: <https://github.com/gwerneckp/garmin-ble>
- garmin-proto-lab: <https://github.com/seofernando25/garmin-proto-lab>
- gfdi (Zig): <https://github.com/braycarlson/gfdi>
- GarminHRM600: <https://github.com/mskerwiderski/GarminHRM600>
- Garmin manual, Pairing Your Phone:
  <https://www8.garmin.com/manuals/webhelp/GUID-EECCAC99-90D6-4AB1-9A3A-EC433D3365E2/EN-US/GUID-AA356C1E-94FB-4E7E-B7F5-042F134AA86D.html>
- Garmin forum, firmware update on Linux:
  <https://forums.garmin.com/outdoor-recreation/outdoor-recreation-archive/f/fenix-5-series/171894/firmware-update-on-linux>
- Garmin forum, sleep and monitoring unknown messages:
  <https://forums.garmin.com/developer/fit-sdk/f/discussion/242794/sleep-and-monitoring-files-unknown-messages>
- GarminDB discussion on MTP: <https://github.com/tcgoetz/GarminDB/discussions/156>
