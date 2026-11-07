// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import "testing"

// Golden encodings, worked out by hand from gdi_file_access.proto and compared against the
// byte patterns in docs/protocol/04 and the values Garmin Connect sent in docs/protocol/05 §8.
// UUIDs are synthetic: real item UUIDs embed a timestamp.

const syncedUUID = "09 A5 A5 00 00 00 00 00 00 11 A5 A5 00 00 00 00 00 00"

var itemUID = UUID{MSB: 0x1234, LSB: 0x5678}

const itemUIDBytes = "09 34 12 00 00 00 00 00 00 11 78 56 00 00 00 00 00 00"

func TestGoldenItemListRequest(t *testing.T) {
	first := FileAccess{ItemListRequest: &ItemListRequest{
		ExcludeAllFlags: []UUID{SyncedFlag},
		GetFlags:        []UUID{SyncedFlag},
	}}.Smart()
	if len(first) != 45 {
		t.Errorf("first listing is %d bytes, want 45", len(first))
	}
	// Smart field 43 (DA 02), Service field 9 (4A), exclude_all_flags 4 (22), get_flags 5 (2A).
	wantBytes(t, "first listing", first, "DA 02 2A 4A 28 22 12 "+syncedUUID+" 2A 12 "+syncedUUID)

	cont := FileAccess{ItemListRequest: &ItemListRequest{SessionID: U32(1)}}.Smart()
	wantBytes(t, "continuation", cont, "DA 02 04 4A 02 08 01")

	// Incremental listing with a previous next_transaction_id (38519 as in the capture).
	incr := FileAccess{ItemListRequest: &ItemListRequest{
		TransactionID:   U32(38519),
		ExcludeAllFlags: []UUID{SyncedFlag},
		GetFlags:        []UUID{SyncedFlag},
	}}.Smart()
	wantBytes(t, "incremental listing", incr, "DA 02 2E 4A 2C 10 F7 AC 02 22 12 "+syncedUUID+" 2A 12 "+syncedUUID)

	// Garmin Connect's full-file listing: get_flags plus included_data_types{format GDXML_FULL_FILE}.
	full := FileAccess{ItemListRequest: &ItemListRequest{
		GetFlags:          []UUID{SyncedFlag},
		IncludedDataTypes: []DataType{{Format: Ptr(FormatGDXMLFullFile)}},
	}}.Smart()
	wantBytes(t, "full-file listing", full, "DA 02 1A 4A 18 2A 12 "+syncedUUID+" 32 02 08 03")
}

func TestGoldenTransferStatusResponse(t *testing.T) {
	gc := FileAccess{TransferStatusResponse: &TransferStatusResponse{NextTransferPriority: U32(24)}}.Smart()
	wantBytes(t, "Garmin Connect reply", gc, "DA 02 05 AA 01 02 08 18")
	gb := FileAccess{TransferStatusResponse: &TransferStatusResponse{NextTransferPriority: U32(1)}}.Smart()
	wantBytes(t, "Gadgetbridge reply", gb, "DA 02 05 AA 01 02 08 01")
}

func TestGoldenModifyFlagsRequest(t *testing.T) {
	got := FileAccess{ModifyFlagsRequest: &ModifyFlagsRequest{
		UID:      &itemUID,
		SetFlags: []UUID{SyncedFlag},
	}}.Smart()
	// docs/protocol/04 §3.7: DA 02 2A 7A 28 0A 12 09 <most> 11 <least> 12 12 09 A5 A5 ... 11 A5 A5 ...
	wantBytes(t, "archive", got, "DA 02 2A 7A 28 0A 12 "+itemUIDBytes+" 12 12 "+syncedUUID)
}

