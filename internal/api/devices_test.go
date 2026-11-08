// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/gattlink"
	"github.com/karamble/goarmin/pkg/garmin/product"
	"github.com/karamble/goarmin/pkg/garmin/realtime"
	"github.com/karamble/goarmin/pkg/garmin/smart"
)

type stubNode struct {
	mu        sync.Mutex
	synced    []string
	requests  []nodehub.Request
	files     []fileaccess.StoredFile
	powered   bool
	forgotten []string
	purged    []uint32
	held      bool  // inside Exclusive
	liveEnd   error // why the stub's live link ends
	liveBusy  error // Live answers this instead, if set
}

func (n *stubNode) Known(addr string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, f := range n.forgotten {
		if f == addr {
			return false
		}
	}
	return true
}

func (n *stubNode) Exclusive(ctx context.Context, kind string, fn func() error) error {
	n.mu.Lock()
	n.held = true
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		n.held = false
		n.mu.Unlock()
	}()
	return fn()
}

func (n *stubNode) ForgetLocked(ctx context.Context, addr string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.held {
		return errors.New("forget outside Exclusive")
	}
	n.forgotten = append(n.forgotten, addr)
	return nil
}

func (n *stubNode) PurgeSpool(unit uint32) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.held {
		return errors.New("purge outside Exclusive")
	}
	n.purged = append(n.purged, unit)
	return nil
}

func (n *stubNode) Status(addr string) nodehub.Status {
	now := time.Now()
	return nodehub.Status{Address: addr, LastOK: true, LastStart: now.Add(-2 * time.Minute), LastEnd: now.Add(-time.Minute),
		NextSync: now.Add(29 * time.Minute)}
}

func (n *stubNode) Sync(req nodehub.Request) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.synced = append(n.synced, req.Addr)
	n.requests = append(n.requests, req)
	return nil
}

func (n *stubNode) Files(unit uint32) ([]fileaccess.StoredFile, error) { return n.files, nil }

func (n *stubNode) Bluetooth(ctx context.Context) (nodehub.Bluetooth, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	state := map[bool]string{true: "on", false: "off"}[n.powered]
	return nodehub.Bluetooth{Adapter: bluez.Adapter{Name: "hci0", Address: "00:11:22:33:44:55", Powered: n.powered, PowerState: state}}, nil
}

func (n *stubNode) PowerOn(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.powered = true
	return nil
}

func (n *stubNode) Live(ctx context.Context, addr string) (*nodehub.Viewer, error) {
	n.mu.Lock()
	busy, end := n.liveBusy, n.liveEnd
	n.mu.Unlock()
	if busy != nil {
		return nil, busy
	}
	ch := make(chan realtime.Reading, 4)
	ch <- realtime.Reading{Name: "heart_rate", Value: 76, Unit: "bpm", At: time.Now()}
	ch <- realtime.Reading{Name: "raw"} // not forwarded
	ch <- realtime.Reading{Name: "body_battery", Value: 83, Unit: "%", At: time.Now()}
	close(ch)
	return nodehub.NewViewer(ch, time.Now().Add(-time.Minute), func() {}, func() error { return end }), nil
}

// liveEvents reads a live stream to its end and returns its event and data lines.
func liveEvents(t *testing.T, c *client, path string) string {
	t.Helper()
	req, _ := http.NewRequest("GET", c.e.srv.URL+path, nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("live: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	var events []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "data: ") || strings.HasPrefix(line, "event: ") {
			events = append(events, line)
		}
	}
	return strings.Join(events, "\n")
}

