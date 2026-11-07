# 03 — Legacy GFDI file download

Part of the goarmin protocol notes (`docs/protocol/`). Derived from Gadgetbridge at commit
`eb3550358d` (Codeberg), cross-checked against garmin-proto-lab, braycarlson/gfdi and
GarminHRM600. Written 2026-11-07. Describes the protocol, not Gadgetbridge's code; goarmin is a
from-scratch implementation.

## 0. Conventions and citation key

- `A/` = `app/src/main/java/nodomain/freeyourgadget/gadgetbridge/`, `S/` = `A/service/devices/garmin/`, `D/` = `A/devices/garmin/`. Cross-check repos are cited by repo name: `garmin-proto-lab/…`, `gfdi/…` (braycarlson), `GarminHRM600/…`.
- **Endianness:** every multi-byte field is little-endian. Sources: writer S/messages/MessageWriter.java:18,24; reader S/messages/GFDIMessage.java:250; directory parser S/FileTransferHandler.java:264.
- **Signedness:** Gadgetbridge reads u8 and u16 as unsigned but u32 as a signed Java int (S/GarminByteBufferReader.java:26-28, 42-44, 46-48). In Go, use `uint32` everywhere.
- **GFDI frame** (owned by the transport and session notes; repeated only to make the test vectors below usable): `[len u16][msgId u16][payload][crc u16]`. `len` counts the whole frame, including the length field and the CRC (S/messages/GFDIMessage.java:130-133, 251-254).
  - CRC is CRC-16/ARC, computed nibble-wise with the FIT table (S/ChecksumCalculator.java:22-25, 41-49). Check value: `"123456789"` gives `0xBB3D` (garmin-proto-lab/spec/PROTOCOL.md:180).
- **Compact incoming IDs:** if bit 15 of msgId is set, the real ID is `(msgId & 0xFF) + 5000` (S/messages/GFDIMessage.java:48-51). The watch may send `FILE_TRANSFER_DATA` this way. Gadgetbridge always replies with full 16-bit IDs.
- **RESPONSE (5000)** payload is `[origMsgId u16][status u8][message-specific bytes…]` (S/messages/status/GFDIStatusMessage.java:9-11; S/messages/status/GenericStatusMessage.java:30-36).
  - Status values: 0 ACK, 1 NAK, 2 UNSUPPORTED, 3 DECODE_ERROR, 4 CRC_ERROR, 5 LENGTH_ERROR (S/messages/GFDIMessage.java:225-231). proto-lab agrees and calls 3 COBS_DECODER_ERROR (garmin-proto-lab/spec/PROTOCOL.md:186-209).
- **Gadgetbridge's per-message order** (S/GarminSupport.java:488-532):
  1. Run the handlers (the first non-null return becomes the "followup").
  2. Send the ACK for the incoming message.
  3. Send the reply, if any.
  4. Send the followup.
  5. Evaluate device events.
  6. Pump the download queue (`processDownloadQueue`).
  - Consequence: anything a handler sends synchronously goes on the wire before the ACK of the message that triggered it (see §7.3).
- **Unparsed messages get no reply at all.** This covers 5006, 5022, 5027 and 5054, and any unknown ID. `UnhandledMessage` builds an UNSUPPORTED status with `sendOutgoing=false` (S/messages/UnhandledMessage.java:9-19; S/messages/status/GenericStatusMessage.java:23-27).

### Message IDs used here (S/messages/GFDIMessage.java:135-202)

| ID | Hex | Name | Direction | Gadgetbridge support |
|---|---|---|---|---|
| 5000 | 0x1388 | RESPONSE | both | yes |
| 5002 | 0x138A | DOWNLOAD_REQUEST | host→watch | yes |
| 5004 | 0x138C | FILE_TRANSFER_DATA | watch→host (download) | yes |
| 5006 | 0x138E | DELETE_FILE | – | ID only, never sent |
| 5007 | 0x138F | FILTER | host→watch | yes |
| 5008 | 0x1390 | SET_FILE_FLAG | host→watch | yes |
| 5009 | 0x1391 | FILE_AVAILABLE | watch→host | yes |
| 5022 | 0x139E | CANCEL_FILE_TRANSFER | ? | ID only |
| 5027 | 0x13A3 | QUEUED_DOWNLOAD | watch→host | ID only, ignored silently |
| 5030 | 0x13A6 | SYSTEM_EVENT | host→watch | yes |
| 5031 | 0x13A7 | SUPPORTED_FILE_TYPES_REQUEST | host→watch | yes |
| 5037 | 0x13AD | SYNCHRONIZATION | watch→host | yes |
| 5054 | 0x13BE | COMPRESSED_FILE_DATA | watch→host | ID only, never requested |

## 1. Fenix 8 defaults (context)

- `GarminFenix8Coordinator` does not override `defaultNewSyncProtocol()` (D/watches/fenix/GarminFenix8Coordinator.java:8-18). The base class returns false (D/GarminCoordinator.java:141-143).
- The `new_sync_protocol` pref defaults to false (S/GarminSupport.java:725-727) and MLR defaults to true (S/GarminSupport.java:729-731).
- Only Venu X1, Venu 4, Vivoactive 6 and CIRQA default to the new protocol. The reason given is "#5021 – Some new devices like Venu X1 misses a lot of files without the new sync protocol" (D/GarminCoordinator.java:124-134; commits d3a5216875, 20e7f94dec).

## 2. Sync triggers

### 2.1 Connection prerequisites (sent once, after the session is up)

`completeInitialization` sends two things (S/GarminSupport.java:1097-1127):
- `SUPPORTED_FILE_TYPES_REQUEST` (S/GarminSupport.java:1103).
- `SYSTEM_EVENT SYNC_READY` (S/GarminSupport.java:1110). The official app does the same on RFCOMM, "after which the device requests a sync" (S/GarminSupport.java:1090-1094).

**SUPPORTED_FILE_TYPES_REQUEST 5031** (S/messages/SupportedFileTypesMessage.java:11-15): the payload is empty, so the frame is 6 bytes.

**Reply: RESPONSE(5031)** (S/messages/status/SupportedFileTypesStatusMessage.java:22-43)

| Off (payload) | Size | Type | Meaning |
|---|---|---|---|
| 0 | 2 | u16 | origMsgId = 5031 |
| 2 | 1 | u8 | status (0 = ACK; otherwise Gadgetbridge ignores the reply) |
| 3 | 1 | u8 | count |
| 4… | var | repeated | `dataType u8`, `subType u8`, `nameLen u8`, `name[nameLen]` UTF-8 (e.g. "FIT_TYPE_4") |

