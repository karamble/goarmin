// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/karamble/goarmin/pkg/protoschema"
)

// These tests render our encodings against Gadgetbridge's .proto files (read at run time,
// never copied into the repo). They are skipped when no Gadgetbridge clone is present.

func loadSchema(t *testing.T) *protoschema.Schema {
	t.Helper()
	dir := protoschema.GadgetbridgeDir()
	if dir == "" {
		t.Skip("no Gadgetbridge clone; schema cross-check skipped")
	}
	s, err := protoschema.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const smartMsg = "garmin_vivomovehr.Smart"

func wantRender(t *testing.T, s *protoschema.Schema, name string, b []byte, want ...string) {
	t.Helper()
	got := s.Render(smartMsg, b)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s renders as\n  %s\nwant\n  %s", name, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestSchemaGarminConnectMessages(t *testing.T) {
	s := loadSchema(t)
	wantRender(t, s, "first listing",
		FileAccess{ItemListRequest: &ItemListRequest{ExcludeAllFlags: []UUID{SyncedFlag}, GetFlags: []UUID{SyncedFlag}}}.Smart(),
		"file_access_service {",
		"  item_list_request {",
		"    exclude_all_flags: uuid{0xa5a5, 0xa5a5}",
		"    get_flags: uuid{0xa5a5, 0xa5a5}",
		"  }",
		"}")
	wantRender(t, s, "continuation",
		FileAccess{ItemListRequest: &ItemListRequest{SessionID: U32(1)}}.Smart(),
		"file_access_service {",
		"  item_list_request {",
		"    session_id: 1",
		"  }",
		"}")
	wantRender(t, s, "pull",
		FileAccess{PullItemRequest: &PullItemDataRequest{
			Item:                       &ItemReference{UID: &itemUID},
			Priority:                   U32(24),
			SupportedTransports:        []TransportProtocol{TransportMultilinkPipe},
			Offset:                     U64(0),
			RequestedCompressionWindow: U32(15),
		}}.Smart(),
		"file_access_service {",
		"  pull_item_request {",
		"    item {",
		"      uid: uuid{0x1234, 0x5678}",
		"    }",
		"    priority: 24",
		"    supported_transports: MULTILINK_TRANSPORT_PIPE(0)",
		"    offset: 0",
		"    requested_compression_window: 15",
		"  }",
		"}")
	wantRender(t, s, "archive",
		FileAccess{ModifyFlagsRequest: &ModifyFlagsRequest{UID: &itemUID, SetFlags: []UUID{SyncedFlag}}}.Smart(),
		"file_access_service {",
		"  modify_flags_request {",
		"    uid: uuid{0x1234, 0x5678}",
		"    set_flags: uuid{0xa5a5, 0xa5a5}",
		"  }",
		"}")
	wantRender(t, s, "transfer status reply",
		FileAccess{TransferStatusResponse: &TransferStatusResponse{NextTransferPriority: U32(24)}}.Smart(),
		"file_access_service {",
		"  transfer_status_response {",
		"    next_transfer_priority: 24",
		"  }",
		"}")
	wantRender(t, s, "pull response",
		FileAccess{PullItemResponse: &PullItemDataResponse{
			Result: Ptr(ItemAccessSuccess), Transport: Ptr(TransportMultilinkPipe),
			TransferHandle: U32(23), CompressionWindow: U32(11), ErrorCode: I32(-3),
		}}.Smart(),
		"file_access_service {",
		"  pull_item_response {",
		"    result: SUCCESS(0)",
		"    transport: MULTILINK_TRANSPORT_PIPE(0)",
		"    transfer_handle: 23",
		"    error_code: -3",
		"    compression_window: 11",
		"  }",
		"}")
}

func TestSchemaReplies(t *testing.T) {
	s := loadSchema(t)
	reply, _ := HTTPNoNetworkReply(Envelope{Service: ServiceConnectIQHTTP, Payload: lfield(1, sfield(1, "u"))})
	wantRender(t, s, "CIQ HTTP reply", reply,
		"connect_iq_http_service {",
		"  connect_iq_http_response {",
		"    status: NETWORK_REQUEST_TIMED_OUT(300)",
		"    http_status_code: 0",
		"  }",
		"}")
	reply, _ = HTTPNoNetworkReply(Envelope{Service: ServiceConnectIQHTTP, Payload: lfield(3, sfield(1, "u"))})
	wantRender(t, s, "CIQ image reply", reply,
		"connect_iq_http_service {",
		"  connect_iq_image_response {",
		"    status: NETWORK_REQUEST_TIMED_OUT(200)",
		"  }",
		"}")
	reply, _ = HTTPNoNetworkReply(Envelope{Service: ServiceConnectIQHTTP, Payload: lfield(5, sfield(1, "u"))})
	wantRender(t, s, "raw resource reply", reply,
		"connect_iq_http_service {",
		"  raw_resource_response {",
		"    status: NETWORK_REQUEST_TIMED_OUT(200)",
		"  }",
		"}")
	reply, _ = ExploreSyncReject(Envelope{Service: ServiceExploreSync, Payload: lfield(1)})
	wantRender(t, s, "explore reject", reply,
		"explore_sync_service {",
		"  start_sync_response {",
		"    status: START_SYNC_REJECTED(2)",
		"  }",
		"}")
	wantRender(t, s, "connection ready", hexb(t, "6A 02 72 00"),
		"core_service {",
		"  connection_ready_notification {",
		"  }",
		"}")
}

func TestSchemaServiceNames(t *testing.T) {
	s := loadSchema(t)
	for n := 1; n <= 80; n++ {
		name, ok := s.FieldName(smartMsg, n)
		if got := ServiceName(n); ok && got != name {
			t.Errorf("ServiceName(%d) = %q, schema says %q", n, got, name)
		} else if !ok && got != fmt.Sprintf("smart_field_%d", n) {
			t.Errorf("ServiceName(%d) = %q, schema has no such field", n, got)
		}
	}
}

// protoFieldName maps a Go field name to the proto field name it encodes.
func protoFieldName(msg, goName string) string {
	switch msg + "." + goName {
	case "FileAccess.ItemListCancel":
		return "item_list_cancel_notification"
	case "FileAccess.ItemAdded":
		return "item_added_notification"
	case "FileAccess.ItemUpdated":
		return "item_updated_notification"
	case "FileAccess.ResourceUpdate":
		return "resource_update_notification"
	case "FileAccess.SyncButton":
		return "sync_button_notification"
	case "SoftwareUpdatePartNumberResponse.PartNumbers":
		return "part_number"
	}
	// CamelCase with acronyms (UID, GDXMLString, SessionID, ChecksumTruncatedMD5) to snake_case.
	r := []rune(goName)
	var b strings.Builder
	for i, c := range r {
		if i > 0 && unicode.IsUpper(c) &&
			(unicode.IsLower(r[i-1]) || unicode.IsDigit(r[i-1]) || (i+1 < len(r) && unicode.IsLower(r[i+1]))) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(c))
	}
	return b.String()
}

