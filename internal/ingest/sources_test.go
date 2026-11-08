// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package ingest

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/zipwalk"
)

func zipOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, data := range files {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestImportZipAndExport(t *testing.T) {
	ctx := context.Background()
	in := newIngester(t)
	u, _ := in.Store.EnsureUser(ctx, "a@example.invalid", "")
	base := day6()
	mon := monitoringFile(base)
	act := activityFile(base.Add(-30 * time.Hour))
	dir := t.TempDir()

	// A zipped USB copy: its FIT files come in, its other files stay out.
	usb := write(t, dir, "usb-copy.zip", zipOf(t, map[string][]byte{
		"GARMIN/Monitor/M1.FIT": mon,
		"GARMIN/notes.txt":      []byte("hello"),
	}))
	rep, err := in.Import(ctx, u, store.SourceUSB, []string{usb})
	if err != nil || rep.Seen != 1 || rep.New != 1 || rep.Ingested != 1 || rep.Other != 1 || rep.Archives != 1 || rep.Export {
		t.Fatalf("usb zip: %+v, %v", rep, err)
	}

	// A Garmin Connect export: FIT files from its nested archives, its JSON kept.
	part := zipOf(t, map[string][]byte{"a@example.invalid_1.fit": act, "a@example.invalid_2.fit": mon})
	export := write(t, dir, "export.zip", zipOf(t, map[string][]byte{
		"DI_CONNECT/DI-Connect-Fitness/a_summarizedActivities.json":       []byte(`[{"summarizedActivitiesExport":[]}]`),
		"DI_CONNECT/DI-Connect-Wellness/a_sleepData.json":                 []byte(`[{"calendarDate":"x"}]`),
		"DI_CONNECT/DI-Connect-Uploaded-Files/UploadedFiles_0-_Part1.zip": part,
		"DI_CONNECT/DI-Connect-Uploaded-Files/broken.zip":                 []byte("not a zip"),
		"INREACH/inreach_messages.csv":                                    []byte("a,b"),
		"README.txt":                                                      []byte("read me"),
	}))
	// The archive is an export as a whole: its files outside DI_CONNECT are kept too.
	want := map[string]int{"DI-Connect-Fitness": 1, "DI-Connect-Wellness": 1, "INREACH": 1, "Top level": 1}
	rep, err = in.Import(ctx, u, store.SourceUpload, []string{export})
	if err != nil || !rep.Export || rep.Seen != 2 || rep.New != 1 || rep.Known != 1 || rep.Kept != 4 || rep.Other != 0 || rep.Read != 7 || rep.Current != "" ||
		rep.Failed != 1 || rep.Archives != 2 || !reflect.DeepEqual(rep.KeptBy, want) {
		t.Fatalf("export: %+v, %v", rep, err)
	}
	if len(rep.Errors) != 1 || !strings.Contains(rep.Errors[0], "broken.zip") {
		t.Fatalf("errors %q", rep.Errors)
	}
	f, err := in.Store.RawFileBySHA(ctx, digest(act))
	if err != nil || f.Source != store.SourceGCExport || f.SourceName != "a@example.invalid_1.fit" || f.FIT == nil {
		t.Fatalf("activity raw file %+v, %v", f, err)
	}
	j, err := in.Store.RawFileBySHA(ctx, digest([]byte(`[{"calendarDate":"x"}]`)))
	if err != nil || j.Source != store.SourceGCExport || j.FIT != nil || j.SourceName != "export.zip/DI_CONNECT/DI-Connect-Wellness/a_sleepData.json" {
		t.Fatalf("kept json %+v, %v", j, err)
	}

	// Importing it again changes nothing.
	rep, err = in.Import(ctx, u, store.SourceUpload, []string{export})
	if err != nil || rep.New != 0 || rep.Kept != 0 || rep.Known != 2 || !reflect.DeepEqual(rep.KeptBy, want) {
		t.Fatalf("again: %+v, %v", rep, err)
	}
}

func TestImportFolderNames(t *testing.T) {
	ctx := context.Background()
	in := newIngester(t)
	u, _ := in.Store.EnsureUser(ctx, "a@example.invalid", "")
	dir := t.TempDir()
	write(t, dir, "GARMIN/Monitor/M1.FIT", monitoringFile(day6()))
	var names []string
	rep, err := in.ImportFrom(ctx, u, Paths([]string{filepath.Join(dir, "GARMIN")}, nil), ImportOptions{
		Source: store.SourceUSB,
		Stored: func(e Entry, sha string) { names = append(names, e.Path+" "+sha[:8]) },
	})
	if err != nil || rep.New != 1 || len(names) != 1 || names[0] != "GARMIN/Monitor/M1.FIT "+digest(monitoringFile(day6()))[:8] {
		t.Fatalf("%+v, %q, %v", rep, names, err)
	}
	if _, err := in.ImportFrom(ctx, u, Paths([]string{filepath.Join(dir, "missing")}, nil), ImportOptions{}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing path: %v", err)
	}
}

func TestImportFromSkipAndProgress(t *testing.T) {
	ctx := context.Background()
	in := newIngester(t)
	u, _ := in.Store.EnsureUser(ctx, "a@example.invalid", "")
	mon := monitoringFile(day6())
	src := func(ctx context.Context, fn func(Entry) error) error {
		for _, e := range []Entry{
			{Path: "GARMIN/Monitor/old.fit", Size: 10, Read: func() ([]byte, error) { t.Fatal("read a skipped file"); return nil, nil }},
			{Path: "GARMIN/Monitor/new.fit", Size: int64(len(mon)), Read: func() ([]byte, error) { return mon, nil }},
			{Path: "GARMIN/Monitor/gone.fit", Size: 5, Read: func() ([]byte, error) { return nil, errors.New("unplugged") }},
			{Path: "GARMIN/big.zip", Size: -1, Err: zipwalk.ErrTooLarge},
		} {
			if err := fn(e); err != nil {
				return err
			}
		}
		return nil
	}
	progress := 0
	rep, err := in.ImportFrom(ctx, u, src, ImportOptions{
		Source:   store.SourceUSB,
		Skip:     func(e Entry) bool { return strings.HasSuffix(e.Path, "old.fit") },
		Progress: func(Report) { progress++ },
	})
	if err != nil || rep.Seen != 3 || rep.Unchanged != 1 || rep.New != 1 || rep.Failed != 2 || progress != 4 || rep.Bytes != int64(len(mon)) {
		t.Fatalf("%+v, progress %d, %v", rep, progress, err)
	}
	if len(rep.Watches) != 1 {
		t.Fatalf("watches %v", rep.Watches)
	}
	for _, n := range rep.Watches {
		if n != 1 {
			t.Fatalf("watches %v", rep.Watches)
		}
	}

	// A canceled import stops.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := in.ImportFrom(cctx, u, src, ImportOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
}

func TestStagedNames(t *testing.T) {
	ctx := context.Background()
	in := newIngester(t)
	u, _ := in.Store.EnsureUser(ctx, "a@example.invalid", "")
	dir := t.TempDir()
	disk := write(t, dir, "000001", monitoringFile(day6()))
	rep, err := in.ImportFrom(ctx, u, Staged([]StagedFile{{Disk: disk, Name: "GARMIN/Monitor/M9.fit"}}, nil), ImportOptions{Source: store.SourceUpload})
	if err != nil || rep.New != 1 {
		t.Fatalf("%+v, %v", rep, err)
	}
	if f, _ := in.Store.RawFileBySHA(ctx, digest(monitoringFile(day6()))); f.SourceName != "M9.fit" || f.Source != store.SourceUpload {
		t.Fatalf("%+v", f)
	}
}

func TestExportPaths(t *testing.T) {
	for p, folder := range map[string]string{
		"export.zip/DI_CONNECT/DI-Connect-Fitness/x.json": "DI-Connect-Fitness",
		"DI_CONNECT/DI-Connect-User/user_profile.json":    "DI-Connect-User",
		"DI_CONNECT/Other/x.json":                         "Other",
		"DI_CONNECT/x.json":                               "DI_CONNECT",
		"export.zip/INREACH/billing.json":                 "INREACH",
		"export.zip/README.txt":                           "Top level",
	} {
		if ExportFolder(p) != folder || (!IsExportPath(p) && !strings.HasPrefix(p, "export.zip/")) {
			t.Errorf("%s: export %v, folder %q", p, IsExportPath(p), ExportFolder(p))
		}
	}
	for _, p := range []string{"GARMIN/Activity/x.fit", "README.txt", "DI_CONNECTED/x"} {
		if IsExportPath(p) {
			t.Errorf("%s counted as an export", p)
		}
	}
}
