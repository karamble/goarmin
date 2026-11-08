// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package nodes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/nodeproto"
)

// TestPairingMirror: a session on a node, as the registry relays it. Only nodes that may pair
// for the owner are offered; the starter alone sees it; a stale view never replaces a newer
// one; a node going offline ends it, and the node's later views of it are cancelled there, as
// is a session the registry does not relay at all.
func TestPairingMirror(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := time.Now()
	var skew atomic.Int64
	g := New(Options{CallTimeout: 2 * time.Second, PairTouch: time.Hour,
		Now: func() time.Time { return start.Add(time.Duration(skew.Load())) }})
	laptop := NodeInfo{ID: 1, Name: "laptop", User: 1}
	pi := NodeInfo{ID: 2, Name: "pi", User: 2}
	for _, n := range []NodeInfo{laptop, pi} {
		g.Hello(n, nodeproto.HelloRequest{Adapter: &nodehub.Bluetooth{Name: "hci0", Powered: true}})
	}

	if cs := g.PairComputers(ctx, 1); len(cs) != 1 || cs[0].Node != 1 || cs[0].Adapter == nil {
		t.Fatalf("computers for account 1: %+v, want only its own node", cs)
	}
	if _, _, err := g.StartPairingOn(ctx, 1, 1, pi.ID); !errors.Is(err, ErrNoPairing) {
		t.Fatalf("pairing on another account's node: %v", err)
	}

	view := func(state string, version uint64) nodehub.PairView {
		return nodehub.PairView{ID: "s1", State: state, Starter: 1, Owner: 1, Version: version}
	}
	cmds := make(chan nodeproto.Command, 16)
	go fakeNode(ctx, g, laptop, func(c nodeproto.Command) nodeproto.CommandResult {
		switch c.Kind {
		case nodeproto.CmdPairStart:
			v := view(nodehub.PairPrepare, 1)
			return nodeproto.CommandResult{Pairing: &v, Created: true}
		case nodeproto.CmdPairScan:
			v := view(nodehub.PairWaiting, 2)
			return nodeproto.CommandResult{Pairing: &v}
		case nodeproto.CmdPairConfirm:
			return ResultFor(fmt.Errorf("%w: The watch is not asking for that now.", nodehub.ErrWrongState))
		}
		return nodeproto.CommandResult{}
	}, cmds)
	piCmds := make(chan nodeproto.Command, 16)
	go fakeNode(ctx, g, pi, func(nodeproto.Command) nodeproto.CommandResult { return nodeproto.CommandResult{} }, piCmds)

	v, created, err := g.StartPairingOn(ctx, 1, 1, 0)
	if err != nil || !created || v.Node != "laptop" || v.NodeID != 1 {
		t.Fatalf("start: %+v %v %v", v, created, err)
	}
	<-cmds
	if _, _, err := g.StartPairingOn(ctx, 2, 2, 0); !errors.Is(err, nodehub.ErrPairingActive) {
		t.Fatalf("a second account's start: %v", err)
	}
	if _, err := g.PairingByID("s1", 2); !errors.Is(err, nodehub.ErrNoSession) {
		t.Fatalf("another account read the session: %v", err)
	}
	if _, ok := g.RelayedSession(pi.ID); ok {
		t.Fatal("the session counts as the pi's")
	}
	if rv, ok := g.RelayedSession(laptop.ID); !ok || rv.Owner != 1 {
		t.Fatal("the session does not count as the laptop's")
	}

	ch, stop, err := g.PairEvents("s1", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if got := <-ch; got.State != nodehub.PairPrepare {
		t.Fatalf("first event %s", got.State)
	}
	if err := g.PairScan("s1", 1); err != nil {
		t.Fatal(err)
	}
	if c := <-cmds; c.Kind != nodeproto.CmdPairScan || c.Session != "s1" || c.User != 1 {
		t.Fatalf("scan sent as %+v", c)
	}
	g.PairPush(laptop, view(nodehub.PairFinding, 5))
	g.PairPush(laptop, view(nodehub.PairWaiting, 3)) // older: ignored
	if got, _ := g.PairingByID("s1", 1); got.State != nodehub.PairFinding {
		t.Fatalf("state %s after a stale push, want finding", got.State)
	}
	if got := latest(ch); got.State != nodehub.PairFinding {
		t.Fatalf("event %s, want finding", got.State)
	}
	err = g.PairConfirm("s1", 1, true)
	if !errors.Is(err, nodehub.ErrWrongState) || !strings.HasSuffix(err.Error(), ": The watch is not asking for that now.") {
		t.Fatalf("a wrong state across the hop: %v", err)
	}
	<-cmds

	// A view of a session the registry does not relay is cancelled on its node.
	g.PairPush(pi, nodehub.PairView{ID: "stray", State: nodehub.PairFinding, Starter: 2})
	if c := <-piCmds; c.Kind != nodeproto.CmdPairCancel || c.Session != "stray" || !c.Admin {
		t.Fatalf("stray session: %+v", c)
	}

	// The laptop goes silent: the session fails here.
	skew.Store(int64(OfflineAfter + time.Second))
	got, err := g.PairingByID("s1", 1)
	if err != nil || got.State != nodehub.PairFailed || got.ErrorCode != CodeNodeOffline {
		t.Fatalf("after the node went offline: %+v %v", got, err)
	}
	if ev := latest(ch); ev.State != nodehub.PairFailed {
		t.Fatalf("event %s, want failed", ev.State)
	}
	if _, ok := g.RunningPairing(); ok {
		t.Fatal("an ended session still runs")
	}
	// It comes back with a newer view of the session: stale, so it is cancelled there.
	g.PairPush(laptop, view(nodehub.PairConfirm, 9))
	if c := <-cmds; c.Kind != nodeproto.CmdPairCancel || c.Session != "s1" {
		t.Fatalf("the node's stale session: %+v", c)
	}
	if got, _ := g.PairingByID("s1", 1); got.State != nodehub.PairFailed {
		t.Fatalf("state %s, want failed", got.State)
	}
	if mine, ok := g.Pairing(1); !ok || mine.ID != "s1" {
		t.Fatal("the starter's ended session is not found again (a reload)")
	}
}

// latest reads the newest view a stream has, waiting a little for one.
func latest(ch <-chan nodehub.PairView) nodehub.PairView {
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		return nodehub.PairView{}
	}
}

