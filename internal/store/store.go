// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package store is goarmin's SQLite database: users, devices, the raw files received and the
// health data derived from them (schema.go). One writer connection, a read-only pool.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/karamble/goarmin/pkg/sqlitex"
)

// Store is the database.
type Store struct {
	db  *sqlitex.DB
	now func() time.Time
}

// Open opens and migrates the database at path.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sqlitex.Open(path, sqlitex.Options{})
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx, migrations); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, now: time.Now}, nil
}

// SetClock replaces the clock (tests).
func (s *Store) SetClock(now func() time.Time) { s.now = now }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// ErrNotFound is returned when a looked-up row does not exist.
var ErrNotFound = errors.New("store: not found")

// User is an account.
type User struct {
	ID          int64
	Email       string
	DisplayName string
	Timezone    string
	Units       string // metric or imperial
	IsAdmin     bool
	CreatedAt   time.Time
}

// EnsureUser returns the user with this email, creating it if needed. The first user created
// becomes admin.
func (s *Store) EnsureUser(ctx context.Context, email, name string) (User, error) {
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return User{}, fmt.Errorf("store: %q is not an email address", email)
	}
	if u, err := s.UserByEmail(ctx, email); err == nil {
		return u, nil
	} else if !errors.Is(err, ErrNotFound) {
		return User{}, err
	}
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO users (email, display_name, is_admin, created_at)
		VALUES (?, ?, (SELECT COUNT(*) = 0 FROM users), ?)`, email, name, s.now().Unix())
	if err != nil {
		return User{}, err
	}
	return s.UserByEmail(ctx, email)
}

// UserByEmail looks a user up.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	var created int64
	err := s.db.R.QueryRowContext(ctx, `SELECT id, email, display_name, timezone, units, is_admin, created_at
		FROM users WHERE email = ?`, strings.TrimSpace(email)).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.Timezone, &u.Units, &u.IsAdmin, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	u.CreatedAt = time.Unix(created, 0)
	return u, err
}

// User looks a user up by id.
func (s *Store) User(ctx context.Context, id int64) (User, error) {
	var u User
	var created int64
	err := s.db.R.QueryRowContext(ctx, `SELECT id, email, display_name, timezone, units, is_admin, created_at
		FROM users WHERE id = ?`, id).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Timezone, &u.Units, &u.IsAdmin, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	u.CreatedAt = time.Unix(created, 0)
	return u, err
}

// Device is a watch.
type Device struct {
	ID       int64
	UnitID   uint32
	Product  uint16
	Name     string
	Firmware string
	Address  string
	// AfterFetch is what a sync does on the watch with fetched files: AfterFetchKeep,
	// AfterFetchArchive or AfterFetchArchiveDelete.
	AfterFetch string
	// Nickname is the owner's name for it ('' = Name, the watch's own).
	Nickname string
	// Battery is the last reported charge in percent (-1 unknown), at BatteryAt.
	Battery   int
	BatteryAt time.Time
	// Paused stops scheduled syncs (Sync now still works).
	Paused bool
	// DeleteSupport is whether the watch deletes files on request: "", "yes" or "no".
	DeleteSupport string

	// Set by UserDevice(s), seen from that user: Current says the watch is theirs now;
	// OwnedFrom and OwnedTo (0 = still) are their latest period with it, in Unix seconds.
	Current            bool
	OwnedFrom, OwnedTo int64
}

// After-fetch modes of a device.
const (
	AfterFetchKeep          = "keep"           // never flag files: another app can still sync them
	AfterFetchArchive       = "archive"        // flag them synced (the watch then deletes health files)
	AfterFetchArchiveDelete = "archive_delete" // also delete activities from the watch
)

// EnsureDevice returns the device with this unit number, creating it if needed; a non-zero
// product or a non-empty name, firmware or address updates the record.
func (s *Store) EnsureDevice(ctx context.Context, d Device) (Device, error) {
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO devices (unit_id, product, name, firmware, address, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (unit_id) DO UPDATE SET
			product = CASE WHEN excluded.product != 0 THEN excluded.product ELSE product END,
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE name END,
			firmware = CASE WHEN excluded.firmware != '' THEN excluded.firmware ELSE firmware END,
			address = CASE WHEN excluded.address != '' THEN excluded.address ELSE address END`,
		d.UnitID, d.Product, d.Name, d.Firmware, strings.ToUpper(d.Address), s.now().Unix())
	if err != nil {
		return Device{}, err
	}
	return s.DeviceByUnit(ctx, d.UnitID)
}

