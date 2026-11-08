// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/product"
	"github.com/karamble/goarmin/pkg/garmin/realtime"
	"github.com/karamble/goarmin/pkg/garmin/watch"
)

const pairAddr = "AA:BB:CC:DD:EE:07"

// apiPairer is a watch that asks to confirm 123456 and identifies as a Fenix 8.
type apiPairer struct{ dir string }

func (p apiPairer) Scan(ctx context.Context, fn func([]bluez.Device) bool) error {
	for fn([]bluez.Device{{Address: pairAddr, Name: "fenix 8 - 47mm", RSSI: -62, HasRSSI: true}}) {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Millisecond):
		}
	}
	return nil
}

func (p apiPairer) Pair(_ context.Context, d bluez.Device, _ bool, pr bluez.Prompter) (bluez.Device, bool, error) {
	ok, err := pr.RequestConfirmation(d, 123456)
	if err != nil || !ok {
		return d, false, errors.New("rejected")
	}
	d.Bonded = true
	return d, true, nil
}

func (p apiPairer) Identify(_ context.Context, addr string, _ bool, step func(string)) (syncnode.Identity, error) {
	step(syncnode.StepConnected)
	step(syncnode.StepLink)
	step(syncnode.StepBattery)
	return syncnode.Identity{Address: addr, Record: syncnode.WatchRecord{Unit: 3_500_000_777, Product: 4536,
		Name: "fenix 8 - 47mm", Firmware: "23.40"}, Battery: watch.Battery{Percent: -1}}, nil
}

func (p apiPairer) Unbond(context.Context, string) error { return nil }
func (p apiPairer) Save(addr string, rec syncnode.WatchRecord) error {
	return syncnode.Save(p.dir, addr, rec)
}

// hubNode lets the test build the hub after the server, on the server's store.
type hubNode struct{ *nodehub.Hub }

func pairingEnv(t *testing.T) (*env, *hubNode) {
	t.Helper()
	n := &hubNode{}
	e := newEnvWith(t, n)
	dir := t.TempDir()
	n.Hub = nodehub.New(nodehub.Options{DataDir: dir, In: e.in, Pairer: apiPairer{dir: dir},
		AdapterState: func(context.Context) (bluez.Adapter, error) {
			return bluez.Adapter{Name: "hci0", Powered: true, Pairable: true, PowerState: "on"}, nil
		},
		Sync: func(context.Context, syncnode.Options) (syncnode.Result, error) {
			return syncnode.Result{Battery: watch.Battery{Percent: -1}}, errors.New("no real sync in this test")
		},
		Live: func(context.Context, syncnode.Options, []multilink.Service, func(realtime.Reading)) (syncnode.Result, error) {
			return syncnode.Result{}, errors.New("no live view in this test")
		}})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		n.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return e, n
}

// sessionStream reads a pairing session's event stream.
type sessionStream struct {
	t      *testing.T
	events chan map[string]any
	ended  chan struct{}
}

func (c *client) stream(id string) *sessionStream {
	c.t.Helper()
	req, _ := http.NewRequest("GET", c.e.srv.URL+"/api/pairing/"+id+"/events", nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		c.t.Fatalf("events: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	s := &sessionStream{t: c.t, events: make(chan map[string]any, 64), ended: make(chan struct{})}
	go func() {
		defer resp.Body.Close()
		defer close(s.ended)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		event := ""
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: ") && event == "session":
				var v map[string]any
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &v); err == nil {
					s.events <- v
				}
			case strings.HasPrefix(line, "data: ") && event == "end":
				return
			}
		}
	}()
	c.t.Cleanup(func() { resp.Body.Close() })
	return s
}

// until returns the first event that satisfies cond.
func (s *sessionStream) until(what string, cond func(map[string]any) bool) map[string]any {
	s.t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case v := <-s.events:
			if cond(v) {
				return v
			}
		case <-timeout:
			s.t.Fatalf("no event with %s", what)
		}
	}
}

func state(st string) func(map[string]any) bool {
	return func(v map[string]any) bool { return v["state"] == st }
}