Gadgetbridge keeps only entries it knows and uses the list purely as a gate. A host-initiated sync is refused if the list is empty (S/GarminSupport.java:611-613, 699-702). proto-lab agrees on the layout and adds that if the watch answers UNSUPPORTED, Garmin Connect falls back to (128,4) and (128,36) (garmin-proto-lab/spec/PROTOCOL.md:621-629).

**SYSTEM_EVENT 5030** (S/messages/SystemEventMessage.java:15-28)

| Off | Size | Type | Meaning |
|---|---|---|---|
| 0 | 1 | u8 | eventType |
| 1 | 1 or var | u8, or length-prefixed string | value. Gadgetbridge always sends u8 0 here. |

eventType values (S/messages/SystemEventMessage.java:30-48): 0 SYNC_COMPLETE, 1 SYNC_FAIL, 2 FACTORY_RESET, 3 PAIR_START, 4 PAIR_COMPLETE, 5 PAIR_FAIL, 6 HOST_DID_ENTER_FOREGROUND, 7 HOST_DID_ENTER_BACKGROUND, 8 SYNC_READY, 9 NEW_DOWNLOAD_AVAILABLE, 10 DEVICE_SOFTWARE_UPDATE, 11 DEVICE_DISCONNECT, 12 TUTORIAL_COMPLETE, 13 SETUP_WIZARD_START, 14 SETUP_WIZARD_COMPLETE, 15 SETUP_WIZARD_SKIPPED, 16 TIME_UPDATED, 17 TRIGGER_REBOOT.

### 2.2 Watch-initiated: SYNCHRONIZATION 5037

Payload (S/messages/SynchronizationMessage.java:38-53):

| Off | Size | Type | Meaning |
|---|---|---|---|
| 0 | 1 | u8 | syncType: 0 MANUAL, 1 AUTOMATIC, 2 TYPE_2 (S/messages/SynchronizationMessage.java:66-70) |
| 1 | 1 | u8 | N = bitmask length in bytes. Gadgetbridge accepts only 4 or 8; anything else returns null, and then nothing is sent, not even an ACK (S/messages/SynchronizationMessage.java:43-50). |
| 2 | N | u32 or u64 LE | category bitmask; bit i = category i |

- proto-lab agrees and allows any N. It names the syncType values 0 manual, 1 invisible, 2 visible-as-needed (garmin-proto-lab/spec/PROTOCOL.md:482-492).
- A syncType greater than 2 makes `fromCode` throw, which turns the message into UnhandledMessage, so no reply is sent (S/messages/SynchronizationMessage.java:72-79).
- **Go note:** Gadgetbridge sign-extends a 4-byte mask (`readInt` into a long, S/messages/SynchronizationMessage.java:46). Zero-extend instead.

Bitmask categories (ordinal = bit; S/messages/SynchronizationMessage.java:82-147):

| Bit | Name | Bit | Name |
|---|---|---|---|
| 0 | SCHEDULES | 20 | SETTINGS_CHANGE |
| 1 | SETTINGS | **21** | **ACTIVITY_SUMMARY** |
| 2 | GOALS | 22 | METRICS_FILE |
| **3** | **WORKOUTS** | 23 | PACE_BAND |
| 4 | COURSES | 24 | SPORT_BACKUP |
| **5** | **ACTIVITIES** | 25 | ULF_CONFIG |
| 6 | PERSONAL_RECORDS | **26** | **SLEEP** |
| 7 | unk_7 | 27 | EXERCISE_BENCHMARK |
| 8 | SOFTWARE_UPDATE | 28 | POWER_GUIDANCE |
| 9 | DEVICE_SETTINGS | 29 | SPORTING_EVENT |
| 10 | LANGUAGE_SETTINGS | 30 | ACTUAL_STEP_RECORDING_CONFIGURATION |
| 11 | USER_PROFILE | 31 | LHA_BACKUP |
| 12 | SPORTS | 32 | BBI_RECORDING_CONFIGURATION |
| 13 | SEGMENT_LEADERS | 33–35 | unk |
| 14 | GOLF_CLUB | 36 | HRV_STATUS |
| 15 | WELLNESS_DEVICE_INFO | 37–38 | unk |
| 16 | WELLNESS_DEVICE_CCF | 39 | UNINSTALL_APP |
| 17 | INSTALL_APP | 40–63 | unk |
| 18 | CHECK_BACK | | |
| 19 | TRUE_UP | | |

proto-lab agrees on 5, 8, 9, 21, 26, 36 and 39 (garmin-proto-lab/spec/PROTOCOL.md:494-498).

**Gadgetbridge behaviour:**
- It always ACKs: `RESPONSE [5037][0]` (S/messages/SynchronizationMessage.java:33).
- It proceeds only if bit 3, 5, 21 or 26 is set, or if syncType is MANUAL (S/messages/SynchronizationMessage.java:55-59). The MANUAL rule was added in 46168abf00. Gadgetbridge ignores, for example, an AUTOMATIC sync that carries only HRV_STATUS or METRICS_FILE.
- To proceed it sends `FILTER(3)` as the followup (S/FileTransferHandler.java:131-139).
- If an upload is in progress, the FILTER is deferred until the upload goes idle. The reason: firmware rejects uploads when other GFDI traffic interleaves (S/FileTransferHandler.java:134-137; commit 88c2f2b7e0).
- **Pitfall:** Gadgetbridge has no "already syncing" guard. A SYNCHRONIZATION that arrives mid-download leads to FILTER, then `initiateDownload()`, which overwrites the in-flight file state (S/FileTransferHandler.java:122-123, 146-149). In Go, ACK it but defer it while busy.

### 2.3 FILTER 5007 (host→watch)

| Off | Size | Type | Meaning |
|---|---|---|---|
| 0 | 1 | u8 | filterType: 0 ALL_FILES_0, 1 DEFAULT, 2 UNK_2, **3 ONLY_NEW_3** (S/messages/FilterMessage.java:30-45) |

- Gadgetbridge always sends 3 (S/FileTransferHandler.java:19, 135). proto-lab names the values 0 no filter, 1 default, 3 pending-uploads-only (garmin-proto-lab/spec/PROTOCOL.md:631-633).
- **Reply RESPONSE(5007):** `[5007 u16][status u8][result u8]` (S/messages/status/FilterStatusMessage.java:14-23).
  - Gadgetbridge ignores `result`. proto-lab says 0 means supported and non-zero means unsupported.
  - If status is not ACK, the parser returns null and Gadgetbridge does nothing further.
  - On an ACK, Gadgetbridge immediately sends `DOWNLOAD_REQUEST` for index 0 (S/FileTransferHandler.java:122-123).

### 2.4 Host-initiated (user presses sync)

