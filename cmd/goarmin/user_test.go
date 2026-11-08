// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/auth"
)

func TestUserSetPassword(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "goarmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	u, _ := st.EnsureUser(ctx, "a@example.invalid", "Ada")
	for i := 0; i < 2; i++ {
		_, hash := auth.NewToken()
		if err := st.CreateSession(ctx, u.ID, hash, time.Hour, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	runWith := func(input string, args ...string) (int, string, string) {
		passwordIn = strings.NewReader(input)
		var out, errOut bytes.Buffer
		code := run(append([]string{"user", "-data", dir}, args...), &out, &errOut)
		return code, out.String(), errOut.String()
	}
	t.Cleanup(func() { passwordIn = nil })

	if code, _, e := runWith("short\n", "-email", "a@example.invalid", "-set-password"); code != 1 || !strings.Contains(e, "at least") {
		t.Fatalf("short password: %d %q", code, e)
	}
	if code, _, e := runWith("a long enough password\n", "-email", "nobody@example.invalid", "-set-password"); code != 1 || !strings.Contains(e, "no account") {
		t.Fatalf("unknown account: %d %q", code, e)
	}
	if code, _, _ := runWith("", "-set-password"); code != 2 {
		t.Fatalf("no email: %d", code)
	}
	code, out, e := runWith("a long enough password  \n", "-email", "a@example.invalid", "-set-password")
	if code != 0 || !strings.Contains(out, "signed out of 2 sessions") {
		t.Fatalf("%d %q %q", code, out, e)
	}
	code, out, _ = runWith("", "")
	if code != 2 {
		t.Fatalf("stray argument: %d", code)
	}
	if code, out, _ = runWith(""); code != 0 || !strings.Contains(out, "a@example.invalid") || !strings.Contains(out, "Ada") {
		t.Fatalf("list: %d %q", code, out)
	}

	st, _ = store.Open(ctx, filepath.Join(dir, "goarmin.db"))
	defer st.Close()
	l, err := st.LoginByEmail(ctx, "a@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	// Spaces are part of the password; only the line end is not.
	if ok, _ := auth.CheckPassword("a long enough password  ", l.PasswordHash); !ok {
		t.Fatal("the new password does not check")
	}
	if left, _ := st.Sessions(ctx, u.ID); len(left) != 0 {
		t.Fatalf("%d sessions left", len(left))
	}
}
