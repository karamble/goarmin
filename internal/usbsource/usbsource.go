// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package usbsource finds Garmin watches plugged in by USB and reads their files, without
// changing anything on them.
package usbsource

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/karamble/goarmin/pkg/mtp"
)

// GarminVendor is Garmin's USB vendor id.
const GarminVendor = 0x091e

// IsGarmin reports whether a USB device is Garmin's.
func IsGarmin(i mtp.Info) bool { return i.Vendor == GarminVendor }

// ErrBusy: another goarmin process is reading a watch over USB.
var ErrBusy = errors.New("usbsource: another goarmin process is reading a watch over USB")

// Lock takes <data>/usb.lock, which the server's USB imports and `goarmin usb` share, so one
// reads a watch at a time. USB does not touch Bluetooth: it is not ble.lock.
func Lock(dataDir string) (unlock func(), err error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "usb.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, fmt.Errorf("usbsource: locking USB: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// Hint says in words what to do about an error opening a watch.
func Hint(err error) string {
	switch {
	case errors.Is(err, mtp.ErrBusy):
		return "another program has the watch open, usually the file manager: close its window for the watch or eject the watch there, then try again"
	case errors.Is(err, mtp.ErrPermission):
		return "this user may not open the watch's USB device; libmtp's udev rules normally give the person at the desktop access (is libmtp installed?)"
	case errors.Is(err, mtp.ErrGone):
		return "the watch was unplugged"
	case errors.Is(err, mtp.ErrTimeout):
		return "the watch stopped answering; unplug it, plug it in again and try once more"
	case errors.Is(err, mtp.ErrUnsupported):
		return "this build of goarmin cannot use USB on this system"
	}
	return ""
}
