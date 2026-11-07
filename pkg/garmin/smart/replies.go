// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import "google.golang.org/protobuf/encoding/protowire"

// Replies for watch requests goarmin does not serve (docs/protocol/04 §10). Each returns the
// Smart bytes to send back in a PROTOBUF_RESPONSE (5044) that reuses the request's id. The
// watch re-sends an unanswered request about every 5 s, so every request needs some answer.

// Field numbers used below, from gdi_core.proto, gdi_http.proto and gdi_explore_sync.proto.
const (
	coreConnectionReadyNotification = 14 // CoreService.connection_ready_notification

	httpConnectIQRequest    = 1 // ConnectIQHTTPService.connect_iq_http_request
	httpConnectIQResponse   = 2 // .connect_iq_http_response
	httpImageRequest        = 3 // .connect_iq_image_request
	httpImageResponse       = 4 // .connect_iq_image_response
	httpRawResourceRequest  = 5 // .raw_resource_request
	httpRawResourceResponse = 6 // .raw_resource_response

	// ConnectIQHTTPResponse.ResponseStatus.NETWORK_REQUEST_TIMED_OUT.
	ciqHTTPNetworkTimedOut = 300
	// ConnectIQImageResponse and RawResourceResponse ResponseStatus.NETWORK_REQUEST_TIMED_OUT.
	ciqImageNetworkTimedOut = 200
	rawNetworkTimedOut      = 200

	coreGetLocationRequest            = 3 // CoreService.get_location_request
	coreGetLocationResponse           = 4 // .get_location_response
	coreLocationUpdatedSetEnabled     = 5 // .location_updated_set_enabled_request
	coreLocationUpdatedSetEnabledResp = 6 // .location_updated_set_enabled_response
	locationServicesDisabled          = 4 // GetLocationResponse.Status.LOCATION_SERVICES_DISABLED
	locationNotAvailable              = 2 // LocationUpdatedSetEnabledResponse(.RequestStatus).Status.NOT_AVAILABLE

	exploreStartSyncRequest  = 1 // ExploreSyncService.start_sync_request
	exploreStartSyncResponse = 2 // ExploreSyncService.start_sync_response
	startSyncRejected        = 2 // StartSyncStatus.START_SYNC_REJECTED
)

// IsCoreConnectionReady reports whether env is the Core service's ConnectionReadyNotification
// (Smart field 13, CoreService field 14). It must be acknowledged at the GFDI level, but no
// protobuf reply may be sent: a reply confuses some firmware.
func IsCoreConnectionReady(env Envelope) bool {
	return env.Service == ServiceCore && hasField(env.Payload, coreConnectionReadyNotification)
}

// CoreLocationReply returns Garmin Connect's answer to the watch's location requests when the
// phone shares no location (capture, docs/protocol/06), and false for anything else:
//   - GetLocationRequest (3) -> GetLocationResponse{status: LOCATION_SERVICES_DISABLED},
//     Smart bytes 6A 04 22 02 08 04;
//   - LocationUpdatedSetEnabledRequest (5) -> LocationUpdatedSetEnabledResponse{status:
//     NOT_AVAILABLE, requests: each requested type, NOT_AVAILABLE}, Smart bytes
//     6A 0A 32 08 08 02 12 04 08 00 10 02 for one SIGNIFICANT_LOCATION request.
//
// The watch writes out its current monitoring data only once these are answered (06, M2).
func CoreLocationReply(env Envelope) ([]byte, bool) {
	if env.Service != ServiceCore {
		return nil, false
	}
	var reply []byte
	err := walk("CoreService", env.Payload, nil, func(f *field) (bool, error) {
		if reply != nil || f.typ != protowire.BytesType {
			return true, nil
		}
		switch f.num {
		case coreGetLocationRequest:
			reply = encLen(nil, coreGetLocationResponse, encVarint(nil, 1, locationServicesDisabled))
		case coreLocationUpdatedSetEnabled:
			resp := encVarint(nil, 1, locationNotAvailable)
			err := walk("LocationUpdatedSetEnabledRequest", f.data, nil, func(r *field) (bool, error) {
				if r.num != 2 || r.typ != protowire.BytesType {
					return true, nil
				}
				var requested uint64
				_ = walk("LocationUpdatedSetEnabledRequest.Request", r.data, nil, func(q *field) (bool, error) {
					if q.num == 1 && q.typ == protowire.VarintType {
						requested = q.u
					}
					return true, nil
				})
				st := encVarint(encVarint(nil, 1, requested), 2, locationNotAvailable)
				resp = encLen(resp, 2, st)
				return true, nil
			})
			if err != nil {
				return false, err
			}
			reply = encLen(nil, coreLocationUpdatedSetEnabledResp, resp)
		}
		return true, nil
	})
	if err != nil || reply == nil {
		return nil, false
	}
	return Envelope{Service: ServiceCore, Payload: reply}.Marshal(), true
}

