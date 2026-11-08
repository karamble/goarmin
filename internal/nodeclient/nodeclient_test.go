// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package nodeclient_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/internal/api"
	"github.com/karamble/goarmin/internal/nodeclient"
	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/nodeproto"
	"github.com/karamble/goarmin/internal/nodes"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/auth"
	"github.com/karamble/goarmin/pkg/blobstore"
	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/fitid/fittest"
	"github.com/karamble/goarmin/pkg/garmin/fakewatch"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/multilink/memlink"
)

const (
	watchAddr = "C0:FF:EE:12:34:56"
	unit      = uint32(3_500_000_001) // the fake watch's unit number
	password  = "a long enough password"
	email     = "someone@example.invalid"
)

// server is a goarmin server with the node API, whose node routes can be made to fail.
type server struct {
	st      *store.Store
	srv     *httptest.Server
	down    atomic.Bool // every node request fails with 503
	upDown  atomic.Bool // only uploads fail
	user    *http.Client
	devices func() []map[string]any
}

func newServer(t *testing.T) *server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(dir, "goarmin.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	blobs, err := blobstore.Open(filepath.Join(dir, "raw"))
	if err != nil {
		t.Fatal(err)
	}
	reg := nodes.New(nodes.Options{})
	token, _ := auth.NewToken()
	s := &server{st: st}
	a, err := api.New(api.Options{Store: st, Blobs: blobs, PublicURL: "http://localhost:8642", SetupToken: token, Node: reg, Nodes: reg})
	if err != nil {
		t.Fatal(err)
	}
	h := a.Handler()
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, nodeproto.Prefix) && (s.down.Load() || (s.upDown.Load() && r.Method == http.MethodPut)) {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(s.srv.Close)

	jar, _ := cookiejar.New(nil)
	s.user = &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"token": token, "email": email, "name": "Someone", "password": password})
	req, _ := http.NewRequest(http.MethodPost, s.srv.URL+"/api/setup", bytes.NewReader(body))
	req.Header.Set("Origin", "http://localhost:8642")
	resp, err := s.user.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %v %v", err, resp)
	}
	resp.Body.Close()
	return s
}

// do sends a request as the signed-in user.
func (s *server) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, s.srv.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:8642")
	resp, err := s.user.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (s *server) rawFiles(t *testing.T) int {
	t.Helper()
	dev, err := s.st.DeviceByUnit(context.Background(), unit)
	if err != nil {
		return 0
	}
	files, err := s.st.RawFilesOf(context.Background(), dev.ID)
	if err != nil {
		t.Fatal(err)
	}
	return len(files)
}

