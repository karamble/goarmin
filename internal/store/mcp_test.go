// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMCPTokens(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	now := time.Now().Truncate(time.Second)
	st.SetClock(func() time.Time { return now })
	a, b := person(t, st, "a@example.invalid"), person(t, st, "b@example.invalid")

	never, err := st.CreateMCPToken(ctx, a.ID, " Claude ", "gma_abcd", "h1", MCPRead, time.Time{})
	if err != nil || never.Name != "Claude" || !never.ExpiresAt.IsZero() || !never.Live(now) {
		t.Fatalf("token %+v, %v", never, err)
	}
	soon, _ := st.CreateMCPToken(ctx, a.ID, "Laptop", "gma_efgh", "h2", MCPWrite, now.Add(time.Hour))
	if _, _, err := st.MCPTokenByHash(ctx, "h2"); err != nil {
		t.Fatalf("live token: %v", err)
	}
	if _, err := st.CreateMCPToken(ctx, b.ID, "dup", "gma_x", "h1", MCPRead, time.Time{}); err == nil {
		t.Fatal("two tokens share a hash")
	}

	// Expiry follows the clock.
	now = now.Add(2 * time.Hour)
	if _, _, err := st.MCPTokenByHash(ctx, "h2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired token: %v", err)
	}
	if tok, u, err := st.MCPTokenByHash(ctx, "h1"); err != nil || tok.ID != never.ID || u.ID != a.ID {
		t.Fatalf("never-expiring token: %+v %+v %v", tok, u, err)
	}

	// Only the owner renames or revokes.
	if err := st.RenameMCPToken(ctx, b.ID, never.ID, "mine"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("b renamed a's token: %v", err)
	}
	if err := st.RevokeMCPToken(ctx, b.ID, never.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("b revoked a's token: %v", err)
	}
	if err := st.RenameMCPToken(ctx, a.ID, never.ID, "Desktop"); err != nil {
		t.Fatal(err)
	}

	// The log: calls, counts, last use, retention.
	old := now.Add(-MCPRetention - time.Hour)
	for _, c := range []MCPCall{
		{TokenID: never.ID, UserID: a.ID, At: old, Tool: "get_daily", OK: true},
		{TokenID: never.ID, UserID: a.ID, At: now.Add(-2 * time.Minute), Tool: "get_account", OK: true, Ms: 3},
		{TokenID: soon.ID, UserID: a.ID, At: now.Add(-time.Minute), Tool: "sync_device", Error: "busy"},
	} {
		if err := st.LogMCPCall(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	calls, err := st.MCPCalls(ctx, a.ID, 10)
	if err != nil || len(calls) != 2 || calls[0].Tool != "sync_device" || calls[0].OK || calls[0].TokenName != "Laptop" ||
		calls[1].TokenName != "Desktop" {
		t.Fatalf("calls %+v, %v", calls, err)
	}
	if bc, _ := st.MCPCalls(ctx, b.ID, 10); len(bc) != 0 {
		t.Fatalf("b sees %d of a's calls", len(bc))
	}
	counts, _ := st.MCPCallCounts(ctx, a.ID, now.Add(-24*time.Hour))
	if counts[never.ID] != 1 || counts[soon.ID] != 1 {
		t.Fatalf("counts %v", counts)
	}
	list, _ := st.MCPTokens(ctx, a.ID)
	if len(list) != 2 || list[0].ID != soon.ID || list[1].LastUsed.Unix() != now.Add(-2*time.Minute).Unix() {
		t.Fatalf("tokens %+v", list)
	}

	// Revoked: gone at once, and listed last.
	if err := st.RevokeMCPToken(ctx, a.ID, never.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.MCPTokenByHash(ctx, "h1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked token: %v", err)
	}
	if err := st.RevokeMCPToken(ctx, a.ID, never.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked twice: %v", err)
	}
	list, _ = st.MCPTokens(ctx, a.ID)
	if list[1].ID != never.ID || list[1].Revoked.IsZero() {
		t.Fatalf("revoked token listed %+v", list)
	}

	// A disabled account's tokens stop working.
	third, _ := st.CreateMCPToken(ctx, b.ID, "B", "gma_b", "h3", MCPRead, time.Time{})
	if _, err := st.db.W.ExecContext(ctx, `UPDATE users SET disabled = 1 WHERE id = ?`, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.MCPTokenByHash(ctx, "h3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled account's token %d: %v", third.ID, err)
	}
}
