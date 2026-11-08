// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package fittest builds small, valid FIT files for tests. Callers pass the time, derived from
// time.Now(), so no test carries a fixed date.
package fittest

import (
	"bytes"
	"time"

	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"
)

// File returns a FIT file of the given type from a Fenix 8 with serial: a file_id created at
// created, then records stress_level messages, one a minute before created. It panics if the
// encoder fails, which only a bug can cause.
func File(typ typedef.File, serial uint32, created time.Time, records int) []byte {
	msgs := []proto.Message{(&mesgdef.FileId{
		Type:         typ,
		Manufacturer: typedef.ManufacturerGarmin,
		Product:      uint16(typedef.GarminProductFenix8),
		SerialNumber: serial,
		TimeCreated:  created,
	}).ToMesg(nil)}
	for i := range records {
		msgs = append(msgs, (&mesgdef.StressLevel{
			StressLevelValue: int16(20 + i%60),
			StressLevelTime:  created.Add(time.Duration(i-records) * time.Minute),
		}).ToMesg(nil))
	}
	var buf bytes.Buffer
	if err := encoder.New(&buf).Encode(&proto.FIT{Messages: msgs}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// F is one raw field of a message built with Message.
type F struct {
	Num   byte
	Value proto.Value
}

// Message builds a message of any number, documented or not, from raw field values. Each
// field's base type follows its value's Go type.
func Message(num typedef.MesgNum, fields ...F) proto.Message {
	m := proto.Message{Num: num}
	for _, f := range fields {
		m.Fields = append(m.Fields, proto.Field{
			FieldBase: &proto.FieldBase{Num: f.Num, BaseType: baseType(f.Value), Scale: 1, Array: isSlice(f.Value)},
			Value:     f.Value,
		})
	}
	return m
}

func baseType(v proto.Value) basetype.BaseType {
	switch v.Type() {
	case proto.TypeInt8:
		return basetype.Sint8
	case proto.TypeUint8, proto.TypeSliceUint8:
		return basetype.Uint8
	case proto.TypeInt16:
		return basetype.Sint16
	case proto.TypeUint16, proto.TypeSliceUint16:
		return basetype.Uint16
	case proto.TypeInt32:
		return basetype.Sint32
	case proto.TypeUint32, proto.TypeSliceUint32:
		return basetype.Uint32
	case proto.TypeString:
		return basetype.String
	case proto.TypeFloat32, proto.TypeSliceFloat32:
		return basetype.Float32
	}
	panic("fittest: unsupported value type " + v.Type().String())
}

func isSlice(v proto.Value) bool {
	switch v.Type() {
	case proto.TypeSliceUint8, proto.TypeSliceUint16, proto.TypeSliceUint32, proto.TypeSliceFloat32:
		return true
	}
	return false
}

// Build returns a FIT file: a Fenix 8 file_id of type typ created at created, then msgs. It
// panics if the encoder fails, which only a bug can cause.
func Build(typ typedef.File, serial uint32, created time.Time, msgs ...proto.Message) []byte {
	all := []proto.Message{(&mesgdef.FileId{
		Type:         typ,
		Manufacturer: typedef.ManufacturerGarmin,
		Product:      uint16(typedef.GarminProductFenix8),
		SerialNumber: serial,
		TimeCreated:  created,
	}).ToMesg(nil)}
	all = append(all, msgs...)
	var buf bytes.Buffer
	if err := encoder.New(&buf).Encode(&proto.FIT{Messages: all}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// FITTime is t as a FIT timestamp: seconds since 1989-12-31 00:00 UTC.
func FITTime(t time.Time) uint32 { return uint32(t.Unix() - 631065600) }
