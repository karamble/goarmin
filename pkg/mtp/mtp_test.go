// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mtp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"reflect"
	"slices"
	"testing"
	"time"
)

const storage = 0x00010001

// watch builds a fake device shaped like a Garmin watch: GARMIN with a few folders and FIT
// files, another top-level folder, and a storage slot without a medium.
func watch(now time.Time) *fakeDevice {
	mod := now.Add(-2 * time.Hour).Format("20060102T150405")
	data := func(n int, seed byte) []byte {
		b := make([]byte, n)
		for i := range b {
			b[i] = seed + byte(i*7)
		}
		return b
	}
	return &fakeDevice{
		storages: []uint32{storage, 0x00020000},
		info: DeviceInfo{
			StandardVersion: 100, VendorExtensionID: 6, VendorExtensionVersion: 100,
			VendorExtensionDesc: "microsoft.com: 1.0;",
			Operations:          []uint16{opGetDeviceInfo, opOpenSession, opCloseSession, opGetStorageIDs, opGetObjectHandles, opGetObjectInfo, opGetObject},
			Manufacturer:        "Garmin", Model: "fenix 8 - 47mm", DeviceVersion: "23.40", SerialNumber: "3912345678",
		},
		objs: []*fakeObj{
			{handle: 1, parent: 0, storage: storage, name: "GARMIN", folder: true, mod: mod},
			{handle: 2, parent: 1, storage: storage, name: "Activity", folder: true, mod: mod},
			{handle: 3, parent: 2, storage: storage, name: "a.fit", data: data(2000, 1), mod: mod},
			{handle: 4, parent: 2, storage: storage, name: "b.fit", data: data(1024, 9), mod: mod},
			{handle: 5, parent: 1, storage: storage, name: "Monitor", folder: true, mod: mod},
			{handle: 6, parent: 5, storage: storage, name: "c.FIT", data: data(300000, 3), mod: mod},
			{handle: 7, parent: 1, storage: storage, name: "Sports", folder: true, mod: mod},
			{handle: 8, parent: 0, storage: storage, name: "Music", folder: true, mod: mod},
			{handle: 9, parent: 8, storage: storage, name: "song.mp3", data: data(10, 0), mod: mod},
			{handle: 10, parent: 1, storage: storage, name: "..", data: data(10, 0), mod: mod},
			{handle: 11, parent: 1, storage: storage, name: "Ünïcödé ⌚ 🏃.txt", data: data(5, 0), mod: mod},
		},
	}
}

