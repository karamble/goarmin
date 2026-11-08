# 06 — The Fenix 8 against goarmin on Linux (live)

Part of the goarmin protocol notes (`docs/protocol/`). This note records what our own code
observed talking directly to our Fenix 8 from Linux (BlueZ 5.87, Intel AX201 adapter), without a
phone. Personal identifiers (unit number, Bluetooth address, positions, tokens) are left out on
purpose. Traces live in `~/goarmin-captures/linux-{probe,sync}-*.jsonl`, outside the repo.

## M1a: pairing and handshake (2026-11-07)

Commands: `goarmin pair`, then `goarmin probe` (via `~/Desktop/goarmin-pair.sh`). Firmware
**23.40**: the watch had installed an update since the phone capture (23.39).

### Pairing
- The watch was put in pairing mode with *Watch Settings › Connectivity › Phone › Pair Phone*.
  While in that mode it advertises its name ("fenix 8 - 47mm"). Otherwise it advertises without
  a name but with Garmin's company ID `0x0087` in the manufacturer data.
- BlueZ paired over **LE** after an LE-only discovery filter, using **numeric comparison**: our
  `org.bluez.Agent1` (capability KeyboardDisplay) received `RequestConfirmation` with a 6-digit
  code that the watch also showed. Confirming on both sides bonded the devices.
- Result: `Paired`, `Bonded` and `Trusted` are all true. No passkey entry and no legacy PIN were
  involved.
- No root was needed. BlueZ's default D-Bus policy allows a normal user to register an agent and
  call `Pair`.

### Link
- `AcquireNotify` and `AcquireWrite` both worked. Notifications and writes go through sockets with
  no D-Bus round trip per packet.
- Maximum write **229 bytes** (ATT MTU 232). The largest notification was also 229 bytes.
- The watch disconnects on its own a moment after the host closes everything; the bond stays.

### Multi-Link: Gadgetbridge's choices work
- **Characteristic pair 0** (`2810`/`2820`) answered at once. Garmin Connect uses pair 1; the
  watch accepts both.
