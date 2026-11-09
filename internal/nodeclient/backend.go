// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package nodeclient

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/nodeproto"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
)

// Remote is the hub's Backend on a sync node: the server over HTTP. It keeps the server's last
// config for each watch (node-cache.json), so syncs go on while the server is away; files then
// wait in the spool, unflagged on the watch, until the server takes them.
type Remote struct {
	c       *Client
	dataDir string
	log     *slog.Logger

	mu      sync.Mutex
	config  map[string]nodeproto.WatchConfig // by address
	pending []nodeproto.RunRequest           // sync runs the server did not take yet
}

var (
	_ nodehub.Backend     = (*Remote)(nil)
	_ nodehub.Hander      = (*Remote)(nil)
	_ nodehub.PairBackend = (*Remote)(nil)
)

// NewRemote makes the backend; it starts from the cached config, if any.
func NewRemote(c *Client, dataDir string, log *slog.Logger) *Remote {
	r := &Remote{c: c, dataDir: dataDir, log: log, config: map[string]nodeproto.WatchConfig{}}
	if hr, err := loadCache(dataDir); err == nil {
		r.setConfig(hr)
	}
	return r
}

// setConfig takes a hello's answer as the watches' config.
func (r *Remote) setConfig(hr nodeproto.HelloResponse) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config = map[string]nodeproto.WatchConfig{}
	for _, w := range hr.Watches {
		r.config[w.Address] = w
	}
}

// remember keeps a hello's answer for later starts.
func (r *Remote) remember(hr nodeproto.HelloResponse) {
	r.setConfig(hr)
	if err := writeJSON(cachePath(r.dataDir), hr); err != nil {
		r.log.Warn("node: keeping the server's config", "err", err)
	}
}

// ErrNoConfig is a sync's error before the server ever said how to sync a watch.
var ErrNoConfig = errors.New("the server has not said how to sync this watch yet (it was not reachable since the node started)")

// Watch implements nodehub.Backend.
func (r *Remote) Watch(_ context.Context, addr string, rec syncnode.WatchRecord) (nodehub.Watch, error) {
	r.mu.Lock()
	cfg, ok := r.config[addr]
	r.mu.Unlock()
	if !ok {
		return nodehub.Watch{}, ErrNoConfig
	}
	w := nodehub.Watch{Addr: addr, Unit: rec.Unit, DeviceID: cfg.DeviceID, Owner: cfg.Owner, Paused: cfg.Paused,
		AfterFetch: cfg.AfterFetch}
	if !cfg.Sync {
		switch cfg.Reason {
		case nodeproto.ReasonOther:
			w.NoOwner = errors.New("this watch belongs to an account this node does not sync for")
		default:
			w.NoOwner = nodehub.ErrNoOwner
		}
		return w, nil
	}
	if cfg.Zone != "" {
		if loc, err := time.LoadLocation(cfg.Zone); err == nil {
			w.Zone = loc
		}
	}
	return w, nil
}

// Stored implements nodehub.Backend.
func (r *Remote) Stored(ctx context.Context, w nodehub.Watch, uid string) bool {
	got, err := r.c.Stored(ctx, w.Unit, []string{uid})
	return err == nil && len(got) == 1 && got[0] == uid
}

// Hand implements nodehub.Hander: one pulled file to the server (FIT files and the rest: the
// server keeps every file the watch handed out).
func (r *Remote) Hand(ctx context.Context, w nodehub.Watch, f fileaccess.StoredFile) error {
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return err
	}
	_, err = r.c.Upload(ctx, w.Unit, filepath.Base(f.Path), b)
	return err
}

// Import implements nodehub.Backend: it hands over every file still in the spool (what the
// server did not take during an earlier sync). A handed file leaves the spool. It stops at the
// first file the server cannot take.
func (r *Remote) Import(ctx context.Context, w nodehub.Watch, dir string) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		res, err := r.c.Upload(ctx, w.Unit, filepath.Base(p), b)
		var he *HTTPError
		if errors.As(err, &he) && he.Status == 409 {
			return nil // waits on the node (nobody owned the watch then): leave it, go on
		}
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		if res.New {
			n++
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			r.log.Warn("node: removing a handed file from the spool", "path", p, "err", err)
		}
		return nil
	})
	return n, err
}

