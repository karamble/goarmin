// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

//go:build linux && (amd64 || arm64 || arm || 386 || riscv64 || loong64)

package mtp

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// Devices lists the MTP devices on USB, as sysfs reports them. It opens nothing.
func Devices() ([]Info, error) { return devicesIn("/sys/bus/usb/devices", "/dev/bus/usb") }

// Open claims the device's MTP interface through usbfs and opens a session. Every transfer
// times out after timeout (0: 10 s).
func Open(ctx context.Context, info Info, timeout time.Duration) (*Session, error) {
	t, err := openUSB(info, timeout)
	if err != nil {
		return nil, err
	}
	return NewSession(ctx, t)
}

// The usbfs ioctls (linux/usbdevice_fs.h), numbered with the generic _IOC layout these
// architectures share.
const (
	iocWrite = 1
	iocRead  = 2
)

func ioc(dir, nr, size uintptr) uintptr { return dir<<30 | size<<16 | 'U'<<8 | nr }

// bulkTransfer is struct usbdevfs_bulktransfer; Go pads it as C does.
type bulkTransfer struct {
	ep      uint32
	len     uint32
	timeout uint32 // milliseconds
	data    uintptr
}

var (
	usbdevfsBulk             = ioc(iocRead|iocWrite, 2, unsafe.Sizeof(bulkTransfer{}))
	usbdevfsClaimInterface   = ioc(iocRead, 15, 4)
	usbdevfsReleaseInterface = ioc(iocRead, 16, 4)
	usbdevfsClearHalt        = ioc(iocRead, 21, 4)
)

type usbfs struct {
	fd      int
	intf    uint32
	in, out uint8
	ms      uint32
}

func openUSB(info Info, timeout time.Duration) (*usbfs, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	fd, err := syscall.Open(info.DevNode, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
			return nil, fmt.Errorf("%w: %s (%s)", ErrPermission, info.DevNode, info.ID())
		}
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ENODEV) {
			return nil, fmt.Errorf("%w: %s", ErrGone, info.DevNode)
		}
		return nil, fmt.Errorf("mtp: opening %s: %w", info.DevNode, err)
	}
	u := &usbfs{fd: fd, intf: uint32(info.Interface), in: info.In, out: info.Out, ms: uint32(timeout / time.Millisecond)}
	if err := u.ioctl(usbdevfsClaimInterface, unsafe.Pointer(&u.intf)); err != nil {
		syscall.Close(fd)
		if errors.Is(err, syscall.EBUSY) {
			return nil, fmt.Errorf("%w: %s (%s)", ErrBusy, info.DevNode, info.ID())
		}
		return nil, fmt.Errorf("mtp: claiming interface %d of %s: %w", info.Interface, info.DevNode, err)
	}
	return u, nil
}

func (u *usbfs) ioctl(req uintptr, arg unsafe.Pointer) error {
	for {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(u.fd), req, uintptr(arg))
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}

func (u *usbfs) bulk(ep uint8, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	bt := bulkTransfer{ep: uint32(ep), len: uint32(len(p)), timeout: u.ms, data: uintptr(unsafe.Pointer(&p[0]))}
	for {
		n, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(u.fd), usbdevfsBulk, uintptr(unsafe.Pointer(&bt)))
		runtime.KeepAlive(p)
		switch errno {
		case 0:
			return int(n), nil
		case syscall.EINTR:
			continue
		case syscall.ETIMEDOUT:
			return 0, ErrTimeout
		case syscall.ENODEV, syscall.ESHUTDOWN:
			return 0, ErrGone
		case syscall.EPIPE:
			// The endpoint stalled; clear it so a new session can start.
			e := uint32(ep)
			_ = u.ioctl(usbdevfsClearHalt, unsafe.Pointer(&e))
			return 0, fmt.Errorf("mtp: endpoint 0x%02X stalled", ep)
		default:
			return 0, fmt.Errorf("mtp: bulk transfer on 0x%02X: %w", ep, errno)
		}
	}
}

func (u *usbfs) Send(p []byte) error {
	for len(p) > 0 {
		n, err := u.bulk(u.out, p)
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("mtp: the device took nothing")
		}
		p = p[n:]
	}
	return nil
}

func (u *usbfs) Receive(p []byte) (int, error) { return u.bulk(u.in, p) }

func (u *usbfs) Close() error {
	_ = u.ioctl(usbdevfsReleaseInterface, unsafe.Pointer(&u.intf))
	return syscall.Close(u.fd)
}
