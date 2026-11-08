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

// Node is a sync node: a computer that syncs the watches near it and uploads their files. It
// belongs to the account that signed it in.
type Node struct {
	ID        int64
	UserID    int64
	Name      string
	Hostname  string
	CreatedAt time.Time
	LastSeen  time.Time
	Version   string
	Revoked   bool
}

const nodeCols = `id, user_id, name, hostname, created_at, last_seen, version, revoked_at IS NOT NULL`

func scanNode(r interface{ Scan(...any) error }) (Node, error) {
	var n Node
	var created, seen int64
	if err := r.Scan(&n.ID, &n.UserID, &n.Name, &n.Hostname, &created, &seen, &n.Version, &n.Revoked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Node{}, ErrNotFound
		}
		return Node{}, err
	}
	n.CreatedAt = time.Unix(created, 0)
	if seen != 0 {
		n.LastSeen = time.Unix(seen, 0)
	}
	return n, nil
}

// CreateNode records a node signed in by user; only its token's hash is kept.
func (s *Store) CreateNode(ctx context.Context, user int64, name, hostname, tokenHash string) (Node, error) {
	now := s.now().Unix()
	res, err := s.db.W.ExecContext(ctx, `INSERT INTO sync_nodes (user_id, name, hostname, token_hash, created_at, last_seen)
		VALUES (?, ?, ?, ?, ?, ?)`, user, truncate(strings.TrimSpace(name), 60), truncate(hostname, 100), tokenHash, now, now)
	if err != nil {
		return Node{}, err
	}
	id, _ := res.LastInsertId()
	return s.NodeByID(ctx, id)
}

// NodeByID returns a node, revoked or not.
func (s *Store) NodeByID(ctx context.Context, id int64) (Node, error) {
	return scanNode(s.db.W.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM sync_nodes WHERE id = ?`, id))
}

// NodeByToken returns the live node a token belongs to, with its account (enabled).
func (s *Store) NodeByToken(ctx context.Context, tokenHash string) (Node, User, error) {
	n, err := scanNode(s.db.R.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM sync_nodes
		WHERE token_hash = ? AND revoked_at IS NULL`, tokenHash))
	if err != nil {
		return Node{}, User{}, err
	}
	var u User
	var created int64
	err = s.db.R.QueryRowContext(ctx, `SELECT id, email, display_name, timezone, units, is_admin, created_at
		FROM users WHERE id = ? AND disabled = 0`, n.UserID).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Timezone, &u.Units, &u.IsAdmin, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, User{}, ErrNotFound
	}
	if err != nil {
		return Node{}, User{}, err
	}
	u.CreatedAt = time.Unix(created, 0)
	return n, u, nil
}

// TouchNode records that the node was just heard from, and what it runs.
func (s *Store) TouchNode(ctx context.Context, id int64, version, hostname string) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE sync_nodes SET last_seen = ?, version = ?, hostname = CASE WHEN ? = '' THEN hostname ELSE ? END
		WHERE id = ?`, s.now().Unix(), truncate(version, 60), hostname, truncate(hostname, 100), id)
	return err
}

// Nodes lists the nodes of user, or every node with all (admins); revoked ones last.
func (s *Store) Nodes(ctx context.Context, user int64, all bool) ([]Node, error) {
	q := `SELECT ` + nodeCols + ` FROM sync_nodes WHERE user_id = ? OR ? ORDER BY revoked_at IS NOT NULL, name, id`
	rows, err := s.db.R.QueryContext(ctx, q, user, all)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// RenameNode changes a node's name.
func (s *Store) RenameNode(ctx context.Context, id int64, name string) error {
	res, err := s.db.W.ExecContext(ctx, `UPDATE sync_nodes SET name = ? WHERE id = ?`, truncate(strings.TrimSpace(name), 60), id)
	return affected(res, err)
}

// RevokeNode ends a node's token: it can no longer upload or take commands.
func (s *Store) RevokeNode(ctx context.Context, id int64) error {
	res, err := s.db.W.ExecContext(ctx, `UPDATE sync_nodes SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, s.now().Unix(), id)
	return affected(res, err)
}

// SetDeviceNode notes which node syncs the device (the one that reported it last).
func (s *Store) SetDeviceNode(ctx context.Context, device, node int64) error {
	_, err := s.db.W.ExecContext(ctx, `UPDATE devices SET node_id = ? WHERE id = ? AND node_id IS NOT ?`, node, device, node)
	return err
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Enrollment is a node waiting for its code to be approved in the dashboard.
type Enrollment struct {
	Code      string
	Name      string
	Hostname  string
	ExpiresAt time.Time
}

// CreateEnrollment starts a code approval: the node shows code and polls with the secret whose
// hash is pollHash.
func (s *Store) CreateEnrollment(ctx context.Context, code, pollHash, name, hostname string, ttl time.Duration) error {
	now := s.now()
	_, _ = s.db.W.ExecContext(ctx, `DELETE FROM node_enrollments WHERE expires_at < ?`, now.Unix())
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO node_enrollments (code, poll_hash, name, hostname, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`, code, pollHash, truncate(strings.TrimSpace(name), 60), truncate(hostname, 100),
		now.Unix(), now.Add(ttl).Unix())
	return err
}

// PendingEnrollment returns an open, unexpired, unapproved enrollment by its code.
func (s *Store) PendingEnrollment(ctx context.Context, code string) (Enrollment, error) {
	var e Enrollment
	var exp int64
	err := s.db.R.QueryRowContext(ctx, `SELECT code, name, hostname, expires_at FROM node_enrollments
		WHERE code = ? AND expires_at > ? AND user_id IS NULL`, code, s.now().Unix()).Scan(&e.Code, &e.Name, &e.Hostname, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return Enrollment{}, ErrNotFound
	}
	e.ExpiresAt = time.Unix(exp, 0)
	return e, err
}

// ApproveEnrollment makes the node for an open enrollment, owned by user; token goes to the
// node on its next poll (and is cleared then), tokenHash stays with the node.
func (s *Store) ApproveEnrollment(ctx context.Context, code string, user int64, token, tokenHash string) (Node, error) {
	e, err := s.PendingEnrollment(ctx, code)
	if err != nil {
		return Node{}, err
	}
	n, err := s.CreateNode(ctx, user, e.Name, e.Hostname, tokenHash)
	if err != nil {
		return Node{}, err
	}
	res, err := s.db.W.ExecContext(ctx, `UPDATE node_enrollments SET user_id = ?, node_id = ?, token = ? WHERE code = ? AND user_id IS NULL`,
		user, n.ID, token, code)
	return n, affected(res, err)
}

// PollEnrollment answers a node's poll: its token once approved (handed out once: the
// enrollment ends then), ErrNotFound when it expired or was never made, "" while it waits.
func (s *Store) PollEnrollment(ctx context.Context, pollHash string) (string, error) {
	var token sql.NullString
	var exp int64
	err := s.db.W.QueryRowContext(ctx, `SELECT token, expires_at FROM node_enrollments WHERE poll_hash = ?`, pollHash).Scan(&token, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if token.Valid && token.String != "" {
		_, err := s.db.W.ExecContext(ctx, `DELETE FROM node_enrollments WHERE poll_hash = ?`, pollHash)
		return token.String, err
	}
	if exp <= s.now().Unix() {
		return "", ErrNotFound
	}
	return "", nil
}
