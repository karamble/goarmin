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

// MCP token scopes: what an assistant holding the token may do.
const (
	MCPRead  = "read"  // everything readable
	MCPWrite = "write" // also plans, syncs, settings, imports, pairing and confirmed removals
	MCPAdmin = "admin" // also users, invites, sync nodes and every watch (admins only)
)

// MCPRetention is how long the call log keeps a call.
const MCPRetention = 30 * 24 * time.Hour

// MCPToken is a bearer token an account made for an AI assistant (Settings › MCP).
type MCPToken struct {
	ID        int64
	UserID    int64
	Name      string
	Prefix    string
	Scope     string
	CreatedAt time.Time
	ExpiresAt time.Time // zero: never
	LastUsed  time.Time // zero: not yet
	Revoked   time.Time // zero: live
}

const mcpTokenCols = `id, user_id, name, prefix, scope, created_at, expires_at, last_used_at, revoked_at`

func scanMCPToken(r interface{ Scan(...any) error }) (MCPToken, error) {
	var t MCPToken
	var created int64
	var expires, used, revoked sql.NullInt64
	if err := r.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &t.Scope, &created, &expires, &used, &revoked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MCPToken{}, ErrNotFound
		}
		return MCPToken{}, err
	}
	t.CreatedAt = time.Unix(created, 0)
	unix := func(v sql.NullInt64) time.Time {
		if !v.Valid {
			return time.Time{}
		}
		return time.Unix(v.Int64, 0)
	}
	t.ExpiresAt, t.LastUsed, t.Revoked = unix(expires), unix(used), unix(revoked)
	return t, nil
}

// Live reports whether the token still works at now: not revoked and not expired.
func (t MCPToken) Live(now time.Time) bool {
	return t.Revoked.IsZero() && (t.ExpiresAt.IsZero() || now.Before(t.ExpiresAt))
}

// CreateMCPToken records a token for user; only its hash is kept. expires zero: never.
func (s *Store) CreateMCPToken(ctx context.Context, user int64, name, prefix, tokenHash, scope string, expires time.Time) (MCPToken, error) {
	var exp any
	if !expires.IsZero() {
		exp = expires.Unix()
	}
	res, err := s.db.W.ExecContext(ctx, `INSERT INTO mcp_tokens (user_id, name, prefix, token_hash, scope, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, user, truncate(strings.TrimSpace(name), 60), prefix, tokenHash, scope, s.now().Unix(), exp)
	if err != nil {
		return MCPToken{}, err
	}
	id, _ := res.LastInsertId()
	return scanMCPToken(s.db.W.QueryRowContext(ctx, `SELECT `+mcpTokenCols+` FROM mcp_tokens WHERE id = ?`, id))
}

// MCPTokens lists user's tokens, live ones first, newest first.
func (s *Store) MCPTokens(ctx context.Context, user int64) ([]MCPToken, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT `+mcpTokenCols+` FROM mcp_tokens WHERE user_id = ?
		ORDER BY revoked_at IS NOT NULL, created_at DESC, id DESC`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MCPToken
	for rows.Next() {
		t, err := scanMCPToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// MCPTokenByHash returns the token with this hash and its account, when the token is live and
// the account enabled; ErrNotFound otherwise.
func (s *Store) MCPTokenByHash(ctx context.Context, tokenHash string) (MCPToken, User, error) {
	t, err := scanMCPToken(s.db.R.QueryRowContext(ctx, `SELECT `+mcpTokenCols+` FROM mcp_tokens WHERE token_hash = ?`, tokenHash))
	if err != nil {
		return MCPToken{}, User{}, err
	}
	if !t.Live(s.now()) {
		return MCPToken{}, User{}, ErrNotFound
	}
	var u User
	var created int64
	err = s.db.R.QueryRowContext(ctx, `SELECT id, email, display_name, timezone, units, is_admin, created_at
		FROM users WHERE id = ? AND disabled = 0`, t.UserID).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Timezone, &u.Units, &u.IsAdmin, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return MCPToken{}, User{}, ErrNotFound
	}
	if err != nil {
		return MCPToken{}, User{}, err
	}
	u.CreatedAt = time.Unix(created, 0)
	return t, u, nil
}

// RenameMCPToken renames one of user's tokens.
func (s *Store) RenameMCPToken(ctx context.Context, user, id int64, name string) error {
	res, err := s.db.W.ExecContext(ctx, `UPDATE mcp_tokens SET name = ? WHERE id = ? AND user_id = ?`,
		truncate(strings.TrimSpace(name), 60), id, user)
	return affected(res, err)
}

// RevokeMCPToken ends one of user's tokens at once.
func (s *Store) RevokeMCPToken(ctx context.Context, user, id int64) error {
	res, err := s.db.W.ExecContext(ctx, `UPDATE mcp_tokens SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		s.now().Unix(), id, user)
	return affected(res, err)
}

// MCPCall is one tool call (or resource read, or prompt) in the log. The arguments are never
// kept.
type MCPCall struct {
	ID        int64
	TokenID   int64
	TokenName string
	UserID    int64
	At        time.Time
	Tool      string
	OK        bool
	Ms        int64
	Error     string
}

// LogMCPCall records a call, notes the token's last use and drops calls past MCPRetention.
func (s *Store) LogMCPCall(ctx context.Context, c MCPCall) error {
	at := c.At
	if at.IsZero() {
		at = s.now()
	}
	tx, err := s.db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_calls (token_id, user_id, at, tool, ok, ms, error) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.TokenID, c.UserID, at.Unix(), truncate(c.Tool, 80), c.OK, c.Ms, truncate(c.Error, 300)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mcp_tokens SET last_used_at = ? WHERE id = ?`, at.Unix(), c.TokenID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_calls WHERE at < ?`, at.Add(-MCPRetention).Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// MCPCalls returns user's most recent calls, newest first.
func (s *Store) MCPCalls(ctx context.Context, user int64, limit int) ([]MCPCall, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT c.id, c.token_id, t.name, c.user_id, c.at, c.tool, c.ok, c.ms, c.error
		FROM mcp_calls c JOIN mcp_tokens t ON t.id = c.token_id
		WHERE c.user_id = ? ORDER BY c.at DESC, c.id DESC LIMIT ?`, user, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MCPCall
	for rows.Next() {
		var c MCPCall
		var at int64
		if err := rows.Scan(&c.ID, &c.TokenID, &c.TokenName, &c.UserID, &at, &c.Tool, &c.OK, &c.Ms, &c.Error); err != nil {
			return nil, err
		}
		c.At = time.Unix(at, 0)
		out = append(out, c)
	}
	return out, rows.Err()
}

// MCPCallCounts counts user's calls since a time, per token.
func (s *Store) MCPCallCounts(ctx context.Context, user int64, since time.Time) (map[int64]int, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT token_id, COUNT(*) FROM mcp_calls WHERE user_id = ? AND at >= ? GROUP BY token_id`,
		user, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
