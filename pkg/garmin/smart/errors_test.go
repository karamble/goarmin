// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestTruncatedInput(t *testing.T) {
	// Every prefix of a single-field FileAccess message is truncated inside that field.
	full := FileAccess{ItemListRequest: &ItemListRequest{ExcludeAllFlags: []UUID{SyncedFlag}, GetFlags: []UUID{SyncedFlag}}}.Marshal()
	for n := 1; n < len(full); n++ {
		got, err := ParseFileAccess(full[:n])
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("prefix %d: err = %v, want unexpected EOF", n, err)
		}
		if !reflect.DeepEqual(got, FileAccess{}) {
			t.Errorf("prefix %d: partial result %#v", n, got)
		}
	}
	// Prefixes of a message with many fields either end on a field boundary or are truncated.
	fl := filler{}
	many := fl.value(reflect.TypeOf(FileAccess{})).Interface().(FileAccess).Marshal()
	for n := 0; n < len(many); n++ {
		if _, err := ParseFileAccess(many[:n]); err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("prefix %d: err = %v", n, err)
		}
	}
	// Envelope prefixes.
	smart := FileAccess{TransferStatusResponse: &TransferStatusResponse{NextTransferPriority: U32(24)}}.Smart()
	for n := 1; n < len(smart); n++ {
		if _, err := ParseEnvelope(smart[:n]); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("envelope prefix %d: err = %v", n, err)
		}
	}
}

func TestWrongWireType(t *testing.T) {
	fixed := func(num int) []byte { return xfield(num, 1) }
	cases := []struct {
		name  string
		parse func([]byte) error
		in    []byte
	}{
		{"FileAccess.item_list_request as varint", errOf(ParseFileAccess), vfield(9, 1)},
		{"FileAccess.item_list_response as fixed64", errOf(ParseFileAccess), fixed(10)},
		{"ItemListResponse.items as varint", errOf(ParseItemListResponse), vfield(4, 1)},
		{"ItemListResponse.session_id as bytes", errOf(ParseItemListResponse), lfield(2)},
		{"ItemReference.uid as varint", errOf(ParseItemReference), vfield(1, 1)},
		{"ItemReference.data_size as fixed64", errOf(ParseItemReference), fixed(3)},
		{"UUID.most_significant as varint", errOf(ParseUUID), vfield(1, 1)},
		{"UUID.least_significant as fixed32", errOf(ParseUUID), []byte{0x15, 1, 0, 0, 0}},
		{"DataType.gdxml_string as varint", errOf(ParseDataType), vfield(2, 5)},
		{"PullItemDataResponse.result as bytes", errOf(ParsePullItemDataResponse), lfield(1)},
		{"PullItemDataResponse.error_code as fixed64", errOf(ParsePullItemDataResponse), fixed(5)},
		{"PullItemDataRequest.supported_transports as fixed32", errOf(ParsePullItemDataRequest), []byte{0x1D, 0, 0, 0, 0}},
		{"GetItemChecksumResponse.checksum as varint", errOf(ParseGetItemChecksumResponse), vfield(3, 5)},
		{"Capabilities.custom_flag_support as bytes", errOf(ParseCapabilities), lfield(5)},
		{"SoftwareUpdatePartNumberResponse.part_number as varint", errOf(ParseSoftwareUpdatePartNumberResponse), vfield(1, 1)},
	}
	for _, tc := range cases {
		if err := tc.parse(tc.in); !errors.Is(err, ErrWireType) {
			t.Errorf("%s: err = %v, want ErrWireType", tc.name, err)
		}
	}

	// A nested error names the path.
	_, err := ParseFileAccess(lfield(10, lfield(4, vfield(1, 1))))
	if !errors.Is(err, ErrWireType) || !strings.Contains(err.Error(), "FileAccess field 10: ItemListResponse field 4: ItemReference field 1") {
		t.Errorf("nested: %v", err)
	}
}

// errOf adapts a ParseX function to an error-only check.
func errOf[T any](parse func([]byte) (T, error)) func([]byte) error {
	return func(b []byte) error { _, err := parse(b); return err }
}

func TestMalformedInput(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		eof  bool // expect io.ErrUnexpectedEOF
	}{
		{"truncated varint", []byte{0x08, 0x80}, true},
		{"truncated tag", []byte{0x80}, true},
		{"length past the end", []byte{0x0A, 0x05, 0x00}, true},
		{"truncated fixed64", []byte{0x51, 1, 2, 3}, true},
		{"truncated packed enum", cat(lfield(1, lfield(3, []byte{0x80}))), true},
		{"reserved wire type 6", []byte{0x0E}, false},
		{"field number 0", []byte{0x00, 0x00}, false},
		{"stray end group", []byte{0x0C}, false},
		{"unterminated group", []byte{0x0B, 0x08, 0x01}, true},
		{"varint overflow", []byte{0x08, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01}, false},
	}
	for _, tc := range cases {
		_, err := ParseFileAccess(tc.in)
		if err == nil {
			t.Errorf("%s: no error", tc.name)
			continue
		}
		if tc.eof != errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
		if !strings.HasPrefix(err.Error(), "smart: ") {
			t.Errorf("%s: error %q lacks the package prefix", tc.name, err)
		}
	}
}

func TestEnvelopeErrors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		is   error
	}{
		{"empty", "", ErrEnvelope},
		{"varint service", "08 01", ErrWireType},
		{"fixed64 service", "11 00 00 00 00 00 00 00 00", ErrEnvelope},
		{"two services", "12 00 12 00", ErrEnvelope},
		{"trailing byte", "DA 02 00 00", ErrEnvelope},
		{"truncated payload", "DA 02 05 AA", io.ErrUnexpectedEOF},
		{"truncated tag", "DA", io.ErrUnexpectedEOF},
		{"field number 0", "02 00", nil},
		{"field number 2^29", "82 80 80 80 10 00", ErrEnvelope},
	}
	for _, tc := range cases {
		env, err := ParseEnvelope(hexb(t, tc.in))
		if err == nil {
			t.Errorf("%s: no error (got %+v)", tc.name, env)
			continue
		}
		if tc.is != nil && !errors.Is(err, tc.is) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.is)
		}
		if !reflect.DeepEqual(env, Envelope{}) {
			t.Errorf("%s: partial envelope %+v", tc.name, env)
		}
	}
}
