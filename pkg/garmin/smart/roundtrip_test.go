// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import (
	"bytes"
	"reflect"
	"testing"
)

// TestRoundTrip marshals a fully populated instance of every message type, once with
// distinct values and once with every optional field pointing at a zero value, and checks
// that parsing gives the same value back and that re-marshalling gives the same bytes.
func TestRoundTrip(t *testing.T) {
	for _, tc := range codecCases() {
		for _, zero := range []bool{false, true} {
			fl := filler{zero: zero}
			want := fl.value(reflect.TypeOf(tc.zero)).Interface()
			b := marshalAny(want)
			got, err := tc.parse(b)
			if err != nil {
				t.Errorf("%s (zero=%v): parse: %v", tc.name, zero, err)
				continue
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s (zero=%v): round trip\n got  %#v\n want %#v", tc.name, zero, got, want)
			}
			if b2 := marshalAny(got); !bytes.Equal(b2, b) {
				t.Errorf("%s (zero=%v): re-marshal\n got  %s\n want %s", tc.name, zero, hexs(b2), hexs(b))
			}
		}
		// The empty message is empty on the wire and parses back to the zero value.
		if b := marshalAny(tc.zero); len(b) != 0 && tc.name != "UUID" {
			t.Errorf("%s: zero value marshals to %s", tc.name, hexs(b))
		}
		if got, err := tc.parse(nil); err != nil || !reflect.DeepEqual(got, tc.zero) {
			t.Errorf("%s: parse(nil) = %#v, %v", tc.name, got, err)
		}
	}
}

// TestZeroPresence checks field by field that a pointer to a zero value is emitted and parses
// back as present, while a nil pointer is absent.
func TestZeroPresence(t *testing.T) {
	for _, tc := range codecCases() {
		typ := reflect.TypeOf(tc.zero)
		for i := 0; i < typ.NumField(); i++ {
			sf := typ.Field(i)
			if !sf.IsExported() || sf.Type.Kind() != reflect.Pointer {
				continue
			}
			v := reflect.New(typ).Elem()
			v.Field(i).Set(reflect.New(sf.Type.Elem())) // pointer to the zero value
			b := marshalAny(v.Interface())
			if len(b) == 0 {
				t.Errorf("%s.%s: zero value not emitted", tc.name, sf.Name)
				continue
			}
			got, err := tc.parse(b)
			if err != nil {
				t.Errorf("%s.%s: %v", tc.name, sf.Name, err)
				continue
			}
			if reflect.ValueOf(got).Field(i).IsNil() {
				t.Errorf("%s.%s: zero value lost its presence", tc.name, sf.Name)
			}
		}
	}
	// The case that matters on the wire: ItemListResponse.SessionID = 0 still means "more".
	got, err := ParseItemListResponse(ItemListResponse{SessionID: U32(0)}.Marshal())
	if err != nil || got.SessionID == nil || *got.SessionID != 0 {
		t.Errorf("SessionID 0: got %v, %v", got.SessionID, err)
	}
	if got, _ := ParseItemListResponse(nil); got.SessionID != nil {
		t.Error("absent SessionID parsed as present")
	}
}

// TestUnknownFieldsPreserved appends records of every wire type with unused field numbers to
// each message: parsing and re-marshalling must give the identical bytes.
func TestUnknownFieldsPreserved(t *testing.T) {
	extra := unknownFields()
	for _, tc := range codecCases() {
		if tc.name == "UUID" {
			continue // a plain value; see TestUUIDDropsUnknownFields
		}
		fl := filler{}
		b := append(marshalAny(fl.value(reflect.TypeOf(tc.zero)).Interface()), extra...)
		got, err := tc.parse(b)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if b2 := marshalAny(got); !bytes.Equal(b2, b) {
			t.Errorf("%s: unknown fields not preserved\n got  %s\n want %s", tc.name, hexs(b2), hexs(b))
		}
	}

	// Unknown fields inside a nested message survive too.
	ref := cat(lfield(1, uuidb(0x1234, 0x5678)), vfield(3, 1910), extra)
	page := cat(vfield(1, 0), lfield(4, ref), vfield(3, 38519))
	fa := lfield(10, page)
	got, err := ParseFileAccess(fa)
	if err != nil {
		t.Fatal(err)
	}
	// Marshal emits known fields in field-number order: next_transaction_id (3) before items (4).
	want := lfield(10, cat(vfield(1, 0), vfield(3, 38519), lfield(4, ref)))
	if b := got.Marshal(); !bytes.Equal(b, want) {
		t.Errorf("nested unknown fields\n got  %s\n want %s", hexs(b), hexs(want))
	}
}

