// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package nodeclient_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/karamble/goarmin/internal/nodeclient"
	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/nodeproto"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/syncnode"
)

// The scripted watches' unit numbers (nodehub.Scripted).
const (
	scriptedNewUnit    = uint32(3_999_000_001)
	scriptedPairedUnit = uint32(3_999_000_002)
)

// pairView is the part of the API's pairing JSON the test reads.
type pairView struct {
	ID        string `json:"id"`
	State     string `json:"state"`
	ErrorCode string `json:"error_code"`
	Node      *struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"node"`
	Watches []struct {
		Address string `json:"address"`
		Owner   string `json:"owner"`
	} `json:"watches"`
	Prompt *struct {
		Kind    string `json:"kind"`
		Passkey string `json:"passkey"`
	} `json:"prompt"`
	Ownership string `json:"ownership"`
	DeviceID  int64  `json:"device_id"`
	Adapter   *struct {
		Powered bool `json:"powered"`
	} `json:"adapter"`
}

func (s *server) pairing(t *testing.T, method, path string, body any, want int) pairView {
	t.Helper()
	code, b := s.do(t, method, "/api/pairing"+path, body)
	if code != want {
		t.Fatalf("%s /api/pairing%s: %d %s, want %d", method, path, code, b, want)
	}
	var v pairView
	if code/100 == 2 && len(b) > 0 {
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatalf("%v: %s", err, b)
		}
	}
	return v
}

// waitView polls the session until cond holds.
func (s *server) waitView(t *testing.T, id, what string, cond func(pairView) bool) pairView {
	t.Helper()
	var v pairView
	waitFor(t, what, 20*time.Second, func() bool {
		v = s.pairing(t, http.MethodGet, "/"+id, nil, http.StatusOK)
		return cond(v)
	})
	return v
}

func (v pairView) owner(addr string) string {
	for _, w := range v.Watches {
		if w.Address == addr {
			return w.Owner
		}
	}
	return ""
}

// TestPairingThroughNode: the server has no Bluetooth; Add a watch runs on the signed-in sync
// node (with the scripted Bluetooth) and the browser drives it through the server. The list's
// owners, the identified watch's owner and the claim are decided on the server; node.json and
// the first sync stay on the node. A watch another account owns is refused.
func TestPairingThroughNode(t *testing.T) {
	s := newServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Another account owns the scripted watch that is paired with the node already.
	other, err := s.st.EnsureUser(ctx, "other@example.invalid", "Other")
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := s.st.EnsureDevice(ctx, store.Device{UnitID: scriptedPairedUnit, Product: 4536, Name: "Forerunner 965",
		Address: nodehub.ScriptedPaired})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.st.ClaimDevice(ctx, theirs.ID, other.ID); err != nil {
		t.Fatal(err)
	}

	c, err := nodeclient.NewClient(s.srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	lr, err := c.Login(ctx, nodeproto.LoginRequest{Email: email, Password: password, Name: "laptop", Hostname: "laptop"})
	if err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	if err := nodeclient.SaveAuth(dataDir, nodeclient.Auth{Server: s.srv.URL, NodeID: lr.NodeID, Token: lr.Token, Account: lr.Account, Name: "laptop"}); err != nil {
		t.Fatal(err)
	}
	nctx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- nodeclient.Run(nctx, nodeclient.Options{DataDir: dataDir, Adapter: "hci0", Version: "test", Interval: time.Hour,
			Hub: func(o *nodehub.Options) { *o = nodehub.Scripted(*o) }})
	}()
	stopped := false
	defer func() {
		if !stopped {
			stop()
			<-done
		}
	}()

	// The server pairs through the node once it is heard from.
	waitFor(t, "the node offered for pairing", 20*time.Second, func() bool {
		code, b := s.do(t, http.MethodGet, "/api/pairing/computers", nil)
		return code == 200 && strings.Contains(string(b), `"name":"laptop"`) && strings.Contains(string(b), `"powered":true`)
	})
	code, b := s.do(t, http.MethodGet, "/api/me", nil)
	if code != 200 || !strings.Contains(string(b), `"pair":true`) {
		t.Fatalf("me: %d %s", code, b)
	}
	if code, b := s.do(t, http.MethodGet, "/api/bluetooth?node="+itoa(lr.NodeID), nil); code != 200 || !strings.Contains(string(b), `"power_state":"on"`) {
		t.Fatalf("the node's Bluetooth: %d %s", code, b)
	}
	if code, b := s.do(t, http.MethodGet, "/api/bluetooth?node=999", nil); code != 200 || !strings.Contains(string(b), `"available":false`) {
		t.Fatalf("an unknown computer's Bluetooth (an admin may ask for any): %d %s", code, b)
	}
	if code, b := s.do(t, http.MethodPost, "/api/bluetooth/power", map[string]any{"on": true, "node": lr.NodeID}); code != 200 {
		t.Fatalf("switching the node's Bluetooth on: %d %s", code, b)
	}

	v := s.pairing(t, http.MethodPost, "", map[string]any{"node": lr.NodeID}, http.StatusCreated)
	if v.State != nodehub.PairPrepare || v.Node == nil || v.Node.Name != "laptop" {
		t.Fatalf("start: %+v", v)
	}
	id := v.ID
	if again := s.pairing(t, http.MethodPost, "", nil, http.StatusOK); again.ID != id {
		t.Fatalf("a second start gave session %q, want the running %q", again.ID, id)
	}

	// The event stream relays the node's session.
	events := s.stream(t, id)
	defer events.Close()

	s.pairing(t, http.MethodPost, "/"+id+"/scan", nil, http.StatusOK)
	v = s.waitView(t, id, "both watches listed with their owners", func(v pairView) bool {
		return v.owner(nodehub.ScriptedNew) == nodehub.OwnerNew && v.owner(nodehub.ScriptedPaired) == nodehub.OwnerOther
	})
	if !events.has(t, `"state":"finding"`) {
		t.Fatal("the event stream did not relay the list")
	}
	if code, b := s.do(t, http.MethodPost, "/api/pairing/"+id+"/select", map[string]string{"address": nodehub.ScriptedPaired}); code != http.StatusConflict {
		t.Fatalf("selecting another account's watch: %d %s", code, b)
	}
	if code, b := s.do(t, http.MethodPost, "/api/pairing/"+id+"/confirm", map[string]bool{"match": true}); code != http.StatusConflict ||
		!strings.Contains(string(b), "not asking") {
		t.Fatalf("confirming with nothing asked: %d %s, want the hub's own 409", code, b)
	}

	s.pairing(t, http.MethodPost, "/"+id+"/select", map[string]string{"address": nodehub.ScriptedNew}, http.StatusOK)
	v = s.waitView(t, id, "the code to confirm", func(v pairView) bool { return v.State == nodehub.PairConfirm })
	if v.Prompt == nil || len(v.Prompt.Passkey) != 6 {
		t.Fatalf("prompt %+v", v.Prompt)
	}
	s.pairing(t, http.MethodPost, "/"+id+"/confirm", map[string]bool{"match": true}, http.StatusOK)
	v = s.waitView(t, id, "the watch identified", func(v pairView) bool { return v.State == nodehub.PairSetup })
	if v.Ownership != nodehub.OwnerNew {
		t.Fatalf("ownership %q, want new", v.Ownership)
	}

	v = s.pairing(t, http.MethodPost, "/"+id+"/setup", map[string]any{"nickname": " Mine ", "garmin_connect": true}, http.StatusOK)
	if v.State != nodehub.PairDone || v.DeviceID == 0 {
		t.Fatalf("setup: %+v", v)
	}

	// Claimed on the server, with its settings, routed to the node; node.json on the node.
	dev, err := s.st.DeviceByUnit(ctx, scriptedNewUnit)
	if err != nil {
		t.Fatal(err)
	}
	if dev.ID != v.DeviceID || dev.Nickname != "Mine" || dev.AfterFetch != store.AfterFetchKeep {
		t.Fatalf("device %+v", dev)
	}
	if owner, err := s.st.Owner(ctx, dev.ID, time.Now().Unix()); err != nil || owner != 1 {
		t.Fatalf("owner %d, %v", owner, err)
	}
	known, err := syncnode.KnownWatches(dataDir)
	if err != nil || known[nodehub.ScriptedNew].Unit != scriptedNewUnit {
		t.Fatalf("node.json %+v, %v", known, err)
	}
	// The first sync ran on the node (the scripted watch is never in range) and reported to the
	// server.
	waitFor(t, "the first sync's log entry", 20*time.Second, func() bool {
		runs, err := s.st.RecentSyncRuns(ctx, dev.ID, 5)
		return err == nil && len(runs) > 0 && runs[0].StartedBy == store.StartedByWizard
	})
	code, b = s.do(t, http.MethodGet, "/api/devices", nil)
	if code != 200 || !strings.Contains(string(b), `"node":"laptop"`) || !strings.Contains(string(b), `"nickname":"Mine"`) {
		t.Fatalf("devices: %d %s", code, b)
	}

	// A watch whose unit another account owns, found out only at identify: refused, nothing
	// saved on the node.
	if _, err := s.st.EnsureDevice(ctx, store.Device{UnitID: scriptedPairedUnit, Address: "02:00:00:00:00:99"}); err != nil {
		t.Fatal(err)
	}
	v = s.pairing(t, http.MethodPost, "", map[string]any{"node": lr.NodeID}, http.StatusCreated)
	id = v.ID
	s.pairing(t, http.MethodPost, "/"+id+"/scan", nil, http.StatusOK)
	s.waitView(t, id, "the paired watch listed as new", func(v pairView) bool { return v.owner(nodehub.ScriptedPaired) == nodehub.OwnerNew })
	s.pairing(t, http.MethodPost, "/"+id+"/select", map[string]string{"address": nodehub.ScriptedPaired}, http.StatusOK)
	v = s.waitView(t, id, "the session to fail", func(v pairView) bool { return v.State == nodehub.PairFailed })
	if v.ErrorCode != nodehub.CodeOwnedElsewhere {
		t.Fatalf("error code %q, want owned_elsewhere", v.ErrorCode)
	}
	known, _ = syncnode.KnownWatches(dataDir)
	if _, saved := known[nodehub.ScriptedPaired]; saved {
		t.Fatal("another account's watch was saved on the node")
	}

	// An admin's cancel ends a relayed session.
	v = s.pairing(t, http.MethodPost, "", nil, http.StatusCreated)
	if code, _ := s.do(t, http.MethodDelete, "/api/admin/pairing", nil); code != http.StatusNoContent {
		t.Fatalf("admin cancel: %d", code)
	}
	if got := s.pairing(t, http.MethodGet, "/"+v.ID, nil, http.StatusOK); got.State != nodehub.PairCanceled {
		t.Fatalf("after the admin's cancel: %s", got.State)
	}

	// The node stops during a session: it tells the server on its way out.
	v = s.pairing(t, http.MethodPost, "", nil, http.StatusCreated)
	s.pairing(t, http.MethodPost, "/"+v.ID+"/scan", nil, http.StatusOK)
	stop()
	<-done
	stopped = true
	if got := s.pairing(t, http.MethodGet, "/"+v.ID, nil, http.StatusOK); got.State != nodehub.PairCanceled || got.ErrorCode != nodehub.CodeStopping {
		t.Fatalf("after the node stopped: %s %s, want canceled by stopping", got.State, got.ErrorCode)
	}
}

// eventStream reads a pairing session's Server-Sent Events.
type eventStream struct {
	resp  *http.Response
	lines chan string
}

func (s *server) stream(t *testing.T, id string) *eventStream {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, s.srv.URL+"/api/pairing/"+id+"/events", nil)
	resp, err := s.user.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("events: %v %v", err, resp)
	}
	e := &eventStream{resp: resp, lines: make(chan string, 256)}
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			select {
			case e.lines <- sc.Text():
			default:
			}
		}
		close(e.lines)
	}()
	return e
}

// has reports whether a line with want arrives within a few seconds.
func (e *eventStream) has(t *testing.T, want string) bool {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case l, ok := <-e.lines:
			if !ok {
				return false
			}
			if strings.Contains(l, want) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func (e *eventStream) Close() { e.resp.Body.Close() }