func TestGoldenPullItemDataRequest(t *testing.T) {
	req := PullItemDataRequest{
		Item:                       &ItemReference{UID: &itemUID},
		Priority:                   U32(24),
		SupportedTransports:        []TransportProtocol{TransportMultilinkPipe},
		Offset:                     U64(0),
		RequestedCompressionWindow: U32(15),
	}
	got := FileAccess{PullItemRequest: &req}.Smart()
	// The tail after item is 10 18 18 00 20 00 28 0F (docs/protocol/04 §3.5); offset 0 is
	// present because the pointer is set.
	wantBytes(t, "Garmin Connect pull", got, "DA 02 20 0A 1E 0A 14 0A 12 "+itemUIDBytes+" 10 18 18 00 20 00 28 0F")

	req.Offset = nil
	wantBytes(t, "pull without offset", req.Marshal(), "0A 14 0A 12 "+itemUIDBytes+" 10 18 18 00 28 0F")
}

func TestGoldenPullItemDataResponse(t *testing.T) {
	resp := PullItemDataResponse{
		Result:            Ptr(ItemAccessSuccess),
		Transport:         Ptr(TransportMultilinkPipe),
		TransferHandle:    U32(23),
		CompressionWindow: U32(11),
		ErrorCode:         I32(-3), // sint32: zigzag 5
	}
	wantBytes(t, "pull response", resp.Marshal(), "08 00 10 00 18 17 28 05 38 0B")
}

func TestGoldenHTTPNoNetworkReplies(t *testing.T) {
	url := sfield(1, "https://example.invalid/x")
	cases := []struct {
		name    string
		request []byte // ConnectIQHTTPService payload
		want    string // "" means no reply
	}{
		{"ConnectIQHTTPRequest", lfield(1, url), "12 07 12 05 08 AC 02 10 00"},
		{"ConnectIQImageRequest", lfield(3, url, vfield(2, 7)), "12 05 22 03 08 C8 01"},
		{"RawResourceRequest", lfield(5, url, vfield(3, 1)), "12 05 32 03 08 C8 01"},
		{"ConnectIQOAuthRequest", lfield(7, url), ""},
		{"OpenWebpageNotification", lfield(11, url), ""},
		{"a response, not a request", lfield(2, vfield(1, 100)), ""},
		{"malformed", []byte{0x0A, 0x05}, ""},
	}
	for _, tc := range cases {
		reply, ok := HTTPNoNetworkReply(Envelope{Service: ServiceConnectIQHTTP, Payload: tc.request})
		if tc.want == "" {
			if ok || reply != nil {
				t.Errorf("%s: got reply %s, want none", tc.name, hexs(reply))
			}
			continue
		}
		if !ok {
			t.Errorf("%s: no reply", tc.name)
			continue
		}
		wantBytes(t, tc.name, reply, tc.want)
	}
	if _, ok := HTTPNoNetworkReply(Envelope{Service: ServiceFileAccess, Payload: lfield(5, url)}); ok {
		t.Error("reply for a FileAccess message")
	}
}

func TestGoldenExploreSyncReject(t *testing.T) {
	start := lfield(1, vfield(1, 1), vfield(2, 2), sfield(3, "0123456789abcdef")) // StartSyncRequest
	reply, ok := ExploreSyncReject(Envelope{Service: ServiceExploreSync, Payload: start})
	if !ok {
		t.Fatal("no reply to StartSyncRequest")
	}
	wantBytes(t, "explore reject", reply, "B2 01 04 12 02 08 02")

	finished := lfield(3, vfield(1, 1)) // SyncFinishedNotification
	if _, ok := ExploreSyncReject(Envelope{Service: ServiceExploreSync, Payload: finished}); ok {
		t.Error("reply to SyncFinishedNotification")
	}
	if _, ok := ExploreSyncReject(Envelope{Service: ServiceCore, Payload: start}); ok {
		t.Error("reply to a Core message")
	}
}

