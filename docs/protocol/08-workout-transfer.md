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

Garmin Connect's workout files, as the Fenix 8 hands them over (31 files: Connect's pushes and the
workouts it keeps). goarmin's `fitwatch.EncodeWorkout` writes the same: each of the 31, read and
written again, compares equal message by message and field by field (`fitwatch.Compare`, the
opt-in `TestEncodeLikeGarminFiles`).

- **Records:** big-endian definitions. Messages in this order:
  1. file_id, then file_creator;
  2. workout, and right after it the description's memo;
  3. each step, followed by its notes' memo;
  4. the exercise titles.
- **file_id:** type 5, manufacturer 1 (Garmin), product 65534 (Garmin Connect), `time_created`
  and a `serial_number` of its own per file; no `number`.
- **file_creator:** software 2621, hardware 0. Older files (2023) say 2517.
- **workout:**
  - `wkt_name`, sport, sub sport, capabilities 32, `num_valid_steps`.
  - **`wkt_description`:** at most 160 bytes, a longer text cut to 157 bytes and "...". The
    whole text is in a `memo_glob` (mesg 26, parent 0, field 17): one part (the text and its
    zero), or parts of 255 bytes numbered by `part_index`, the last padded with zeros.
  - **The total time:** fields 10 and 21 (milliseconds), with 9 = 0 and 23 = 0 (enums). The
    total is a stated figure, not a sum of the steps: Garmin's library workouts state round
    times (300, 600, 1,800 s) that count repetitions at anything from 2 to 9 s.
  - **Field 16:** 16 bytes. All zeros for a user's workout, the last byte 1 mostly for Garmin's
    own library content (partner content such as Gold's Gym ends in 0).
  - **Garmin's originals:** some add 9 = 1, 22 = 0 (uint32) and 24 = 0 (enum).
- **workout_step:**
  - **A rest:** intensity rest, target type open, secondary target type open.
  - **An exercise for reps:** target type open. An exercise for time has no target (or open).
  - **Every step:** `target_value` 0, `secondary_target_value` 0 (unless it has a target), and
    `weight_display_unit` kilogram.
  - **A repeat step:** `duration_type` 6, `duration_value` = the index of its first step,
    `target_value` = the count, the undocumented **field 18 = 0** (an enum), and nothing else.
    Connect's JSON has `skipLastRestStep` and `smartRepeat` per repeat, both false; field 18 is
    likely one of them.
  - **An AMRAP:** `duration_type` 7, `target_value` its time in ms; no field 18.
  - **Notes:** the whole text in `notes` and again in a `memo_glob` (mesg 27, the step's index,
    field 8).
  - **Strength sets:** always [the exercise for reps, a rest for time] in a repeat. Repeats are
    one level deep.
- **exercise_title:** one per distinct exercise, with its display name. Garmin's order follows
  nothing in the workout (a hash map's, likely); goarmin keeps the order of a file it read.
- **An older writer** (Garmin's own content from 2023) leaves out the description memo, the
  secondary target values and the exercise titles. goarmin keeps that too for such a file.

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
- **Sync:** `syncnode.Options.Send` pushes files after the pulls and deletes and before the
  sync takes stock, so the inventory lists them. The server's outbox (`internal/outbox`) says
  what: the workouts the library switched on that a watch lacks. A push the watch refuses is not
  retried until the workout is switched on again; a lost link ends the sync's pushes.
- **Probe:** `goarmin probe -device … -send FILE -type 5|7` pushes a file by hand;
  `-inventory 5` lists the watch's workouts.
