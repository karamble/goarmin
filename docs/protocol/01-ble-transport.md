# 01 — BLE transport layer

Part of the goarmin protocol notes (`docs/protocol/`). Derived from Gadgetbridge at commit
`eb3550358d` (Codeberg), cross-checked against garmin-proto-lab, garmin-ble and garmin-bridge.
Written 2026-11-07. Describes the protocol, not Gadgetbridge's code; goarmin is a from-scratch
implementation. CRC and COBS claims were checked by the research agent with throwaway scripts;
re-derive example bytes in Go unit tests rather than copying them.

**Path prefixes used in citations** (all Gadgetbridge paths are relative to `~/go/src/codeberg.org/Freeyourgadget/Gadgetbridge` @ `eb3550358d`):

- `S/` = `app/src/main/java/nodomain/freeyourgadget/gadgetbridge/service/devices/garmin/`
- `C/` = `app/src/main/java/nodomain/freeyourgadget/gadgetbridge/devices/garmin/`
- `B/` = `app/src/main/java/nodomain/freeyourgadget/gadgetbridge/` (generic Gadgetbridge BLE core)
- `T/` = `app/src/test/java/nodomain/freeyourgadget/gadgetbridge/service/devices/garmin/`
- `R/` = `app/src/main/res/`
- `PL/` = `~/go/src/github.com/seofernando25/garmin-proto-lab/`
- `GBLE/` = `~/go/src/github.com/gwerneckp/garmin-ble/`
- `GBR/` = `~/go/src/github.com/wh1le/garmin-bridge/`

Layering, bottom to top:

1. GATT characteristic pair.
2. Multi-Link (ML) mux. Byte 0 is either a handle or an MLR header.
3. Per handle, one of three things:
   - a raw service payload (realtime services);
   - a raw byte stream;
   - an MLR reliable stream.
4. On the GFDI handle, a COBS-framed byte stream.
5. A GFDI frame: `len | type | payload | CRC16` (see `02-gfdi-session.md`; the CRC belongs to this layer).

---

## 1. GATT discovery, characteristic selection, notifications, MTU, writes

### 1.1 Services Gadgetbridge recognises

Gadgetbridge registers four "supported" services. Only characteristics of these services go into its UUID→characteristic map (`S/GarminSupport.java:251-254`, `B/service/btle/AbstractBTLESingleDeviceSupport.java:286-334`).

| Name | Service UUID | Host TX (write) | Host RX (notify) | Source |
|---|---|---|---|---|
| V2 Multi-Link (ML) | `6a4e2800-667b-11e3-949a-0800200c9a66` | `6a4e2820`…`6a4e2824` | `6a4e2810`…`6a4e2814` | `S/communicator/v2/CommunicatorV2.java:67-83` |
| V1 GFDI (Vivomove HR) | `6a4e2401-667b-11e3-949a-0800200c9a66` | `6a4e4c80-…` | `6a4ecd28-…` | `S/communicator/v1/CommunicatorV1.java:27-30` |
| V0 GFDI (Vivofit / FR620) | `9b012401-bc30-ce9a-e111-0f67e491abde` | `df334c80-e6a7-d082-274d-78fc66f85e16` | `4acbcd28-7425-868e-f447-915c8f00d0cb` | `S/communicator/v1/CommunicatorV1.java:22-25` |
| Connect IQ watch-app channel (not ML) | `6a4e8022-667b-11e3-949a-0800200c9a66` | `6a4e4c80-…` | `6a4ecd28-…` | `S/communicator/GarminWatchAppCommunicator.java:40-42` |

All 6a4e UUIDs share the suffix `-667b-11e3-949a-0800200c9a66`.

**Pitfall: UUID collision.** The watch-app characteristics use the same UUIDs as the V1 GFDI characteristics. Gadgetbridge resolves the watch-app ones through the service object (`S/communicator/GarminWatchAppCommunicator.java:110-133`, `S/GarminSupport.java:402-405`). V1 looks its characteristics up globally by UUID (`S/communicator/v1/CommunicatorV1.java:52-58`). A Go client must resolve characteristics **per service**.

### 1.2 Protocol selection order (Gadgetbridge)

1. Try V2 (ML) first (`S/GarminSupport.java:377-379`).
2. ML pair search:
   - Iterate i = 0..4, with RX = `0x2810+i` and TX = `0x2820+i` ("send = receive + 0x10").
   - Use the **first i where both exist**. Only that one pair is used. The other pairs are ignored and get no CCCD write (`S/communicator/v2/CommunicatorV2.java:122-143`).
   - History: before commit `2fa22224bd`, Gadgetbridge hardcoded `2812/2822`. That commit introduced the dynamic search.
3. If no ML pair exists, try V1, then V0. V1/V0 need both characteristics of the respective set (`S/communicator/v1/CommunicatorV1.java:51-70`).
4. If nothing is found: log "not a Garmin device?" and set state NOT_CONNECTED (`S/GarminSupport.java:380-388`).
5. Independently, if service `6a4e8022` exists, also enable its RX notifications (`S/GarminSupport.java:393-396`, `S/communicator/GarminWatchAppCommunicator.java:132`).

**V1/V0 framing:** no ML handle byte and no control channel. The raw COBS stream goes straight onto TX/RX (`S/communicator/v1/CommunicatorV1.java:83-115`). Realtime services are unsupported there (`:117-145`).

### 1.3 Notification enabling / descriptor writes

- `TransactionBuilder.notify(rx, true)` becomes `NotifyAction` (`B/service/btle/TransactionBuilder.java:245-252`). It does two things:
  - calls `setCharacteristicNotification`;
  - writes the CCCD (0x2902): `01 00` if the characteristic has PROPERTY_NOTIFY, otherwise `02 00` if it has PROPERTY_INDICATE (`B/service/btle/actions/NotifyAction.java:104-137`).
- On ML, only the selected RX characteristic gets a CCCD write (`S/communicator/v2/CommunicatorV2.java:132`).
- The CCCD write is queued **before** the first control write (CLOSE_ALL) in the same transaction (`:132-133`). This ensures the watch's response is not missed.

### 1.4 MTU negotiation

