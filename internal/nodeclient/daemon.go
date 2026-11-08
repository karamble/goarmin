// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package nodeclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/nodeproto"
	"github.com/karamble/goarmin/internal/nodes"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/garmin/realtime"
)

// Options configures a sync node.
type Options struct {
	DataDir  string
	Adapter  string
	Version  string
	Hostname string
	Logger   *slog.Logger
	// Features and NoArchive go to the hub (nodehub.Options).
	Features  bool
	NoArchive bool
	// Interval between scheduled syncs (30 min).
	Interval time.Duration
	// HelloEvery is how often the node says hello unasked (5 min).
	HelloEvery time.Duration
	// Hub, if set, adjusts the hub's options before it starts (tests replace the Bluetooth side).
	Hub func(*nodehub.Options)
}

// ErrNotSignedIn is returned when the node has no sign-in yet.
var ErrNotSignedIn = errors.New("this computer is not signed in to a server yet (goarmin node login -server URL)")

// node is a running sync node.
type node struct {
	opt   Options
	log   *slog.Logger
	c     *Client
	be    *Remote
	hub   *nodehub.Hub
	hello chan struct{}

	mu      sync.Mutex
	lives   map[string]*nodehub.Viewer
	online  bool
	stopped bool
}

// Run runs the node until ctx ends or the server revokes its sign-in.
func Run(ctx context.Context, opt Options) error {
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opt.Interval <= 0 {
		opt.Interval = 30 * time.Minute
	}
	if opt.HelloEvery <= 0 {
		opt.HelloEvery = 5 * time.Minute
	}
	if opt.Hostname == "" {
		opt.Hostname, _ = os.Hostname()
	}
	auth, err := LoadAuth(opt.DataDir)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotSignedIn
	}
	if err != nil {
		return err
	}
	c, err := NewClient(auth.Server, auth.Token)
	if err != nil {
		return err
	}
	n := &node{opt: opt, log: opt.Logger, c: c, be: NewRemote(c, opt.DataDir, opt.Logger), hello: make(chan struct{}, 1),
		lives: map[string]*nodehub.Viewer{}}
	hopt := nodehub.Options{DataDir: opt.DataDir, Adapter: opt.Adapter, Interval: opt.Interval, Backend: n.be,
		Logger: opt.Logger, Features: opt.Features, NoArchive: opt.NoArchive}
	if opt.Hub != nil {
		opt.Hub(&hopt)
	}
	n.hub = nodehub.New(hopt)

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	n.sayHello(ctx) // before the hub's first sync, so it has the server's config
	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); n.hub.Run(ctx) }()
	go func() { defer wg.Done(); n.helloLoop(ctx) }()
	go func() { defer wg.Done(); n.progressLoop(ctx) }()
	go func() {
		defer wg.Done()
		if err := n.commandLoop(ctx); err != nil {
			cancel(err)
		}
	}()
	opt.Logger.Info("node: running", "server", c.Server(), "node", auth.Name, "account", auth.Account, "no_archive", opt.NoArchive)
	<-ctx.Done()
	n.mu.Lock()
	n.stopped = true
	for _, v := range n.lives {
		v.Stop()
	}
	n.mu.Unlock()
	wg.Wait()
	if err := context.Cause(ctx); errors.Is(err, ErrRevoked) {
		return err
	}
	return nil
}

// sayHello reports the node's watches and takes the server's config for them.
func (n *node) sayHello(ctx context.Context) {
	watches, err := syncnode.KnownWatches(n.opt.DataDir)
	if err != nil {
		n.log.Warn("node: reading node.json", "err", err)
		return
	}
	req := nodeproto.HelloRequest{Version: n.opt.Version, Hostname: n.opt.Hostname, Watches: []nodeproto.Watch{}}
	addrs := make([]string, 0, len(watches))
	for a := range watches {
		addrs = append(addrs, a)
	}
	sort.Strings(addrs)
	for _, a := range addrs {
		w := watches[a]
		req.Watches = append(req.Watches, nodeproto.Watch{Address: a, Unit: w.Unit, Product: w.Product, Name: w.Name, Firmware: w.Firmware})
	}
	actx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if b, err := n.hub.Bluetooth(actx); err == nil {
		req.Adapter = &b
	}
	cancel()
	hctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	hr, err := n.c.Hello(hctx, req)
	if err != nil {
		n.setOnline(false, err)
		return
	}
	n.be.remember(hr)
	n.setOnline(true, nil)
	n.be.flush(ctx)
}

// setOnline logs when the server comes and goes.
func (n *node) setOnline(on bool, err error) {
	n.mu.Lock()
	was := n.online
	n.online = on
	n.mu.Unlock()
	switch {
	case on && !was:
		n.log.Info("node: the server answers", "server", n.c.Server())
	case !on && was:
		n.log.Warn("node: the server does not answer; syncs go on, files wait on the watch", "err", err)
	case !on && errors.Is(err, ErrRevoked):
		n.log.Error("node: " + err.Error())
	}
}

func (n *node) helloLoop(ctx context.Context) {
	t := time.NewTicker(n.opt.HelloEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-n.hello:
		}
		n.sayHello(ctx)
	}
}

// askHello makes the hello loop say hello soon.
func (n *node) askHello() {
	select {
	case n.hello <- struct{}{}:
	default:
	}
}

