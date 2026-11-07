// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package fitid checks that bytes are a complete, valid FIT file (header, every record, CRC,
// chained files included) and reads its identity from the first file_id message. A sync uses it
// before storing a file pulled from a watch; an importer uses it to recognise duplicates.
package fitid

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
)

// Info identifies a FIT file.
type Info struct {
	// Type is the file_id type, e.g. typedef.FileMonitoringB (32).
	Type         typedef.File
	Manufacturer typedef.Manufacturer
	Product      uint16
	SerialNumber uint32
	// TimeCreated is zero when the file does not carry one.
	TimeCreated time.Time
	Number      uint16
	// Files is the number of chained FIT files, Messages the number of records in all of them.
	Files, Messages int
	ProfileVersion  uint16
}

// ErrNotFIT reports bytes that do not start with a FIT header.
var ErrNotFIT = errors.New("fitid: not a FIT file")

// Looks reports whether b starts with a FIT header (".FIT" at offset 8).
func Looks(b []byte) bool {
	return len(b) >= 12 && b[0] >= 12 && string(b[8:12]) == ".FIT"
}

// Inspect decodes the whole of b, checking every CRC, and returns the identity of its first
// file. Component expansion is off: only validity and identity matter here.
func Inspect(b []byte) (Info, error) {
	if !Looks(b) {
		return Info{}, ErrNotFIT
	}
	var info Info
	found := false
	dec := decoder.New(bytes.NewReader(b), decoder.WithNoComponentExpansion())
	for dec.Next() {
		fit, err := dec.Decode()
		if err != nil {
			return info, fmt.Errorf("fitid: file %d: %w", info.Files+1, err)
		}
		info.Files++
		info.Messages += len(fit.Messages)
		if info.Files == 1 {
			info.ProfileVersion = fit.FileHeader.ProfileVersion
		}
		if found {
			continue
		}
		for i := range fit.Messages {
			m := &fit.Messages[i]
			if m.Num != typedef.MesgNumFileId {
				continue
			}
			id := mesgdef.NewFileId(m)
			info.Type, info.Manufacturer, info.Product = id.Type, id.Manufacturer, id.Product
			info.SerialNumber, info.TimeCreated, info.Number = id.SerialNumber, id.TimeCreated, id.Number
			found = true
			break
		}
	}
	if info.Files == 0 {
		return info, errors.New("fitid: no FIT file in the data")
	}
	if !found {
		return info, errors.New("fitid: no file_id message")
	}
	return info, nil
}