// HTTPNoNetworkReply returns the Smart reply saying "no network" for a ConnectIQ HTTP-service
// request (Smart field 2), and false if env is not such a request:
//   - ConnectIQHTTPRequest (1)  -> ConnectIQHTTPResponse{status: NETWORK_REQUEST_TIMED_OUT(300),
//     http_status_code: 0}, Smart bytes 12 07 12 05 08 AC 02 10 00;
//   - ConnectIQImageRequest (3) -> ConnectIQImageResponse{status: NETWORK_REQUEST_TIMED_OUT(200)},
//     Smart bytes 12 05 22 03 08 C8 01;
//   - RawResourceRequest (5)    -> RawResourceResponse{status: NETWORK_REQUEST_TIMED_OUT(200)},
//     Smart bytes 12 05 32 03 08 C8 01.
//
// OAuth requests (7, 9) and OpenWebpageNotification (11) get no reply here: the schema has no
// "no network" status for them, and Gadgetbridge leaves them unanswered too.
func HTTPNoNetworkReply(env Envelope) ([]byte, bool) {
	if env.Service != ServiceConnectIQHTTP {
		return nil, false
	}
	var reply []byte
	err := walk("ConnectIQHTTPService", env.Payload, nil, func(f *field) (bool, error) {
		if reply != nil || f.typ != protowire.BytesType {
			return true, nil
		}
		switch f.num {
		case httpConnectIQRequest:
			resp := encVarint(nil, 1, ciqHTTPNetworkTimedOut) // status
			resp = encVarint(resp, 2, 0)                      // http_status_code
			reply = encLen(nil, httpConnectIQResponse, resp)
		case httpImageRequest:
			reply = encLen(nil, httpImageResponse, encVarint(nil, 1, ciqImageNetworkTimedOut))
		case httpRawResourceRequest:
			reply = encLen(nil, httpRawResourceResponse, encVarint(nil, 1, rawNetworkTimedOut))
		}
		return true, nil
	})
	if err != nil || reply == nil {
		return nil, false
	}
	return Envelope{Service: ServiceConnectIQHTTP, Payload: reply}.Marshal(), true
}

// ExploreSyncReject returns the reply rejecting an ExploreSync StartSyncRequest (Smart field
// 22, ExploreSyncService field 1): StartSyncResponse{status: START_SYNC_REJECTED(2)}, Smart
// bytes B2 01 04 12 02 08 02. It returns false for any other message.
func ExploreSyncReject(env Envelope) ([]byte, bool) {
	if env.Service != ServiceExploreSync || !hasField(env.Payload, exploreStartSyncRequest) {
		return nil, false
	}
	resp := encLen(nil, exploreStartSyncResponse, encVarint(nil, 1, startSyncRejected))
	return Envelope{Service: ServiceExploreSync, Payload: resp}.Marshal(), true
}
