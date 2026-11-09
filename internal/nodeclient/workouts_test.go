// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package nodeclient_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/typedef"

	"github.com/karamble/goarmin/internal/nodeclient"
	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/nodeproto"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/fitid/fittest"
	"github.com/karamble/goarmin/pkg/garmin/fakewatch"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/multilink/memlink"
)

// TestNodeWorkoutLibrary: a node's sync hands the watch's workout to the server and takes stock
// of the watch's workouts; the library shows the workout on the watch, and removed once it is
// deleted there.
func TestNodeWorkoutLibrary(t *testing.T) {
	now := time.Now()
	s := newServer(t)
	w := fakewatch.New(fakewatch.Options{})
	wk := mesgdef.NewWorkout(nil)
	wk.WktName, wk.Sport = "Tempo", typedef.SportRunning
	st := mesgdef.NewWorkoutStep(nil)
	st.MessageIndex, st.DurationType, st.DurationValue, st.Intensity = 0, typedef.WktStepDurationTime, 900000, typedef.IntensityActive
	f := w.AddFile("FIT_TYPE_5", fittest.Build(typedef.FileWorkout, unit, now.Add(-24*time.Hour), wk.ToMesg(nil), st.ToMesg(nil)))
	open := func(_ context.Context, _ int) (multilink.Link, io.Closer, error) {
		a, b := memlink.Pipe(memlink.Options{})
		go func() { _ = w.Serve(context.Background(), b) }()
		return a, a, nil
	}
	dataDir := t.TempDir()
	c, err := nodeclient.NewClient(s.srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
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
	if _, err := s.st.EnsureDevice(ctx, store.Device{UnitID: unit, Product: 4536, Name: "Fake", Address: watchAddr}); err != nil {
		t.Fatal(err)
	}
	nctx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		_ = nodeclient.Run(nctx, nodeclient.Options{DataDir: dataDir, Adapter: "hci0", Version: "test", Interval: time.Hour,
			Hub: func(o *nodehub.Options) {
				o.Sync = func(ctx context.Context, opt syncnode.Options) (syncnode.Result, error) {
					return syncnode.SyncWith(ctx, opt, watchAddr, open, []int{0})
				}
				o.AdapterState = func(context.Context) (bluez.Adapter, error) {
					return bluez.Adapter{Name: "hci0", Powered: true, PowerState: "on"}, nil
				}
			}})
	}()
	state := func() string {
		code, body := s.do(t, http.MethodGet, "/api/workouts", nil)
		var list []struct {
			Name    string `json:"name"`
			Watches []struct {
				State     string `json:"state"`
				CheckedAt int64  `json:"checked_at"`
			} `json:"watches"`
		}
		if code != 200 || json.Unmarshal(body, &list) != nil || len(list) != 1 || list[0].Name != "Tempo" || len(list[0].Watches) != 1 ||
			list[0].Watches[0].CheckedAt == 0 {
			return ""
		}
		return list[0].Watches[0].State
	}
	waitFor(t, "the workout on the watch in the library", 20*time.Second, func() bool { return state() == "on" })
	if w.Stats().TypedListings == 0 {
		t.Fatal("the sync took no stock of the watch's workouts")
	}

	w.RemoveFile(f.UID)
	dev, err := s.st.DeviceByUnit(ctx, unit)
	if err != nil {
		t.Fatal(err)
	}
	if code, body := s.do(t, http.MethodPost, "/api/devices/"+itoa(dev.ID)+"/sync", nil); code != http.StatusAccepted {
		t.Fatalf("sync now: %d %s", code, body)
	}
	waitFor(t, "the workout removed on the watch", 20*time.Second, func() bool { return state() == "removed" })
}