func TestPairingRoutes(t *testing.T) {
	e, _ := pairingEnv(t)
	admin := e.admin()

	v := decodeJSON[map[string]any](t, admin.want(201, "POST", "/api/pairing", nil))
	id := v["id"].(string)
	if v["state"] != "prepare" || v["adapter"].(map[string]any)["powered"] != true || v["busy"] != nil {
		t.Fatalf("new session %v", v)
	}
	if o := v["owner"].(map[string]any); o["email"] != "admin@example.invalid" {
		t.Fatalf("owner %v", o)
	}
	if again := decodeJSON[map[string]any](t, admin.want(200, "POST", "/api/pairing", nil)); again["id"] != id {
		t.Fatalf("starting again gave another session: %v", again)
	}
	s := admin.stream(id)
	s.until("the first view", state("prepare"))

	admin.want(200, "POST", "/api/pairing/"+id+"/scan", nil)
	v = s.until("the list", func(v map[string]any) bool { return v["state"] == "finding" && len(v["watches"].([]any)) == 1 })
	w := v["watches"].([]any)[0].(map[string]any)
	if w["address"] != pairAddr || w["signal"] != "ok" || w["owner"] != "new" || w["seen_now"] != true {
		t.Fatalf("listed watch %v", w)
	}
	admin.want(409, "POST", "/api/pairing/"+id+"/confirm", map[string]bool{"match": true}) // nothing to confirm yet
	admin.want(409, "POST", "/api/pairing/"+id+"/select", map[string]string{"address": "11:22:33:44:55:66"})
	admin.want(400, "POST", "/api/pairing/"+id+"/confirm", map[string]any{})

	admin.want(200, "POST", "/api/pairing/"+id+"/select", map[string]string{"address": strings.ToLower(pairAddr)})
	v = s.until("the code", state("confirm"))
	if p := v["prompt"].(map[string]any); p["passkey"] != "123456" || p["kind"] != "confirm" || p["deadline"].(float64) < float64(time.Now().Unix()) {
		t.Fatalf("prompt %v", p)
	}
	admin.want(200, "POST", "/api/pairing/"+id+"/confirm", map[string]bool{"match": true})
	v = s.until("setup", state("setup"))
	ident := v["identified"].(map[string]any)
	if ident["model"] != product.Name(4536) || ident["battery"] != nil || v["ownership"] != "new" || v["bond"] != "code" {
		t.Fatalf("identified %v ownership %v", ident, v["ownership"])
	}
	admin.want(400, "POST", "/api/pairing/"+id+"/setup", map[string]any{"nickname": strings.Repeat("x", 61)})
	v = decodeJSON[map[string]any](t, admin.want(200, "POST", "/api/pairing/"+id+"/setup",
		map[string]any{"nickname": "Trail", "garmin_connect": true, "include_synced": true}))
	if v["state"] != "done" || v["device_id"] == nil || v["adapter"] != nil {
		t.Fatalf("after setup %v", v)
	}
	select {
	case <-s.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end")
	}
	devs := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/devices", nil))
	if len(devs) != 1 || devs[0]["nickname"] != "Trail" || devs[0]["after_fetch"] != "keep" || devs[0]["address"] != pairAddr ||
		devs[0]["id"] != v["device_id"] || devs[0]["paired"] != true {
		t.Fatalf("devices %v", devs)
	}
	if mine := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/pairing", nil)); mine["state"] != "done" {
		t.Fatalf("a reload: %v", mine)
	}

	// Another account: 404 for the admin's session, 409 while one runs, 403 for owner.
	inv := decodeJSON[map[string]any](t, admin.want(201, "POST", "/api/admin/invites", map[string]string{}))
	b := e.client()
	bu := decodeJSON[map[string]any](t, b.want(201, "POST", "/api/invites/"+strings.TrimPrefix(inv["url"].(string), public+"/invite/")+"/accept",
		map[string]string{"email": "b@example.invalid", "password": password}))
	b.want(404, "GET", "/api/pairing/"+id, nil)
	b.want(404, "GET", "/api/pairing", nil)
	b.want(403, "POST", "/api/pairing", map[string]any{"owner": 1})
	if cs := decodeJSON[[]map[string]any](t, b.want(200, "GET", "/api/pairing/computers", nil)); len(cs) != 1 || cs[0]["here"] != true {
		t.Fatalf("computers %v, want this server", cs)
	}
	b.want(403, "GET", "/api/pairing/computers?owner=1", nil)
	b.want(404, "GET", "/api/bluetooth?node=5", nil)
	b.want(404, "POST", "/api/bluetooth/power", map[string]any{"on": true, "node": 5})
	admin.want(404, "POST", "/api/pairing", map[string]any{"owner": 999})
	v = decodeJSON[map[string]any](t, admin.want(201, "POST", "/api/pairing", map[string]any{"owner": bu["id"]}))
	if o := v["owner"].(map[string]any); o["email"] != "b@example.invalid" {
		t.Fatalf("an admin adding for b: owner %v", o)
	}
	id2 := v["id"].(string)
	b.want(409, "POST", "/api/pairing", nil)
	b.want(404, "DELETE", "/api/pairing/"+id2, nil)
	b.want(404, "POST", "/api/pairing/"+id2+"/scan", nil)
	b.want(403, "DELETE", "/api/admin/pairing", nil)
	evil := e.client()
	evil.hc, evil.origin = admin.hc, "http://evil.example"
	evil.want(403, "POST", "/api/pairing/"+id2+"/scan", nil)
	running := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/admin/pairing", nil))
	if running["id"] != id2 || running["state"] != "prepare" || running["starter"].(map[string]any)["email"] != "admin@example.invalid" ||
		running["owner"].(map[string]any)["email"] != "b@example.invalid" {
		t.Fatalf("admin view of the running session: %v", running)
	}
	b.want(403, "GET", "/api/admin/pairing", nil)
	admin.want(204, "DELETE", "/api/admin/pairing", nil)
	admin.want(404, "GET", "/api/admin/pairing", nil)
	if v := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/pairing/"+id2, nil)); v["state"] != "canceled" {
		t.Fatalf("after the admin cancel: %v", v)
	}
	v = decodeJSON[map[string]any](t, b.want(201, "POST", "/api/pairing", nil))
	b.want(204, "DELETE", fmt.Sprintf("/api/pairing/%s", v["id"]), nil)
}

func TestPairingWithoutSync(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	admin.want(503, "POST", "/api/pairing", nil)
	admin.want(503, "GET", "/api/pairing", nil)

	// A node without pairing (an older or remote one) says so too.
	e = newEnvWith(t, &stubNode{})
	admin = e.admin()
	admin.want(503, "POST", "/api/pairing", nil)
}
