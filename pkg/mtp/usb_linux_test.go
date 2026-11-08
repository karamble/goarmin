// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

//go:build linux && (amd64 || arm64 || riscv64 || loong64)

package mtp

import "testing"

// The ioctl numbers as linux/usbdevice_fs.h defines them on 64-bit architectures.
func TestIoctlNumbers(t *testing.T) {
	for _, tc := range []struct {
		name      string
		got, want uintptr
	}{
		{"USBDEVFS_BULK", usbdevfsBulk, 0xC0185502},
		{"USBDEVFS_CLAIMINTERFACE", usbdevfsClaimInterface, 0x8004550F},
		{"USBDEVFS_RELEASEINTERFACE", usbdevfsReleaseInterface, 0x80045510},
		{"USBDEVFS_CLEAR_HALT", usbdevfsClearHalt, 0x80045515},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = 0x%X, want 0x%X", tc.name, tc.got, tc.want)
		}
	}
}
