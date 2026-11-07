// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ErrExists is returned when an account with the email exists already.
var ErrExists = errors.New("store: an account with this email exists")

// Login is what a password login needs to know about an account.
type Login struct {
	User         User
	PasswordHash string
	Disabled     bool
}

// LoginByEmail returns the login data of an account.
func (s *Store) LoginByEmail(ctx context.Context, email string) (Login, error) {
	var l Login
	var created int64
	err := s.db.R.QueryRowContext(ctx, `SELECT id, email, display_name, timezone, units, is_admin, created_at, password_hash, disabled
		FROM users WHERE email = ?`, strings.TrimSpace(email)).Scan(&l.User.ID, &l.User.Email, &l.User.DisplayName,
		&l.User.Timezone, &l.User.Units, &l.User.IsAdmin, &created, &l.PasswordHash, &l.Disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return Login{}, ErrNotFound
	}
	l.User.CreatedAt = time.Unix(created, 0)
	return l, err
}

// CanLogIn reports whether any enabled account has a password or a passkey: until one has,
// goarmin offers the first-run setup.
func (s *Store) CanLogIn(ctx context.Context) (bool, error) {
	var n int
	err := s.db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u WHERE u.disabled = 0 AND (u.password_hash != ''
		OR EXISTS (SELECT 1 FROM webauthn_credentials c WHERE c.user_id = u.id))`).Scan(&n)
	return n > 0, err
}

// SetPassword stores a new password hash.
func (s *Store) SetPassword(ctx context.Context, user int64, hash string) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, user)
	return err
}

// ClaimSetup makes the first login: the account with email (an import may have created it)
// or a new one becomes an admin with this password.
func (s *Store) ClaimSetup(ctx context.Context, email, name, hash string) (User, error) {
	email = strings.TrimSpace(email)
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, is_admin = 1, disabled = 0,
			display_name = CASE WHEN ? != '' THEN ? ELSE display_name END WHERE email = ?`, hash, name, name, email)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			return nil
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO users (email, display_name, is_admin, password_hash, created_at)
			VALUES (?, ?, 1, ?, ?)`, email, name, hash, s.now().Unix())
		return err
	})
	if err != nil {
		return User{}, err
	}
	return s.UserByEmail(ctx, email)
}

// Session is a signed-in browser.
type Session struct {
	ID        string // the first 16 hex digits of the token hash: enough to name it, useless to use it
	UserID    int64
	CreatedAt time.Time
	LastSeen  time.Time
	ExpiresAt time.Time
	UserAgent string
	IP        string
}

// CreateSession stores a session under its token hash.
func (s *Store) CreateSession(ctx context.Context, user int64, tokenHash string, ttl time.Duration, ua, ip string) error {
	now := s.now()
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO sessions (token_hash, user_id, created_at, expires_at, last_seen, user_agent, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, tokenHash, user, now.Unix(), now.Add(ttl).Unix(), now.Unix(), truncate(ua, 200), ip)
	return err
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// SessionUser returns the user of a live session and refreshes its last-seen time (at most
// once a minute, to spare the writer).
func (s *Store) SessionUser(ctx context.Context, tokenHash string) (User, error) {
	var u User
	var created, lastSeen int64
	now := s.now().Unix()
	err := s.db.R.QueryRowContext(ctx, `SELECT u.id, u.email, u.display_name, u.timezone, u.units, u.is_admin, u.created_at, s.last_seen
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ? AND u.disabled = 0`, tokenHash, now).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.Timezone, &u.Units, &u.IsAdmin, &created, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	u.CreatedAt = time.Unix(created, 0)
	if now-lastSeen > 60 {
		_, _ = s.db.W.ExecContext(ctx, `UPDATE sessions SET last_seen = ? WHERE token_hash = ?`, now, tokenHash)
	}
	return u, nil
}

// DeleteSession ends a session.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.W.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteSessionByID ends one of user's sessions by its ID.
func (s *Store) DeleteSessionByID(ctx context.Context, user int64, id string) error {
	if len(id) != 16 {
		return ErrNotFound
	}
	res, err := s.db.W.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND substr(token_hash, 1, 16) = ?`, user, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Sessions lists user's live sessions, newest first.
