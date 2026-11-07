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

// ErrStale is returned when a setting changed since the revision the writer read.
var ErrStale = errors.New("store: the setting changed in the meantime")

// Setting is one stored per-user setting.
type Setting struct {
	Value     string // JSON
	Rev       int64
	UpdatedAt time.Time
}

// Settings returns all of user's settings by key.
func (s *Store) Settings(ctx context.Context, user int64) (map[string]Setting, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT key, value, rev, updated_at FROM user_settings WHERE user_id = ?`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Setting{}
	for rows.Next() {
		var k string
		var st Setting
		var at int64
		if err := rows.Scan(&k, &st.Value, &st.Rev, &at); err != nil {
			return nil, err
		}
		st.UpdatedAt = time.Unix(at, 0)
		out[k] = st
	}
	return out, rows.Err()
}

// Setting returns one setting (ErrNotFound if unset).
func (s *Store) Setting(ctx context.Context, user int64, key string) (Setting, error) {
	var st Setting
	var at int64
	err := s.db.R.QueryRowContext(ctx, `SELECT value, rev, updated_at FROM user_settings WHERE user_id = ? AND key = ?`, user, key).
		Scan(&st.Value, &st.Rev, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return Setting{}, ErrNotFound
	}
	st.UpdatedAt = time.Unix(at, 0)
	return st, err
}

// PutSetting stores a setting. With ifRev ≥ 0 the write only succeeds if the stored revision
// is still ifRev (0 = unset); otherwise it is ErrStale. It returns the new revision.
func (s *Store) PutSetting(ctx context.Context, user int64, key, value string, ifRev int64) (int64, error) {
	var rev int64
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var cur int64
		err := tx.QueryRowContext(ctx, `SELECT rev FROM user_settings WHERE user_id = ? AND key = ?`, user, key).Scan(&cur)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if ifRev >= 0 && ifRev != cur {
			return ErrStale
		}
		rev = cur + 1
		_, err = tx.ExecContext(ctx, `INSERT INTO user_settings (user_id, key, value, rev, updated_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (user_id, key) DO UPDATE SET value = excluded.value, rev = excluded.rev, updated_at = excluded.updated_at`,
			user, key, value, rev, s.now().Unix())
		return err
	})
	return rev, err
}

// DeleteSetting removes a setting.
func (s *Store) DeleteSetting(ctx context.Context, user int64, key string) error {
	_, err := s.db.W.ExecContext(ctx, `DELETE FROM user_settings WHERE user_id = ? AND key = ?`, user, key)
	return err
}

// Profile is a partial profile update: nil fields stay as they are.
type Profile struct {
	DisplayName *string
	Timezone    *string
	Units       *string
}

// UpdateProfile changes the given profile fields.
func (s *Store) UpdateProfile(ctx context.Context, user int64, p Profile) error {
	var sets []string
	var args []any
	if p.DisplayName != nil {
		sets, args = append(sets, "display_name = ?"), append(args, *p.DisplayName)
	}
	if p.Timezone != nil {
		sets, args = append(sets, "timezone = ?"), append(args, *p.Timezone)
	}
	if p.Units != nil {
		sets, args = append(sets, "units = ?"), append(args, *p.Units)
	}
	if len(sets) == 0 {
		return nil
	}
	_, err := s.db.W.ExecContext(ctx, `UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id = ?`, append(args, user)...)
	return err
}