func TestDevicesAndLive(t *testing.T) {
	node := &stubNode{}
	e := newEnvWith(t, node)
	admin := e.admin()
	e.healthData("admin@example.invalid")
	ctx := context.Background()
	u, _ := e.st.UserByEmail(ctx, "admin@example.invalid")
	devs, _ := e.st.UserDevices(ctx, u.ID)
	if len(devs) != 1 {
		t.Fatalf("%d devices", len(devs))
	}
	if _, err := e.st.EnsureDevice(ctx, store.Device{UnitID: devs[0].UnitID, Address: "aa:bb:cc:dd:ee:01"}); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprint(devs[0].ID)
	list := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/devices", nil))
	if list[0]["address"] != "AA:BB:CC:DD:EE:01" || list[0]["after_fetch"] != "keep" || list[0]["sync"] == nil || list[0]["current"] != true {
		t.Fatalf("devices %v", list)
	}
	if list[0]["model"] == nil || list[0]["battery"] != nil || list[0]["paused"] != false {
		t.Fatalf("device fields %v", list[0])
	}
	// Every time in the API is Unix seconds.
	sync := list[0]["sync"].(map[string]any)
	for _, k := range []string{"last_start", "last_end", "next_sync"} {
		if v, ok := sync[k].(float64); !ok || v < float64(time.Now().Add(-time.Hour).Unix()) {
			t.Fatalf("sync.%s = %#v", k, sync[k])
		}
	}
	admin.want(400, "PATCH", "/api/devices/"+id, map[string]string{"after_fetch": "burn"})
	admin.want(200, "PATCH", "/api/devices/"+id, map[string]string{"after_fetch": "archive"})
	admin.want(202, "POST", "/api/devices/"+id+"/sync", nil)
	if len(node.synced) != 1 || node.synced[0] != "AA:BB:CC:DD:EE:01" {
		t.Fatalf("sync requests %v", node.synced)
	}
	admin.want(200, "GET", "/api/devices/"+id+"/sync-runs", nil)

	// The live stream: hello, two readings (the raw one dropped), then the end and why.
	got := liveEvents(t, admin, "/api/devices/"+id+"/live")
	if !strings.HasPrefix(got, "event: hello\ndata: {\"since\":") || !strings.Contains(got, `"name":"heart_rate"`) ||
		!strings.Contains(got, `"name":"body_battery"`) || strings.Contains(got, `"raw"`) || strings.Count(got, "event: reading") != 2 ||
		!strings.HasSuffix(got, `event: end`+"\n"+`data: {"reason":"left"}`) {
		t.Fatalf("events:\n%s", got)
	}
	for err, want := range map[error]string{
		nodehub.ErrTimeLimit:   `{"reason":"time_limit"}`,
		gattlink.ErrNotInRange: `"error_code":"not_in_range"`,
	} {
		node.mu.Lock()
		node.liveEnd = err
		node.mu.Unlock()
		if got := liveEvents(t, admin, "/api/devices/"+id+"/live"); !strings.Contains(got, want) {
			t.Fatalf("end after %v:\n%s", err, got)
		}
	}
	// A busy watch: 409 with what holds Bluetooth.
	for kind, word := range map[string]string{"sync": "syncing", "pairing": "being added", "command": "goarmin command"} {
		node.mu.Lock()
		node.liveBusy = &nodehub.BusyError{Holder: nodehub.Holder{Kind: kind, Since: time.Now()}}
		node.mu.Unlock()
		res := decodeJSON[map[string]any](t, admin.want(409, "GET", "/api/devices/"+id+"/live", nil))
		if !strings.Contains(res["error"].(string), word) || res["busy"].(map[string]any)["kind"] != kind {
			t.Fatalf("busy %s: %v", kind, res)
		}
	}
	node.mu.Lock()
	node.liveBusy = nil
	node.mu.Unlock()

	// Another account: none of the device routes.
	inv := decodeJSON[map[string]any](t, admin.want(201, "POST", "/api/admin/invites", map[string]string{}))
	b := e.client()
	b.want(201, "POST", "/api/invites/"+strings.TrimPrefix(inv["url"].(string), public+"/invite/")+"/accept",
		map[string]string{"email": "b@example.invalid", "password": password})
	b.want(404, "PATCH", "/api/devices/"+id, map[string]string{"after_fetch": "keep"})
	b.want(404, "POST", "/api/devices/"+id+"/sync", nil)
	b.want(404, "GET", "/api/devices/"+id+"/sync-runs", nil)
	b.want(404, "GET", "/api/devices/"+id+"/live", nil)
	if len(node.synced) != 1 {
		t.Fatal("another account triggered a sync")
	}
}

// TestNoNode: without --with-sync the sync and live routes say so.
func TestNoNode(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	e.healthData("admin@example.invalid")
	ctx := context.Background()
	u, _ := e.st.UserByEmail(ctx, "admin@example.invalid")
	devs, _ := e.st.UserDevices(ctx, u.ID)
	id := fmt.Sprint(devs[0].ID)
	admin.want(503, "POST", "/api/devices/"+id+"/sync", nil)
	admin.want(503, "GET", "/api/devices/"+id+"/live", nil)
}

