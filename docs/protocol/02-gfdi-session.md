# 02 — GFDI message layer and session lifecycle

Part of the goarmin protocol notes (`docs/protocol/`). Derived from Gadgetbridge at commit
`eb3550358d` (Codeberg), cross-checked against garmin-proto-lab, garmin-ble and garmin-bridge.
Written 2026-11-07. Describes the protocol, not Gadgetbridge's code; goarmin is a from-scratch
implementation. Example frames were computed by the research agent with the CRC algorithm (checked
against a Gadgetbridge test vector and the standard check value); re-derive them in Go unit tests
rather than copying them.

Three findings that correct earlier assumptions:
- **The CRC-16 belongs to the GFDI layer, not the BLE transport.** It is the last 2 bytes of every decoded GFDI frame, and COBS wraps the whole frame including the CRC. Don't implement it twice.
- **AUTH_NEGOTIATION is not answered with zero flags.** Since commit cd46e24819 Gadgetbridge echoes the watch's byte 0 and its flag mask back.
- **CURRENT_TIME_REQUEST has no DST-offset field.** Gadgetbridge computes one and never writes it. Its two transition fields also look swapped whenever the next transition is a DST start (i.e. outside DST), compared with the recovered Garmin Connect logic.

## 0. Conventions

- **Path abbreviations:**
  - `S/` = `app/src/main/java/nodomain/freeyourgadget/gadgetbridge/service/devices/garmin/`
  - `C/` = `app/src/main/java/nodomain/freeyourgadget/gadgetbridge/devices/garmin/`
  - Both are inside `~/go/src/codeberg.org/Freeyourgadget/Gadgetbridge`.
- **Cross-check sources:**
  - **PL** = `~/go/src/github.com/seofernando25/garmin-proto-lab` (`spec/PROTOCOL.md`, `src/garmin_proto_lab/*`).
  - **GBLE** = `~/go/src/github.com/gwerneckp/garmin-ble`.
  - **GBR** = `~/go/src/github.com/wh1le/garmin-bridge` (tested on a Fenix 6 Pro).
- **Directions:** W→H is watch to host; H→W is host to watch.
- **Integers:** little-endian unless stated otherwise.
- **Offsets:** "payload offset" is counted from the first byte after the 4-byte GFDI header. Frame offset = payload offset + 4.
- **Byte notation:** a value written `5024 (A0 13)` is shown as it appears on the wire.

## 1. Layer boundary

- **One COBS frame carries one complete GFDI message.** The transport COBS-decodes notification bytes and hands each complete frame to `GarminSupport.onMessage` (`S/communicator/v2/CommunicatorV2.java:498-502`; V1 behaves the same at `S/communicator/v1/CommunicatorV1.java:109`).
- **On send,** the complete GFDI frame (length, ID, payload and CRC) is COBS-encoded, then prefixed with the ML handle byte and fragmented (`S/communicator/v2/CommunicatorV2.java:161-190`).
- **The CRC-16 is part of the GFDI frame,** not of the transport (`S/messages/GFDIMessage.java:130-133, 269-276`).
- **The receiver enforces "declared length == frame length".** Bytes after a GFDI message inside one COBS frame are therefore illegal (`S/messages/GFDIMessage.java:262-267`).

## 2. GFDI frame

### 2.1 Ordinary header

| Frame offset | Size | Type | Meaning |
|---|---|---|---|
| 0 | 2 | u16 LE | Total frame length. Includes this field, the ID, the payload and the 2 CRC bytes (`S/messages/GFDIMessage.java:131` writes position+2; reader comment "includes CRC" at `:251`). Minimum 6. |
| 2 | 2 | u16 LE | Message ID (5000…5112, see the enum at `S/messages/GFDIMessage.java:135-202`) |
| 4 | N | bytes | Payload |
| 4+N | 2 | u16 LE | CRC-16 over frame bytes [0, 4+N): length field, ID and payload (`S/messages/GFDIMessage.java:132, 270-271`) |

### 2.2 Compact (transaction) header, received only

- **Detection:** if bit 15 of the u16 at frame offset 2 is set, the frame uses the compact form.
- **Normalisation:** Gadgetbridge computes `id = (raw & 0xFF) + 5000` and discards the high byte (`S/messages/GFDIMessage.java:46-51`, commit c2710c32ab "Should fix #4522").
  - Byte 2 = ID − 5000.
  - Byte 3 = 0x80 | sequence number.
  - Gadgetbridge's comment treats the sequence as 7 bits, `(raw>>8)&0x7F`.
  - PL says it is 5 bits, values 0..31 (PL `spec/PROTOCOL.md:165-178`; `src/garmin_proto_lab/frame.py:81-88`).
- **Gadgetbridge never sends compact frames.** Its replies always use the ordinary 16-bit ID and the normalised original ID.
- **This disagrees with PL.** PL says that in transaction mode a 5000 response carries the matching transaction ID in its compact header (PL `spec/PROTOCOL.md:196`).
- The 0x8000 normalisation is also in GBR (`src/protocol/message.py:72-73`).

### 2.3 CRC

- **Algorithm:** CRC-16/ARC (reflected, polynomial 0xA001, init 0, no final XOR). Gadgetbridge implements it as a nibble table (`S/ChecksumCalculator.java:22-25, 41-48`), processing the low nibble then the high nibble of each byte.
- **Check value:** "123456789" → 0xBB3D. This agrees with PL (`spec/PROTOCOL.md:180-184`).
- **Verified vector:** the Gadgetbridge test frame `2E00941302CA…0101 97F3` (`app/src/test/java/nodomain/freeyourgadget/gadgetbridge/service/devices/garmin/GarminSupportTest.java:120`) has a computed CRC of 0xF397, matching its last two bytes `97 F3`. Its length field is 0x2E = 46 = total size, and its ID is 0x1394 = 5012.

### 2.4 Receive validation in Gadgetbridge

- **Length mismatch or bad CRC:** the `MessageReader` constructor throws an `IllegalArgumentException` (`S/messages/GFDIMessage.java:248-255, 262-276`). The constructor runs outside the try block (`:43`), so the message is dropped and no `CRC_ERROR` or `LENGTH_ERROR` status is ever sent.
- **Exception while parsing a payload:** the message becomes an `UnhandledMessage` and gets no reply (`S/messages/GFDIMessage.java:87-89`).
- **Leftover payload bytes:** only logged (`S/messages/GFDIMessage.java:278-291`).

### 2.5 Primitive encodings

| Primitive | Encoding | Source |
|---|---|---|
| u8 | 1 byte, read as unsigned | `S/GarminByteBufferReader.java:26` |
| u16 | LE, read as unsigned | `S/GarminByteBufferReader.java:42`; LE set at `S/messages/GFDIMessage.java:250`, `S/messages/MessageWriter.java:18,24` |
| u32/i32 | LE. Java reads it as signed `int`; use `uint32` for Garmin timestamps, unit IDs and lengths, and `int32` for UTC offsets | `S/GarminByteBufferReader.java:46` |
| u64 | LE | `S/GarminByteBufferReader.java:50` |
| string (default) | u8 byte-length prefix, then UTF-8 bytes. No NUL terminator. Max 255 bytes, longer throws. | writer `S/messages/MessageWriter.java:55-62`; reader `S/GarminByteBufferReader.java:63-68` |
| string, variant 2 | u16 length prefix (only in CREATE_FILE) | `S/GarminByteBufferReader.java:71-80`; `S/messages/CreateFileMessage.java:43` |
| string, variant 3 | NUL-terminated (only in NOTIFICATION_CONTROL) | `S/GarminByteBufferReader.java:82-95`; `S/messages/NotificationControlMessage.java:91,94` |
| boolean | 1 byte, 0 or 1; readers test `== 1` | `S/messages/NotificationSubscriptionMessage.java:24`; `S/messages/DeviceInformationMessage.java:66` |
| bitmask sets | bit n of byte k means ordinal 8k+n (LSB first) | `C/GarminCapability.java:204-220, 238-254` |