- Gadgetbridge requests `requestMtu(515)` at the very start of the init transaction if `allow_high_mtu` is set (`S/GarminSupport.java:373-375`). The pref defaults to true (`R/xml/devicesettings_high_mtu.xml:5-6`).
- `RequestMtuAction` waits for the result (`B/service/btle/actions/RequestMtuAction.java:51-54`). If an MTU > 23 was already negotiated, it re-requests the current value rather than 515 (`:58-76`).
- Since Android 14 the OS negotiates 517 whenever any MTU > 23 is requested (commit `8905fb9eb3` message, #5156).
- `onMtuChanged` (`S/GarminSupport.java:408-427`):
  - ignored if status ≠ success, if mtu < 23, or if high MTU is disabled;
  - otherwise `maxWriteSize = calcMaxWriteChunk(mtu) = min(512, max(23, mtu) − 3)` (`B/service/btle/AbstractBTLEDeviceSupport.java:92-100`, `S/communicator/v2/CommunicatorV2.java:114-119`).
  - Existing MLR instances are updated via `setMaxPacketSize` (`:116-118`).
- Default before any MTU event: `maxWriteSize = 20` (`S/communicator/v2/CommunicatorV2.java:96`).
- Data point: a Venu X1 sent 229-byte notifications (1 handle byte + 228 COBS bytes) (`T/communicator/CobsCoDecTest.java:143-150`, chunk length measured). The watch's own MTU limits what it sends.
- Cross-check: PL also says Garmin Connect requests 515 and uses MTU − 3 (`PL/spec/PROTOCOL.md:51`).

### 1.5 Write type and flow control

- `WriteAction` writes with `characteristic.getWriteType()` (`B/service/btle/actions/WriteAction.java:79-92`). That is Android's per-characteristic default: write-without-response if the characteristic advertises it, else write request. This is Android framework behaviour, not visible in Gadgetbridge code.
- Gadgetbridge does not override it for Garmin. The only override is the generic `PREFS_DEVICE_GATT_SYNCHRONOUS_WRITES` (default false), which forces write-request when a characteristic has both properties (`B/service/btle/AbstractBTLESingleDeviceSupport.java:295, 314-322`).
- **Flow control:** every write action waits for `onCharacteristicWrite` before the next queued GATT op runs (`WriteAction.expectsResult()==true`: `B/service/btle/actions/WriteAction.java:109-112`; latch wait: `B/service/btle/BtLEQueue.java:204-209`). A failed action aborts the rest of that transaction (`BtLEQueue.java:213-216`).
- All GATT ops go through one FIFO queue. Each GFDI message (all its fragments) is one transaction, so fragments of different messages never interleave (`S/communicator/v2/CommunicatorV2.java:176-189`).
- Cross-check: GBLE and GBR both write with `response=False` (`GBLE/garmin_ble/transport/ble.py:176`, `GBR/src/protocol/transport.py:113,119,188,199`). GBR is tested on a Fenix 6 Pro (`GBR/README.md:7`).

### 1.6 Chunking of large packets

| Path | Per-write layout | Max payload per write |
|---|---|---|
| ML control (handle 0) | single 13-byte write, never chunked | — |
| ML non-reliable GFDI | `[handle][COBS bytes…]` | `maxWriteSize − 1` (`S/communicator/v2/CommunicatorV2.java:177-188`) |
| ML reliable (MLR) | `[MLR hdr 2B][bytes…]` | `maxPacketSize − 2`, where maxPacketSize = maxWriteSize (`S/communicator/v2/MlrCommunicator.java:107-120`) |
| ML non-reliable service write (`MlServiceWriter`) | `[handle][value]`, **not chunked** | caller's responsibility (`S/communicator/v2/CommunicatorV2.java:836-843`) |
| V1/V0 | raw COBS bytes | `maxWriteSize − 1` (off-by-one: there is no handle byte; harmless) (`S/communicator/v1/CommunicatorV1.java:91-98`) |
| Watch-app channel | `[fragment hdr][…]` | `calcMaxWriteChunk(mtu) − 1` (`S/communicator/GarminWatchAppCommunicator.java:136-140`) |

Chunks are cut without regard to COBS structure. The receiver must treat them as a byte stream.

---

## 2. Multi-Link control protocol (handle 0x00)

### 2.1 Common header (both directions, all little-endian)

| Off | Size | Type | Meaning |
|---|---|---|---|
| 0 | 1 | u8 | ML handle = `0x00` (control) |
| 1 | 1 | u8 | Request type (ordinal, see 2.2) |
| 2 | 8 | u64 LE | Client ID. Gadgetbridge = `2` (`S/communicator/v2/CommunicatorV2.java:85`) |
| 10 | … | | Type-specific |

- On receive, Gadgetbridge reads the client ID and **drops any control message whose client ID ≠ 2** (`S/communicator/v2/CommunicatorV2.java:334-341`). So the watch echoes the client ID in responses. GBR, tested on hardware, does the same (`GBR/src/protocol/transport.py:155-159`).
- If an inbound control message has type 0, 2, 5 or 7 (request types), Gadgetbridge logs and ignores it (`:350-355`).

### 2.2 Request types (`S/communicator/v2/CommunicatorV2.java:727-748`)

| Value | Name | Direction | Gadgetbridge handling |
|---|---|---|---|
| 0 | REGISTER_ML_REQ | host→watch | sent |
| 1 | REGISTER_ML_RESP | watch→host | handled (`:356-432`) |
| 2 | CLOSE_HANDLE_REQ | host→watch | sent |
| 3 | CLOSE_HANDLE_RESP | watch→host | handled (`:433-470`) |
| 4 | UNK_HANDLE | watch→host | **no switch case, silently ignored**. PL names it INVALID_HANDLE, with the handle at offset 12 (`PL/src/garmin_proto_lab/multilink.py:47, 223-232`) |
| 5 | CLOSE_ALL_REQ | host→watch | sent |
| 6 | CLOSE_ALL_RESP | watch→host | handled (`:471-483`) |
| 7 | UNK_REQ | ? | ignored |
| 8 | UNK_RESP | ? | logged only (`:484-486`) |

### 2.3 Message layouts

Gadgetbridge allocates every request as a 13-byte buffer (`ByteBuffer.allocate(13)`).

**REGISTER_ML_REQ** (13 bytes, `S/communicator/v2/CommunicatorV2.java:677-685`)

| Off | Size | Type | Meaning |
|---|---|---|---|
| 0 | 1 | u8 | 0x00 |
| 1 | 1 | u8 | 0x00 (type) |
| 2 | 8 | u64 LE | client ID (2) |
| 10 | 2 | u16 LE | service code (2.4) |
| 12 | 1 | u8 | flags: `0x02` = request reliable (MLR), `0x00` = not (`:683`) |

Example, GFDI reliable: `00 00 02 00 00 00 00 00 00 00 01 00 02`

**REGISTER_ML_RESP** (parse, `:356-370`)

| Off | Size | Type | Meaning |
|---|---|---|---|
| 10 | 2 | u16 LE | service code |
| 12 | 1 | u8 | status. 0 = OK. Gadgetbridge just logs ≠ 0 and returns (`:364-367`). PL: 1 invalid service, 2 pending auth, 3 already in use, 4 rejected (`PL/spec/PROTOCOL.md:887`) |
| 13 | 1 | u8 | handle (read only when status = 0) |
| 14 | 1 | u8 | reliable. Gadgetbridge: `!= 0` ⇒ MLR (`:369, 417`). PL: bit0 = reliable; byte 15 = optional "revision"; for status 3, bytes 13..14 = alternate characteristic short UUID (`PL/src/garmin_proto_lab/multilink.py:144-166`) |

Example (handle value assumed, see 2.5): `00 01 02 00 00 00 00 00 00 00 01 00 00 80 01`

**CLOSE_HANDLE_REQ** (13 bytes, `:687-695`)

| Off | Size | Type | Meaning |
|---|---|---|---|
| 10 | 2 | u16 LE | service code |
| 12 | 1 | u8 | handle |

**CLOSE_HANDLE_RESP** (`:433-438`; note: handle comes **before** status here)

| Off | Size | Type | Meaning |
|---|---|---|---|
| 10 | 2 | u16 LE | service code |
| 12 | 1 | u8 | handle (unsigned; fix `1b1c68f00e`, needed for handles ≥ 0x80) |
| 13 | 1 | u8 | status (PL: 0 ok, 1 invalid handle, 2 no connection, `PL/src/garmin_proto_lab/multilink.py:61-64`) |

**CLOSE_ALL_REQ** (`:668-675`)

| Off | Size | Type | Meaning |
|---|---|---|---|
| 10 | 2 | u16 LE | 0x0000 |
| 12 | 1 | u8 | 0x00. This is an unwritten pad byte of Gadgetbridge's 13-byte buffer. GBR sends only 12 bytes and works on a Fenix 6 Pro (`GBR/src/protocol/transport.py:186-188`) |

Gadgetbridge bytes: `00 05 02 00 00 00 00 00 00 00 00 00 00`

**CLOSE_ALL_RESP:** Gadgetbridge parses nothing past the client ID (`:471-483`). PL puts a status byte at offset 12 (`PL/src/garmin_proto_lab/multilink.py:209-220`).

### 2.4 Service codes (u16, `S/communicator/v2/CommunicatorV2.java:750-777`)

| Code | Service |
|---|---|
| 1 | GFDI |
| 2 | NFC |
| 3 | HEALTH_SDK |
| 4 | REGISTRATION |
| 5 | CONNEXT |
| 6 | REALTIME_HR |
| 7 | REALTIME_STEPS |
| 8 | REALTIME_CALORIES |
| 9 | REALTIME_FLOORS |
| 10 | REALTIME_INTENSITY |
| 12 | REALTIME_HRV |
| 13 | REALTIME_STRESS |
| 15 | ECHO |
| 16 | REALTIME_ACCELEROMETER |
| 19 | REALTIME_SPO2 |
| 20 | REALTIME_BODY_BATTERY |
| 21 | REALTIME_RESPIRATION |
| 22 | KEEP_ALIVE |
| 26 | REALTIME_ACTIVE_TIME |
| 0x2018, 0x4018, 0x6018, 0xA018, 0xC018, 0xE018 | FILE_TRANSFER_2/4/6/A/C/E |

PL additionally lists 0x8018 (`PL/spec/PROTOCOL.md:887`). Gadgetbridge lacks it.

### 2.5 Handle assignment and inbound routing

- **The watch assigns handles.** Gadgetbridge keeps a `handle↔service` map (`:92-94, 372-373`).
- **Reliable handles carry bit 7.** Evidence:
  - Gadgetbridge keys MLR instances by the response handle (`:425`) but looks them up as `((b0 & 0x70) >> 4) | 0x80` (`:201`). That lookup only works if the registered handle is `0x80 | n`.
  - The close-response fix needed `& 0xff` (commit `1b1c68f00e`).
  - PL: "Reliable handles are 0x80..0x87", non-reliable "below 0x80" (`PL/spec/PROTOCOL.md:893`).
  - Only 3 bits identify a reliable handle, so at most 8 concurrent MLR handles exist.
- **Inbound routing order** (`S/communicator/v2/CommunicatorV2.java:193-243`):
  1. Drop notifications that are not from the selected RX characteristic (`:194-197`).
  2. If `b0 & 0x80` **and** an MLR instance exists for `((b0&0x70)>>4)|0x80`, hand the whole packet to MLR (`:199-218`).
  3. Otherwise fall through. "#5476 – non-MLR handles can also have the msb set" (`:219`; commit `33a774f837`).
  4. `b0 == 0x00` → control message (`:225-228`).
  5. Otherwise look up the full byte `b0` as a plain handle and pass `value[1:]` to that service's callback (`:230-236`).
  6. **Unknown handle:** log a warning and drop (`:238-240`). The watch-side counterpart, type 4, is ignored by Gadgetbridge (see 2.2).
- Cross-check, agrees: GBR (non-reliable, Fenix 6 Pro) routes on the exact bare handle byte (`GBR/src/protocol/transport.py:129-137`).
- Cross-check, disagrees: GBLE decodes *every* MSB-set byte as "handle in bits 6:4" even without MLR (`GBLE/garmin_ble/protocol/mlr.py:113-129`). That contradicts Gadgetbridge and GBR.

### 2.6 Startup sequence (Gadgetbridge)

Performed on every (re)connect inside the init transaction.

1. CCCD write on the ML RX characteristic (`S/communicator/v2/CommunicatorV2.java:132`).
2. Write CLOSE_ALL_REQ on TX (`:133`). Gadgetbridge sets `closingAll = true` at the same time (`:134`).
3. The watch may first send CLOSE_HANDLE_RESP for handles left over from the previous session.
   - While `closingAll` is set, a GFDI close does **not** trigger re-registration (`:104-107, 456-464`).
   - Commit `9afeff3bb3` explains why: on reconnection Gadgetbridge would otherwise register GFDI twice and leak a retransmitting MLR instance.
4. The watch sends CLOSE_ALL_RESP. Gadgetbridge then (`:471-483`):
   - sets `closingAll = false`;
   - clears both maps;
   - calls `onClose()` on all callbacks;
   - sends REGISTER_ML_REQ(GFDI = 1, flags = `0x02` if pref `garmin_mlr`, else `0x00`).
   - Pref default: true (`S/GarminSupport.java:729-731`, `C/GarminDeviceSettings.kt:180-186`). Enabled by default since commit `edfd709f43` (2026-04-14).
5. The watch sends REGISTER_ML_RESP(1, 0, H, R) (`:368-431`):
   - If R ≠ 0, Gadgetbridge creates an MLR instance for H. If one already exists for H, it is closed and replaced (`:417-426`).
   - Otherwise Gadgetbridge uses plain ML (`:427-430`).
   - If Gadgetbridge has no callback for the registered service, it logs, then builds a CLOSE_HANDLE_REQ **but never writes it**. `closeService(...)`'s return value is unused at `:411`, which is a bug.
6. Gadgetbridge sends nothing on GFDI by itself. `GfdiCallback` has no `onConnect` (`:490-503`). Everything that follows is driven by inbound GFDI (DEVICE_INFORMATION → reply, CONFIGURATION → INITIALIZED: `S/GarminSupport.java:505-510, 561-563, 1097-1126`).
7. **Timeouts: none in Gadgetbridge** for CLOSE_ALL_RESP or REGISTER_ML_RESP. A register status ≠ 0 for GFDI is not retried, so the link stays without GFDI until reconnect.
   - GBLE uses 10 s for CLOSE_ALL and 5 s for registration (`GBLE/garmin_ble/watch.py:233-235`).
   - GBR waits 10 s (`GBR/src/protocol/transport.py:89`).

**Cross-check of startup:** PL says Garmin Connect behaves differently:

- After CLOSE_ALL it first registers REGISTRATION (4) non-reliable.
- It queries a supported-service "info page".
- On status 3 it retries on an alternate characteristic.
- Sources: `PL/src/garmin_proto_lab/multilink_client.py:243-283`, `PL/spec/PROTOCOL.md:887`.

Gadgetbridge never does any of this and works.

**Client ID differs:** Garmin Connect uses `0x01` (`PL/spec/PROTOCOL.md:889`, `PL/src/garmin_proto_lab/multilink.py:32`). Gadgetbridge, GBLE and GBR use `2`.

**CLOSE_ALL byte order disagreement:** GBLE packs `type, u16 0, u64 client, u8 0`, which puts the client ID at offset 4 (`GBLE/garmin_ble/protocol/mlr.py:152-154`). Gadgetbridge, GBR and PL all put it at offset 2. Use Gadgetbridge's layout.

### 2.7 Closing handles

- **Host-initiated:** send CLOSE_HANDLE_REQ(service, current handle) (`:323-326, 882-892`).
  - For MLR, close only after all data is acknowledged, otherwise the transfer is truncated (`:821-825`). Uploads do this from the ack listener (`S/GarminSupport.java:2402-2413`).
- **On any CLOSE_HANDLE_RESP** (`:433-469`) Gadgetbridge:
  - calls `onClose()`;
  - closes and removes the MLR instance;
  - clears both maps;
  - re-registers GFDI with the same reliable flag if GFDI was closed "unexpectedly" (watch-initiated) and no close-all is pending (commit `3f0bb26cc3`).
- The watch also closes file-transfer handles itself at end of transfer. Gadgetbridge's download path finalises (inflates) only in `onClose()` (`S/GarminSupport.java:2051-2060`).

### 2.8 Realtime and file-transfer services (users of handles)

**Realtime services:**

- Registered on demand with flags 0, always non-reliable (`S/communicator/v2/CommunicatorV2.java:279-332`). Closed with CLOSE_HANDLE_REQ.
- One notification is one service message: no COBS, no CRC, payload = `value[1:]`.
- Payload examples:
  - HR: `[type u8][hr u8][resting u8]…` (`:508-510`)
  - Steps: `[steps u32 LE][goal u32 LE]` (`:532-533`)
  - SpO2: `[i8][garmin-ts u32]` (`:617-618`)
  - HRV: `[rr u16][u32]` (`:651-652`)
  - Accelerometer: 16 bytes (`:546-611`)
- The accelerometer requires the host to write `0x01` on its handle after registration (`:566-568`).
- HR one-shot: register, then close on the first HR > 0 (`:279-286, 522-524`).

**File transfer:**

- Gadgetbridge picks the first unregistered service in the order 0x2018, 0x4018, 0x6018, 0xA018, 0xC018, 0xE018 (`:245-276`).
- Reliable = MLR pref, or forced true for pushes (`S/GarminSupport.java:2418`).
- Pipe protocol (on top of MLR, not GFDI):
  - host sends 6 bytes: `00 | dir (00 = read, 01 = write) | token u16 LE | 00 00` (`S/GarminSupport.java:2042-2047, 2380-2386`);
  - the watch's first reply is `00 00 00` = OK (`:2198-2208`); for pushes, any nonzero `value[0]` = rejected (`:2396`);
  - then a raw (zlib) byte stream follows.
- PL says the handle is u64 LE (10-byte message) (`PL/spec/PROTOCOL.md:934-940`). This is a disagreement, and it lies above the transport layer (see `04-protobuf-fileaccess-http.md` §3.6).

---

## 3. MLR: Multi-Link Reliable layer (`S/communicator/v2/MlrCommunicator.java`)

Gadgetbridge's own documentation for this layer is linked at `:17`: gadgetbridge.org/internals/specifics/garmin-protocol/#multi-link-reliable-protocol.

### 3.1 Negotiation

- The host sets flags `0x02` in REGISTER_ML_REQ.
- If the watch answers with reliable ≠ 0, Gadgetbridge creates an `MlrCommunicator(handle, maxWriteSize, …)` (`S/communicator/v2/CommunicatorV2.java:417-426, 846-861`).
- All sequence state starts at 0 for each new registration (`MlrCommunicator.java:43-53`). There is no reset message: resetting means close plus re-register.

### 3.2 Packet header (both directions, every packet)

| Byte | Bits | Field |
|---|---|---|
| 0 | 7 | `1` = MLR flag (`MLR_FLAG_MASK 0x80`) |
| 0 | 6..4 | handle & 7 (`HANDLE_MASK 0x70`, shift 4) |
| 0 | 3..0 | RN bits 5..2 (`REQ_NUM_MASK 0x0F`) |
| 1 | 7..6 | RN bits 1..0 |
| 1 | 5..0 | SN (`SEQ_NUM_MASK 0x3F`) |
| 2… | | payload (0 bytes = ACK-only packet) |

- Encode: `b0 = 0x80 | (h&7)<<4 | RN>>2` and `b1 = (RN&3)<<6 | SN` (`:318-331`).
- Decode: `RN = (b0&0x0F)<<2 | b1>>6` and `SN = b1&0x3F` (`:141-143`).
- RN is cumulative: "next SN I expect from you".
- Examples:
  - handle 0x80, ACK-only with RN = 1 → `80 40`
  - handle 0x81, RN = 5, SN = 3 → `91 43`
- Cross-check: PL is identical (`PL/spec/PROTOCOL.md:893-901`).

Receive validation (`:126-148`):

- length < 2 → drop;
- flag clear → drop;
- `(b0>>4)&7 ≠ handle&7` → drop.

### 3.3 Constants (`S/communicator/v2/MlrCommunicator.java:22-33`)

| Constant | Value |
|---|---|
| Sequence modulus | 64 (`MAX_SEQ_NUM 0x3F`) |
| Initial and max in-flight window | 32 (`INITIAL_MAX_UNACKED_SEND 0x20`; the cap is the same value, `:238`) |
| Initial RTO | 1000 ms |
| Max RTO | 20000 ms |
| Delayed-ACK timer | 20 ms (`ACK_TIMEOUT`). Commit `4eeab7b5c3`: "The official app acks 7~17 ms after a burst of packets." |
| Immediate-ACK threshold | 5 unacked received packets (`ACK_TRIGGER_THRESHOLD`) |
| Retry limit | **none** (retransmits forever with backoff capped at 20 s) |

### 3.4 Sender

- `sendMessage` splits bytes into fragments of `maxPacketSize − 2` (`:96-124`).
- MLR carries **no message boundaries**: it is a reliable byte stream.
- `runProtocolLocked` (`:286-316`): while the queue is non-empty and `(nextSendSeq − lastRcvAck) mod 64 < window`, send `hdr(RN = nextRcvSeq, SN = nextSendSeq) + data`. Each sent fragment is stored in `sentFragments[SN]`, together with the RN used. `nextSendSeq` is then incremented mod 64.
- The retransmission timer starts when a packet goes out with nothing outstanding (`:309-312`).
- Each packet is its own GATT write transaction, FIFO with everything else (`S/communicator/v2/CommunicatorV2.java:855-857`).

ACK processing (`:207-248`), triggered whenever an inbound packet's `RN ≠ lastRcvAck` (`:186-188`):

1. Cancel the retransmission timer.
2. Free `sentFragments[lastRcvAck … RN−1]`.
3. Set `lastRcvAck = RN`.
4. Reset RTO to 1000 ms.
5. Grow the window to `min(window + numAcked, 32)`.
6. Notify the ack listener with acked bytes and an "all acked" flag.
7. Restart the timer if packets are still outstanding.

Gadgetbridge does **not** check that RN lies within the outstanding range. A stale RN would wrap and corrupt state; a Go client should ignore ACKs outside `[lastRcvAck, nextSendSeq]`.

Retransmission timeout (`:341-376`):

1. Set `RTO = min(2·RTO, 20000)` and `window = max(1, window/2)`.
2. Resend **all** outstanding `[lastRcvAck, nextSendSeq)` (go-back-N), even beyond the new window.
3. Restart the timer.

The resulting retransmission schedule is ≈ +1 s, +2 s, +4 s, +8 s, +16 s, then every 20 s.

**Each retransmitted packet carries the RN stored at its first send, not the current one** (`:302-305, 369-370`; commit `f4556e81cc` "Fix MLR retransmission request numbers"). PL says Garmin Connect puts the current RN in every outbound packet (`PL/spec/PROTOCOL.md:918`). This is a disagreement.

### 3.5 Receiver (`:184-204, 251-283`)

- An ACK-only packet (length 2) is processed for its RN and nothing else.
- A data packet with `SN == nextRcvSeq` is accepted: `nextRcvSeq++` mod 64 and the payload goes up to the service.
- Any other SN (out of order or duplicate) is **dropped, not buffered**, and an ACK-only packet with the current `nextRcvSeq` is sent immediately.
- After delivering data (`:251-261`):
  - if `(nextRcvSeq − lastSendAck) mod 64 ≥ 5`, send an ACK now;
  - otherwise (re)arm the 20 ms timer. It is re-armed on every packet, so effectively it fires 20 ms after the last packet of a burst.
- ACK-only packet = `hdr(RN = nextRcvSeq, SN = 0)` with no payload; then `lastSendAck = nextRcvSeq` (`:270-283`).
- Data packets also piggy-back the current RN, but in Gadgetbridge they **neither** update `lastSendAck` **nor** cancel the ACK timer. Redundant ACK-only packets therefore follow.
- PL differs on these points:
  - piggy-backed ACKs clear the pending ACK;
  - the deferred-ACK timer is 10 ms;
  - the window starts at 32, grows +1 per advancing ACK up to 63;
  - RTO uses an RFC 6298-style estimator (α 1/8, β 1/4, min RTO 500 ms).
  - Sources: `PL/spec/PROTOCOL.md:905-932`, `PL/src/garmin_proto_lab/mlr.py:23-29`.
- None of these differences is needed for interoperability as far as Gadgetbridge shows.

### 3.5a goarmin's MLR: two hazards found in testing (M1b)

`pkg/garmin/multilink` implements the sender and receiver above with Gadgetbridge's constants.
Our capture of Garmin Connect contains no host retransmission, so it cannot settle the RN
disagreement. Two lossy tests (10 % and 5 % loss in both directions) showed that both readings
can break on their own terms.

1. **A stale RN can look like a fresh ACK.** An RN has 6 bits. Say a fragment is first sent
   when we have received R packets, and we have received 32 or more since. The peer has seen
   later RNs from us by then. Read relative to its own window, `(R − lastAck) mod 64` can land
   inside it, so the peer frees packets we never got.
   - From then on the peer never resends them, and the stream deadlocks.
   - goarmin therefore keeps the original RN (Gadgetbridge) only while fewer than 16 packets
     have arrived since the first send. After that it sends the current RN.
2. **RNs go stale in the writer queue too.** On a slow link a burst of go-back-N
   retransmissions can wait in the outgoing queue while ACK-only packets overtake it. The
   queued data packets then reach the wire carrying RNs 40 packets old, with the same effect.
   - goarmin queues packet *builders*: a packet's header is computed when it goes on the wire.
   - A fragment acknowledged in the meantime is not sent at all.
   - Each fragment and the pending ACK are queued at most once.
   - A data packet that carries the current RN counts as an ACK and cancels the delayed-ACK
     timer (garmin-proto-lab's behaviour).

Other goarmin choices:
- **Stale ACKs are ignored.** An ACK with an RN outside `[lastAck, nextSend]` changes nothing.
- **Retransmissions are capped.** A stream fails after 8 consecutive timeouts without progress
  (about 110 s with the default backoff); Gadgetbridge retries forever.
- **The watch's CLOSE_HANDLE_RESP ends the stream.** It arrives unsolicited at the end of a
  pull. Payloads already received are still delivered.

### 3.6 Interaction with COBS, errors, teardown

- **GFDI over MLR:**
  - Out: GFDI frame → COBS-encode → MLR fragments (`S/communicator/v2/CommunicatorV2.java:170-175`).
  - In: in-order MLR payloads → COBS decoder (`:860`, `:499-501`). The COBS `00` delimiters travel inside the MLR payload.
- File-transfer handles over MLR carry raw bytes, without COBS.
- `close()`: marks closed, cancels all timers, clears the queue (`MlrCommunicator.java:378-388`).
- On GATT disconnect: closed and timers cancelled (`:390-397`, via `CommunicatorV2.java:155-159`, `GarminSupport.java:445-450`).
- `dispose()` closes all instances (`CommunicatorV2.java:146-152`). CLOSE_ALL_RESP does *not* close MLR instances. Only re-registration of the same handle or a CLOSE_HANDLE_RESP does.

---

## 4. COBS framing (`S/communicator/CobsCoDec.java`)

### 4.1 Variant

Standard COBS (Cheshire/Baker) with **one leading `0x00` and one trailing `0x00`** around each frame (`:51-54, 115-119, 151`).

Encoder rules:

- Code byte = run length + 1.
- `0xFF` = 254 non-zero bytes with no implied zero (`:136-141`).
- If the input ends in `0x00`, an extra code `0x01` is emitted (`:147-149`, fix `076a85d762`).
- A 254-byte trailing run also gets a final `0x01`. The decoder ignores it.
- Encoded size ≤ n + ⌈n/254⌉ + 3. Gadgetbridge allocates 2n + 2.

Example (`T/communicator/CobsCoDecTest.java:95-100`):

- Input: `2C 00 A0 13 96 00 …`
- Output: `00 02 2C 04 A0 13 96 …`

### 4.2 Gadgetbridge decoder behaviour

`receivedBytes` appends to a buffer and attempts a decode (`:32-43`). A decode attempt:

- does nothing if fewer than 4 bytes are buffered, or the buffer's last byte ≠ 0 (`:61-66`);
- discards the buffer if the first byte ≠ 0 (`:67-77`);
- decodes codes until a code `0x00` or the end (`:78-93`);
- resets on a truncated frame (underflow) (`:94-102`) or on overflow (`:39-42`).

Other properties:

- Buffer: 10,000 bytes on BLE (`:16-18`) and 0x10000 on RFCOMM (`S/GarminRfcommListener.java:128`). Overflow drops the frame.
- No inactivity timeout. It was removed in `a561809ec9` because some devices pause about 3 s mid-frame.
- GBLE still resets after 1.5 s (`GBLE/garmin_ble/protocol/cobs.py:7,14-21`). Don't copy that.

**Fragility (do not replicate):**

- Gadgetbridge retrieves **one** decoded message per notification (`S/communicator/v2/CommunicatorV2.java:499-501`).
- If one notification holds the end of frame N plus frame N+1, the decoder:
  1. appends a spurious `0x00` to frame N (the `hasRemaining()` check at `:90-92`), so the GFDI length check fails;
  2. drops frame N+1's trailing zero on `compact()`.
- The research agent verified this with a faithful Python port. Gadgetbridge works in practice because watches start each frame in a new notification: the Venu X1 capture shows frame continuation in the next notification and the next frame in a fresh one (`T/communicator/CobsCoDecTest.java:143-169`).
- A Go client should instead decode a stream: split on `0x00`, decode each non-empty segment, loop until no complete frame remains. PL agrees: "repeated zero delimiters are harmless" (`PL/spec/PROTOCOL.md:135`).
- GBR does the same split (`GBR/src/protocol/transport.py:139-149`).

### 4.3 Splitting and reassembly

- The sender cuts the COBS byte string at write-size boundaries (1.6).
- The receiver concatenates the per-handle payloads (after the handle byte or MLR header). A trailing `00` may arrive alone in its own notification (`T/communicator/CobsCoDecTest.java:80-93`).
- The max GFDI frame is governed by GFDI, not by the transport:
  - the watch's `maxPacketSize` from DEVICE_INFORMATION (e.g. 520 on an Instinct 2S, decoded from `T/communicator/CobsCoDecTest.java:39-41`);
  - Gadgetbridge defaults of 400 and 375 (`S/GarminSupport.java:222`, `S/FileTransferHandler.java:64`);
  - Gadgetbridge's 10 KiB out-buffer (`S/messages/GFDIMessage.java:38`).

---

## 5. CRC-16 (`S/ChecksumCalculator.java`)

The CRC is applied at the GFDI layer (see `02-gfdi-session.md` §2.3); it is described here because the same function is used for file-data CRC chaining.

### 5.1 Algorithm

- A nibble-table CRC. Table `{0000, CC01, D801, 1400, F001, 3C00, 2800, E401, A001, 6C00, 7800, B401, 5000, 9C01, 8801, 4400}` (`:22-25`).
- Per byte b, low nibble first, then high nibble: `crc = ((crc>>4)&0x0FFF) ^ T[crc&15] ^ T[nibble]` (`:41-49`).
- This is the 4-bit table of reflected poly 0xA001, so the result is **CRC-16/ARC**:
  - poly 0x8005, refin/refout true, init 0x0000, xorout 0;
  - check("123456789") = 0xBB3D.
- Verified by the research agent with a script; agrees with `PL/spec/PROTOCOL.md:180-184` and with the GBLE table (`GBLE/garmin_ble/protocol/crc.py`).
- Seed: 0 for frames (`:30-32`). File-data CRCs chain a running seed (`S/FileTransferHandler.java:529,541`).

### 5.2 Coverage and position (GFDI frame, pre-COBS)

| Off | Size | Type | Meaning |
|---|---|---|---|
| 0 | 2 | u16 LE | total length **including** the 2 CRC bytes |
| 2 | 2 | u16 LE | message type. If bit 15 is set: compact form, type = (low byte) + 5000 (`S/messages/GFDIMessage.java:46-51`) |
| 4 | N | | payload |
| 4+N | 2 | u16 LE | CRC over bytes [0, len−2) |

- Writer: `S/messages/GFDIMessage.java:130-133`.
- Reader checks length == decoded size, then CRC (`:248-276`).
- Example, a DEVICE_INFORMATION frame of 44 bytes ending `… 00 00 04 B8`: CRC(first 42 bytes) = 0xB804, stored as `04 B8` (verified).

---

## 6. Connection lifecycle (Android Gadgetbridge)

### 6.1 Bonding

- Garmin coordinators don't override the bonding style, so they use `BONDING_STYLE_ASK` (`B/devices/AbstractDeviceCoordinator.java:725`; the HRM variant uses NONE: `C/hrm/GarminHrmCoordinator.kt:22-23`). `suggestUnbindBeforePair` = false (`C/GarminCoordinator.java:119-121`).
- Pairing UI (`B/activities/pairing/DevicePairingViewModel.kt`):
  - if already bonded, skip the bond step; for ASK, the user decides (`:132-140`);
  - `createBond()` (`:179-197`);
  - once bonded, wait **1000 ms** (`:205, 226`), then `disconnect()` + `connect(firstTime=true)` (`:201-211`).
- `connectFirstTime` sets `mFirstConnect` (`S/GarminSupport.java:1786-1789`). After init this triggers GFDI system events PAIR_COMPLETE, SYNC_COMPLETE and SETUP_WIZARD_COMPLETE (`:1119-1126`).
- Gadgetbridge never bonds during normal connects.
- Gadgetbridge does not implement Garmin's proprietary auth (5101–5112). It answers AUTH_NEGOTIATION with ACK/"GUESS_OK" (`S/messages/AuthNegotiationMessage.java:21`).
- PL: with an OS bond, Garmin Connect skips proprietary auth (`PL/spec/PROTOCOL.md:390-402`).

### 6.2 Connect

1. `connectGatt(autoConnect=false, TRANSPORT_LE)` (`B/service/btle/BtLEQueue.java:380-388`; `useAutoConnect()=false`: `S/GarminSupport.java:352-354`).
2. Connect timeout: **5000 ms**, or 45000 ms with the low-power pref (`BtLEQueue.java:349-359`).
3. On CONNECTED, wait before service discovery (`B/service/btle/AbstractBTLEDeviceSupport.java:73-81`; `BtLEQueue.java:666-689`):
   - 300 ms on Android 8–14;
   - Android 15+: 1000 ms if bonded, else 300 ms;
   - below Android 8: 1600 / 300 ms.
   - Cached services are reused.
4. `onServicesDiscovered` runs init only if the state is below INITIALIZING (`B/service/btle/AbstractBTLESingleDeviceSupport.java:360-366`; double-init fix `7fed89a648`). The init transaction runs in this order:
   1. state INITIALIZING;
   2. requestMtu(515);
   3. ML RX CCCD;
   4. CLOSE_ALL;
   5. watch-app RX CCCD;
   6. requestConnectionPriority(BALANCED), or LOW_POWER with the pref (`AbstractBTLESingleDeviceSupport.java:383-389`).
   - Sources: `S/GarminSupport.java:363-399`.
5. Then the control handshake (2.6), then GFDI. INITIALIZED is set on the CONFIGURATION capabilities (`S/GarminSupport.java:561-563, 1116`).

### 6.3 Disconnect and reconnect

- MLR timers are stopped (6.2 / 3.6).
- `handleDisconnected` (`BtLEQueue.java:425-510`):
  - statuses 0x81, 0x85, 0x05, 0x08, 0x0F, 0x93 force a full disconnect (`:448-466`);
  - otherwise, with auto-reconnect, Gadgetbridge calls `gatt.connect()` immediately (`:476-492`);
  - else WAITING_FOR_RECONNECT / WAITING_FOR_SCAN (`:499-510`).
- Every reconnect re-runs the full init: a new `CommunicatorV2`, CLOSE_ALL, then register GFDI (`AbstractBTLESingleDeviceSupport.java:136`).

### 6.4 Keep-alive

- **None.** `KEEP_ALIVE(22)` is defined but never referenced (`S/communicator/v2/CommunicatorV2.java:768`; added in commit `379cd62b4e` together with other IDs).
- Gadgetbridge only sends GFDI SystemEvent HOST_DID_ENTER_FOREGROUND/BACKGROUND on app state changes (`S/GarminSupport.java:239-244`).
- GBLE uses a GFDI TIME_UPDATED system event every 60 s "as keep-alive" (`GBLE/garmin_ble/watch.py:902-919`).

### 6.5 What tears the link down in Gadgetbridge

- user disconnect or `dispose()` (`S/GarminSupport.java:287-317`);
- the 5 s connect timeout;
- the GATT error statuses above;
- BLE supervision timeout;
- no Garmin characteristics found (`S/GarminSupport.java:383-387`).

MLR never tears the link down.

### 6.6 Single phone

GBLE claims a watch accepts only one BLE client: a phone running Garmin Connect causes CLOSE_ALL to go unanswered (`GBLE/garmin_ble/watch.py:495-499`). This is unverified. Relevant for goarmin: a Fenix 8 synced by goarmin probably can't also be connected to a phone app at the same time.

---

## 7. Fenix 8 / newer-device specifics

- **No Fenix-8-specific transport code exists.**
  - The coordinator only matches the advertised name `^fenix 8( - [\d+]+mm)?$` (`C/watches/fenix/GarminFenix8Coordinator.java:10-12`).
  - Real advertised name: "fenix 8 - 51mm" (#4226, `app/src/test/java/nodomain/freeyourgadget/gadgetbridge/devices/BluetoothNameTest.java:204`).
  - Variants: Pro (experimental, name unconfirmed: `C/watches/fenix/GarminFenix8ProCoordinator.java`) and Solar.
  - Scan match also uses manufacturer data from company 0x0087, with the part number in the first 2 bytes, big-endian (`C/GarminCoordinator.java` `supports()`).
- **New sync protocol:** forced on only for Venu 4, Venu X1, Vivoactive 6 and Cirqa. Fenix 8 does not get it by default. When it is forced, MLR is forced on too (`C/GarminCoordinator.java:124-138`). Comments:
  - "#5021 – Venu X1 misses files without new sync";
  - "new sync protocol without MLR fails to sync large workouts";
  - "TODO: check DeviceInformationMessage.protocolVersion >= 150 instead" (a protocolVersion ≥ 150 hint for MLR).
- Gadgetbridge advertises protocol version 150 and `maxPacketSize = −1` in its DEVICE_INFORMATION reply. Protocol flags = 1 when the watch's version/100 == 1 (`S/messages/DeviceInformationMessage.java:21-23, 93-118`).
- **Capability bits** (CONFIGURATION bitmap, LSB-first per byte: `C/GarminCapability.java:204-220`):
  - `MULTI_LINK_SERVICE` = bit 76 (`:113`);
  - `SYNC_2` = bit 90 (`:127`);
  - `FEATURE_CAPABILITIES_EXCHANGE` = bit 95.
  - These match PL (`PL/spec/PROTOCOL.md:443`).
  - Gadgetbridge advertises all of them (`C/GarminCapability.java:172-176`) but **does not gate ML on them**: it simply probes GATT.
- **Relevant transport history:**
  - `b352b164d4` initial MLR (2025-10);
  - `33a774f837` #5476 (MSB-set handles in non-MLR mode);
  - `f4556e81cc` (retransmit with original RN);
  - `edfd709f43` MLR default on;
  - `1b1c68f00e` / `3f0bb26cc3` / `9afeff3bb3` (close-handle handling);
  - `d373aca7ea` / `a561809ec9` (COBS recovery, no timeout);
  - `8843c5beac` / `4eeab7b5c3` (window bursts, 20 ms ACK);
  - `8905fb9eb3` #5156 (MTU 517 on Android 14).
- **Optional RFCOMM path:** pref default false. The phone listens with SPP UUID `deab91e4-670f-11e1-9ace-30bc4824019b`, the watch connects after an SDP query, and the link carries plain COBS-framed GFDI (`S/GarminRfcommListener.java:34-43`).

---

## 8. Linux/BlueZ implications (Go + tinygo-org/bluetooth)

1. **Pairing (out of band from GATT).**
   - Export an `org.bluez.Agent1` and `RegisterAgent(path, "KeyboardDisplay")` + `RequestDefaultAgent` on `AgentManager1`, then call `Device1.Pair()`. This is what GBR does, tested on a Fenix 6 Pro (`GBR/src/bluetooth.py:62-78`). It handles:
     - `RequestPasskey`: the user types the PIN shown on the watch;
     - `RequestConfirmation`: auto-accept.
   - Also implement `DisplayPasskey`, `RequestAuthorization`, `AuthorizeService`, `Cancel` and `Release`: the Fenix 8's pairing method is unknown.
   - Put the watch in "Pair Phone" mode first (`GBR/README.md:94-95`).
   - Use a long D-Bus timeout for `Pair()`, since it blocks until the passkey is entered. Set `Trusted=true` afterwards.
   - Gadgetbridge expects an OS bond and implements no Garmin auth, so treat a successful OS bond as a prerequisite (see open questions).
2. **Connect, then GATT.**
   - After `Device1.Connect`, wait for ServicesResolved.
   - Find service `6a4e2800` and pick the first RX/TX pair, 2810/2820 … 2814/2824, **within that service**.
   - The watch is dual-mode; make sure BlueZ uses the LE bearer, e.g. by scanning just before connecting. Verify bearer selection on 5.87.
3. **Notifications.**
   - Call `EnableNotifications` (StartNotify → CCCD `01 00`) on the RX characteristic and **wait until it completes before writing CLOSE_ALL**, mirroring Gadgetbridge's ordering.
   - With BlueZ's D-Bus PropertiesChanged path, deliver notifications strictly in order to one goroutine.
   - Verify that BlueZ never coalesces identical consecutive values. For MLR that would be harmless (duplicates and ACKs); for non-MLR COBS it would not.
4. **MTU.**
   - BlueZ negotiates the MTU itself on connect (ExchangeMTU default 517). There is no `requestMtu`.
   - Read `GetMTU()` (GattCharacteristic1 "MTU") after services are resolved; re-read it if it reports 23.
   - Use `maxWrite = min(512, mtu − 3)`, with 20 as fallback. The watch's own MTU may be smaller; the Venu X1 used 229-byte notifications.
5. **Writes.**
   - Use `WriteWithoutResponse` (ATT Write Command) when the TX characteristic has `write-without-response`, else `Write`. This matches Android's default and GBLE/GBR.
   - A Write Command must fit in `mtu − 3`, and BlueZ won't split it.
   - Serialize **all** writes (control, GFDI fragments, MLR data and ACKs) through one TX goroutine, in order. Android gets per-write pacing from `onCharacteristicWrite`; on BlueZ, the blocking D-Bus `WriteValue` reply gives similar pacing.
   - Keep all fragments of one non-MLR COBS frame contiguous.
6. **MLR timers.** Implement them with Go timers: 20 ms ACK delay (10 ms is also fine), 5-packet trigger, RTO 1 s → 20 s backoff, window 32. D-Bus latency is a few ms and is fine.
7. **Reconnect.** BlueZ won't redo the Garmin handshake for you. On disconnect:
   - drop all handle and MLR state;
   - reconnect with backoff;
   - re-run CCCD → CLOSE_ALL → REGISTER GFDI;
   - re-register any realtime or file services you need.
   - Add handshake timeouts that Gadgetbridge lacks: about 10 s for CLOSE_ALL_RESP and 5 s for REGISTER_ML_RESP, as GBLE does.
8. **Connection parameters.** The kernel controls them (no `requestConnectionPriority`). The defaults are fine.

---

## 9. Gadgetbridge behaviours a reimplementation should not copy

- One COBS message retrieved per notification, plus the spurious-zero bug on concatenated frames (4.2).
- Unvalidated MLR ACK range (3.4).
- An unsent close request on "no callback" (`CommunicatorV2.java:411`).
- No handshake timeouts and no retry when GFDI registration is refused (2.6).
- Unlimited MLR retransmission.
- The CommunicatorV1 chunk size off-by-one.
- Global characteristic lookup by UUID (1.1).

---

## 10. Open questions / uncertain

1. **TX write properties on the Fenix 8** (write, write-without-response, or both) and whether RX supports notify, indicate, or both. No GATT dump exists in any of the four sources.
2. **Fenix 8 ATT MTU and notification size.** Only Venu X1 data exists (229 bytes).
3. **Characteristic pairs:** which pairs the Fenix 8 exposes (Gadgetbridge scans 2810–2814; PL says 2810–2819 and an info characteristic 2803). Also whether status 3 (already in use) plus an alternate characteristic actually occurs (PL only).
4. **Client ID 2 (Gadgetbridge) vs 1 (Garmin Connect):** does the watch scope CLOSE_ALL and handles per client ID? Could using 1 clash with Garmin Connect state on the watch?
5. **REGISTER_ML_RESP trailer:** is the reliable byte a bit0 flag or any nonzero value? Is the "revision" byte optional? What is the CLOSE_ALL_RESP status layout? What is the type-4 INVALID_HANDLE payload, and what do types 7/8 mean?
6. **Actual handle values the watch assigns.** Reliable is assumed to be 0x80|n (strongly implied). What did #5476 actually see: MSB-set handles for non-reliable registrations?
7. **MLR RN on retransmission:** original (Gadgetbridge, working) vs current (PL static). Does the watch care?
8. **Watch-side MLR parameters** (window, RTO, ACK delay). Does the watch ever send more than 32 unacked packets, or put two COBS frames in one MLR payload?
9. **Bonding:** does the Fenix 8 require an LE bond before answering CLOSE_ALL/REGISTER (register status 2 = "pending auth"?), or does it start proprietary auth (5101) when unbonded? Which SMP method does it use (passkey entry vs numeric comparison)?
10. **Idle link:** does the watch drop it without any keep-alive? KEEP_ALIVE(22) semantics are unknown.
11. **File-pipe configure message:** Gadgetbridge's 6-byte u16 token vs PL's u64 transfer handle (above transport, but affects reliable services).
12. **BlueZ:** dual-mode bearer selection, PropertiesChanged coalescing, and tinygo's exact `WriteWithoutResponse`/`GetMTU` implementation. Verify against the vendored tinygo version.
