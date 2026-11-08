// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSnake(t *testing.T) {
	for in, want := range map[string]string{
		"ECG_SUMMARY": "ecg_summary", "SleepLevel": "sleep_level", "HRVStatus": "hrv_status", "file_id": "file_id",
		"DateTime": "date_time", "FILE_TYPE": "file_type", "Uint8": "uint8", "LocalDateTime": "local_date_time",
		"skin temp": "skin_temp", "ActivitiyType": "activitiy_type", "": "",
	} {
		if got := snake(in); got != want {
			t.Errorf("snake(%q) = %q, want %q", in, got, want)
		}
	}
	if enumName("ActivitiyType") != "activity_type" {
		t.Fatal(enumName("ActivitiyType"))
	}
	if unit("BEATS_PER_MINUTE") != "bpm" || unit("SECONDS") != "s" || unit("watts") != "watts" {
		t.Fatal("units")
	}
	if words("running_track") != "Running track" || words("") != "" {
		t.Fatal("words")
	}
}

// The committed tables must be what the generator writes from the profiles now: a hand edit or
// a forgotten regeneration fails here. Needs the Gadgetbridge clone (GADGETBRIDGE, or where the
// clones live by convention); without it the test is skipped.
func TestGeneratedFilesUpToDate(t *testing.T) {
	gb := os.Getenv("GADGETBRIDGE")
	if gb == "" {
		home, _ := os.UserHomeDir()
		gb = filepath.Join(home, "go", "src", "codeberg.org", "Freeyourgadget", "Gadgetbridge")
	}
	if _, err := os.Stat(filepath.Join(gb, "FitCodeGenerator")); err != nil {
		t.Skip("no Gadgetbridge clone at", gb)
	}
	p, err := Build(gb)
	if err != nil {
		t.Fatal(err)
	}
	goSrc, err := p.Go()
	if err != nil {
		t.Fatal(err)
	}
	for file, want := range map[string][]byte{"../../pkg/fitprofile/profile_gen.go": goSrc, "../../web/src/lib/fitTypes.gen.ts": p.TS()} {
		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is out of date: run make fitprofile", file)
		}
	}
}
