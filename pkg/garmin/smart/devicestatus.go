// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import "google.golang.org/protobuf/encoding/protowire"

// DeviceStatusService fields (gdi_device_status: field numbers only).
const (
	dsBatteryRequest  = 2 // RemoteDeviceBatteryStatusRequest {}
	dsBatteryResponse = 3 // RemoteDeviceBatteryStatusResponse
)

// BatteryStatusOK is the response status of a battery answer that carries a level.
const BatteryStatusOK = 1

// BatteryLevel is the watch's answer to a battery request.
type BatteryLevel struct {
	Status         uint64 // 1 OK, 2 no remote device, 0 unknown
	Percent        int    // -1 when absent
	BatteryStatus  uint64 // the watch's own condition code (meaning unpublished)
	HoursRemaining int    // -1 when absent
}

// BatteryRequest returns the Smart bytes that ask the watch for its battery level, as
// Gadgetbridge does after the handshake (DeviceStatusService.remote_device_battery_status_request).
func BatteryRequest() []byte {
	return Envelope{Service: ServiceDeviceStatus, Payload: encLen(nil, dsBatteryRequest, nil)}.Marshal()
}

// IsBatteryRequest reports whether env asks for the battery (for a watch simulator).
func IsBatteryRequest(env Envelope) bool {
	return env.Service == ServiceDeviceStatus && hasField(env.Payload, dsBatteryRequest)
}

// BatteryResponse returns the Smart bytes of a watch's answer with percent (for a simulator).
func BatteryResponse(percent int) []byte {
	resp := encVarint(nil, 1, BatteryStatusOK)
	resp = encVarint(resp, 2, uint64(percent))
	return Envelope{Service: ServiceDeviceStatus, Payload: encLen(nil, dsBatteryResponse, resp)}.Marshal()
}

// ParseBatteryResponse reads the answer to BatteryRequest, and false if env is not one.
func ParseBatteryResponse(env Envelope) (BatteryLevel, bool) {
	b := BatteryLevel{Percent: -1, HoursRemaining: -1}
	if env.Service != ServiceDeviceStatus {
		return b, false
	}
	found := false
	err := walk("DeviceStatusService", env.Payload, nil, func(f *field) (bool, error) {
		if f.num != dsBatteryResponse || f.typ != protowire.BytesType {
			return true, nil
		}
		found = true
		return true, walk("RemoteDeviceBatteryStatusResponse", f.data, nil, func(g *field) (bool, error) {
			if g.typ != protowire.VarintType {
				return true, nil
			}
			switch g.num {
			case 1:
				b.Status = g.u
			case 2:
				b.Percent = int(g.u)
			case 3:
				b.BatteryStatus = g.u
			case 4:
				b.HoursRemaining = int(g.u)
			}
			return true, nil
		})
	})
	return b, err == nil && found
}