- **Client ID 2** (Gadgetbridge's) works. Garmin Connect uses 1.
- `CLOSE_ALL_REQ` (12 bytes) is answered by `CLOSE_ALL_RESP` with the trailer `00 00 01` (the
  same as with Garmin Connect).
- `REGISTER_ML_REQ` GFDI **without the reliable flag**: status 0, **handle `0x01`, not reliable**.
  The response took about 1.1 s; timeouts must allow for that.
- No REGISTRATION(4) or KEEP_ALIVE(22) registration was needed for the handshake.

### GFDI
- `DEVICE_INFORMATION` arrived unprompted right after registration: protocol **151**, product
  **4536**, firmware 23.40.
  - **max packet 400** on this non-reliable link. The phone capture showed **4000** on Garmin
    Connect's reliable (MLR) link. Hypothesis: the watch announces a smaller frame limit when
    GFDI is not reliable. Large transfers should therefore use an MLR GFDI handle, as Garmin
    Connect does.
- Our reply was one `RESPONSE` in Garmin Connect's form: protocol 150, product and unit `FF…`,
  sw `0x2DF1`, maxPacket `0xFFFF`, three names, flag 1. The watch accepted it.
- The watch's `CONFIGURATION` was **compact** (seq 1). We ACKed it in compact form with seq 1,
  then sent Garmin Connect's 17-byte host bitmap as compact seq 0. The watch answered `RESPONSE`
  seq 0, status 0.
  - Watch capabilities: 79 bits, **SYNC_2 set**, MULTI_LINK_SERVICE and
    FEATURE_CAPABILITIES_EXCHANGE set (same as the capture).
- `SUPPORTED_FILE_TYPES_REQUEST` (compact seq 1) returned the same 16 types as with Garmin
  Connect.
- **No `AUTH_NEGOTIATION` (5101) and no secure-session messages.** The OS bond is the only
  authentication.
- The handshake from CLOSE_ALL to the file-type list took **1.5 s**.

### Two hosts stay paired
- After pairing with the laptop, the phone's Bluetooth was switched back on, and **Garmin Connect
  reconnected to the watch normally**.
- The Fenix 8 therefore keeps both bonds. The phone does not have to be given up for
  notifications.
- Still untested: whether both hosts can be *connected* at the same time. During the probe the
  phone's Bluetooth was off.
- Only one host may archive. Whichever host marks files synced makes the watch delete the health
  files, so once goarmin syncs, Garmin Connect's sync must be stopped (05 §7).

### Consequences for the implementation (M1b onwards)
- Keep Gadgetbridge's defaults (pair 0, client 2) and fall back to the next pair or client ID 1.
- Use a reliable (MLR) GFDI handle for syncing, to get the 4000-byte frame limit; the
  non-reliable path is proven as a fallback.
- Compact frames are mandatory: echo the sequence number in responses, and number the host's own
  requests from 0.
- Allow at least 5 s for Multi-Link control responses.

## M2: the first live sync (2026-11-07)

Command: `goarmin sync -once -no-archive`, run twice (via `~/Desktop/goarmin-sync.sh`) with the
phone's Bluetooth off. The phone had synced with Garmin Connect shortly before, so the watch had
already handed its health files to Garmin Connect. Traces: `~/goarmin-captures/linux-sync-*.jsonl`
(`pkg/garmin/mltrace`); `snoopdecode <trace>.jsonl` decodes them like a btsnoop log.

### Result

| | Run 1 | Run 2 |
|---|---|---|
| Connected, handshake done | 3.4 s (2.6 s of it LE scan and connect) | 4.0 s |
| Listed | 142 items, 1 ignored | 0 |
| Pulled | 141 files, 964 KB inflated, in 81.8 s | 0 |
| Failed, MLR retransmissions | 0, 0 | 0, 0 |

- **Every pulled file is a complete, valid FIT file** (all CRCs checked), and every file the
  watch made itself carries the unit number as its `file_id` serial.
- Run 2 pulled nothing: the stored cursor made its listing incremental (below).
- No warnings, no fallback: the first attempt (pair 0, client 2, reliable GFDI) worked both times.

### Link
- GFDI registered **reliable** (MLR) and the watch announced **max packet 4000**, as with Garmin
  Connect. Notifications were at most 229 bytes.
- **Not a single MLR retransmission or duplicate** in either direction across 3776 packets. BLE
  does not lose packets at this layer, so goarmin's MLR recovery (01 §3.5a) was not exercised on
  hardware; the fake watch's loss tests remain its only proof.
- The watch gave GFDI handle `0x80` in run 1 and **`0x82` in run 2**. Handles are the watch's
  choice; nothing may assume `0x80`.
- File pipes were registered reliable, rotating through `0x2018` … `0xE018` on handles
  `0x81`–`0x87`, exactly as Garmin Connect does (05 §8).

### What the watch asks for after SYNC_READY
Within 0.4 s of `SYNC_READY` the watch sends a burst of requests, the same ones Garmin Connect
answers. goarmin answers as follows, and **the watch accepted every answer**: no repeats, no
disconnect, no effect on the sync.

| Watch request | goarmin's answer |
|---|---|
| `CURRENT_TIME_REQUEST` | the time reply (02) |
| `MUSIC_CONTROL_CAPABILITIES` | `00` |
| `NOTIFICATION_SUBSCRIPTION` (`00 00`, then `01 00`) | `00` + the payload |
| core `connection_ready_notification` | kept |
| event_sharing `subscribe_request` (alert type ACCESSORY_UTILITIES_ACTIVITY_STATE) | discarded |
| live_track `status_notification`, `share_device_setting_notification`, `share_meta_config_notification` | discarded |
| group_live_track `get_live_track_connections_request` | discarded |
| sms_notification `sms_canned_list_request` | discarded |
| device_message `message_subscribe` | discarded |
| audio_prompts `set_language_request` | discarded |
| realtime_settings `changeNotification` | discarded |
| core `location_updated_set_enabled_request`, `get_location_request` (twice) | discarded |
| connect_iq_http `raw_resource_request` (5 during the sync) | `raw_resource_response { status: NETWORK_REQUEST_TIMED_OUT }` |

"Discarded" is the protobuf status DISCARDED / UNKNOWN_REQUEST (100), Gadgetbridge's answer for
services it does not serve.

- The proxied HTTP requests go to Garmin's servers: device messaging capabilities (`PATCH`), the
  LiveTrack settings (`GET`, twice), the day's lifestyle log and the user's contacts.
- **Privacy:** these requests carry a bearer token for the owner's Garmin account and the unit
  number, and `get_live_track_connections_request` carries the watch's last position. A goarmin
  trace therefore holds credentials and location. Traces stay in `~/goarmin-captures`, are never
  committed and should not be shared.

### Listing
- First listing, no cursor, `exclude_all_flags`/`get_flags` A5A5: two pages (4097 and 1687
  bytes), 142 items, `next_transaction_id` 38764, stored with the items.
- It holds everything not yet flagged as synced:
  - the files Garmin Connect only pulls and never flags (113 sports files, 11 workouts,
    2 courses, settings, device, totals, goals and a few more);
  - only two health files (FIT 35 and 74), recorded after the phone's last sync. The monitor,
    metrics, sleep and HRV files were not listed because Garmin Connect had archived them.
- One item has `format: GDXML_FULL_FILE` and no type name: the device XML (64 KB, the file Garmin
  Connect pulls with a dedicated listing, 05 §8). The default policy ignores it.
- Run 2 sent `ItemListRequest { transaction_id: 38764, exclude_all_flags: A5A5, get_flags: A5A5 }`
  and got one page with no items and `next_transaction_id` 38764 again. **The cursor works as an
  incremental listing.**
- **`FIT_TYPE_<n>` is the FIT `file_id.type` of the file**, for all 141 files.

| Listed as | Files | `file_id` | Made by |
|---|---|---|---|
| FIT_TYPE_1 | 1 | device | the watch |
| FIT_TYPE_2 | 1 | settings | the watch |
| FIT_TYPE_3 | 113 | sport | the watch |
| FIT_TYPE_5 | 11 | workout | Garmin Connect (product 65534) or no maker |
| FIT_TYPE_6 | 2 | course | 1 the watch, 1 Garmin Connect |
| FIT_TYPE_7, 10, 11 | 1 each | schedules, totals, goals | the watch |
| FIT_TYPE_35 | 1 | segment_list | the watch |
| FIT_TYPE_8, 29, 33 (2), 37, 65, 74 | 1 each | not in the public FIT profile | the watch |
| FIT_TYPE_59 | 1 | not in the public profile, 1735 messages | no maker, no serial |
| FIT_TYPE_87 | 1 | not in the public profile | Garmin Connect |

### Pulls
- All 141 `PullItemDataRequest`s (priority 24) were answered `SUCCESS`, transport pipe,
  `compression_window: 11`.
- 249 KB on the wire, 964 KB inflated (3.9 : 1).
- The watch closed every pipe itself (05 §8). Its `TransferStatusRequest` usually comes 45 ms
  after that close, **but sometimes before it** (up to 180 ms earlier). The engine accepts both
  orders.
- The two health files were stored and left pending, because `-no-archive` skips the archive. Run
  2 reported them as deferred again without pulling them a second time.

### Speed

Median per file, goarmin (141 pulls) against Garmin Connect's capture (34 pulls):

| Step | goarmin | Garmin Connect |
|---|---|---|
| pull request → `PullItemDataResponse` | 108 ms | 108 ms |
| register the pipe → response | 90 ms | 89 ms |
| configure → `00 00 00` | 90 ms | 89 ms |
| reply → data done (small files) | 135 ms | 91 ms |
| close → `TransferStatusRequest` | 46 ms | 45 ms |
| `TransferStatusRequest` → host's answer | 0 ms | **954 ms** |
| host's answer → next pull | 27 ms | 109 ms |
| **one file** | **0.54 s** | **1.83 s** |

- The link steps match. Garmin Connect is slower only because it waits about 1 s before answering
  each `TransferStatusRequest`.
- The same 18.7 KB FIT 74 file took 270 ms on goarmin's pipe and 359 ms on Garmin Connect's. The
  larger files moved at about 23 KB/s compressed (14.5 KB in 585 ms).
- goarmin's 27 ms between files is its own work: storing the file and committing the state, each
  fsynced.
- **Every watch answer lands on a ~45 ms grid, the connection interval.** In Garmin Connect's
  capture the watch itself asks for 30–45 ms (an L2CAP connection parameter update request, latency
  0, timeout 4 s) right after connecting. Android then runs at 45–48.75 ms for most of the sync,
  with one stretch at 18.75 ms. BlueZ accepts the watch's request too. A shorter interval would
  mean overriding the watch from the host (kernel connection parameters, needs root). It is not
  needed: a night of health files (about 30) takes about 20 s.

### Run 3: `-include-synced`, and the USB comparison
Same day, phone Bluetooth off again, `goarmin sync -once -no-archive -include-synced`. The listing
then sends only `get_flags: A5A5`, without `exclude_all_flags`, so it lists flagged files too.

- 181 items:
  - the 142 from run 1;
  - 36 activities (FIT 4), a FIT 77 and a FIT 72, all flagged by Garmin Connect but still kept;
  - a `BACKUP_PRIMARY` item (204 KB, not FIT, flagged), which the policy ignores like the device
    XML.
- 38 new files pulled, 2.3 MB inflated in 47.8 s, all FIT valid, again without one MLR
  retransmission. On the pipe the large activities moved at 35–38 KB/s compressed, about 1.1 s
  per file.
- **Not a single monitor, metrics, sleep, HRV or other health file**, although nothing was
  excluded. **The watch deletes a health file as soon as a host flags it synced**; only
  activities, FIT 72 and FIT 77 stay. A health file therefore reaches goarmin only if goarmin
  lists it before Garmin Connect archives it.
- FIT 72 is a daily health file: nap events (msg 412) plus undocumented messages 282, 384 and
  others. Garmin Connect flags it, so the default policy now archives it too.

Comparison by SHA-256 with the USB copies taken the night before (`~/goarmin-captures/fenix8-usb-*`):
- **175 files are byte-identical over Bluetooth and USB**, in 14 folders: Activity (36),
  Sports (113), Workouts (11), Courses (2), MltSport (2), Settings, Totals, Goals, Records,
  Location, Schedule and Calendar.
- 3 files were recorded after the USB copy (FIT 35, 72, 74).
- 5 types never appear on USB: FIT 1 (device), 37, 59, 77 and 87. **Bluetooth reaches everything
  USB shows, and more.**

### The watch writes health files only on request
- In the hours after the M2 runs, with the phone's Bluetooth off, the watch listed **no new
  file**, even after an hour.
- Garmin Connect's capture shows why. During each sync it sends the GarminConnect service's
  (Smart field 47) `CutWellnessFilesRequest { priority: STANDARD }`, `FA 02 04 1A 02 08 00`.
  - About 0.8 s after the answer, the watch announces a new monitoring file (FIT 32) with
    `ItemAddedNotification`, sometimes a FIT 41 as well.
  - Garmin Connect sent it five times; each time a file followed.
- **Until a host asks, the watch keeps its monitoring data to itself.** goarmin now asks
  (`fileaccess.Options.CutWellness`) and then waits up to 5 s for the announcements
  (`Options.Settle`) before it ends the sync.
  - Live, at 11:25: answer `SUCCESS` after 0.46 s, then a FIT 32 and a FIT 41 within 40 ms.
    Both were pulled in the same sync.
  - The FIT 32 covered the hour since Garmin Connect's last sync: heart rate, stress, Body
    Battery, respiration, steps, distance, intensity, altitude and the first resting HR (59).
- goarmin now also answers the watch's location requests as Garmin Connect does without
  location sharing:
  - `GetLocationResponse { LOCATION_SERVICES_DISABLED }`, `6A 04 22 02 08 04`;
  - `LocationUpdatedSetEnabledResponse { NOT_AVAILABLE, … }`, `6A 0A 32 08 08 02 12 04 08 00 10 02`.

  On their own, these answers did not make the watch write a file.
- The watch announces `wellness_support` and `lifestyle_logging_support` in its
  `FeatureCapabilitiesResponse` to Garmin Connect. goarmin sends no `FeatureCapabilitiesRequest`
  yet, and the cut works without it.

### Live values (realtime services)
- `goarmin live` registers the realtime Multi-Link services without MLR (01 §2.8). Live on
  2026-11-07 the first values came 10 s after the start, connection and handshake included, then
  every 1–3 s:

  | Service | Message | Meaning |
  |---|---|---|
  | REALTIME_HR (6) | `03 4c 3b ff ff` | heart rate 76, resting 59 (as the watch shows) |
  | REALTIME_STEPS (7) | `62 00 00 00 96 0f 00 00` | steps 98, goal 3990 |
  | REALTIME_BODY_BATTERY (20) | `53` | 83 % |
  | REALTIME_RESPIRATION (21) | `0e` | 14 breaths/min |
  | REALTIME_STRESS (13) | `16 00 fc 00` | stress 22 (u16 LE); the second value (−4, −2) is unknown. Gadgetbridge does not decode this service |

- The services share the connection with GFDI. A sync and a live view can run at the same time.

### M2 status
- Done:
  - a live sync over MLR, every file FIT valid and byte-identical to the USB copy where USB has
    it;
  - a repeat sync pulls only new files;
  - `-include-synced` fetches what the watch keeps after another host's archive (activities);
  - the traces can be decoded with snoopdecode.
- Open:
  1. **Sleep and HRV files** (FIT 49, 68): the watch writes them after a night. With the cut
     request, a sync in the morning should get them. Monitor files arrive with every sync now.
  2. **Archiving**, only once Garmin Connect has stopped syncing this watch (05 §7).
- When the watch deletes files:
  - health files: at the moment any host flags them synced (above);
  - activities, sports, settings, workouts, courses: never because of the flag;
  - on request: FileAccess `DeleteItemRequest` (04 §3.7). goarmin has it (`goarmin sync -delete
    <uid>`, `-delete-activities`); whether the Fenix 8 accepts it is the next live test
    (`~/Desktop/goarmin-delete-test.sh`, one activity that is backed up twice).
- Later:
  - pull the device XML and `BACKUP_PRIMARY` once, for the device registry and as a backup;
  - answer the watch's location requests (M8, together with weather and GPS assist data).

## Battery level (2026-11-07)

- **No report on its own.** During our syncs the Fenix 8 sent no `BATTERY_STATUS` (5023), so
  goarmin had no battery level after several syncs.
- **Asking for it.** goarmin now asks after the handshake, as Gadgetbridge does. It sends
  `PROTOBUF_REQUEST` with the Smart bytes `42 02 12 00`: device_status_service (8) carrying an
  empty remote_device_battery_status_request (2).
- **The answer.** It is expected in remote_device_battery_status_response (3): status (1, OK = 1),
  current_battery_level (2, percent), battery_status (3) and estimated_hours_remaining (4).
- **Handling.** The request is best effort with a 5 s limit; a watch that does not answer only
  leaves the level unknown.
- **On hardware (2026-11-07, firmware 23.40).** The Fenix 8 answers. The first sync that reached
  the watch reported 91 % (18:51); the answer came within the sync's first seconds.

## Pairing from the browser (D8, 2026-11-07)

The Fenix 8 was paired again through the dashboard's *Add a watch* (`serve --with-sync`,
firmware 23.40, BlueZ 5.87).
- **No code appeared**, neither in the browser nor on the watch. The bond file settles why: its
  keys are `Authenticated=3` (LE Secure Connections with a confirmed code), the first pairing's
  from the terminal. A fresh pairing without a code would have left `2`. So **the existing bond was
  reused** (the *Use this watch* path) and no pairing took place. The file's time (20:28:00) is
  the first sync's connection: BlueZ rewrites the file when a connection's parameters are agreed,
  which the Fenix asks for on every connection.
