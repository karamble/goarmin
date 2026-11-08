// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestImports(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	now := time.Now()
	st.SetClock(func() time.Time { return now })
	a, b := person(t, st, "a@example.invalid"), person(t, st, "b@example.invalid")
	dev, err := st.EnsureDevice(ctx, Device{UnitID: 7})
	if err != nil {
		t.Fatal(err)
	}
	up, err := st.CreateImport(ctx, Import{UserID: a.ID, Kind: ImportUpload, Source: SourceUpload, Name: "3 files", Status: ImportUploading})
	if err != nil || up.ID == 0 || up.CreatedAt != now.Unix() || up.StartedBy != "manual" {
		t.Fatalf("%+v, %v", up, err)
	}
	usb, err := st.CreateImport(ctx, Import{UserID: a.ID, Kind: ImportUSB, Source: SourceUSB, DeviceID: dev.ID, StartedBy: "plugged", Status: ImportQueued})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateImport(ctx, Import{UserID: b.ID, Kind: ImportUpload, Source: SourceUpload, Status: ImportQueued}); err != nil {
		t.Fatal(err)
	}

	now = now.Add(time.Minute)
	if err := st.SetImportStatus(ctx, usb.ID, ImportRunning); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveImportReport(ctx, usb.ID, []byte(`{"seen":3}`)); err != nil {
		t.Fatal(err)
	}
	got, err := st.Import(ctx, a.ID, usb.ID)
	if err != nil || got.Status != ImportRunning || got.StartedAt != now.Unix() || string(got.Report) != `{"seen":3}` || got.DeviceID != dev.ID || got.Finished() {
		t.Fatalf("%+v, %v", got, err)
	}
	now = now.Add(time.Minute)
	if err := st.FinishImport(ctx, usb.ID, ImportDone, []byte(`{"seen":4}`), ""); err != nil {
		t.Fatal(err)
	}
	// A finished import stays finished.
	if err := st.SetImportStatus(ctx, usb.ID, ImportRunning); err != nil {
		t.Fatal(err)
	}
	if got, _ = st.Import(ctx, a.ID, usb.ID); got.Status != ImportDone || got.FinishedAt != now.Unix() || !got.Finished() {
		t.Fatalf("%+v", got)
	}
	if last, err := st.LastDeviceImport(ctx, a.ID, dev.ID); err != nil || last.ID != usb.ID {
		t.Fatalf("last %+v, %v", last, err)
	}
	if _, err := st.LastDeviceImport(ctx, b.ID, dev.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("b's last: %v", err)
	}

	// Another account's import is not found; the list is the account's own, newest first.
	if _, err := st.Import(ctx, b.ID, usb.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("b reads a's import: %v", err)
	}
	list, err := st.Imports(ctx, a.ID, 0, 10)
	if err != nil || len(list) != 2 || list[0].ID != usb.ID || list[1].ID != up.ID {
		t.Fatalf("list %+v, %v", list, err)
	}
	if older, _ := st.Imports(ctx, a.ID, usb.ID, 10); len(older) != 1 || older[0].ID != up.ID {
		t.Fatalf("older %+v", older)
	}

	// After a restart nothing is left half done.
	ids, err := st.FailUnfinishedImports(ctx, "the server restarted")
	if err != nil || len(ids) != 2 {
		t.Fatalf("failed %v, %v", ids, err)
	}
	if got, _ = st.Import(ctx, a.ID, up.ID); got.Status != ImportFailed || got.Error != "the server restarted" {
		t.Fatalf("%+v", got)
	}
}

func TestUSBSeenAndSerial(t *testing.T) {
	ctx := context.Background()
	st := openTest(t)
	mod := time.Now().Add(-time.Hour).Unix()
	if _, err := st.USBSeenFile(ctx, "S1", "GARMIN/Activity/a.fit"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	for _, size := range []int64{100, 200} {
		if err := st.SetUSBSeen(ctx, "S1", "GARMIN/Activity/a.fit", USBSeen{Size: size, Modified: mod, SHA256: "ab"}); err != nil {
			t.Fatal(err)
		}
	}
	if u, err := st.USBSeenFile(ctx, "S1", "GARMIN/Activity/a.fit"); err != nil || u.Size != 200 || u.Modified != mod || u.SHA256 != "ab" {
		t.Fatalf("%+v, %v", u, err)
	}
	if _, err := st.USBSeenFile(ctx, "S2", "GARMIN/Activity/a.fit"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another watch: %v", err)
	}

	d1, _ := st.EnsureDevice(ctx, Device{UnitID: 1})
	d2, _ := st.EnsureDevice(ctx, Device{UnitID: 2})
	if !d1.USBAuto || d1.USBSerial != "" {
		t.Fatalf("defaults %+v", d1)
	}
	if err := st.SetUSBSerial(ctx, d1.ID, "S1"); err != nil {
		t.Fatal(err)
	}
	if d, err := st.DeviceByUSBSerial(ctx, "S1"); err != nil || d.ID != d1.ID {
		t.Fatalf("%+v, %v", d, err)
	}
	// The serial moves with the watch that reports it now.
	if err := st.SetUSBSerial(ctx, d2.ID, "S1"); err != nil {
		t.Fatal(err)
	}
	if d, _ := st.DeviceByUSBSerial(ctx, "S1"); d.ID != d2.ID {
		t.Fatalf("serial on %d", d.ID)
	}
	if d, _ := st.DeviceByUnit(ctx, 1); d.USBSerial != "" {
		t.Fatalf("d1 kept %q", d.USBSerial)
	}
	if _, err := st.DeviceByUSBSerial(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty serial: %v", err)
	}
	if err := st.SetUSBAuto(ctx, d2.ID, false); err != nil {
		t.Fatal(err)
	}
	if d, _ := st.DeviceByUnit(ctx, 2); d.USBAuto {
		t.Fatal("usb_auto still on")
	}
}