### 2.6 Size limits

- **u16 length:** the theoretical maximum frame is 65535 bytes.
- **Watch limit:** the watch's `max_packet_size` from DEVICE_INFORMATION is a whole-frame size, length and CRC included. Evidence:
  - Protobuf chunk = max − 20, i.e. 18 header bytes plus 2 CRC bytes (`S/ProtocolBufferHandler.java:684-688`).
  - File-data chunk = max − 13 (`S/FileTransferHandler.java:539`).
  - PL describes it as "payload limit − 14" (`spec/PROTOCOL.md:802`), which is consistent.
- **Defaults before DEVICE_INFORMATION arrives:**
  - 400 (`S/GarminSupport.java:222`).
  - 375 for file transfer (`S/FileTransferHandler.java:64`) and for protobuf chunks (`S/ProtocolBufferHandler.java:76`).
- **Gadgetbridge buffers:** outgoing frame buffer 10 KiB (`S/messages/GFDIMessage.java:38`); `MessageWriter` default 16 KiB (`S/messages/MessageWriter.java:9`).
- **Other caps:** CONFIGURATION payload ≤ 255 bytes (`S/messages/ConfigurationMessage.java:33`); settings count ≤ 255 (`S/messages/SetDeviceSettingsMessage.java:13`).

## 3. RESPONSE (5000)

### 3.1 Layout

| Payload offset | Size | Type | Meaning |
|---|---|---|---|
| 0 | 2 | u16 | Original message ID, normalised full ID (`S/messages/status/GenericStatusMessage.java:34`) |
| 2 | 1 | u8 | Status (§3.2) |
| 3… | var | | Response-specific data (§3.3) |

On receive, the original ID is read raw as a u16, without compact normalisation (`S/messages/status/GFDIStatusMessage.java:10`).

### 3.2 Status enum

The values are enum ordinals (`S/messages/GFDIMessage.java:225-242`). They agree with PL (`spec/PROTOCOL.md:198-207`) and GBR.

| Value | Gadgetbridge name | PL name |
|---|---|---|
| 0 | ACK | ACK |
| 1 | NAK | NAK |
| 2 | UNSUPPORTED | UNKNOWN_OR_NOT_SUPPORTED |
| 3 | DECODE_ERROR | COBS_DECODER_ERROR |
| 4 | CRC_ERROR | CRC_ERROR |
| 5 | LENGTH_ERROR | LENGTH_ERROR |

- **Gadgetbridge's parser throws on values above 5** (`:239`).
- **PL says unknown status values must be kept, not remapped.**

### 3.3 Response variants Gadgetbridge parses (W→H)

Dispatch on the original ID is at `S/messages/status/GFDIStatusMessage.java:13-52`:

- **5043/5044:** protobuf status (§5.13).
- **5031:** supported file types (§5.8).
- **5007:** filter status (§5.10).
- **5101:** auth status (§5.3).
- **5008:** set-file-flags status (`S/messages/status/SetFileFlagsStatusMessage.java:21-40`).
- **File transfer, FIT and notification messages:** their own status classes.
- **Anything else:** treated as a bare status byte.

### 3.4 ACK rules as implemented by Gadgetbridge

**Processing order for each inbound message** (`S/GarminSupport.java:461-534`, comment at `:488-493`):
1. Run the handlers; they may produce a follow-up message.
2. Send the message's status/ACK (`:503`).
3. Send its "reply", from its `generateOutgoing` (`:512`).
4. Send the follow-up (`:514`).
5. Evaluate device events (`:516-530`).
6. Process the download queue (`:532`).

**Timing:** everything happens synchronously in the receive callback. There are no GFDI-level timers, retransmissions or ACK waits. Gadgetbridge sends several host messages back-to-back without waiting for their RESPONSEs; only FILTER→directory, protobuf chunking and FIT definition→data are gated on a status. PL reports that Garmin Connect's dispatcher times requests out after about 45 s (`spec/PROTOCOL.md:211-213`). The watch-side timeout is unknown.

| Incoming (W→H) | What Gadgetbridge sends back | Source |
|---|---|---|
| RESPONSE 5000 | nothing ("don't ack the ack") | `S/messages/status/GFDIStatusMessage.java:48-51`; commit d0b525f420 |
| DEVICE_INFORMATION 5024 | **two** RESPONSEs: a bare ACK, then the full identity reply (§5.1) | `S/messages/DeviceInformationMessage.java:46, 78, 92-120` |
| CONFIGURATION 5050 | bare ACK, then a host CONFIGURATION message (not a RESPONSE) | `S/messages/ConfigurationMessage.java:39, 53-60` |
| AUTH_NEGOTIATION 5101 | one RESPONSE carrying auth data | `S/messages/AuthNegotiationMessage.java:21, 43` |
| CURRENT_TIME_REQUEST 5052 | one RESPONSE carrying time data, no separate ACK; UNSUPPORTED if time sync is disabled in prefs | `S/messages/CurrentTimeRequestMessage.java:18, 81-91`; `S/GarminSupport.java:481-485` |
| NOTIFICATION_SUBSCRIPTION 5036 | one RESPONSE with notification status, sent from the event handler | `S/messages/NotificationSubscriptionMessage.java:19-20`; `S/GarminSupport.java:587-605` |
| MUSIC_CONTROL_CAPABILITIES 5042 | one RESPONSE carrying the command list | `S/messages/MusicControlCapabilitiesMessage.java:11, 22-36` |
| SYNCHRONIZATION 5037 | bare ACK, plus a FILTER follow-up if the sync should proceed | `S/messages/SynchronizationMessage.java:33`; `S/FileTransferHandler.java:131-139` |
| BATTERY_STATUS 5023, FILE_AVAILABLE 5009, WEATHER_REQUEST 5014, FIND_MY_PHONE_REQUEST/CANCEL 5039/5040, MUSIC_CONTROL 5041, CREATE_FILE 5005 | bare ACK | `S/messages/BatteryStatusMessage.java:27`; `FileAvailableMessage.java:36`; `WeatherRequestMessage.java:15`; `FindMyPhoneRequestMessage.java:16`; `FindMyPhoneCancelMessage.java:14`; `MusicControlMessage.java:36`; `CreateFileMessage.java:17` (all under `S/messages/`) |
| PROTOBUF_REQUEST/RESPONSE 5043/5044 | bare ACK if the message is complete and Gadgetbridge returns a protobuf reply; otherwise the 8-byte protobuf status | `S/messages/ProtobufMessage.java:29-33`; `S/ProtocolBufferHandler.java:119-136, 276-295` |
| FIT_DEFINITION 5011 / FIT_DATA 5012 | RESPONSE plus a 1-byte code (0 = APPLIED) | `S/messages/FitDefinitionMessage.java:21`; `S/messages/FitDataMessage.java:22`; `S/messages/status/FitDefinitionStatusMessage.java:43-55` |
| MUSIC_CONTROL_ENTITY_UPDATE 5049 | nothing (no status set) | `S/messages/MusicControlEntityUpdateMessage.java:18` |
| Known ID without a parser: SYSTEM_EVENT 5030, QUEUED_DOWNLOAD 5027, DEVICE_SETTINGS, FILTER, CANCEL_FILE_TRANSFER, 5054, 5102-5112, … | **nothing** (an UNSUPPORTED status is built with send=false) | `S/messages/GFDIMessage.java:80-83`; `S/messages/UnhandledMessage.java:15-19` |
| ID not in the enum | **nothing** | `S/messages/GFDIMessage.java:53-55`; `S/messages/UnhandledMessage.java:9-13`; `S/messages/status/GenericStatusMessage.java:23-27` |

**Disagreements:**
- PL says unhandled requests "can receive UNKNOWN_OR_NOT_SUPPORTED" (`spec/PROTOCOL.md:213`).
- GBLE ACKs every non-RESPONSE message with status 0 (`garmin_ble/watch.py:711-714`).
- GBR replies UNSUPPORTED to protobuf requests (`src/protocol/handshake.py:85-92`).

