// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package gattlink connects to a bonded Garmin watch through BlueZ and opens its Multi-Link
// characteristic pairs as links for pkg/garmin/watch (docs/protocol/01 §1, 05 §6, 06).
package gattlink

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
)

// ServiceUUID is the Multi-Link GATT service.
const ServiceUUID = "6a4e2800-667b-11e3-949a-0800200c9a66"

// RXUUID and TXUUID name characteristic pair n: 6a4e2810+n carries notifications from the
// watch, 6a4e2820+n takes writes. The Fenix 8 has pairs 0 to 3.
func RXUUID(n int) string { return fmt.Sprintf("6a4e%04x-667b-11e3-949a-0800200c9a66", 0x2810+n) }

// TXUUID names the write characteristic of pair n.
func TXUUID(n int) string { return fmt.Sprintf("6a4e%04x-667b-11e3-949a-0800200c9a66", 0x2820+n) }

// Watch is a connected watch with its Multi-Link characteristics.
type Watch struct {
	Device bluez.Device
	// Pairs lists the usable characteristic pairs, ascending.
	Pairs []int

	c     *bluez.Client
	chars map[string]bluez.Characteristic
}

// Errors a caller can tell apart (errors.Is).
var (
	// ErrNotPaired: the watch is not paired with this adapter.
	ErrNotPaired = errors.New("gattlink: the watch is not paired with this computer")
	// ErrNotInRange: the watch did not show up in the scan and the connection failed, which is
	// what an out-of-range watch (or one with Bluetooth off) looks like.
	ErrNotInRange = errors.New("gattlink: the watch is not in range")
	// ErrAdapterOff: the Bluetooth adapter is powered off.
	ErrAdapterOff = errors.New("gattlink: Bluetooth is off on this computer")
)

// Connect connects to the bonded watch addr on adapter and discovers its Multi-Link
// characteristics. A watch that is not connected yet is first looked for with a short LE scan,
// which makes BlueZ use the LE bearer for this dual-mode device.
func Connect(ctx context.Context, c *bluez.Client, adapter, addr string) (*Watch, error) {
	addr, err := bluez.NormalizeAddress(addr)
	if err != nil {
		return nil, err
	}
	if ad, err := c.Adapter(ctx, adapter); err == nil && !ad.Powered {
		return nil, fmt.Errorf("%w (%s)", ErrAdapterOff, adapter)
	}
	d, err := c.Device(ctx, adapter, addr)
	if err != nil || !d.Paired {
		return nil, fmt.Errorf("%w: %s on %s; run goarmin pair first", ErrNotPaired, addr, adapter)
	}
	if !d.Connected {
		scanCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
		found, _ := c.ScanLE(scanCtx, adapter, 10*time.Second, func(x bluez.Device) bool { return x.Address == addr },
			func(found []bluez.Device) bool { return len(found) > 0 && found[0].HasRSSI })
		cancel()
		seen := len(found) > 0 && found[0].HasRSSI
		connCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := c.Connect(connCtx, d)
		cancel()
		if err != nil && !seen && ctx.Err() == nil {
			return nil, fmt.Errorf("%w: %s was not seen in a 10 s scan (%w)", ErrNotInRange, addr, err)
		}
		if err != nil {
			return nil, fmt.Errorf("gattlink: connecting to %s: %w", addr, err)
		}
		if d, err = c.Device(ctx, adapter, addr); err != nil {
			return nil, err
		}
	}
	chars, err := c.Characteristics(ctx, d)
	if err != nil {
		return nil, err
	}
	w := &Watch{Device: d, c: c, chars: map[string]bluez.Characteristic{}}
	for _, ch := range chars {
		if ch.ServiceUUID == ServiceUUID {
			w.chars[ch.UUID] = ch
		}
	}
	for n := range 8 {
		_, rx := w.chars[RXUUID(n)]
		_, tx := w.chars[TXUUID(n)]
		if rx && tx {
			w.Pairs = append(w.Pairs, n)
		}
	}
	if len(w.Pairs) == 0 {
		return w, errors.New("gattlink: the watch has no Multi-Link characteristic pair")
	}
	return w, nil
}

// Order returns the usable pairs with preferred first, if it is usable.
func (w *Watch) Order(preferred int) []int {
	if !slices.Contains(w.Pairs, preferred) {
		return slices.Clone(w.Pairs)
	}
	out := []int{preferred}
	for _, n := range w.Pairs {
		if n != preferred {
			out = append(out, n)
		}
	}
	return out
}

// Open opens characteristic pair n; it has the signature of watch.Opener.
func (w *Watch) Open(ctx context.Context, n int) (multilink.Link, io.Closer, error) {
	rx, okRX := w.chars[RXUUID(n)]
	tx, okTX := w.chars[TXUUID(n)]
	if !okRX || !okTX {
		return nil, nil, fmt.Errorf("gattlink: no characteristic pair %d", n)
	}
	l, err := w.c.OpenLink(ctx, rx, tx)
	if err != nil {
		return nil, nil, fmt.Errorf("gattlink: pair %d: %w", n, err)
	}
	return l, l, nil
}

// Disconnect drops the Bluetooth connection; the bond stays.
func (w *Watch) Disconnect(ctx context.Context) error { return w.c.Disconnect(ctx, w.Device) }
