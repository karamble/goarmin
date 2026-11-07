// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package gfdi

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
)

// DeviceInfo is the watch's DEVICE_INFORMATION (5024), the first GFDI message of a session.
type DeviceInfo struct {
	ProtocolVersion uint16
	ProductNumber   uint16
	UnitNumber      uint32
	SoftwareVersion uint16
	// MaxPacketSize is the largest whole GFDI frame the watch accepts.
	MaxPacketSize uint16
	BluetoothName string
	DeviceName    string
	DeviceModel   string
	// Tail holds the optional trailing bytes (dual-pairing flag and MACs on some models).
	Tail []byte
}

// SoftwareVersionString formats the firmware version as the watch shows it, e.g. "23.39".
func (d DeviceInfo) SoftwareVersionString() string {
	return fmt.Sprintf("%d.%02d", d.SoftwareVersion/100, d.SoftwareVersion%100)
}

var errShort = errors.New("gfdi: payload too short")

// lpstr reads a string with a one-byte length prefix.
func lpstr(b []byte, i int) (string, int, error) {
	if i >= len(b) {
		return "", i, errShort
	}
	n := int(b[i])
	if i+1+n > len(b) {
		return "", i, errShort
	}
	return string(b[i+1 : i+1+n]), i + 1 + n, nil
}

func appendLPStr(b []byte, s string) []byte {
	if len(s) > 255 {
		s = s[:255]
	}
	return append(append(b, byte(len(s))), s...)
}

// ParseDeviceInfo decodes a DEVICE_INFORMATION payload.
func ParseDeviceInfo(p []byte) (DeviceInfo, error) {
	if len(p) < 12 {
		return DeviceInfo{}, fmt.Errorf("gfdi: DEVICE_INFORMATION: %w (%d bytes)", errShort, len(p))
	}
	d := DeviceInfo{
		ProtocolVersion: binary.LittleEndian.Uint16(p[0:]),
		ProductNumber:   binary.LittleEndian.Uint16(p[2:]),
		UnitNumber:      binary.LittleEndian.Uint32(p[4:]),
		SoftwareVersion: binary.LittleEndian.Uint16(p[8:]),
		MaxPacketSize:   binary.LittleEndian.Uint16(p[10:]),
	}
	var err error
	i := 12
	if d.BluetoothName, i, err = lpstr(p, i); err != nil {
		return d, fmt.Errorf("gfdi: DEVICE_INFORMATION name: %w", err)
	}
	if d.DeviceName, i, err = lpstr(p, i); err != nil {
		return d, fmt.Errorf("gfdi: DEVICE_INFORMATION device name: %w", err)
	}
	if d.DeviceModel, i, err = lpstr(p, i); err != nil {
		return d, fmt.Errorf("gfdi: DEVICE_INFORMATION model: %w", err)
	}
	d.Tail = append([]byte(nil), p[i:]...)
	return d, nil
}

// Payload encodes a DEVICE_INFORMATION payload, as a watch sends it (used by fake watches).
func (d DeviceInfo) Payload() []byte {
	b := make([]byte, 0, 16+len(d.BluetoothName)+len(d.DeviceName)+len(d.DeviceModel)+len(d.Tail))
	b = binary.LittleEndian.AppendUint16(b, d.ProtocolVersion)
	b = binary.LittleEndian.AppendUint16(b, d.ProductNumber)
	b = binary.LittleEndian.AppendUint32(b, d.UnitNumber)
	b = binary.LittleEndian.AppendUint16(b, d.SoftwareVersion)
	b = binary.LittleEndian.AppendUint16(b, d.MaxPacketSize)
	b = appendLPStr(b, d.BluetoothName)
	b = appendLPStr(b, d.DeviceName)
	b = appendLPStr(b, d.DeviceModel)
	return append(b, d.Tail...)
}

// CapabilitiesFromBits builds a bitmap of n bytes with the given bits set.
func CapabilitiesFromBits(n int, bits ...int) Capabilities {
	c := make(Capabilities, n)
	for _, bit := range bits {
		if bit >= 0 && bit/8 < n {
			c[bit/8] |= 1 << (bit % 8)
		}
	}
	return c
}

