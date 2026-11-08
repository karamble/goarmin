// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/internal/importer"
	"github.com/karamble/goarmin/internal/ingest"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/usbsource"
	"github.com/karamble/goarmin/pkg/auth"
	"github.com/karamble/goarmin/pkg/blobstore"
	"github.com/karamble/goarmin/pkg/fitid/fittest"
	"github.com/karamble/goarmin/pkg/mtp"
)

const usbUnit = 3_912_345_678

// usbTree is a watch's files over MTP, in memory.
type usbTree struct{ files map[string][]byte }

func (t *usbTree) names() []string {
	var ns []string
	for n := range t.files {
		ns = append(ns, n)
	}
	// One file only in these tests, or sorted by the caller.
	return ns
}

func (t *usbTree) Walk(ctx context.Context, root string, fn func(mtp.Entry) error) error {
	for i, n := range t.names() {
		if err := fn(mtp.Entry{Handle: uint32(i + 1), Path: n, Name: filepath.Base(n), Size: int64(len(t.files[n])), Modified: time.Now().Add(-time.Hour).Truncate(time.Second)}); err != nil {
			return err
		}
	}
	return nil
}

func (t *usbTree) Get(ctx context.Context, h uint32, w io.Writer) (int64, error) {
	n, err := w.Write(t.files[t.names()[h-1]])
	return int64(n), err
}

func (t *usbTree) Close() error { return nil }

// newImportEnv is a server that imports, with a watch on USB.
func newImportEnv(t *testing.T, opt Options) *env {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "goarmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := blobstore.Open(filepath.Join(dir, "raw"))
	if err != nil {
		t.Fatal(err)
	}
	in := &ingest.Ingester{Store: st, Blobs: blobs}
	tree := &usbTree{files: map[string][]byte{"GARMIN/Monitor/M1.FIT": fittest.File(typedef.FileMonitoringB, usbUnit, time.Now().Add(-2*time.Hour), 10)}}
	finder := &usbsource.Finder{
		Devices: func() ([]mtp.Info, error) {
			return []mtp.Info{{Path: "3-8", Vendor: usbsource.GarminVendor, Product: 0x4ff1, Manufacturer: "Garmin", ProductName: "fenix 8", Serial: "3912345678"}}, nil
		},
		Open: func(context.Context, mtp.Info) (usbsource.Tree, error) { return tree, nil },
	}
	r := importer.New(importer.Options{Store: st, In: in, DataDir: dir, USB: finder, Every: time.Hour})
	go r.Run(ctx)
	token, _ := auth.NewToken()
	opt.Store, opt.Blobs, opt.PublicURL, opt.SetupToken, opt.Imports = st, blobs, public, token, r
	s, err := New(opt)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &env{t: t, srv: srv, st: st, in: in, setup: token}
}

type upfile struct {
	name string
	data []byte
}

func multipartBody(t *testing.T, files ...upfile) (string, []byte) {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	mw.WriteField("note", "a field that is no file")
	for _, f := range files {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, f.name))
		h.Set("Content-Type", "application/octet-stream")
		w, err := mw.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(f.data)
	}
	mw.Close()
	return mw.FormDataContentType(), b.Bytes()
}

