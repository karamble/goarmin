// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mtp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Info describes an MTP device found on USB.
type Info struct {
	// Path is the device's sysfs name, which follows the port it is plugged into ("3-8").
	Path    string
	Bus     int
	Address int
	// DevNode is the usbfs node Open uses (/dev/bus/usb/003/007).
	DevNode      string
	Vendor       uint16
	Product      uint16
	Manufacturer string // the USB descriptor strings, "" when the device has none
	ProductName  string
	Serial       string
	// Interface is the MTP interface's number, In, Out and Event its endpoint addresses.
	Interface int
	In        uint8
	Out       uint8
	Event     uint8
	MaxPacket int
}

// ID is the vendor and product id as lsusb prints them ("091e:4ff1").
func (i Info) ID() string { return fmt.Sprintf("%04x:%04x", i.Vendor, i.Product) }

// devicesIn reads MTP devices from a sysfs tree (sys: /sys/bus/usb/devices) and names their
// usbfs nodes under dev (/dev/bus/usb).
func devicesIn(sys, dev string) ([]Info, error) {
	ents, err := os.ReadDir(sys)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Info
	for _, e := range ents {
		name := e.Name()
		if strings.Contains(name, ":") {
			continue // an interface, read below with its device
		}
		d := filepath.Join(sys, name)
		vendor, ok1 := readHex(d, "idVendor")
		product, ok2 := readHex(d, "idProduct")
		bus, ok3 := readDec(d, "busnum")
		addr, ok4 := readDec(d, "devnum")
		if !ok1 || !ok2 || !ok3 || !ok4 {
			continue
		}
		info := Info{
			Path: name, Bus: bus, Address: addr,
			DevNode:      filepath.Join(dev, fmt.Sprintf("%03d", bus), fmt.Sprintf("%03d", addr)),
			Vendor:       uint16(vendor),
			Product:      uint16(product),
			Manufacturer: readStr(d, "manufacturer"),
			ProductName:  readStr(d, "product"),
			Serial:       readStr(d, "serial"),
		}
		if intf, ok := mtpInterface(d, name); ok {
			info.Interface, info.In, info.Out, info.Event, info.MaxPacket = intf.num, intf.in, intf.out, intf.event, intf.maxPacket
			out = append(out, info)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Path < out[b].Path })
	return out, nil
}

type iface struct {
	num            int
	in, out, event uint8
	maxPacket      int
}

// mtpInterface finds the device's MTP interface: the Still Image class (6/1/1) or a
// vendor-specific interface named "MTP", with a bulk endpoint each way.
func mtpInterface(dir, name string) (iface, bool) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return iface{}, false
	}
	for _, e := range ents {
		if !strings.HasPrefix(e.Name(), name+":") {
			continue
		}
		d := filepath.Join(dir, e.Name())
		cls, _ := readHex(d, "bInterfaceClass")
		sub, _ := readHex(d, "bInterfaceSubClass")
		proto, _ := readHex(d, "bInterfaceProtocol")
		num, _ := readHex(d, "bInterfaceNumber")
		label := strings.ToUpper(readStr(d, "interface"))
		still := cls == 6 && sub == 1 && proto == 1
		vendorMTP := cls == 0xFF && strings.Contains(label, "MTP")
		if !still && !vendorMTP {
			continue
		}
		f := iface{num: int(num)}
		eps, _ := os.ReadDir(d)
		for _, ep := range eps {
			if !strings.HasPrefix(ep.Name(), "ep_") {
				continue
			}
			p := filepath.Join(d, ep.Name())
			a, ok := readHex(p, "bEndpointAddress")
			if !ok {
				continue
			}
			kind, dirn := readStr(p, "type"), readStr(p, "direction")
			switch {
			case kind == "Bulk" && dirn == "in":
				f.in = uint8(a)
				if mp, ok := readHex(p, "wMaxPacketSize"); ok {
					f.maxPacket = int(mp & 0x7FF)
				}
			case kind == "Bulk" && dirn == "out":
				f.out = uint8(a)
			case kind == "Interrupt" && dirn == "in":
				f.event = uint8(a)
			}
		}
		if f.in != 0 && f.out != 0 {
			return f, true
		}
	}
	return iface{}, false
}

func readStr(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readHex(dir, name string) (uint64, bool) {
	v, err := strconv.ParseUint(readStr(dir, name), 16, 32)
	return v, err == nil
}

func readDec(dir, name string) (int, bool) {
	v, err := strconv.Atoi(readStr(dir, name))
	return v, err == nil
}