func (s *Store) Sessions(ctx context.Context, user int64) ([]Session, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT substr(token_hash, 1, 16), created_at, last_seen, expires_at, user_agent, ip
		FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY last_seen DESC`, user, s.now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		x := Session{UserID: user}
		var c, l, e int64
		if err := rows.Scan(&x.ID, &c, &l, &e, &x.UserAgent, &x.IP); err != nil {
			return nil, err
		}
		x.CreatedAt, x.LastSeen, x.ExpiresAt = time.Unix(c, 0), time.Unix(l, 0), time.Unix(e, 0)
		out = append(out, x)
	}
	return out, rows.Err()
}

// PurgeSessions deletes expired sessions.
func (s *Store) PurgeSessions(ctx context.Context) error {
	_, err := s.db.W.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, s.now().Unix())
	return err
}

// Invite is an open invitation.
type Invite struct {
	Email     string
	CreatedBy int64
	ExpiresAt time.Time
}

// CreateInvite stores an invitation under its token hash.
func (s *Store) CreateInvite(ctx context.Context, tokenHash, email string, by int64, ttl time.Duration) error {
	now := s.now()
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO invites (token_hash, email, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		tokenHash, strings.TrimSpace(email), by, now.Unix(), now.Add(ttl).Unix())
	return err
}

// OpenInvite returns an unused, unexpired invitation.
func (s *Store) OpenInvite(ctx context.Context, tokenHash string) (Invite, error) {
	var inv Invite
	var exp int64
	err := s.db.R.QueryRowContext(ctx, `SELECT email, created_by, expires_at FROM invites
		WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`, tokenHash, s.now().Unix()).Scan(&inv.Email, &inv.CreatedBy, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return Invite{}, ErrNotFound
	}
	inv.ExpiresAt = time.Unix(exp, 0)
	return inv, err
}

// AcceptInvite creates the invited account and uses up the invitation, in one transaction.
func (s *Store) AcceptInvite(ctx context.Context, tokenHash, email, name, hash string) (User, error) {
	email = strings.TrimSpace(email)
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var want string
		err := tx.QueryRowContext(ctx, `SELECT email FROM invites WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
			tokenHash, s.now().Unix()).Scan(&want)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if want != "" && !strings.EqualFold(want, email) {
			return ErrNotFound
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE email = ?`, email).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrExists
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO users (email, display_name, password_hash, created_at) VALUES (?, ?, ?, ?)`,
			email, name, hash, s.now().Unix())
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		_, err = tx.ExecContext(ctx, `UPDATE invites SET used_at = ?, used_by = ? WHERE token_hash = ?`, s.now().Unix(), id, tokenHash)
		return err
	})
	if err != nil {
		return User{}, err
	}
	return s.UserByEmail(ctx, email)
}

// Users lists every account (admin view: no health data).
func (s *Store) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, email, display_name, timezone, units, is_admin, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var c int64
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Timezone, &u.Units, &u.IsAdmin, &c); err != nil {
			return nil, err
		}
		u.CreatedAt = time.Unix(c, 0)
		out = append(out, u)
	}
	return out, rows.Err()
}

// WebAuthnID returns user's passkey handle, creating it with newID on first use.
func (s *Store) WebAuthnID(ctx context.Context, user int64, newID func() []byte) ([]byte, error) {
	var id []byte
	if err := s.db.W.QueryRowContext(ctx, `SELECT webauthn_id FROM users WHERE id = ?`, user).Scan(&id); err != nil {
		return nil, err
	}
	if len(id) > 0 {
		return id, nil
	}
	id = newID()
	_, err := s.db.W.ExecContext(ctx, `UPDATE users SET webauthn_id = ? WHERE id = ? AND webauthn_id IS NULL`, id, user)
	if err != nil {
		return nil, err
	}
	return s.WebAuthnID(ctx, user, newID)
}

// UserByWebAuthnID finds the account of a passkey handle.
func (s *Store) UserByWebAuthnID(ctx context.Context, handle []byte) (User, error) {
	var u User
	var c int64
	err := s.db.R.QueryRowContext(ctx, `SELECT id, email, display_name, timezone, units, is_admin, created_at FROM users
		WHERE webauthn_id = ? AND disabled = 0`, handle).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Timezone, &u.Units, &u.IsAdmin, &c)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	u.CreatedAt = time.Unix(c, 0)
	return u, err
}

// StoredCredential is a registered passkey.
type StoredCredential struct {
	ID         []byte
	Name       string
	JSON       string // webauthn.Credential
	CreatedAt  time.Time
	LastUsedAt time.Time // zero if never
}

// Credentials returns user's passkeys.
func (s *Store) Credentials(ctx context.Context, user int64) ([]StoredCredential, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, name, credential, created_at, COALESCE(last_used_at, 0)
		FROM webauthn_credentials WHERE user_id = ? ORDER BY created_at`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredCredential
	for rows.Next() {
		var c StoredCredential
		var created, used int64
		if err := rows.Scan(&c.ID, &c.Name, &c.JSON, &created, &used); err != nil {
			return nil, err
		}
		c.CreatedAt = time.Unix(created, 0)
		if used > 0 {
			c.LastUsedAt = time.Unix(used, 0)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddCredential stores a new passkey.
func (s *Store) AddCredential(ctx context.Context, user int64, id []byte, name, credJSON string) error {
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO webauthn_credentials (id, user_id, name, credential, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, user, truncate(name, 100), credJSON, s.now().Unix())
	return err
}

// UpdateCredential stores a passkey's state after a login (its signature counter, flags).
func (s *Store) UpdateCredential(ctx context.Context, user int64, id []byte, credJSON string) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE webauthn_credentials SET credential = ?, last_used_at = ? WHERE id = ? AND user_id = ?`,
		credJSON, s.now().Unix(), id, user)
	return err
}

// DeleteCredential removes one of user's passkeys.
func (s *Store) DeleteCredential(ctx context.Context, user int64, id []byte) error {
	res, err := s.db.W.ExecContext(ctx, `DELETE FROM webauthn_credentials WHERE id = ? AND user_id = ?`, id, user)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UserDevices lists the devices user has or had, the current ones first, each with user's
// latest period (Device.Current, OwnedFrom, OwnedTo).
func (s *Store) UserDevices(ctx context.Context, user int64) ([]Device, error) {
	rows, err := s.db.R.QueryContext(ctx, userDeviceQuery+` ORDER BY (a.valid_to IS NULL) DESC, d.id`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		d, err := scanUserDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
