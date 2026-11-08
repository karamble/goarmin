// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package zipwalk

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

type entry struct {
	name   string
	data   []byte
	stored bool // no compression
}

func mkzip(t *testing.T, mod time.Time, es ...entry) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, e := range es {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: mod}
		if e.stored {
			h.Method = zip.Store
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func noise(n int, seed byte) []byte {
	b := make([]byte, n)
	x := uint32(seed) + 1
	for i := range b {
		x = x*1664525 + 1013904223
		b[i] = byte(x >> 24)
	}
	return b
}

type found struct {
	files   map[string][]byte
	skipped map[string]error
}

func walk(t *testing.T, w *Walker, z []byte) (found, error) {
	t.Helper()
	f := found{files: map[string][]byte{}, skipped: map[string]error{}}
	w.Skipped = func(p string, err error) { f.skipped[p] = err }
	err := w.Walk(context.Background(), bytes.NewReader(z), int64(len(z)), "", func(fl File) error {
		b, err := fl.Read()
		if err != nil {
			f.skipped[fl.Path] = err
			return nil
		}
		f.files[fl.Path] = b
		return nil
	})
	return f, err
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestNestedArchives(t *testing.T) {
	mod := time.Now().Add(-time.Hour).Truncate(time.Second)
	fit := noise(5000, 1)
	deepest := mkzip(t, mod, entry{name: "too-deep.fit", data: fit})
	level3 := mkzip(t, mod, entry{name: "c.fit", data: fit}, entry{name: "deeper.zip", data: deepest})
	level2 := mkzip(t, mod, entry{name: "b.fit", data: fit}, entry{name: "l3.zip", data: level3})
	big := mkzip(t, mod, entry{name: "inside-big.json", data: noise(3000, 2), stored: true})
	top := mkzip(t, mod,
		entry{name: "DI_CONNECT/"},
		entry{name: "DI_CONNECT/DI-Connect-Fitness/summary.json", data: []byte(`{"a":1}`)},
		entry{name: "DI_CONNECT/Part1.zip", data: level2},
		entry{name: "big.zip", data: big, stored: true},
		entry{name: "../../escape.fit", data: fit},
		entry{name: `win\path\x.fit`, data: fit},
		entry{name: "/abs.fit", data: fit},
		entry{name: "fake.zip", data: []byte("not a zip at all")},
	)
	w := &Walker{Limits: Limits{MemArchive: 1024, TempDir: t.TempDir(), MaxDepth: 2}}
	f, err := walk(t, w, top)
	if err != nil {
		t.Fatal(err)
	}
	wantFiles := []string{"DI_CONNECT/DI-Connect-Fitness/summary.json", "DI_CONNECT/Part1.zip/b.fit", "DI_CONNECT/Part1.zip/l3.zip/c.fit",
		"abs.fit", "big.zip/inside-big.json", "escape.fit", "win/path/x.fit"}
	if got := keys(f.files); !reflect.DeepEqual(got, wantFiles) {
		t.Fatalf("files %q\nwant  %q", got, wantFiles)
	}
	if !bytes.Equal(f.files["DI_CONNECT/Part1.zip/l3.zip/c.fit"], fit) {
		t.Fatal("nested content differs")
	}
	if !errors.Is(f.skipped["DI_CONNECT/Part1.zip/l3.zip/deeper.zip"], ErrDepth) || !errors.Is(f.skipped["fake.zip"], ErrNotZip) || len(f.skipped) != 2 {
		t.Fatalf("skipped %v", f.skipped)
	}
	if w.Archives() != 4 { // top, Part1, l3, big (from a temporary file)
		t.Fatalf("archives = %d", w.Archives())
	}
	if w.Found() != len(wantFiles) { // the files handed over; archives (nested or not, broken or too deep) are not files
		t.Fatalf("found = %d, want %d", w.Found(), len(wantFiles))
	}
	if left, _ := os.ReadDir(w.TempDir); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}

func TestModified(t *testing.T) {
	mod := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	z := mkzip(t, mod, entry{name: "a.fit", data: []byte("x")})
	var got time.Time
	err := (&Walker{}).Walk(context.Background(), bytes.NewReader(z), int64(len(z)), "up", func(f File) error {
		if f.Path != "up/a.fit" || f.Size != 1 {
			t.Fatalf("%+v", f)
		}
		got = f.Modified
		return nil
	})
	if err != nil || !got.Equal(mod) {
		t.Fatalf("modified %v, want %v (%v)", got, mod, err)
	}
}

func TestLimits(t *testing.T) {
	mod := time.Now()
	many := []entry{}
	for i := 0; i < 20; i++ {
		many = append(many, entry{name: filepath.Join("f", string(rune('a'+i))+".fit"), data: []byte{byte(i)}})
	}
	if _, err := walk(t, &Walker{Limits: Limits{MaxEntries: 10}}, mkzip(t, mod, many...)); !errors.Is(err, ErrTooManyEntries) {
		t.Fatalf("entries: %v", err)
	}
	two := mkzip(t, mod, entry{name: "a.fit", data: noise(4000, 3)}, entry{name: "b.fit", data: noise(4000, 4)})
	if _, err := walk(t, &Walker{Limits: Limits{MaxTotal: 6000}}, two); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("total: %v", err)
	}
	// Two megabytes of zeros compress a thousandfold: no real FIT or JSON file does that.
	bomb := mkzip(t, mod, entry{name: "bomb.json", data: make([]byte, 2<<20)}, entry{name: "ok.fit", data: noise(100, 5)})
	f, err := walk(t, &Walker{}, bomb)
	if err != nil || !errors.Is(f.skipped["bomb.json"], ErrRatio) || f.files["ok.fit"] == nil {
		t.Fatalf("bomb: %v %v", f.skipped, err)
	}
	f, err = walk(t, &Walker{Limits: Limits{MaxFile: 50}}, mkzip(t, mod, entry{name: "big.fit", data: noise(100, 6)}))
	if err != nil || !errors.Is(f.skipped["big.fit"], ErrFileTooLarge) {
		t.Fatalf("file size: %v %v", f.skipped, err)
	}
	if _, err := walk(t, &Walker{}, []byte("PK not really")); !errors.Is(err, ErrNotZip) {
		t.Fatalf("not a zip: %v", err)
	}
}

func TestCallerErrorEndsWalk(t *testing.T) {
	mod := time.Now()
	inner := mkzip(t, mod, entry{name: "stop.fit", data: []byte("x")}, entry{name: "after.fit", data: []byte("y")})
	middle := mkzip(t, mod, entry{name: "inner.zip", data: inner})
	top := mkzip(t, mod, entry{name: "middle.zip", data: middle}, entry{name: "later.fit", data: []byte("z")})
	stop := errors.New("stop here")
	var seen []string
	err := (&Walker{}).Walk(context.Background(), bytes.NewReader(top), int64(len(top)), "", func(f File) error {
		seen = append(seen, f.Path)
		if f.Path == "middle.zip/inner.zip/stop.fit" {
			return stop
		}
		return nil
	})
	if err != stop || !reflect.DeepEqual(seen, []string{"middle.zip/inner.zip/stop.fit"}) {
		t.Fatalf("err %v, seen %q", err, seen)
	}
}

func TestCanceled(t *testing.T) {
	z := mkzip(t, time.Now(), entry{name: "a.fit", data: []byte("x")})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (&Walker{}).Walk(ctx, bytes.NewReader(z), int64(len(z)), "", func(File) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestClean(t *testing.T) {
	for in, want := range map[string]string{"a/b.fit": "a/b.fit", "../x": "x", "a/../../b": "b", `c:\x\y.fit`: "c:/x/y.fit", "./a": "a"} {
		if got, ok := clean(in); !ok || got != want {
			t.Errorf("clean(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "dir/", `dir\`, "a\x00b", ".", "/"} {
		if got, ok := clean(in); ok {
			t.Errorf("clean(%q) = %q, want refused", in, got)
		}
	}
}
