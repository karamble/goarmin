// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import (
	"reflect"
	"testing"
	"time"
)

// Decode tests for messages the watch sends, built record by record as the Fenix 8 sent them
// (docs/protocol/05 §8). UUIDs are synthetic and timestamps derive from the clock.

func smartFileAccess(fields ...[]byte) []byte { return lfield(ServiceFileAccess, fields...) }

func parseSmartFileAccess(t *testing.T, b []byte) FileAccess {
	t.Helper()
	env, err := ParseEnvelope(b)
	if err != nil {
		t.Fatal(err)
	}
	f, err := env.FileAccess()
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// itemRecord builds an ItemReference as the watch lists it.
func itemRecord(msb, lsb uint64, dataType []byte, size uint64, txn uint64, urgency Urgency, mtime uint32) []byte {
	return cat(
		lfield(1, uuidb(msb, lsb)),
		lfield(2, dataType),
		vfield(3, size),
		vfield(4, 0), // requested_flags
		vfield(5, txn),
		vfield(6, uint64(urgency)),
		vfield(7, uint64(mtime)),
	)
}

func TestDecodeItemListResponsePages(t *testing.T) {
	now := time.Now()
	mtime := func(ago time.Duration) uint32 { return uint32(now.Add(-ago).Unix()) }

	// Page 1: only the first item names its type; the others carry just the key.
	named := cat(vfield(1, 0), sfield(2, "FIT_TYPE_32"), vfield(3, 7))
	keyOnly := cat(vfield(1, 0), vfield(3, 7))
	page1 := smartFileAccess(lfield(10,
		vfield(1, 0), // status SUCCESS, present
		vfield(2, 1), // session_id: more pages follow
		lfield(4, itemRecord(0x1001, 0x100, named, 2048, 33652, UrgencySlow, mtime(3*time.Hour))),
		lfield(4, itemRecord(0x1002, 0x200, keyOnly, 4096, 33653, UrgencySlow, mtime(2*time.Hour))),
		lfield(4, itemRecord(0x1003, 0x300, keyOnly, 512, 33654, UrgencySlow, mtime(time.Hour))),
	))
	// Page 2: no session_id, next_transaction_id instead.
	page2 := smartFileAccess(lfield(10,
		vfield(1, 0),
		vfield(3, 38519),
		lfield(4, itemRecord(0x1004, 0x400, keyOnly, 1910, 33655, UrgencyNormal, mtime(time.Minute))),
	))

	names := map[uint32]string{} // per-session key -> type name
	var all []ItemReference
	for i, b := range [][]byte{page1, page2} {
		f := parseSmartFileAccess(t, b)
		if f.Kind() != "ItemListResponse" {
			t.Fatalf("page %d: Kind = %s", i+1, f.Kind())
		}
		r := f.ItemListResponse
		if r.Status == nil || *r.Status != ItemListSuccess {
			t.Errorf("page %d: status %v", i+1, r.Status)
		}
		for _, it := range r.Items {
			if dt := it.DataType; dt != nil && dt.GDXMLString != nil && dt.GDXMLStringKey != nil {
				names[*dt.GDXMLStringKey] = *dt.GDXMLString
			}
		}
		all = append(all, r.Items...)
		switch i {
		case 0:
			if r.SessionID == nil || *r.SessionID != 1 || r.NextTransactionID != nil {
				t.Errorf("page 1: session %v, next transaction %v", r.SessionID, r.NextTransactionID)
			}
		case 1:
			if r.SessionID != nil || r.NextTransactionID == nil || *r.NextTransactionID != 38519 {
				t.Errorf("page 2: session %v, next transaction %v", r.SessionID, r.NextTransactionID)
			}
		}
	}
	if len(all) != 4 {
		t.Fatalf("%d items, want 4", len(all))
	}
	for i, it := range all {
		if it.UID == nil || it.UID.MSB != uint64(0x1001+i) || it.UID.LSB != uint64(0x100*(i+1)) {
			t.Errorf("item %d: uid %v", i, it.UID)
		}
		dt := it.DataType
		if dt == nil || dt.Format == nil || *dt.Format != FormatGDXMLDataType || dt.GDXMLStringKey == nil {
			t.Fatalf("item %d: data type %#v", i, dt)
		}
		if (dt.GDXMLString != nil) != (i == 0) {
			t.Errorf("item %d: gdxml_string %v", i, dt.GDXMLString)
		}
		if names[*dt.GDXMLStringKey] != "FIT_TYPE_32" {
			t.Errorf("item %d: key %d does not resolve", i, *dt.GDXMLStringKey)
		}
		if it.RequestedFlags == nil || *it.RequestedFlags != 0 || it.TransactionID == nil || *it.TransactionID != uint32(33652+i) {
			t.Errorf("item %d: flags %v, transaction %v", i, it.RequestedFlags, it.TransactionID)
		}
		if it.ModifiedTime == nil || *it.ModifiedTime > uint32(now.Unix()) {
			t.Errorf("item %d: modified time %v", i, it.ModifiedTime)
		}
	}
	if *all[3].Urgency != UrgencyNormal || *all[0].Urgency != UrgencySlow || *all[3].DataSize != 1910 {
		t.Errorf("urgency/size: %v %v %v", *all[0].Urgency, *all[3].Urgency, *all[3].DataSize)
	}
}

func TestDecodePullItemDataResponse(t *testing.T) {
	f := parseSmartFileAccess(t, smartFileAccess(lfield(2,
		vfield(1, 0), vfield(2, 0), vfield(3, 23), vfield(7, 11))))
	want := &PullItemDataResponse{
		Result:            Ptr(ItemAccessSuccess),
		Transport:         Ptr(TransportMultilinkPipe),
		TransferHandle:    U32(23),
		CompressionWindow: U32(11),
	}
	if !reflect.DeepEqual(f.PullItemResponse, want) || f.Kind() != "PullItemResponse" {
		t.Errorf("got %#v", f.PullItemResponse)
	}

	// A failure with negative sint32 codes (zigzag 3 = -2, 9 = -5).
	f = parseSmartFileAccess(t, smartFileAccess(lfield(2, vfield(1, 3), vfield(5, 3), vfield(6, 4), vfield(9, 9))))
	r := f.PullItemResponse
	if *r.Result != ItemAccessNoAvailableResources || *r.ErrorCode != -2 || *r.ErrorSource != 4 || *r.DataSpecificErrorCode != -5 {
		t.Errorf("failure: %v %d %d %d", *r.Result, *r.ErrorCode, *r.ErrorSource, *r.DataSpecificErrorCode)
	}
	if r.CompressionWindow != nil || r.TransferHandle != nil {
		t.Error("absent fields decoded as present")
	}
}

func TestDecodeTransferStatusRequest(t *testing.T) {
	f := parseSmartFileAccess(t, smartFileAccess(lfield(5, lfield(1, uuidb(0x1234, 0x5678)), vfield(2, 23))))
	want := &TransferStatusRequest{UID: &itemUID, TransferHandle: U32(23)}
	if !reflect.DeepEqual(f.TransferStatusRequest, want) || f.Kind() != "TransferStatusRequest" {
		t.Errorf("got %#v", f.TransferStatusRequest)
	}

	f = parseSmartFileAccess(t, smartFileAccess(lfield(5, lfield(1, uuidb(1, 2)), vfield(2, 9), vfield(3, 4), vfield(7, 30))))
	r := f.TransferStatusRequest
	if r.FailureReason == nil || *r.FailureReason != FailureCancelledByDevice || *r.NewPriority != 30 {
		t.Errorf("failure: %#v", r)
	}
}

func TestDecodeItemUpdatedNotification(t *testing.T) {
	f := parseSmartFileAccess(t, smartFileAccess(lfield(17, lfield(1, uuidb(0x1234, 0x5678)), lfield(2, uuidb(0xA5A5, 0xA5A5)))))
	want := &ItemUpdatedNotification{UID: &itemUID, ChangedFlagsSet: []UUID{SyncedFlag}}
	if !reflect.DeepEqual(f.ItemUpdated, want) || f.Kind() != "ItemUpdated" {
		t.Errorf("got %#v", f.ItemUpdated)
	}
}

func TestDecodeResourceUpdateNotification(t *testing.T) {
	f := parseSmartFileAccess(t, smartFileAccess(lfield(20, vfield(1, 1), vfield(2, 24))))
	want := &ResourceUpdateNotification{Status: Ptr(ResourceCurrentPriorityLowered), CurrentPriority: U32(24)}
	if !reflect.DeepEqual(f.ResourceUpdate, want) || f.Kind() != "ResourceUpdate" {
		t.Errorf("got %#v", f.ResourceUpdate)
	}
}

func TestDecodeItemAddedNotification(t *testing.T) {
	mtime := uint32(time.Now().Add(-5 * time.Minute).Unix())
	dt := cat(vfield(1, 0), sfield(2, "FIT_TYPE_4"), vfield(3, 3))
	f := parseSmartFileAccess(t, smartFileAccess(lfield(12, lfield(1, itemRecord(0x1234, 0x5678, dt, 1910, 33700, UrgencyNormal, mtime)))))
	want := &ItemAddedNotification{Items: []ItemReference{{
		UID:            &itemUID,
		DataType:       &DataType{Format: Ptr(FormatGDXMLDataType), GDXMLString: S("FIT_TYPE_4"), GDXMLStringKey: U32(3)},
		DataSize:       U64(1910),
		RequestedFlags: U32(0),
		TransactionID:  U32(33700),
		Urgency:        Ptr(UrgencyNormal),
		ModifiedTime:   U32(mtime),
	}}}
	if !reflect.DeepEqual(f.ItemAdded, want) || f.Kind() != "ItemAdded" {
		t.Errorf("got %#v", f.ItemAdded)
	}
}

func TestDecodeSmallNotifications(t *testing.T) {
	f := parseSmartFileAccess(t, smartFileAccess(lfield(22)))
	if f.SyncButton == nil || f.Kind() != "SyncButton" {
		t.Errorf("sync button: %#v", f)
	}
	f = parseSmartFileAccess(t, smartFileAccess(lfield(11, vfield(1, 2))))
	if f.ItemListCancel == nil || *f.ItemListCancel.SessionID != 2 {
		t.Errorf("list cancel: %#v", f.ItemListCancel)
	}
	f = parseSmartFileAccess(t, smartFileAccess(lfield(16, vfield(1, 0))))
	if f.ModifyFlagsResponse == nil || *f.ModifyFlagsResponse.Status != ModifyFlagsSuccess {
		t.Errorf("modify flags response: %#v", f.ModifyFlagsResponse)
	}
	f = parseSmartFileAccess(t, smartFileAccess(lfield(7, vfield(1, 0))))
	if f.PriorityUpdateResponse == nil || *f.PriorityUpdateResponse.Status != PriorityUpdateSuccess {
		t.Errorf("priority update response: %#v", f.PriorityUpdateResponse)
	}
	f = parseSmartFileAccess(t, smartFileAccess(lfield(19, vfield(1, 0))))
	if f.CancelTransferResponse == nil || *f.CancelTransferResponse.Status != CancelTransferSuccess {
		t.Errorf("cancel transfer response: %#v", f.CancelTransferResponse)
	}
	f = parseSmartFileAccess(t, smartFileAccess(lfield(4, vfield(1, 0), vfield(4, 41))))
	if f.PushItemResponse == nil || *f.PushItemResponse.TransferHandle != 41 {
		t.Errorf("push response: %#v", f.PushItemResponse)
	}
}

func TestDecodePackedRepeatedEnum(t *testing.T) {
	// Garmin sends repeated enums unpacked; the packed form is accepted as well.
	r, err := ParsePullItemDataRequest(cat(lfield(3, []byte{0x00, 0x00}), vfield(3, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.SupportedTransports) != 3 {
		t.Errorf("transports %v", r.SupportedTransports)
	}
	// Re-marshalled unpacked.
	wantBytes(t, "unpacked", r.Marshal(), "18 00 18 00 18 00")
}
