// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// Hand-built wire records, so decode tests do not depend on the encoder under test.

func vfield(num int, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(nil, protowire.Number(num), protowire.VarintType), v)
}

func xfield(num int, v uint64) []byte {
	return protowire.AppendFixed64(protowire.AppendTag(nil, protowire.Number(num), protowire.Fixed64Type), v)
}

func lfield(num int, parts ...[]byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, protowire.Number(num), protowire.BytesType), cat(parts...))
}

func sfield(num int, s string) []byte { return lfield(num, []byte(s)) }

func cat(parts ...[]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

func uuidb(msb, lsb uint64) []byte { return cat(xfield(1, msb), xfield(2, lsb)) }

// unknownFields is one record of every wire type with field numbers no message defines.
func unknownFields() []byte {
	group := cat(protowire.AppendTag(nil, 102, protowire.StartGroupType), vfield(1, 5),
		protowire.AppendTag(nil, 102, protowire.EndGroupType))
	fixed32 := protowire.AppendFixed32(protowire.AppendTag(nil, 100, protowire.Fixed32Type), 0xDEADBEEF)
	return cat(vfield(99, 300), fixed32, xfield(103, 0x0102030405060708), sfield(101, "abc"), group)
}

// hexb parses "DA 02 2A ..." into bytes.
func hexb(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

// hexs formats bytes as "DA 02 2A ...".
func hexs(b []byte) string { return strings.ToUpper(fmt.Sprintf("% x", b)) }

func wantBytes(t *testing.T, what string, got []byte, want string) {
	t.Helper()
	if g := hexs(got); g != strings.ToUpper(want) {
		t.Errorf("%s\n got  %s\n want %s", what, g, strings.ToUpper(want))
	}
}

// codecCase describes one message type: its Go zero value, its proto full name and its
// parser. All tests that cover "every message type" iterate over codecCases.
type codecCase struct {
	name  string
	proto string
	zero  any
	parse func([]byte) (any, error)
}

func newCase[T marshaler](proto string, parse func([]byte) (T, error)) codecCase {
	var zero T
	return codecCase{
		name:  reflect.TypeOf(zero).Name(),
		proto: proto,
		zero:  zero,
		parse: func(b []byte) (any, error) { v, err := parse(b); return v, err },
	}
}

const fa = "GDI.Proto.FileAccess."

func codecCases() []codecCase {
	return []codecCase{
		newCase("GDI.Proto.DataTypes.UUID", ParseUUID),
		newCase(fa+"DataType", ParseDataType),
		newCase(fa+"ItemReference", ParseItemReference),
		newCase(fa+"Capabilities", ParseCapabilities),
		newCase(fa+"ItemListRequest", ParseItemListRequest),
		newCase(fa+"ItemListResponse", ParseItemListResponse),
		newCase(fa+"ItemListCancelNotification", ParseItemListCancelNotification),
		newCase(fa+"ItemAddedNotification", ParseItemAddedNotification),
		newCase(fa+"SyncButtonNotification", ParseSyncButtonNotification),
		newCase(fa+"PullItemDataRequest", ParsePullItemDataRequest),
		newCase(fa+"PullItemDataResponse", ParsePullItemDataResponse),
		newCase(fa+"PushItemDataRequest", ParsePushItemDataRequest),
		newCase(fa+"PushItemDataResponse", ParsePushItemDataResponse),
		newCase(fa+"TransferStatusRequest", ParseTransferStatusRequest),
		newCase(fa+"TransferStatusResponse", ParseTransferStatusResponse),
		newCase(fa+"PriorityUpdateRequest", ParsePriorityUpdateRequest),
		newCase(fa+"PriorityUpdateResponse", ParsePriorityUpdateResponse),
		newCase(fa+"CancelTransferRequest", ParseCancelTransferRequest),
		newCase(fa+"CancelTransferResponse", ParseCancelTransferResponse),
		newCase(fa+"ResourceUpdateNotification", ParseResourceUpdateNotification),
		newCase(fa+"ModifyFlagsRequest", ParseModifyFlagsRequest),
		newCase(fa+"ModifyFlagsResponse", ParseModifyFlagsResponse),
		newCase(fa+"ItemUpdatedNotification", ParseItemUpdatedNotification),
		newCase(fa+"DeleteItemRequest", ParseDeleteItemRequest),
		newCase(fa+"DeleteItemResponse", ParseDeleteItemResponse),
		newCase(fa+"GetItemChecksumRequest", ParseGetItemChecksumRequest),
		newCase(fa+"GetItemChecksumResponse", ParseGetItemChecksumResponse),
		newCase(fa+"SoftwareUpdatePartNumberRequest", ParseSoftwareUpdatePartNumberRequest),
		newCase(fa+"SoftwareUpdatePartNumberResponse", ParseSoftwareUpdatePartNumberResponse),
		newCase(fa+"Service", ParseFileAccess),
	}
}

// filler sets every exported field of a message, recursively. With zero set, every optional
// field points at a zero value and every repeated field holds one zero element, which checks
// that presence round-trips. Otherwise values are distinct and non-zero: uint64s exceed 32
// bits, sint32s are negative, enums take their highest defined value.
type filler struct {
	zero bool
	n    uint64
}

func (fl *filler) value(t reflect.Type) reflect.Value {
	v := reflect.New(t).Elem()
	fl.fill(v)
	return v
}

func (fl *filler) fill(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				fl.fill(v.Field(i))
			}
		}
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		fl.fill(p.Elem())
		v.Set(p)
	case reflect.Slice:
		k := 2
		if fl.zero {
			k = 1
		}
		s := reflect.MakeSlice(v.Type(), 0, k)
		for j := 0; j < k; j++ {
			e := reflect.New(v.Type().Elem()).Elem()
			fl.fill(e)
			s = reflect.Append(s, e)
		}
		v.Set(s)
	case reflect.Uint32:
		if fl.zero {
			return
		}
		if _, isEnum := v.Interface().(fmt.Stringer); isEnum {
			for x := uint64(63); x > 0; x-- {
				v.SetUint(x)
				if !strings.Contains(v.Interface().(fmt.Stringer).String(), "(") {
					return
				}
			}
			v.SetUint(0)
			return
		}
		fl.n++
		v.SetUint(fl.n)
	case reflect.Uint64:
		if !fl.zero {
			fl.n++
			v.SetUint(fl.n<<36 | fl.n)
		}
	case reflect.Int32:
		if !fl.zero {
			fl.n++
			v.SetInt(-int64(fl.n))
		}
	case reflect.String:
		if !fl.zero {
			fl.n++
			v.SetString(fmt.Sprintf("s%d", fl.n))
		}
	case reflect.Bool:
		v.SetBool(!fl.zero)
	default:
		panic("filler: unexpected kind " + v.Kind().String())
	}
}

func marshalAny(v any) []byte { return v.(marshaler).Marshal() }
