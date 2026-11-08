// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package fitnamed decodes FIT messages into names and values people can read: every message
// and field named from the FIT profile (pkg/fitprofile, community names included), numbers
// scaled into their units, times as times, enumerations by name. Messages and fields the
// profile does not know are kept by number. It serves inspecting a file and keeping messages
// no table reads as readable documents.
package fitnamed

import (
	"bytes"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/proto"

	"github.com/karamble/goarmin/pkg/fitprofile"
)

// garminEpoch is the FIT time origin, 1989-12-31 00:00 UTC, in Unix seconds.
const garminEpoch = 631065600

// Message is a FIT message with its fields named.
type Message struct {
	Num      uint16  `json:"num"`
	Name     string  `json:"name"`
	Known    bool    `json:"known"`    // the profile knows it
	Official bool    `json:"official"` // in Garmin's published profile
	Fields   []Field `json:"fields"`
}

// Field is one valid field of a message.
type Field struct {
	Num   uint8  `json:"num"`
	Name  string `json:"name"`
	Units string `json:"units,omitempty"`
	// Value is what the field means: a number in Units, a string, true or false, an
	// enumeration's name, a time (UTC, RFC 3339; a local time without a zone), or a list.
	Value any `json:"value"`
	// Raw is the stored number where Value is not it (scaled, a time, a name).
	Raw   any  `json:"raw,omitempty"`
	Known bool `json:"known"`
}

// ErrNotFIT is returned for bytes that are not a FIT file.
var ErrNotFIT = errors.New("fitnamed: not a FIT file")

// Decode names the messages of a FIT file (all its chained parts) for which keep is true
// (every message if keep is nil).
func Decode(b []byte, keep func(num uint16) bool) ([]Message, error) {
	if len(b) < 12 || string(b[8:12]) != ".FIT" {
		return nil, ErrNotFIT
	}
	var out []Message
	dec := decoder.New(bytes.NewReader(b), decoder.WithNoComponentExpansion())
	for dec.Next() {
		f, err := dec.Decode()
		if err != nil {
			return out, err
		}
		for i := range f.Messages {
			if keep == nil || keep(uint16(f.Messages[i].Num)) {
				out = append(out, FromProto(&f.Messages[i]))
			}
		}
	}
	return out, nil
}

// FromProto names one decoded message. Invalid fields (the FIT "no value") are left out.
func FromProto(m *proto.Message) Message {
	num := uint16(m.Num)
	pm, known := fitprofile.Lookup(num)
	out := Message{Num: num, Name: fitprofile.MessageName(num), Known: known, Official: pm.Official, Fields: []Field{}}
	for i := range m.Fields {
		f := &m.Fields[i]
		if f.IsExpandedField || !f.Value.Valid(f.BaseType) {
			continue
		}
		pf, fk := pm.Field(f.Num)
		nf := Field{Num: f.Num, Name: fitprofile.FieldName(num, f.Num), Units: pf.Units, Known: fk}
		nf.Value, nf.Raw = render(f.Value, pf)
		out.Fields = append(out.Fields, nf)
	}
	return out
}

// Get returns the value of the field called name.
func (m Message) Get(name string) (any, bool) {
	for _, f := range m.Fields {
		if f.Name == name {
			return f.Value, true
		}
	}
	return nil, false
}

// Map is the message as name → value, for a document; a name that repeats gets its number.
func (m Message) Map() map[string]any {
	out := make(map[string]any, len(m.Fields))
	for _, f := range m.Fields {
		k := f.Name
		if _, dup := out[k]; dup {
			k += "_" + strconv.Itoa(int(f.Num))
		}
		out[k] = f.Value
	}
	return out
}

// render turns a stored value into what it means, with the stored number where they differ.
func render(v proto.Value, pf fitprofile.Field) (value, raw any) {
	switch v.Type() {
	case proto.TypeString:
		return v.String(), nil
	case proto.TypeBool:
		return v.Bool() == 1, nil
	case proto.TypeSliceString:
		out := []any{}
		for _, s := range v.SliceString() {
			out = append(out, s)
		}
		return out, nil
	}
	if nums, ok := slice(v); ok {
		vals, raws := make([]any, len(nums)), make([]any, len(nums))
		differ := false
		for i, n := range nums {
			vals[i], raws[i] = scalar(n, pf)
			differ = differ || raws[i] != nil
		}
		if !differ {
			raws = nil
		}
		if raws == nil {
			return vals, nil
		}
		return vals, raws
	}
	n, ok := number(v)
	if !ok {
		return v.Any(), nil
	}
	return scalar(n, pf)
}

