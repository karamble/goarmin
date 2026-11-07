// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package multilink

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Client IDs a host uses in control messages. The watch echoes the ID in its responses.
const (
	ClientGarminConnect uint64 = 1
	ClientGadgetbridge  uint64 = 2
)

// Service identifies a Multi-Link service a host registers to get a handle.
type Service uint16

// Multi-Link services (docs/protocol/01-ble-transport.md §2.4, 05-fenix8-capture.md §2).
const (
	ServiceGFDI                Service = 1
	ServiceNFC                 Service = 2
	ServiceHealthSDK           Service = 3
	ServiceRegistration        Service = 4
	ServiceConnext             Service = 5
	ServiceRealtimeHR          Service = 6
	ServiceRealtimeSteps       Service = 7
	ServiceRealtimeCalories    Service = 8
	ServiceRealtimeFloors      Service = 9
	ServiceRealtimeIntensity   Service = 10
	ServiceRealtimeHRV         Service = 12
	ServiceRealtimeStress      Service = 13
	ServiceEcho                Service = 15
	ServiceRealtimeAccel       Service = 16
	ServiceRealtimeSpO2        Service = 19
	ServiceRealtimeBodyBattery Service = 20
	ServiceRealtimeRespiration Service = 21
	ServiceKeepAlive           Service = 22
	ServiceRealtimeActiveTime  Service = 26
)

// FileTransferServices are the file pipe services, in the order Garmin Connect rotates through
// them; a host registers the first one not in use for each transfer.
var FileTransferServices = []Service{0x2018, 0x4018, 0x6018, 0x8018, 0xA018, 0xC018, 0xE018}

var serviceNames = map[Service]string{
	ServiceGFDI: "GFDI", ServiceNFC: "NFC", ServiceHealthSDK: "HEALTH_SDK", ServiceRegistration: "REGISTRATION",
	ServiceConnext: "CONNEXT", ServiceRealtimeHR: "REALTIME_HR", ServiceRealtimeSteps: "REALTIME_STEPS",
	ServiceRealtimeCalories: "REALTIME_CALORIES", ServiceRealtimeFloors: "REALTIME_FLOORS",
	ServiceRealtimeIntensity: "REALTIME_INTENSITY", ServiceRealtimeHRV: "REALTIME_HRV",
	ServiceRealtimeStress: "REALTIME_STRESS", ServiceEcho: "ECHO", ServiceRealtimeAccel: "REALTIME_ACCELEROMETER",
	ServiceRealtimeSpO2: "REALTIME_SPO2", ServiceRealtimeBodyBattery: "REALTIME_BODY_BATTERY",
	ServiceRealtimeRespiration: "REALTIME_RESPIRATION", ServiceKeepAlive: "KEEP_ALIVE",
	ServiceRealtimeActiveTime: "REALTIME_ACTIVE_TIME",
}

func (s Service) String() string {
	if n, ok := serviceNames[s]; ok {
		return n
	}
	if s&0x0FFF == 0x018 {
		return fmt.Sprintf("FILE_TRANSFER_%X", uint16(s)>>12)
	}
	return fmt.Sprintf("SERVICE_0x%04X", uint16(s))
}

// ControlType is the second byte of a control message on handle 0x00.
type ControlType byte

// Control message types.
const (
	RegisterRequest     ControlType = 0
	RegisterResponse    ControlType = 1
	CloseHandleRequest  ControlType = 2
	CloseHandleResponse ControlType = 3
	InvalidHandle       ControlType = 4
	CloseAllRequest     ControlType = 5
	CloseAllResponse    ControlType = 6
)

var controlNames = []string{"REGISTER_ML_REQ", "REGISTER_ML_RESP", "CLOSE_HANDLE_REQ", "CLOSE_HANDLE_RESP",
	"INVALID_HANDLE", "CLOSE_ALL_REQ", "CLOSE_ALL_RESP", "UNK_REQ", "UNK_RESP"}

func (t ControlType) String() string {
	if int(t) < len(controlNames) {
		return controlNames[t]
	}
	return fmt.Sprintf("CONTROL_%d", byte(t))
}

const (
	controlHandle = 0x00
	flagReliable  = 0x02
)

func controlHeader(t ControlType, client uint64, n int) []byte {
	b := make([]byte, n)
	b[0], b[1] = controlHandle, byte(t)
	binary.LittleEndian.PutUint64(b[2:10], client)
	return b
}

// EncodeCloseAll builds CLOSE_ALL_REQ. Like Garmin Connect it is 12 bytes; Gadgetbridge's
// 13th byte is an unwritten pad.
func EncodeCloseAll(client uint64) []byte {
	return controlHeader(CloseAllRequest, client, 12)
}

// EncodeRegister builds REGISTER_ML_REQ for svc, asking for a reliable (MLR) handle if
// reliable is set.
func EncodeRegister(client uint64, svc Service, reliable bool) []byte {
	b := controlHeader(RegisterRequest, client, 13)
	binary.LittleEndian.PutUint16(b[10:12], uint16(svc))
	if reliable {
		b[12] = flagReliable
	}
	return b
}

