// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"cmp"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/karamble/goarmin/pkg/fitwatch"
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
	// Want says the workout belongs on this watch: the library's switch, or, never switched,
	// whether the watch has it. Switched on and then deleted on the watch, it is no longer
	// wanted.
	Want bool
	// Pending is what the next sync does about it: PendingSend, or PendingRemove (it waits to be
	// deleted on the watch, which takes no deletes); "" nothing.
	Pending string
	// Failed is why the last push of it to this watch failed ("" if it did not).
	Failed string
	// Outdated says the watch holds only an older version of it; OldCopies names the older
	// versions it holds beside the current one (they wait to be deleted on the watch).
	Outdated  bool
	OldCopies []string
}

// LibraryWatch is a watch the library reports on: one of the account's current watches that
// syncs (or took stock before).
type LibraryWatch struct {
	Device
	InventoryAt time.Time // zero: it has not taken stock yet
}

// LibraryWorkout is one workout of an account's library: the versions goarmin keeps of it (a
// watch's file, Garmin Connect's, goarmin's own edit), grouped by name and sport (their entry
// key), and where it is now.
type LibraryWorkout struct {
	// WorkoutRow is the version shown: goarmin's own (an edit), else a watch's (what a watch
	// runs), else the newest.
	WorkoutRow
	Versions []WorkoutRow      // every version, the shown one first
	Hidden   bool              // hidden from the library (and on no watch)
	Origins  []string          // OriginGoarmin, OriginWatch, OriginGarminConnect: where its versions came from
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

	// A version's name leads to its entry: a renamed workout's new name and its old one both.
	alias := map[string]string{}
	for _, r := range rows {
		if r.Origin == OriginGoarmin {
			alias[WorkoutKey(r.Name, r.Sport)] = r.EntryKey
		}
	}
	for _, r := range rows {
		if k := WorkoutKey(r.Name, r.Sport); alias[k] == "" {
			alias[k] = r.EntryKey
		}
	}
	entryOf := func(name, sport string) string {
		k := WorkoutKey(name, sport)
		if e := alias[k]; e != "" {
			return e
		}
		return k
	}

	// Which workout (by entry) each watch keeps, or kept, through the files its items are, and
	// which versions of it the items it lists now hold (by file sha; an item's name).
	type held struct{ sha, name string }
	type seen struct {
		on, gone int64 // the latest listing, the latest removal (Unix; 0: none)
		items    map[string]held
	}
	where := map[int64]map[string]*seen{}
	at := func(dev int64, k string) *seen {
		if where[dev] == nil {
			where[dev] = map[string]*seen{}
		}
		x := where[dev][k]
		if x == nil {
			x = &seen{items: map[string]held{}}
			where[dev][k] = x
		}
		return x
	}
	if len(lib.Watches) > 0 {
		q, err := s.db.R.QueryContext(ctx, `SELECT wi.device_id, wi.uid, wf.name, wf.sport, COALESCE(wi.listed_at, 0), COALESCE(wi.gone_at, 0),
				COALESCE(r.sha256, '')
			FROM watch_items wi JOIN workout_files wf ON wf.raw_file_id = wi.raw_file_id LEFT JOIN raw_files r ON r.id = wi.raw_file_id
			WHERE wf.user_id = ? AND wi.device_id IN (SELECT a.device_id FROM device_assignments a WHERE a.user_id = ? AND a.valid_to IS NULL)`,
			user, user)
		if err != nil {
			return lib, err
		}
		for q.Next() {
			var dev, listedAt, goneAt int64
			var uid, name, sport, sha string
			if err := q.Scan(&dev, &uid, &name, &sport, &listedAt, &goneAt, &sha); err != nil {
				q.Close()
				return lib, err
			}
			x := at(dev, entryOf(name, sport))
			if goneAt == 0 {
				x.on = max(x.on, max(listedAt, 1)) // linked by a sync, not yet listed: on it all the same
				x.items[uid] = held{sha, name}
			} else {
				x.gone = max(x.gone, goneAt)
			}
		}
		q.Close()
		if err := q.Err(); err != nil {
			return lib, err
		}
	}
	targets, sends, err := s.libraryWants(ctx, user)
	if err != nil {
		return lib, err
	}
	// A workout goarmin pushed is on the watch under the id the watch gave it, before the watch
	// hands the file back.
	for dev, byKey := range sends {
		for k, x := range byKey {
			if !x.ok || x.uid == "" || x.listed == 0 && x.gone == 0 {
				continue
			}
			y := at(dev, k)
			if x.gone == 0 {
				y.on = max(y.on, x.listed)
				if _, linked := y.items[x.uid]; !linked {
					y.items[x.uid] = held{sha: x.sha}
				}
			} else {
				y.gone = max(y.gone, x.gone)
			}
		}
	}
	hidden, err := s.hiddenWorkouts(ctx, user)
	if err != nil {
		return lib, err
	}
	groups := map[string]*LibraryWorkout{}
	var order []string
	for _, r := range rows {
		k := r.EntryKey
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
			return cmp.Or(cmp.Compare(originRank(a.Origin), originRank(b.Origin)), b.RecordedAt.Compare(a.RecordedAt), cmp.Compare(b.ID, a.ID))
		})
		g.WorkoutRow = g.Versions[0]
		for _, v := range g.Versions { // a watch's file has no description; Garmin Connect's may
			if g.Description == "" {
				g.Description = v.Description
			}
		}
		for _, o := range []string{OriginGoarmin, OriginWatch, OriginGarminConnect} {
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
			// Is the version shown the one the watch holds? Known for a watch's file and goarmin's
			// own; an older version alone on the watch makes it outdated, beside it an old copy.
			if cur := g.FileSHA; x != nil && p.State == PresenceOn && cur != "" {
				has := false
				for _, it := range x.items {
					has = has || it.sha == cur
				}
				for _, it := range x.items {
					if has && it.sha != cur && it.sha != "" {
						p.OldCopies = append(p.OldCopies, cmp.Or(it.name, g.Name))
					}
				}
				p.Outdated = !has && len(x.items) > 0
				slices.Sort(p.OldCopies)
			}
			t, switched := targets[w.ID][k]
			wantOn(&p, w, t, switched, sends[w.ID][k], g.FileSHA)
			g.Watches = append(g.Watches, p)
			if p.State == PresenceOn {
				g.Hidden = false // a workout on a watch shows, hidden or not
			}
		}
		if g.Hidden = hidden[k]; g.Hidden {
			for _, p := range g.Watches {
				g.Hidden = g.Hidden && p.State != PresenceOn
			}
		}
		lib.Workouts = append(lib.Workouts, *g)
	}
	// By the name shown (a renamed workout under its new name).
	slices.SortStableFunc(lib.Workouts, func(a, b LibraryWorkout) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(a.ID, b.ID))
	})
	return lib, nil
}

