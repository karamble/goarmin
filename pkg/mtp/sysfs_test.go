// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mtp

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// sysdev writes a USB device with its interfaces into a fake sysfs tree.
func sysdev(t *testing.T, root, name string, attrs map[string]string, intfs map[string]map[string]string) {
	t.Helper()
	write := func(dir string, kv map[string]string) {
		for k, v := range kv {
			p := filepath.Join(dir, k)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(v+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	d := filepath.Join(root, name)
	write(d, attrs)
	for in, kv := range intfs {
		write(filepath.Join(d, in), kv)
	}
}

func TestDevicesFromSysfs(t *testing.T) {
	root := t.TempDir()
	bulk := func(addr, dir string) map[string]string {
		return map[string]string{"bEndpointAddress": addr, "type": "Bulk", "direction": dir, "wMaxPacketSize": "0200"}
	}
	ep := func(eps map[string]map[string]string, kv map[string]string) map[string]string {
		for name, e := range eps {
			for k, v := range e {
				kv[name+"/"+k] = v
			}
		}
		return kv
	}
	// A watch whose MTP interface is vendor-specific and named "MTP".
	sysdev(t, root, "3-8", map[string]string{"idVendor": "091e", "idProduct": "4ff1", "busnum": "3", "devnum": "7",
		"manufacturer": "Garmin", "product": "fenix 8", "serial": "0000ABCDEF12"},
		map[string]map[string]string{"3-8:1.0": ep(map[string]map[string]string{
			"ep_01": bulk("01", "out"), "ep_81": bulk("81", "in"),
			"ep_82": {"bEndpointAddress": "82", "type": "Interrupt", "direction": "in", "wMaxPacketSize": "001c"},
		}, map[string]string{"bInterfaceClass": "ff", "bInterfaceSubClass": "ff", "bInterfaceProtocol": "00", "bInterfaceNumber": "00", "interface": "MTP"})})
	// A phone with the Still Image class.
	sysdev(t, root, "1-2", map[string]string{"idVendor": "18d1", "idProduct": "4ee1", "busnum": "1", "devnum": "12"},
		map[string]map[string]string{"1-2:1.1": ep(map[string]map[string]string{
			"ep_02": bulk("02", "out"), "ep_83": bulk("83", "in"),
		}, map[string]string{"bInterfaceClass": "06", "bInterfaceSubClass": "01", "bInterfaceProtocol": "01", "bInterfaceNumber": "01"})})
	// A USB drive and a hub: not MTP.
	sysdev(t, root, "2-1", map[string]string{"idVendor": "0781", "idProduct": "5581", "busnum": "2", "devnum": "3"},
		map[string]map[string]string{"2-1:1.0": ep(map[string]map[string]string{"ep_01": bulk("01", "out"), "ep_82": bulk("82", "in")},
			map[string]string{"bInterfaceClass": "08", "bInterfaceSubClass": "06", "bInterfaceProtocol": "50", "bInterfaceNumber": "00"})})
	sysdev(t, root, "usb1", map[string]string{"idVendor": "1d6b", "idProduct": "0002", "busnum": "1", "devnum": "1"}, nil)
	// A vendor interface without bulk endpoints is not usable.
	sysdev(t, root, "4-1", map[string]string{"idVendor": "091e", "idProduct": "0003", "busnum": "4", "devnum": "2"},
		map[string]map[string]string{"4-1:1.0": {"bInterfaceClass": "ff", "bInterfaceSubClass": "ff", "bInterfaceProtocol": "00", "bInterfaceNumber": "00", "interface": "MTP"}})

	got, err := devicesIn(root, "/dev/bus/usb")
	if err != nil {
		t.Fatal(err)
	}
	want := []Info{
		{Path: "1-2", Bus: 1, Address: 12, DevNode: "/dev/bus/usb/001/012", Vendor: 0x18d1, Product: 0x4ee1, Interface: 1, In: 0x83, Out: 0x02, MaxPacket: 512},
		{Path: "3-8", Bus: 3, Address: 7, DevNode: "/dev/bus/usb/003/007", Vendor: 0x091e, Product: 0x4ff1, Manufacturer: "Garmin", ProductName: "fenix 8",
			Serial: "0000ABCDEF12", Interface: 0, In: 0x81, Out: 0x01, Event: 0x82, MaxPacket: 512},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if got[1].ID() != "091e:4ff1" {
		t.Fatal(got[1].ID())
	}
	if none, err := devicesIn(filepath.Join(root, "missing"), "/dev/bus/usb"); err != nil || none != nil {
		t.Fatalf("%v, %v", none, err)
	}
}