// DeviceByUnit looks a device up by its unit number.
func (s *Store) DeviceByUnit(ctx context.Context, unit uint32) (Device, error) {
	d, err := scanDevice(s.db.W.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices d WHERE d.unit_id = ?`, unit))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

// DeviceByAddress looks a device up by its Bluetooth address (any case).
func (s *Store) DeviceByAddress(ctx context.Context, addr string) (Device, error) {
	d, err := scanDevice(s.db.W.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices d WHERE d.address = ?
		ORDER BY d.id DESC LIMIT 1`, strings.ToUpper(addr)))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

// HadOtherOwner reports whether anyone but user ever owned device.
func (s *Store) HadOtherOwner(ctx context.Context, device, user int64) (bool, error) {
	var n int
	err := s.db.W.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_assignments WHERE device_id = ? AND user_id != ?`,
		device, user).Scan(&n)
	return n > 0, err
}

// deviceCols are a device's columns in scanDevice order.
const deviceCols = `d.id, d.unit_id, d.product, d.name, d.firmware, d.address, d.after_fetch, d.nickname,
	COALESCE(d.battery, -1), COALESCE(d.battery_at, 0), d.paused, d.delete_support`

func deviceDest(d *Device, batteryAt *int64) []any {
	return []any{&d.ID, &d.UnitID, &d.Product, &d.Name, &d.Firmware, &d.Address, &d.AfterFetch, &d.Nickname,
		&d.Battery, batteryAt, &d.Paused, &d.DeleteSupport}
}

func scanDevice(row interface{ Scan(...any) error }, extra ...any) (Device, error) {
	var d Device
	var at int64
	if err := row.Scan(append(deviceDest(&d, &at), extra...)...); err != nil {
		return Device{}, err
	}
	if at != 0 {
		d.BatteryAt = time.Unix(at, 0)
	}
	return d, nil
}

// userDeviceQuery selects a user's devices with that user's latest period of each.
const userDeviceQuery = `SELECT ` + deviceCols + `, a.valid_from, a.valid_to
	FROM devices d JOIN device_assignments a ON a.device_id = d.id
	WHERE a.user_id = ? AND a.valid_from = (SELECT MAX(b.valid_from) FROM device_assignments b
		WHERE b.device_id = d.id AND b.user_id = a.user_id)`

func scanUserDevice(row interface{ Scan(...any) error }) (Device, error) {
	var from int64
	var to sql.NullInt64
	d, err := scanDevice(row, &from, &to)
	if err != nil {
		return Device{}, err
	}
	d.OwnedFrom, d.OwnedTo, d.Current = from, to.Int64, !to.Valid
	return d, nil
}

// UserDevice returns a device user has or had; another user's is ErrNotFound. Former owners
// may read (their own period); use OwnedDevice for anything that acts on the watch.
func (s *Store) UserDevice(ctx context.Context, user, id int64) (Device, error) {
	d, err := scanUserDevice(s.db.R.QueryRowContext(ctx, userDeviceQuery+` AND d.id = ?`, user, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

// OwnedDevice returns a device that is user's now; anyone else's, a former one's included, is
// ErrNotFound.
func (s *Store) OwnedDevice(ctx context.Context, user, id int64) (Device, error) {
	d, err := s.UserDevice(ctx, user, id)
	if err == nil && !d.Current {
		return Device{}, ErrNotFound
	}
	return d, err
}

// OwnedWatch is a device with whoever owns it now (OwnerID 0: nobody).
type OwnedWatch struct {
	Device
	OwnerID               int64
	OwnerEmail, OwnerName string
}

// AllDevices lists every watch on the server with its current owner (for admins: identity only).
func (s *Store) AllDevices(ctx context.Context) ([]OwnedWatch, error) {
	now := s.now().Unix()
	rows, err := s.db.R.QueryContext(ctx, `SELECT `+deviceCols+`, COALESCE(u.id, 0), COALESCE(u.email, ''), COALESCE(u.display_name, '')
		FROM devices d LEFT JOIN device_assignments a ON a.device_id = d.id AND a.valid_from <= ? AND (a.valid_to IS NULL OR a.valid_to > ?)
		LEFT JOIN users u ON u.id = a.user_id ORDER BY d.id`, now, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OwnedWatch
	for rows.Next() {
		var w OwnedWatch
		d, err := scanDevice(rows, &w.OwnerID, &w.OwnerEmail, &w.OwnerName)
		if err != nil {
			return nil, err
		}
		w.Device = d
		w.Current = w.OwnerID != 0
		out = append(out, w)
	}
	return out, rows.Err()
}

// DeviceByID returns a device whoever owns it (for admins and the hub).
func (s *Store) DeviceByID(ctx context.Context, id int64) (Device, error) {
	d, err := scanDevice(s.db.R.QueryRowContext(ctx, `SELECT `+deviceCols+` FROM devices d WHERE d.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

// SetPaused stops or resumes a watch's scheduled syncs.
func (s *Store) SetPaused(ctx context.Context, device int64, paused bool) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE devices SET paused = ? WHERE id = ?`, paused, device)
	return err
}

// SetNickname names a watch for its owner (” = the watch's own name).
func (s *Store) SetNickname(ctx context.Context, device int64, name string) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE devices SET nickname = ? WHERE id = ?`, name, device)
	return err
}

