// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package usbsource

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/karamble/goarmin/pkg/mtp"
)

// tree is an MTP session over files in memory.
type tree struct {
	files  map[string][]byte // path → content
	claim  map[string]int64  // sizes the watch claims instead
	mod    time.Time
	closed bool
}

func (t *tree) Walk(ctx context.Context, root string, fn func(mtp.Entry) error) error {
	var paths []string
	for p := range t.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for i, p := range paths {
		if !strings.HasPrefix(strings.ToUpper(p), strings.ToUpper(root)+"/") {
			continue
		}
		size := int64(len(t.files[p]))
		if c, ok := t.claim[p]; ok {
			size = c
		}
		if err := fn(mtp.Entry{Handle: uint32(i + 1), Path: p, Name: filepath.Base(p), Size: size, Modified: t.mod}); err != nil {
			return err
		}
	}
	return nil
}

func (t *tree) Get(ctx context.Context, h uint32, w io.Writer) (int64, error) {
	var paths []string
	for p := range t.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	n, err := w.Write(t.files[paths[h-1]])
	return int64(n), err
}

func (t *tree) Close() error {
	t.closed = true
	return nil
}

func put(t *testing.T, p string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func finder(t *testing.T, open func(context.Context, mtp.Info) (Tree, error)) (*Finder, string, string) {
	gvfs, drives := t.TempDir(), t.TempDir()
	watch := mtp.Info{Path: "3-8", Bus: 3, Address: 7, Vendor: GarminVendor, Product: 0x4ff1, Manufacturer: "Garmin", ProductName: "fenix 8", Serial: "0000ABCDEF12"}
	old := mtp.Info{Path: "1-4", Bus: 1, Address: 5, Vendor: GarminVendor, Product: 0x2000}
	phone := mtp.Info{Path: "1-2", Vendor: 0x18d1, Product: 0x4ee1, Serial: "PHONE"}
	// The desktop has mounted both watches: the new GVfs naming and the old one.
	put(t, filepath.Join(gvfs, "mtp:host=Garmin_fenix_8_0000ABCDEF12", "Primary", "GARMIN", "Activity", "a.fit"), []byte("mounted a"))
	put(t, filepath.Join(gvfs, "mtp:host=%5Busb%3A001%2C005%5D", "Internal Storage", "Garmin", "Monitor", "m.fit"), []byte("old m"))
	put(t, filepath.Join(gvfs, "mtp:host=Phone_PHONE", "Internal", "DCIM", "x.jpg"), []byte("photo"))
	put(t, filepath.Join(gvfs, "sftp:host=example.invalid", "x"), []byte("x"))
	// A watch that is a USB drive, and a drive that is no watch.
	put(t, filepath.Join(drives, "FORERUNNER", "GARMIN", "ACTIVITY", "R.FIT"), []byte("drive r"))
	put(t, filepath.Join(drives, "STICK", "photos", "y.jpg"), []byte("y"))
	f := &Finder{
		Devices:    func() ([]mtp.Info, error) { return []mtp.Info{phone, watch, old}, nil },
		Open:       open,
		GVfsRoots:  []string{gvfs, filepath.Join(gvfs, "missing")},
		DriveRoots: []string{drives},
	}
	return f, gvfs, drives
}

func TestFind(t *testing.T) {
	f, gvfs, drives := finder(t, nil)
	ws, err := f.Find(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ ID, Name, How, Port, Mount string }
	var got []row
	for _, w := range ws {
		got = append(got, row{w.ID, w.Name, w.How, w.Port, w.Mount})
	}
	want := []row{
		{ws[0].ID, "FORERUNNER", Drive, "", filepath.Join(drives, "FORERUNNER", "GARMIN")},
		{"usb-0000ABCDEF12", "Garmin fenix 8", Direct, "3-8", filepath.Join(gvfs, "mtp:host=Garmin_fenix_8_0000ABCDEF12", "Primary", "GARMIN")},
		{"usb-1-4", "Garmin watch", Direct, "1-4", filepath.Join(gvfs, "mtp:host=%5Busb%3A001%2C005%5D", "Internal Storage", "Garmin")},
	}
	if !reflect.DeepEqual(got, want) || !strings.HasPrefix(ws[0].ID, "drive-") {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestReadDirectAndFallback(t *testing.T) {
	mod := time.Now().Add(-time.Hour).Truncate(time.Second)
	tr := &tree{mod: mod, files: map[string][]byte{"GARMIN/Activity/a.fit": []byte("direct a"), "GARMIN/Monitor/m.fit": []byte("direct m"), "Music/x.mp3": []byte("no")}}
	busy := false
	f, _, _ := finder(t, func(ctx context.Context, info mtp.Info) (Tree, error) {
		if busy {
			return nil, mtp.ErrBusy
		}
		return tr, nil
	})
	ws, _ := f.Find(context.Background())
	w := ws[1]
	read := func() (string, map[string]string, error) {
		got := map[string]string{}
		how, err := f.Read(context.Background(), w, func(fl File) error {
			b, err := fl.Read()
			if err != nil {
				return err
			}
			got[fl.Path] = string(b)
			return nil
		})
		return how, got, err
	}
	how, got, err := read()
	if err != nil || how != Direct || !reflect.DeepEqual(got, map[string]string{"GARMIN/Activity/a.fit": "direct a", "GARMIN/Monitor/m.fit": "direct m"}) || !tr.closed {
		t.Fatalf("direct: %s %v %v closed %v", how, got, err, tr.closed)
	}
	busy = true
	how, got, err = read()
	if err != nil || how != GVfs || !reflect.DeepEqual(got, map[string]string{"GARMIN/Activity/a.fit": "mounted a"}) {
		t.Fatalf("fallback: %s %v %v", how, got, err)
	}
	// Busy without a mount to fall back on is an error that says so.
	w.Mount = ""
	if _, err := f.Read(context.Background(), w, func(File) error { return nil }); !errors.Is(err, mtp.ErrBusy) || Hint(err) == "" {
		t.Fatalf("busy: %v", err)
	}
	// A drive is read from its folder, names from GARMIN down.
	got = map[string]string{}
	how, err = f.Read(context.Background(), ws[0], func(fl File) error {
		b, _ := fl.Read()
		got[fl.Path] = string(b)
		return nil
	})
	if err != nil || how != Drive || !reflect.DeepEqual(got, map[string]string{"GARMIN/ACTIVITY/R.FIT": "drive r"}) {
		t.Fatalf("drive: %s %v %v", how, got, err)
	}
}

func TestReadLimit(t *testing.T) {
	big := &tree{files: map[string][]byte{"GARMIN/big.fit": make([]byte, 10), "GARMIN/ok.fit": []byte("ok")},
		claim: map[string]int64{"GARMIN/big.fit": MaxFile + 1}}
	got := map[string]error{}
	err := readTree(context.Background(), big, func(fl File) error {
		_, got[fl.Path] = fl.Read()
		return nil
	})
	if err != nil || got["GARMIN/big.fit"] == nil || got["GARMIN/ok.fit"] != nil {
		t.Fatalf("%v, %v", got, err)
	}
	lw := &limitWriter{w: io.Discard, left: 4}
	if _, err := lw.Write([]byte("12345")); err == nil {
		t.Fatal("limitWriter let too much through")
	}
}