// originRank orders an entry's versions for showing: goarmin's own edit, then a watch's file,
// then Garmin Connect's.
func originRank(origin string) int {
	switch origin {
	case OriginGoarmin:
		return 0
	case OriginWatch:
		return 1
	}
	return 2
}

// ErrWorkoutNameTaken is a rename to the name (and sport) of another workout in the library.
var ErrWorkoutNameTaken = errors.New("store: another workout has that name")

// SaveWorkoutVersion makes or replaces goarmin's own version of user's library entry entryKey:
// w, its description and kept file fields included. It returns the version's id;
// ErrWorkoutNameTaken when another entry has a version of w's name and sport.
func (s *Store) SaveWorkoutVersion(ctx context.Context, user int64, entryKey string, w fitwatch.Workout) (int64, error) {
	if entryKey == "" {
		return 0, errors.New("store: a workout version needs its entry")
	}
	steps, err := json.Marshal(w.Steps)
	if err != nil {
		return 0, err
	}
	extra, err := extraJSON(w.Extra)
	if err != nil {
		return 0, err
	}
	estimated := fitwatch.Total(w.Steps)
	if x := w.Extra; x != nil && x.F21 != nil && *x.F21 > 0 {
		estimated = int(*x.F21 / 1000) // the original's stated total, kept
	}
	var id int64
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT entry_key, name, sport FROM workouts WHERE user_id = ?`, user)
		if err != nil {
			return err
		}
		want := WorkoutKey(w.Name, w.Sport)
		taken := false
		for rows.Next() {
			var entry, name, sport string
			if err := rows.Scan(&entry, &name, &sport); err != nil {
				rows.Close()
				return err
			}
			if entry == "" {
				entry = WorkoutKey(name, sport)
			}
			if entry != entryKey && WorkoutKey(name, sport) == want {
				taken = true
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if taken {
			return ErrWorkoutNameTaken
		}
		return tx.QueryRowContext(ctx, `INSERT INTO workouts (user_id, origin, device_id, entry_key, name, sport, sub_sport, steps, estimated_s,
			description, extra, recorded_at, raw_file_id) VALUES (?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
			ON CONFLICT (user_id, entry_key) WHERE origin = 'goarmin' DO UPDATE SET name = excluded.name, sport = excluded.sport,
				sub_sport = excluded.sub_sport, steps = excluded.steps, estimated_s = excluded.estimated_s,
				description = excluded.description, extra = excluded.extra, recorded_at = excluded.recorded_at
			RETURNING id`,
			user, OriginGoarmin, entryKey, strings.TrimSpace(w.Name), w.Sport, w.SubSport, string(steps), estimated,
			w.Description, extra, s.now().Unix()).Scan(&id)
	})
	return id, err
}

// DeleteWorkoutVersion removes goarmin's own version id of user's workouts: an edit or a rename
// (the entry falls back to its originals), or a workout made in goarmin, which then leaves the
// library with its switches, sends and hiding (unless a watch handed its file back). It
// answers ErrNotFound for any other id.
func (s *Store) DeleteWorkoutVersion(ctx context.Context, user, id int64) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		var entry string
		err := tx.QueryRowContext(ctx, `DELETE FROM workouts WHERE user_id = ? AND id = ? AND origin = ? RETURNING entry_key`,
			user, id, OriginGoarmin).Scan(&entry)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil || !strings.HasPrefix(entry, newEntryPrefix) {
			return err
		}
		var left int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workouts WHERE user_id = ? AND entry_key = ?`, user, entry).Scan(&left); err != nil || left > 0 {
			return err
		}
		for _, t := range []string{"workout_targets", "watch_sends", "workout_hidden"} {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+t+` WHERE user_id = ? AND entry_key = ?`, user, entry); err != nil {
				return err
			}
		}
		return nil
	})
}

