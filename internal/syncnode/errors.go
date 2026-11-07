// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package syncnode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/garmin/gattlink"
)

// Error codes of a failed sync, for the sync log and its hints.
const (
	CodeNotInRange = "not_in_range" // not seen and not reachable: away, or its Bluetooth is off
	CodeNotBonded  = "not_bonded"   // this computer has no pairing with it
	CodeAdapterOff = "adapter_off"  // this computer's Bluetooth is off
	CodeTimeout    = "timeout"
	CodeTransfer   = "transfer" // anything else between connecting and the end of the sync
)

// ErrorCode classifies a sync's error ("" for nil).
func ErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, gattlink.ErrNotInRange):
		return CodeNotInRange
	case errors.Is(err, gattlink.ErrNotPaired):
		return CodeNotBonded
	case errors.Is(err, gattlink.ErrAdapterOff):
		return CodeAdapterOff
	case errors.Is(err, context.DeadlineExceeded):
		return CodeTimeout
	default:
		return CodeTransfer
	}
}

// ErrBluetoothBusy: another goarmin process on this computer is using Bluetooth.
var ErrBluetoothBusy = errors.New("syncnode: another goarmin (the server, or a command) is using Bluetooth right now")

// LockBluetooth takes the data directory's Bluetooth lock (<dataDir>/ble.lock) without waiting:
// the server holds it during each sync, live view or pairing, and the commands (pair, sync,
// probe, live) take it too, so two processes never talk to the watch at once. The lock goes
// with the process, also when it crashes.
func LockBluetooth(dataDir string) (unlock func(), err error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "ble.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrBluetoothBusy
		}
		return nil, fmt.Errorf("syncnode: locking Bluetooth: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// Forget unpairs the watch at addr from this computer (BlueZ removes the bond) and drops it
// from node.json, so the server stops syncing it. Its spool stays: the record of what was
// fetched keeps a later pairing from fetching it all again.
func Forget(ctx context.Context, dataDir, adapter, addr string) error {
	c, err := bluez.Open()
	if err != nil {
		return err
	}
	defer c.Close()
	if d, err := c.Device(ctx, adapter, addr); err == nil {
		if err := c.RemoveDevice(ctx, adapter, d); err != nil {
			return fmt.Errorf("syncnode: removing the pairing: %w", err)
		}
	}
	return ForgetRecord(dataDir, addr)
}

// ForgetRecord drops the watch at addr from node.json only.
func ForgetRecord(dataDir, addr string) error {
	return updateNode(dataDir, func(n *nodeFile) bool {
		if _, ok := n.Watches[addr]; !ok {
			return false
		}
		delete(n.Watches, addr)
		return true
	})
}

// PurgeSpool removes the fetched files of the watch with this unit number from its spool,
// keeping the spool's record (state.json): a hand-over or a deleted history leaves nothing
// behind, and nothing is fetched twice.
func PurgeSpool(dataDir string, unit uint32) error {
	dir := filepath.Join(dataDir, "watches", strconv.FormatUint(uint64(unit), 10), "files")
	err := os.RemoveAll(dir)
	if err == nil || os.IsNotExist(err) {
		return os.MkdirAll(dir, 0o700)
	}
	return err
}