func TestIsCoreConnectionReady(t *testing.T) {
	env, err := ParseEnvelope(hexb(t, "6A 02 72 00")) // Smart{13: CoreService{14: {}}}
	if err != nil {
		t.Fatal(err)
	}
	if !IsCoreConnectionReady(env) {
		t.Error("ConnectionReadyNotification not recognised")
	}
	for name, env := range map[string]Envelope{
		"get_location_request":  {Service: ServiceCore, Payload: lfield(3)},
		"varint field 14":       {Service: ServiceCore, Payload: vfield(14, 0)},
		"other service":         {Service: ServiceFileAccess, Payload: lfield(14)},
		"malformed core":        {Service: ServiceCore, Payload: []byte{0x72}},
		"empty core":            {Service: ServiceCore},
		"ready after a request": {Service: ServiceCore, Payload: cat(lfield(3), lfield(14))},
	} {
		want := name == "ready after a request"
		if got := IsCoreConnectionReady(env); got != want {
			t.Errorf("%s: IsCoreConnectionReady = %v, want %v", name, got, want)
		}
	}
}

func TestEnvelopeGolden(t *testing.T) {
	env := Envelope{Service: ServiceFileAccess, Payload: hexb(t, "AA 01 02 08 18")}
	wantBytes(t, "envelope", env.Marshal(), "DA 02 05 AA 01 02 08 18")
	for _, tc := range []struct {
		service int
		tag     string
	}{
		{ServiceConnectIQHTTP, "12"}, {ServiceDataTransfer, "3A"}, {ServiceCore, "6A"},
		{ServiceExploreSync, "B2 01"}, {ServiceCredentials, "DA 01"}, {ServiceECG, "BA 02"},
		{ServiceFileAccess, "DA 02"},
	} {
		// Smart tag bytes from docs/protocol/04 §2.
		wantBytes(t, ServiceName(tc.service), Envelope{Service: tc.service}.Marshal(), tc.tag+" 00")
	}
}

// TestGoldenCoreLocationReply checks the replies byte for byte against Garmin Connect's in
// the capture (requests as our Fenix 8 sent them, docs/protocol/06).
func TestGoldenCoreLocationReply(t *testing.T) {
	for _, tc := range []struct{ req, want string }{
		{"6A 04 1A 02 08 00", "6A 04 22 02 08 04"},
		{"6A 08 2A 06 08 01 12 02 08 00", "6A 0A 32 08 08 02 12 04 08 00 10 02"},
	} {
		env, err := ParseEnvelope(hexb(t, tc.req))
		if err != nil {
			t.Fatal(err)
		}
		got, ok := CoreLocationReply(env)
		if !ok {
			t.Errorf("no reply to %s", tc.req)
			continue
		}
		wantBytes(t, "reply to "+tc.req, got, tc.want)
	}
	ready, _ := ParseEnvelope(hexb(t, "6A 02 72 00"))
	if _, ok := CoreLocationReply(ready); ok {
		t.Error("ConnectionReadyNotification answered")
	}
}

// TestGoldenCutWellnessFiles: the request as Garmin Connect sends it (capture, 7 bytes) and a
// round trip of the answer.
func TestGoldenCutWellnessFiles(t *testing.T) {
	wantBytes(t, "cut wellness files", CutWellnessFilesRequest(false), "FA 02 04 1A 02 08 00")
	env, err := ParseEnvelope(CutWellnessFilesRequest(true))
	if err != nil || !IsCutWellnessFilesRequest(env) {
		t.Fatalf("request not recognised: %v", err)
	}
	for _, st := range []CutWellnessStatus{CutWellnessSuccess, CutWellnessIgnored, CutWellnessNotSupported} {
		env, err := ParseEnvelope(CutWellnessFilesResponse(st))
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := ParseCutWellnessFilesResponse(env); !ok || got != st {
			t.Errorf("status %v: got %v, %v", st, got, ok)
		}
	}
	if _, ok := ParseCutWellnessFilesResponse(env); ok {
		t.Error("a request parsed as a response")
	}
}
