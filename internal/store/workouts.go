// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"cmp"
	"context"
	"database/sql"
	"slices"
	"strings"
	"time"

	"github.com/karamble/goarmin/pkg/garmin/product"
)

// WorkoutFITType is a watch's workout file type: the one type a sync takes stock of, linked to
// the files a sync stores.
const WorkoutFITType = 5

// LinkWatchItem records that the watch's item uid (of a GDXML type, "FIT_TYPE_5") is the raw
// file with the given id: a sync stored it as "<uid>.fit". A file the raw store had already (the
// same bytes under another item) links too.
func (s *Store) LinkWatchItem(ctx context.Context, device int64, uid, typ string, file int64) error {
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO watch_items (device_id, uid, type, raw_file_id) VALUES (?, ?, ?, ?)
		ON CONFLICT (device_id, uid) DO UPDATE SET raw_file_id = excluded.raw_file_id, type = excluded.type`, device, uid, typ, file)
	return err
}

// InventoryItem is one item a watch listed when it took stock.
type InventoryItem struct {
	UID  string
	Type string
	Size uint64
}

// WatchInventory is what a watch kept of some GDXML types when it took stock at At.
type WatchInventory struct {
	Types []string
	Items []InventoryItem
	At    time.Time
}

// SetWatchInventory records an inventory of device: what it lists is on the watch; an item of
// those types it listed before, or a sync stored, and does not list now is gone. Items not yet
// linked to a file are linked to the raw file a sync stored under their uid.
func (s *Store) SetWatchInventory(ctx context.Context, device int64, inv WatchInventory) error {
	at := inv.At.Unix()
	listed := map[string]bool{}
	for _, it := range inv.Items {
		listed[it.UID] = true
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		for _, it := range inv.Items {
			if _, err := tx.ExecContext(ctx, `INSERT INTO watch_items (device_id, uid, type, size, listed_at) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT (device_id, uid) DO UPDATE SET type = excluded.type, size = excluded.size, listed_at = excluded.listed_at,
					gone_at = NULL`, device, it.UID, it.Type, int64(it.Size), at); err != nil {
				return err
			}
		}
		for _, typ := range inv.Types {
			rows, err := tx.QueryContext(ctx, `SELECT uid FROM watch_items WHERE device_id = ? AND type = ? AND gone_at IS NULL`, device, typ)
			if err != nil {
				return err
			}
			var gone []string
			for rows.Next() {
				var uid string
				if err := rows.Scan(&uid); err != nil {
					rows.Close()
					return err
				}
				if !listed[uid] {
					gone = append(gone, uid)
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			for _, uid := range gone {
				if _, err := tx.ExecContext(ctx, `UPDATE watch_items SET gone_at = ? WHERE device_id = ? AND uid = ?`, at, device, uid); err != nil {
					return err
				}
			}
		}
		// Files a sync stored before items were linked at all (or without a device) carry the
		// item's uid in their name.
		if _, err := tx.ExecContext(ctx, `UPDATE watch_items SET raw_file_id = (SELECT r.id FROM raw_files r
				WHERE r.source_name = watch_items.uid || '.fit' AND (r.device_id = watch_items.device_id OR r.device_id IS NULL) ORDER BY r.id LIMIT 1)
			WHERE device_id = ? AND raw_file_id IS NULL AND gone_at IS NULL`, device); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE devices SET inventory_at = ? WHERE id = ?`, at, device)
		return err
	})
}

// Where a library workout is, seen from one watch.
const (
	PresenceOn      = "on"      // the watch keeps it now
	PresenceRemoved = "removed" // it did, and no longer does (deleted on the watch, or by Garmin Connect)
	PresenceAbsent  = "absent"  // the watch has not had it
	PresenceUnknown = "unknown" // the watch has not taken stock yet
)

// WorkoutPresence is where a library workout is on one watch: State (Presence*) since At (on:
// when the watch last listed it; removed: when it first did not).
type WorkoutPresence struct {
	DeviceID int64
	State    string
	At       time.Time
}

// LibraryWatch is a watch the library reports on: one of the account's current watches that
// syncs (or took stock before).
type LibraryWatch struct {
	Device
	InventoryAt time.Time // zero: it has not taken stock yet
}

// LibraryWorkout is one workout of an account's library: the versions goarmin keeps of it (a
// watch's file, Garmin Connect's), grouped by name and sport, and where it is now.
type LibraryWorkout struct {
	// WorkoutRow is the version shown: a watch's (what a watch runs), else the newest.
	WorkoutRow
	Versions []WorkoutRow      // every version, the shown one first
	Origins  []string          // OriginWatch, OriginGarminConnect: where its versions came from
	Watches  []WorkoutPresence // one per Library.Watches, in that order
}

// Library is an account's workout library.
type Library struct {
	Workouts []LibraryWorkout // by name
	Watches  []LibraryWatch
}

// WorkoutKey groups versions of one workout: its name (case and spacing aside) and its sport.
func WorkoutKey(name, sport string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " ")) + "\x00" + sport
}

