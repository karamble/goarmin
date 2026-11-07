// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muktihari/fit/encoder"
	"github.com/muktihari/fit/profile"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"
)

// writeTestFIT builds a small monitoring file like the ones a Garmin watch writes: a file_id and a
// stress_level message that carries Body Battery in undocumented field 3.
func writeTestFIT(t *testing.T, path string, now time.Time, bodyBattery int8) {
	t.Helper()
	fileID := (&mesgdef.FileId{
		Type:         typedef.FileMonitoringB,
		Manufacturer: typedef.ManufacturerGarmin,
		Product:      uint16(typedef.GarminProductFenix8),
		SerialNumber: 1234,
		TimeCreated:  now,
	}).ToMesg(nil)

	stress := &mesgdef.StressLevel{
		StressLevelValue: 30,
		StressLevelTime:  now.Add(-time.Minute),
		UnknownFields: []proto.Field{{
			FieldBase: &proto.FieldBase{
				Name:     "unknown",
				Num:      3,
				Type:     profile.Sint8,
				BaseType: basetype.Sint8,
				Scale:    1,
			},
			Value: proto.Int8(bodyBattery),
		}},
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fit := &proto.FIT{Messages: []proto.Message{fileID, stress.ToMesg(nil)}}
	if err := encoder.New(f).Encode(fit); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

func TestInspectFindsUndocumentedBodyBattery(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	sub := filepath.Join(dir, "Monitor")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFIT(t, filepath.Join(sub, "a.fit"), now, 57)
	writeTestFIT(t, filepath.Join(sub, "b.FIT"), now.Add(-time.Hour), 12)
	if err := os.WriteFile(filepath.Join(sub, "notes.txt"), []byte("not a FIT file"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &report{mesgs: map[typedef.MesgNum]*mesgStat{}, devices: map[string]int{}}
	if err := r.walk(dir); err != nil {
		t.Fatal(err)
	}

	if len(r.files) != 2 {
		t.Fatalf("inspected %d files, want 2 (.fit and .FIT, not .txt)", len(r.files))
	}
	for _, f := range r.files {
		if f.err != nil {
			t.Fatalf("%s: decode error: %v", f.path, f.err)
		}
		if f.folder != "Monitor" {
			t.Errorf("%s: folder %q, want Monitor", f.path, f.folder)
		}
		if f.fileType != typedef.FileMonitoringB.String() {
			t.Errorf("%s: file type %q, want %q", f.path, f.fileType, typedef.FileMonitoringB.String())
		}
		if !strings.Contains(f.device, "fenix8") {
			t.Errorf("%s: device %q, want it to name the fenix8", f.path, f.device)
		}
	}

	s := r.mesgs[typedef.MesgNumStressLevel]
	if s == nil || s.count != 2 {
		t.Fatalf("stress_level messages: %+v, want 2", s)
	}
	bb := s.fields[3]
	if bb == nil || !bb.unknown {
		t.Fatalf("field 3 of stress_level: %+v, want an undocumented field", bb)
	}
	if !bb.numeric || bb.min != 12 || bb.max != 57 {
		t.Errorf("field 3 range: numeric=%v min=%v max=%v, want 12..57", bb.numeric, bb.min, bb.max)
	}
	if known := s.fields[0]; known == nil || known.unknown || known.name != "stress_level_value" {
		t.Errorf("field 0: %+v, want documented stress_level_value", known)
	}
}

func TestInspectReportsDecodeErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.fit"), []byte("definitely not FIT"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &report{mesgs: map[typedef.MesgNum]*mesgStat{}, devices: map[string]int{}}
	if err := r.walk(dir); err != nil {
		t.Fatal(err)
	}
	if len(r.files) != 1 || r.files[0].err == nil {
		t.Fatalf("want one file with a decode error, got %+v", r.files)
	}
}