- Since then the session reports how the watch is paired (`bond`: existing, code, no_code), and
  the server logs `nodehub: pairing` (with `repair`) and `nodehub: paired`.
- **Agent.** This laptop runs a system `bt-agent -c NoInputNoOutput` as the default agent.
  goarmin registers its own agent (KeyboardDisplay) as a non-default agent on the D-Bus
  connection that calls `Device1.Pair`; bluetoothd asks the caller's agent. Whether that path
  shows the code is not yet confirmed on hardware (see above).
- "No matching connection for device" in bluetoothd's log appears after every disconnect: it
  says nothing about pairing.
- **Result.** Paired, Bonded and Trusted are true, and the bond works.
  The first sync (`wizard`, with older activities) started right after setup and finished OK in
  12 s: 192 items listed, nothing new to pull. The battery read during identify (91 %) was
  stored. `node.json` kept the same characteristic pair and client.
- **The watch cannot forget a host.** The Fenix 8 offers no way to remove a known connection from
  its phone list (the user, 2026-11-07). If *Pair again* removed the laptop's bond and the watch
  then refused a new pairing, there would be no way back on the watch's side. So *Pair again*
  stays for a pairing that is already broken, and a fresh pairing from the browser was not tested
  on this watch.
- **Not tried yet:** the first-session events (`GOARMIN_FIRST_SESSION`), the watch's own confirm
  timeout, a phone connected at the same time, and deleting a file from the watch.