// renderValue is how protoschema prints v, the value of a field (or its first element);
// "" means a nested message, printed as "name {".
func renderValue(msg, goName string, v reflect.Value) string {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Slice {
		if v.Kind() == reflect.Slice {
			v = v.Index(0)
		} else {
			v = v.Elem()
		}
	}
	if u, ok := v.Interface().(UUID); ok {
		return fmt.Sprintf("uuid{0x%x, 0x%x}", u.MSB, u.LSB)
	}
	if st, ok := v.Interface().(fmt.Stringer); ok && v.Kind() == reflect.Uint32 {
		return fmt.Sprintf("%s(%d)", st, v.Uint())
	}
	switch v.Kind() {
	case reflect.Struct:
		return ""
	case reflect.Uint64:
		if msg+"."+goName == "GetItemChecksumResponse.ChecksumTruncatedMD5" { // fixed64
			return fmt.Sprintf("0x%016x", v.Uint())
		}
		return fmt.Sprint(v.Uint())
	case reflect.String:
		return strconv.Quote(v.String())
	}
	return fmt.Sprint(v.Interface())
}

var unknownLine = regexp.MustCompile(`^\s*f\d+[: ]`)

// TestSchemaEveryField sets each field of each message on its own, renders the encoding
// against the schema and checks the field name and value: a wrong field number, wire type or
// scalar encoding (e.g. sint32 without zigzag) shows up as a different line.
func TestSchemaEveryField(t *testing.T) {
	s := loadSchema(t)
	for _, tc := range codecCases() {
		if !s.HasMessage(tc.proto) {
			t.Errorf("%s: schema has no message %s", tc.name, tc.proto)
			continue
		}
		typ := reflect.TypeOf(tc.zero)
		if tc.name == "UUID" {
			got := s.Render(tc.proto, itemUID.Marshal())
			want := []string{"most_significant: 0x0000000000001234", "least_significant: 0x0000000000005678"}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("UUID renders as %q", got)
			}
			continue
		}
		for i := 0; i < typ.NumField(); i++ {
			sf := typ.Field(i)
			if !sf.IsExported() {
				continue
			}
			v := reflect.New(typ).Elem()
			fl := filler{}
			fl.fill(v.Field(i))
			lines := s.Render(tc.proto, marshalAny(v.Interface()))
			name := protoFieldName(tc.name, sf.Name)
			want := name + " {"
			if val := renderValue(tc.name, sf.Name, v.Field(i)); val != "" {
				want = name + ": " + val
			}
			if len(lines) == 0 || lines[0] != want {
				t.Errorf("%s.%s renders as %q, want first line %q", tc.name, sf.Name, lines, want)
			}
			for _, l := range lines {
				if strings.Contains(l, "!!") || unknownLine.MatchString(l) || strings.Contains(l, "(enum ") {
					t.Errorf("%s.%s: schema does not know line %q", tc.name, sf.Name, l)
				}
			}
		}
	}
}

