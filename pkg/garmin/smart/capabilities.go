// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import "google.golang.org/protobuf/encoding/protowire"

// Feature capabilities (gdi_core.proto CoreService fields 8 and 9): right after the handshake's
// CONFIGURATION, Garmin Connect tells the watch which services it supports, and the watch
// answers with its own. Garmin Connect's request includes the GarminConnect service's
// wellness_threshold_met_support; the watch answers wellness_support and
// lifestyle_logging_support (docs/protocol/05 §3, the capture at 1.379 s).

const (
	coreFeatureCapabilitiesRequest  = 8
	coreFeatureCapabilitiesResponse = 9

	fcGarminGUID       = 1
	fcVersion          = 2
	fcGarminConnect    = 11 // GarminConnect.ClientFeatureCapabilities / ServerFeatureCapabilities
	fcFileAccess       = 16 // FileAccess.Capabilities
	fcDeviceStatus     = 19 // DeviceStatus.CapabilitiesRequest / Response
	gcWellnessMet      = 5  // GarminConnect.Service.wellness_threshold_met_notification
	gcLifestyleLogging = 7  // GarminConnect.Service.lifestyle_logging_update_request_notification
)

// HostFeatures is what a host announces in its FeatureCapabilitiesRequest: a subset of what
// Garmin Connect sends. No GUID is sent (Garmin Connect sends its installation's).
type HostFeatures struct {
	// WellnessThresholdMet: garmin_connect_request { version: 2, wellness_threshold_met_support: true }.
	WellnessThresholdMet bool
	// FileAccess: an empty request_file_access_capabilities, which asks for the watch's.
	FileAccess bool
	// DeviceStatus: an empty device_status_request.
	DeviceStatus bool
}

// GarminConnectFeatures is the part of Garmin Connect's request that concerns syncing.
var GarminConnectFeatures = HostFeatures{WellnessThresholdMet: true, FileAccess: true, DeviceStatus: true}

// FeatureCapabilitiesRequest returns the Smart bytes of the host's announcement.
func FeatureCapabilitiesRequest(h HostFeatures) []byte {
	var p []byte
	if h.WellnessThresholdMet {
		p = encLen(p, fcGarminConnect, encVarint(encVarint(nil, 1, 2), 2, 1))
	}
	if h.FileAccess {
		p = encLen(p, fcFileAccess, nil)
	}
	if h.DeviceStatus {
		p = encLen(p, fcDeviceStatus, nil)
	}
	return Envelope{Service: ServiceCore, Payload: encLen(nil, coreFeatureCapabilitiesRequest, p)}.Marshal()
}

// GUIDStatus is FeatureCapabilitiesResponse.GuidStatus.
type GUIDStatus uint64

// GUIDStatus values.
const (
	GUIDUnset   GUIDStatus = 0
	GUIDMatch   GUIDStatus = 1
	GUIDNoMatch GUIDStatus = 2
)

func (g GUIDStatus) String() string {
	switch g {
	case GUIDMatch:
		return "MATCH"
	case GUIDNoMatch:
		return "NO_MATCH"
	}
	return "UNSET"
}

// WatchFeatures is the watch's FeatureCapabilitiesResponse, as far as goarmin reads it.
type WatchFeatures struct {
	GUIDStatus              GUIDStatus
	Version                 uint64
	WellnessSupport         bool
	LifestyleLoggingSupport bool
	// FileAccess capabilities (present when the watch answered them).
	FileAccess          bool
	CustomFlagSupport   bool
	ChecksumMethod      uint64 // 1: truncated MD5
	ChecksumMaxFileSize uint64
	// Fields lists the response's top-level field numbers, for the record.
	Fields []uint64
}

// ParseFeatureCapabilitiesResponse reads the watch's answer, and false if env is not one.
func ParseFeatureCapabilitiesResponse(env Envelope) (WatchFeatures, bool) {
	var w WatchFeatures
	if env.Service != ServiceCore {
		return w, false
	}
	found := false
	err := walk("CoreService", env.Payload, nil, func(f *field) (bool, error) {
		if f.num != coreFeatureCapabilitiesResponse || f.typ != protowire.BytesType {
			return true, nil
		}
		found = true
		return true, walk("FeatureCapabilitiesResponse", f.data, nil, func(g *field) (bool, error) {
			w.Fields = append(w.Fields, uint64(g.num))
			switch {
			case g.num == 1 && g.typ == protowire.VarintType:
				w.GUIDStatus = GUIDStatus(g.u)
			case g.num == 2 && g.typ == protowire.VarintType:
				w.Version = g.u
			case g.num == fcGarminConnect && g.typ == protowire.BytesType:
				return true, walk("ServerFeatureCapabilities", g.data, nil, func(h *field) (bool, error) {
					if h.typ == protowire.VarintType {
						switch h.num {
						case 2:
							w.WellnessSupport = h.u != 0
						case 3:
							w.LifestyleLoggingSupport = h.u != 0
						}
					}
					return true, nil
				})
			case g.num == fcFileAccess && g.typ == protowire.BytesType:
				w.FileAccess = true
				return true, walk("FileAccess.Capabilities", g.data, nil, func(h *field) (bool, error) {
					if h.typ == protowire.VarintType {
						switch h.num {
						case 3:
							w.ChecksumMethod = h.u
						case 4:
							w.ChecksumMaxFileSize = h.u
						case 5:
							w.CustomFlagSupport = h.u != 0
						}
					}
					return true, nil
				})
			}
			return true, nil
		})
	})
	return w, err == nil && found
}

// IsFeatureCapabilitiesRequest reports whether env is a host's announcement (for a watch
// simulator).
func IsFeatureCapabilitiesRequest(env Envelope) bool {
	return env.Service == ServiceCore && hasField(env.Payload, coreFeatureCapabilitiesRequest)
}

// FeatureCapabilitiesResponse returns the Smart bytes of a watch's answer (for a simulator):
// GUID status UNSET, version 1, wellness and lifestyle logging support, custom flags.
func FeatureCapabilitiesResponse() []byte {
	r := encVarint(nil, 1, uint64(GUIDUnset))
	r = encVarint(r, 2, 1)
	r = encLen(r, fcGarminConnect, encVarint(encVarint(nil, 2, 1), 3, 1))
	r = encLen(r, fcFileAccess, encVarint(encVarint(encVarint(nil, 3, 1), 4, 3145728), 5, 1))
	return Envelope{Service: ServiceCore, Payload: encLen(nil, coreFeatureCapabilitiesResponse, r)}.Marshal()
}

// IsWellnessThresholdMet reports whether env is the watch's WellnessThresholdMetNotification:
// it has collected enough wellness data for the host to cut files.
func IsWellnessThresholdMet(env Envelope) bool {
	return env.Service == ServiceGarminConnect && hasField(env.Payload, gcWellnessMet)
}

// IsLifestyleLoggingUpdate reports whether env is the watch's
// LifestyleLoggingUpdateRequestNotification.
func IsLifestyleLoggingUpdate(env Envelope) bool {
	return env.Service == ServiceGarminConnect && hasField(env.Payload, gcLifestyleLogging)
}

// WellnessThresholdMetNotification returns the Smart bytes of that notification (for a simulator).
func WellnessThresholdMetNotification() []byte {
	return Envelope{Service: ServiceGarminConnect, Payload: encLen(nil, gcWellnessMet, nil)}.Marshal()
}