// SetBattery records a watch's reported charge.
func (s *Store) SetBattery(ctx context.Context, device int64, percent int, at time.Time) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE devices SET battery = ?, battery_at = ? WHERE id = ?`, percent, at.Unix(), device)
	return err
}

// SetDeleteSupport records whether the watch deletes files on request ("yes" or "no").
func (s *Store) SetDeleteSupport(ctx context.Context, device int64, v string) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE devices SET delete_support = ? WHERE id = ?`, v, device)
	return err
}

// SetAfterFetch changes a device's after-fetch mode.
func (s *Store) SetAfterFetch(ctx context.Context, device int64, mode string) error {
	switch mode {
	case AfterFetchKeep, AfterFetchArchive, AfterFetchArchiveDelete:
	default:
		return errors.New("store: after_fetch must be keep, archive or archive_delete")
	}
	_, err := s.db.W.ExecContext(ctx, `UPDATE devices SET after_fetch = ? WHERE id = ?`, mode, device)
	return err
}

// SoleUser returns the only account that can sign in; with none or several it is ErrNotFound.
// A watch that nobody ever owned goes to the sole user of a single-person server; on a shared
// server the person adding it claims it.
func (s *Store) SoleUser(ctx context.Context) (User, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT u.id FROM users u WHERE u.disabled = 0 AND (u.password_hash != ''
		OR EXISTS (SELECT 1 FROM webauthn_credentials c WHERE c.user_id = u.id)) LIMIT 2`)
	if err != nil {
		return User{}, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return User{}, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return User{}, err
	}
	if len(ids) != 1 {
		return User{}, ErrNotFound
	}
	return s.User(ctx, ids[0])
}

// FirstAdmin returns the oldest admin account.
func (s *Store) FirstAdmin(ctx context.Context) (User, error) {
	var id int64
	err := s.db.R.QueryRowContext(ctx, `SELECT id FROM users WHERE is_admin = 1 AND disabled = 0 ORDER BY id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	return s.User(ctx, id)
}

// SyncRun is one sync of a device.
type SyncRun struct {
	ID                                  int64
	DeviceID                            int64
	OwnerID                             int64 // the owner then (0 = nobody)
	StartedBy                           string
	Started, Finished                   time.Time
	OK                                  bool
	Listed, Failed                      int
	Pulled, Archived, Deleted, Imported int
	Bytes                               int64
	Error, ErrorCode                    string
}

// How a sync started.
const (
	StartedBySchedule = "schedule"
	StartedByManual   = "manual"
	StartedByWizard   = "wizard"
	StartedByDelete   = "delete"
)

// RecordSyncRun appends to the sync log.
func (s *Store) RecordSyncRun(ctx context.Context, r SyncRun) error {
	if r.StartedBy == "" {
		r.StartedBy = StartedBySchedule
	}
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO sync_runs (device_id, owner_id, started_by, started_at, finished_at, ok,
		listed, failed, pulled, archived, deleted, bytes, imported, error, error_code)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.DeviceID, nullIf0(r.OwnerID), r.StartedBy, r.Started.Unix(),
		r.Finished.Unix(), r.OK, r.Listed, r.Failed, r.Pulled, r.Archived, r.Deleted, r.Bytes, r.Imported,
		truncate(r.Error, 500), r.ErrorCode)
	return err
}

// SyncRuns returns the latest runs of a device that happened while user owned it, newest first:
// the next owner's syncs are theirs alone.
func (s *Store) SyncRuns(ctx context.Context, user, device int64, limit int) ([]SyncRun, error) {
	return s.querySyncRuns(ctx, `SELECT `+syncRunCols+` FROM sync_runs r
		WHERE r.device_id = ? AND EXISTS (SELECT 1 FROM device_assignments a WHERE a.device_id = r.device_id AND a.user_id = ?
			AND a.valid_from <= r.started_at AND (a.valid_to IS NULL OR a.valid_to > r.started_at))
		ORDER BY r.started_at DESC, r.id DESC LIMIT ?`, device, user, limit)
}

