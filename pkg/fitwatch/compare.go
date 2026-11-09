// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fitwatch

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"
)

// Compare reports how two FIT files differ in what they say: message by message in order,
// field by field, with a field holding its invalid value counted as absent, and the file's
// identity (file_id serial number and time created) left out. Byte layout (definitions,
// endianness, field order) does not count. An empty result means they say the same.
func Compare(a, b []byte) ([]string, error) {
	ma, err := comparable(a)
	if err != nil {
		return nil, fmt.Errorf("fitwatch: compare: the first file: %w", err)
	}
	mb, err := comparable(b)
	if err != nil {
		return nil, fmt.Errorf("fitwatch: compare: the second file: %w", err)
	}
	var out []string
	for i := 0; i < max(len(ma), len(mb)); i++ {
		switch {
		case i >= len(ma):
			out = append(out, fmt.Sprintf("message %d: only the second file has %s", i, mb[i]))
			continue
		case i >= len(mb):
			out = append(out, fmt.Sprintf("message %d: only the first file has %s", i, ma[i]))
			continue
		}
		x, y := ma[i], mb[i]
		if x.num != y.num {
			out = append(out, fmt.Sprintf("message %d: %s, not %s", i, x, y))
			continue
		}
		for _, k := range unionKeys(x.fields, y.fields) {
			vx, okx := x.fields[k]
			vy, oky := y.fields[k]
			switch {
			case !okx:
				out = append(out, fmt.Sprintf("message %d (%s) field %d: only the second file has %s", i, x.name(), k, vy))
			case !oky:
				out = append(out, fmt.Sprintf("message %d (%s) field %d: only the first file has %s", i, x.name(), k, vx))
			case vx != vy:
				out = append(out, fmt.Sprintf("message %d (%s) field %d: %s, not %s", i, x.name(), k, vx, vy))
			}
		}
	}
	return out, nil
}

// cmpMesg is a message as Compare sees it: its number and its valid fields as text.
type cmpMesg struct {
	num    typedef.MesgNum
	fields map[byte]string
}

func (m cmpMesg) name() string { return m.num.String() }

func (m cmpMesg) String() string { return fmt.Sprintf("%s %v", m.name(), m.fields) }

func comparable(b []byte) ([]cmpMesg, error) {
	fit, err := decoder.New(bytes.NewReader(b), decoder.WithNoComponentExpansion()).Decode()
	if err != nil {
		return nil, err
	}
	var out []cmpMesg
	for _, m := range fit.Messages {
		c := cmpMesg{num: m.Num, fields: map[byte]string{}}
		for _, f := range m.Fields {
			if !f.Value.Valid(f.BaseType) {
				continue
			}
			if m.Num == typedef.MesgNumFileId && (f.Num == 3 || f.Num == 4) {
				continue // the file's identity
			}
			c.fields[f.Num] = valueText(f.Value)
		}
		out = append(out, c)
	}
	return out, nil
}

// valueText is a value as Compare compares it; text up to its terminating zero.
func valueText(v proto.Value) string {
	switch v.Type() {
	case proto.TypeString:
		return fmt.Sprintf("%q", v.String())
	case proto.TypeSliceString:
		return fmt.Sprintf("%q", v.SliceString())
	}
	return fmt.Sprint(v.Any())
}

func unionKeys(a, b map[byte]string) []byte {
	var keys []byte
	for k := range a {
		keys = append(keys, k)
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}
