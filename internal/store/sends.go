// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"database/sql"
	"time"
)

// What the next sync does about a library workout on one watch (WorkoutPresence.Pending).
const (
	PendingSend   = "send"   // wanted and not on the watch: the next sync sends it
	PendingRemove = "remove" // on the watch and not wanted: it waits to be deleted on the watch
)

// SendReasonLibrary is a workout sent because the library's switch wants it on the watch.
const SendReasonLibrary = "library"

// SetWorkoutTarget switches user's library workout entryKey on or off for a watch: on, the
// next sync sends it if the watch lacks it; off, it stays until deleted on the watch (a watch
// takes no deletes).
func (s *Store) SetWorkoutTarget(ctx context.Context, user, device int64, entryKey string, want bool) error {
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO workout_targets (user_id, device_id, entry_key, want, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, device_id, entry_key) DO UPDATE SET want = excluded.want, updated_at = excluded.updated_at`,
		user, device, entryKey, want, s.now().Unix())
	return err
}

// SendRecord is what became of one file goarmin pushed to a watch.
type SendRecord struct {
	EntryKey string
	Reason   string // SendReasonLibrary
	SHA256   string // the bytes sent
	// UID is the watch's id for the file ('' if it named none); Err why the push failed.
	UID string
	Err string
	// Refused says the watch said no (rather than the link failing); Unsupported that it takes
	// no pushes at all.
	Refused, Unsupported bool
}

// RecordSends records the pushes of one sync of device for user at at: what was sent and the
// watch's id for it (so the library knows the watch has it before its next inventory), or why
// it failed. A watch that took a push sends files; one that refused as unsupported does not.
func (s *Store) RecordSends(ctx context.Context, user, device int64, recs []SendRecord, at time.Time) error {
	if len(recs) == 0 {
		return nil
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		support := ""
		for _, r := range recs {
			ok := r.Err == ""
			if _, err := tx.ExecContext(ctx, `INSERT INTO watch_sends (user_id, device_id, entry_key, reason, sha256, item_uid, ok, refused, error, at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (user_id, device_id, entry_key, reason) DO UPDATE SET sha256 = excluded.sha256, item_uid = excluded.item_uid,
					ok = excluded.ok, refused = excluded.refused, error = excluded.error, at = excluded.at`,
				user, device, r.EntryKey, r.Reason, r.SHA256, r.UID, ok, r.Refused, r.Err, at.Unix()); err != nil {
				return err
			}
			switch {
			case ok:
				support = "yes"
				if r.UID != "" {
					if _, err := tx.ExecContext(ctx, `INSERT INTO watch_items (device_id, uid, type, listed_at) VALUES (?, ?, ?, ?)
						ON CONFLICT (device_id, uid) DO NOTHING`, device, r.UID, "FIT_TYPE_5", at.Unix()); err != nil {
						return err
					}
				}
			case r.Unsupported && support == "":
				support = "no"
			}
		}
		if support == "" {
			return nil
		}
		_, err := tx.ExecContext(ctx, `UPDATE devices SET send_support = ? WHERE id = ?`, support, device)
		return err
	})
}

// OutboxEntry is a library workout a watch should get: its entry and the version to send (the
// one shown: goarmin's own, else a watch's file, else Garmin Connect's).
type OutboxEntry struct {
	EntryKey    string
	Version     WorkoutRow
	Description string
}

// WatchOutbox returns what device should get of user's library at its next sync: every
// workout switched on for it that it does not have (and has not refused since).
func (s *Store) WatchOutbox(ctx context.Context, user, device int64) ([]OutboxEntry, error) {
	lib, err := s.WorkoutLibrary(ctx, user)
	if err != nil {
		return nil, err
	}
	at := -1
	for i, w := range lib.Watches {
		if w.ID == device {
			at = i
		}
	}
	if at < 0 {
		return nil, nil // not one of user's current watches
	}
	var out []OutboxEntry
	for _, w := range lib.Workouts {
		if w.Watches[at].Pending == PendingSend {
			out = append(out, OutboxEntry{EntryKey: w.EntryKey, Version: w.WorkoutRow, Description: w.Description})
		}
	}
	return out, nil
}

// target is a library switch: whether the workout is wanted on the watch, since when.
type target struct {
	want    bool
	updated time.Time
}

// sendState is the last push of a library workout to a watch, and where its item is.
type sendState struct {
	ok, refused bool
	err, uid    string
	at          time.Time
	listed      int64 // when an inventory last listed the item (0: not yet, or none)
	gone        int64 // when an inventory first missed it (0: not)
}

// libraryWants loads user's library switches and pushes, by watch and entry.
func (s *Store) libraryWants(ctx context.Context, user int64) (map[int64]map[string]target, map[int64]map[string]*sendState, error) {
	targets := map[int64]map[string]target{}
	rows, err := s.db.R.QueryContext(ctx, `SELECT device_id, entry_key, want, updated_at FROM workout_targets WHERE user_id = ?`, user)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var dev, at int64
		var key string
		var want bool
		if err := rows.Scan(&dev, &key, &want, &at); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if targets[dev] == nil {
			targets[dev] = map[string]target{}
		}
		targets[dev][key] = target{want: want, updated: time.Unix(at, 0)}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	sends := map[int64]map[string]*sendState{}
	rows, err = s.db.R.QueryContext(ctx, `SELECT s.device_id, s.entry_key, s.ok, s.refused, s.error, s.item_uid, s.at,
			COALESCE(wi.listed_at, 0), COALESCE(wi.gone_at, 0)
		FROM watch_sends s LEFT JOIN watch_items wi ON wi.device_id = s.device_id AND wi.uid = s.item_uid AND s.item_uid <> ''
		WHERE s.user_id = ? AND s.reason = ?`, user, SendReasonLibrary)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var dev, at int64
		var key string
		x := &sendState{}
		if err := rows.Scan(&dev, &key, &x.ok, &x.refused, &x.err, &x.uid, &at, &x.listed, &x.gone); err != nil {
			return nil, nil, err
		}
		x.at = time.Unix(at, 0)
		if sends[dev] == nil {
			sends[dev] = map[string]*sendState{}
		}
		sends[dev][key] = x
	}
	return targets, sends, rows.Err()
}

// wantOn works out whether a library workout is wanted on a watch and what the next sync does
// about it, into p (its State and At already set): shown is the version the library shows,
// listed the workout's items the watch lists now.
//
// The watch has the current version when it has the workout at all, unless the version shown
// is goarmin's own (an edit or a rename): then only once goarmin sent that version and the
// watch lists it. An older version it still lists is an old copy, to delete on the watch.
func wantOn(p *WorkoutPresence, w LibraryWatch, t target, switched bool, x *sendState, shown WorkoutRow, listed map[string]bool) {
	p.Want = p.State == PresenceOn // never switched: wanted while the watch has any version
	if switched {
		p.Want = t.want
		if t.want && p.State == PresenceRemoved && p.At.After(t.updated) {
			p.Want = false // deleted on the watch after it was switched on (in the same second, the switch came after)
		}
	}
	// The wish to send began with the switch, or with the edit if that is newer.
	since := t.updated
	edited := shown.Origin == OriginGoarmin
	if edited && shown.RecordedAt.After(since) {
		since = shown.RecordedAt
	}
	current := p.State == PresenceOn
	sentNow := x != nil && x.ok && x.uid != "" && listed[x.uid] && !x.at.Before(shown.RecordedAt)
	if edited {
		current = p.State == PresenceOn && sentNow
		for uid := range listed {
			if !sentNow || uid != x.uid {
				p.OldCopies++
			}
		}
	}
	if x != nil && !x.ok {
		p.Failed = x.err
	}
	switch {
	case p.Want && !current:
		switch {
		case w.SendSupport == "no":
			p.Failed = "this watch does not take workouts from goarmin"
		case x != nil && x.refused && !x.at.Before(since):
			// The watch refused it since it was switched on or edited: not again until then.
		case x != nil && x.ok && x.gone == 0 && !x.at.Before(since):
			// Sent, and the watch has not missed it (it may not have listed it yet).
		default:
			p.Pending = PendingSend
		}
	case !p.Want && p.State == PresenceOn:
		p.Pending = PendingRemove
	}
}
