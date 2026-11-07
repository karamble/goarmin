// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package bluez is a small client for the BlueZ D-Bus API on Linux: LE discovery, pairing with
// an org.bluez.Agent1, connecting, and a GATT characteristic pair as a packet link
// (AcquireNotify/AcquireWrite, falling back to StartNotify/WriteValue).
//
// It talks to bluetoothd on the system bus with github.com/godbus/dbus/v5 and needs no root:
// BlueZ's default D-Bus policy lets any user call org.bluez. Bluetooth addresses are handled
// in capitals, AA:BB:CC:DD:EE:FF.
package bluez

import (
	"context"
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	service         = "org.bluez"
	ifaceAdapter    = "org.bluez.Adapter1"
	ifaceDevice     = "org.bluez.Device1"
	ifaceGattSvc    = "org.bluez.GattService1"
	ifaceGattChar   = "org.bluez.GattCharacteristic1"
	ifaceAgentMgr   = "org.bluez.AgentManager1"
	ifaceAgent      = "org.bluez.Agent1"
	ifaceProps      = "org.freedesktop.DBus.Properties"
	ifaceObjManager = "org.freedesktop.DBus.ObjectManager"
	rootPath        = "/org/bluez"
)

// Client is a private connection to the system bus.
type Client struct {
	conn *dbus.Conn
}

// Open connects to the system bus.
func Open() (*Client, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("bluez: connect to the system bus: %w", err)
	}
	return &Client{conn: conn}, nil
}

// Close closes the bus connection; acquired file descriptors stay valid until closed.
func (c *Client) Close() error { return c.conn.Close() }

// managedObjects returns bluetoothd's object tree.
func (c *Client) managedObjects(ctx context.Context) (map[dbus.ObjectPath]map[string]map[string]dbus.Variant, error) {
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	err := c.conn.Object(service, "/").CallWithContext(ctx, ifaceObjManager+".GetManagedObjects", 0).Store(&objs)
	if err != nil {
		return nil, fmt.Errorf("bluez: GetManagedObjects: %w", err)
	}
	return objs, nil
}

func (c *Client) call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) *dbus.Call {
	return c.conn.Object(service, path).CallWithContext(ctx, method, 0, args...)
}

func (c *Client) setProperty(ctx context.Context, path dbus.ObjectPath, iface, name string, v any) error {
	return c.call(ctx, path, ifaceProps+".Set", iface, name, dbus.MakeVariant(v)).Err
}

func (c *Client) getProperty(ctx context.Context, path dbus.ObjectPath, iface, name string) (dbus.Variant, error) {
	var v dbus.Variant
	err := c.call(ctx, path, ifaceProps+".Get", iface, name).Store(&v)
	return v, err
}

// AdapterPath returns the object path of an adapter such as "hci0".
func AdapterPath(name string) dbus.ObjectPath { return dbus.ObjectPath(rootPath + "/" + name) }

// DevicePath returns the object path BlueZ uses for addr on adapter.
func DevicePath(adapter, addr string) dbus.ObjectPath {
	return dbus.ObjectPath(rootPath + "/" + adapter + "/dev_" + strings.ReplaceAll(strings.ToUpper(addr), ":", "_"))
}

// NormalizeAddress returns addr in capitals with colons, or an error if it is not a 48-bit
// Bluetooth address.
func NormalizeAddress(addr string) (string, error) {
	a := strings.ToUpper(strings.ReplaceAll(addr, "_", ":"))
	if len(a) != 17 {
		return "", fmt.Errorf("bluez: %q is not a Bluetooth address", addr)
	}
	for i, r := range a {
		if i%3 == 2 {
			if r != ':' {
				return "", fmt.Errorf("bluez: %q is not a Bluetooth address", addr)
			}
			continue
		}
		if !strings.ContainsRune("0123456789ABCDEF", r) {
			return "", fmt.Errorf("bluez: %q is not a Bluetooth address", addr)
		}
	}
	return a, nil
}
