# 04 — Protobuf channel, FileAccess ("new sync") and the HTTP bridge

Part of the goarmin protocol notes (`docs/protocol/`). Derived from Gadgetbridge at commit
`eb3550358d` (Codeberg), cross-checked against garmin-proto-lab, GarminHRM600 and garmin-ble.
Written 2026-11-07. Describes the protocol, not Gadgetbridge's code; goarmin is a from-scratch
implementation. Example byte strings were computed by the research agent; re-derive them in Go
unit tests rather than copying them.


This spec is derived from Gadgetbridge (GB) at commit `eb3550358d`. Nothing was modified. It is cross-checked against garmin-proto-lab (static reverse engineering, not yet tested on hardware), GarminHRM600 (captures from an HRM 600) and garmin-ble (an old snapshot of the GB protos).

**Citation prefixes.** GB paths are relative to `~/go/src/codeberg.org/Freeyourgadget/Gadgetbridge`.
- `S/` = `app/src/main/java/nodomain/freeyourgadget/gadgetbridge/service/devices/garmin/`
- `D/` = `app/src/main/java/nodomain/freeyourgadget/gadgetbridge/devices/garmin/`
- `P/` = `GBProtobufGenerator/src/main/proto/garmin/`
- `GPL/` = `~/go/src/github.com/seofernando25/garmin-proto-lab/`
- `HRM/` = `~/go/src/github.com/mskerwiderski/GarminHRM600/`
- `GBLE/` = `~/go/src/github.com/gwerneckp/garmin-ble/`

**Byte order.** All integers are little-endian unless stated otherwise (`S/messages/MessageWriter.java:16-25`, `S/messages/GFDIMessage.java:104,250`).

---

## 1. GFDI-level framing: PROTOBUF_REQUEST (5043), PROTOBUF_RESPONSE (5044), CANCEL_PROTOBUF (5045)

### 1.1 GFDI envelope (summary only; the GFDI team owns this)

`[len u16][type u16][payload][crc16 u16]`. `len` counts the whole message, including itself and the CRC (`S/messages/GFDIMessage.java:101-113,130-133`).

Incoming types may use the "compact" form. If bit 15 is set, the id is `(type & 0xFF) + 5000` and the sequence number is `(type >> 8) & 0x7F` (`S/messages/GFDIMessage.java:48-51`). So 5043, 5044 and 5045 can arrive as kinds 0x2B, 0x2C and 0x2D (`HRM/docs/gfdi-filetransfer.md:113,141`). GB always sends full ids.

Type ids on the wire: 5043 = `B3 13`, 5044 = `B4 13`, 5045 = `B5 13`, RESPONSE 5000 = `88 13` (`S/messages/GFDIMessage.java:136,171-173`).

### 1.2 PROTOBUF_REQUEST / PROTOBUF_RESPONSE message

The layout is the same in both directions (`S/messages/ProtobufMessage.java:44-52` parse, `:83-93` build).

| Msg offset | Payload offset | Size | Field |
|---|---|---|---|
| 0 | – | 2 | GFDI length = 20 + N |
| 2 | – | 2 | type 5043 / 5044 |
| 4 | 0 | 2 | `request_id` u16 |
| 6 | 2 | 4 | `data_offset` u32: byte offset of this chunk within the serialized Smart message |
| 10 | 6 | 4 | `total_protobuf_length` u32: full serialized Smart length |
| 14 | 10 | 4 | `protobuf_data_length` u32: N, the bytes in this chunk |
| 18 | 14 | N | chunk bytes (a slice of one serialized `Smart` message) |
| 18+N | – | 2 | CRC |

- A message is **complete (single-chunk)** when `data_offset == 0 && protobuf_data_length == total_protobuf_length`. Otherwise it is "chunked" (`S/messages/ProtobufMessage.java:74-80`).
- **5043 vs 5044.** 5043 starts an RPC or carries a notification. 5044 answers a 5043 and reuses that request's `request_id` (`S/ProtocolBufferHandler.java:677-694`). Both carry a `Smart` message. GB dispatches both through the same `Smart` handler, so watch answers such as `ItemListResponse` arrive as 5044 (`S/ProtocolBufferHandler.java:110-117`, `S/messages/GFDIMessage.java:74-75`).

### 1.3 Protobuf status (ACK) message: RESPONSE 5000 for an original 5043/5044

Layout (`S/messages/status/ProtobufStatusMessage.java:30-46` parse, `:75-86` build). The parse is selected by the original type (`S/messages/status/GFDIStatusMessage.java:9-14`).

| Msg offset | Size | Field |
|---|---|---|
| 0 | 2 | length = `0x0011` (17) |
| 2 | 2 | 5000 |
| 4 | 2 | original type (5043 / 5044) |
| 6 | 1 | GFDI status: 0 ACK, 1 NAK, 2 UNSUPPORTED, 3 DECODE_ERROR, 4 CRC_ERROR, 5 LENGTH_ERROR (`S/messages/GFDIMessage.java:225-231`) |
| 7 | 2 | `request_id` |
| 9 | 4 | `data_offset`: echoes the offset of the chunk being acknowledged |
| 13 | 1 | chunk status: 0 KEPT, 1 DISCARDED (`S/messages/status/ProtobufStatusMessage.java:93-95`) |
| 14 | 1 | status code: 0 NO_ERROR, 100 UNKNOWN_REQUEST_ID, 101 DUPLICATE_PACKET, 102 MISSING_PACKET, 103 EXCEEDED_TOTAL_PROTOBUF_LENGTH, 200 PROTOBUF_PARSE_ERROR, 201 UNKNOWN (`:110-117`) |
| 15 | 2 | CRC |

- "OK" means ACK + KEPT + NO_ERROR (`S/messages/status/ProtobufStatusMessage.java:68-72`).
- There is also a short "generic" ACK: `09 00 88 13 <orig type u16> <status u8> <crc>`, 9 bytes (`S/messages/status/GenericStatusMessage.java:29-37`).

How GB acknowledges an **incoming** 5043/5044:
- **Any chunk of a multi-chunk message, including the last:** long form, `ACK, id, offset, KEPT, NO_ERROR` (`S/messages/ProtobufMessage.java:29-33`).
- **Single-chunk message:** generic short ACK by default (`:29-30`). The long form replaces it in two cases:
  - The service was handled with no immediate response: `KEPT/NO_ERROR` (`S/ProtocolBufferHandler.java:276-284`).
  - No handler recognised it: `DISCARDED` + `100 UNKNOWN_REQUEST_ID` (`:285-295`).
  If the handler returned an immediate response, the short generic ACK stays.
- **Order of output:** GB runs the handler, then sends the ACK, then sends the response or next chunk (`S/GarminSupport.java:488-514`). Exception: a handler that itself sends a new 5043 synchronously, such as the list continuation, gets that 5043 out before the ACK (`S/FileSyncServiceHandler.kt:168-173`).
- **Acks from the watch** for 5043/5044 are always parsed as the long form (`S/messages/status/GFDIStatusMessage.java:13-14`).
- **Agreement:** garmin-proto-lab describes the same 8-byte ack body after `[orig type][status]`: `requestId u16 | offset u32 | failed u8 | status u8` (`GPL/spec/PROTOCOL.md:802`).
- **Recommendation:** always send the long form. It is what garmin-proto-lab models, and GB's short form is also accepted in practice.

### 1.4 Chunking and reassembly

**Max chunk.** `maxChunk = max_packet_size − 20`. That is 18 header bytes (length, type, 14-byte protobuf header) plus 2 CRC bytes. `max_packet_size` is the u16 from the watch's DEVICE_INFORMATION (5024) (`S/ProtocolBufferHandler.java:684-688`, `S/messages/DeviceInformationMessage.java:59,141`, `S/GarminSupport.java:606-610`). Until that arrives the default is 375, "tested on Vívomove Style" (`S/ProtocolBufferHandler.java:76`).
- Disagreement: garmin-proto-lab uses `max_packet_size − 14` (`GPL/spec/PROTOCOL.md:802`, `GPL/src/garmin_proto_lab/protobuf_transport.py:126-130`). The difference is whether `max_packet_size` includes length, type and CRC. **Use GB's −20; it is safe and proven on hardware.**

**Outgoing (stop-and-wait)** (`S/ProtocolBufferHandler.java:696-711,300-322,779-789`):
1. If the serialized length L ≤ maxChunk, send one message: offset 0, total L, chunk L.
2. Otherwise send `[0, maxChunk)` with total L and keep the buffer, keyed by `request_id`.
3. On an OK status for that `request_id` from the watch: if `L ≤ ackOffset + maxChunk`, the transfer is done. Otherwise send `[ackOffset+maxChunk, min(ackOffset+2·maxChunk, L))`.
4. **A non-OK status is ignored and the transfer stalls.** GB has a TODO for this (`S/ProtocolBufferHandler.java:301`). garmin-proto-lab treats anything other than NO_ERROR or DUPLICATE as fatal (`GPL/src/garmin_proto_lab/protobuf_link.py:70-82`).

**Incoming** (`S/ProtocolBufferHandler.java:324-339,756-794`):
1. A complete message is parsed immediately.
2. Offset 0 starts or overwrites the buffer for that `request_id`, sized by `total_protobuf_length`.
3. Offset equal to the bytes held so far: append.
4. GB has no other recovery. A mismatched offset throws (`:772-774`), and a non-zero offset with no buffer dereferences null (`:332-338`).
5. Once complete, `Smart.parseFrom`. On a parse error GB only logs (`:126-132`).

