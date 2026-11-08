// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mtp

import "errors"

var (
	// ErrBusy: another program holds the device's MTP interface (often a file manager through
	// GVfs). Closing it, or reading through its mount instead, works.
	ErrBusy = errors.New("mtp: another program has the device open")
	// ErrPermission: the usbfs node is not open to this user. MTP devices normally get the
	// desktop user's access from libmtp's and systemd's udev rules.
	ErrPermission = errors.New("mtp: no permission to open the device")
	// ErrGone: the device was unplugged.
	ErrGone = errors.New("mtp: the device is gone")
	// ErrTimeout: a transfer did not finish in time.
	ErrTimeout = errors.New("mtp: the device did not answer in time")
	// ErrUnsupported: this build has no USB access (another OS or architecture).
	ErrUnsupported = errors.New("mtp: USB access is not supported on this system")
)