## Missing health files (2026-11-07)

The user's account (created on 7 Nov) has no sleep (FIT 49), HRV status (FIT 68) or metrics (FIT
44) files. Its 100 nights come from the daily-health file (FIT 72), which carries night summaries
(scores, stage durations) but no stage changes, overnight HRV or SpO2. The watch offered none of
those files in the syncs since (192 items listed), and the USB copy of 7 Nov has empty Sleep,
HRVStatus and Metrics folders: an app that had marked them as synced made the watch delete them.
Still to find out: when the Fenix 8 writes these files for a new night, and whether it needs a
request (as monitoring files need CutWellnessFilesRequest).

## Feature capabilities (2026-11-07)

Garmin Connect's first Smart request after CONFIGURATION is a `FeatureCapabilitiesRequest` (core
f8). For syncing, its part `garmin_connect_request { version: 2, wellness_threshold_met_support:
true }` matters. The watch answers `wellness_support: true, lifestyle_logging_support: true`
(05 §3). goarmin had never sent it, and in about ten hours of goarmin syncs the watch offered
not one metrics (44), sleep (49) or HRV status (68) file, while Garmin Connect's single sync in
the capture listed 12 metrics files.

- goarmin now sends the same request without a GUID (`smart.GarminConnectFeatures`:
  `5A 04 08 02 10 01`, plus empty file-access and device-status requests). It does this in every
  sync and live link of `serve --with-sync` (`GOARMIN_FEATURES=0` leaves it out) and in
  `goarmin sync -features`.