Recommended receive rules (these match garmin-proto-lab's reassembler, `GPL/src/garmin_proto_lab/protobuf_transport.py:178-202`):
- Offset below the bytes held: ack **101 DUPLICATE**, KEPT, and do not append.
- Offset above: ack **102 MISSING**, DISCARDED.
- Overflowing total: ack **103 EXCEEDED**.
- Unparseable: **200 PARSE_ERROR**.
- Cap the total length (GB has no cap).
- Key buffers by `(type, request_id)`. GB uses a single map for both directions (`S/ProtocolBufferHandler.java:74`), which garmin-proto-lab avoids (`GPL/src/garmin_proto_lab/protobuf_link.py:46`).

**The watch's behaviour without an ACK.** It re-sends the same un-ACKed 5043/5044 roughly every 5 s and never sends the next chunk. This includes complete frames. Duplicate responses then confuse request matching (`HRM/docs/gfdi-filetransfer.md:141-156`).

### 1.5 Request ids and timeouts

- **Host request ids:** `(last + 1) mod 65536`. The counter starts at 0 per connection, so the first id is 1 (`S/ProtocolBufferHandler.java:77,104-107`). garmin-proto-lab starts at 0 and skips ids that are in use (`GPL/src/garmin_proto_lab/protobuf_link.py:60-66`).
- **Responses reuse the watch's id.** Asynchronous responses, such as HTTP, are sent later with the original id (`S/GarminSupport.java:579-586`).
- **Timeouts:** GB has no protobuf-level timeout or retry. garmin-proto-lab uses 30 s per response and 5 s for the cancel (`GPL/src/garmin_proto_lab/protobuf_link.py:42,88-105`).

### 1.6 CANCEL_PROTOBUF (5045)

**GB:** defined in the enum only (`S/messages/GFDIMessage.java:173`). An incoming 5045 becomes `UnhandledMessage` with **no ACK sent** (`S/messages/GFDIMessage.java:80-83`, `S/messages/UnhandledMessage.java:15-19`). GB never sends one.

**garmin-proto-lab (static):**
- Payload is `request_id u16`.
- The receiver drops pending reassembly and response state for that id and replies `RESPONSE[5045][ACK][request_id u16]`.
- The host sends a 5045 after a failed or timed-out request (`GPL/spec/PROTOCOL.md:804`, `GPL/src/garmin_proto_lab/protobuf_transport.py:110-123`, `GPL/src/garmin_proto_lab/protobuf_link.py:137-147`).

---

## 2. The Smart envelope

Every 5043/5044 payload is one `Smart` message (proto2, package `garmin_vivomovehr`, `P/gdi_smart_proto.proto:45-116`). Each field is a service sub-message; normally exactly one is set.
- garmin-proto-lab notes that Garmin's own schema declares these as **extensions** of an empty `GDI.Proto.Smart.Smart`. The wire format is identical (`GPL/spec/PROTOCOL.md:808`).
- GB handles only the first matching service, in a fixed order (`S/ProtocolBufferHandler.java:134-275`).

Direction: **W→H** means the watch initiates and the host answers. **H→W** means the host initiates. This is GB usage.

| # | Field | Type (file) | GB usage / direction |
|---|---|---|---|
| 1 | calendar_events_service | GDI.Proto.Calendar.CalendarService (gdi_calendar) | W→H CalendarEventsRequest → CalendarEventsResponse (`S/ProtocolBufferHandler.java:341-428`) |
| 2 | connect_iq_http_service | GDI.Proto.ConnectIQHTTP.ConnectIQHTTPService (gdi_http) | W→H HTTP bridge, see §5 (`:143-150`) |
| 3 | connect_iq_installed_apps_service | ConnectIQInstalledAppsService (gdi_installed_apps) | H→W list/delete apps; W answers (`:206-231`) |
| 4 | connect_iq_app_settings_service | ConnectIQAppSettingsService (gdi_app_settings) | H→W get/set app settings, plus watch requests (`:259-261`) |
| 5 | international_golf_service | stub | unused |
| 6 | swing_sensor_service | stub | unused |
| 7 | data_transfer_service | GDI.Proto.DataTransfer.DataTransferService (gdi_data_transfer) | W→H DataDownloadRequest, FileAccessItemRequest (`:151-169`) |
| 8 | device_status_service | DeviceStatusService (gdi_device_status) | H→W battery request; W responds (`:170-173`, `S/GarminSupport.java:1343-1352`) |
| 9–11 | incident_detection / audio_prompts / wifi_setup | (various) | unused |
| 12 | find_my_watch_service | FindMyWatchService | H→W find/cancel (`:174-177`) |
| 13 | core_service | GDI.Proto.Core.CoreService (gdi_core) | W→H GetLocationRequest, LocationUpdatedSetEnabledRequest, ConnectionReadyNotification (no reply, `:526-532`). H→W LocationUpdatedNotification (`S/GarminSupport.java:1737-1747`) |
| 14, 15 | group_live_track / expresspay_command | | unused |
| 16 | sms_notification_service | SmsNotificationService | W→H canned-list request; H→W changed notification (`:581-614,713-738`) |
| 17–21 | live_track_messaging, instant_input, sport_profile_setup, hsa_data, live_track | | unused |
| 22 | explore_sync_service | ExploreSyncService (gdi_explore_sync) | both directions, see §4 (`:265-275`) |
| 23 | way_point_transfer_service | | not handled in the protobuf handler |
| 24 | device_message_service | DeviceMessageService | W→H (`:262-264`) |
| 25, 26 | lte / anti_theft_alarm | | unused |
| 27 | credentials_service | GDI.Proto.Credentials.CredentialsService | W→H GCOAuthCredentialsRequest → optional fake reply (`:182-202`) |
| 28–38 | inreach_*, event_sharing, generic_item_transfer, hand_calibration, wind, simple_setup, switcher, launch_monitor | | unused |
| 39 | samd_service | EcgService (gdi_ecg_service) | W→H ECG zip transfer; **only when new sync is enabled** (`:248-258`) |
| 40, 41 | navigation_sync / kid_device | | unused |
| 42 | realtime_settings_service | RealtimeSettingsService | H→W init/definition/state; W responses (`:178-181`, `S/GarminSupport.java:564-578`) |
| **43** | **file_access_service** | **GDI.Proto.FileAccess.Service (gdi_file_access)** | **both directions, see §3** (`:232-247`) |
| 44 | service44 | Service44 | unknown |
| 45 | dezl_service | | unused |
| 46 | deeplinking_service | DeepLinkingService | unused |
| 47 | garmin_connect_service | GDI.Proto.GarminConnect.Service | unused |
| 48 | xero_chronograph_service | | unused |
| 49 | gncs_service | GDI.Proto.GNCS.Service | W→H MediaRequest (notification image via data_xfer) (`:538-579`) |
| 50, 52–71 | inreach_configuration, dive, messenger_on_device, companion_rangefinder, dog, live_session(56), radar, topside_buoy, iop_message, store_and_forward, heart_rate(61), running(62), accessory_utilities(63), bike_power, development_testing, xero_game_cam, connection_management, inreach_calls, live_sync, psa_sample, subscription(71) | | unused. Field 51 is not defined. |

Smart tag bytes for the services in this spec:
- field 2: `12`
- field 7: `3A`
- field 13: `6A`
- field 22: `B2 01`
- field 27: `DA 01`
- field 39: `BA 02`
- field 43: `DA 02`

---

## 3. New file sync (FileAccess) end to end

### 3.1 Enabling it and capability discovery

**GB's switch.** GB uses FileAccess only when the per-device pref `new_sync_protocol` is on; the default is false (`S/GarminSupport.java:725-727`). Coordinators for Venu X1, Venu 4, Vivoactive 6 and Cirqa set it to true and also force `garmin_mlr = true`:
- `#5021` ("Some new devices like Venu X1 miss a lot of files without the new sync protocol") has `TODO: check presence of GarminCapability.SYNC_2 instead` (`D/GarminCoordinator.java:124-131`).
- "New sync protocol without MLR fails to sync large workouts" has `TODO: check DeviceInformationMessage.protocolVersion >= 150 instead` (`D/GarminCoordinator.java:129-131`).
- These defaults were added in `5b7360235a` "Garmin CIRQA: Experimental support".
- **The Fenix 8 coordinator does not override the default, so it uses legacy sync.**

**SYNC_2** is capability bit **90**: byte 11, bit 2 (LSB-first) of the GFDI Configuration capability bitmap (`D/GarminCapability.java:127,204-219`).
- GB advertises every capability it knows, SYNC_2 included (`D/GarminCapability.java:172-176`).
- garmin-proto-lab agrees: bit 90 is the runtime selector for FileAccess, and Garmin Connect also advertises it when it wants FileAccess (`GPL/spec/PROTOCOL.md:443,846`).

**FileAccess `Capabilities`** (`P/gdi_file_access.proto:22-33`) travels inside Core `FeatureCapabilitiesRequest` / `FeatureCapabilitiesResponse` field 16 (`P/gdi_core.proto:92,137`). That is CoreService fields 8 and 9 (`P/gdi_core.proto:50-51`).
- **GB never sends a FeatureCapabilitiesRequest.** No caller exists; the grep found no `setFeatureCapabilities` anywhere.
- garmin-proto-lab gates the exchange on peer bit **95** (FEATURE_CAPABILITIES_EXCHANGE, `D/GarminCapability.java:132`). It uses it only for optional checksum and custom-flag metadata (`GPL/spec/PROTOCOL.md:846,989`).

### 3.2 FileAccess `Service` (Smart field 43)

Proto2, package `GDI.Proto.FileAccess` (`P/gdi_file_access.proto:263-289`). Every field is `optional`. Field 8 is not defined.

| # | Field | Type | Direction (GB) |
|---|---|---|---|
| 1 | pull_item_request | PullItemDataRequest | H→W |
| 2 | pull_item_response | PullItemDataResponse | W→H (in 5044) |
| 3 | push_item_request | PushItemDataRequest | H→W (uploads) |
| 4 | push_item_response | PushItemDataResponse | W→H |
| 5 | transfer_status_request | TransferStatusRequest | W→H (needs a reply) |
| 6 | priority_update_request | PriorityUpdateRequest | H→W (GB unused) |
| 7 | priority_update_response | PriorityUpdateResponse | W→H |
| 9 | item_list_request | ItemListRequest | H→W |
| 10 | item_list_response | ItemListResponse | W→H |
| 11 | item_list_cancel_notification | ItemListCancelNotification | W→H (GB ignores) |
| 12 | item_added_notification | ItemAddedNotification | W→H |
| 13 | delete_item_request | DeleteItemRequest | H→W (GB unused) |
| 14 | delete_item_response | DeleteItemResponse | W→H |
| 15 | modify_flags_request | ModifyFlagsRequest | H→W |
| 16 | modify_flags_response | ModifyFlagsResponse | W→H |
| 17 | item_updated_notification | ItemUpdatedNotification | W→H |
| 18 | cancel_transfer_request | CancelTransferRequest | H→W (GB unused) |
| 19 | cancel_transfer_response | CancelTransferResponse | W→H |
| 20 | resource_update_notification | ResourceUpdateNotification | W→H (no-op) |
| 21 | transfer_status_response | TransferStatusResponse | H→W |
| 22 | sync_button_notification | SyncButtonNotification (empty) | W→H: user pressed sync |
| 23 / 24 | software_update_part_number_request / _response | | unused |
| 25 / 26 | get_item_checksum_request / _response | | GB unused |

GB dispatch is in `S/FileSyncServiceHandler.kt:32-47`. When new sync is off, everything except `push_item_response` is ignored (`S/ProtocolBufferHandler.java:232-247`).

### 3.3 Item identity and type (DataType / ItemReference)

**`UUID`** (`P/gdi_data_types.proto:140-143`): `required fixed64 most_significant = 1; required fixed64 least_significant = 2;`. Both are wire type 1, tags `09` and `11`, 8 bytes LE each.
- The top 32 bits of `most_significant` are a Garmin timestamp of the file. Garmin epoch = Unix 631065600 (`S/GarminSupport.java:2138-2143`, `S/GarminTimeUtils.java:8`).
- HRM 600 only: `least & 0xFFFFFFFF` equals the file size (`HRM/docs/gfdi-filetransfer.md:149-150`).

**`ItemReference`** (`P/gdi_file_access.proto:155-169`):

| # | Field | Type | Notes |
|---|---|---|---|
| 1 | uid | UUID | primary key |
| 2 | data_type | DataType | |
| 3 | data_size | uint64 | clear (decompressed) size (`GPL/spec/PROTOCOL.md:944`) |
| 4 | requested_flags | uint32 | "0 on venu 4" (old notes, `git show 4ce9361642^:…gdi_file_sync_service.proto:186`) |
| 5 | transaction_id | uint32 | per-item, incrementing (33652, 33653, …) (same:185) |
| 6 | urgency | enum Urgency {IMMEDIATE=0, NORMAL=1, SLOW=2} | "2 on venu 4" |
| 7 | modified_time | uint32 | Garmin timestamp; sent when MODIFIED_TIME metadata is requested |

**`DataType`** (`P/gdi_file_access.proto:35-56`):

| # | Field | Type | Notes |
|---|---|---|---|
| 1 | format | enum Format | GDXML_DATA_TYPE=0, SOFTWARE_UPDATE=1, SPECIAL_CASE=2, GDXML_FULL_FILE=3, DATA_XFER=4, GDXML_UPDATE_FILE=5. Old notes: "0 fit, 3 binary" |
| 2 | gdxml_string | string | type name, e.g. "FIT_TYPE_4" |
| 3 | gdxml_string_key | uint32 | session-ephemeral key for the string. Old notes: "8 monitor, 9 sports"; "mapping is ephemeral" |
| 4 | gdxml_file_identifier | string | |
| 5 | software_update_path | string | |
| 6 | special_case | enum SpecialCase {COUNTER_BYTE_TEST=1} | |
| 7 | update_part_number | string | |

**Name resolution:**
- Only the first item of a given type carries `gdxml_string`. Later items carry only `gdxml_string_key`. GB keeps the key→string table **per response page only** (`S/FileSyncServiceHandler.kt:135-153`). garmin-proto-lab keeps it across the whole listing transaction (`GPL/spec/PROTOCOL.md:868`). **Keep it per session.**
- With no name: `computedName = "TYPE_<key>"`, or "DEVICE_XML" when key == 3 (`S/FileSyncServiceHandler.kt:248-264`). This is a rename bug; see Open questions.

**Mapping names to Garmin file types.**
- FIT files use `gdxml_string = "FIT_TYPE_<n>"`, where n is the legacy FIT subtype under datatype 128 (`S/FileType.java:226-232,59-156`).
- Types GB pulls (`pull=true`): 4 ACTIVITY, 9 WEIGHT, 15 MONITOR_A, 28 MONITOR_DAILY, **32 MONITOR**, 35 SEGMENT_LIST, 38 SCORE, 41 CHANGELOG, 44 METRICS, **49 SLEEP**, 52 USER_BEHAVIOR_LOG, 57 SPORTS_BACKUP, 58 DEVICE_58, 61 ECG, 66, **68 HRV_STATUS**, 70 HSA, 71 COM_ACT, 72 FBT_BACKUP, 73 SKIN_TEMP, 74 FBT_PTD_BACKUP, 77 SCHEDULE, 79 SLP_DISR, 82 AREA_COURSES (`S/FileType.java:63-140`).
- Non-FIT names GB pulls: ErrorShutdownReports, IQErrorReports, GOLF_SCORECARD, ULFLogs, KPI, ACTIVITY_GCPD, BACKUP_PRIMARY, BACKUP_SUPPLEMENTARY, BLELogs, FitnessHistory, GPSData, RAMDump, WELLNESS_TYPE_1 (`S/FileType.java:164-217`).
- Matching is case-insensitive (`S/FileType.java:260-270`).
- garmin-proto-lab confirms FIT_TYPE_4 = activity, 32 = monitoring, 49 = sleep (`GPL/spec/PROTOCOL.md:874`).

### 3.4 ItemListRequest / ItemListResponse

**`ItemListRequest`** (`P/gdi_file_access.proto:121-133`):

| # | Field | Type | GB value |
|---|---|---|---|
| 1 | session_id | uint32 | set to the previous response's `session_id` to continue a listing |
| 2 | transaction_id | uint32 | the previous listing's `next_transaction_id`, only when not continuing a session |
| 3 | max_item_count | uint32 | not set. Old note: per page, not a total; pages are about 100 items |
| 4 | exclude_all_flags | repeated UUID | `[{most=0xA5A5, least=0xA5A5}]` ("synced") |
| 5 | get_flags | repeated UUID | `[{0xA5A5, 0xA5A5}]` |
| 6 | included_data_types | repeated DataType | not set (all types) |
| 7 | requested_metadata | repeated enum {MODIFIED_TIME=0} (unpacked) | not set |

- GB builds this in `S/FileSyncServiceHandler.kt:178-197`, with `FLAGS_SYNCED = 42405L` (0xA5A5) at `:307`.
- Without field 4 the watch returns files that were synced months ago (`:182`, commit `56fde01c27`).

**`ItemListResponse`** (`:135-153`):

| # | Field | Type | Notes |
|---|---|---|---|
| 1 | status | enum | SUCCESS=0, FAIL_UNKNOWN_SESSION_ID=1, FAIL_TOO_MANY_EXCLUDE_FLAGS=2, FAIL_TOO_MANY_GET_FLAGS=3, FAIL_OTHER=4, FAIL_TOO_MANY_INCLUDE_TYPES=5, FAIL_INVALID_DATA_TYPE=6 |
| 2 | session_id | uint32 | present only when more items remain in **this** listing. Old notes: "2 on venu 4, 1 on venu 3"; mutually exclusive with next_transaction_id |
| 3 | next_transaction_id | uint32 | e.g. 33737, incrementing |
| 4 | items | repeated ItemReference | |
| 5 | error_code | uint32 | |
| 6 | abandoned_session_id | uint32 | "1 on venu 4" |
| 7 | malformed_data_type | DataType | |

**Paging rules in GB** (`S/FileSyncServiceHandler.kt:118-176`):
1. Each item is filtered and queued as it arrives.
2. `nextPageId = next_transaction_id`. In proto2 an absent field reads as **0**, so after any response GB sends `transaction_id = 0` in the next fresh listing.
3. **Keep pulling within a cursor.** If `session_id` is present, GB immediately sends another ItemListRequest with `session_id` (plus the same flags and no transaction_id). It repeats until a page arrives without `session_id`. Before this fix, anything past about 100 items, "which can easily be all SPORTS backlog", was never seen (`:163-173`, commit `56fde01c27`).
4. **#5461** ("some watches do not send the next page ID"). The fallback "use the max `ItemReference.transaction_id` seen" was added in `752ea8b992`, but **its code was removed in `56fde01c27`**. Only the comment remains (`:158-161`).
5. A non-SUCCESS status is only logged. Items and the cursor are still processed (`:119-125`).
6. State lives in memory only, so a new connection starts without session or transaction ids and relies on the synced-flag exclusion.

garmin-proto-lab agrees on paging, but it sends **only** `session_id` on continuation (`GPL/spec/PROTOCOL.md:868`, `GPL/src/garmin_proto_lab/file_access_client.py:180-196`).

**Example bytes, first GB listing** (Smart, 45 bytes, my encoding):
```
DA 02 2A  4A 28  22 12 09 A5 A5 00 00 00 00 00 00 11 A5 A5 00 00 00 00 00 00
                 2A 12 09 A5 A5 00 00 00 00 00 00 11 A5 A5 00 00 00 00 00 00
```
The continuation inserts `08 <session_id varint>` after `4A` and adjusts the lengths. Wrapped in GFDI it is `41 00 B3 13 <id u16> 00 00 00 00 2D 00 00 00 2D 00 00 00 <45 bytes> <crc>`.

### 3.5 Pulling an item: PullItemDataRequest / PullItemDataResponse

**`PullItemDataRequest`** (`P/gdi_file_access.proto:213-219`):

| # | Field | Type | GB value |
|---|---|---|---|
| 1 | item | ItemReference | the listed item (see note) |
| 2 | priority | uint32 | 24 |
| 3 | supported_transports | repeated enum TransportProtocol (unpacked) | `[MULTILINK_TRANSPORT_PIPE=0]` |
| 4 | offset | uint64 | 0 (clear-file resume offset per garmin-proto-lab) |
| 5 | requested_compression_window | uint32 | 15 |

- GB values: `S/FileSyncServiceHandler.kt:199-215`. Tail bytes after `item`: `10 18 18 00 20 00 28 0F`.
- Priority bands: `TransferPriorityLevel` LOWEST 0-9, UTILITY 10-19, STANDARD 20-29, USER_INITIATED 30-39, HIGHEST 40-49 (`:312-323`). garmin-proto-lab uses 20 (`GPL/src/garmin_proto_lab/file_access_client.py:204`).
- **Note on `item`:** if the listed `data_type` lacked `gdxml_string`, or it differed from the canonical name, GB replaces `data_type` with `{gdxml_string: <name>}` **only**. The uid and other fields are kept (`S/FileSyncServiceHandler.kt:284-293`).

**`PullItemDataResponse`** (`:221-231`):
- 1 result (`ItemAccessResult`: SUCCESS=0, ITEM_TYPE_NOT_SUPPORTED=1, NO_SUPPORTED_TRANSPORT=2, NO_AVAILABLE_RESOURCES=3, INTERNAL_ERROR=4, INVALID_PARAMS=5, INVALID_OFFSET=6, AMBIGUOUS_DATA_TYPE=7, UNKNOWN_ITEM=8, NO_SPACE=9, DATA_TYPE_ERROR=10, CONFLICTING_PUSH_REQUEST=11; `:98-111`)
- 2 transport (enum)
- 3 transfer_handle uint32
- 4 file_path string
- 5 error_code **sint32** (zigzag)
- 6 error_source uint32
- 7 compression_window uint32 (old note "11")
- 8 data_specific_error_source uint32
- 9 data_specific_error_code **sint32**

**Push messages are numbered differently.** Pull logic cannot be reused for them:
- `PushItemDataRequest` has no `offset`, so its field 4 is `requested_compression_window`.
- `PushItemDataResponse` puts `offset` at field 2, so every later field is one higher than in
  `PullItemDataResponse` (`transfer_handle` 4, `compression_window` 8).
- `pkg/garmin/smart` keeps separate types for the two directions. Its schema test checks every
  field number against `gdi_file_access.proto`.

GB handling: a non-SUCCESS result counts as a failed download, and the queue moves to the next file. SUCCESS leads to `downloadFileFromServiceV2(transfer_handle)` (`S/FileSyncServiceHandler.kt:102-116`, `S/GarminSupport.java:614-621`). On HRM 600, result 3 means the item was evicted from a ring buffer (`HRM/docs/gfdi-filetransfer.md:157-161`).

### 3.6 Moving the bytes: MultiLink "transport pipe" (not DOWNLOAD_REQUEST)

The file bytes **never** travel in GFDI or DOWNLOAD_REQUEST. They go over a separate MultiLink (ML) file-transfer service on the V2 ML GATT service `6A4E2800-667B-11E3-949A-0800200C9A66` (`S/communicator/v2/CommunicatorV2.java:67`). GB requires `CommunicatorV2`; otherwise it logs "Communicator is not V2" and the item stalls (`S/GarminSupport.java:2027-2032`).

1. **Register a pipe service.** Take the first service id not currently registered from `[0x2018, 0x4018, 0x6018, 0xA018, 0xC018, 0xE018]` (`S/communicator/v2/CommunicatorV2.java:245-276,770-775`). For one download at a time this is 0x2018 (`HRM/hrm600/filesync.py:28` uses the same list).
   - garmin-proto-lab lists **0x8018** as well (`GPL/spec/PROTOCOL.md:887`).
   - Register request: `00 00 | client_id u64 | service u16 | flags u8`, with flags `02` for reliable/MLR when the `garmin_mlr` pref is on (default true) and `00` otherwise (`S/communicator/v2/CommunicatorV2.java:677-685`, `S/GarminSupport.java:729-731`).
   - GB's client_id is 2 (`:85`). garmin-proto-lab says Garmin Connect uses 1 (`GPL/spec/PROTOCOL.md:889`).
   - Response type 1: `service u16 | status u8 | handle u8 | reliable u8` (`:356-431`).
   - ML handle details and MLR framing (2-byte header, `MLR_FLAG 0x80`, `#5476`) belong to the BLE transport team (`S/communicator/v2/MlrCommunicator.java:22-33`, `GPL/spec/PROTOCOL.md:893-930`).
2. **Configure the pipe.** Write 6 bytes on the new handle: `00 | 00 | transfer_handle u16 | 00 00` (`S/GarminSupport.java:2039-2049`). The second byte is the direction: 0 = read/pull, 1 = write/push (`:2380-2387`, `GPL/spec/PROTOCOL.md:942`).
   - garmin-proto-lab instead encodes `00 | dir | transfer_handle u64` in 10 bytes, statically derived (`GPL/src/garmin_proto_lab/file_access_proto.py:999-1010`). GB's 6-byte form is proven on hardware and HRM600 uses the same (`HRM/hrm600/filesync.py:73-75`).
3. **Configure reply from the watch.** GB requires the first message on the pipe to be at most 3 bytes and all zero, `00 00 00` (`S/GarminSupport.java:2197-2209`).
   - garmin-proto-lab decodes it as `echo | general_status | configure_status`. General: 0 OK, 1 unknown command, 2 invalid data. Configure: 0 OK, 1 unknown transfer handle, 2 unexpected direction, 3 unexpected size (`GPL/spec/PROTOCOL.md:942`).
4. **Data.** Every later message's payload is appended in order. With MLR the payload is the in-sequence data after the 2-byte header.
5. **End.** GB treats the watch closing the handle as the end: management `00 03 | client_id u64 | service u16 | handle u8 | status u8`, "CLOSE_HANDLE_RESP" (`S/communicator/v2/CommunicatorV2.java:433-454`, `HRM/docs/gfdi-filetransfer.md:180-181`). On that close GB inflates (`S/GarminSupport.java:2051-2066`).
   - garmin-proto-lab ends instead when the clear size is reached and the zlib stream has ended, then waits for the watch's TransferStatusRequest (`GPL/spec/PROTOCOL.md:944`).
6. **Decompression.** GB always runs a zlib (RFC 1950) inflate (`java.util.zip.Inflater()`, `S/../../../util/CompressionUtils.kt:31-46`, called at `S/GarminSupport.java:2060`).
   - HRM 600 sends **raw, uncompressed** data (`HRM/docs/gfdi-filetransfer.md:176-179`).
   - garmin-proto-lab inflates only if `PullItemDataResponse.compression_window` is present, and checks it is 9..15 (`GPL/src/garmin_proto_lab/file_access_client.py:233-239,500`).
   - **Recommendation:** inflate iff `compression_window` is present (or the first two bytes form a valid zlib header). Check the inflated length equals `data_size`. In Go, `compress/zlib` handles windows up to 32 KiB.
7. **TransferStatusRequest** (W→H, field 5; `P/gdi_file_access.proto:325-342`). Fields: uid UUID=1, transfer_handle=2, failure_reason=3 (TRANSPORT_FAILED=0, HIGHER_PRIORITY_REQUEST_RECEIVED=1, ITEM_DELETED=2, CANCELLED_BY_CLIENT=3, CANCELLED_BY_DEVICE=4, COMPRESSION_FAILED=5), transport_status=4, transport_provider_id=5, transport_error_code=6, new_priority=7.
   - GB always answers with 5044 carrying `TransferStatusResponse{next_transfer_priority(1)=1}`, Smart bytes `DA 02 05 AA 01 02 08 01`. It does nothing else, even when a failure_reason is present (`S/FileSyncServiceHandler.kt:78-86`).
   - HRM600 sees this field-5 message ({uid, handle}) at grant time and uses it to bind handles to items. It calls it a "grant notification" (`HRM/docs/gfdi-filetransfer.md:124,157-159`).
8. **Storage.** FIT files go to the importer. Other files are written using the type name, the date (modified_time, else uid>>32) and the uid (`S/GarminSupport.java:2081-2178`).

### 3.7 After the download: archive, delete, checksum

**ModifyFlagsRequest** (`P/gdi_file_access.proto:177-181`): uid UUID=1, set_flags repeated UUID=2, clear_flags repeated UUID=3.
- GB sends `{uid, set_flags:[{0xA5A5, 0xA5A5}]}` after a successful save, unless "keep activity data on device" is set (`S/GarminSupport.java:2180-2187`, `S/FileSyncServiceHandler.kt:217-244`).
- With "fetch unknown files" on, only known pull types get archived (#5824, commit `d085e6ae72`).
- Bytes: `DA 02 2A 7A 28 0A 12 09 <most 8B> 11 <least 8B> 12 12 09 A5 A5 00×6 11 A5 A5 00×6`.

**ModifyFlagsResponse** (`:183-196`): status (SUCCESS=0, FAIL_ITEM_NOT_PROVIDED=1, FAIL_ITEM_NOT_FOUND=2, FAIL_FLAGS_NOT_PROVIDED=3, FAIL_MAX_FLAGS_REACHED=4, FAIL_OTHER=5), error_source=2, error_code=3. GB only logs it (`S/FileSyncServiceHandler.kt:72-76`).

**ItemUpdatedNotification** (`:171-175`): uid=1, changed_flags_set=2, changed_flags_clear=3. GB: no-op (`S/FileSyncServiceHandler.kt:66-70`).

**DeleteItemRequest** (`:58-61`): uid=1, direction=2 (TransferDirection PUSH=0, PULL=1). **DeleteItemResponse** (`:63-77`): result (SUCCESS=0, ITEM_DOES_NOT_EXIST=1, ITEM_CANNOT_BE_DELETED=2, TRANSFER_IN_PROGRESS=3, DELETE_FAILED=4, INVALID_PARAMS=5, OPERATION_NOT_SUPPORTED=6), error_code=2, error_source=3. GB never uses these, and Garmin Connect did not send one in our capture (05). `FileAccess.Capabilities` has no flag for it, so only the response tells whether a watch deletes. goarmin sends `{uid, direction: PULL}` (`fileaccess.DeleteRequest`, `Engine.Delete`, `Options.Delete`); the Fenix 8 result goes in 06.

**GetItemChecksumRequest** (`:79-81`): uid=1. **GetItemChecksumResponse** (`:83-96`): uid=1, result=2 (UNSPECIFIED=0, SUCCESS=1, FAIL_ITEM_DOES_NOT_EXIST=2, FAIL_ITEM_SIZE_TOO_LARGE=3, FAIL_INTERNAL_ERROR=4, FAIL_DEVICE_IS_BUSY=5), checksum_truncated_md5 **fixed64**=3.
- GB never uses these.
- Per garmin-proto-lab: `wire_u64 = LE uint64 of MD5(clear file)[0:8]`, requested only if `Capabilities.server_file_checksum_method == TRUNCATED_MD5(1)` and the size is at most `checksum_max_file_size_byte` (`GPL/spec/PROTOCOL.md:950-963`).

**CancelTransferRequest** (transfer_handle=1) / **CancelTransferResponse** (status SUCCESS=0, UNKNOWN_TRANSFER=1) (`:9-20`). Not used by GB. garmin-proto-lab issues it on local failure and treats both results as success (`GPL/spec/PROTOCOL.md:870,948`).

**Notifications from the watch:**
- **ItemAddedNotification** (items repeated=1): each item goes to the same filter and queue (`S/FileSyncServiceHandler.kt:88-100`).
- **SyncButtonNotification**: start a full fetch (`:59-63`).
- **ResourceUpdateNotification** (status: CURRENT_PRIORITY_LOWERED=1, FILE_SPACE_FREED=2; current_priority=2): no-op (`:49-57`).
- **ItemListCancelNotification** (session_id=1): GB does not handle it and does not clear its cursor.

**ECG (Smart 39, gated by new sync):** the watch sends `EcgTransferStartReq` and GB replies `EcgTransferStartAck{unk2=1}`. The watch then sends `EcgFile{data=1 bytes, name=2 "*.zip"}` inline (`S/EcgServiceHandler.kt:23-37,45-46`, `P/gdi_ecg_service.proto:7-42`).

### 3.8 One complete GB new-sync run, in order

1. The connection is up: ML GFDI (V2), handshake done, DEVICE_INFORMATION has set the chunk size, Configuration exchanged.
2. The trigger is a user or automatic fetch (`S/GarminSupport.java:678-723`) or a watch `SyncButtonNotification`.
3. **Legacy flush.** GB sends DOWNLOAD_REQUEST for directory index 0 so that "the watch flushes the data, otherwise we might get incomplete monitor files" (`S/GarminSupport.java:716-718`).
   - The `garmin_legacy_sync_flush` pref (default true) can turn this off for #6700 ("some firmwares freeze on the legacy sync request"). GB then sends ItemListRequest directly (`:689-697`, `D/GarminDeviceSettings.kt:171-179`).
4. When the directory download completes, its entries are **ignored** and ItemListRequest is sent (`S/FileTransferHandler.java:252-259`, `S/GarminSupport.java:323-331`).
5. 5043 ItemListRequest → watch long ACK → watch 5044 ItemListResponse → GB ACK.
6. Items are filtered by type and queued. If `session_id` is present, go back to step 5 with `session_id`.
7. The queue is processed at the end of every incoming GFDI message (`S/GarminSupport.java:532,1200-1269`). The first queued item gets a 5043 PullItemDataRequest.
8. The watch answers with 5044 PullItemDataResponse. On SUCCESS GB registers the ML pipe (§3.6 step 1).
9. Configure `00 00 H16 00 00`, receive `00 00 00`, receive the data, and answer TransferStatusRequest whenever it arrives.
10. The watch closes the handle. GB inflates, saves and sends ModifyFlagsRequest. The watch sends ModifyFlagsResponse, which also restarts the queue for the next item.
    - Caveat: the queue only advances on an incoming GFDI message, so with "keep data on device" it can stall until some other message arrives.
11. When the queue is empty, GB processes the FIT files, then sends GFDI SystemEvent **SYNC_COMPLETE** (5030) (`S/GarminSupport.java:1271-1341`). If enabled, ExploreSync starts next (`:1336-1340`).

---

## 4. ExploreSync (Smart field 22): summary

ExploreSync is the Garmin Explore catalog channel (`P/gdi_explore_sync.proto:80-110`): lines/tracks, collections and waypoints, using version stamps and digest sync.
- GB starts it after file sync with a `StartSyncRequest{sync_type FULL=1 | INCREMENTAL=2, protocol_version=2, app_uuid=<16 bytes>}` (`S/ExploreSyncHandler.java:766-783`). GB's 16 bytes are ASCII "Gadgetbridge GB!" (`:147-148`); pick your own.
- It fetches only `LINE_TYPE_ACTIVITY` lines (POINTS, then SUMMARY/STAT/SPORT parts), writes them as GPX and stores activity summaries (`:77-82,288-291,380-506`).
- It sends empty acknowledgements for collection and waypoint traffic (`:807-838`).
- It is gated on watch capability EXPLORE_SYNC (bit 69) and the pref (default true) (`D/GarminCoordinator.java:136-147`, `S/GarminSupport.java:1336-1340`).
- **A FIT-first client can ignore it.** The data duplicates the activity FIT files.
- If the watch starts it: with the feature disabled, GB acks `DISCARDED/100` (`S/ProtocolBufferHandler.java:265-275,285-295`). A cleaner option is a 5044 `StartSyncResponse{status(1)=START_SYNC_REJECTED(2)}` (`P/gdi_explore_sync.proto:17-20,130-148`).

---

## 5. HTTP bridge (gdi_http, Smart field 2): summary

### 5.1 Messages

All proto2, package `GDI.Proto.ConnectIQHTTP` (`P/gdi_http.proto`).

**`ConnectIQHTTPService`** (`:73-85`): 1 connect_iq_http_request, 2 connect_iq_http_response, 3 connect_iq_image_request, 4 connect_iq_image_response, 5 raw_resource_request, 6 raw_resource_response, 7 connect_iq_oauth_request, 8 connect_iq_oauth_response, 9 connect_iq_oauth_complete_request, 10 connect_iq_oauth_complete_response, 11 open_webpage_notification.

**`RawResourceRequest`** (`:185-220`). Used by firmware features: weather, AGPS, OAuth, image service, contacts, hydration.
- 1 url (required)
- 2 max_size uint32
- 3 method (UNKNOWN 0, GET 1, PUT 2, POST 3, DELETE 4, PATCH 5, HEAD 6)
- 4 body string
- 5 headers repeated {1 key, 2 value} (both required)
- 6 use_data_xfer bool
- 7 raw_body bytes
- 8 forms repeated MultipartForm{name, filename, content_type, resource_data, xfer_data}
- 9 connection_timeout_seconds int32
- 10 read_timeout_seconds int32
- 11 encrypted_request EncryptedEnvelope{cipher_text, iv, tag}

**`RawResourceResponse`** (`:222-242`):
- 1 status (UNKNOWN 0, **OK 100**, **NETWORK_REQUEST_TIMED_OUT 200**, FILE_TOO_LARGE 300, DATA_TRANSFER_ITEM_FAILURE 400, DECRYPTION_FAILURE 500)
- 2 http_status_code int32
- 3 resource_data bytes
- 4 xfer_data DataTransferItem{required id=1, required size=2} (`P/gdi_data_types.proto:31-34`)
- 5 headers repeated {key, value}

**`ConnectIQHTTPRequest`** (`:17-49`). Used by Connect IQ `makeWebRequest`.
- 1 url (required)
- 2 http_method
- 3 http_header_fields bytes (**Monkey-C serialized map**)
- 4 http_body bytes (Monkey-C serialized)
- 5 max_response_length
- 6 include_http_header_fields_in_response
- 7 compress_response_body
- 8 response_type (JSON 0, URL_ENCODED 1, PLAIN_TEXT 2, XML 3)
- 9 version

**`ConnectIQHTTPResponse`** (`:51-71`):
- 1 status (UNKNOWN 0, OK 100, INVALID_HTTP_HEADER_FIELDS_IN_REQUEST 200, INVALID_HTTP_BODY_IN_REQUEST 201, INVALID_HTTP_METHOD_IN_REQUEST 202, **NETWORK_REQUEST_TIMED_OUT 300**, INVALID_HTTP_BODY_IN_NETWORK_RESPONSE 400, INVALID_HTTP_HEADER_FIELDS_IN_NETWORK_RESPONSE 401, NETWORK_RESPONSE_TOO_LARGE 402, INSECURE_REQUEST 1001)
- 2 http_status_code int32
- 3 http_body (Monkey-C serialized)
- 4 http_header_fields
- 5 inflated_size (0 means uncompressed)
- 6 response_type

**Image, OAuth and webpage messages** are defined at `:87-183`.

**Monkey-C serialization ("GarminJson")**, big-endian (`S/http/GarminJson.java:43-56,58-101,103-183`):
- Optional string section: magic `AB CD AB CD`, u32 length, then entries `[u16 len incl. NUL][utf8][00]`.
- Data section: magic `DA 7A DA 7A`, u32 length, then values breadth-first, each a tag plus payload:
  - 00 null
  - 01 sint32
  - 02 float
  - 03 string (u32 offset into the string section)
  - 05 array (u32 count)
  - 09 bool
  - 0B map (u32 count; key/value pairs)
  - 0E sint64
  - 0F double
  - 13 char
  - 14 byte array
- The Connect IQ Content-Type header arrives as enum text: "0" = form-urlencoded, "1" = JSON (`S/http/GarminHttpRequest.java:25-30,59-70`).

### 5.2 How GB processes requests

- The watch sends 5043 with Smart{2:{5|1|11}}. GB ACKs it, then answers with 5044 using the same request id. The answer is immediate, or asynchronous through ProtobufResponseEvent (`S/ProtocolBufferHandler.java:143-150`, `S/GarminSupport.java:579-586`).
- Interceptors are tried in order and the first whose `supports()` matches handles the request (`S/http/HttpHandler.java:60-71,127-151`):
  1. **Weather.** Host `api.gcs.garmin.com|.cn` or `cache.dciwx.com`, path `/weather/…`. Serves `/weather/v{1,2}/forecast/day`, `/forecast/hour`, `/current` and `/weather/pointWinds` as JSON 200 built from the phone's weather data. "These get requested on connection at most every 5 minutes" (`S/http/interceptors/WeatherInterceptor.java:64-180`).
  2. **AGPS.** Host `api.gcs.garmin.com|.cn`, path `/ephemeris/…`. Serves a locally stored file (`S/http/interceptors/AgpsInterceptor.java:59-143`):
     - `etag: "<md5hex>"`; a matching `if-none-match` gets 304 with an empty body.
     - `cache-control: max-age=14400`.
     - Content-Type is the request's `accept` header, or `application/octet-stream`.
     - The format is checked per URL: `constellations=` tar, `/rxnetworks/`, `/ephemeris/cpe/sony`.
     - A missing or invalid file gets no response.
  3. **ImageService** (`/image-service/`), **Contacts** (`connectapi.garmin.com /device-gateway/usercontact/`), **Hydration** (`connectapi.garmin.com` paths) (`S/http/interceptors/*`).
  4. **OAuth.** Any host with a path under `/api/oauth/`, `/oauth/` or `/oauthTokenExchangeService/`. POST only (`S/http/interceptors/OauthInterceptor.java:50-62`).
     - With fake OAuth **off** (the default; it needs both the warning and enable prefs, `S/GarminPrefs.java:17-20`), it returns nothing. Token-like paths also trigger a weekly "authentication expired" notification (`:64-73,199-238`).
     - With it on, it returns 200 JSON with random tokens, `expires_in 7776000`, `refresh_token_expires_in "31536000"` (a string), a long scope list and `customerId` (`:75-191`):
       - `/oauth/connect_exchange/token`: **snake_case** fields. Using camelCase here makes the watch drop the token, retry every minute and never request weather (#6537, `:123-140`).
       - `/oauthTokenExchangeService/connectToIT`: camelCase `accessToken`, `tokenType`, `refreshToken`, `expiresIn`, … (`:141-155`).
       - `/api/oauth/token` and `/oauth/refresh_token/token`: snake_case, echoing the `refresh_token` from the form body (`:156-191`).
  5. **Firewall** (always last; matches everything) (`S/http/interceptors/FirewallInterceptor.java:60-133`):
     - Requires phone internet, the per-device "internet access" pref (default **false**), and a matching ALLOW rule (default **BLOCK**) (`app/src/main/java/nodomain/freeyourgadget/gadgetbridge/internet/InternetFirewall.kt:47-58,63`).
     - `*.garmin.com`, `*.garmin.cn` and `*.dciwx.com` are **always blocked**, "due to fake OAuth" (`:78-84`).
     - Otherwise it forwards asynchronously and answers later (`:99-168`).
- **Result mapping when an interceptor returns nothing** (no interceptor, blocked, or a failure):
  - Raw: `RawResourceResponse{status=200 NETWORK_REQUEST_TIMED_OUT}` (`S/http/HttpHandler.java:112-116`).
  - Web: `ConnectIQHTTPResponse{status=300 NETWORK_REQUEST_TIMED_OUT, http_status_code=0}` (`:240-245`).
  - An exception gives `status UNKNOWN(0)` (`:253-258`). An asynchronous failure gives UNKNOWN (`:285-302`).
- **Response shaping** (`S/http/HttpHandler.java:153-218,304-373`):
  - Raw with `use_data_xfer=true`: the body is registered and the response carries only `xfer_data{id, size}`.
  - Raw with a request header `accept-encoding == "gzip"` (keys are lower-cased): the body is gzipped and `Content-Encoding: gzip` is added.
  - Web: the body is converted to Monkey-C.
    - A body longer than `max_response_length` gives 402.
    - A non-JSON body when JSON was expected gives 400.
    - Headers are included only if requested.
- **Not answered by GB:** raw requests with multipart `forms` (`:103-106`), ConnectIQImageRequest and OAuth requests over fields 3/7 (`:94-96`), and OpenWebpageNotification (`:220-229`). The 5043 is ACKed KEPT but no 5044 follows.

### 5.3 data_xfer, the large-payload path used by AGPS, images and http responses

**DataTransferService** (Smart 7; `P/gdi_data_transfer.proto:36-46`):
- `DataDownloadRequest{required id=1, required offset=2, max_chunk_size=3}` → `DataDownloadResponse{required status=1 (UNKNOWN 0, SUCCESS 1, INVALID_ID 2, INVALID_OFFSET 3), required id=2, required offset=3, payload=4}`.
- The chunk is marked delivered when the watch's long ACK for that 5044 is OK (`S/http/DataTransferHandler.java:55-88,134-155`, `S/ProtocolBufferHandler.java:300-309`).

**Push variant:**
1. The watch sends `FileAccessItemRequest{data_xfer_id=1, expected_latency=2}`.
2. GB replies `FileAccessItemResponse{status OK=1, data_xfer_id=2, file_access_item_uid=3 UUID{most=id, least=random}}` (`S/http/DataTransferHandler.java:90-108`).
3. After the watch ACKs that reply, GB pushes over MLR (`S/GarminSupport.java:2276-2343,2356-2424`):
   - FileAccess `PushItemDataRequest{item{uid, data_type{format DATA_XFER=4}, data_size}, priority 34, transports [0], requested_compression_window 15}` (`P/gdi_file_access.proto:233-238`).
   - `PushItemDataResponse` (`:240-251`) returns transfer_handle (field 4).
   - GB opens a reliable pipe and writes `00 01 H16 00 00`.
   - The watch replies with first byte `00`.
   - GB sends a zlib stream of **stored blocks** with a 1 KiB window, header bytes `28 15`. The watch rejects 32 KiB windows (`S/GarminSupport.java:2229-2233,2316`, `CompressionUtils.kt:53-78`).
   - GB closes the handle after every byte is MLR-acked.
4. Without MLR this push fails (`S/GarminSupport.java:2283-2287`).

---

## 6. Proto schema reference for Go

**General notes:**
- **Every GB `.proto` file is `syntax = "proto2"`** (all 38 files).
- Package names (`GDI.Proto.*`, `garmin_vivomovehr`) do not appear on the wire. Add your own `go_package`.
- Write your own `.proto` from these tables; the GB files are AGPL.
- For Go and protoc-gen-go:
  - Keep `required` where listed, or relax it to optional and unmarshal with `proto.UnmarshalOptions{AllowPartial:true}`. Marshalling a message with an unset `required` field returns an error.
  - Always set both UUID halves. Required enum fields with value 0 (for example `gcs_location = PRODUCTION(0)`) must still be emitted.
  - Keep the exact scalar types: `fixed64` (UUID halves, checksum), `sint32` (Pull/Push `error_code`, `data_specific_error_code`), and `uint64` (`data_size`, `offset`).
  - Repeated scalars and enums are **unpacked** (`[packed=false]`, the proto2 default). Send them unpacked; accept both.

**gdi_data_types.proto**
- `UUID{required fixed64 most_significant=1; required fixed64 least_significant=2}` (`P/gdi_data_types.proto:140-143`)
- `DataTransferItem{required uint32 id=1; required uint32 size=2}` (`:31-34`)

**gdi_file_access.proto.** Fully specified in §3.2–§3.7, in proto order:
- CancelTransferRequest/Response (`:9-20`)
- Capabilities (`:22-33`): server_push_file_bundle_support bool=1, software_update_part_number_request_support bool=2, server_file_checksum_method enum{NOT_SUPPORTED=0, TRUNCATED_MD5=1}=3, checksum_max_file_size_byte uint32=4, custom_flag_support bool=5
- DataType (`:35-56`)
- DeleteItem* (`:58-77`)
- GetItemChecksum* (`:79-96`)
- enum ItemAccessResult (`:98-111`)
- ItemAddedNotification{repeated ItemReference items=1} (`:113-115`)
- ItemListCancelNotification{uint32 session_id=1} (`:117-119`)
- ItemListRequest/Response (`:121-153`)
- ItemReference (`:155-169`)
- ItemUpdatedNotification (`:171-175`)
- ModifyFlags* (`:177-196`)
- PriorityUpdateRequest{transfer_handle=1, new_priority=2} / Response{status: SUCCESS=0, INVALID_PRIORITY=1, UNKNOWN_TRANSFER=2} (`:198-211`)
- PullItemData* (`:213-231`)
- PushItemData* (`:233-251`)
- ResourceUpdateNotification (`:253-261`)
- Service (`:263-289`)
- SoftwareUpdatePartNumberRequest{requestor enum DISPLAY=0, APP=1} / Response{repeated string part_number=1} (`:291-302`)
- SyncButtonNotification{} (`:304-305`)
- enum TransferDirection (`:307-310`)
- enum TransferPriorityLevel (`:312-323`)
- TransferStatusRequest (`:325-342`)
- TransferStatusResponse{uint32 next_transfer_priority=1} (`:344-346`)
- enum TransportProtocol{MULTILINK_TRANSPORT_PIPE=0} (`:348-350`)

**gdi_smart_proto.proto.** The `Smart` table in §2 (`P/gdi_smart_proto.proto:45-116`). For a minimal client, declare fields 2, 7, 13, 22, 27 and 43 and keep the rest as unknown fields.

**gdi_core.proto** (`P/gdi_core.proto`):
- `CoreService` (`:42-61`): 1 sync_request, 2 sync_response, 3 get_location_request, 4 get_location_response, 5 location_updated_set_enabled_request, 6 …_response, 7 location_updated_notification, 8 feature_capabilities_request, 9 feature_capabilities_response, 10/11 garmin_guid_overwrite req/resp, 12/13 dual_pairing req/resp, 14 connection_ready_notification (empty; **do not reply**, `S/ProtocolBufferHandler.java:526-532`, commit `721bb7487f`), 15/16 check_pending_software req/resp, 17 time_sync_request (empty), 18 time_sync_response{uint64 current_time=1}
- `FeatureCapabilitiesRequest` (`:81-119`): garmin_guid bytes=1, client_version uint32=2, display_name string=3, …, **request_file_access_capabilities FileAccess.Capabilities=16**, …
- `FeatureCapabilitiesResponse` (`:121-164`): guid_status enum{UNSET=0, MATCH=1, NO_MATCH=2}=1, version uint32=2, …, **response_file_access_capabilities=16**
- `SyncRequest{bool use_this_connection=1}` (`:271-273`)
- `SyncResponse{required status: UNKNOWN_RESPONSE_STATUS=0, OK=1, BUSY_WITH_ANOTHER_APP=2}` (`:275-283`)
- `GetLocationRequest/Response` (`:180-200`; Response `status` is required: OK=1 … TRY_AGAIN_LATER=5)

**gdi_data_transfer.proto.** All of it (`P/gdi_data_transfer.proto:9-109`), as in §5.3, plus:
- InitiateDataUploadRequest{required id=1}
- InitiateDataUploadResponse{required id=1, required status=2 (UNKNOWN=0, OK=1, INVALID_ID=2)}
- DataUploadRequest{required id=1, required offset=2, required bytes payload=3}
- DataUploadResponse{required id=1, required status=2 (UNKNOWN=0, SUCCESS=1, INVALID_ID=2, INVALID_OFFSET=3, SIZE_MISMATCH=4, ABORT=5)}
- DataUploadCanceledNotification{required id=1}
- CapabilitiesRequest{service_version=1}

**gdi_http.proto.** §5.1 (`P/gdi_http.proto:9-242`).

**gdi_credentials.proto**:
- `CredentialsService` (`:21-30`): 1 gc_oauth_request{} (empty), 2 gc_oauth_response, 3/4 di_oauth_2 req/resp, 5/6 oauth2 req/resp, 7/8 sip req/resp
- `GCOAuthCredentialsResponse{required OAuthCredentials oauth_credentials=1; required GCSLocation gcs_location=2}`
- `OAuthCredentials{required string consumer_key=1, consumer_secret=2, oauth_token=3, oauth_secret=4}`
- `GCSLocation{PRODUCTION=0, CHINA=1, TEST=2, STAGE=3}` (`:53-66,91-96`)
- GB's fake values: UUID strings, plus 35 random alphanumeric characters for the secrets (`S/ProtocolBufferHandler.java:182-202`).

**gdi_explore_sync.proto.** `ExploreSyncService` fields 1–19 and 24–33 (`:80-110`); StartSyncRequest/Response (`:117-148`); SyncFinishedNotification{status=1} (`:150-152`). Only needed to reject cleanly.

**gdi_ecg_service.proto.** EcgService{file_transfer=1}; EcgFileTransfer{3 init_request, 4 init_response, 5 transfer_start_req, 6 transfer_start_ack{unk2=2}, 7 ecg_file{data=1, name=2, unk3=3, unk4=4}, 8/9 finish notify/ack strings} (`P/gdi_ecg_service.proto:7-42`).

---

## 7. Workarounds and issue references

| Ref | Where | What |
|---|---|---|
| #5021 | `D/GarminCoordinator.java:126-128` | Venu X1 and others miss files under legacy sync, so new sync is the default there. TODO: detect SYNC_2 |
| (no #) | `D/GarminCoordinator.java:129-131`, commit `5b7360235a` | New sync without MLR fails on large workouts. TODO: use protocolVersion ≥ 150 |
| #5461 | `S/FileSyncServiceHandler.kt:158-161`, commits `752ea8b992` (added) and `56fde01c27` (removed) | Missing next page id; the max-transaction_id fallback code is gone, only the comment remains |
| (commit) | `56fde01c27` | `session_id` cursor appears when a listing has more than 100 items; keep pulling |
| `cb1a2050c6` | | Do not send transaction_id when it is unknown |
| `64c296f625` | `S/FileSyncServiceHandler.kt:307` | FLAGS_SYNCED must be a long, because UUID halves are fixed64 |
| `98e7aa2c39` | | PullItemDataResponse result 3 is a failure; continue with the next file |
| `9d8a4b3745` | `:78-86` | Answer TransferStatusRequest with next_transfer_priority 1 |
| #6700 | `S/GarminSupport.java:689-697` | Legacy flush can freeze some firmware; it is optional |
| #5824 | `S/FileType.java:140,144`, `d085e6ae72` | Archive some unknown files; Instinct runs out of memory otherwise |
| #5476 | `S/communicator/v2/CommunicatorV2.java:219` | A non-MLR handle can have the MSB set |
| #6537 | `S/http/interceptors/OauthInterceptor.java:124-126` | connect_exchange token must be snake_case JSON |
| `721bb7487f` | `S/ProtocolBufferHandler.java:526-532` | Do not reply to ConnectionReadyNotification; it confuses some firmware |
| `1efd451559` | `S/ProtocolBufferHandler.java:684-688` | Chunk size comes from max_packet_size − 20 |
| `9c4da444cf`, `709afc5df7` | | Chunk-map removal fixes; stop after the last chunk |
| `4ce9361642` / `8b4d1e9d52` | | Renamed to opengarmin names: cursor_id→session_id, next_page_id→next_transaction_id, File→ItemReference, FileType→DataType (code→format, name_id→gdxml_string_key) |

---

## 8. Cross-checks

- **garmin-proto-lab** agrees on:
  - the 14-byte protobuf header and status codes (`GPL/spec/PROTOCOL.md:792-802`);
  - Smart field 43 and Core capabilities field 16 (`:848`);
  - every FileAccess service field number (`:850-866`);
  - session and transaction paging (`:868`);
  - transport enum 0 (`:870`);
  - pipe direction 0/1, the configure reply and zlib (`:942-944`);
  - bit 90 / bit 95 (`:846`).

  It disagrees with GB on:
  - chunk overhead (−14 vs −20);
  - pipe configure (u64 handle in 10 bytes vs GB's 6 bytes);
  - service id 0x8018 (garmin-proto-lab includes it);
  - client id (1 vs GB's 2);
  - transfer end (byte count plus TransferStatus vs handle close);
  - continuation content (only session_id vs GB's flags too);
  - string-table scope (per transaction vs per page).

  garmin-proto-lab has not been tested on hardware (`GPL/spec/PROTOCOL.md:1144`).
- **GarminHRM600** (real HRM 600 captures):
  - confirms GB's 6-byte configure, the service list without 0x8018, the `00 00 00` first message and the end on CLOSE_HANDLE_RESP (`HRM/docs/gfdi-filetransfer.md:168-181`);
  - shows **uncompressed** payloads (`:176`);
  - shows that every chunk and every complete 0x2C frame must be ACKed, or the watch re-sends every ~5 s (`:141-156`);
  - shows field 5 (TransferStatusRequest) arriving with {uid, handle} around the pull and binding handle to file (`:124,157`).
- **garmin-ble** protos are an old GB snapshot (`GBLE/proto/gdi_file_sync_service.proto:6-83`). FileRequest unk3/4/5 are today's supported_transports/offset/requested_compression_window, and FileListResponse unk2/unk6 are session_id/abandoned_session_id. They are wire-compatible and add nothing new.

---

## 9. Minimum viable FileAccess download (checklist)

1. ☐ Use an ML (V2) GFDI link. Read `max_packet_size` from DEVICE_INFORMATION and set `maxChunk = max_packet_size − 20`.
2. ☐ Implement 5043/5044 send (stop-and-wait on the long status, check the echoed offset) and receive (reassemble by `(type, id)`, long ACK on **every** chunk and complete frame, 101/102/103/200 on errors). Answer 5045 by echoing the id.
3. ☐ Set capability bit 90 (SYNC_2) in the host Configuration, and use FileAccess when the watch sets bit 90. For Fenix 8, check against a capture.
4. ☐ Optional but GB's default: legacy directory flush (DOWNLOAD_REQUEST index 0) before listing.
5. ☐ Send `ItemListRequest{exclude_all_flags:[{0xA5A5, 0xA5A5}], get_flags:[{0xA5A5, 0xA5A5}]}`. While `session_id` is present, send `{session_id}` again. Keep the `gdxml_string_key → gdxml_string` table for the whole session. Keep `next_transaction_id`.
6. ☐ Select items by name: FIT_TYPE_4 (activity), 32 (monitor), 49 (sleep), 68 (HRV), plus 44, 28, 15 and others as wanted.
7. ☐ Send `PullItemDataRequest{item (as listed, with gdxml_string filled in), priority 24, supported_transports [0], offset 0, requested_compression_window 15}`. Check `result == SUCCESS` and `transport == 0`. Record `transfer_handle` and `compression_window`.
8. ☐ Register the first free pipe service from 0x2018, 0x4018, …, using the reliable (MLR) flag 0x02; GB says large workouts fail without it. Write `00 00 <handle u16> 00 00`. Expect `00 00 00`.
9. ☐ Append payloads until the watch closes the handle, or until `data_size` is reached after inflating. Answer every TransferStatusRequest with 5044 `TransferStatusResponse{next_transfer_priority:1}`. Treat a `failure_reason` as a failed transfer.
10. ☐ Inflate with zlib only if `compression_window` was returned (or a zlib header is detected). Check the length equals `data_size`. Validate the FIT header and CRC. Save durably.
11. ☐ Only then, optionally archive with `ModifyFlagsRequest{uid, set_flags:[{0xA5A5, 0xA5A5}]}` and check `ModifyFlagsResponse.status == 0`.
12. ☐ Handle ItemAddedNotification (enqueue), SyncButtonNotification (start a sync), and ItemListCancelNotification (drop the cursor). ResourceUpdateNotification and ItemUpdatedNotification are no-ops.
13. ☐ Drive the download queue yourself, without waiting for unrelated messages. Use your own timeouts (about 30 s). On failure, optionally send CancelTransferRequest and move to the next item.
14. ☐ Finish with GFDI SystemEvent SYNC_COMPLETE (the legacy team owns this).
15. ☐ Tests: build every timestamp (Garmin uid>>32, modified_time) from `now` at run time, never from fixed dates.

## 10. Minimum HTTP bridge behaviour

- ACK every 5043, and **always** send a 5044 with the same id. An unanswered request is re-sent about every 5 s.
- **"No network" replies:**
  - `RawResourceRequest` → `RawResourceResponse{status: 200 NETWORK_REQUEST_TIMED_OUT}`. Smart bytes `12 05 32 03 08 C8 01`.
  - `ConnectIQHTTPRequest` → `ConnectIQHTTPResponse{status: 300, http_status_code: 0}`. Smart bytes `12 07 12 05 08 AC 02 10 00`.
  - `ConnectIQImageRequest` → `ConnectIQImageResponse{status: 200 NETWORK_REQUEST_TIMED_OUT}` (GB leaves it unanswered).
  - `OpenWebpageNotification` → just ACK.
  This is exactly what GB does for blocked or unknown requests, and GB's default blocks everything except a few local fakes, so watches tolerate it.
- **OAuth.** `CredentialsService.gc_oauth_request` can be ignored. GB with fake OAuth off ACKs `DISCARDED/100`.
  - Without valid OAuth answers (or fake ones), Garmin-server features such as weather and AGPS will not be requested or used.
  - To provide weather locally: fake GCOAuth (with `gcs_location` emitted) and the OAuth token endpoints in the exact JSON shapes of §5.2. Then serve `/weather/*` JSON.
  - To provide AGPS: serve the files with ETag/304 handling, via `xfer_data` when `use_data_xfer` is set. That needs DataDownloadRequest support, or FileAccessItemRequest plus an MLR push.
- **Never forward to Garmin domains** (GB blocks them outright). If you forward anything else, replace the outgoing `User-Agent` with a shared, current desktop-browser UA constant, and add no identifying headers.

---

## 11. Open questions / uncertain

1. **The `gdxml_string_key == 3` check is a rename bug.** It was originally `FileType.code(field 1 = format) == 3` (GDXML_FULL_FILE → DEVICE_XML). Rename commit `4ce9361642` mapped it to field 3 (`S/FileSyncServiceHandler.kt:252-256`; compare `git show 714262fac5`). Likewise `"TYPE_"+key` was meant to be `"TYPE_"+format`.
2. Does any current firmware still omit `next_transaction_id` (#5461)? GB's fallback code has been removed.
3. Pipe configure: GB's 6-byte form (u16 handle + 2 zero bytes, proven on hardware) vs garmin-proto-lab's 10-byte form (u64 handle). Which does Fenix 8 require, or accept both?
4. End of transfer: handle close vs byte count. Also whether TransferStatusRequest arrives before or after the data (HRM600: around the grant; garmin-proto-lab: after), and what `next_transfer_priority` means. GB always replies 1.
5. Compression: is a zlib stream only sent when `compression_window` is returned? HRM 600 sends raw; GB always inflates.
6. Semantics of `get_flags`, `ItemReference.requested_flags` and `exclude_all_flags` ("exclude items having all listed flags"?). Why does the 0xA5A5/0xA5A5 UUID mean "synced", and does Garmin Connect use the same value? Related: `Capabilities.custom_flag_support`.
7. Continuation: send only `session_id` (garmin-proto-lab) or repeat the flags (GB)? Does ACK-before-continuation order matter?
8. Does the watch validate the `data_type` echoed in `PullItemDataRequest.item`? GB rewrites it to only `gdxml_string`.
9. 5045 semantics are known only from static analysis (garmin-proto-lab). GB ignores 5045 and never ACKs it.
10. Status code for an unknown service: GB uses `DISCARDED + 100 UNKNOWN_REQUEST_ID`, garmin-proto-lab says `201`. Either seems tolerated.
11. Does `max_packet_size` include length, type and CRC? (GB −20 vs garmin-proto-lab −14.)
12. ML client id 2 (GB) vs 1 (Garmin Connect). Is 0x8018 a valid pipe service on Fenix 8?
13. Fenix 8 specifics: does it set bit 90 / bit 95? Its protocolVersion (≥150?), its Capabilities (TRUNCATED_MD5?), and whether new sync without MLR truncates large FIT files on it. Needs a capture.
14. `ItemListResponse.abandoned_session_id` ("1 on venu 4") and `ItemListCancelNotification`: when to restart a listing.
15. DeleteItem, GetItemChecksum, CancelTransfer and PriorityUpdate have never been exercised by GB; their behaviour on hardware is unverified.
16. ExploreSync rejection: does `START_SYNC_REJECTED` stop the watch from retrying?
