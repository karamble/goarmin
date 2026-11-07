// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package auth has the building blocks of a small multi-user login: argon2id password hashes,
// random tokens that are stored only as their SHA-256, and a limiter for failed attempts.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters, OWASP's recommended minimum (19 MiB, 2 passes).
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
	saltLen      = 16
)

// MinPasswordLen is enforced on every new password.
const MinPasswordLen = 12

// MaxPasswordLen bounds the work an attacker can make the server do per attempt.
const MaxPasswordLen = 1024

// ErrWeakPassword rejects passwords shorter than MinPasswordLen.
var ErrWeakPassword = fmt.Errorf("auth: a password needs at least %d characters", MinPasswordLen)

// ErrLongPassword rejects absurdly long passwords.
var ErrLongPassword = fmt.Errorf("auth: a password may have at most %d characters", MaxPasswordLen)

// HashPassword returns an encoded argon2id hash.
func HashPassword(pw string) (string, error) {
	switch n := len([]rune(pw)); {
	case n < MinPasswordLen:
		return "", ErrWeakPassword
	case len(pw) > MaxPasswordLen:
		return "", ErrLongPassword
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemory, argonTime, argonThreads,
		enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// CheckPassword compares a password with an encoded hash in constant time.
func CheckPassword(pw, encoded string) (bool, error) {
	if len(pw) > MaxPasswordLen {
		return false, nil
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("auth: unknown hash format")
	}
	var mem, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &t, &p); err != nil {
		return false, err
	}
	if mem > 1<<21 || t > 16 || p == 0 { // refuse hashes that would cost minutes
		return false, errors.New("auth: hash parameters out of range")
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(pw), salt, t, mem, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// dummyHash is checked when an account is unknown, so a login answers in the same time
// whether or not the account exists.
var dummyHash, _ = HashPassword("timing-equaliser-not-a-password")

// CheckUnknown burns the time of a real check.
func CheckUnknown(pw string) { _, _ = CheckPassword(pw, dummyHash) }