// num is a stored number: an integer, or a float for the float base types.
type num struct {
	i     int64
	f     float64
	float bool
}

func (n num) value() any {
	if n.float {
		return n.f
	}
	return n.i
}

// scalar renders one number by its field's profile type, scale and offset.
func scalar(n num, pf fitprofile.Field) (value, raw any) {
	if !n.float {
		switch pf.Type {
		case "date_time":
			if n.i >= 0x10000000 { // below: seconds since power-up, not a time
				return time.Unix(n.i+garminEpoch, 0).UTC().Format(time.RFC3339), n.i
			}
		case "local_date_time":
			if n.i >= 0x10000000 {
				return time.Unix(n.i+garminEpoch, 0).UTC().Format("2006-01-02T15:04:05"), n.i
			}
		}
		switch pf.Type {
		case "file":
			if n.i >= 0 && n.i < 256 {
				return fitprofile.FileTypeName(uint8(n.i)), n.i
			}
		case "garmin_product":
			if name := fitprofile.DeviceName(1, uint16(n.i)); name != "" && n.i >= 0 && n.i < 65536 {
				return name, n.i
			}
		}
		if e, ok := fitprofile.LookupEnum(pf.Type); ok && n.i >= 0 {
			if name := e.Value(uint32(n.i)); name != "" {
				return name, n.i
			}
		}
	}
	if (pf.Scale != 0 && pf.Scale != 1) || pf.Offset != 0 {
		x := n.f
		if !n.float {
			x = float64(n.i)
		}
		return round(pf.Value(x)), n.value()
	}
	if n.float {
		return round(n.f), nil
	}
	return n.i, nil
}

// round keeps a scaled value to what its scale can carry, without float noise.
func round(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0
	}
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'g', 10, 64), 64)
	return v
}

func number(v proto.Value) (num, bool) {
	switch v.Type() {
	case proto.TypeInt8:
		return num{i: int64(v.Int8())}, true
	case proto.TypeUint8:
		return num{i: int64(v.Uint8())}, true
	case proto.TypeInt16:
		return num{i: int64(v.Int16())}, true
	case proto.TypeUint16:
		return num{i: int64(v.Uint16())}, true
	case proto.TypeInt32:
		return num{i: int64(v.Int32())}, true
	case proto.TypeUint32:
		return num{i: int64(v.Uint32())}, true
	case proto.TypeInt64:
		return num{i: v.Int64()}, true
	case proto.TypeUint64:
		return num{i: int64(v.Uint64())}, true
	case proto.TypeFloat32:
		return num{f: float64(v.Float32()), float: true}, true
	case proto.TypeFloat64:
		return num{f: v.Float64(), float: true}, true
	}
	return num{}, false
}

// slice returns the valid elements of an array value (invalid ones left out).
func slice(v proto.Value) ([]num, bool) {
	var out []num
	add := func(n num, invalid bool) {
		if !invalid {
			out = append(out, n)
		}
	}
	switch v.Type() {
	case proto.TypeSliceInt8:
		for _, x := range v.SliceInt8() {
			add(num{i: int64(x)}, x == math.MaxInt8)
		}
	case proto.TypeSliceUint8:
		for _, x := range v.SliceUint8() {
			add(num{i: int64(x)}, x == math.MaxUint8)
		}
	case proto.TypeSliceInt16:
		for _, x := range v.SliceInt16() {
			add(num{i: int64(x)}, x == math.MaxInt16)
		}
	case proto.TypeSliceUint16:
		for _, x := range v.SliceUint16() {
			add(num{i: int64(x)}, x == math.MaxUint16)
		}
	case proto.TypeSliceInt32:
		for _, x := range v.SliceInt32() {
			add(num{i: int64(x)}, x == math.MaxInt32)
		}
	case proto.TypeSliceUint32:
		for _, x := range v.SliceUint32() {
			add(num{i: int64(x)}, x == math.MaxUint32)
		}
	case proto.TypeSliceInt64:
		for _, x := range v.SliceInt64() {
			add(num{i: x}, x == math.MaxInt64)
		}
	case proto.TypeSliceUint64:
		for _, x := range v.SliceUint64() {
			add(num{i: int64(x)}, x == math.MaxUint64)
		}
	case proto.TypeSliceFloat32:
		for _, x := range v.SliceFloat32() {
			add(num{f: float64(x), float: true}, math.Float32bits(x) == math.MaxUint32)
		}
	case proto.TypeSliceFloat64:
		for _, x := range v.SliceFloat64() {
			add(num{f: x, float: true}, math.Float64bits(x) == math.MaxUint64)
		}
	default:
		return nil, false
	}
	return out, true
}