## 4. Session start sequence, from "GFDI handle registered" to "ready to sync"

**Transport prologue** (documented in the transport notes, listed for context):
- MTU request 515 (`S/GarminSupport.java:373-375`).
- CLOSE_ALL (`S/communicator/v2/CommunicatorV2.java:132-134`).
- On CLOSE_ALL_RESP, register GFDI (`:471-483`).
- On REGISTER_ML_RESP, install `GfdiCallback` (`:375-377`). Its `onConnect` is a no-op (`:800-803`), so **the host sends nothing at GFDI level and waits for the watch.**

| # | Dir | Message | Notes / source |
|---|---|---|---|
| 1 | W→H | DEVICE_INFORMATION 5024 | Sent unprompted (GBLE `garmin_ble/protocol/gfdi.py:197-198`). Parsed at `S/messages/DeviceInformationMessage.java:54-81`. |
| 2 | H→W | RESPONSE(5024, ACK), bare | Gadgetbridge quirk (§3.4) |
| 3 | H→W | RESPONSE(5024, ACK, host identity) | §5.1. Host stores `max_packet_size` (`S/GarminSupport.java:606-610`). |
| 4 | W→H | CONFIGURATION 5050 (watch capabilities) | `S/messages/ConfigurationMessage.java:42-45` |
| 5 | H→W | RESPONSE(5050, ACK) | |
| 6 | H→W | CONFIGURATION 5050 (host capabilities) | §5.2 |
| 7 | — | `CapabilitiesDeviceEvent` triggers `completeInitialization()` | `S/GarminSupport.java:561-563, 1097-1127`. Since a87ddbb66a, older devices with no CONFIGURATION can trigger it from FIT capabilities (`S/FitLocalMessageHandler.java:92-98`). |
| 7a | H→W | SUPPORTED_FILE_TYPES_REQUEST 5031 | `S/GarminSupport.java:1103` |
| 7b | H→W | DEVICE_SETTINGS 5026 | `:1104, 1354-1360` |
| 7c | H→W | SYSTEM_EVENT TIME_UPDATED (16) | Only if both the global and device time-sync prefs are on, which is the default (`:1106-1108, 1363-1365`) |
| 7d | H→W | SYSTEM_EVENT SYNC_READY (8) | "needed for vivomove style" (`:1109-1110`) |
| 7e | H→W | PROTOBUF_REQUEST: DeviceStatus RemoteDeviceBatteryStatusRequest | `:1112, 1343-1352` |
| 7f | H→W | PROTOBUF_REQUEST: InstalledApps GetInstalledAppsRequest (WATCH_APP) | `:1113, 748-756` |
| 7g | — | Device state set to INITIALIZED (= "ready") | `:1116` |
| 7h | H→W | **First connect only:** SYSTEM_EVENT PAIR_COMPLETE (4), SYNC_COMPLETE (0), SETUP_WIZARD_COMPLETE (14) | `:1119-1126`; flag set by `connectFirstTime()` (`:1786-1789`); commit 8bd666ec89 |
| 7i | H→W | Only if the watch has REALTIME_SETTINGS: PROTOBUF_REQUEST RealtimeSettings InitRequest | `:564-578` |
| 7j | — | Watch capability names persisted to prefs | `C/GarminCapability.java:272-275` |
| 8 | W→H | A RESPONSE for each of 6 and 7a–7i | Gadgetbridge only consumes the 5031 response, storing the file-type list (`S/GarminSupport.java:611-613`). |
| 9 | W→H | Unordered, any time: AUTH_NEGOTIATION 5101, CURRENT_TIME_REQUEST 5052, NOTIFICATION_SUBSCRIPTION 5036, PROTOBUF_REQUEST (e.g. Core ConnectionReadyNotification), BATTERY_STATUS 5023, SYNCHRONIZATION 5037, WEATHER_REQUEST 5014, … | Gadgetbridge enforces no order. The GBR docstring lists the observed order as DEVICE_INFORMATION, CONFIGURATION, AUTH_NEGOTIATION, then optional CURRENT_TIME_REQUEST (`src/protocol/handshake.py:1-8`). |
| 10 | | Sync (§6) | |

- **Re-registration within one connection:** if GFDI is re-registered inside the same BLE connection (`S/communicator/v2/CommunicatorV2.java:456-464`), the second CONFIGURATION does **not** redo step 7. A guard skips it when already INITIALIZED (`S/GarminSupport.java:1098-1102`).
- **Prerequisite for host-initiated legacy fetch:** the supported-file-types list must be non-empty, otherwise Gadgetbridge aborts (`S/GarminSupport.java:699-702`).

## 5. Message reference

### 5.1 DEVICE_INFORMATION 5024

**W→H payload** (`S/messages/DeviceInformationMessage.java:54-75`; agrees with PL `spec/PROTOCOL.md:412-431`):

| Offset | Size | Type | Field |
|---|---|---|---|
| 0 | 2 | u16 | protocol_version |
| 2 | 2 | u16 | product_number (Fenix 8 = 4534/4536, §9) |
| 4 | 4 | u32 | unit_number. Gadgetbridge renders it as unsigned decimal (`:57`). |
| 8 | 2 | u16 | software_version. Shown as `v/100 "." %02d v%100` (`:149-153`). |
| 10 | 2 | u16 | max_packet_size (whole frame, §2.6) |
| 12 | 1+n | lpstr | bluetooth_friendly_name |
| … | 1+n | lpstr | device_name |
| … | 1+n | lpstr | device_model |
| … | 1 | u8, optional | has_mac (dual-pairing flag); 1 means the next 12 bytes follow |
| … | 6 | bytes | BLE MAC, little-endian (reverse for display) |
| … | 6 | bytes | Classic BT MAC, little-endian. Gadgetbridge formats it upper-case `XX:XX:…` (`:70`). |
| … | 1 | u8, optional | unknown (PL: "limitation byte") |

- The optional tail was added in 13edca454d.
- PL notes that products 2787, 3192 and 3307 skip the extended section (`spec/PROTOCOL.md:429`).

**H→W reply** (RESPONSE) (`S/messages/DeviceInformationMessage.java:19-25, 92-120`):