func open(t *testing.T, f *fakeDevice) *Session {
	t.Helper()
	s, err := NewSession(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func walkAll(t *testing.T, s *Session, root string) []Entry {
	t.Helper()
	var out []Entry
	if err := s.Walk(context.Background(), root, func(e Entry) error {
		out = append(out, e)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func paths(es []Entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Path)
	}
	return out
}

func TestWalkAndGet(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name  string
		chunk int
		zlp   bool
		merge bool
	}{
		{"one transfer per container", 0, false, false},
		{"512-byte transfers with empty end packets", 512, true, false},
		{"512-byte transfers, no empty packet", 512, false, false},
		{"response merged into the data", 512, false, true},
		{"odd transfer size", 333, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := watch(now)
			f.chunk, f.zlp, f.merge = tc.chunk, tc.zlp, tc.merge
			s := open(t, f)
			if s.Info.Model != "fenix 8 - 47mm" || s.Info.SerialNumber != "3912345678" || !s.Info.Supports(opGetObject) {
				t.Fatalf("info = %+v", s.Info)
			}
			es := walkAll(t, s, "garmin")
			want := []string{"GARMIN/Activity", "GARMIN/Activity/a.fit", "GARMIN/Activity/b.fit", "GARMIN/Monitor", "GARMIN/Monitor/c.FIT", "GARMIN/Sports", "GARMIN/Ünïcödé ⌚ 🏃.txt"}
			if got := paths(es); !reflect.DeepEqual(got, want) {
				t.Fatalf("paths = %q, want %q", got, want)
			}
			for _, e := range es {
				if e.Folder {
					continue
				}
				o := f.obj(e.Handle)
				if e.Size != int64(len(o.data)) || !e.Modified.Equal(now.Add(-2*time.Hour).Truncate(time.Second)) {
					t.Fatalf("%s: size %d, modified %v", e.Path, e.Size, e.Modified)
				}
				var b bytes.Buffer
				n, err := s.Get(context.Background(), e.Handle, &b)
				if err != nil {
					t.Fatalf("%s: %v", e.Path, err)
				}
				if n != int64(len(o.data)) || !bytes.Equal(b.Bytes(), o.data) {
					t.Fatalf("%s: got %d bytes, want %d", e.Path, n, len(o.data))
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			if !f.closed || f.open || f.ops[len(f.ops)-1] != opCloseSession {
				t.Fatalf("closed %v, open %v, last op 0x%04X", f.closed, f.open, f.ops[len(f.ops)-1])
			}
		})
	}
}

func TestTransactionIDs(t *testing.T) {
	f := watch(time.Now())
	s := open(t, f)
	walkAll(t, s, "GARMIN/Activity")
	// GetDeviceInfo and OpenSession outside the session use 0, then 1, 2, 3, …
	if f.tids[0] != 0 || f.tids[1] != 0 {
		t.Fatalf("tids = %v", f.tids)
	}
	for i := 2; i < len(f.tids); i++ {
		if f.tids[i] != uint32(i-1) {
			t.Fatalf("tids = %v", f.tids)
		}
	}
}

func TestWalkWholeStorageAndMissingRoot(t *testing.T) {
	f := watch(time.Now())
	s := open(t, f)
	if got := len(walkAll(t, s, "")); got != 10 { // all but ".."
		t.Fatalf("%d entries", got)
	}
	if got := walkAll(t, s, "NOPE"); len(got) != 0 {
		t.Fatalf("got %v", paths(got))
	}
	if got := paths(walkAll(t, s, "GARMIN/Activity")); !reflect.DeepEqual(got, []string{"GARMIN/Activity/a.fit", "GARMIN/Activity/b.fit"}) {
		t.Fatalf("got %q", got)
	}
}

func TestSkip(t *testing.T) {
	s := open(t, watch(time.Now()))
	var got []string
	err := s.Walk(context.Background(), "GARMIN", func(e Entry) error {
		got = append(got, e.Path)
		switch e.Path {
		case "GARMIN/Activity":
			return fs.SkipDir
		case "GARMIN/Monitor/c.FIT":
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"GARMIN/Activity", "GARMIN/Monitor", "GARMIN/Monitor/c.FIT"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	boom := errors.New("boom")
	if err := s.Walk(context.Background(), "GARMIN", func(Entry) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestRootRefused(t *testing.T) {
	f := watch(time.Now())
	f.rootRefused = true
	s := open(t, f)
	if got := len(walkAll(t, s, "GARMIN")); got != 7 {
		t.Fatalf("%d entries", got)
	}
}

func TestBusyKeepsSession(t *testing.T) {
	f := watch(time.Now())
	f.busy = map[uint16]int{opGetObjectInfo: 1}
	s := open(t, f)
	_, err := s.ObjectInfo(context.Background(), 3)
	var re *ResponseError
	if !errors.As(err, &re) || !re.Busy() || re.Op != opGetObjectInfo {
		t.Fatalf("err = %v", err)
	}
	if o, err := s.ObjectInfo(context.Background(), 3); err != nil || o.Name != "a.fit" {
		t.Fatalf("after busy: %+v, %v", o, err)
	}
	if _, err := s.ObjectInfo(context.Background(), 99); !errors.As(err, &re) || re.Code != RespInvalidObjectHandle {
		t.Fatalf("err = %v", err)
	}
}

func TestBrokenSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(f *fakeDevice)
		want error
	}{
		{"short container", func(f *fakeDevice) { f.short = true }, ErrProtocol},
		{"wrong transaction", func(f *fakeDevice) { f.wrongTID = true }, ErrProtocol},
		{"timeout", func(f *fakeDevice) { f.stopAfter = f.recvs }, ErrTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := watch(time.Now())
			s := open(t, f)
			tc.set(f)
			if _, err := s.StorageIDs(context.Background()); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			// Once broken, the session refuses everything rather than read out of step.
			f.short, f.wrongTID, f.stopAfter = false, false, 0
			if _, err := s.StorageIDs(context.Background()); !errors.Is(err, tc.want) {
				t.Fatalf("second call: %v", err)
			}
			if err := s.Close(); err != nil || !f.closed {
				t.Fatalf("close: %v", err)
			}
		})
	}
}

func TestTimeoutInsideData(t *testing.T) {
	f := watch(time.Now())
	f.chunk = 4096
	s := open(t, f)
	f.stopAfter = f.recvs + 3 // part of c.FIT's 300 kB, then nothing
	if _, err := s.Get(context.Background(), 6, io.Discard); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
}

type failWriter struct{ after int }

func (w *failWriter) Write(p []byte) (int, error) {
	if w.after <= 0 {
		return 0, errors.New("disk full")
	}
	w.after -= len(p)
	return len(p), nil
}

func TestWriterErrorKeepsSession(t *testing.T) {
	f := watch(time.Now())
	f.chunk = 512
	s := open(t, f)
	if _, err := s.Get(context.Background(), 6, &failWriter{after: 10000}); err == nil || err.Error() != "disk full" {
		t.Fatalf("err = %v", err)
	}
	var b bytes.Buffer
	if _, err := s.Get(context.Background(), 3, &b); err != nil || !bytes.Equal(b.Bytes(), f.obj(3).data) {
		t.Fatalf("after a writer error: %v", err)
	}
}

func TestSessionAlreadyOpen(t *testing.T) {
	f := watch(time.Now())
	f.alreadyOpen = true
	s := open(t, f)
	if ids, err := s.StorageIDs(context.Background()); err != nil || !reflect.DeepEqual(ids, []uint32{storage}) {
		t.Fatalf("ids %v, %v", ids, err)
	}
}

func TestOpenFails(t *testing.T) {
	f := watch(time.Now())
	f.busy = map[uint16]int{opOpenSession: 1}
	if _, err := NewSession(context.Background(), f); err == nil || !f.closed {
		t.Fatalf("err = %v, closed %v", err, f.closed)
	}
}

func TestCanceled(t *testing.T) {
	s := open(t, watch(time.Now()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.StorageIDs(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.StorageIDs(context.Background()); err != nil {
		t.Fatalf("a canceled call before sending must not break the session: %v", err)
	}
}

func TestStorageInfo(t *testing.T) {
	s := open(t, watch(time.Now()))
	si, err := s.StorageInfo(context.Background(), storage)
	if err != nil || si.Description != "Primary" || si.VolumeLabel != "GARMIN" || si.MaxCapacity != 32<<30 {
		t.Fatalf("%+v, %v", si, err)
	}
}

func TestStrings(t *testing.T) {
	for _, s := range []string{"", "a", "GARMIN", "Ünïcödé ⌚ 🏃", string(make([]rune, 200))} {
		e := &enc{}
		e.str(s)
		e.u16(0xBEEF)
		d := &dataset{b: e.Bytes()}
		got := d.str()
		want := s
		if i := slices.Index([]rune(s), 0); i >= 0 {
			want = string([]rune(s)[:i]) // a NUL ends a PTP string
		}
		if got != want || d.u16() != 0xBEEF || d.err != nil {
			t.Fatalf("%q: got %q, err %v", s, got, d.err)
		}
	}
	// A count beyond the data is an error, not a panic.
	d := &dataset{b: []byte{10, 'a', 0}}
	if d.str(); !errors.Is(d.err, ErrProtocol) {
		t.Fatalf("err = %v", d.err)
	}
	d = &dataset{b: []byte{0xFF, 0xFF, 0xFF, 0x0F}}
	if d.u32s(); !errors.Is(d.err, ErrProtocol) {
		t.Fatalf("array err = %v", d.err)
	}
}

func TestParseDate(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	berlin := time.FixedZone("", 3600)
	local := now.In(berlin)
	for _, tc := range []struct {
		in   string
		want time.Time
	}{
		{local.Format("20060102T150405"), now},
		{local.Format("20060102T150405") + ".5", now},
		{now.UTC().Format("20060102T150405") + "Z", now},
		{now.In(time.FixedZone("", -5*3600)).Format("20060102T150405") + "-0500", now},
		{now.In(time.FixedZone("", 5*3600+1800)).Format("20060102T150405") + "+0530", now},
		{"", time.Time{}},
		{"garbage", time.Time{}},
		{local.Format("20060102T150405") + "+5", time.Time{}},
	} {
		if got := parseDate(tc.in, berlin); !got.Equal(tc.want) {
			t.Errorf("parseDate(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestResponseErrorText(t *testing.T) {
	e := &ResponseError{Op: opGetObject, Code: RespDeviceBusy}
	if e.Error() != "mtp: GetObject: device busy (0x2019)" {
		t.Fatal(e.Error())
	}
	e = &ResponseError{Op: 0x9999, Code: 0x2FFF}
	if e.Error() != "mtp: operation 0x9999: response (0x2FFF)" {
		t.Fatal(e.Error())
	}
}