`onFetchRecordedData` (S/GarminSupport.java:678-723):
- With the legacy protocol and a non-empty supported-types list, it sends `DOWNLOAD_REQUEST(index 0)` directly. **No FILTER is sent first** (S/GarminSupport.java:718).
- The comment there says the directory request also makes the watch flush data ("otherwise we might get incomplete monitor files", S/GarminSupport.java:716-717).
- The debug-log fetch instead downloads DEVICE_XML at index 0xFFFD (S/GarminSupport.java:679-683; S/FileTransferHandler.java:151-153).

### 2.5 Watch push: FILE_AVAILABLE 5009

Payload (S/messages/FileAvailableMessage.java:39-61). Bytes 0–15 are identical to a directory entry (§4.2):

| Off | Size | Type | Meaning |
|---|---|---|---|
| 0–15 | 16 | struct | directory entry |
| 16 | 1 | u8 | unknown, "seems to be a flag (0/1)". Gadgetbridge does not read it (S/messages/FileAvailableMessage.java:48). |

- Gadgetbridge ACKs it with `RESPONSE [5009][0]` (S/messages/FileAvailableMessage.java:36; S/messages/GFDIMessage.java:115-117), then applies the same selection rules as for directory entries (S/FileTransferHandler.java:124-127).
- Before commit 0bde2e95ed, Gadgetbridge replied UNSUPPORTED.
- proto-lab calls it "File Ready", needs at least 16 bytes, and agrees on the layout (garmin-proto-lab/spec/PROTOCOL.md:500-514).

## 3. DOWNLOAD_REQUEST 5002 and its status

### 3.1 Request (S/messages/DownloadRequestMessage.java:47-59)

| Frame off | Payload off | Size | Type | Field | Gadgetbridge value |
|---|---|---|---|---|---|
| 0 | – | 2 | u16 | frame length = 20 | |
| 2 | – | 2 | u16 | 5002 | |
| 4 | 0 | 2 | u16 | fileIndex (0 = directory) | entry.fileIndex |
| 6 | 2 | 4 | u32 | dataOffset | 0 |
| 10 | 6 | 1 | u8 | requestType: **0 CONTINUE, 1 NEW** (S/messages/DownloadRequestMessage.java:61-64) | 1 |
| 11 | 7 | 2 | u16 | crcSeed | 0 |
| 13 | 9 | 4 | u32 | dataSize | 0 |
| 17 | 13 | 1 | u8 | requestFlags: 0x01 = use compression (S/messages/DownloadRequestMessage.java:28-29) | 0 |
| 18 | – | 2 | u16 | frame CRC | |

