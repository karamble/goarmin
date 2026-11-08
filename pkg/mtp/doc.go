// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package mtp reads files from MTP devices over USB on Linux, using only the standard library.
// MTP (Media Transfer Protocol) builds on PTP; Garmin watches use it, and so do phones.
//
// Devices lists what sysfs reports, without opening anything. Open claims the device's MTP
// interface through the kernel's usbfs (/dev/bus/usb/BBB/DDD) and opens a session. The person
// at the desktop normally has access already: libmtp's udev rules mark MTP devices as media
// players, and systemd's uaccess rule lets the session user open them. If another program has
// the device open (a file manager through GVfs, for example), the claim fails with ErrBusy.
//
// A Session lists storages and objects, walks folders (Walk) and downloads objects (Get). The
// package is read-only by construction: it has no operation that writes, moves or deletes
// anything on a device.
//
// The protocol runs over a Transport (one call per USB bulk transfer), so it can be tested
// against an in-memory device; see the tests.
package mtp
