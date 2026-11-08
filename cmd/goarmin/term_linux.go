// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// isTerminal reports whether f is an interactive terminal.
func isTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

// readSecret reads one line from the terminal f without showing it.
func readSecret(f *os.File, max int) (string, error) {
	var old syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&old))); errno != 0 {
		return "", errors.New("standard input is no terminal")
	}
	quiet := old
	quiet.Lflag &^= syscall.ECHO
	quiet.Lflag |= syscall.ICANON | syscall.ISIG
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&quiet))); errno != 0 {
		return "", errno
	}
	defer syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCSETS, uintptr(unsafe.Pointer(&old))) //nolint:errcheck
	return readLine(f, max)
}

// readLine reads one line (at most max bytes), without its line ending.
func readLine(r io.Reader, max int) (string, error) {
	line, err := bufio.NewReader(io.LimitReader(r, int64(max)+2)).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
