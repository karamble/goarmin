// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bluez

import (
	"errors"
	"strings"

	"github.com/godbus/dbus/v5"
)

// Error names bluetoothd answers with (org.bluez.Error.<name>), as ErrorName returns them.
const (
	ErrNameAuthenticationFailed   = "AuthenticationFailed"
	ErrNameAuthenticationRejected = "AuthenticationRejected"
	ErrNameAuthenticationCanceled = "AuthenticationCanceled"
	ErrNameAuthenticationTimeout  = "AuthenticationTimeout"
	ErrNameConnectionAttempt      = "ConnectionAttemptFailed"
	ErrNameFailed                 = "Failed"
	ErrNameInProgress             = "InProgress"
	ErrNameAlreadyExists          = "AlreadyExists"
	ErrNameDoesNotExist           = "DoesNotExist"
	ErrNameNotReady               = "NotReady"
	ErrNameBlocked                = "Blocked"
)

// ErrorName returns the short name of the BlueZ error inside err ("AuthenticationFailed" for
// org.bluez.Error.AuthenticationFailed), "UnknownObject" for a device D-Bus no longer has, or
// "" when err carries no D-Bus error.
func ErrorName(err error) string {
	var de dbus.Error
	var dp *dbus.Error
	var name string
	switch {
	case errors.As(err, &dp) && dp != nil:
		name = dp.Name
	case errors.As(err, &de):
		name = de.Name
	default:
		return ""
	}
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}
