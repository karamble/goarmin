// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/nodeproto"
	"github.com/karamble/goarmin/internal/nodes"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/auth"
	"github.com/karamble/goarmin/pkg/fitid/fittest"
)

// nodeEnv is a server with the node API and a clock the registry reads.
type nodeEnv struct {
	*env
	reg *nodes.Registry
	mu  sync.Mutex
	now time.Time
}

func newNodeEnv(t *testing.T) *nodeEnv {
	t.Helper()
	ne := &nodeEnv{now: time.Now()}
	ne.reg = nodes.New(nodes.Options{Now: func() time.Time { ne.mu.Lock(); defer ne.mu.Unlock(); return ne.now }, CallTimeout: 2 * time.Second})
	ne.env = newEnvOpts(t, Options{Node: ne.reg, Nodes: ne.reg})
	return ne
}

func (ne *nodeEnv) advance(d time.Duration) {
	ne.mu.Lock()
	ne.now = ne.now.Add(d)
	ne.mu.Unlock()
}

// nodeDo sends a node request with token (empty: none) and decodes the answer into out.
func (e *env) nodeDo(token, method, path string, body any, out any) int {
	e.t.Helper()
	var rd io.Reader
	if b, ok := body.([]byte); ok {
		rd = bytes.NewReader(b)
	} else if body != nil {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, e.srv.URL+nodeproto.Prefix+path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if b, ok := body.([]byte); ok {
		_ = b
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func (e *env) nodeLogin(email string) nodeproto.LoginResponse {
	e.t.Helper()
	var lr nodeproto.LoginResponse
	if code := e.nodeDo("", http.MethodPost, "/login", nodeproto.LoginRequest{Email: email, Password: password, Name: "laptop"}, &lr); code != http.StatusCreated {
		e.t.Fatalf("node login %s: %d", email, code)
	}
	return lr
}

func upload(e *env, token string, unit uint32, name string, b []byte) int {
	e.t.Helper()
	sum := sha256.Sum256(b)
	req, _ := http.NewRequest(http.MethodPut, e.srv.URL+nodeproto.Prefix+"/files/"+hex.EncodeToString(sum[:]), bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(nodeproto.HeaderUnit, strconv.FormatUint(uint64(unit), 10))
	req.Header.Set(nodeproto.HeaderName, name)
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		e.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestNodeLoginAndRevoke(t *testing.T) {
	ne := newNodeEnv(t)
	admin := ne.admin()
	if code := ne.nodeDo("", http.MethodPost, "/login", nodeproto.LoginRequest{Email: "admin@example.invalid", Password: "not the password at all"}, nil); code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", code)
	}
	if code := ne.nodeDo("", http.MethodPost, "/hello", nodeproto.HelloRequest{}, nil); code != http.StatusUnauthorized {
		t.Fatalf("hello without a token: %d", code)
	}
	lr := ne.nodeLogin("admin@example.invalid")
	var hr nodeproto.HelloResponse
	if code := ne.nodeDo(lr.Token, http.MethodPost, "/hello", nodeproto.HelloRequest{Version: "test"}, &hr); code != 200 || hr.NodeID != lr.NodeID {
		t.Fatalf("hello: %d %+v", code, hr)
	}
	var list []map[string]any
	_ = json.Unmarshal(admin.want(200, "GET", "/api/nodes", nil), &list)
	if len(list) != 1 || list[0]["name"] != "laptop" || list[0]["online"] != true {
		t.Fatalf("nodes: %v", list)
	}
	admin.want(200, "PATCH", "/api/nodes/"+strconv.FormatInt(lr.NodeID, 10), map[string]string{"name": "Kitchen laptop"})
	admin.want(204, "DELETE", "/api/nodes/"+strconv.FormatInt(lr.NodeID, 10), nil)
	if code := ne.nodeDo(lr.Token, http.MethodPost, "/hello", nodeproto.HelloRequest{}, nil); code != http.StatusUnauthorized {
		t.Fatalf("hello after the revoke: %d", code)
	}
}

func TestNodeEnrollByCode(t *testing.T) {
	ne := newNodeEnv(t)
	admin := ne.admin()
	var en nodeproto.EnrollResponse
	if code := ne.nodeDo("", http.MethodPost, "/enroll", nodeproto.EnrollRequest{Name: "pi"}, &en); code != http.StatusCreated || len(en.Code) != 9 {
		t.Fatalf("enroll: %d %+v", code, en)
	}
	if code := ne.nodeDo("", http.MethodGet, "/enroll/"+en.Poll, nil, nil); code != http.StatusAccepted {
		t.Fatalf("poll before approval: %d", code)
	}
	admin.want(404, "POST", "/api/nodes/approve", map[string]string{"code": "ZZZZ-ZZZZ"})
	admin.want(200, "POST", "/api/nodes/approve", map[string]string{"code": en.Code})
	var pr nodeproto.PollResponse
	if code := ne.nodeDo("", http.MethodGet, "/enroll/"+en.Poll, nil, &pr); code != 200 || pr.Token == "" {
		t.Fatalf("poll after approval: %d %+v", code, pr)
	}
	if code := ne.nodeDo("", http.MethodGet, "/enroll/"+en.Poll, nil, nil); code != http.StatusNotFound {
		t.Fatalf("the token was handed out twice: %d", code)
	}
	if code := ne.nodeDo(pr.Token, http.MethodPost, "/hello", nodeproto.HelloRequest{}, nil); code != 200 {
		t.Fatalf("hello with the approved token: %d", code)
	}
}

// TestNodeWatches: a node syncs its own account's watches; another account's watch is not
// routed to it and its uploads are refused; the same file twice is stored once; an offline
// node's watch answers 503.
func TestNodeWatches(t *testing.T) {
	ne := newNodeEnv(t)
	ctx := context.Background()
	admin := ne.admin()
	// A second account with a password.
	bob, err := ne.st.EnsureUser(ctx, "bob@example.invalid", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := auth.HashPassword(password)
	if err := ne.st.SetPassword(ctx, bob.ID, hash); err != nil {
		t.Fatal(err)
	}
	// The admin's watch and Bob's watch.
	mine, err := ne.st.EnsureDevice(ctx, store.Device{UnitID: 3_912_345_678, Product: 4536, Address: "C0:FF:EE:12:34:56"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ne.st.ClaimDevice(ctx, mine.ID, 1); err != nil {
		t.Fatal(err)
	}
	theirs, err := ne.st.EnsureDevice(ctx, store.Device{UnitID: 3_912_345_679, Product: 4536, Address: "C0:FF:EE:12:34:57"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ne.st.ClaimDevice(ctx, theirs.ID, bob.ID); err != nil {
		t.Fatal(err)
	}

	bobNode := ne.nodeLogin("bob@example.invalid")
	var hr nodeproto.HelloResponse
	ne.nodeDo(bobNode.Token, http.MethodPost, "/hello", nodeproto.HelloRequest{Watches: []nodeproto.Watch{
		{Address: "C0:FF:EE:12:34:56", Unit: 3_912_345_678, Product: 4536},
		{Address: "C0:FF:EE:12:34:57", Unit: 3_912_345_679, Product: 4536},
	}}, &hr)
	if len(hr.Watches) != 2 || hr.Watches[0].Sync || hr.Watches[0].Reason != nodeproto.ReasonOther || !hr.Watches[1].Sync {
		t.Fatalf("Bob's node config: %+v", hr.Watches)
	}
	// Bob's node may not upload for the admin's watch.
	now := time.Now()
	file := fittest.File(typedef.FileMonitoringB, 3_912_345_678, now.Add(-time.Hour), 30)
	if code := upload(ne.env, bobNode.Token, 3_912_345_678, "a.fit", file); code != http.StatusForbidden {
		t.Fatalf("upload for another account's watch: %d", code)
	}
	if ne.reg.Known("C0:FF:EE:12:34:56") {
		t.Fatal("the admin's watch was routed to Bob's node")
	}

	// The admin's node: the same file twice is stored once; a wrong hash is refused.
	myNode := ne.nodeLogin("admin@example.invalid")
	ne.nodeDo(myNode.Token, http.MethodPost, "/hello", nodeproto.HelloRequest{Watches: []nodeproto.Watch{
		{Address: "C0:FF:EE:12:34:56", Unit: 3_912_345_678, Product: 4536}}}, &hr)
	if !hr.Watches[0].Sync {
		t.Fatalf("my node's config: %+v", hr.Watches)
	}
	if code := upload(ne.env, myNode.Token, 3_912_345_678, "a.fit", file); code != 200 {
		t.Fatalf("upload: %d", code)
	}
	if code := upload(ne.env, myNode.Token, 3_912_345_678, "a.fit", file); code != 200 {
		t.Fatalf("upload again: %d", code)
	}
	files, err := ne.st.RawFilesOf(ctx, mine.ID)
	if err != nil || len(files) != 1 {
		t.Fatalf("raw files %d, %v", len(files), err)
	}
	req, _ := http.NewRequest(http.MethodPut, ne.srv.URL+nodeproto.Prefix+"/files/"+hex.EncodeToString(make([]byte, 32)), bytes.NewReader(file))
	req.Header.Set("Authorization", "Bearer "+myNode.Token)
	req.Header.Set(nodeproto.HeaderUnit, "3912345678")
	req.Header.Set(nodeproto.HeaderName, "b.fit")
	resp, _ := http.DefaultTransport.RoundTrip(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("a wrong hash: %d", resp.StatusCode)
	}

	// Stored tells what the server holds.
	var sr nodeproto.StoredResponse
	ne.nodeDo(myNode.Token, http.MethodPost, "/stored", nodeproto.StoredRequest{Unit: 3_912_345_678, UIDs: []string{"a", "b"}}, &sr)
	if len(sr.Stored) != 1 || sr.Stored[0] != "a" {
		t.Fatalf("stored: %+v", sr)
	}

	// The dashboard sees the watch through the node; once the node is silent too long it is
	// offline and a sync answers 503.
	var devs []map[string]any
	_ = json.Unmarshal(admin.want(200, "GET", "/api/devices", nil), &devs)
	st, _ := devs[0]["sync"].(map[string]any)
	if st == nil || st["node"] != "laptop" || st["offline"] == true {
		t.Fatalf("device sync status: %v", devs[0])
	}
	ne.advance(nodes.OfflineAfter + time.Minute)
	if got := ne.reg.Status("C0:FF:EE:12:34:56"); !got.Offline {
		t.Fatalf("status after silence: %+v", got)
	}
	admin.want(503, "POST", "/api/devices/"+strconv.FormatInt(mine.ID, 10)+"/sync", nil)
}

// TestNodeCommands: a sync request reaches the node as a command, and its answer comes back.
func TestNodeCommands(t *testing.T) {
	ne := newNodeEnv(t)
	ctx := context.Background()
	admin := ne.admin()
	dev, err := ne.st.EnsureDevice(ctx, store.Device{UnitID: 3_912_345_678, Product: 4536, Address: "C0:FF:EE:12:34:56"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ne.st.ClaimDevice(ctx, dev.ID, 1); err != nil {
		t.Fatal(err)
	}
	lr := ne.nodeLogin("admin@example.invalid")
	ne.nodeDo(lr.Token, http.MethodPost, "/hello", nodeproto.HelloRequest{Watches: []nodeproto.Watch{
		{Address: "C0:FF:EE:12:34:56", Unit: 3_912_345_678, Product: 4536}}}, nil)

	// The node: poll, answer busy to the first sync, fine to the second.
	answers := []nodeproto.CommandResult{{Code: nodeproto.CodeBusy, Error: "busy", Busy: &nodehub.Holder{Kind: "live"}}, {}}
	go func() {
		for _, ans := range answers {
			var cr nodeproto.CommandsResponse
			ne.nodeDo(lr.Token, http.MethodGet, "/commands", nil, &cr)
			for _, c := range cr.Commands {
				if c.Kind != nodeproto.CmdSync || c.Address != "C0:FF:EE:12:34:56" {
					t.Errorf("command %+v", c)
				}
				ne.nodeDo(lr.Token, http.MethodPost, "/commands/"+c.ID, ans, nil)
			}
		}
	}()
	path := "/api/devices/" + strconv.FormatInt(dev.ID, 10) + "/sync"
	admin.want(409, "POST", path, nil)
	admin.want(202, "POST", path, nil)
}