// HostInfo describes the host in its DEVICE_INFORMATION reply. The watch shows the names as
// the connected device; nothing here leaves the Bluetooth link.
type HostInfo struct {
	ProtocolVersion uint16
	SoftwareVersion uint16
	BluetoothName   string
	Manufacturer    string
	Model           string
	// DualPairing is the final flag byte; Garmin Connect and Gadgetbridge send 1 to watches
	// with protocol version 1xx.
	DualPairing bool
}

// GarminConnectHost returns the reply values Garmin Connect sends (docs/protocol/05 §3.6),
// with the given names.
func GarminConnectHost(btName, manufacturer, model string) HostInfo {
	return HostInfo{ProtocolVersion: 150, SoftwareVersion: 0x2DF1, BluetoothName: btName,
		Manufacturer: manufacturer, Model: model, DualPairing: true}
}

// ReplyData returns the bytes that follow [5024][status] in the host's RESPONSE.
func (h HostInfo) ReplyData() []byte {
	b := make([]byte, 0, 32+len(h.BluetoothName)+len(h.Manufacturer)+len(h.Model))
	b = binary.LittleEndian.AppendUint16(b, h.ProtocolVersion)
	b = binary.LittleEndian.AppendUint16(b, 0xFFFF)     // product number: none
	b = binary.LittleEndian.AppendUint32(b, 0xFFFFFFFF) // unit number: none
	b = binary.LittleEndian.AppendUint16(b, h.SoftwareVersion)
	b = binary.LittleEndian.AppendUint16(b, 0xFFFF) // max packet size: unlimited
	b = appendLPStr(b, h.BluetoothName)
	b = appendLPStr(b, h.Manufacturer)
	b = appendLPStr(b, h.Model)
	if h.DualPairing {
		return append(b, 1)
	}
	return append(b, 0)
}

// Capabilities is a CONFIGURATION (5050) capability bitmap; bit n of byte k is capability
// 8k+n.
type Capabilities []byte

// Capability bit numbers used by goarmin (docs/protocol/02 §5.2).
const (
	CapSync                        = 3
	CapDeviceInitiatesSync         = 4
	CapHostInitiatedSyncRequests   = 5
	CapExplicitArchive             = 29
	CapExploreSync                 = 69
	CapCurrentTimeRequestSupport   = 71
	CapMultiLinkService            = 76
	CapOAuthCredentials            = 77
	CapSync2                       = 90
	CapRealtimeSettings            = 92
	CapFeatureCapabilitiesExchange = 95
)

// GarminConnectHostCapabilities is the exact 17-byte host bitmap Garmin Connect sent our
// Fenix 8 (docs/protocol/05 §3.8): 83 bits, SYNC_2 set; MULTI_LINK_SERVICE, EXPLORE_SYNC and
// FEATURE_CAPABILITIES_EXCHANGE clear.
func GarminConnectHostCapabilities() Capabilities {
	return Capabilities{0xF9, 0xFF, 0xDF, 0xFF, 0xFF, 0xFF, 0xFE, 0xCC, 0xDF, 0xA5, 0x0A, 0x17, 0x2B, 0x02, 0x2B, 0x00, 0x02}
}

// Has reports whether capability bit is set.
func (c Capabilities) Has(bit int) bool {
	return bit >= 0 && bit/8 < len(c) && c[bit/8]&(1<<(bit%8)) != 0
}

// Bits lists the set capability bits in ascending order.
func (c Capabilities) Bits() []int {
	var out []int
	for k, b := range c {
		for n := 0; n < 8; n++ {
			if b&(1<<n) != 0 {
				out = append(out, 8*k+n)
			}
		}
	}
	return out
}

// ParseConfiguration decodes a CONFIGURATION payload: [n][n bitmap bytes].
func ParseConfiguration(p []byte) (Capabilities, error) {
	if len(p) < 1 || len(p) < 1+int(p[0]) {
		return nil, fmt.Errorf("gfdi: CONFIGURATION: %w (%d bytes)", errShort, len(p))
	}
	return slices.Clone(Capabilities(p[1 : 1+int(p[0])])), nil
}

// Payload encodes the capabilities as a CONFIGURATION payload.
func (c Capabilities) Payload() []byte {
	return append([]byte{byte(len(c))}, c...)
}
