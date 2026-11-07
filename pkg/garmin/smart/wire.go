// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import (
	"errors"
	"fmt"
	"math"

	"google.golang.org/protobuf/encoding/protowire"
)

// ErrWireType is wrapped by decode errors for a known field that arrived with a wire type its
// proto type cannot have, for example a varint where a UUID message is expected.
var ErrWireType = errors.New("wrong wire type")

// ErrEnvelope is wrapped by ParseEnvelope errors for input that is not exactly one
// length-delimited Smart service field.
var ErrEnvelope = errors.New("not a single Smart service field")

// U32 returns a pointer to v, for optional uint32 fields.
func U32(v uint32) *uint32 { return &v }

// U64 returns a pointer to v, for optional uint64 and fixed64 fields.
func U64(v uint64) *uint64 { return &v }

// S returns a pointer to v, for optional string fields.
func S(v string) *string { return &v }

// I32 returns a pointer to v, for optional sint32 fields.
func I32(v int32) *int32 { return &v }

// B returns a pointer to v, for optional bool fields.
func B(v bool) *bool { return &v }

// Ptr returns a pointer to v, for optional enum and message fields.
func Ptr[T any](v T) *T { return &v }

// field is one decoded field record.
type field struct {
	num  protowire.Number
	typ  protowire.Type
	u    uint64 // value of a varint, fixed32 or fixed64 field
	data []byte // contents of a length-delimited field (aliases the input)
}

func wireTypeName(t protowire.Type) string {
	switch t {
	case protowire.VarintType:
		return "varint"
	case protowire.Fixed32Type:
		return "fixed32"
	case protowire.Fixed64Type:
		return "fixed64"
	case protowire.BytesType:
		return "length-delimited"
	case protowire.StartGroupType:
		return "group"
	case protowire.EndGroupType:
		return "end-group"
	}
	return fmt.Sprintf("wire type %d", t)
}

func (f *field) want(t protowire.Type) error {
	if f.typ != t {
		return fmt.Errorf("%w: %s, want %s", ErrWireType, wireTypeName(f.typ), wireTypeName(t))
	}
	return nil
}

// walk decodes the field records of one message named msg. fn decodes a field and reports
// whether it knows it; the raw bytes of every field it does not know are appended to *unknown
// (dropped when unknown is nil). Errors carry the message name and field number.
func walk(msg string, b []byte, unknown *[]byte, fn func(f *field) (bool, error)) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return fmt.Errorf("%s: %w", msg, protowire.ParseError(n))
		}
		m := protowire.ConsumeFieldValue(num, typ, b[n:])
		if m < 0 {
			return fmt.Errorf("%s field %d: %w", msg, num, protowire.ParseError(m))
		}
		f := field{num: num, typ: typ}
		v := b[n : n+m]
		switch typ {
		case protowire.VarintType:
			f.u, _ = protowire.ConsumeVarint(v)
		case protowire.Fixed32Type:
			x, _ := protowire.ConsumeFixed32(v)
			f.u = uint64(x)
		case protowire.Fixed64Type:
			f.u, _ = protowire.ConsumeFixed64(v)
		case protowire.BytesType:
			f.data, _ = protowire.ConsumeBytes(v)
		}
		known, err := fn(&f)
		if err != nil {
			return fmt.Errorf("%s field %d: %w", msg, num, err)
		}
		if !known && unknown != nil {
			*unknown = append(*unknown, b[:n+m]...)
		}
		b = b[n+m:]
	}
	return nil
}

// unmarshaler is implemented by pointers to every message type.
type unmarshaler[T any] interface {
	*T
	unmarshal(b []byte) error
}

// parse decodes b into a fresh T and returns the zero T on error.
func parse[T any, PT unmarshaler[T]](b []byte) (T, error) {
	var m T
	if err := PT(&m).unmarshal(b); err != nil {
		var zero T
		return zero, fmt.Errorf("smart: %w", err)
	}
	return m, nil
}

// Field decoders. Each checks the wire type and stores the value; a scalar that occurs twice
// keeps the last value, a message that occurs twice is merged, repeated fields append.

func decU32(dst **uint32, f *field) error {
	if err := f.want(protowire.VarintType); err != nil {
		return err
	}
	v := uint32(f.u)
	*dst = &v
	return nil
}

func decU64(dst **uint64, f *field) error {
	if err := f.want(protowire.VarintType); err != nil {
		return err
	}
	v := f.u
	*dst = &v
	return nil
}

func decS32(dst **int32, f *field) error {
	if err := f.want(protowire.VarintType); err != nil {
		return err
	}
	v := int32(protowire.DecodeZigZag(f.u & math.MaxUint32))
	*dst = &v
	return nil
}

func decBool(dst **bool, f *field) error {
	if err := f.want(protowire.VarintType); err != nil {
		return err
	}
	v := protowire.DecodeBool(f.u)
	*dst = &v
	return nil
}

func decFixed64(dst **uint64, f *field) error {
	if err := f.want(protowire.Fixed64Type); err != nil {
		return err
	}
	v := f.u
	*dst = &v
	return nil
}