// TestPairErrors: the hub's pairing errors keep their meaning across the hop.
func TestPairErrors(t *testing.T) {
	for _, want := range []error{nodehub.ErrNoSession, nodehub.ErrOwnedElsewhere, nodehub.ErrNotListed,
		nodehub.ErrPairingActive, nodehub.ErrStopping, nodehub.ErrCannotPair} {
		if got := resultErr(ResultFor(fmt.Errorf("wrapped: %w", want))); !errors.Is(got, want) {
			t.Errorf("%v came back as %v", want, got)
		}
	}
}

// TestPairNodeChoice: with several nodes, the one with Bluetooth on is picked, then the one heard
// from last; an admin's node may pair for anyone.
func TestPairNodeChoice(t *testing.T) {
	g := New(Options{})
	g.Hello(NodeInfo{ID: 1, Name: "a", User: 1}, nodeproto.HelloRequest{Adapter: &nodehub.Bluetooth{Powered: false}})
	g.Hello(NodeInfo{ID: 2, Name: "b", User: 1}, nodeproto.HelloRequest{Adapter: &nodehub.Bluetooth{Powered: true}})
	g.Hello(NodeInfo{ID: 3, Name: "c", User: 9, Admin: true}, nodeproto.HelloRequest{})
	g.mu.Lock()
	defer g.mu.Unlock()
	if r := g.pairNodeLocked(0, 1); r == nil || r.ID != 2 {
		t.Fatalf("picked %+v, want the node with Bluetooth on", r)
	}
	if r := g.pairNodeLocked(0, 5); r == nil || r.ID != 3 {
		t.Fatalf("picked %+v for account 5, want the admin's node", r)
	}
	if r := g.pairNodeLocked(1, 5); r != nil {
		t.Fatal("account 1's node pairs for account 5")
	}
}

// TestPairingTouchCatchesUp: a session that ended on the node while its last push got lost is
// ended here by the next touch's answer; one the node no longer has ends as gone.
func TestPairingTouchCatchesUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	g := New(Options{CallTimeout: 2 * time.Second, PairTouch: 200 * time.Millisecond})
	laptop := NodeInfo{ID: 1, Name: "laptop", User: 1}
	g.Hello(laptop, nodeproto.HelloRequest{Adapter: &nodehub.Bluetooth{Powered: true}})
	var ended atomic.Bool
	go fakeNode(ctx, g, laptop, func(c nodeproto.Command) nodeproto.CommandResult {
		v := nodehub.PairView{ID: "s1", State: nodehub.PairFinding, Starter: 1, Owner: 1, Version: 1}
		if c.Kind == nodeproto.CmdPairTouch && ended.Load() {
			v.State, v.Version = nodehub.PairExpired, 7
		}
		return nodeproto.CommandResult{Pairing: &v, Created: c.Kind == nodeproto.CmdPairStart}
	}, nil)
	if _, _, err := g.StartPairingOn(ctx, 1, 1, 0); err != nil {
		t.Fatal(err)
	}
	ch, stop, err := g.PairEvents("s1", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	<-ch
	ended.Store(true) // expired on the node; its push never arrived
	deadline := time.After(5 * time.Second)
	for {
		select {
		case v := <-ch:
			if v.State == nodehub.PairExpired {
				return
			}
		case <-deadline:
			t.Fatal("the touch did not bring the node's end")
		}
	}
}