func TestUUIDDropsUnknownFields(t *testing.T) {
	u, err := ParseUUID(cat(uuidb(0x1234, 0x5678), vfield(99, 1)))
	if err != nil || u != itemUID {
		t.Fatalf("ParseUUID = %v, %v", u, err)
	}
	wantBytes(t, "UUID", u.Marshal(), itemUIDBytes)
	// A missing half decodes as zero; Marshal still emits both halves.
	u, err = ParseUUID(xfield(2, 7))
	if err != nil || u != (UUID{LSB: 7}) {
		t.Fatalf("ParseUUID(lsb only) = %v, %v", u, err)
	}
	wantBytes(t, "UUID", u.Marshal(), "09 00 00 00 00 00 00 00 00 11 07 00 00 00 00 00 00 00")
	if s := itemUID.String(); s != "0000000000001234-0000000000005678" {
		t.Errorf("String = %q", s)
	}
}

func TestEnvelopeRoundTrip(t *testing.T) {
	inner := FileAccess{ItemListRequest: &ItemListRequest{SessionID: U32(1)}}
	b := inner.Smart()
	env, err := ParseEnvelope(b)
	if err != nil {
		t.Fatal(err)
	}
	if env.Service != ServiceFileAccess || ServiceName(env.Service) != "file_access_service" {
		t.Errorf("service %d (%s)", env.Service, ServiceName(env.Service))
	}
	if !bytes.Equal(env.Marshal(), b) {
		t.Errorf("re-marshal %s, want %s", hexs(env.Marshal()), hexs(b))
	}
	b[len(b)-1] = 0x7F // Payload is a copy
	got, err := env.FileAccess()
	if err != nil || !reflect.DeepEqual(got, inner) {
		t.Errorf("FileAccess() = %#v, %v", got, err)
	}
	if _, err := (Envelope{Service: ServiceCore}).FileAccess(); err == nil {
		t.Error("FileAccess() of a Core envelope did not fail")
	}
	if n := ServiceName(51); n != "smart_field_51" {
		t.Errorf("ServiceName(51) = %q", n)
	}
	if n := ServiceName(ServiceEventSharing); n != "event_sharing" {
		t.Errorf("ServiceName(30) = %q", n)
	}
}

func TestMergeRepeatedMessageField(t *testing.T) {
	// The same message field twice is merged: scalars take the last value, repeated append.
	b := cat(
		lfield(10, vfield(2, 1), lfield(4, lfield(1, uuidb(1, 1)))),
		lfield(10, vfield(2, 2), lfield(4, lfield(1, uuidb(2, 2)))),
	)
	got, err := ParseFileAccess(b)
	if err != nil {
		t.Fatal(err)
	}
	r := got.ItemListResponse
	if r == nil || r.SessionID == nil || *r.SessionID != 2 || len(r.Items) != 2 || r.Items[1].UID.MSB != 2 {
		t.Errorf("merge: %#v", r)
	}
}

func TestKind(t *testing.T) {
	typ := reflect.TypeOf(FileAccess{})
	for i := 0; i < typ.NumField(); i++ {
		sf := typ.Field(i)
		if !sf.IsExported() {
			continue
		}
		var f FileAccess
		reflect.ValueOf(&f).Elem().Field(i).Set(reflect.New(sf.Type.Elem()))
		if k := f.Kind(); k != sf.Name {
			t.Errorf("Kind with %s set = %q", sf.Name, k)
		}
	}
	two := FileAccess{PullItemRequest: &PullItemDataRequest{}, PullItemResponse: &PullItemDataResponse{}}
	if k := two.Kind(); k != "PullItemRequest+PullItemResponse" {
		t.Errorf("Kind of two = %q", k)
	}
	unk, err := ParseFileAccess(cat(vfield(8, 1), lfield(99)))
	if err != nil {
		t.Fatal(err)
	}
	if k := unk.Kind(); k != "Unknown(8,99)" {
		t.Errorf("Kind of unknown = %q", k)
	}
	if k := (FileAccess{}).Kind(); k != "Empty" {
		t.Errorf("Kind of empty = %q", k)
	}
}

func TestEnumStrings(t *testing.T) {
	for _, tc := range []struct {
		got, want string
	}{
		{ItemAccessNoAvailableResources.String(), "NO_AVAILABLE_RESOURCES"},
		{ItemAccessResult(42).String(), "ItemAccessResult(42)"},
		{TransportMultilinkPipe.String(), "MULTILINK_TRANSPORT_PIPE"},
		{PriorityStandardStart.String(), "STANDARD_START"},
		{PriorityHighestEnd.String(), "HIGHEST_END"},
		{TransferPriorityLevel(24).String(), "TransferPriorityLevel(24)"},
		{ResourceUpdateStatus(0).String(), "ResourceUpdateStatus(0)"},
		{UrgencySlow.String(), "SLOW"},
		{FailureCompressionFailed.String(), "COMPRESSION_FAILED"},
		{ChecksumMethodTruncatedMD5.String(), "TRUNCATED_MD5"},
	} {
		if tc.got != tc.want {
			t.Errorf("String = %q, want %q", tc.got, tc.want)
		}
	}
}