// TestFormerOwnerReadsOnly: after a hand-over the old owner still sees the watch and their own
// syncs, but can no longer act on it, and never sees the new owner's.
func TestFormerOwnerReadsOnly(t *testing.T) {
	node := &stubNode{}
	e := newEnvWith(t, node)
	admin := e.admin()
	e.healthData("admin@example.invalid")
	ctx := context.Background()
	a, _ := e.st.UserByEmail(ctx, "admin@example.invalid")
	devs, _ := e.st.UserDevices(ctx, a.ID)
	dev, err := e.st.EnsureDevice(ctx, store.Device{UnitID: devs[0].UnitID, Address: "aa:bb:cc:dd:ee:02"})
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprint(dev.ID)
	now := time.Now()
	if err := e.st.RecordSyncRun(ctx, store.SyncRun{DeviceID: dev.ID, Started: now.Add(-time.Hour), Finished: now.Add(-59 * time.Minute), OK: true}); err != nil {
		t.Fatal(err)
	}
	inv := decodeJSON[map[string]any](t, admin.want(201, "POST", "/api/admin/invites", map[string]string{}))
	b := e.client()
	b.want(201, "POST", "/api/invites/"+strings.TrimPrefix(inv["url"].(string), public+"/invite/")+"/accept",
		map[string]string{"email": "b@example.invalid", "password": password})
	bu, _ := e.st.UserByEmail(ctx, "b@example.invalid")
	if err := e.st.ReleaseDevice(ctx, dev.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ClaimDevice(ctx, dev.ID, bu.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.RecordSyncRun(ctx, store.SyncRun{DeviceID: dev.ID, Started: now.Add(time.Second), Finished: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}

	list := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/devices", nil))
	if len(list) != 1 || list[0]["current"] != false || list[0]["sync"] != nil || list[0]["owned_to"] == nil {
		t.Fatalf("the former owner's list %v", list)
	}
	runs := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/devices/"+id+"/sync-runs", nil))
	if len(runs) != 1 || runs[0]["ok"] != true {
		t.Fatalf("the former owner's runs %v", runs)
	}
	if _, ok := runs[0]["started_at"].(float64); !ok {
		t.Fatalf("started_at %#v", runs[0]["started_at"])
	}
	admin.want(404, "PATCH", "/api/devices/"+id, map[string]string{"after_fetch": "archive"})
	admin.want(404, "POST", "/api/devices/"+id+"/sync", nil)
	admin.want(404, "GET", "/api/devices/"+id+"/live", nil)

	b.want(200, "PATCH", "/api/devices/"+id, map[string]string{"after_fetch": "archive"})
	if runs := decodeJSON[[]map[string]any](t, b.want(200, "GET", "/api/devices/"+id+"/sync-runs", nil)); len(runs) != 1 || runs[0]["ok"] != false {
		t.Fatalf("the new owner's runs %v", runs)
	}
	if len(node.synced) != 0 {
		t.Fatal("a former owner triggered a sync")
	}
}

// TestDeviceRoutes: settings, the summary, the file list with deletes, the sync log and
// Bluetooth.
func TestDeviceRoutes(t *testing.T) {
	node := &stubNode{}
	e := newEnvWith(t, node)
	admin := e.admin()
	e.healthData("admin@example.invalid")
	ctx := context.Background()
	a, _ := e.st.UserByEmail(ctx, "admin@example.invalid")
	devs, _ := e.st.UserDevices(ctx, a.ID)
	dev, err := e.st.EnsureDevice(ctx, store.Device{UnitID: devs[0].UnitID, Address: "aa:bb:cc:dd:ee:03"})
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprint(dev.ID)

	// Settings: a nickname, the after-fetch mode and the pause, each optional.
	got := decodeJSON[map[string]any](t, admin.want(200, "PATCH", "/api/devices/"+id,
		map[string]any{"nickname": "  Daily  ", "paused": true}))
	if got["nickname"] != "Daily" || got["paused"] != true || got["after_fetch"] != "keep" {
		t.Fatalf("patched %v", got)
	}
	admin.want(400, "PATCH", "/api/devices/"+id, map[string]any{"nickname": strings.Repeat("x", 61)})
	admin.want(400, "PATCH", "/api/devices/"+id, map[string]any{"colour": "red"})

	// Sync now with and without a body.
	admin.want(202, "POST", "/api/devices/"+id+"/sync", nil)
	admin.want(202, "POST", "/api/devices/"+id+"/sync", map[string]any{"include_synced": true})
	if len(node.requests) != 2 || node.requests[0].IncludeSynced || !node.requests[1].IncludeSynced ||
		node.requests[1].By != store.StartedByManual {
		t.Fatalf("requests %+v", node.requests)
	}

	// The summary counts what came from the watch.
	sum := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/devices/"+id+"/summary", nil))
	if sum["files"].(float64) < 1 || sum["days"].(float64) < 1 || sum["first_day"] == "" {
		t.Fatalf("summary %v", sum)
	}

	// The file list shows the user's stored files with their state on the watch.
	uid := smart.UUID{MSB: 1, LSB: 2}
	settings := smart.UUID{MSB: 1, LSB: 3}
	for sha, f := range map[string]struct {
		uid smart.UUID
		typ uint8
	}{"feed": {uid, 4}, "beef": {settings, 2}} {
		if _, _, err := e.st.AddRawFile(ctx, store.RawFile{SHA256: sha, Size: 99, UserID: a.ID, DeviceID: dev.ID,
			Source: store.SourceBLE, SourceName: f.uid.String() + ".fit", FIT: &store.FITIdentity{Type: f.typ}}); err != nil {
			t.Fatal(err)
		}
	}
	node.files = []fileaccess.StoredFile{
		{Item: fileaccess.Item{UID: uid, TypeName: "FIT_TYPE_4", Size: 99}, Outcome: fileaccess.Kept, At: time.Now()},
		{Item: fileaccess.Item{UID: settings, TypeName: "FIT_TYPE_2"}, Outcome: fileaccess.Kept, At: time.Now()},
		{Item: fileaccess.Item{UID: smart.UUID{LSB: 9}, TypeName: "FIT_TYPE_4"}, Outcome: fileaccess.Archived}, // not stored for this user
	}
	files := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/devices/"+id+"/files", nil))
	byUID := map[string]map[string]any{}
	for _, f := range files {
		byUID[f["uid"].(string)] = f
	}
	act, set := byUID[uid.String()], byUID[settings.String()]
	if len(files) != 2 || act["state"] != "kept" || act["on_watch"] != true || act["deletable"] != true || act["fit_type"] != float64(4) {
		t.Fatalf("files %v", files)
	}
	// The watch's settings stay put: never deletable.
	if set["on_watch"] != true || set["deletable"] != false {
		t.Fatalf("a settings file is deletable: %v", set)
	}
	admin.want(400, "POST", "/api/devices/"+id+"/files/delete", map[string]any{"uids": []string{settings.String()}})
	admin.want(400, "POST", "/api/devices/"+id+"/files/delete", map[string]any{"uids": []string{smart.UUID{LSB: 9}.String()}})
	admin.want(202, "POST", "/api/devices/"+id+"/files/delete", map[string]any{"uids": []string{uid.String()}})
	if r := node.requests[len(node.requests)-1]; r.By != store.StartedByDelete || len(r.Delete) != 1 || r.Delete[0] != uid.String() {
		t.Fatalf("delete request %+v", r)
	}

	// The sync log pages by id, newest first, and filters problems.
	now := time.Now()
	for i := range 5 {
		at := now.Add(time.Duration(i-5) * time.Minute)
		if err := e.st.RecordSyncRun(ctx, store.SyncRun{DeviceID: dev.ID, Started: at, Finished: at.Add(time.Second), OK: i%2 == 0}); err != nil {
			t.Fatal(err)
		}
	}
	page := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/sync-runs?limit=3", nil))
	items := page["items"].([]any)
	if len(items) != 3 || page["next_before"] == nil {
		t.Fatalf("first page %v", page)
	}
	next := fmt.Sprint(int64(page["next_before"].(float64)))
	page2 := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/sync-runs?limit=3&before="+next, nil))
	if n := len(page2["items"].([]any)); n != 2 || page2["next_before"] != nil {
		t.Fatalf("second page %v", page2)
	}
	probs := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/sync-runs?problems=1&device="+id, nil))
	for _, it := range probs["items"].([]any) {
		if it.(map[string]any)["ok"] != false {
			t.Fatalf("problems %v", probs)
		}
	}

	// Bluetooth: its state, and switching it on (never off).
	bt := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/bluetooth", nil))
	if bt["available"] != true || bt["power_state"] != "off" {
		t.Fatalf("bluetooth %v", bt)
	}
	admin.want(400, "POST", "/api/bluetooth/power", map[string]any{"on": false})
	if bt := decodeJSON[map[string]any](t, admin.want(200, "POST", "/api/bluetooth/power", map[string]any{"on": true})); bt["powered"] != true {
		t.Fatalf("after power on %v", bt)
	}
}

// TestRemovingAWatch: unpair keeps the watch and its data; hand over ends the ownership and
// empties the spool; deleting its data needs the watch's name typed; the zip has the files;
// admins see every watch and can release one.
func TestRemovingAWatch(t *testing.T) {
	node := &stubNode{}
	e := newEnvWith(t, node)
	admin := e.admin()
	e.healthData("admin@example.invalid")
	ctx := context.Background()
	a, _ := e.st.UserByEmail(ctx, "admin@example.invalid")
	devs, _ := e.st.UserDevices(ctx, a.ID)
	dev, err := e.st.EnsureDevice(ctx, store.Device{UnitID: devs[0].UnitID, Address: "aa:bb:cc:dd:ee:04"})
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprint(dev.ID)

	// The raw files, as a zip.
	body := admin.want(200, "GET", "/api/devices/"+id+"/raw.zip", nil)
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || len(zr.File) < 2 {
		t.Fatalf("zip: %d files, %v", len(zr.File), err)
	}

	// Unpair: the watch stays the user's, without a pairing.
	got := decodeJSON[map[string]any](t, admin.want(200, "POST", "/api/devices/"+id+"/unpair", nil))
	if got["current"] != true || got["paired"] != false || got["sync"] != nil || len(node.forgotten) != 1 {
		t.Fatalf("after unpair %v (forgotten %v)", got, node.forgotten)
	}

	// The admin view lists it with its owner.
	list := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/admin/devices", nil))
	if len(list) == 0 || list[0]["owner"] == nil {
		t.Fatalf("admin devices %v", list)
	}

	// Delete its data: the name must be typed.
	admin.want(400, "POST", "/api/devices/"+id+"/delete-data", map[string]any{"confirm": "yes"})
	had := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/devices/"+id+"/summary", nil))
	gone := decodeJSON[map[string]any](t, admin.want(200, "POST", "/api/devices/"+id+"/delete-data",
		map[string]any{"confirm": " " + strings.ToUpper(product.Name(dev.Product)) + " "}))
	if gone["files"].(float64) < 2 || gone["activities"].(float64) != 1 || gone["days"] != had["days"] {
		t.Fatalf("deleted %v", gone)
	}
	if sum := decodeJSON[map[string]any](t, admin.want(200, "GET", "/api/devices/"+id+"/summary", nil)); sum["files"] != float64(0) {
		t.Fatalf("summary after the delete %v", sum)
	}

	// Hand over: ownership ends now, the spool is emptied; then it is read-only.
	got = decodeJSON[map[string]any](t, admin.want(200, "POST", "/api/devices/"+id+"/release", map[string]any{"unpair": true}))
	if got["current"] != false || got["owned_to"] == nil || len(node.purged) != 2 {
		t.Fatalf("after the hand-over %v (purged %v)", got, node.purged)
	}
	admin.want(404, "POST", "/api/devices/"+id+"/release", nil)
	admin.want(404, "POST", "/api/devices/"+id+"/unpair", nil)
	// Nobody owns it now: an admin release has nothing to end.
	admin.want(409, "POST", "/api/admin/devices/"+id+"/release", nil)
	admin.want(204, "POST", "/api/admin/devices/"+id+"/forget", nil)
}

// TestScaleIsNoWatch: a scale is listed as a scale, takes a nickname, and answers 409 to what
// only a watch does.
func TestScaleIsNoWatch(t *testing.T) {
	node := &stubNode{}
	e := newEnvWith(t, node)
	admin := e.admin()
	ctx := context.Background()
	u, _ := e.st.UserByEmail(ctx, "admin@example.invalid")
	scale, err := e.st.EnsureDevice(ctx, store.Device{UnitID: 4242, Product: 3461}) // Index Smart Scale 2
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.AssignIfUnowned(ctx, scale.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprint(scale.ID)
	list := decodeJSON[[]map[string]any](t, admin.want(200, "GET", "/api/devices", nil))
	if len(list) != 1 || list[0]["kind"] != "scale" || list[0]["model"] != "Index Smart Scale 2" {
		t.Fatalf("devices %v", list)
	}
	admin.want(200, "PATCH", "/api/devices/"+id, map[string]string{"nickname": "Bathroom"})
	for _, c := range []struct{ method, path string }{
		{"POST", "/sync"}, {"GET", "/live"}, {"GET", "/files"}, {"POST", "/unpair"},
	} {
		admin.want(409, c.method, "/api/devices/"+id+c.path, nil)
	}
	admin.want(409, "PATCH", "/api/devices/"+id, map[string]string{"after_fetch": "archive"})
	admin.want(409, "POST", "/api/devices/"+id+"/release", map[string]bool{"unpair": true})
	if len(node.synced) != 0 {
		t.Fatal("a scale was synced")
	}
}