// WorkoutLibrary returns user's workout library: every stored workout once, with the versions
// of it, and on which of user's watches it is.
func (s *Store) WorkoutLibrary(ctx context.Context, user int64) (Library, error) {
	var lib Library
	rows, err := s.Workouts(ctx, user, 0)
	if err != nil {
		return lib, err
	}
	devs, err := s.UserDevices(ctx, user)
	if err != nil {
		return lib, err
	}
	stocked := map[int64]time.Time{}
	q, err := s.db.R.QueryContext(ctx, `SELECT d.id, d.inventory_at FROM devices d JOIN device_assignments a ON a.device_id = d.id
		WHERE a.user_id = ? AND a.valid_to IS NULL AND d.inventory_at IS NOT NULL`, user)
	if err != nil {
		return lib, err
	}
	for q.Next() {
		var id, at int64
		if err := q.Scan(&id, &at); err != nil {
			q.Close()
			return lib, err
		}
		stocked[id] = time.Unix(at, 0)
	}
	q.Close()
	if err := q.Err(); err != nil {
		return lib, err
	}
	for _, d := range devs {
		at, ok := stocked[d.ID]
		if d.Current && d.Kind == product.Watch && (d.Address != "" || ok) {
			lib.Watches = append(lib.Watches, LibraryWatch{Device: d, InventoryAt: at})
		}
	}

	// Which workout (by key) each watch keeps, or kept, through the files its items are.
	type seen struct{ on, gone int64 } // the latest listing, the latest removal (Unix; 0: none)
	where := map[int64]map[string]*seen{}
	if len(lib.Watches) > 0 {
		q, err := s.db.R.QueryContext(ctx, `SELECT wi.device_id, wf.name, wf.sport, COALESCE(wi.listed_at, 0), COALESCE(wi.gone_at, 0)
			FROM watch_items wi JOIN workout_files wf ON wf.raw_file_id = wi.raw_file_id
			WHERE wf.user_id = ? AND wi.device_id IN (SELECT a.device_id FROM device_assignments a WHERE a.user_id = ? AND a.valid_to IS NULL)`,
			user, user)
		if err != nil {
			return lib, err
		}
		for q.Next() {
			var dev, listedAt, goneAt int64
			var name, sport string
			if err := q.Scan(&dev, &name, &sport, &listedAt, &goneAt); err != nil {
				q.Close()
				return lib, err
			}
			if where[dev] == nil {
				where[dev] = map[string]*seen{}
			}
			k := WorkoutKey(name, sport)
			x := where[dev][k]
			if x == nil {
				x = &seen{}
				where[dev][k] = x
			}
			if goneAt == 0 {
				x.on = max(x.on, max(listedAt, 1)) // linked by a sync, not yet listed: on it all the same
			} else {
				x.gone = max(x.gone, goneAt)
			}
		}
		q.Close()
		if err := q.Err(); err != nil {
			return lib, err
		}
	}

	groups := map[string]*LibraryWorkout{}
	var order []string
	for _, r := range rows {
		k := WorkoutKey(r.Name, r.Sport)
		g := groups[k]
		if g == nil {
			g = &LibraryWorkout{}
			groups[k] = g
			order = append(order, k)
		}
		g.Versions = append(g.Versions, r)
	}
	for _, k := range order {
		g := groups[k]
		slices.SortStableFunc(g.Versions, func(a, b WorkoutRow) int {
			if (a.Origin == OriginWatch) != (b.Origin == OriginWatch) {
				if a.Origin == OriginWatch {
					return -1
				}
				return 1
			}
			return cmp.Or(b.RecordedAt.Compare(a.RecordedAt), cmp.Compare(b.ID, a.ID))
		})
		g.WorkoutRow = g.Versions[0]
		for _, v := range g.Versions { // a watch's file has no description; Garmin Connect's may
			if g.Description == "" {
				g.Description = v.Description
			}
		}
		for _, o := range []string{OriginWatch, OriginGarminConnect} {
			if slices.ContainsFunc(g.Versions, func(v WorkoutRow) bool { return v.Origin == o }) {
				g.Origins = append(g.Origins, o)
			}
		}
		for _, w := range lib.Watches {
			p := WorkoutPresence{DeviceID: w.ID, State: PresenceAbsent}
			x := where[w.ID][k]
			switch {
			case x != nil && x.on > 0:
				p.State = PresenceOn
				if x.on > 1 {
					p.At = time.Unix(x.on, 0)
				}
			case x != nil && x.gone > 0:
				p.State, p.At = PresenceRemoved, time.Unix(x.gone, 0)
			case w.InventoryAt.IsZero():
				p.State = PresenceUnknown
			}
			g.Watches = append(g.Watches, p)
		}
		lib.Workouts = append(lib.Workouts, *g)
	}
	return lib, nil
}

// LibraryWorkoutByID returns the library workout one of whose versions has this id, or
// ErrNotFound.
func (s *Store) LibraryWorkoutByID(ctx context.Context, user, id int64) (LibraryWorkout, []LibraryWatch, error) {
	lib, err := s.WorkoutLibrary(ctx, user)
	if err != nil {
		return LibraryWorkout{}, nil, err
	}
	for _, w := range lib.Workouts {
		if slices.ContainsFunc(w.Versions, func(v WorkoutRow) bool { return v.ID == id }) {
			return w, lib.Watches, nil
		}
	}
	return LibraryWorkout{}, nil, ErrNotFound
}
