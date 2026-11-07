// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fitid_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/pkg/fitid"
	"github.com/karamble/goarmin/pkg/fitid/fittest"
)

func TestInspect(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	b := fittest.File(typedef.FileMonitoringB, 3_500_000_001, now, 50)
	info, err := fitid.Inspect(b)
	if err != nil {
		t.Fatal(err)
	}
	if info.Type != typedef.FileMonitoringB || info.SerialNumber != 3_500_000_001 || info.Manufacturer != typedef.ManufacturerGarmin ||
		info.Product != uint16(typedef.GarminProductFenix8) || !info.TimeCreated.Equal(now) || info.Files != 1 || info.Messages != 51 {
		t.Fatalf("info %+v", info)
	}

	// Chained files count; identity comes from the first.
	chained := append(slices.Clone(b), fittest.File(typedef.FileActivity, 7, now.Add(-time.Hour), 3)...)
	info, err = fitid.Inspect(chained)
	if err != nil || info.Files != 2 || info.Type != typedef.FileMonitoringB {
		t.Fatalf("chained: %+v, %v", info, err)
	}
}

func TestInspectRejects(t *testing.T) {
	b := fittest.File(typedef.FileMonitoringDaily, 1, time.Now(), 5)
	bad := slices.Clone(b)
	bad[len(bad)/2] ^= 0x40
	if _, err := fitid.Inspect(bad); err == nil {
		t.Error("corrupt record accepted")
	}
	if _, err := fitid.Inspect(b[:len(b)-1]); err == nil {
		t.Error("truncated file accepted")
	}
	if _, err := fitid.Inspect([]byte("<?xml version=\"1.0\"?><device/>")); !errors.Is(err, fitid.ErrNotFIT) {
		t.Errorf("XML: %v", err)
	}
	if fitid.Looks(b[:11]) || !fitid.Looks(b) {
		t.Error("Looks")
	}
}