| Offset | Size | Type | Gadgetbridge value |
|---|---|---|---|
| 0 | 2 | u16 | 5024 |
| 2 | 1 | u8 | 0 (ACK) |
| 3 | 2 | u16 | host protocol version: always **150** (`:23`) |
| 5 | 2 | u16 | product number 0xFFFF (−1) |
| 7 | 4 | u32 | unit number 0xFFFFFFFF |
| 11 | 2 | u16 | software version 7791 (0x1E6F) |
| 13 | 2 | u16 | max packet size 0xFFFF |
| 15 | lpstr | | Android adapter BT name; falls back to "Gadgetbridge" (`:105-115`). The watch shows it as "Connected with X" (commit 2bc9020910, #6550). |
| … | lpstr | | manufacturer (`Build.MANUFACTURER`) |
| … | lpstr | | device (`Build.DEVICE`) |
| … | 1 | u8 | `protocolFlags` = 1 if watch protocol_version/100 == 1, else 0 (`:93, 118`) |

Example frame with all three strings set to "linux" and the final byte 1:
`28 00 88 13 A0 13 00 96 00 FF FF FF FF FF FF 6F 1E FF FF 05 6C 69 6E 75 78 05 6C 69 6E 75 78 05 6C 69 6E 75 78 01 3F 38`

**Disagreements:**
- **Protocol version:** PL replies 150 only if the peer is ≥ 150 *and* transaction IDs are used, otherwise 113 (`src/garmin_proto_lab/handshake.py:48-73`). GBR echoes the watch's own version (`src/protocol/handshake.py:20-34`).
- **Final byte:** PL calls it the dual-pairing capability and always sends 1 (`spec/PROTOCOL.md:433-437`).
- **Number of responses:**
  - Before 2bc9020910 (2026-08-26), Gadgetbridge sent only the bare ACK and still worked.
  - GBLE still sends only a bare ACK.
  - GBR and PL send a single full reply.

### 5.2 CONFIGURATION 5050 (same layout in both directions)

| Offset | Size | Type | Field |
|---|---|---|---|
| 0 | 1 | u8 | N = bitmask length in bytes |
| 1 | N | bytes | Capability bitmask, LSB first (bit b = ordinal) |

Sources: `S/messages/ConfigurationMessage.java:42-45, 53-60`; `C/GarminCapability.java:204-220, 238-254`.

**GarminCapability ordinals** (= bit numbers; enum at `C/GarminCapability.java:36-166`, ordinal = source line − 37):

| Byte | Bits |
|---|---|
| 0 | 0 CONNECT_MOBILE_FIT_LINK, 1 GOLF_FIT_LINK, 2 VIVOKID_JR_FIT_LINK, 3 SYNC, 4 DEVICE_INITIATES_SYNC, 5 HOST_INITIATED_SYNC_REQUESTS, 6 GNCS, 7 ADVANCED_MUSIC_CONTROLS |
| 1 | 8 FIND_MY_PHONE, 9 FIND_MY_WATCH, 10 CONNECTIQ_HTTP, 11 CONNECTIQ_SETTINGS, 12 CONNECTIQ_WATCH_APP_DOWNLOAD, 13 CONNECTIQ_WIDGET_DOWNLOAD, 14 CONNECTIQ_WATCH_FACE_DOWNLOAD, 15 CONNECTIQ_DATA_FIELD_DOWNLOAD |
| 2 | 16 CONNECTIQ_APP_MANAGEMENT, 17 COURSE_DOWNLOAD, 18 WORKOUT_DOWNLOAD, 19 GOLF_COURSE_DOWNLOAD, 20 DELTA_SOFTWARE_UPDATE_FILES, 21 FITPAY, 22 LIVETRACK, 23 LIVETRACK_AUTO_START |
| 3 | 24 LIVETRACK_MESSAGING, 25 GROUP_LIVETRACK, 26 WEATHER_CONDITIONS, 27 WEATHER_ALERTS, 28 GPS_EPHEMERIS_DOWNLOAD, 29 EXPLICIT_ARCHIVE, 30 SWING_SENSOR, 31 SWING_SENSOR_REMOTE |
| 4 | 32 INCIDENT_DETECTION, 33 TRUEUP, 34 INSTANT_INPUT, 35 SEGMENTS, 36 AUDIO_PROMPT_LAP, 37 AUDIO_PROMPT_PACE_SPEED, 38 AUDIO_PROMPT_HEART_RATE, 39 AUDIO_PROMPT_POWER |
| 5 | 40 AUDIO_PROMPT_NAVIGATION, 41 AUDIO_PROMPT_CADENCE, 42 SPORT_GENERIC, 43 SPORT_RUNNING, 44 SPORT_CYCLING, 45 SPORT_TRANSITION, 46 SPORT_FITNESS_EQUIPMENT, 47 SPORT_SWIMMING |
| 6 | 48 STOP_SYNC_AFTER_SOFTWARE_UPDATE, 49 CALENDAR, 50 WIFI_SETUP, 51 SMS_NOTIFICATIONS, 52 BASIC_MUSIC_CONTROLS, 53 AUDIO_PROMPTS_SPEECH, 54 DELTA_SOFTWARE_UPDATES, 55 GARMIN_DEVICE_INFO_FILE_TYPE |
| 7 | 56 SPORT_PROFILE_SETUP, 57 HSA_SUPPORT, 58 SPORT_STRENGTH, 59 SPORT_CARDIO, 60 UNION_PAY, 61 IPASS, 62 CIQ_AUDIO_CONTENT_PROVIDER, 63 UNION_PAY_INTERNATIONAL |
| 8 | 64 REQUEST_PAIR_FLOW, 65 LOCATION_UPDATE, 66 LTE_SUPPORT, 67 DEVICE_DRIVEN_LIVETRACK_SUPPORT, 68 CUSTOM_CANNED_TEXT_LIST_SUPPORT, 69 EXPLORE_SYNC, 70 INCIDENT_DETECT_AND_ASSISTANCE, 71 CURRENT_TIME_REQUEST_SUPPORT |
| 9 | 72 CONTACTS_SUPPORT, 73 LAUNCH_REMOTE_CIQ_APP_SUPPORT, 74 DEVICE_MESSAGES, 75 WAYPOINT_TRANSFER, 76 MULTI_LINK_SERVICE, 77 OAUTH_CREDENTIALS, 78 GOLF_9_PLUS_9, 79 ANTI_THEFT_ALARM |
| 10 | 80 INREACH, 81 EVENT_SHARING, 82 HSA_FEATURE_MESSAGE, 83 HANDS_CALIBRATION, 84 LOCATION_UPDATE_PROVIDER, 85 LIVE_TRACK_COURSE_SHARING, 86 SIMPLE_SETUP_FEATURE_LIST, 87 WIND_DATA_APP_SUPPORT |
| 11 | 88 HTTP_RAW_RESOURCE_REQUEST, 89 SAMD, **90 SYNC_2**, 91 NAVIGATION_SYNC, 92 REALTIME_SETTINGS, 93 HAND_CALIBRATION_IMG_DETECT, 94 TOYSTORE_MOBILE, 95 FEATURE_CAPABILITIES_EXCHANGE |
| 12 | 96 DEEP_LINKING, 97 GARMIN_CONNECT_SERVER, 98 CREDENTIALS_OAUTH2_SUPPORT, 99 GENERIC_CREDENTIALS, 100–103 CAPABILITY_100…103 |
| 13 | 104–111 CAPABILITY_104…111 |
| 14 | 112–119 CAPABILITY_112…119 |
| 15 | 120–127 CAPABILITY_120…127 |
| 16 | 128 CAPABILITY_128 (bits 129–135 have no enum value) |

**What Gadgetbridge advertises as host:**
- Every bit **except** 22, 23, 28, 104–111 and 114–119 (`C/GarminCapability.java:169-201`).
- The payload is 17 bytes: `FF FF 3F EF FF FF FF FF FF FF FF FF FF 00 03 FF 01`.
- Full frame: `18 00 BA 13 11 FF FF 3F EF FF FF FF FF FF FF FF FF FF 00 03 FF 01 1E 00`.

**What Gadgetbridge reads from the watch:**
- All set bits are persisted as names in pref `garmin_capabilities` (`C/GarminCapability.java:267-276`; `C/GarminPreferences.java:6`) and queried through `C/GarminCoordinator.java:452-455`.
- Runtime uses:
  - REALTIME_SETTINGS (`S/GarminSupport.java:564`; `C/watches/GarminWatchCoordinator.java:35`)
  - WEATHER_CONDITIONS (`S/GarminSupport.java:954`)
  - EXPLORE_SYNC (`S/GarminSupport.java:1336`)
  - CONNECTIQ_WATCH_APP_DOWNLOAD, WAYPOINT_TRANSFER, BASIC/ADVANCED_MUSIC_CONTROLS, FIND_MY_WATCH (`C/GarminCoordinator.java:402, 442-443, 464-465, 470`)
  - WORKOUT/COURSE_DOWNLOAD, SEGMENTS and SPORT_*, in install handlers.
- **SYNC_2 is never read.** It appears only in a TODO (`C/GarminCoordinator.java:127`).

**PL's minimal host set** (`src/garmin_proto_lab/capabilities.py:18-35`; `spec/PROTOCOL.md:439-443`):
- Pairing set: bits 0, 3, 4, 5, 64, 71.
- Fitness adds 29 and 90; notifications add 6.
- PL also says peer bit 95 gates the Core FeatureCapabilities protobuf request, which Gadgetbridge never sends.
- Frame for {0, 3, 4, 5, 29, 64, 71, 90}: `13 00 BA 13 0C 39 00 00 20 00 00 00 00 81 00 00 04 28 3B`.
- GBR instead echoes the watch's own bitmap back and sends no ACK for 5050 (`src/protocol/handshake.py:39-49`; `src/protocol/__init__.py:68-74`).
- GBLE sends no host CONFIGURATION at all.

### 5.3 AUTH_NEGOTIATION 5101

**W→H payload** (`S/messages/AuthNegotiationMessage.java:24-30`):

| Offset | Size | Type | Field |
|---|---|---|---|
| 0 | 1 | u8 | unknown (PL: "prefix") |
| 1 | 4 | u32 | flags. Gadgetbridge enum `UNK_00000001` … `UNK_10000000`, bits 0–7 only (`:46-60`); PL: peer algorithm mask, bit 0 = XXTEA. |

**H→W reply** (RESPONSE) (`S/messages/status/AuthNegotiationStatusMessage.java:55-66`):

| Offset | Size | Type | Gadgetbridge value | PL meaning |
|---|---|---|---|---|
| 0 | 2 | u16 | 5101 | |
| 2 | 1 | u8 | 0 ACK | |
| 3 | 1 | u8 | 0 = GUESS_OK (1 = GUESS_KO) (`:68-71`) | common-algorithm status, 0 = common found |
| 4 | 1 | u8 | echo of watch byte 0 | has-persistent-LTK flag |
| 5 | 4 | u32 | echo of watch flags. Bits above 7 are dropped by the 8-value EnumSet round-trip. | host algorithm mask (PL sends 0x00000001) |

- **History:** zero flags were sent in 7fa5cd1be5; echoing since cd46e24819 (2024-08-16, "not tested against real devices") (`S/messages/AuthNegotiationMessage.java:21`).
- **No handshake follows in Gadgetbridge:** `AuthNegotiationMessage.generateOutgoing` is never sent (`:43`), and 5102–5112 have no handlers.
- **Example frame** for watch input `00 01000000`: `0F 00 88 13 ED 13 00 00 00 01 00 00 00 B7 86`.
- **GBR's reply is one byte shorter:** `[ACK][00][u32 0]` (`src/protocol/handshake.py:52-59`).
- **PL:** on an OS-bonded link, Garmin Connect's StubAuth throws if 5101 ever arrives. 5101 belongs to the unbonded proprietary-auth route (`spec/PROTOCOL.md:390-400`).

### 5.4 SYSTEM_EVENT 5030 (H→W; Gadgetbridge does not parse it inbound)

| Offset | Size | Type | Field |
|---|---|---|---|
| 0 | 1 | u8 | event type |
| 1 | 1 or 1+n | u8 or lpstr | value. Integer values are written as **one byte**; strings as lpstr (`S/messages/SystemEventMessage.java:15-28`). Gadgetbridge always sends u8 0. |

**Event enum** (`S/messages/SystemEventMessage.java:30-48`):

| Value | Event |
|---|---|
| 0 | SYNC_COMPLETE |
| 1 | SYNC_FAIL |
| 2 | FACTORY_RESET |
| 3 | PAIR_START |
| 4 | PAIR_COMPLETE |
| 5 | PAIR_FAIL |
| 6 | HOST_DID_ENTER_FOREGROUND |
| 7 | HOST_DID_ENTER_BACKGROUND |
| 8 | SYNC_READY |
| 9 | NEW_DOWNLOAD_AVAILABLE |
| 10 | DEVICE_SOFTWARE_UPDATE |
| 11 | DEVICE_DISCONNECT |
| 12 | TUTORIAL_COMPLETE |
| 13 | SETUP_WIZARD_START |
| 14 | SETUP_WIZARD_COMPLETE |
| 15 | SETUP_WIZARD_SKIPPED |
| 16 | TIME_UPDATED |
| 17 | TRIGGER_REBOOT |

**When Gadgetbridge sends each:**
- TIME_UPDATED: at init and on time change (`S/GarminSupport.java:1106-1108, 1363-1365`).
- SYNC_READY: at init (`:1110`, and RFCOMM init at `:1094`).
- PAIR_COMPLETE, SYNC_COMPLETE, SETUP_WIZARD_COMPLETE: first connect only (`:1119-1126`).
- SYNC_COMPLETE: after each fetch finishes (`:1327-1329`).
- HOST_DID_ENTER_FOREGROUND/BACKGROUND: on Android app lifecycle (`:231-247`; commit 11ca07451a). It is no longer sent at init.
- Never sent: DEVICE_DISCONNECT.

**Example frames:**

| Event | Frame |
|---|---|
| SYNC_READY | `08 00 A6 13 08 00 D5 C5` |
| TIME_UPDATED | `08 00 A6 13 10 00 DF C5` |
| PAIR_COMPLETE | `08 00 A6 13 04 00 D0 C5` |
| SYNC_COMPLETE | `08 00 A6 13 00 00 D2 05` |
| SETUP_WIZARD_COMPLETE | `08 00 A6 13 0E 00 D6 65` |

- **GBR** sends SYNC_READY with no value byte (`src/protocol/handshake.py:108-111`).
- **GBLE** sends TIME_UPDATED every 60 s as a keep-alive (`garmin_ble/watch.py:902-922`).
- **Gadgetbridge has no keep-alive.**

### 5.5 CURRENT_TIME_REQUEST 5052

- **W→H payload:** u32 `referenceID` (`S/messages/CurrentTimeRequestMessage.java:21-25`).
- **H→W reply:** a RESPONSE with no separate ACK (`:18, 28-92`), laid out as below.

| Offset | Size | Type | Field |
|---|---|---|---|
| 0 | 2 | u16 | 5052 |
| 2 | 1 | u8 | 0 ACK (2 = UNSUPPORTED if time sync is disabled, `S/GarminSupport.java:481-485`) |
| 3 | 4 | u32 | referenceID, echoed |
| 7 | 4 | u32 | now, Garmin epoch seconds, UTC (`:35`) |
| 11 | 4 | i32 | total local UTC offset in seconds, **including DST** (`:34`) |
| 15 | 4 | u32 | Gadgetbridge: the transition **after** the next one (variable "nextTransitionEnds"); 0 if none |
| 19 | 4 | u32 | Gadgetbridge: the **next** transition ("nextTransitionStarts"); 0 if none |

- **No DST-offset field:** `dstOffset` is computed at `:33` but never written. The total payload is 23 bytes.
- **Transition lookups** are guarded against exceptions (#5914, commit 4cb5a9e34d, `:37-69`).

**Ordering disagreement for offsets 15 and 19:**
- PL (`spec/PROTOCOL.md:466-480`; `src/garmin_proto_lab/time_sync.py:67-93`) puts the next DST *start* at 15 and the next DST *end* at 19. It classifies each transition by whether it enters DST.
- Gadgetbridge's positional order matches PL only while DST is active, i.e. when the next transition is a DST end.
- In winter Gadgetbridge sends the two values swapped.
- GBLE echoes referenceID as 0 and sends zeros (`garmin_ble/protocol/gfdi.py:86-113`). GBR echoes the reference and sends zeros (`src/protocol/handshake.py:62-72`).

**Settled by our capture (note 05).** Garmin Connect's reply to the Fenix 8 on 2026-11-07 was `91 3C 1B 00 | now | 10 0E 00 00 | 90 0F 0B 46 | 10 25 29 47`:
- the reference echoed;
- `now` in Garmin-epoch seconds, **UTC**;
- offset +3600 (CET);
- 2027-03-28 01:00 UTC at offset 15 (the next DST **start**);
- 2027-10-31 01:00 UTC at offset 19 (the next DST **end**).

That is garmin-proto-lab's ordering, and Gadgetbridge's winter order is the swapped one. goarmin (`pkg/garmin/watch`) follows Garmin Connect and sends zeros for zones without DST.

### 5.6 BATTERY_STATUS 5023 (W→H; bare ACK)

| Offset | Size | Type | Field |
|---|---|---|---|
| 0 | 1 | u8 | status = byte & 0x70: 0x10 good, 0x20 ok, 0x30 low, 0x40 critical, 0x50 new, 0x70 invalid |
| 1 | 1 | u8 | Gadgetbridge logs it as volts = byte/100 (semantics unverified) |
| 2 | 1 | u8 | capacity % |
| 3–6 | 4 | ? | unknown |

- Sources: `S/messages/BatteryStatusMessage.java:30-50`, commit bd8597c267 (taken from PL).
- PL gives a minimum of 6 bytes (`spec/PROTOCOL.md:449-462`).
- Gadgetbridge's real battery level comes from the protobuf DeviceStatus request (`S/GarminSupport.java:1343-1352`).

### 5.7 DEVICE_SETTINGS 5026 (H→W)

| Offset | Size | Type | Field |
|---|---|---|---|
| 0 | 1 | u8 | count (1–255) |
| 1… | | repeated | `[u8 setting id][u8 value length][value]`. Boolean: len 1, value 0/1. Integer: len 4, i32. String: lpstr, where the length byte is the string length. |

- Source: `S/messages/SetDeviceSettingsMessage.java:18-40`.
- **Setting IDs** (`:42-52`): 0 DEVICE_NAME, 1 CURRENT_TIME, 2 DAYLIGHT_SAVINGS_TIME_OFFSET, 3 TIME_ZONE_OFFSET, 4 NEXT_DAYLIGHT_SAVINGS_START, 5 NEXT_DAYLIGHT_SAVINGS_END, 6 AUTO_UPLOAD_ENABLED, 7 WEATHER_CONDITIONS_ENABLED, 8 WEATHER_ALERTS_ENABLED.
- **Gadgetbridge sends** 6=true, 7=true, 8=false (`S/GarminSupport.java:1354-1360`). Frame: `10 00 A2 13 03 06 01 01 07 01 01 08 01 00 87 28`.
- **GBR sends** 8=true (`src/protocol/handshake.py:95-105`).

### 5.8 SUPPORTED_FILE_TYPES_REQUEST 5031

- **H→W:** empty payload (`S/messages/SupportedFileTypesMessage.java:10-15`). Frame `06 00 A7 13 3B 75`.
- **W→H RESPONSE:** layout below. Unknown types are skipped; a non-ACK status yields nothing (`S/messages/status/SupportedFileTypesStatusMessage.java:22-43`).

| Offset | Size | Type | Field |
|---|---|---|---|
| 0 | 2 | u16 | 5031 |
| 2 | 1 | u8 | status |
| 3 | 1 | u8 | count |
| 4… | | repeated | `[u8 dataType][u8 subType][lpstr name]`, e.g. 128/4 = activity (`S/FileType.java:22-38, 53-140`) |

PL agrees and adds a fallback for UNSUPPORTED (`spec/PROTOCOL.md:621-629`).

### 5.9 SYNCHRONIZATION 5037 (W→H; bare ACK)

| Offset | Size | Type | Field |
|---|---|---|---|
| 0 | 1 | u8 | type: 0 MANUAL, 1 AUTOMATIC, 2 TYPE_2 (`S/messages/SynchronizationMessage.java:66-80`). PL: 0 manual, 1 invisible, 2 visible-as-needed. **An unknown value throws, so Gadgetbridge sends no ACK.** |
| 1 | 1 | u8 | bitmask size. **Gadgetbridge accepts only 4 or 8; anything else is dropped with no ACK** (`:41-50`). PL allows N bytes (`spec/PROTOCOL.md:482-492`). |
| 2 | 4/8 | u32/u64 | pending-category bitmask, bit n = category n |

- **Named categories** (`:82-148`): 0 SCHEDULES, 1 SETTINGS, 2 GOALS, 3 WORKOUTS, 4 COURSES, 5 ACTIVITIES, 6 PERSONAL_RECORDS, 8 SOFTWARE_UPDATE, 9 DEVICE_SETTINGS, 10 LANGUAGE_SETTINGS, 11 USER_PROFILE, 12 SPORTS, 13 SEGMENT_LEADERS, 14 GOLF_CLUB, 15 WELLNESS_DEVICE_INFO, 16 WELLNESS_DEVICE_CCF, 17 INSTALL_APP, 18 CHECK_BACK, 19 TRUE_UP, 20 SETTINGS_CHANGE, 21 ACTIVITY_SUMMARY, 22 METRICS_FILE, 23 PACE_BAND, 24 SPORT_BACKUP, 25 ULF_CONFIG, 26 SLEEP, 27 EXERCISE_BENCHMARK, 28 POWER_GUIDANCE, 29 SPORTING_EVENT, 30 ACTUAL_STEP_RECORDING_CONFIGURATION, 31 LHA_BACKUP, 32 BBI_RECORDING_CONFIGURATION, 36 HRV_STATUS, 39 UNINSTALL_APP. All others are unknown. These agree with PL (`spec/PROTOCOL.md:494-498`).
- **Proceed rule:** bit 3, 5, 21 or 26 set, or type MANUAL (`:55-59`).

### 5.10 FILTER 5007

- **H→W payload:** u8 filter, one of 0 ALL_FILES_0, 1 DEFAULT, 2 UNK_2, 3 ONLY_NEW_3 (`S/messages/FilterMessage.java:31-45`). PL names 3 "pending uploads only" (`spec/PROTOCOL.md:631-633`). Gadgetbridge sends 3; frame `07 00 8F 13 03 C9 1A`.
- **W→H RESPONSE:** `[5007][status][u8 unk]`, where PL says 0 = supported. On ACK, Gadgetbridge requests the directory (`S/messages/status/FilterStatusMessage.java:14-23`; `S/FileTransferHandler.java:122-123`).

### 5.11 Directory download and FILE_AVAILABLE (pointer to `03-legacy-file-sync.md`)

**DOWNLOAD_REQUEST 5002, H→W** (14 bytes; `S/messages/DownloadRequestMessage.java:47-64`; agrees with PL `spec/PROTOCOL.md:536-547`):
- u16 fileIndex (0 = directory)
- u32 offset
- u8 type (0 CONTINUE, 1 NEW)
- u16 crcSeed
- u32 size
- u8 flags (bit 0 = compression; Gadgetbridge sends 0)

Directory request frame: `14 00 8A 13 00 00 00 00 00 00 01 00 00 00 00 00 00 00 30 32`.

**FILE_AVAILABLE 5009, W→H** (bare ACK; `S/messages/FileAvailableMessage.java:39-61`):
- u16 index
- u8 dataType
- u8 subType
- u16 fileNumber
- u8 specificFlags
- u8 fileFlags
- u32 size
- u32 Garmin timestamp (0 = none)
- [u8 unknown]

**SET_FILE_FLAG 5008, H→W:** u16 index, u8 flags. ARCHIVE = 0x10; the full set is READ 0x80, WRITE 0x40, ERASE 0x20, APPEND 0x08, CRYPTO 0x04 (`S/messages/SetFileFlagsMessage.java:19-37`).

### 5.12 NOTIFICATION_SUBSCRIPTION 5036 (W→H)

- **Payload:** u8 enable (1 = subscribe), then an optional u8 unk (PL: feature flags) (`S/messages/NotificationSubscriptionMessage.java:23-28`).
- **Reply:** a RESPONSE only, no bare ACK: `[5036][ACK][u8 status 0 ENABLED / 1 DISABLED][u8 enable echo][u8 unk]`. Gadgetbridge sends unk = 0 (`S/messages/status/NotificationSubscriptionStatusMessage.java:22-38`; `S/GarminSupport.java:600-605`).
- PL and GBR use the same layout (`spec/PROTOCOL.md:703-720`; `src/protocol/handshake.py:75-82`).
- Example "disabled" reply for enable=1: `0C 00 88 13 AC 13 00 01 01 00 26 C7`.

### 5.13 PROTOBUF_REQUEST 5043 / PROTOBUF_RESPONSE 5044

**Message** (both directions; `S/messages/ProtobufMessage.java:44-52, 83-93`; agrees with PL `spec/PROTOCOL.md:790-804`):

| Offset | Size | Type | Field |
|---|---|---|---|
| 0 | 2 | u16 | requestId. Host-originated IDs increment mod 65536 starting at 1 (`S/ProtocolBufferHandler.java:104-107`); replies reuse the watch's ID. |
| 2 | 4 | u32 | dataOffset |
| 6 | 4 | u32 | total protobuf length |
| 10 | 4 | u32 | this chunk's length |
| 14 | n | bytes | GDI `Smart` protobuf bytes |

**Protobuf status** (RESPONSE) (`S/messages/status/ProtobufStatusMessage.java:75-139`):
- u16 orig, u8 status
- u16 requestId
- u32 dataOffset
- u8 chunkStatus (0 KEPT, 1 DISCARDED)
- u8 code (0 NO_ERROR, 100 UNKNOWN_REQUEST_ID, 101 DUPLICATE_PACKET, 102 MISSING_PACKET, 103 EXCEEDED_TOTAL_PROTOBUF_LENGTH, 200 PROTOBUF_PARSE_ERROR, 201 UNKNOWN)

**Gadgetbridge behaviour:**
- **Unhandled services** get KEPT/NO_ERROR if processed, otherwise DISCARDED/UNKNOWN_REQUEST_ID (`S/ProtocolBufferHandler.java:276-295`). Example for request 7: `11 00 88 13 B3 13 00 07 00 00 00 00 00 01 64 1D FF`.
- **Core ConnectionReadyNotification** gets no protobuf reply. Since 721bb7487f (2026-11-02): "Some tested FW versions seem to have state-tracking issues when a Protobuf response is sent" (`S/ProtocolBufferHandler.java:526-532`).
- **Outgoing chunking** uses `max_packet_size − 20` (`:684-711`). The next chunk is sent when the watch's status for the previous one arrives (`:300-322`).

Full protobuf-layer detail lives in `04-protobuf-fileaccess-http.md`.

### 5.14 Other unsolicited W→H messages

| Message | Layout | Gadgetbridge reply |
|---|---|---|
| WEATHER_REQUEST 5014 | u8 format, i32 lat, i32 lon, u8 hours (`S/messages/WeatherRequestMessage.java:19-26`) | bare ACK. FIT weather follows only if data exists and the watch has WEATHER_CONDITIONS (`S/GarminSupport.java:556-560, 953-966`). |
| FIND_MY_PHONE_REQUEST 5039 / CANCEL 5040 | u8 duration / empty | bare ACK |
| MUSIC_CONTROL_CAPABILITIES 5042 | optional u8 | `[5042][ACK][u8 count][count × u8 command 0..8]`, with all 9 commands (`S/messages/MusicControlCapabilitiesMessage.java:22-48`) |
| MUSIC_CONTROL 5041 | u8 command | bare ACK |
| FIT_DEFINITION/FIT_DATA 5011/5012 | FIT records | `[id][ACK][u8 0]` |
| QUEUED_DOWNLOAD 5027 | PL: u8 N plus an N-byte bitset | Gadgetbridge: nothing. PL: ACK plus data byte 0 (`spec/PROTOCOL.md:445-447`). |
| PROTOBUF_REQUEST: location, calendar, CIQ HTTP, OAuth, FileAccess, … | §5.13 | various; DISCARDED if unhandled |

## 6. What triggers a sync

1. **Watch-initiated** (typical after SYNC_READY or HOST_DID_ENTER_FOREGROUND; commit ab5999a480 says foreground is sent "so that the watch can initiate the synchronization"):
   1. W→H SYNCHRONIZATION.
   2. H→W ACK.
   3. If the proceed rule matches, H→W FILTER(3) (`S/FileTransferHandler.java:131-139`). It is deferred while an upload is busy.
   4. W→H RESPONSE(5007, ACK).
   5. H→W DOWNLOAD_REQUEST(index 0) (`S/FileTransferHandler.java:122-123, 146-149`).
2. **Host-initiated:** DOWNLOAD_REQUEST(index 0) directly, with **no FILTER** (`S/GarminSupport.java:678-723`, at `:718`). This is also used as a "flush" before the new protobuf sync (`:716-717`). #6700: the flush can freeze some firmwares; there is an option to skip it (`:689-697`; commit 9daafa4061; pref `C/GarminPreferences.java:21`).
3. **FILE_AVAILABLE 5009** queues the file directly (`S/FileTransferHandler.java:124-127, 299-324`).
4. **After each file:**
   - SET_FILE_FLAG ARCHIVE, unless keep-on-device (`S/GarminSupport.java:639-641`).
   - Files that already exist locally are skipped and archived (`:1236-1248`).
   - At the end: SYSTEM_EVENT SYNC_COMPLETE (`:1327-1329`).

## 7. Time handling

- **Garmin epoch:** `GARMIN_TIME_EPOCH = 631065600` = 1989-12-31T00:00:00Z, the FIT epoch (`S/GarminTimeUtils.java:8-24`). This agrees with PL and GBR. GBLE's comment says "1990-01-01" (`garmin_ble/protocol/gfdi.py:21`), but its value is the same 631065600.
- **Conversion:** garmin = unix − 631065600. Use uint32: Java's `int` cast (`:14-16`) breaks after 2038 in Unix terms, while Garmin u32 lasts until 2126.
- **GFDI timestamps are UTC Garmin seconds.** This covers the time reply, FILE_AVAILABLE and the directory (`S/messages/FileAvailableMessage.java:47-52`).
- **The only local-time quantities** are the total UTC offset in the 5052 reply and the DEVICE_SETTINGS IDs 2–5, which Gadgetbridge never sends.
- **Day of week** uses the local zone (`S/GarminTimeUtils.java:26-28`).
- **Clock injection:** take one clock value per reply and derive everything from it, including in tests (project rule: tests never hardcode dates).

## 8. Unit ID / serial

- **Watch:** u32 at DEVICE_INFORMATION payload offset 4. It is unsigned (`S/messages/DeviceInformationMessage.java:57`) and shown as "Unit ID" in `fwVersion2` (`:138`; commit 62d458255b).
- **Host:** sends 0xFFFFFFFF.
- **Gadgetbridge keys all per-device state by the BLE MAC address,** not the unit ID (`S/GarminSupport.java:358`). For goarmin's multi-user model, key watches by unit ID (also present in every FIT `file_id` as the serial), with the MAC as a secondary attribute.

## 9. Fenix 8 and newer-device notes

- **Coordinators:** Fenix 8 (`C/watches/fenix/GarminFenix8Coordinator.java:8-18`) matches the name `^fenix 8( - [\d+]+mm)?$`. The test name "fenix 8 - 51mm" refers to #4226 (`app/src/test/java/nodomain/freeyourgadget/gadgetbridge/devices/BluetoothNameTest.java:204`). The Solar and Pro coordinators are marked experimental with unconfirmed names.
- **Product numbers:** 4534 and 4536 (Fenix 8), 4532 and 4533 (Solar), 4631 (Pro) (`FitCodeGenerator/src/main/resources/fit_profile.json:7680-7683, 7703`). Gadgetbridge also matches Garmin advertising manufacturer data, bytes 0–1 big-endian part number (`C/GarminCoordinator.java:105-116`).
- **No Fenix-specific GFDI code exists.** Defaults:
  - Legacy sync (`defaultNewSyncProtocol` = false, `C/GarminCoordinator.java:141-143`).
  - MLR pref defaults to true at runtime (`S/GarminSupport.java:729-731`).
  - Explore sync off on watches (`C/watches/GarminWatchCoordinator.java:21-26`).
  - Only Venu X1, Venu 4, vívoactive 6 and Cirqa default to the new sync. #5021: "Venu X1 misses a lot of files without the new sync protocol".
- **TODOs added in 9896fa4ebc (2026-10-26),** neither implemented:
  - "check presence of GarminCapability.SYNC_2 instead" (`C/GarminCoordinator.java:127`).
  - "check DeviceInformationMessage.protocolVersion >= 150 instead" for enabling MLR (`:130`).
- **Related issues:** #4522 compact IDs (seen on vívoactive 5, `S/FileType.java:166`); #6550 device-info reply; #6700 legacy flush freeze; #5914 DST crash guard.

## 10. Teardown, reconnect and persisted state

- **No GFDI goodbye:** Gadgetbridge sends no GFDI message at teardown; DEVICE_DISCONNECT is unused.
- **`dispose()`** (`S/GarminSupport.java:287-317`) does the following:
  - Resets the file-sync state.
  - Closes the MLR communicators (`S/communicator/v2/CommunicatorV2.java:146-152`).
  - Tells ExploreSync to drop its buffers.
- **Reconnect:** `useAutoConnect` = false (`S/GarminSupport.java:352-354`). Reconnect is done by Gadgetbridge's generic BLE layer. Every connection re-runs `initializeDevice` (`:363-399`): CLOSE_ALL, register GFDI, then the full §4 handshake.
- **Unexpected GFDI close by the watch:** immediate re-registration (`S/communicator/v2/CommunicatorV2.java:456-464`) without redoing init (§4).
- **Persisted between sessions:**
  - Watch capability names (`C/GarminCapability.java:274`).
  - Installed apps (`S/GarminSupport.java:893`).
  - The RFCOMM fallback flag, Edge only (`:1082-1084`).
  - New-sync and MLR defaults (`C/GarminCoordinator.java:124-139`).
  - Downloaded files, used for name-based dedupe (`S/GarminSupport.java:1673-1717`).
  - The pending-file DB (`:629-634`).
  - On the watch itself: the ARCHIVE flags.
- **Not persisted:** first-connect flag (`:219`), max packet size, protobuf request counter. Gadgetbridge stores **no pairing or session keys**; it relies on the OS bond.

## 11. Minimum viable pull-only client checklist

**MUST**
1. Frame and parse GFDI as in §2: u16 total length including CRC, u16 ID, CRC-16/ARC over everything before it, little-endian. Normalise compact IDs (bit 15). Reject frames with a bad length or CRC.
2. Never ACK a RESPONSE (5000).
3. DEVICE_INFORMATION: send one full RESPONSE (§5.1) and record `max_packet_size`. Never send a frame larger than that limit.
4. CONFIGURATION: ACK it, then send the host CONFIGURATION. Use at least bits {0, 3, 4, 5, 29, 64, 71}, plus 90 only if FileAccess/SYNC_2 is implemented. Store the watch's bitmask, particularly 90 and 95.
5. After the watch's CONFIGURATION, send SUPPORTED_FILE_TYPES_REQUEST, DEVICE_SETTINGS (AUTO_UPLOAD_ENABLED=true) and SYSTEM_EVENT SYNC_READY, in Gadgetbridge's order.
6. CURRENT_TIME_REQUEST: one RESPONSE with the 20-byte time data (§5.5). Use PL's start/end classification.
7. PROTOBUF_REQUEST/RESPONSE: reassemble chunks. Always send the 8-byte protobuf status: KEPT/NO_ERROR for ConnectionReady and other handled requests (no protobuf reply to ConnectionReady); DISCARDED/UNKNOWN_REQUEST_ID otherwise.
8. Bare-ACK these: SYNCHRONIZATION, FILE_AVAILABLE, BATTERY_STATUS, WEATHER_REQUEST, FIND_MY_PHONE request/cancel, MUSIC_CONTROL, FIT_* (with code 0). Also QUEUED_DOWNLOAD, per PL.
9. NOTIFICATION_SUBSCRIPTION: reply with status DISABLED (1) plus the enable echo.
10. AUTH_NEGOTIATION, if it arrives: reply as Gadgetbridge does (§5.3), and log it as a sign that the link is not OS-bonded.
11. Sync: either SYNCHRONIZATION→FILTER(3)→directory (index 0), or a host-initiated directory read. Then download, SET_FILE_FLAG ARCHIVE, and finally SYSTEM_EVENT SYNC_COMPLETE.

**SHOULD**
- On first pairing, send PAIR_COMPLETE, SYNC_COMPLETE and SETUP_WIZARD_COMPLETE.
- Send TIME_UPDATED after the handshake.
- Send HOST_DID_ENTER_FOREGROUND when a sync is wanted.
- For anything unknown, reply UNSUPPORTED (per PL) rather than staying silent; Gadgetbridge's silence is tolerated but undocumented.

**OPTIONAL**
- Battery and app-list protobufs, realtime settings, weather, music capabilities (reply ACK with 0 commands: `0A 00 88 13 B2 13 00 00 CD 89`, untested), periodic keep-alive.

## 12. Cross-check summary

**Agreements across Gadgetbridge, PL, GBR and GBLE:**
- Frame layout, CRC, status codes.
- DEVICE_INFORMATION parse.
- Protobuf header and status layout.
- Capability ordinals (PL's 0/3/4/5/6/29/64/71/90/95 match).
- Filter values.
- SYNCHRONIZATION category bits.
- The Garmin epoch value.

**Disagreements:**
- Compact sequence width; transaction-ID echo in responses (§2.2).
- Host protocol version and the final byte of the identity reply (§5.1).
- One versus two DEVICE_INFORMATION responses.
- Full versus minimal versus echoed host CONFIGURATION.
- Auth reply contents and length.
- 5052 DST field order (Gadgetbridge swapped outside DST), referenceID echo (GBLE sends 0).
- SYNC_READY value byte (GBR omits it).
- Unknown-message handling (Gadgetbridge silent, PL UNSUPPORTED, GBLE ACK).
- QUEUED_DOWNLOAD (Gadgetbridge silent, PL ACK).
- Protobuf bare ACK (Gadgetbridge sometimes) versus the 8-byte status (PL, GBLE).
- SYNCHRONIZATION bitmask size (Gadgetbridge accepts only 4 or 8).
- BATTERY_STATUS length (7 versus at least 6).

## 13. Open questions / uncertain

1. Does a Fenix 8 send compact/transaction frames, and must host responses echo the transaction ID (PL) or are ordinary frames enough (Gadgetbridge)?
2. What protocol_version does a Fenix 8 report? Should the host answer 150 or 113? PL ties 150 to transaction mode, and Gadgetbridge's TODO ties ≥150 to MLR. What does the trailing DEVICE_INFORMATION byte mean?
3. Does a Fenix 8 advertise SYNC_2 (bit 90) and FEATURE_CAPABILITIES_EXCHANGE (95)? Is the legacy directory complete on it (#5021)? Does the #6700 freeze affect its firmware?
4. Watch-side ACK deadline, and whether unanswered messages (Gadgetbridge's silent cases, ignored protobuf requests) cause a disconnect or stall. There is no positive evidence either way.
5. Does 5101 ever arrive on a BlueZ-bonded link, and what reply is correct then? Gadgetbridge's echo is untested; PL says the bonded route doesn't expect 5101.
6. The correct order of the 5052 DST fields: Gadgetbridge's positional order versus PL's classified order. Does the watch use them at all?
7. Does TIME_UPDATED cause the watch to send CURRENT_TIME_REQUEST? This is inferred, not documented. Is any keep-alive needed? Gadgetbridge has none; GBLE sends TIME_UPDATED every 60 s.
8. Does the watch care about Gadgetbridge's double DEVICE_INFORMATION response?
9. Unknown fields: auth byte 0; BATTERY_STATUS bytes 1 and 3–6; FilterStatus byte; SYNCHRONIZATION TYPE_2; SetFileFlags status index (+1 TODO at `S/messages/status/SetFileFlagsStatusMessage.java:29`).
10. Which host capability bits are necessary and sufficient for the watch to send SYNCHRONIZATION and offer files. Does advertising fewer bits (GNCS, weather, CIQ HTTP) suppress unsolicited requests? This is inference only.