func (c *client) upload(status int, ct string, body []byte) []byte {
	c.t.Helper()
	req, _ := http.NewRequest("POST", c.e.srv.URL+"/api/imports", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	if c.origin != "" {
		req.Header.Set("Origin", c.origin)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != status {
		c.t.Fatalf("upload: %d %s, want %d", resp.StatusCode, out, status)
	}
	return out
}

// waitImport polls an import until it ends.
func (c *client) waitImport(id any) map[string]any {
	c.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		imp := decodeJSON[map[string]any](c.t, c.want(200, "GET", fmt.Sprint("/api/imports/", id), nil))
		switch imp["status"] {
		case "done", "failed", "canceled":
			return imp
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.t.Fatal("the import did not finish")
	return nil
}

func (e *env) second(admin *client) *client {
	inv := decodeJSON[map[string]any](e.t, admin.want(201, "POST", "/api/admin/invites", map[string]string{}))
	token := strings.TrimPrefix(inv["url"].(string), public+"/invite/")
	b := e.client()
	b.want(201, "POST", "/api/invites/"+token+"/accept", map[string]string{"email": "b@example.invalid", "password": password})
	return b
}

func TestUploadImport(t *testing.T) {
	e := newImportEnv(t, Options{})
	a := e.admin()
	b := e.second(a)
	ct, body := multipartBody(t,
		upfile{"GARMIN/Monitor/M1.FIT", fittest.File(typedef.FileMonitoringB, usbUnit, time.Now().Add(-5*time.Hour), 5)},
		upfile{"export/DI_CONNECT/DI-Connect-Fitness/a_summarizedActivities.json", []byte(`[{"x":1}]`)},
	)
	imp := decodeJSON[map[string]any](t, a.upload(202, ct, body))
	if imp["kind"] != "upload" || imp["name"] != "2 files" {
		t.Fatalf("%v", imp)
	}
	done := a.waitImport(imp["id"])
	rep := done["report"].(map[string]any)
	if done["status"] != "done" || done["source"] != "gc_export" || rep["new"] != 1.0 || rep["kept"] != 1.0 || rep["export"] != true {
		t.Fatalf("%v", done)
	}
	list := decodeJSON[map[string][]map[string]any](t, a.want(200, "GET", "/api/imports", nil))["items"]
	if len(list) != 1 || list[0]["id"] != imp["id"] {
		t.Fatalf("list %v", list)
	}
	// Another account sees none of it.
	b.want(404, "GET", fmt.Sprint("/api/imports/", imp["id"]), nil)
	b.want(404, "DELETE", fmt.Sprint("/api/imports/", imp["id"]), nil)
	if got := decodeJSON[map[string][]map[string]any](t, b.want(200, "GET", "/api/imports", nil))["items"]; len(got) != 0 {
		t.Fatalf("b lists %v", got)
	}
	a.want(409, "DELETE", fmt.Sprint("/api/imports/", imp["id"]), nil)
	a.want(404, "GET", "/api/imports/999", nil)

	// Refused: not multipart, nothing in it, from another site.
	a.want(415, "POST", "/api/imports", map[string]string{"x": "y"})
	ct, empty := multipartBody(t)
	a.upload(400, ct, empty)
	evil := e.client()
	evil.hc = a.hc
	evil.origin = "https://evil.example.invalid"
	ct, body = multipartBody(t, upfile{"x.fit", []byte("x")})
	evil.upload(403, ct, body)
}

func TestUploadLimits(t *testing.T) {
	e := newImportEnv(t, Options{ImportMax: 4 << 10, ImportMaxFiles: 2})
	a := e.admin()
	ct, body := multipartBody(t, upfile{"big.fit", make([]byte, 8<<10)})
	a.upload(413, ct, body)
	ct, body = multipartBody(t, upfile{"1.fit", []byte("1")}, upfile{"2.fit", []byte("2")}, upfile{"3.fit", []byte("3")})
	a.upload(413, ct, body)
	// The refused uploads are on record as failed.
	for _, imp := range decodeJSON[map[string][]map[string]any](t, a.want(200, "GET", "/api/imports", nil))["items"] {
		if imp["status"] != "failed" || imp["error"] == "" {
			t.Fatalf("%v", imp)
		}
	}
}

func TestPartPath(t *testing.T) {
	for name, want := range map[string]string{
		"GARMIN/Activity/a.fit": "GARMIN/Activity/a.fit",
		"../../etc/passwd":      "etc/passwd",
		`C:\Users\x\a.fit`:      "C:/Users/x/a.fit",
		"/abs/b.fit":            "abs/b.fit",
		"":                      "",
		"..":                    "",
	} {
		ct, body := multipartBody(t, upfile{name, []byte("x")})
		_, params, _ := strings.Cut(ct, "boundary=")
		mr := multipart.NewReader(bytes.NewReader(body), params)
		mr.NextPart() // the plain field
		p, err := mr.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		got, ok := partPath(p)
		if got != want || ok != (want != "") {
			t.Errorf("partPath(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
}

func TestUploadName(t *testing.T) {
	for _, tc := range []struct {
		names []string
		want  string
	}{
		{[]string{"x.zip"}, "x.zip"},
		{[]string{"GARMIN/a.fit", "GARMIN/b/c.fit"}, "GARMIN (2 files)"},
		{[]string{"a.fit", "b.fit"}, "2 files"},
		{[]string{"GARMIN/a.fit", "Other/b.fit"}, "2 files"},
	} {
		var fs []ingest.StagedFile
		for _, n := range tc.names {
			fs = append(fs, ingest.StagedFile{Name: n})
		}
		if got := uploadName(fs); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.names, got, tc.want)
		}
	}
}

func TestUSBRoutes(t *testing.T) {
	e := newImportEnv(t, Options{})
	a := e.admin()
	b := e.second(a)
	ws := decodeJSON[[]map[string]any](t, a.want(200, "GET", "/api/usb", nil))
	if len(ws) != 1 || ws[0]["id"] != "usb-3912345678" || ws[0]["owner"] != "unknown" || ws[0]["device"] != nil || ws[0]["how"] != "direct" {
		t.Fatalf("%v", ws)
	}
	imp := decodeJSON[map[string]any](t, a.want(202, "POST", "/api/usb/usb-3912345678/import", nil))
	if done := a.waitImport(imp["id"]); done["status"] != "done" || done["kind"] != "usb" || done["report"].(map[string]any)["new"] != 1.0 {
		t.Fatalf("%v", done)
	}
	ws = decodeJSON[[]map[string]any](t, a.want(200, "GET", "/api/usb", nil))
	dev, _ := ws[0]["device"].(map[string]any)
	if ws[0]["owner"] != "yours" || ws[0]["auto"] != true || dev == nil || ws[0]["last"] == nil {
		t.Fatalf("%v", ws)
	}
	devID := fmt.Sprint(dev["id"])
	if got := decodeJSON[map[string]any](t, a.want(200, "PATCH", "/api/devices/"+devID, map[string]any{"usb_auto": false})); got["usb_auto"] != false {
		t.Fatalf("%v", got)
	}
	if ws = decodeJSON[[]map[string]any](t, a.want(200, "GET", "/api/usb", nil)); ws[0]["auto"] != false {
		t.Fatalf("%v", ws)
	}
	// To another account it is someone else's watch, without its name or history.
	ws = decodeJSON[[]map[string]any](t, b.want(200, "GET", "/api/usb", nil))
	if ws[0]["owner"] != "other" || ws[0]["device"] != nil || ws[0]["last"] != nil || ws[0]["auto"] != nil {
		t.Fatalf("%v", ws)
	}
	b.want(409, "POST", "/api/usb/usb-3912345678/import", nil)
	b.want(404, "PATCH", "/api/devices/"+devID, map[string]any{"usb_auto": true})
	a.want(404, "POST", "/api/usb/usb-nope/import", nil)
}

func TestNoImports(t *testing.T) {
	e := newEnv(t)
	a := e.admin()
	ct, body := multipartBody(t, upfile{"x.fit", []byte("x")})
	a.upload(503, ct, body)
	if ws := decodeJSON[[]map[string]any](t, a.want(200, "GET", "/api/usb", nil)); len(ws) != 0 {
		t.Fatalf("%v", ws)
	}
	a.want(503, "POST", "/api/usb/usb-x/import", nil)
}
