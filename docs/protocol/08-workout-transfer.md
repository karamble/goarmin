# 08 — Sending workouts and schedules to the watch

How a host puts workouts and a training schedule on a Garmin watch, as Garmin Connect does it
with a Fenix 8 (firmware 23.40), and what goarmin does. Back to the [index](README.md).

**Sources:** three Bluetooth snoop captures of Garmin Connect on a phone, made on 2026-11-09:
1. sending workouts and scheduling one for the next day;
2. deleting workouts in the app;
3. removing the scheduled day.

Then goarmin's own pushes and delete attempts on the same watch. The captures stay outside the
repository. Decode one with `go run ./tools/snoopdecode -out DIR <btsnoop_hci.log>`, which saves
pulled files as `token_<n>.fit` and pushed ones as `push_<n>.fit`.

## Push (FileAccess, the newer protocol)

Garmin Connect sends workouts and schedules with FileAccess pushes. The older GFDI upload
(`CREATE_FILE` 5005, `UPLOAD_REQUEST` 5003), which Gadgetbridge uses, does not appear.

**One push, in order:**
1. **The host asks to push:** `PushItemDataRequest` (Smart FileAccess field 3):

   | Field | Value |
   |---|---|
   | `item.uid` | most significant half `0xffffffffffffffff`, least a number of the host's (Garmin Connect counts up) |
   | `item.data_type` | `{format GDXML_DATA_TYPE, gdxml_string "FIT_TYPE_5"}` for a workout, `"FIT_TYPE_7"` for a schedule |
   | `item.data_size` | the file's clear (uncompressed) size |
   | `priority` | 34 |
   | `supported_transports` | `[MULTILINK_TRANSPORT_PIPE]` |
   | `requested_compression_window` | 15 |

2. **The watch answers** `PushItemDataResponse` `{result SUCCESS, offset 0, transport pipe,
   transfer_handle, compression_window 11}`.
3. **The pipe:** the host registers a Multi-Link file-transfer service (client 1, reliable) and
   configures it for writing: `00 01 <transfer_handle u16 LE> 00 00`. The watch answers
   `00 00 00`.
4. **The data:** the host writes the file as one zlib stream and closes the handle when the
   last byte is acknowledged.
   - Garmin Connect's header is `38 8d`, declaring a 2 KiB window, the watch's 11.
   - goarmin writes stored deflate blocks under a header that declares the watch's window.
     Stored blocks never refer back, so they fit any window, and the watch takes them.
5. **The status:** the watch asks `TransferStatusRequest {uid, transfer_handle}`. The host
   answers `TransferStatusResponse`, empty, or with `next_transfer_priority` when another
   transfer follows.
6. **The new item:** for a workout, the watch announces `ItemAddedNotification` with **its own
   uid** (`4554…`, as for its own files), the same size and a transaction id.
   - From then on it lists the workout like any other `FIT_TYPE_5`.
   - It keeps the bytes unchanged: a sync pulls back exactly what was pushed.
   - A schedule is not announced and not listed.

A push takes about 2 s on the Fenix 8.

## Workout files

Garmin Connect's workout files, as pushed (goarmin's `fitwatch.EncodeWorkout` writes the same):

- **Records:** big-endian definitions.
- **file_id:** type 5, manufacturer 1 (Garmin), product 65534 (Garmin Connect), `time_created`
  and a `serial_number` of its own per file; no `number`.
- **file_creator:** software 2621, hardware 0.
- **workout:**
  - `wkt_name`, `wkt_description`, sport, sub sport, capabilities 32, `num_valid_steps`;
  - a total duration in fields 9 (`duration_type` time), 10 and 21 (milliseconds), with 23
    (`time_flag`) = 0;
  - field 16: 16 bytes, zeros (one file ended in `01`).
- **workout_step:**
  - a step without a target carries no target type, only zero `target_value` and
    `secondary_target_value`. Rest steps carry `open` for both targets;
  - the note is in `notes` and again in full as a `memo_glob` (mesg 27, field 8, zero-terminated);
  - exercises carry category and number, and `weight_display_unit` kilogram on every step;
  - **a repeat step** has `duration_type` 6, `duration_value` = the index of its first step,
    `target_value` = the count, and no intensity.
- **exercise_title:** one per exercise, with its display name.

## Schedule files: the whole calendar

- **One schedule file is the whole calendar:** each push replaces the watch's schedules.
  Garmin Connect pushes it again after every change; removing the last scheduled day pushed a
  file with **no entries** (72 bytes).
- **The file:**
  - file_id `{type 7, manufacturer 1, product 65534, time_created, serial 1, number 1}`;
  - file_creator `{software 26, hardware 0}`;
  - one `schedule` message (28) per day: manufacturer, product, serial number and time created
    **of the workout file it names**, `type` 0 (workout), `scheduled_time` = the day's local
    midnight (a local date-time), `completed` 0, field 10 = 255.
- **After the schedule,** Garmin Connect pushes each scheduled workout again, rebuilt with a
  fresh serial (the one the schedule names). The watch does not announce or list that copy: it
  keeps it with its calendar, not in the workout list.

## Removing a workout: not possible over Bluetooth

| Request | The Fenix 8 answers |
|---|---|
| `DeleteItemRequest {uid: the watch's id, direction PULL}` | `OPERATION_NOT_SUPPORTED` |
| `DeleteItemRequest {uid: the watch's id, direction PUSH}` | `ITEM_DOES_NOT_EXIST` |
| `DeleteItemRequest {uid: the host's push id, direction PUSH}` | `ITEM_DOES_NOT_EXIST` |

Garmin Connect doesn't remove workouts either: deleting workouts in the app sent nothing to the
watch. A workout stays on the watch until it is deleted there (Workouts › the workout ›
Delete). Scheduled days are removed by pushing the schedule again without them.

## In goarmin

- **Push:** `fileaccess.Engine.Push` sends a file; `fitwatch.EncodeWorkout` and
  `fitwatch.EncodeSchedule` write the files.
- **Probe:** `goarmin probe -device … -send FILE -type 5|7` pushes a file by hand;
  `-inventory 5` lists the watch's workouts.
