// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bluez

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

// Device is a snapshot of an org.bluez.Device1 object.
type Device struct {
	Path             dbus.ObjectPath
	Adapter          dbus.ObjectPath
	Address          string // capitals
	AddressType      string // "public" or "random"
	Name             string
	Alias            string
	RSSI             int16
	HasRSSI          bool
	Paired           bool
	Bonded           bool
	Trusted          bool
	Connected        bool
	ServicesResolved bool
	UUIDs            []string
	// ManufacturerData maps Bluetooth SIG company IDs to their advertising payload.
	ManufacturerData map[uint16][]byte
}

// deviceFromProps builds a Device from its D-Bus properties.
func deviceFromProps(path dbus.ObjectPath, p map[string]dbus.Variant) Device {
	d := Device{Path: path, ManufacturerData: map[uint16][]byte{}}
	str := func(k string) string {
		if v, ok := p[k]; ok {
			if s, ok := v.Value().(string); ok {
				return s
			}
		}
		return ""
	}
	boolean := func(k string) bool {
		if v, ok := p[k]; ok {
			if b, ok := v.Value().(bool); ok {
				return b
			}
		}
		return false
	}
	d.Address = strings.ToUpper(str("Address"))
	d.AddressType = str("AddressType")
	d.Name = str("Name")
	d.Alias = str("Alias")
	d.Paired, d.Bonded, d.Trusted = boolean("Paired"), boolean("Bonded"), boolean("Trusted")
	d.Connected, d.ServicesResolved = boolean("Connected"), boolean("ServicesResolved")
	if v, ok := p["Adapter"]; ok {
		d.Adapter, _ = v.Value().(dbus.ObjectPath)
	}
	if v, ok := p["RSSI"]; ok {
		if r, ok := v.Value().(int16); ok {
			d.RSSI, d.HasRSSI = r, true
		}
	}
	if v, ok := p["UUIDs"]; ok {
		d.UUIDs, _ = v.Value().([]string)
	}
	if v, ok := p["ManufacturerData"]; ok {
		if m, ok := v.Value().(map[uint16]dbus.Variant); ok {
			for id, data := range m {
				if b, ok := data.Value().([]byte); ok {
					d.ManufacturerData[id] = b
				}
			}
		}
	}
	return d
}

// DisplayName returns the device name, falling back to the alias and then the address.
func (d Device) DisplayName() string {
	switch {
	case d.Name != "":
		return d.Name
	case d.Alias != "":
		return d.Alias
	}
	return d.Address
}

// Devices lists the devices bluetoothd knows on adapter (e.g. "hci0"), sorted by address.
func (c *Client) Devices(ctx context.Context, adapter string) ([]Device, error) {
	objs, err := c.managedObjects(ctx)
	if err != nil {
		return nil, err
	}
	ap := AdapterPath(adapter)
	var out []Device
	for path, ifaces := range objs {
		props, ok := ifaces[ifaceDevice]
		if !ok {
			continue
		}
		d := deviceFromProps(path, props)
		if d.Adapter != ap {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out, nil
}

// Device returns the current state of the device with this address.
func (c *Client) Device(ctx context.Context, adapter, addr string) (Device, error) {
	a, err := NormalizeAddress(addr)
	if err != nil {
		return Device{}, err
	}
	devs, err := c.Devices(ctx, adapter)
	if err != nil {
		return Device{}, err
	}
	for _, d := range devs {
		if d.Address == a {
			return d, nil
		}
	}
	return Device{}, fmt.Errorf("%w: %s on %s (scan for it first)", ErrUnknownDevice, a, adapter)
}

// Pair bonds with the device. It blocks until pairing completes, fails or ctx ends; a
// registered agent answers the passkey or confirmation request in the meantime.
func (c *Client) Pair(ctx context.Context, d Device) error {
	if err := c.call(ctx, d.Path, ifaceDevice+".Pair").Err; err != nil {
		return fmt.Errorf("bluez: pair %s: %w", d.Address, err)
	}
	return nil
}

// PairBonded pairs like Pair and then requires a bond: a pairing BlueZ keeps no keys for
// (Paired but not Bonded) would have to be repeated on the next connection. It returns the
// device as BlueZ reports it afterwards.
func (c *Client) PairBonded(ctx context.Context, adapter string, d Device) (Device, error) {
	if err := c.Pair(ctx, d); err != nil {
		return d, err
	}
	now, err := c.Device(ctx, adapter, d.Address)
	if err != nil {
		return d, err
	}
	if !now.Bonded {
		return now, fmt.Errorf("%w: %s", ErrNotBonded, d.Address)
	}
	return now, nil
}

// CancelPairing aborts a Pair in progress.
func (c *Client) CancelPairing(ctx context.Context, d Device) error {
	return c.call(ctx, d.Path, ifaceDevice+".CancelPairing").Err
}

// SetTrusted marks the device as trusted so BlueZ accepts its reconnections.
func (c *Client) SetTrusted(ctx context.Context, d Device, trusted bool) error {
	return c.setProperty(ctx, d.Path, ifaceDevice, "Trusted", trusted)
}

// Connect connects to the device and waits until its GATT services are resolved.
func (c *Client) Connect(ctx context.Context, d Device) error {
	if err := c.call(ctx, d.Path, ifaceDevice+".Connect").Err; err != nil {
		return fmt.Errorf("bluez: connect %s: %w", d.Address, err)
	}
	return c.waitBool(ctx, d.Path, ifaceDevice, "ServicesResolved", true)
}

// Disconnect closes the connection to the device.
func (c *Client) Disconnect(ctx context.Context, d Device) error {
	return c.call(ctx, d.Path, ifaceDevice+".Disconnect").Err
}

// RemoveDevice deletes the device and its bond from the adapter.
func (c *Client) RemoveDevice(ctx context.Context, adapter string, d Device) error {
	return c.call(ctx, AdapterPath(adapter), ifaceAdapter+".RemoveDevice", d.Path).Err
}

// waitBool polls a boolean property until it has the wanted value.
func (c *Client) waitBool(ctx context.Context, path dbus.ObjectPath, iface, name string, want bool) error {
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		v, err := c.getProperty(ctx, path, iface, name)
		if err == nil {
			if b, ok := v.Value().(bool); ok && b == want {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("bluez: waiting for %s=%v on %s: %w", name, want, path, ctx.Err())
		case <-t.C:
		}
	}
}

// ErrNotFound reports that a scan ended without finding a matching device.
var ErrNotFound = errors.New("bluez: no matching device found")

// ErrUnknownDevice reports that bluetoothd has no device with that address (not seen by a
// scan, or forgotten since).
var ErrUnknownDevice = errors.New("bluez: device not known")

// ErrNotBonded reports a pairing that left no bond.
var ErrNotBonded = errors.New("bluez: paired, but no bond was stored")
