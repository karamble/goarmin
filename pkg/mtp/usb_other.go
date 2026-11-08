// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

//go:build !(linux && (amd64 || arm64 || arm || 386 || riscv64 || loong64))

package mtp

import (
	"context"
	"time"
)

// Devices finds nothing here: this build has no USB access.
func Devices() ([]Info, error) { return nil, nil }

// Open fails with ErrUnsupported here.
func Open(ctx context.Context, info Info, timeout time.Duration) (*Session, error) {
	return nil, ErrUnsupported
}
