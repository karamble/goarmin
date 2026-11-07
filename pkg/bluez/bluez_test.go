// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package bluez

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestNormalizeAddress(t *testing.T) {
	for in, want := range map[string]string{
		"c0:ff:ee:12:34:56": "C0:FF:EE:12:34:56",
		"C0_FF_EE_12_34_56": "C0:FF:EE:12:34:56",
	} {
		if got, err := NormalizeAddress(in); err != nil || got != want {
			t.Errorf("NormalizeAddress(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "C0:FF:EE:12:34", "C0:FF:EE:12:34:5G", "C0-FF-EE-12-34-56"} {
		if _, err := NormalizeAddress(bad); err == nil {
			t.Errorf("NormalizeAddress(%q) accepted", bad)
		}
	}
}

func TestDevicePath(t *testing.T) {
	if got := DevicePath("hci0", "c0:ff:ee:12:34:56"); got != "/org/bluez/hci0/dev_C0_FF_EE_12_34_56" {
		t.Fatalf("DevicePath = %q", got)
	}
}

func TestDeviceFromProps(t *testing.T) {
	props := map[string]dbus.Variant{
		"Address":          dbus.MakeVariant("c0:ff:ee:12:34:56"),
		"AddressType":      dbus.MakeVariant("public"),
		"Name":             dbus.MakeVariant("fenix 8 - 47mm"),
		"Adapter":          dbus.MakeVariant(dbus.ObjectPath("/org/bluez/hci0")),
		"RSSI":             dbus.MakeVariant(int16(-61)),
		"Paired":           dbus.MakeVariant(true),
		"Bonded":           dbus.MakeVariant(true),
		"ServicesResolved": dbus.MakeVariant(false),
		"UUIDs":            dbus.MakeVariant([]string{"6a4e2800-667b-11e3-949a-0800200c9a66"}),
		"ManufacturerData": dbus.MakeVariant(map[uint16]dbus.Variant{0x0087: dbus.MakeVariant([]byte{0x11, 0xB8})}),
	}
	d := deviceFromProps("/org/bluez/hci0/dev_C0_FF_EE_12_34_56", props)
	if d.Address != "C0:FF:EE:12:34:56" || d.Name != "fenix 8 - 47mm" || d.Adapter != "/org/bluez/hci0" {
		t.Fatalf("identity fields: %+v", d)
	}
	if !d.Paired || !d.Bonded || d.ServicesResolved || !d.HasRSSI || d.RSSI != -61 {
		t.Fatalf("state fields: %+v", d)
	}
	if got := d.ManufacturerData[0x0087]; len(got) != 2 || got[1] != 0xB8 {
		t.Fatalf("manufacturer data: %v", d.ManufacturerData)
	}
	if d.DisplayName() != "fenix 8 - 47mm" {
		t.Fatalf("DisplayName = %q", d.DisplayName())
	}
	if (Device{Address: "AA:BB:CC:DD:EE:FF"}).DisplayName() != "AA:BB:CC:DD:EE:FF" {
		t.Fatal("DisplayName must fall back to the address")
	}
}

type stubPrompter struct {
	passkey   uint32
	confirmed bool
	shown     uint32
	cancelled bool
}

func (s *stubPrompter) RequestPasskey(Device) (uint32, error) { return s.passkey, nil }
func (s *stubPrompter) RequestConfirmation(_ Device, pk uint32) (bool, error) {
	s.shown = pk
	return s.confirmed, nil
}
func (s *stubPrompter) DisplayPasskey(_ Device, pk uint32, _ uint16) { s.shown = pk }
func (s *stubPrompter) Cancel()                                      { s.cancelled = true }

// The agent answers only for the device being paired and rejects everything else.
func TestAgentOnlyAnswersForTarget(t *testing.T) {
	target := Device{Path: "/org/bluez/hci0/dev_C0_FF_EE_12_34_56"}
	other := dbus.ObjectPath("/org/bluez/hci0/dev_11_22_33_44_55_66")
	p := &stubPrompter{passkey: 123456, confirmed: true}
	a := &Agent{prompter: p, target: target}

	if pk, err := a.RequestPasskey(target.Path); err != nil || pk != 123456 {
		t.Errorf("RequestPasskey(target) = %d, %v", pk, err)
	}
	if _, err := a.RequestPasskey(other); err == nil || err.Name != "org.bluez.Error.Rejected" {
		t.Errorf("RequestPasskey(other) err = %v, want Rejected", err)
	}
	if err := a.RequestConfirmation(target.Path, 654321); err != nil || p.shown != 654321 {
		t.Errorf("RequestConfirmation(target) = %v, shown %d", err, p.shown)
	}
	p.confirmed = false
	if err := a.RequestConfirmation(target.Path, 1); err == nil || err.Name != "org.bluez.Error.Rejected" {
		t.Errorf("declined confirmation err = %v, want Rejected", err)
	}
	if err := a.AuthorizeService(other, "x"); err == nil {
		t.Error("AuthorizeService(other) must be rejected")
	}
	if err := a.RequestAuthorization(target.Path); err != nil {
		t.Errorf("RequestAuthorization(target) = %v", err)
	}
	if _, err := a.RequestPinCode(target.Path); err == nil {
		t.Error("legacy PIN requests must be rejected")
	}
	_ = a.Cancel()
	if !p.cancelled {
		t.Error("Cancel not forwarded to the prompter")
	}
}

func TestSetMTU(t *testing.T) {
	l := &Link{maxWrite: 20}
	l.setMTU(23)
	if l.maxWrite != 20 {
		t.Errorf("MTU 23 → %d, want the 20-byte default", l.maxWrite)
	}
	l.setMTU(247)
	if l.maxWrite != 244 {
		t.Errorf("MTU 247 → %d, want 244", l.maxWrite)
	}
	l.setMTU(517)
	if l.maxWrite != 512 {
		t.Errorf("MTU 517 → %d, want the 512 cap", l.maxWrite)
	}
}

func TestErrorName(t *testing.T) {
	val := fmt.Errorf("bluez: pair X: %w", dbus.Error{Name: "org.bluez.Error.AuthenticationFailed"})
	var inner error = &dbus.Error{Name: "org.bluez.Error.InProgress"}
	ptr := fmt.Errorf("wrapped: %w", fmt.Errorf("bluez: %w", inner))
	obj := dbus.Error{Name: "org.freedesktop.DBus.Error.UnknownObject"}
	for _, c := range []struct {
		err  error
		want string
	}{{val, ErrNameAuthenticationFailed}, {ptr, ErrNameInProgress}, {obj, "UnknownObject"}, {errors.New("plain"), ""},
		{ErrNotBonded, ""}} {
		if got := ErrorName(c.err); got != c.want {
			t.Errorf("ErrorName(%v) = %q, want %q", c.err, got, c.want)
		}
	}
	if ErrorName(nil) != "" {
		t.Error("ErrorName(nil) must be empty")
	}
}

// pollDevices reports the kept devices on every tick and stops when told to or when ctx ends.
func TestPollDevices(t *testing.T) {
	round := 0
	list := func(context.Context) ([]Device, error) {
		round++
		devs := []Device{{Address: "C0:FF:EE:12:34:56", Name: "fenix 8"}}
		if round >= 2 {
			devs = append(devs, Device{Address: "11:22:33:44:55:66", Name: "phone"}, Device{Address: "C0:FF:EE:00:00:01", Name: "fenix 7"})
		}
		return devs, nil
	}
	keep := func(d Device) bool { return strings.HasPrefix(d.Address, "C0:FF:EE") }
	tick := make(chan time.Time, 10)
	for i := 0; i < 10; i++ {
		tick <- time.Now()
	}
	var seen [][]Device
	err := pollDevices(context.Background(), list, keep, func(m []Device) bool {
		seen = append(seen, m)
		return len(m) < 2
	}, tick)
	if err != nil || len(seen) != 2 || len(seen[0]) != 1 || len(seen[1]) != 2 {
		t.Fatalf("err %v, rounds %v", err, seen)
	}

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err = pollDevices(ctx, list, nil, func([]Device) bool {
		calls++
		cancel()
		return true
	}, make(chan time.Time))
	if err != nil || calls != 1 {
		t.Fatalf("a cancelled poll: err %v after %d calls", err, calls)
	}

	boom := errors.New("bus gone")
	if err := pollDevices(context.Background(), func(context.Context) ([]Device, error) { return nil, boom }, nil,
		func([]Device) bool { return true }, tick); !errors.Is(err, boom) {
		t.Fatalf("a failing list: %v", err)
	}
}