- Gadgetbridge always sends NEW with offset 0, seed 0, size 0 and flags 0 (S/FileTransferHandler.java:141-149).
- The flags byte became a parameter in c1c36229ec (#7014); before that it was a hard-coded 0.
- **Agreements:**
  - proto-lab: 14-byte payload. It reads the fields as data_offset, a request/version flag of 1, and two reserved fields written as 0, then compression_requested. Garmin Connect "normally" requests compression for the directory (garmin-proto-lab/spec/PROTOCOL.md:536-561, 633).
  - braycarlson/gfdi omits the last byte and sends a 13-byte payload (gfdi/src/protocol/gfdi.zig:330-341).
  - GarminHRM600 documents 13 bytes too (GarminHRM600/docs/gfdi-filetransfer.md, "Download flow").

### 3.2 Reply: RESPONSE(5002) (S/messages/status/DownloadRequestStatusMessage.java:15-30)

| Frame off | Payload off | Size | Type | Field |
|---|---|---|---|---|
| 0 | – | 2 | u16 | len = 14 |
| 2 | – | 2 | u16 | 5000 |
| 4 | 0 | 2 | u16 | origMsgId = 5002 |
| 6 | 2 | 1 | u8 | status (GFDI) |
| 7 | 3 | 1 | u8 | downloadStatus |
| 8 | 4 | 4 | u32 | maxFileSize = number of bytes the watch will send |
| 12 | – | 2 | u16 | CRC |

downloadStatus values (S/messages/status/DownloadRequestStatusMessage.java:40-48):

| Value | Current name (from proto-lab) | Older Gadgetbridge / HRM600 name |
|---|---|---|
| 0 | OK | OK |
| 1 | INDEX_NOT_FOUND | INDEX_UNKNOWN |
| 2 | NOT_READABLE | INDEX_NOT_READABLE |
| 3 | NOT_READY | NO_SPACE_LEFT |
| 4 | INVALID_REQUEST | INVALID |
| 5 | CRC_MISMATCH | NOT_READY |
| 6 | RANGE_EXCEEDS_FILE | CRC_INCORRECT |

The names were changed in 71e2841d8e. The older names look copied from UploadStatus (S/messages/status/UploadRequestStatusMessage.java:34-42). Prefer the current names, which match garmin-proto-lab/spec/PROTOCOL.md:553-561.

**Gadgetbridge behaviour:**
- **OK:** it allocates an in-memory buffer of exactly `maxFileSize` bytes (S/FileTransferHandler.java:216-217, 517-523).
- **Other downloadStatus:** it emits a "download failed" event, drops the file (no retry, no archive) and moves on to the next one (S/FileTransferHandler.java:218-225; S/GarminSupport.java:616-621; commit 2339242c33).
- **GFDI status not ACK:** the parser returns null and **Gadgetbridge stalls** until disconnect (S/messages/status/DownloadRequestStatusMessage.java:18-20).
- proto-lab: Garmin Connect retries NOT_READY once after about 1 s (garmin-proto-lab/spec/PROTOCOL.md:655-657).

## 4. Directory (file index 0)

### 4.1 Retrieval

- The directory is fetched like any other file: `DOWNLOAD_REQUEST(0)`, then the status gives its size, then 5004 chunks follow.
- When the buffer is full, Gadgetbridge checks that `size % 16 == 0` (and throws if not), then walks 16-byte records (S/FileTransferHandler.java:201-211, 252-296).

### 4.2 Entry layout (16 bytes; S/FileTransferHandler.java:266-283)

The Java comments `//2`, `//3`… are end offsets, not start offsets.

| Off | Size | Type | Field |
|---|---|---|---|
| 0 | 2 | u16 | fileIndex: the handle to use in DOWNLOAD_REQUEST and SET_FILE_FLAG |
| 2 | 1 | u8 | dataType (128 = FIT, 255 = other, 8 = XML) |
| 3 | 1 | u8 | subType (FIT file type) |
| 4 | 2 | u16 | fileNumber |
| 6 | 1 | u8 | specificFlags (data-type-specific; unused by Gadgetbridge) |
| 7 | 1 | u8 | fileFlags (bit table below; Gadgetbridge never inspects them) |
| 8 | 4 | u32 | fileSize |
| 12 | 4 | u32 | timestamp: seconds since the Garmin epoch, i.e. Unix minus 631065600 (S/GarminTimeUtils.java:8, 18-20). 0 means "no date" and is stored as null (S/FileTransferHandler.java:276-282). |

- braycarlson uses the same layout (gfdi/src/protocol/gfdi.zig:476-487).
- proto-lab treats bytes 3–5 as a 3-byte "identifier" whose first byte is the subtype, and calls byte 6 RESERVED_6 (garmin-proto-lab/spec/PROTOCOL.md:635-649).
- Background, from ANT-FS (from memory; it is not in these sources): byte 6 is the "data type specific flags".

### 4.3 fileFlags bits (same numbering as SET_FILE_FLAG; S/messages/SetFileFlagsMessage.java:28-37)

| Bit | Name |
|---|---|
| 0x80 | READ |
| 0x40 | WRITE |
| 0x20 | ERASE (was called DELETE before 71e2841d8e) |
| 0x10 | **ARCHIVE** |
| 0x08 | APPEND |
| 0x04 | CRYPTO |
| 0x02, 0x01 | unknown |

proto-lab says Garmin's higher-level sync code only shows records that have READ set and ARCHIVE clear (garmin-proto-lab/spec/PROTOCOL.md:647). Gadgetbridge does not filter on flags at all. It relies on FILTER(3) and on its local "already downloaded" check.

### 4.4 Does the directory start with a 16-byte header? (disagreement)

- **proto-lab:** the directory starts with an opaque 16-byte header, and entries begin at byte 16 (garmin-proto-lab/spec/PROTOCOL.md:635; garmin-proto-lab/src/garmin_proto_lab/filetransfer.py:439-460).
- **Gadgetbridge and braycarlson:** both parse from byte 0.
- **Evidence for the header:**
  - Gadgetbridge's own note `UNKNOWN_1_0(1, 0), // venu 3, fileIndex=4096` (S/FileType.java:56). That is exactly what an ANT-FS-style header (version 0x00, structure length 0x10, time format 1, …) decodes to when read as an entry.
  - Gadgetbridge's guard against an all-zero entry ("infinite loop", S/FileTransferHandler.java:305-308, 589-597; commit e84fdc9882).
  - The INDEX_UNKNOWN stalls seen when "fetch unknown files" is on (commit 2339242c33).
- Gadgetbridge survives because the header record decodes to an unknown or non-pull type and is skipped.
- **Recommendation for Go:** parse every 16-byte record, but only download entries whose (dataType, subType) is in the pull set (§4.6). That rule rejects the header automatically, because its byte 2 is a time-format value of 0, 1 or 2. Log record 0 raw.

### 4.5 Gadgetbridge selection rules (`conditionallyDownload`, S/FileTransferHandler.java:299-324)

1. Drop a null entry, or one that is all zero (index 0, type DIRECTORY or null, everything else 0, no date).
2. Drop it if (dataType, subType) is not in the FILETYPE table. This happens even with "fetch unknown files" on (S/FileTransferHandler.java:285-289, 310-314).
3. Drop it if the type's `pull` flag is false, unless the `fetch_unknown_files` pref is set (default false; A/util/preferences/DevicePrefs.java:75-76).
4. Otherwise, queue it (S/GarminSupport.java:323-342).

If the directory contains no entry with dataType 128, Gadgetbridge switches to the Classic-BT RFCOMM fallback (Edge 520 Plus case; S/FileTransferHandler.java:293-295; S/GarminSupport.java:1073-1087).

### 4.6 FILETYPE table (S/FileType.java:53-219)

- `pull=Y` means downloaded and archived by default.
- The pull set equals the Garmin Connect "GCUploadAgent=Uploadable" list (commit 3dd4a3705d).
- `typeName` for dataType 128 is `FIT_TYPE_<sub>` (S/FileType.java:230-232).
- The Fenix 8 USB folder column comes either from a code comment (**c**, S/FileType.java:22-38, 117) or from a name match (**n**, inference).

| Enum | type/sub | pull | USB folder | Enum | type/sub | pull | USB folder |
|---|---|---|---|---|---|---|---|
| DIRECTORY | 0/0 | (always) | – | CHANGELOG | 128/41 | **Y** | ChngLog (c) |
| UNKNOWN_1_0 | 1/0 | | (dir header?) | FIT_TYPE_42 | 128/42 | | |
| DEVICE_XML | 8/255 | (debug, idx 0xFFFD) | – | FIT_TYPE_43 | 128/43 | | |
| DEVICE_1 | 128/1 | | | METRICS | 128/44 | **Y** | Metrics (c) |
| SETTINGS | 128/2 | | | BAT_SWING | 128/45 | | |
| SPORTS | 128/3 | | | ROSTER | 128/46 | | |
| ACTIVITY | 128/4 | **Y** | Activity (c) | DIVE_PLAN | 128/47 | | DivePlans (n) |
| WORKOUTS | 128/5 | | Workouts (n) | HSA_DATA | 128/48 | | |
| COURSES | 128/6 | | Courses (n) | SLEEP | 128/49 | **Y** | Sleep (c) |
| SCHEDULES | 128/7 | | Schedule? (n) | SOFTWARE | 128/50 | | |
| LOCATION | 128/8 | | Location (n) | CHALLENGE_RESULT | 128/51 | | |
| WEIGHT | 128/9 | **Y** | | USER_BEHAVIOR_LOG | 128/52 | **Y** | |
| TOTALS | 128/10 | | Totals (n) | CHRONO_ROUND | 128/53 | | |
| GOALS | 128/11 | | Goals (n) | CHRONO_SHOT | 128/54 | | |
| MAP | 128/12 | | | CHRONO_SCORECARD | 128/55 | | |
| DEBUG | 128/13 | | | PACE_BANDS | 128/56 | | PaceBands (n) |
| BLOOD_PRESSURE | 128/14 | | | SPORTS_BACKUP | 128/57 | **Y** | |
| MONITOR_A | 128/15 | **Y** | | DEVICE_58 | 128/58 | **Y** | Device (c) |
| FIT_TYPE_16..19 | 128/16–19 | | | MUSCLE_MAP | 128/59 | | |
| SUMMARY | 128/20 | | SUMMARY (n) | RUNNING_TRACK | 128/60 | | RunningTrack (n) |
| GLUCOSE | 128/21 | | | ECG | 128/61 | **Y** | |
| TRACKING_RECORDS | 128/22 | | | BENCHMARK | 128/62 | | Benchmark (n) |
| TRACKING_EVENTS | 128/23 | | | POWER_GUIDANCE | 128/63 | | PowerGuide (n) |
| FIT_TYPE_24 | 128/24 | | | FIT_TYPE_64 | 128/64 | | |
| VECTOR | 128/25 | | | CALENDAR | 128/65 | | Calendar (n) |
| FIT_TYPE_26/27 | 128/26–27 | | | FIT_TYPE_66 | 128/66 | **Y** | |
| MONITOR_DAILY | 128/28 | **Y** | | FIT_TYPE_67 | 128/67 | | |
| RECORDS | 128/29 | | Records (n) | HRV_STATUS | 128/68 | **Y** | HRVStatus (c) |
| ALERT | 128/30 | | | *(69 absent: dropped)* | | | |
| UNKNOWN_31 | 128/31 | | | HSA | 128/70 | **Y** | HSA (c) |
| MONITOR | 128/32 | **Y** | Monitor (c) | COM_ACT | 128/71 | **Y** | ComAct (n) |
| MLT_SPORT | 128/33 | | MltSport (n) | FBT_BACKUP | 128/72 | **Y** | FbtBackup (c) |
| SEGMENTS | 128/34 | | Segments (n) | SKIN_TEMP | 128/73 | **Y** | SkinTemp (n) |
| SEGMENT_LIST | 128/35 | **Y** | Seg_List (n) | FBT_PTD_BACKUP | 128/74 | **Y** | |
| GOLF | 128/36 | | | FIT_TYPE_75/76 | 128/75–76 | | |
| CLUBS | 128/37 | | Clubs (n) | SCHEDULE | 128/77 | **Y** | Schedule? (n) |
| SCORE | 128/38 | **Y** | SCORCRDS (c) | FIT_TYPE_78 | 128/78 | | |
| ADJUSTMENTS | 128/39 | | Adjstmts (n) | SLP_DISR | 128/79 | **Y** | |
| HMD | 128/40 | | HMD (n) | FIT_TYPE_80/81 | 128/80–81 | | |
| | | | | AREA_COURSES | 128/82 | **Y** | |
| | | | | FIT_TYPE_83 | 128/83 | | |
| | | | | *(84 absent: dropped)* | | | |
| | | | | FIT_TYPE_85/86 | 128/85–86 | | |
| | | | | GEAR | 128/87 | | |
| | | | | FIT_TYPE_88..99 | 128/88–99 | | |

Non-FIT types (S/FileType.java:159-168):

| Enum | type/sub | pull |
|---|---|---|
| DOWNLOAD_COURSE | 255/4 | |
| UNKNOWN_255_008 | 255/8 | |
| PRG | 255/17 | |
| UNKNOWN_255_020 | 255/20 | |
| UNKNOWN_255_022 | 255/22 | |
| IQ_ERROR_REPORTS | 255/244 | **Y** |
| ERROR_SHUTDOWN_REPORTS | 255/245 | **Y** |
| GOLF_SCORECARD | 255/246 | **Y** (XML) |
| ULF_LOGS | 255/247 | **Y** |
| KPI | 255/248 | **Y** |

- The name-only entries (S/FileType.java:171-218: ACTIVITY_GCPD, BACKUP_PRIMARY/SUPPLEMENTARY, BLELogs, FitnessHistory, GPSData, RAMDump, WELLNESS_TYPE_1 and all the map/IQ/media names) have type = `Integer.MIN_VALUE`. They can never match a legacy directory entry.
- Apps and Audio on the USB drive are IQ apps and audio prompts, not legacy directory pull types.
- **Summary of what Gadgetbridge downloads by default (dataType/subType):** 128/{4, 9, 15, 28, 32, 35, 38, 41, 44, 49, 52, 57, 58, 61, 66, 68, 70, 71, 72, 73, 74, 77, 79, 82} and 255/{244, 245, 246, 247, 248}. Everything else is skipped.
- **Why pull types Gadgetbridge doesn't parse:** #5824. Watches "start running out of memory" if those files are never synced and archived (commit d085e6ae72).

## 5. File data transfer (5004)

### 5.1 FILE_TRANSFER_DATA, watch→host (S/messages/FileTransferDataMessage.java:31-39)

| Frame off | Payload off | Size | Type | Field |
|---|---|---|---|---|
| 0 | – | 2 | u16 | len = 13 + N |
| 2 | – | 2 | u16 | 5004 (or compact `0x04, 0x80|txid`) |
| 4 | 0 | 1 | u8 | flags (Gadgetbridge ignores it; proto-lab: "transfer marker", byte 0 carries a first-chunk boolean in Garmin Connect's sender) |
| 5 | 1 | 2 | u16 | running CRC16 |
| 7 | 3 | 4 | u32 | dataOffset of this chunk |
| 11 | 7 | N | bytes | file data |
| 11+N | – | 2 | u16 | frame CRC |

proto-lab, braycarlson and HRM600 all agree on this layout (garmin-proto-lab/spec/PROTOCOL.md:563-581; gfdi/src/protocol/gfdi.zig:465-474).

### 5.2 CRC chaining

- The per-file running CRC starts at 0 (S/FileTransferHandler.java:471-475).
- For each chunk: `expected = CRC16(seed = runningCrc, chunk data)`. The value in the chunk must equal `expected`; then `runningCrc = expected` (S/FileTransferHandler.java:529-532).
- So the field is the CRC of **all file bytes from offset 0 to the end of this chunk**. Example: chunk 1 = bytes 00..0F gives CRC 0x170A; chunk 2 = bytes 10..1F with seed 0x170A gives 0xC36A, the same as the CRC over all 32 bytes.
- The CRC is the same algorithm as the frame CRC (S/ChecksumCalculator.java:41-49).

### 5.3 Host ACK per chunk: RESPONSE(5004) (S/messages/status/FileTransferDataStatusMessage.java:49-63; S/messages/FileTransferDataMessage.java:22-27)

| Frame off | Payload off | Size | Type | Field |
|---|---|---|---|---|
| 0 | – | 2 | u16 | len = 14 |
| 2 | – | 2 | u16 | 5000 |
| 4 | 0 | 2 | u16 | 5004 |
| 6 | 2 | 1 | u8 | status = 0 ACK |
| 7 | 3 | 1 | u8 | transferStatus |
| 8 | 4 | 4 | u32 | **next expected offset = dataOffset + N** |
| 12 | – | 2 | u16 | CRC |

- transferStatus values (S/messages/status/FileTransferDataStatusMessage.java:65-72): 0 OK, 1 RESEND, 2 ABORT, 3 CRC_MISMATCH, 4 OFFSET_MISMATCH, 5 SYNC_PAUSED. proto-lab agrees, with 1 named RETRY_LAST and 5 absent (garmin-proto-lab/src/garmin_proto_lab/filetransfer.py:18-23). **Gadgetbridge only ever sends OK.**
- History: Gadgetbridge used to ACK with the chunk's own dataOffset. Some devices then resent the same chunk forever (#5353). btsnoops confirmed that the field must be the next offset (commit bd2bac38f0).

### 5.4 Flow, end of file, errors

- **Flow control is stop-and-wait.** One chunk is ACKed before the next arrives. An Edge 520 Plus resent an un-ACKed chunk every 5 s indefinitely (commit 853bdb4254).
- **End of file:** Gadgetbridge finishes when bytes received equals `maxFileSize` from the DownloadRequestStatus (S/FileTransferHandler.java:195-196). It does not use the directory's fileSize, and there is no explicit EOF flag.
- **Error behaviour in Gadgetbridge.** In every case below the handler throws *before* the ACK is sent, so no ACK goes out. With MLR the exception is only logged (S/communicator/v2/MlrCommunicator.java:162-167).

| Condition | Code | Effect |
|---|---|---|
| chunk with no active download | S/FileTransferHandler.java:191-192 | no ACK |
| `dataOffset` ≠ bytes received | S/FileTransferHandler.java:526-527 | no ACK |
| CRC mismatch | S/FileTransferHandler.java:530-531 | no ACK, data not appended |
| chunk larger than the remaining buffer | `ByteBuffer.put` overflow | no ACK |

- **proto-lab instead** (garmin-proto-lab/src/garmin_proto_lab/filetransfer.py:150-198):
  - re-ACKs OK with the current offset when it sees a duplicate of the previous chunk;
  - otherwise replies OFFSET_MISMATCH(4), CRC_MISMATCH(3) or ABORT(2);
  - aborts after 3 consecutive bad chunks;
  - ACKs a 5004 that arrives with no receiver with abort status 2 (garmin-proto-lab/spec/PROTOCOL.md:659).
- braycarlson does not verify the chunk CRC. On an offset mismatch it ACKs OK with its current offset to resync (gfdi/src/session/session.zig:459-482).

### 5.5 Chunk size

- The watch chooses the chunk size, and Gadgetbridge accepts any length.
- Gadgetbridge advertises its own max packet size as 0xFFFF (`ourMaxPacketSize = -1`, S/messages/DeviceInformationMessage.java:21, 104).
- Gadgetbridge's own sender (upload) uses `maxPacketSize − 13` bytes of data per chunk, where maxPacketSize comes from the watch's DeviceInformation (S/FileTransferHandler.java:539; S/GarminSupport.java:606-610). The 13 is frame overhead: 4 header + 7 prefix + 2 CRC.
- Over RFCOMM, an Edge 520 Plus sent 16 KiB chunks and overflowed the 10,000-byte COBS buffer (S/communicator/CobsCoDec.java:16-18; RFCOMM now uses 64 KiB, S/GarminRfcommListener.java:128).
- **Go:** size frame buffers at 64 KiB or more.

### 5.6 Resume and timeouts

- **Resume:** Gadgetbridge never resumes. The file is held in memory and lost on interruption. It is re-downloaded from 0 next time, because only complete files are written to disk (S/FileTransferHandler.java:228-250).
- The request has fields for resuming (requestType CONTINUE = 0, dataOffset, crcSeed). Their semantics are inferred: CONTINUE, offset = bytes held, seed = CRC of those bytes.
- **Timeouts:** Gadgetbridge has none on this path. The busy state is cleared only on completion or disconnect (S/GarminSupport.java:1191-1198, 290).
- External figures:
  - proto-lab: Garmin Connect's dispatcher timeout is about 45 s (garmin-proto-lab/spec/PROTOCOL.md:211-215).
  - braycarlson: 15 s stall timer, 4 stalls maximum (gfdi/src/session/session.zig:170-171).
  - Edge 520 Plus watch-side retransmit: about 5 s.

## 6. COMPRESSED_FILE_DATA 5054, QUEUED_DOWNLOAD 5027, CANCEL 5022, DELETE 5006

Gadgetbridge implements none of these (IDs only: commit bd8597c267; silent per §0). Everything in this section comes from proto-lab alone.

**5054 payload** (garmin-proto-lab/spec/PROTOCOL.md:595-619; garmin-proto-lab/src/garmin_proto_lab/filetransfer.py:202-245, 296-397):

| Off | Size | Type | Field |
|---|---|---|---|
| 0 | 1 | u8 | packet counter, mod 256, starting at 0 |
| 1 | 1 | u8 | flags: 0x01 first/restart (from proto-lab's builder), 0x02 end of the current compressed stream |
| 2 | 4 | u32 | cumulative decompressed position (checkpoint) |
| 6 | 2 | u16 | running CRC16 of the decompressed output |
| 8 | N | bytes | zlib-wrapped DEFLATE data |

- After the 0x02 flag, a new inflater is started. Output offset and CRC continue across streams.
- The CRC is checked only when the decompressed output reaches the position named in the packet.
- **Host reply data:** `[counter u8][status u8][expected counter u8, only when status = 4]`. Status values: 0 OK, 1 ABORT, 2 INCORRECT_CRC, 3 DECOMPRESS_FAILURE, 4 UNEXPECTED_COUNTER.
- Compression windowBits must be in 9..15.
- 5054 is triggered by `requestFlags = 0x01` in DOWNLOAD_REQUEST. **Don't request it in the MVP.**
- If a compressed read is cancelled by the watch, Garmin Connect retries uncompressed (garmin-proto-lab/spec/PROTOCOL.md:657).

**5027 QUEUED_DOWNLOAD:** `[N u8][N bitset bytes]`, where bit n is download category n. Garmin Connect ACKs it with response data byte 0 (garmin-proto-lab/spec/PROTOCOL.md:445-447). Gadgetbridge ignores it silently.

**5022 CANCEL_FILE_TRANSFER:** cancels the active transfer and is ACKed (garmin-proto-lab/spec/PROTOCOL.md:659). The layout is not documented anywhere.

**5006 DELETE_FILE:** Gadgetbridge never sends it, and no layout is known.

## 7. After download

### 7.1 SET_FILE_FLAG 5008, host→watch (S/messages/SetFileFlagsMessage.java:18-26)

| Frame off | Payload off | Size | Type | Field |
|---|---|---|---|---|
| 0 | – | 2 | u16 | len = 9 |
| 2 | – | 2 | u16 | 5008 |
| 4 | 0 | 2 | u16 | fileIndex |
| 6 | 1 | 1 | u8 | flags bitmask (§4.3). Gadgetbridge sends only ARCHIVE = 0x10. |
| 7 | – | 2 | u16 | CRC |

proto-lab agrees: `index u16 LE | 0x10` (garmin-proto-lab/spec/PROTOCOL.md:651-653).

### 7.2 Reply: RESPONSE(5008) (S/messages/status/SetFileFlagsStatusMessage.java:21-40)

`[5008 u16][status u8][flagsStatus u8: 0 APPLIED, 1 ERROR(guessed)][fileIndex u16][flags u8]`, so the frame is 13 bytes.
- Gadgetbridge adds 1 to the returned index ("TODO: check if always or only on archival", S/messages/status/SetFileFlagsStatusMessage.java:29). This suggests the watch echoes index − 1. Unverified.
- Gadgetbridge only logs this reply. It does not wait for it before sending the next request.

### 7.3 When Gadgetbridge archives

- After a successful save, unless `keep_activity_data_on_device` is true (default false, S/GarminSupport.java:673-675), and never for DEVICE_XML (S/GarminSupport.java:639-641).
- Also for queued entries that are already on disk, so the watch stops listing them (S/GarminSupport.java:1238-1243).
- **Wire-order artefact:** the save, and with it the SET_FILE_FLAG, runs inside the handler for the last chunk. The order on the wire is therefore: SET_FILE_FLAG(N), then the ACK for the last chunk of N, then DOWNLOAD_REQUEST(N+1) (S/GarminSupport.java:495-532).
- **Recommended for Go:** ACK, then SET_FILE_FLAG, then the next request.
- **What ARCHIVE means** is inferred:
  - Gadgetbridge's comments call it "delete file from watch".
  - Garmin Connect advertises an "Explicit Archive 29" capability (garmin-proto-lab/spec/PROTOCOL.md:439-443).
  - Reading: archived files drop out of the ONLY_NEW (3) listing and become reclaimable by the watch.

### 7.4 FILE_AVAILABLE

FILE_AVAILABLE is the watch announcing a single new file (§2.5). It is handled exactly like a directory entry. Duplicates against the directory listing are harmless, because the on-disk check runs when an entry is dequeued.

### 7.5 FIT post-processing in Gadgetbridge

- Saved FIT files are recorded in a "pending files" DB table (S/GarminSupport.java:628-637).
- When the queue drains, `FitAsyncProcessor` imports them and removes each one from the table only on success (S/GarminSupport.java:1271-1313; S/fit/FitAsyncProcessor.java:58-72).
- `FitImporter` dispatches on the FIT `file_id.type`, not on the directory subtype: ACTIVITY becomes a workout, MONITOR becomes activity samples, SLEEP becomes sleep stages; METRICS, DEVICE_58 and HRV_STATUS only feed the generic sample families (S/fit/FitImporter.java:562-600).
- Non-FIT files are just stored.

## 8. Bookkeeping

- **Storage path.** The base directory is per device (`getWritableExportDirectory`, S/GarminSupport.java:1732-1734). Within it, the file goes to `<TYPE>/<yyyy>/<TYPE>_<yyyy-MM-dd_HH-mm-ss>_<fileIndex>.<fit|bin>`.
  - Without a date: `<TYPE>/<TYPE>_<fileIndex>.<ext>`.
  - TYPE is the enum name, e.g. `ACTIVITY/2026/ACTIVITY_…_123.fit`. The extension is fit when dataType = 128 (S/FileTransferHandler.java:621-625; S/GarminUtils.java:26-33, 52-69; S/FileType.java:280-282).
  - The timestamp is formatted in the **phone's local zone**. **Go:** use UTC.
  - mtime is set to the entry's date (S/FileTransferHandler.java:237-240).
- **`getFileName` vs `getLegacyFileName`.** `getFileName()` is the basename of the output path (S/FileTransferHandler.java:628-631). `getLegacyFileName()` was removed in cb1d0c2eb0, which introduced the per-type/year folders. Its pattern `<TYPE>_<index>_<timestamp>.<ext>` now lives inline in `alreadyDownloaded`.
- **Dedupe (`alreadyDownloaded`, S/GarminSupport.java:1673-1717).** An entry is skipped if any of these exists and is non-empty:
  1. the current path;
  2. the basename at the export root;
  3. the legacy-2 name at the export root.
  - A zero-length file is re-downloaded (commit 8be679d1fd).
  - The timestamp is part of the key because the watch **reuses file indices** (commit 42e44de1ac).
- **Ordering and concurrency:**
  - The queue is FIFO in directory order, with no sorting (S/GarminSupport.java:203, 257, 336).
  - One download at a time, gated on `currentlyDownloading` (S/GarminSupport.java:1210, 1234-1267), with a single slot in the handler (S/FileTransferHandler.java:179-188).
  - The queue is pumped after every incoming message (S/GarminSupport.java:532).
- **Error handling:** no retries anywhere.
  - A save failure (IOException) leaves `GarminSupport.currentlyDownloading` set, and the sync stalls until reconnect (S/FileTransferHandler.java:241-244).
- **End of sync:** `SYSTEM_EVENT SYNC_COMPLETE (0,0)` is sent after local FIT processing (S/GarminSupport.java:1327-1334).
  - Caveat: it is sent only if at least one entry was queued (`isBusyFetching`, S/GarminSupport.java:1216-1219, 1271). An empty sync never sends SYNC_COMPLETE.

## 9. Newer-device workarounds and rule history

| Rule | Why | Ref |
|---|---|---|
| New sync by default on Venu X1/4, VA6, CIRQA; **not Fenix 8** | #5021: legacy misses many files on those devices | D/GarminCoordinator.java:124-134; d3a5216875, 20e7f94dec |
| Directory request also used as a "flush" | otherwise monitor files may be incomplete; can freeze some firmware in new-sync mode (#6700) | S/GarminSupport.java:689-697, 716-718; 9daafa4061 |
| Pull all GC-uploadable types | #5824: watch memory exhaustion | d085e6ae72, 3dd4a3705d, b91628cca3 |
| All-zero entry ignored | infinite loop | e84fdc9882 |
| ACK carries the next offset | #5353: chunk resent forever | bd2bac38f0 |
| Download failure moves on to the next file | sync got stuck on INDEX_UNKNOWN | 2339242c33 |
| FILTER deferred during upload | upload rejected if traffic interleaves | 88c2f2b7e0 |
| RFCOMM fallback when no FIT entries | Edge 520 Plus lists files only on RFCOMM | 1ea63a9e69, 853bdb4254 |
| FILE_AVAILABLE ACK and download | previously replied UNSUPPORTED | 0bde2e95ed |

- **PendingHD and "Position Enhancement":** nothing anywhere in Gadgetbridge at eb3550358d mentions them (grep of A/ is empty). Not covered here.
- **braycarlson's index sweep.** After reading the directory, braycarlson/gfdi requests every index from 1 to 1024 and stops after 16 consecutive refusals. It never archives (gfdi/src/session/session.zig:166-171, 568-596). This brute-force fallback hints that the legacy directory can be incomplete. Which watch it was used against is unknown.

## 10. One complete sync, watch-initiated

These are plaintext GFDI frames before COBS, encryption or MLR. "H" is the host, "W" the watch.

1. **Once per connection.**
   - H→W `SUPPORTED_FILE_TYPES_REQUEST`: `06 00 A7 13 3B 75`.
   - W→H `RESPONSE(5031)` with the type list.
   - H→W `SYNC_READY`: `08 00 A6 13 08 00 D5 C5`.
2. W→H `SYNCHRONIZATION` (`type, N, mask`).
3. H→W ACK: `09 00 88 13 AD 13 00 E1 4A`.
4. If it should proceed and no upload is in flight, H→W `FILTER(3)`: `07 00 8F 13 03 C9 1A`.
5. W→H `RESPONSE(5007)`: `[8F 13][00][result]`.
6. H→W `DOWNLOAD_REQUEST(0)`: `14 00 8A 13 00 00 00 00 00 00 01 00 00 00 00 00 00 00 30 32`.
7. W→H `RESPONSE(5002)`: `[8A 13][00][00][size u32]`.
8. **Repeat until received = size.**
   - W→H `5004 [flags][crc][off][data]`.
   - H checks the offset and the CRC chain, appends the data, then sends `RESPONSE(5004, 0, 0, off+N)`. Example for next offset 0x1F0: `0E 00 88 13 8C 13 00 00 F0 01 00 00 FE D1`.
9. Parse the directory and queue the entries that pass §4.5 (plus the READ and ARCHIVE rule from checklist item 8).
10. **For each queued entry, in order:**
    - If it is already on disk and non-empty: send SET_FILE_FLAG (unless keep) and skip it.
    - Otherwise: `DOWNLOAD_REQUEST(idx)`, e.g. idx 0x0123 is `14 00 8A 13 23 01 00 00 00 00 01 00 00 00 00 00 00 00 10 50`. Then status and chunks as in steps 7–8.
    - On completion: save, then `SET_FILE_FLAG(idx, 0x10)`, e.g. `09 00 90 13 23 01 10 AC 0F` (recommended order: ACK the last chunk first), then the next entry.
11. W→H `RESPONSE(5008)` (fire-and-forget).
12. Queue drained: import, then H→W `SYNC_COMPLETE`: `08 00 A6 13 00 00 D2 05`.

**Variants:**
- *Host-initiated:* start at step 6. Gadgetbridge skips FILTER here; sending FILTER(3) first is recommended.
- *FILE_AVAILABLE:* the host ACKs (`09 00 88 13 91 13 00 21 46`) and runs step 10 for that one entry.

The test-vector frames above were computed by the research agent with a throwaway script. Re-derive
them in Go unit tests from the CRC implementation, not by copying.

## 11. Minimum viable legacy download checklist

1. GFDI frame codec, CRC-16/ARC (`123456789` gives `0xBB3D`), and compact-ID decoding. Buffers of 64 KiB or more.
2. After the session is up, send 5031 and `SYNC_READY (8,0)`. ACK every 5037 and 5009.
3. Start a sync on SYNCHRONIZATION (bit 3, 5, 21, 26, or MANUAL; sensible to accept any bit), on user request, or on FILE_AVAILABLE. Ignore or defer triggers while busy.
4. Send `FILTER(3)` and wait for `RESPONSE(5007)`. Then send `DOWNLOAD_REQUEST(0, NEW, offset 0, seed 0, size 0, flags 0)`. The flags byte is the 14th payload byte.
5. On `RESPONSE(5002)`, proceed only if status = 0 and downloadStatus = 0.
   - Use `maxFileSize` as the exact byte count.
   - On NOT_READY (3), retry once after about 1 s.
   - On any other failure, skip the file. A size of 0 means nothing to download.
6. For each 5004:
   - Require `off == received`. If it is a duplicate of the previous offset, re-ACK OK with `received`.
   - Check `crc == CRC16(seed=running, data)`.
   - Write the data to a `.part` file and ACK `[5004][0][0][received]`.
   - On a mismatch, send OFFSET_MISMATCH (4) or CRC_MISMATCH (3). After 3 bad chunks, or about 15–45 s without a chunk, abandon the file.
7. The directory is complete when received = maxFileSize. Require `len % 16 == 0` and parse the 16-byte records.
8. Select entries with dataType/subType in the §4.6 pull set. Recommended: also require READ (0x80) set and ARCHIVE (0x10) clear. Drop all-zero records. Log record 0 raw (possible header).
9. Download one file at a time in FIFO order. Dedupe on disk by (type, fileIndex, timestamp in UTC). Skip non-empty existing files, but still archive them.
10. After an atomic rename to the final name: ACK the last chunk, then (unless keep-on-device) `SET_FILE_FLAG [idx][0x10]`. Never archive DEVICE_XML (0xFFFD).
11. When the queue is empty, send `SYSTEM_EVENT SYNC_COMPLETE (0,0)`, even if nothing was downloaded.
12. Leave 5054 alone: never set requestFlags bit 0. Tolerate unknown IDs (5027, 5022) without crashing.
13. Tests: derive every directory timestamp from `time.Now()` converted to the Garmin epoch (Unix − 631065600). Never use literal dates (project rule).

## 12. Open questions / uncertain

1. Does the directory really start with a 16-byte header (proto-lab), or is it entries from byte 0 (Gadgetbridge, braycarlson)? Needs a Fenix 8 hexdump of index 0.
2. downloadStatus values 3–6: current names vs older Gadgetbridge/HRM600 names.
3. Meaning of the 5004 `flags` byte; meaning of byte 6 (specificFlags/RESERVED_6); meaning of the trailing byte 16 in FILE_AVAILABLE.
4. Is the SET_FILE_FLAG reply's index really one less than the one sent (Gadgetbridge's +1)?
5. Without FILTER (Gadgetbridge's host-initiated path), does the listing include archived files? Is the filter sticky per connection?
6. Does the watch delete ARCHIVE'd files, or only hide them? What does ERASE (0x20) do?
7. CONTINUE/resume semantics (dataOffset + crcSeed) and the meaning of `dataSize`. Is the 14th flags byte optional (braycarlson sends 13 bytes)?
8. Zero-length files: does the watch send any 5004 chunk? Gadgetbridge would wait forever.
9. Fenix 8 chunk size over MLR and its retransmit timer (about 5 s on Edge 520 Plus); exact timeouts.
10. 5054 details (proto-lab only); 5027 semantics; 5022 layout; whether DELETE_FILE (5006) works at all.
11. Bitmask lengths other than 4 or 8 (Gadgetbridge drops them without an ACK).
12. Timestamps below 0x10000000 may be "system time" rather than a date (ANT-FS convention, not in Gadgetbridge).
13. Is the Fenix 8 legacy directory complete (a #5021-like risk)? Compare a BLE directory dump against the USB `GARMIN/` tree; braycarlson's index sweep is a fallback.
14. Nothing in Gadgetbridge covers PendingHD or "Position Enhancement" files.
15. The folder mappings marked (n) are name-based guesses; only the (c) ones are backed by Gadgetbridge code comments.