func (s *server) runs(t *testing.T) []store.SyncRun {
	t.Helper()
	dev, err := s.st.DeviceByUnit(context.Background(), unit)
	if err != nil {
		return nil
	}
	runs, err := s.st.RecentSyncRuns(context.Background(), dev.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestNodeEndToEnd: a node signs in, syncs the fake watch and hands every file to the server;
// the watch flags a file only after the server has it. With the server away nothing is flagged
// and the spool keeps the files; once it is back, the next sync hands them over and flags them.
func TestNodeEndToEnd(t *testing.T) {
	now := time.Now()
	s := newServer(t)
	w := fakewatch.New(fakewatch.Options{})
	for i := range 4 {
		w.AddFile("FIT_TYPE_32", fittest.File(typedef.FileMonitoringB, unit, now.Add(-time.Duration(i+1)*time.Hour), 40+i))
	}
	open := func(_ context.Context, _ int) (multilink.Link, io.Closer, error) {
		a, b := memlink.Pipe(memlink.Options{})
		go func() { _ = w.Serve(context.Background(), b) }()
		return a, a, nil
	}

	// Sign in, as goarmin node login does.
	dataDir := t.TempDir()
	c, err := nodeclient.NewClient(s.srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := c.Login(ctx, nodeproto.LoginRequest{Email: email, Password: "wrong password here"}); err == nil {
		t.Fatal("a wrong password signed the node in")
	}
	lr, err := c.Login(ctx, nodeproto.LoginRequest{Email: email, Password: password, Name: "laptop", Hostname: "laptop"})
	if err != nil {
		t.Fatal(err)
	}
	if err := nodeclient.SaveAuth(dataDir, nodeclient.Auth{Server: s.srv.URL, NodeID: lr.NodeID, Token: lr.Token, Account: lr.Account, Name: "laptop"}); err != nil {
		t.Fatal(err)
	}
	if err := syncnode.Save(dataDir, watchAddr, syncnode.WatchRecord{Unit: unit, Product: 4536, Name: "Fake"}); err != nil {
		t.Fatal(err)
	}
	// The watch is known on the server already and marks files as synced (as the laptop's
	// copy on the server has it).
	pre, err := s.st.EnsureDevice(ctx, store.Device{UnitID: unit, Product: 4536, Name: "Fake", Address: watchAddr})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.SetAfterFetch(ctx, pre.ID, store.AfterFetchArchive); err != nil {
		t.Fatal(err)
	}

	// The first (scheduled) sync runs with the server refusing uploads.
	s.upDown.Store(true)
	nctx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- nodeclient.Run(nctx, nodeclient.Options{DataDir: dataDir, Adapter: "hci0", Version: "test", Interval: time.Hour,
			Hub: func(o *nodehub.Options) {
				o.Sync = func(ctx context.Context, opt syncnode.Options) (syncnode.Result, error) {
					return syncnode.SyncWith(ctx, opt, watchAddr, open, []int{0})
				}
				o.AdapterState = func(context.Context) (bluez.Adapter, error) {
					return bluez.Adapter{Name: "hci0", Powered: true, PowerState: "on"}, nil
				}
			}})
	}()
	waitFor(t, "the first sync's log entry", 20*time.Second, func() bool { return len(s.runs(t)) >= 1 })
	if got := w.Unsynced(); got != 4 {
		t.Fatalf("%d files unsynced on the watch after a sync the server took nothing from, want 4", got)
	}
	if n := s.rawFiles(t); n != 0 {
		t.Fatalf("the server has %d files while refusing uploads", n)
	}
	files, _ := filepath.Glob(filepath.Join(dataDir, "watches", "*", "files", "*", "*.fit"))
	if len(files) != 4 {
		t.Fatalf("%d files wait in the spool, want 4", len(files))
	}

	// The dashboard sees the watch's status through the node, and asks for a sync now.
	s.upDown.Store(false)
	dev, err := s.st.DeviceByUnit(ctx, unit)
	if err != nil {
		t.Fatal(err)
	}
	code, body := s.do(t, http.MethodPost, "/api/devices/"+itoa(dev.ID)+"/sync", nil)
	if code != http.StatusAccepted {
		t.Fatalf("sync now: %d %s", code, body)
	}
	waitFor(t, "the files on the server and flagged on the watch", 20*time.Second, func() bool {
		return s.rawFiles(t) == 4 && w.Unsynced() == 0
	})
	if got := w.Stats().Pulls; got != 4 {
		t.Fatalf("the watch served %d pulls, want 4 (none twice)", got)
	}
	files, _ = filepath.Glob(filepath.Join(dataDir, "watches", "*", "files", "*", "*.fit"))
	if len(files) != 0 {
		t.Fatalf("%d files still in the spool after the server took them", len(files))
	}
	code, body = s.do(t, http.MethodGet, "/api/devices", nil)
	if code != 200 || !strings.Contains(string(body), `"node":"laptop"`) {
		t.Fatalf("devices: %d %s", code, body)
	}

	// The file list comes from the node's spool record.
	code, body = s.do(t, http.MethodGet, "/api/devices/"+itoa(dev.ID)+"/files", nil)
	if code != 200 || strings.Count(string(body), `"uid"`) != 4 {
		t.Fatalf("files: %d %s", code, body)
	}

	// Revoked: the node stops.
	code, _ = s.do(t, http.MethodDelete, "/api/nodes/"+itoa(lr.NodeID), nil)
	if code != http.StatusNoContent {
		t.Fatalf("revoke: %d", code)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "no longer accepts") {
			t.Fatalf("node ended with %v, want the revoked error", err)
		}
	case <-time.After(40 * time.Second):
		t.Fatal("the node kept running after its sign-in was revoked")
	}
	stop()
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
