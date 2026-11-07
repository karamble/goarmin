// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bluez

import (
	"context"
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

// Adapter is a snapshot of an org.bluez.Adapter1 object.
type Adapter struct {
	Name     string // e.g. "hci0"
	Address  string
	Alias    string
	Powered  bool
	Pairable bool
	// PowerState is BlueZ's account of the power: "on", "off", "off-enabling", "on-disabling",
	// or "off-blocked" when rfkill blocks the radio (only the system can lift that). Older BlueZ
	// versions leave it empty.
	PowerState  string
	Discovering bool
}

// Adapter returns the state of the named adapter.
func (c *Client) Adapter(ctx context.Context, name string) (Adapter, error) {
	objs, err := c.managedObjects(ctx)
	if err != nil {
		return Adapter{}, err
	}
	props, ok := objs[AdapterPath(name)][ifaceAdapter]
	if !ok {
		return Adapter{}, fmt.Errorf("bluez: adapter %s not found", name)
	}
	a := Adapter{Name: name}
	if v, ok := props["Address"].Value().(string); ok {
		a.Address = v
	}
	if v, ok := props["Alias"].Value().(string); ok {
		a.Alias = v
	}
	a.Powered, _ = props["Powered"].Value().(bool)
	a.Pairable, _ = props["Pairable"].Value().(bool)
	if v, ok := props["PowerState"]; ok {
		a.PowerState, _ = v.Value().(string)
	}
	a.Discovering, _ = props["Discovering"].Value().(bool)
	return a, nil
}

// SetPowered switches the adapter on or off (no root needed). An rfkill block wins: the call
// then fails with org.bluez.Error.Blocked.
func (c *Client) SetPowered(ctx context.Context, name string, on bool) error {
	return c.setProperty(ctx, AdapterPath(name), ifaceAdapter, "Powered", on)
}

// SetPairable lets the adapter accept new pairings (no root needed).
func (c *Client) SetPairable(ctx context.Context, name string, on bool) error {
	return c.setProperty(ctx, AdapterPath(name), ifaceAdapter, "Pairable", on)
}

// Discover runs LE-only discovery on the adapter and calls fn with the matching devices (keep;
// nil keeps all) about every 500 ms, until fn returns false or ctx ends; ctx ending is not an
// error. Discovery stops when it returns. Restricting discovery to LE makes BlueZ use the LE
// bearer for dual-mode devices such as Garmin watches. While discovery runs, BlueZ keeps
// devices it has seen; about 30 s after it stops it forgets the unpaired ones.
func (c *Client) Discover(ctx context.Context, adapter string, keep func(Device) bool, fn func([]Device) bool) error {
	ap := AdapterPath(adapter)
	filter := map[string]dbus.Variant{"Transport": dbus.MakeVariant("le"), "DuplicateData": dbus.MakeVariant(false)}
	if err := c.call(ctx, ap, ifaceAdapter+".SetDiscoveryFilter", filter).Err; err != nil {
		return fmt.Errorf("bluez: SetDiscoveryFilter: %w", err)
	}
	if err := c.call(ctx, ap, ifaceAdapter+".StartDiscovery").Err; err != nil {
		return fmt.Errorf("bluez: StartDiscovery: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = c.call(stopCtx, ap, ifaceAdapter+".StopDiscovery").Err
	}()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	return pollDevices(ctx, func(ctx context.Context) ([]Device, error) { return c.Devices(ctx, adapter) }, keep, fn, tick.C)
}

// pollDevices is Discover's loop without D-Bus: list, filter, report, wait for the next tick.
func pollDevices(ctx context.Context, list func(context.Context) ([]Device, error), keep func(Device) bool,
	fn func([]Device) bool, tick <-chan time.Time) error {
	for {
		devs, err := list(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		var matched []Device
		for _, dv := range devs {
			if keep == nil || keep(dv) {
				matched = append(matched, dv)
			}
		}
		if !fn(matched) {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick:
		}
	}
}

// ScanLE runs LE-only discovery on the adapter for at most d and returns the devices known
// afterwards that match keep (nil keeps all). It stops early when stopEarly (nil means never)
// returns true for the current matches.
func (c *Client) ScanLE(ctx context.Context, adapter string, d time.Duration, keep func(Device) bool, stopEarly func([]Device) bool) ([]Device, error) {
	deadline := time.Now().Add(d)
	var last []Device
	err := c.Discover(ctx, adapter, keep, func(m []Device) bool {
		last = m
		return !(stopEarly != nil && stopEarly(m)) && !time.Now().After(deadline)
	})
	if err != nil {
		return nil, err
	}
	return last, ctx.Err()
}