// EncodeCloseHandle builds CLOSE_HANDLE_REQ.
func EncodeCloseHandle(client uint64, svc Service, handle byte) []byte {
	b := controlHeader(CloseHandleRequest, client, 13)
	binary.LittleEndian.PutUint16(b[10:12], uint16(svc))
	b[12] = handle
	return b
}

// Control is a decoded control message.
type Control struct {
	Type    ControlType
	Client  uint64
	Service Service
	// Status is 0 on success (REGISTER_ML_RESP, CLOSE_HANDLE_RESP). garmin-proto-lab names
	// REGISTER_ML_RESP failures 1 invalid service, 2 pending auth, 3 already in use, 4 rejected.
	Status   byte
	Handle   byte
	Reliable bool
	// Flags is the request flag byte of a REGISTER_ML_REQ.
	Flags byte
	// Extra holds bytes after the fields decoded for this type.
	Extra []byte
}

// ErrNotControl reports a packet that is not a control message.
var ErrNotControl = errors.New("multilink: not a control message")

// IsControl reports whether a packet is on the control handle and long enough to hold a
// control header.
func IsControl(b []byte) bool { return len(b) >= 10 && b[0] == controlHandle }

// ParseControl decodes a control message.
func ParseControl(b []byte) (Control, error) {
	if !IsControl(b) {
		return Control{}, ErrNotControl
	}
	c := Control{Type: ControlType(b[1]), Client: binary.LittleEndian.Uint64(b[2:10])}
	rest := b[10:]
	svc := func() bool {
		if len(rest) < 2 {
			return false
		}
		c.Service = Service(binary.LittleEndian.Uint16(rest))
		rest = rest[2:]
		return true
	}
	switch c.Type {
	case RegisterRequest:
		if svc() && len(rest) >= 1 {
			c.Flags, c.Reliable, rest = rest[0], rest[0]&flagReliable != 0, rest[1:]
		}
	case RegisterResponse:
		if svc() && len(rest) >= 1 {
			c.Status, rest = rest[0], rest[1:]
			if c.Status == 0 && len(rest) >= 2 {
				c.Handle, c.Reliable, rest = rest[0], rest[1] != 0, rest[2:]
			}
		}
	case CloseHandleRequest:
		if svc() && len(rest) >= 1 {
			c.Handle, rest = rest[0], rest[1:]
		}
	case CloseHandleResponse:
		// Unlike REGISTER_ML_RESP, the handle precedes the status here.
		if svc() && len(rest) >= 2 {
			c.Handle, c.Status, rest = rest[0], rest[1], rest[2:]
		}
	case InvalidHandle:
		if len(rest) >= 3 {
			c.Handle = rest[2]
		}
	}
	c.Extra = append([]byte(nil), rest...)
	return c, nil
}

// String formats a control message for logs.
func (c Control) String() string {
	s := fmt.Sprintf("%s client=%d", c.Type, c.Client)
	switch c.Type {
	case RegisterRequest:
		s += fmt.Sprintf(" service=%s reliable=%v", c.Service, c.Reliable)
	case RegisterResponse:
		s += fmt.Sprintf(" service=%s status=%d", c.Service, c.Status)
		if c.Status == 0 {
			s += fmt.Sprintf(" handle=0x%02X reliable=%v", c.Handle, c.Reliable)
		}
	case CloseHandleRequest, CloseHandleResponse:
		s += fmt.Sprintf(" service=%s handle=0x%02X status=%d", c.Service, c.Handle, c.Status)
	}
	if len(c.Extra) > 0 {
		s += fmt.Sprintf(" extra=% X", c.Extra)
	}
	return s
}

// Responses are sent by the watch. goarmin encodes them only for fake watches and tests.

// EncodeRegisterResponse builds REGISTER_ML_RESP in the Fenix 8's 16-byte form (a trailing
// 00 after the reliable byte). handle and reliable are meaningful only when status is 0.
func EncodeRegisterResponse(client uint64, svc Service, status, handle byte, reliable bool) []byte {
	b := controlHeader(RegisterResponse, client, 16)
	binary.LittleEndian.PutUint16(b[10:12], uint16(svc))
	b[12], b[13] = status, handle
	if reliable {
		b[14] = 1
	}
	return b
}

// EncodeCloseHandleResponse builds CLOSE_HANDLE_RESP (the handle precedes the status).
func EncodeCloseHandleResponse(client uint64, svc Service, handle, status byte) []byte {
	b := controlHeader(CloseHandleResponse, client, 14)
	binary.LittleEndian.PutUint16(b[10:12], uint16(svc))
	b[12], b[13] = handle, status
	return b
}

// EncodeCloseAllResponse builds CLOSE_ALL_RESP with the trailer the Fenix 8 sends, 00 00 01.
func EncodeCloseAllResponse(client uint64) []byte {
	b := controlHeader(CloseAllResponse, client, 13)
	b[12] = 1
	return b
}