// Finish implements nodehub.Backend: the run goes to the server, or waits for it.
func (r *Remote) Finish(ctx context.Context, w nodehub.Watch, out nodehub.Outcome) error {
	req := nodeproto.RunRequest{Unit: w.Unit, Run: out.Run, DeleteSupport: out.DeleteSupport}
	if out.Battery.Percent >= 0 && !out.Battery.At.IsZero() {
		p := out.Battery.Percent
		req.Battery, req.BatteryAt = &p, out.Battery.At
	}
	if err := r.c.Run(ctx, req); err != nil {
		r.mu.Lock()
		if len(r.pending) < 500 {
			r.pending = append(r.pending, req)
		}
		r.mu.Unlock()
		return fmt.Errorf("the sync log entry waits for the server: %w", err)
	}
	return nil
}

var _ nodehub.Inventorier = (*Remote)(nil)

// Inventory implements nodehub.Inventorier: what the watch keeps goes to the server, best effort
// (the next sync's inventory replaces it anyway). A server from before the workout library
// answers 404, which is not an error.
func (r *Remote) Inventory(ctx context.Context, w nodehub.Watch, inv syncnode.Inventory) error {
	req := nodeproto.InventoryRequest{Unit: w.Unit, Types: inv.Types, At: inv.At, Items: []nodeproto.InventoryItem{}}
	for _, it := range inv.Items {
		req.Items = append(req.Items, nodeproto.InventoryItem{UID: it.UID.String(), Type: it.TypeName, Size: it.Size})
	}
	err := r.c.Inventory(ctx, req)
	var he *HTTPError
	if errors.As(err, &he) && he.Status == http.StatusNotFound {
		return nil
	}
	return err
}

// flush sends the sync runs the server missed.
func (r *Remote) flush(ctx context.Context) {
	r.mu.Lock()
	runs := r.pending
	r.pending = nil
	r.mu.Unlock()
	for i, req := range runs {
		if err := r.c.Run(ctx, req); err != nil {
			r.mu.Lock()
			r.pending = append(runs[i:], r.pending...)
			r.mu.Unlock()
			return
		}
	}
}

// History implements nodehub.Backend.
func (r *Remote) History(ctx context.Context, rec syncnode.WatchRecord, _ int) ([]store.SyncRun, error) {
	h, err := r.c.History(ctx, rec.Unit)
	return h.Runs, err
}

// OwnerFlags implements nodehub.PairBackend: the server decides whose a listed watch is.
func (r *Remote) OwnerFlags(ctx context.Context, owner int64, ws []nodehub.PairCandidate) (map[string]string, error) {
	return r.c.PairOwners(ctx, nodeproto.PairOwnersRequest{Owner: owner, Watches: ws})
}

// Identified implements nodehub.PairBackend.
func (r *Remote) Identified(ctx context.Context, owner int64, unit uint32) (nodehub.IdentifiedOwner, error) {
	return r.c.PairIdentified(ctx, nodeproto.PairIdentifiedRequest{Owner: owner, Unit: unit})
}

// Claim implements nodehub.PairBackend: the server adds the watch to the owner's account and
// says how to sync it, so its first sync runs before the next hello.
func (r *Remote) Claim(ctx context.Context, c nodehub.PairClaim) (int64, error) {
	res, err := r.c.PairClaim(ctx, c)
	var he *HTTPError
	if errors.As(err, &he) && he.Status == http.StatusConflict {
		return 0, nodehub.ErrOwnedElsewhere
	}
	if err != nil {
		return 0, err
	}
	r.mu.Lock()
	r.config[res.Watch.Address] = res.Watch
	r.mu.Unlock()
	return res.DeviceID, nil
}