- **On hardware (21:38):** the watch answered `guid_status` UNSET (no GUID sent), version 1,
  wellness and lifestyle-logging support, custom flags, fields 2, 10, 11, 16, 19. The sync ran
  normally. The cut answered `IGNORED`, two minutes after the previous cut.
- `WellnessThresholdMetNotification` (GarminConnect f5) and
  `LifestyleLoggingUpdateRequestNotification` (f7) are now logged and counted
  (`nodehub: the watch reported its wellness threshold met`); none came in this session.
- Still to see: whether metrics files arrive in the next syncs, and sleep and HRV status files
  after the next night.

## HRV history in the daily-health file (2026-11-07)

Message 401 in the daily-health file (FIT 72) carries the HRV status widget's four weeks. It has
28 slots, one per night, oldest first, as of the local time in field 0:

- field 2: the nightly HRV in ms;
- fields 3 and 4: the balanced range's lower and upper bound.

Unused slots are 0xFFFF. The last valid HRV slot is last night: on 7 Nov, slot 21 was 57 ms in
46–60, as the watch showed. Each slot before it is one night earlier. Two range values lingered
in slots 26 and 27, after last night, without an HRV value; goarmin ignores everything after
last night.

goarmin used to keep only last night. Since ingest version 4 it writes every night's value and
range (`hrv_last_night`, `hrv_target_low/high`). After a reingest, the user's account went from
1 to 22 nights of HRV (17 Oct–7 Nov), without any HRV status file (FIT 68).

