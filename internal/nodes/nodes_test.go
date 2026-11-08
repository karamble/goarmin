// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package nodes

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/nodeproto"
	"github.com/karamble/goarmin/pkg/garmin/realtime"
)

const addr = "C0:FF:EE:12:34:56"

// fakeNode answers the registry's commands as a node would, with answer per command kind.
func fakeNode(ctx context.Context, g *Registry, info NodeInfo, answer func(nodeproto.Command) nodeproto.CommandResult, seen chan<- nodeproto.Command) {
	for ctx.Err() == nil {
		for _, c := range g.Poll(ctx, info) {
			if seen != nil {
				seen <- c
			}
			g.Result(info, c.ID, answer(c))
		}
	}
}

func TestLiveThroughNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	g := New(Options{LiveLinger: 50 * time.Millisecond, CallTimeout: 2 * time.Second})
	info := NodeInfo{ID: 1, Name: "laptop", User: 1}
	g.Hello(info, nodeproto.HelloRequest{Watches: []nodeproto.Watch{{Address: addr, Unit: 7}}})
	if !g.Known(addr) || g.Status(addr).Node != "laptop" {
		t.Fatalf("the node's watch is not routed to it: %+v", g.Status(addr))
	}
	seen := make(chan nodeproto.Command, 16)
	busy := false
	go fakeNode(ctx, g, info, func(c nodeproto.Command) nodeproto.CommandResult {
		if c.Kind == nodeproto.CmdLiveStart && busy {
			return ResultFor(&nodehub.BusyError{Holder: nodehub.Holder{Kind: "sync"}})
		}
		return nodeproto.CommandResult{}
	}, seen)

	v, err := g.Live(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	if c := <-seen; c.Kind != nodeproto.CmdLiveStart || c.Address != addr {
		t.Fatalf("command %+v", c)
	}
	now := time.Now()
	g.LiveBatch(info, nodeproto.LiveRequest{Address: addr, Since: now, Readings: []realtime.Reading{{Name: "heart_rate", Value: 71, At: now}}})
	if r := <-v.C; r.Name != "heart_rate" || r.Value != 71 {
		t.Fatalf("reading %+v", r)
	}
	if !g.Status(addr).Live {
		t.Fatal("the status does not show the live link")
	}
	// A second viewer gets the recent readings first, and shares the link (no second command).
	v2, err := g.Live(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	if r := <-v2.C; r.Value != 71 {
		t.Fatalf("replay %+v", r)
	}
	// The node ends the link: both viewers learn why.
	g.LiveBatch(info, nodeproto.LiveRequest{Address: addr, End: &nodeproto.LiveEnd{Code: EndTimeLimit}})
	for _, x := range []*nodehub.Viewer{v, v2} {
		for range x.C {
		}
		if !errors.Is(x.Err(), nodehub.ErrTimeLimit) {
			t.Fatalf("end reason %v", x.Err())
		}
	}

	// Busy on the node: the API's 409.
	busy = true
	if _, err := g.Live(ctx, addr); !errors.Is(err, nodehub.ErrBusy) {
		t.Fatalf("busy node: %v", err)
	}
	<-seen
	busy = false

	// The last viewer leaves: after the linger the node is told to stop.
	v3, err := g.Live(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	<-seen // live_start
	v3.Stop()
	select {
	case c := <-seen:
		if c.Kind != nodeproto.CmdLiveStop {
			t.Fatalf("after the last viewer: %+v", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the node was not told to stop the live link")
	}
}

func TestOfflineNode(t *testing.T) {
	now := time.Now()
	g := New(Options{Now: func() time.Time { return now }})
	info := NodeInfo{ID: 1, Name: "laptop", User: 1}
	g.Hello(info, nodeproto.HelloRequest{Watches: []nodeproto.Watch{{Address: addr, Unit: 7}}})
	now = now.Add(OfflineAfter + time.Second)
	if st := g.Status(addr); !st.Offline || st.Node != "laptop" {
		t.Fatalf("status %+v", st)
	}
	if err := g.Sync(nodehub.Request{Addr: addr}); !errors.Is(err, ErrOffline) {
		t.Fatalf("sync on an offline node: %v", err)
	}
	if _, err := g.Bluetooth(context.Background()); !errors.Is(err, ErrNoNode) {
		t.Fatalf("bluetooth with no node online: %v", err)
	}
	g.Drop(1)
	if g.Known(addr) || g.Available() {
		t.Fatal("a revoked node's watch is still routed")
	}
}