func decString(dst **string, f *field) error {
	if err := f.want(protowire.BytesType); err != nil {
		return err
	}
	v := string(f.data)
	*dst = &v
	return nil
}

func decStrings(dst *[]string, f *field) error {
	if err := f.want(protowire.BytesType); err != nil {
		return err
	}
	*dst = append(*dst, string(f.data))
	return nil
}

func decEnum[E ~uint32](dst **E, f *field) error {
	if err := f.want(protowire.VarintType); err != nil {
		return err
	}
	v := E(f.u)
	*dst = &v
	return nil
}

// decEnums accepts a repeated enum unpacked (one varint per record, as Garmin sends it) or
// packed (a length-delimited run of varints).
func decEnums[E ~uint32](dst *[]E, f *field) error {
	switch f.typ {
	case protowire.VarintType:
		*dst = append(*dst, E(f.u))
		return nil
	case protowire.BytesType:
		for b := f.data; len(b) > 0; {
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return fmt.Errorf("packed: %w", protowire.ParseError(n))
			}
			*dst = append(*dst, E(v))
			b = b[n:]
		}
		return nil
	}
	return f.want(protowire.VarintType)
}

func decMsg[T any, PT unmarshaler[T]](dst **T, f *field) error {
	if err := f.want(protowire.BytesType); err != nil {
		return err
	}
	if *dst == nil {
		*dst = new(T)
	}
	return PT(*dst).unmarshal(f.data)
}

func decMsgs[T any, PT unmarshaler[T]](dst *[]T, f *field) error {
	if err := f.want(protowire.BytesType); err != nil {
		return err
	}
	var v T
	if err := PT(&v).unmarshal(f.data); err != nil {
		return err
	}
	*dst = append(*dst, v)
	return nil
}

// Field encoders. A nil pointer emits nothing; a pointer to a zero value is emitted.

func encVarint(b []byte, num protowire.Number, v uint64) []byte {
	b = protowire.AppendTag(b, num, protowire.VarintType)
	return protowire.AppendVarint(b, v)
}

func encLen(b []byte, num protowire.Number, data []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, data)
}

func encFixed64v(b []byte, num protowire.Number, v uint64) []byte {
	b = protowire.AppendTag(b, num, protowire.Fixed64Type)
	return protowire.AppendFixed64(b, v)
}

func encU32(b []byte, num protowire.Number, v *uint32) []byte {
	if v == nil {
		return b
	}
	return encVarint(b, num, uint64(*v))
}

func encU64(b []byte, num protowire.Number, v *uint64) []byte {
	if v == nil {
		return b
	}
	return encVarint(b, num, *v)
}

func encS32(b []byte, num protowire.Number, v *int32) []byte {
	if v == nil {
		return b
	}
	return encVarint(b, num, protowire.EncodeZigZag(int64(*v)))
}

func encBool(b []byte, num protowire.Number, v *bool) []byte {
	if v == nil {
		return b
	}
	return encVarint(b, num, protowire.EncodeBool(*v))
}

func encFixed64(b []byte, num protowire.Number, v *uint64) []byte {
	if v == nil {
		return b
	}
	return encFixed64v(b, num, *v)
}

func encString(b []byte, num protowire.Number, v *string) []byte {
	if v == nil {
		return b
	}
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendString(b, *v)
}

func encStrings(b []byte, num protowire.Number, vs []string) []byte {
	for i := range vs {
		b = encString(b, num, &vs[i])
	}
	return b
}

func encEnum[E ~uint32](b []byte, num protowire.Number, v *E) []byte {
	if v == nil {
		return b
	}
	return encVarint(b, num, uint64(*v))
}

func encEnums[E ~uint32](b []byte, num protowire.Number, vs []E) []byte {
	for _, v := range vs {
		b = encVarint(b, num, uint64(v))
	}
	return b
}

type marshaler interface{ Marshal() []byte }

func encMsg[T any, PT interface {
	*T
	marshaler
}](b []byte, num protowire.Number, m PT) []byte {
	if m == nil {
		return b
	}
	return encLen(b, num, m.Marshal())
}

func encMsgs[T marshaler](b []byte, num protowire.Number, ms []T) []byte {
	for _, m := range ms {
		b = encLen(b, num, m.Marshal())
	}
	return b
}

// fieldNumbers lists the field numbers of the records in b, for log text; it stops at the
// first malformed record.
func fieldNumbers(b []byte) []int {
	var nums []int
	for len(b) > 0 {
		num, _, n := protowire.ConsumeField(b)
		if n < 0 {
			break
		}
		nums = append(nums, int(num))
		b = b[n:]
	}
	return nums
}

// hasField reports whether the well-formed message b has a length-delimited field num.
func hasField(b []byte, num protowire.Number) bool {
	found := false
	err := walk("", b, nil, func(f *field) (bool, error) {
		found = found || (f.num == num && f.typ == protowire.BytesType)
		return true, nil
	})
	return err == nil && found
}