## First metrics file, and the realtime services (2026-11-08)

- **Metrics file.** At 00:02 on 2026-11-08, the first sync after midnight listed a metrics file
  (FIT 44) for the first time since goarmin syncs this watch. It was made at 23:57:36, five
  hours after goarmin started announcing its FeatureCapabilities. It is 433 bytes and holds only
  two messages that neither the FIT profile nor Gadgetbridge names:
  - **241:** the timestamp, field 0 = the same instant in local time (+3600 s), field 1 = 0;
  - **281:** fields 0–3 invalid (`0xFFFFFFFF`), 7 and 8 invalid (`0xFF`), 11 = 158, the rest 0.

  Training load, status and readiness (369, 378, 403…) are not in it. The watch has no recent
  activity with heart rate to compute them from, which matches the mostly invalid 281. Sleep
  (49) and HRV status (68) files have still not been offered.
- **Realtime services** opened with `goarmin live` (it now prints the opened services):
  - SpO2 and beat intervals (HRV) are **accepted but send nothing** at rest. Gadgetbridge opens
    them the same way, with no extra enable command.
  - Active time is **refused**.
  - Intensity sends 14 bytes, ten of them zero in a week without intensity minutes, then the
    weekly goal (u32 LE, 150). The server logs every changed message of a service it cannot
    read yet ("realtime message not read yet"), so the other fields can be worked out once
    the week has intensity minutes.

## The Fenix 8 on USB (2026-11-08)

- On USB the Fenix 8 is `091e:51b8` with no USB name strings; its MTP interface is found and
  claimed through usbfs without root (libmtp's `mtp-probe` plus systemd's uaccess rule).
- **The USB serial is the unit number in hexadecimal**, zero-padded to 12 digits (unit
  3912345678 would be `0000e931a84e`). MTP DeviceInfo reports manufacturer "Garmin", model
  "fenix 8 - 47mm", version "2340" (23.40) and the same serial. One storage, "Internal
  Storage" (29.6 GB).
- GARMIN holds 241 FIT files and 169 others (maps, audio, apps, weather, logs). Compared with
  what Bluetooth delivered, USB adds **one metrics file (FIT 44) a day in `GARMIN/Debug`** (36
  files from 2026-10-08), activity summaries (FIT 20, some recorded by an earlier Instinct 2)
  and a few single files.
- `Sleep`, `HRVStatus`, `SkinTemp` and `SlpDisr` were empty at 01:30; `Metrics` held only the
  two files Bluetooth had brought. Whether a night fills them is still open.