// UserSyncRuns pages through the runs of user's watches from user's own time with each (the
// sync log), newest first: device 0 means all, problems keeps the failed ones, before is the
// last id of the previous page (0 for the first).
func (s *Store) UserSyncRuns(ctx context.Context, user, device int64, problems bool, before int64, limit int) ([]SyncRun, error) {
	q := `SELECT ` + syncRunCols + ` FROM sync_runs r
		WHERE EXISTS (SELECT 1 FROM device_assignments a WHERE a.device_id = r.device_id AND a.user_id = ?
			AND a.valid_from <= r.started_at AND (a.valid_to IS NULL OR a.valid_to > r.started_at))`
	args := []any{user}
	if device != 0 {
		q += ` AND r.device_id = ?`
		args = append(args, device)
	}
	if problems {
		q += ` AND r.ok = 0`
	}
	if before > 0 {
		q += ` AND r.id < ?`
		args = append(args, before)
	}
	q += ` ORDER BY r.id DESC LIMIT ?`
	return s.querySyncRuns(ctx, q, append(args, limit)...)
}

// RecentSyncRuns returns a device's latest runs whoever owned it, newest first (for the hub's
// own status after a restart).
func (s *Store) RecentSyncRuns(ctx context.Context, device int64, limit int) ([]SyncRun, error) {
	return s.querySyncRuns(ctx, `SELECT `+syncRunCols+` FROM sync_runs r WHERE r.device_id = ? ORDER BY r.started_at DESC, r.id DESC LIMIT ?`,
		device, limit)
}

const syncRunCols = `r.id, r.device_id, COALESCE(r.owner_id, 0), r.started_by, r.started_at, r.finished_at, r.ok,
	r.listed, r.failed, r.pulled, r.archived, r.deleted, r.bytes, r.imported, r.error, r.error_code`

func (s *Store) querySyncRuns(ctx context.Context, q string, args ...any) ([]SyncRun, error) {
	rows, err := s.db.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncRun
	for rows.Next() {
		var r SyncRun
		var st, fin int64
		if err := rows.Scan(&r.ID, &r.DeviceID, &r.OwnerID, &r.StartedBy, &st, &fin, &r.OK, &r.Listed, &r.Failed, &r.Pulled,
			&r.Archived, &r.Deleted, &r.Bytes, &r.Imported, &r.Error, &r.ErrorCode); err != nil {
			return nil, err
		}
		r.Started, r.Finished = time.Unix(st, 0), time.Unix(fin, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}

// Owner returns who wore device at time t (Unix seconds).
func (s *Store) Owner(ctx context.Context, device int64, t int64) (int64, error) {
	var u int64
	err := s.db.W.QueryRowContext(ctx, `SELECT user_id FROM device_assignments
		WHERE device_id = ? AND valid_from <= ? AND (valid_to IS NULL OR valid_to > ?)
		ORDER BY valid_from DESC LIMIT 1`, device, t, t).Scan(&u)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return u, err
}

// ErrOwned is returned when a device has an owner already.
var ErrOwned = errors.New("store: the watch belongs to someone")

// ReleaseDevice ends user's ownership of device now (a hand-over): from now on it is nobody's
// until someone claims it. Data recorded before stays with user.
func (s *Store) ReleaseDevice(ctx context.Context, device, user int64) error {
	res, err := s.db.W.ExecContext(ctx, `UPDATE device_assignments SET valid_to = ?
		WHERE device_id = ? AND user_id = ? AND valid_to IS NULL`, s.now().Unix(), device, user)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClaimDevice makes device user's. The new period starts where the last one ended (or at the
// beginning, for a watch nobody owned), so whatever the watch recorded in between belongs to
// the claimant. The watch's settings start over. A device someone owns now is ErrOwned.
func (s *Store) ClaimDevice(ctx context.Context, device, user int64) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		var open int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_assignments WHERE device_id = ? AND valid_to IS NULL`,
			device).Scan(&open); err != nil {
			return err
		}
		if open > 0 {
			return ErrOwned
		}
		var from int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(valid_to), 0) FROM device_assignments WHERE device_id = ?`,
			device).Scan(&from); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO device_assignments (device_id, user_id, valid_from) VALUES (?, ?, ?)`,
			device, user, from); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE devices SET after_fetch = ?, nickname = '', paused = 0 WHERE id = ?`,
			AfterFetchKeep, device)
		return err
	})
}

// AssignIfUnowned gives a device that nobody owns yet to user, from the beginning of its
// data, and returns the device's current owner.
func (s *Store) AssignIfUnowned(ctx context.Context, device, user int64) (int64, error) {
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO device_assignments (device_id, user_id, valid_from)
		SELECT ?, ?, 0 WHERE NOT EXISTS (SELECT 1 FROM device_assignments WHERE device_id = ?)`, device, user, device)
	if err != nil {
		return 0, err
	}
	return s.Owner(ctx, device, s.now().Unix())
}
