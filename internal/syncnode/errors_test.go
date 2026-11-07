// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package syncnode

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/karamble/goarmin/pkg/garmin/gattlink"
)

func TestErrorCode(t *testing.T) {
	for err, want := range map[error]string{
		nil: "",
		fmt.Errorf("x: %w", gattlink.ErrNotInRange):      CodeNotInRange,
		fmt.Errorf("x: %w", gattlink.ErrNotPaired):       CodeNotBonded,
		gattlink.ErrAdapterOff:                           CodeAdapterOff,
		fmt.Errorf("pull: %w", context.DeadlineExceeded): CodeTimeout,
		errors.New("pipe broke"):                         CodeTransfer,
	} {
		if got := ErrorCode(err); got != want {
			t.Errorf("ErrorCode(%v) = %q, want %q", err, got, want)
		}
	}
}

func TestLockBluetooth(t *testing.T) {
	dir := t.TempDir()
	unlock, err := LockBluetooth(dir)
	if err != nil {
		t.Fatal(err)
	}
	// flock locks belong to the open file: a second open in this process is refused like another process.
	if _, err := LockBluetooth(dir); !errors.Is(err, ErrBluetoothBusy) {
		t.Fatalf("second lock: %v", err)
	}
	unlock()
	again, err := LockBluetooth(dir)
	if err != nil {
		t.Fatalf("after unlock: %v", err)
	}
	again()
}