// commandLoop long-polls the server for commands and runs each.
func (n *node) commandLoop(ctx context.Context) error {
	wait := time.Second
	for {
		cmds, err := n.c.Commands(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, ErrRevoked) {
			n.setOnline(false, err)
			return err
		}
		if err != nil {
			n.setOnline(false, err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(wait):
			}
			wait = min(wait*2, time.Minute)
			continue
		}
		if wait > time.Second { // back after an outage: hello first, it refreshes the config
			n.askHello()
		}
		wait = time.Second
		n.setOnline(true, nil)
		for _, cmd := range cmds {
			go n.run(ctx, cmd)
		}
	}
}

// run carries out one command and answers it.
func (n *node) run(ctx context.Context, cmd nodeproto.Command) {
	var res nodeproto.CommandResult
	switch cmd.Kind {
	case nodeproto.CmdSync:
		res = nodes.ResultFor(n.hub.Sync(nodehub.Request{Addr: cmd.Address, By: cmd.By, IncludeSynced: cmd.IncludeSynced, Delete: cmd.Delete}))
	case nodeproto.CmdFiles:
		files, err := n.hub.Files(cmd.Unit)
		res = nodes.ResultFor(err)
		res.Files = files
	case nodeproto.CmdForget:
		res = nodes.ResultFor(n.hub.Unpair(ctx, cmd.Address))
		n.askHello()
	case nodeproto.CmdPurge:
		res = nodes.ResultFor(n.hub.Exclusive(ctx, "purge", func() error { return n.hub.PurgeSpool(cmd.Unit) }))
	case nodeproto.CmdPowerOn:
		res = nodes.ResultFor(n.hub.PowerOn(ctx))
	case nodeproto.CmdLiveStart:
		res = nodes.ResultFor(n.liveStart(ctx, cmd.Address))
	case nodeproto.CmdLiveStop:
		n.mu.Lock()
		v := n.lives[cmd.Address]
		n.mu.Unlock()
		if v != nil {
			v.Stop()
		}
	case nodeproto.CmdHello:
		n.askHello()
	default:
		res = nodeproto.CommandResult{Code: nodeproto.CodeFailed, Error: "this node does not know the command " + cmd.Kind}
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := n.c.Result(rctx, cmd.ID, res); err != nil {
		n.log.Info("node: answering a command", "kind", cmd.Kind, "err", err)
	}
}

// liveStart opens the watch's live link and relays its readings to the server.
func (n *node) liveStart(ctx context.Context, addr string) error {
	n.mu.Lock()
	if n.stopped {
		n.mu.Unlock()
		return nodehub.ErrStopping
	}
	if old := n.lives[addr]; old != nil {
		n.mu.Unlock()
		return nil // already relayed: the server's viewers share it
	}
	n.mu.Unlock()
	v, err := n.hub.Live(ctx, addr)
	if err != nil {
		return err
	}
	n.mu.Lock()
	n.lives[addr] = v
	n.mu.Unlock()
	go n.relay(ctx, addr, v)
	return nil
}

// relay sends a live link's readings to the server, a batch a second, and how it ended.
func (n *node) relay(ctx context.Context, addr string, v *nodehub.Viewer) {
	defer func() {
		n.mu.Lock()
		if n.lives[addr] == v {
			delete(n.lives, addr)
		}
		n.mu.Unlock()
	}()
	send := func(rs []realtime.Reading, end *nodeproto.LiveEnd) {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := n.c.Live(sctx, nodeproto.LiveRequest{Address: addr, Since: v.Since, Readings: rs, End: end}); err != nil {
			n.log.Info("node: relaying live values", "err", err)
		}
	}
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var batch []realtime.Reading
	for {
		select {
		case r, ok := <-v.C:
			if !ok {
				end := nodes.LiveEndFor(v.Err())
				send(batch, &end)
				return
			}
			batch = append(batch, r)
		case <-t.C:
			if len(batch) > 0 {
				send(batch, nil)
				batch = nil
			}
		}
	}
}

// progressLoop reports the node's state when it changes (every second while a sync or live
// link runs), and every 20 s regardless.
func (n *node) progressLoop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var last []byte
	var sent time.Time
	var adapter *nodehub.Bluetooth
	var adapterAt time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		watches, err := syncnode.KnownWatches(n.opt.DataDir)
		if err != nil {
			continue
		}
		req := nodeproto.ProgressRequest{Status: []nodehub.Status{}}
		for a := range watches {
			req.Status = append(req.Status, n.hub.Status(a))
		}
		sort.Slice(req.Status, func(i, j int) bool { return req.Status[i].Address < req.Status[j].Address })
		if h, ok := n.hub.Busy(); ok {
			req.Busy = &h
		}
		if time.Since(adapterAt) > 30*time.Second {
			actx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if b, err := n.hub.Bluetooth(actx); err == nil {
				b.Busy = nil
				adapter = &b
			}
			cancel()
			adapterAt = time.Now()
		}
		req.Adapter = adapter
		b, _ := json.Marshal(req)
		if string(b) == string(last) && time.Since(sent) < 20*time.Second {
			continue
		}
		pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = n.c.Progress(pctx, req)
		cancel()
		if err == nil {
			last, sent = b, time.Now()
		}
	}
}