// TestSchemaEnumNames renders every value 0..63 of every enum-typed field: values our String
// names must render with the same name, values it does not name must be unknown to the
// schema too.
func TestSchemaEnumNames(t *testing.T) {
	s := loadSchema(t)
	for _, tc := range codecCases() {
		typ := reflect.TypeOf(tc.zero)
		for i := 0; i < typ.NumField(); i++ {
			sf := typ.Field(i)
			et := sf.Type
			if et.Kind() == reflect.Pointer || et.Kind() == reflect.Slice {
				et = et.Elem()
			}
			if !sf.IsExported() || et.Kind() != reflect.Uint32 || !et.Implements(reflect.TypeOf((*fmt.Stringer)(nil)).Elem()) {
				continue
			}
			for x := uint64(0); x < 64; x++ {
				ev := reflect.New(et).Elem()
				ev.SetUint(x)
				v := reflect.New(typ).Elem()
				if sf.Type.Kind() == reflect.Slice {
					v.Field(i).Set(reflect.Append(reflect.MakeSlice(sf.Type, 0, 1), ev))
				} else {
					p := reflect.New(et)
					p.Elem().Set(ev)
					v.Field(i).Set(p)
				}
				lines := s.Render(tc.proto, marshalAny(v.Interface()))
				str := ev.Interface().(fmt.Stringer).String()
				name := protoFieldName(tc.name, sf.Name)
				want := fmt.Sprintf("%s: %s(%d)", name, str, x)
				if strings.Contains(str, "(") {
					want = fmt.Sprintf("%s: %d (enum ", name, x)
				}
				if len(lines) != 1 || !strings.HasPrefix(lines[0], want) {
					t.Errorf("%s.%s = %d renders as %q, want %q", tc.name, sf.Name, x, lines, want)
				}
			}
		}
	}
}
