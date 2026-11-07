// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := CheckPassword("correct horse battery", h); !ok || err != nil {
		t.Fatalf("right password: %v %v", ok, err)
	}
	if ok, _ := CheckPassword("correct horse batterY", h); ok {
		t.Fatal("wrong password accepted")
	}
	if _, err := HashPassword("short"); err != ErrWeakPassword {
		t.Fatalf("short password: %v", err)
	}
	if _, err := HashPassword(strings.Repeat("x", MaxPasswordLen+1)); err != ErrLongPassword {
		t.Fatalf("long password: %v", err)
	}
	if _, err := CheckPassword("x", "$argon2id$v=19$m=99999999,t=1,p=1$AAAA$AAAA"); err == nil {
		t.Fatal("expensive hash parameters accepted")
	}
}

func TestToken(t *testing.T) {
	a, ha := NewToken()
	b, _ := NewToken()
	if a == b || len(a) != 43 || HashToken(a) != ha || len(ha) != 64 {
		t.Fatalf("tokens %q %q hash %q", a, b, ha)
	}
}

func TestLimiter(t *testing.T) {
	now := time.Now()
	l := NewLimiter(3, time.Minute)
	l.Now = func() time.Time { return now }
	for range 3 {
		if l.Blocked("k") {
			t.Fatal("blocked too early")
		}
		l.Fail("k")
	}
	if !l.Blocked("k") || l.Blocked("other") {
		t.Fatal("not blocked after 3 failures")
	}
	now = now.Add(2 * time.Minute)
	if l.Blocked("k") {
		t.Fatal("still blocked after the window")
	}
	l.Fail("k")
	l.Reset("k")
	if l.Blocked("k") {
		t.Fatal("blocked after reset")
	}
}
