// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import (
	"strconv"

	"google.golang.org/protobuf/encoding/protowire"
)

// The GarminConnect service (Smart field 47, gdi_garmin_connect.proto): the host asks the
// watch to "cut" its wellness files, i.e. to write the monitoring data collected since the
// last cut into files it then announces with ItemAddedNotification. Garmin Connect sends this
// during every sync; without it the Fenix 8 keeps the data to itself (docs/protocol/06, M2).

const (
	gcCutWellnessFilesRequest  = 3 // Service.cut_wellness_files_request
	gcCutWellnessFilesResponse = 4 // Service.cut_wellness_files_response
)

// CutWellnessStatus is CutWellnessFilesResponse.Status.
type CutWellnessStatus uint32

// CutWellnessStatus values.
const (
	CutWellnessSuccess      CutWellnessStatus = 0
	CutWellnessIgnored      CutWellnessStatus = 1
	CutWellnessNotSupported CutWellnessStatus = 2
)

func (s CutWellnessStatus) String() string {
	switch s {
	case CutWellnessSuccess:
		return "SUCCESS"
	case CutWellnessIgnored:
		return "IGNORED"
	case CutWellnessNotSupported:
		return "NOT_SUPPORTED"
	}
	return "STATUS_" + strconv.FormatUint(uint64(s), 10)
}

// CutWellnessFilesRequest returns the Smart bytes of the request, with priority STANDARD (0)
// or USER_INITIATED (1). STANDARD is FA 02 04 1A 02 08 00, as Garmin Connect sends it.
func CutWellnessFilesRequest(userInitiated bool) []byte {
	var prio uint64
	if userInitiated {
		prio = 1
	}
	req := encLen(nil, gcCutWellnessFilesRequest, encVarint(nil, 1, prio))
	return Envelope{Service: ServiceGarminConnect, Payload: req}.Marshal()
}

// IsCutWellnessFilesRequest reports whether env is a CutWellnessFilesRequest (for a watch
// simulator).
func IsCutWellnessFilesRequest(env Envelope) bool {
	return env.Service == ServiceGarminConnect && hasField(env.Payload, gcCutWellnessFilesRequest)
}

// CutWellnessFilesResponse returns the Smart bytes of the watch's answer.
func CutWellnessFilesResponse(st CutWellnessStatus) []byte {
	resp := encLen(nil, gcCutWellnessFilesResponse, encVarint(nil, 1, uint64(st)))
	return Envelope{Service: ServiceGarminConnect, Payload: resp}.Marshal()
}

// ParseCutWellnessFilesResponse returns the status of a CutWellnessFilesResponse (SUCCESS
// when the field is absent, the proto2 default), and false if env is not one.
func ParseCutWellnessFilesResponse(env Envelope) (CutWellnessStatus, bool) {
	if env.Service != ServiceGarminConnect {
		return 0, false
	}
	var st CutWellnessStatus
	found := false
	err := walk("GarminConnectService", env.Payload, nil, func(f *field) (bool, error) {
		if f.num != gcCutWellnessFilesResponse || f.typ != protowire.BytesType {
			return true, nil
		}
		found = true
		return true, walk("CutWellnessFilesResponse", f.data, nil, func(g *field) (bool, error) {
			if g.num == 1 && g.typ == protowire.VarintType {
				st = CutWellnessStatus(g.u)
			}
			return true, nil
		})
	})
	return st, err == nil && found
}