// newEntryPrefix starts the entry key of a workout made in goarmin.
const newEntryPrefix = "goarmin:"

// CreateWorkout adds w, made in goarmin, to user's library as a workout of its own, and returns
// its version's id; ErrWorkoutNameTaken when the library has a workout of that name and sport.
func (s *Store) CreateWorkout(ctx context.Context, user int64, w fitwatch.Workout) (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return s.SaveWorkoutVersion(ctx, user, newEntryPrefix+hex.EncodeToString(b[:]), w)
}

// SetWorkoutFileSHA records the sha of the file goarmin's version id is written as.
func (s *Store) SetWorkoutFileSHA(ctx context.Context, user, id int64, sha string) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE workouts SET file_sha256 = ? WHERE user_id = ? AND id = ? AND origin = ?`, sha, user, id, OriginGoarmin)
	return err
}

// SetWorkoutHidden hides user's library workout entryKey from the library, or shows it again.
// A hidden workout shows while a watch has it.
func (s *Store) SetWorkoutHidden(ctx context.Context, user int64, entryKey string, hide bool) error {
	if !hide {
		_, err := s.db.W.ExecContext(ctx, `DELETE FROM workout_hidden WHERE user_id = ? AND entry_key = ?`, user, entryKey)
		return err
	}
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO workout_hidden (user_id, entry_key, at) VALUES (?, ?, ?)
		ON CONFLICT (user_id, entry_key) DO NOTHING`, user, entryKey, s.now().Unix())
	return err
}

func (s *Store) hiddenWorkouts(ctx context.Context, user int64) (map[string]bool, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT entry_key FROM workout_hidden WHERE user_id = ?`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
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
