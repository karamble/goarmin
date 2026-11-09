// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fileaccess_test

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"testing"

	"github.com/karamble/goarmin/pkg/garmin/fakewatch"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/smart"
)

func TestStoredZlib(t *testing.T) {
	for _, n := range []int{1, 1508, 65535, 65536, 200_000} {
		data := payload(uint64(n), n)
		for _, window := range []uint32{9, 11, 15} {
			z := fileaccess.StoredZlib(data, window)
			if got := uint32(z[0]>>4) + 8; got != window || z[0]&0x0f != 8 || (uint16(z[0])<<8|uint16(z[1]))%31 != 0 {
				t.Fatalf("%d bytes, window %d: header % x", n, window, z[:2])
			}
			zr, err := zlib.NewReader(bytes.NewReader(z))
			if err != nil {
				t.Fatal(err)
			}
			back, err := io.ReadAll(zr)
			if err != nil || !bytes.Equal(back, data) {
				t.Fatalf("%d bytes, window %d: round trip %d bytes, %v", n, window, len(back), err)
			}
		}
	}
}

// TestPush sends a workout and a schedule as Garmin Connect does: the workout is filed under
// the watch's own id and announced, the schedule is taken but not listed.
func TestPush(t *testing.T) {
	w := newWatch(fakewatch.Options{})
	before := len(w.Files())
	eng := fileaccess.New(connect(t, w, 0, 0).conn, fileaccess.NewMemStore(), fileaccess.Options{})
	workout := payload(5, 1508)
	got, err := eng.Push(syncCtx(t), "FIT_TYPE_5", workout, fileaccess.PushOptions{Announce: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.HostUID.MSB != ^uint64(0) || !got.Announced || got.UID == got.HostUID || got.UID == (smart.UUID{}) {
		t.Fatalf("pushed %+v", got)
	}
	files := w.Files()
	if len(files) != before+1 || files[len(files)-1].UID != got.UID || !bytes.Equal(files[len(files)-1].Data, workout) ||
		files[len(files)-1].TypeName != "FIT_TYPE_5" {
		t.Fatalf("the watch has %v", files)
	}
	// The stream declared the window the watch asked for (11 on the Fenix 8), not 32 KiB.
	if h := w.Stats().PushHeaders; len(h) != 1 || h[0][0] != 0x38 {
		t.Fatalf("zlib headers %x", h)
	}

	schedule := payload(7, 122)
	got, err = eng.Push(syncCtx(t), "FIT_TYPE_7", schedule, fileaccess.PushOptions{})
	if err != nil || got.Announced {
		t.Fatalf("schedule: %+v, %v", got, err)
	}
	if s := w.Schedules(); len(s) != 1 || !bytes.Equal(s[0], schedule) || len(w.Files()) != before+1 {
		t.Fatalf("schedules %d, files %d", len(s), len(w.Files()))
	}
	if w.Stats().Pushes != 2 {
		t.Fatalf("stats %+v", w.Stats())
	}
}

func TestPushRefused(t *testing.T) {
	w := newWatch(fakewatch.Options{Faults: fakewatch.Faults{PushResult: func(name string, _ int) (smart.ItemAccessResult, bool) {
		return smart.ItemAccessItemTypeNotSupported, name == "FIT_TYPE_6"
	}}})
	eng := fileaccess.New(connect(t, w, 0, 0).conn, fileaccess.NewMemStore(), fileaccess.Options{})
	_, err := eng.Push(syncCtx(t), "FIT_TYPE_6", payload(6, 300), fileaccess.PushOptions{Announce: true})
	var pe *fileaccess.PushError
	if !errors.Is(err, fileaccess.ErrPushRefused) || !errors.As(err, &pe) || pe.Result != smart.ItemAccessItemTypeNotSupported {
		t.Fatalf("refused push: %v", err)
	}
	// The engine stays usable.
	if _, err := eng.Push(syncCtx(t), "FIT_TYPE_5", payload(5, 300), fileaccess.PushOptions{Announce: true}); err != nil {
		t.Fatalf("a push after a refusal: %v", err)
	}
}
