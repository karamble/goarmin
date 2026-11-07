// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package watch_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/karamble/goarmin/pkg/garmin/fakewatch"
	"github.com/karamble/goarmin/pkg/garmin/gfdi"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/multilink/memlink"
	"github.com/karamble/goarmin/pkg/garmin/smart"
	"github.com/karamble/goarmin/pkg/garmin/watch"
)

func ctxT(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func serve(t *testing.T, w *fakewatch.Watch) *memlink.Link {
	t.Helper()
	a, b := memlink.Pipe(memlink.Options{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = w.Serve(context.Background(), b)
	}()
	t.Cleanup(func() {
		a.Close()
		<-done
	})
	return a
}

func TestHandshakeWithFakeFenix8(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	w := fakewatch.New(fakewatch.Options{AskTime: true})
	link := serve(t, w)
	host := gfdi.GarminConnectHost("testhost", "Linux", "goarmin")
	c, err := watch.Handshake(ctxT(t), link, watch.Options{Host: &host, Location: berlin})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Device.ProductNumber != 4536 || c.Device.MaxPacketSize != 4000 || c.Device.SoftwareVersionString() != "23.40" {
		t.Fatalf("device %+v", c.Device)
	}
	if !c.GFDI.Reliable() || c.GFDI.Handle() != 0x80 {
		t.Fatalf("GFDI handle 0x%02X reliable %v", c.GFDI.Handle(), c.GFDI.Reliable())
	}
	if c.Session.MaxPacket() != 4000 || !c.Session.Compact() {
		t.Fatalf("session max packet %d compact %v", c.Session.MaxPacket(), c.Session.Compact())
	}
	if !c.WatchCaps.Has(gfdi.CapSync2) || len(c.FileTypes) != 16 || c.FileTypes[1].Name != "FIT_TYPE_32" {
		t.Fatalf("caps %v, %d file types", c.WatchCaps.Has(gfdi.CapSync2), len(c.FileTypes))
	}
	st := w.Stats()
	if !bytes.Equal(st.HostCaps, gfdi.GarminConnectHostCapabilities()) {
		t.Fatalf("watch got host caps % X", st.HostCaps)
	}
	if !bytes.Equal(st.HostInfo, host.ReplyData()) {
		t.Fatalf("watch got host info % X", st.HostInfo)
	}
	// The watch's CURRENT_TIME_REQUEST was answered.
	deadline := time.Now().Add(5 * time.Second)
	for len(w.Stats().TimeReply) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	tr := w.Stats().TimeReply
	if len(tr) != 20 || binary.LittleEndian.Uint32(tr) != 0x001B3C91 {
		t.Fatalf("time reply % X", tr)
	}
	got := int64(binary.LittleEndian.Uint32(tr[4:])) + watch.GarminEpoch
	if d := time.Now().Unix() - got; d < 0 || d > 10 {
		t.Fatalf("time reply is %d s off", d)
	}
}

func TestHandshakePlainGFDI(t *testing.T) {
	w := fakewatch.New(fakewatch.Options{})
	c, err := watch.Handshake(ctxT(t), serve(t, w), watch.Options{PlainGFDI: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// Without MLR the Fenix 8 announces 400 (docs/protocol/06).
	if c.GFDI.Reliable() || c.Device.MaxPacketSize != 400 {
		t.Fatalf("reliable %v, max packet %d", c.GFDI.Reliable(), c.Device.MaxPacketSize)
	}
}

func TestHandshakeRequiresSync2(t *testing.T) {
	caps := fakewatch.Fenix8Capabilities()
	caps[gfdi.CapSync2/8] &^= 1 << (gfdi.CapSync2 % 8)
	w := fakewatch.New(fakewatch.Options{Capabilities: caps})
	if _, err := watch.Handshake(ctxT(t), serve(t, w), watch.Options{}); !errors.Is(err, watch.ErrNoSync2) {
		t.Fatalf("got %v, want ErrNoSync2", err)
	}
}

func TestHandshakeSilentWatch(t *testing.T) {
	a, b := memlink.Pipe(memlink.Options{})
	defer a.Close()
	go func() { // reads everything, answers nothing
		for {
			select {
			case <-b.Recv():
			case <-b.Done():
				return
			}
		}
	}()
	start := time.Now()
	_, err := watch.Handshake(ctxT(t), a, watch.Options{HandshakeTimeout: 300 * time.Millisecond})
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("got %v after %v", err, time.Since(start))
	}
}

// Dial skips a pair that does not open and a client ID the watch does not answer.
func TestDialFallback(t *testing.T) {
	w := fakewatch.New(fakewatch.Options{})
	open := func(_ context.Context, pair int) (multilink.Link, io.Closer, error) {
		if pair == 0 {
			return nil, nil, errors.New("no such characteristic")
		}
		a, b := memlink.Pipe(memlink.Options{})
		go func() { _ = w.Serve(context.Background(), b) }()
		return a, a, nil
	}
	c, closer, tries, err := watch.Dial(ctxT(t), open, []int{0, 1}, nil, watch.Options{})
	if err != nil {
		t.Fatalf("%v (%+v)", err, tries)
	}
	defer closer.Close()
	defer c.Close()
	if len(tries) != 2 || tries[0].Err == nil || tries[1].Pair != 1 || tries[1].Client != multilink.ClientGadgetbridge {
		t.Fatalf("attempts %+v", tries)
	}
}

func TestCurrentTimeReply(t *testing.T) {
	now := time.Now()
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	b := watch.CurrentTimeReply(7, now, berlin)
	if len(b) != 20 || binary.LittleEndian.Uint32(b) != 7 {
		t.Fatalf("reply % X", b)
	}
	if got := binary.LittleEndian.Uint32(b[4:]); got != watch.GarminTime(now) {
		t.Fatalf("now %d, want %d", got, watch.GarminTime(now))
	}
	_, off := now.In(berlin).Zone()
	if got := int32(binary.LittleEndian.Uint32(b[8:])); got != int32(off) {
		t.Fatalf("offset %d, want %d", got, off)
	}
	start := time.Unix(int64(binary.LittleEndian.Uint32(b[12:]))+watch.GarminEpoch, 0).In(berlin)
	end := time.Unix(int64(binary.LittleEndian.Uint32(b[16:]))+watch.GarminEpoch, 0).In(berlin)
	// Field 12 is the next DST start, field 16 the next DST end, both within a year.
	for name, tt := range map[string]time.Time{"start": start, "end": end} {
		if !tt.After(now) || tt.After(now.AddDate(1, 0, 1)) {
			t.Fatalf("next DST %s %v is not within the coming year", name, tt)
		}
	}
	if !start.IsDST() || start.Add(-time.Hour).IsDST() {
		t.Fatalf("%v is not a DST start", start)
	}
	if end.IsDST() || !end.Add(-time.Hour).IsDST() {
		t.Fatalf("%v is not a DST end", end)
	}
	// In winter the start comes first, in summer the end.
	if now.In(berlin).IsDST() != end.Before(start) {
		t.Fatalf("order of start %v and end %v does not fit DST=%v now", start, end, now.In(berlin).IsDST())
	}

	// A zone without DST sends zeros.
	u := watch.CurrentTimeReply(0, now, time.UTC)
	if !bytes.Equal(u[8:], make([]byte, 12)) {
		t.Fatalf("UTC reply % X", u[8:])
	}
}

func TestParseFileTypes(t *testing.T) {
	data := []byte{2, 128, 32, 11}
	data = append(data, "FIT_TYPE_32"...)
	data = append(data, 255, 245, 3, 'E', 'S', 'R')
	got := watch.ParseFileTypes(data)
	if len(got) != 2 || got[0] != (watch.FileType{DataType: 128, SubType: 32, Name: "FIT_TYPE_32"}) || got[1].Name != "ESR" {
		t.Fatalf("%+v", got)
	}
	if got := watch.ParseFileTypes(data[:8]); len(got) != 0 {
		t.Fatalf("truncated list gave %+v", got)
	}
}

// The host announces its features like Garmin Connect and keeps the watch's answer; a
// WellnessThresholdMetNotification from the watch is counted, not discarded.
func TestHandshakeAnnouncesFeatures(t *testing.T) {
	w := fakewatch.New(fakewatch.Options{})
	link := serve(t, w)
	c, err := watch.Handshake(ctxT(t), link, watch.Options{Features: &smart.GarminConnectFeatures})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if w.Stats().FeatureRequests != 1 {
		t.Fatalf("%d feature requests", w.Stats().FeatureRequests)
	}
	if c.Features == nil || !c.Features.WellnessSupport || !c.Features.LifestyleLoggingSupport || !c.Features.CustomFlagSupport {
		t.Fatalf("features %+v", c.Features)
	}

	w2 := fakewatch.New(fakewatch.Options{})
	c2, err := watch.Handshake(ctxT(t), serve(t, w2), watch.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if w2.Stats().FeatureRequests != 0 || c2.Features != nil {
		t.Fatal("features announced without Options.Features")
	}
}
