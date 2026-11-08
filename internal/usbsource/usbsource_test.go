// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package usbsource

import (
	"errors"
	"fmt"
	"testing"

	"github.com/karamble/goarmin/pkg/mtp"
)

func TestLock(t *testing.T) {
	dir := t.TempDir()
	unlock, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(dir); !errors.Is(err, ErrBusy) {
		t.Fatalf("second lock: %v", err)
	}
	unlock()
	again, err := Lock(dir)
	if err != nil {
		t.Fatalf("after unlock: %v", err)
	}
	again()
}

func TestHint(t *testing.T) {
	for _, err := range []error{mtp.ErrBusy, mtp.ErrPermission, mtp.ErrGone, mtp.ErrTimeout, mtp.ErrUnsupported} {
		if Hint(fmt.Errorf("opening: %w", err)) == "" {
			t.Errorf("no hint for %v", err)
		}
	}
	if Hint(errors.New("other")) != "" {
		t.Error("a hint for an unknown error")
	}
	if !IsGarmin(mtp.Info{Vendor: 0x091e}) || IsGarmin(mtp.Info{Vendor: 0x18d1}) {
		t.Error("IsGarmin")
	}
}
