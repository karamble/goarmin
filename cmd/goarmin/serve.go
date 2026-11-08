// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"github.com/karamble/goarmin/internal/api"
	"github.com/karamble/goarmin/internal/config"
	"github.com/karamble/goarmin/internal/importer"
	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/tiles"
	"github.com/karamble/goarmin/internal/usbsource"
	"github.com/karamble/goarmin/internal/webui"
	"github.com/karamble/goarmin/pkg/auth"
)

func runServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	listen := fs.String("listen", config.String("GOARMIN_LISTEN", "127.0.0.1:8642"), "address to listen on")
	public := fs.String("public-url", config.String("GOARMIN_PUBLIC_URL", ""), "URL users open (passkeys are bound to its host); default http://localhost:<port>")
	dataDir := fs.String("data", config.DataDir(), "data directory (database and raw files)")
	verbose := fs.Bool("v", false, "log every request error in detail")
	withSync := fs.Bool("with-sync", false, "also run the sync role here: sync this computer's watches on a schedule, import their files, stream live values")
	tileURL := fs.String("tile-url", config.String("GOARMIN_TILE_URL", ""), "map tiles: a preset (jawg-matrix, jawg-dark) or a URL template with {z}, {x}, {y} (and optionally {s}, {token}); empty: no map tiles, routes are drawn on their own. OpenStreetMap's own servers refuse goarmin (it never identifies itself). The access token comes from GOARMIN_TILE_TOKEN")
	adapter := fs.String("adapter", config.String("GOARMIN_ADAPTER", "hci0"), "Bluetooth adapter (with -with-sync)")
	interval := fs.Duration("sync-interval", 30*time.Minute, "time between scheduled syncs (with -with-sync)")
	fakePairer := fs.Bool("fake-pairer", false, "development: scripted Bluetooth with made-up watches, for trying Add a watch (with -with-sync, never on the default data directory)")
	noUSB := fs.Bool("no-usb", false, "do not look for watches plugged in by USB (imports of uploaded files still work)")
	importMaxGB := fs.Int("import-max-gb", 0, "largest upload in GB (default 8, or GOARMIN_IMPORT_MAX_GB)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *importMaxGB == 0 {
		n, err := config.Int("GOARMIN_IMPORT_MAX_GB", 8)
		if err != nil || n <= 0 {
			fmt.Fprintln(stderr, "goarmin serve: GOARMIN_IMPORT_MAX_GB must be a positive number of GB")
			return 2
		}
		*importMaxGB = n
	}
	if *fakePairer && (!*withSync || samePath(*dataDir, config.DataDir())) {
		fmt.Fprintln(stderr, "goarmin serve: -fake-pairer needs -with-sync and a scratch -data directory (not "+config.DataDir()+")")
		return 2
	}
	firstSession, err := config.Bool("GOARMIN_FIRST_SESSION", false)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin serve:", err)
		return 2
	}
	// Announce the host's features as Garmin Connect does (docs/protocol/06, Feature
	// capabilities); GOARMIN_FEATURES=0 leaves it out.
	features, err := config.Bool("GOARMIN_FEATURES", true)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin serve:", err)
		return 2
	}
	if *public == "" {
		_, port, err := net.SplitHostPort(*listen)
		if err != nil {
			fmt.Fprintln(stderr, "goarmin serve: -listen must be host:port")
			return 2
		}
		*public = "http://localhost:" + port
	}
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))
	ctx, stop := signalContext()
	defer stop()
	in, closeData, err := openData(ctx, *dataDir, log)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin serve:", err)
		return 1
	}
	defer closeData()
	ui, err := webui.Handler()
	if err != nil {
		fmt.Fprintln(stderr, "goarmin serve:", err)
		return 1
	}
	setupToken := ""
	if ok, err := in.Store.CanLogIn(ctx); err != nil {
		fmt.Fprintln(stderr, "goarmin serve:", err)
		return 1
	} else if !ok {
		setupToken, _ = auth.NewToken()
		fmt.Fprintf(stdout, "First run: create the admin account at\n  %s/setup?token=%s\n(the token is valid until this process ends)\n", *public, setupToken)
	}
	opt := api.Options{Store: in.Store, Blobs: in.Blobs, PublicURL: *public, SetupToken: setupToken, UI: ui, Logger: log,
		Version: version}
	switch tc, err := tiles.New(tiles.Options{DataDir: *dataDir, Upstream: *tileURL, Token: config.String("GOARMIN_TILE_TOKEN", ""),
		Dark: config.String("GOARMIN_TILE_DARK", ""), Attribution: config.String("GOARMIN_TILE_ATTRIBUTION", ""), Logger: log}); {
	case errors.Is(err, tiles.ErrNoUpstream):
		log.Info("map tiles off: routes are drawn on their own (set -tile-url or GOARMIN_TILE_URL for a map)")
	case err != nil:
		fmt.Fprintln(stderr, "goarmin serve: map tiles:", err)
		return 1
	default:
		defer tc.Close()
		opt.Tiles, opt.TilesDark, opt.TilesAttribution = tc.Handler, tc.Dark, tc.Attribution
		log.Info("map tiles on", "cache", filepath.Join(*dataDir, "tiles"), "dark", tc.Dark)
	}
	hubDone := make(chan struct{})
	if *withSync {
		hopt := nodehub.Options{DataDir: *dataDir, Adapter: *adapter, Interval: *interval, In: in, Logger: log,
			FirstSession: firstSession, Features: features}
		if *fakePairer {
			hopt = nodehub.Scripted(hopt)
			log.Warn("scripted Bluetooth (-fake-pairer): made-up watches, no real syncs")
		}
		hub := nodehub.New(hopt)
		go func() {
			hub.Run(ctx)
			close(hubDone)
		}()
		opt.Node = hub
		log.Info("sync role on", "adapter", *adapter, "interval", *interval)
	}
	var usb *usbsource.Finder
	switch {
	case *noUSB:
		log.Info("usb off (-no-usb)")
	case *fakePairer:
		log.Info("usb off on a scratch server with -fake-pairer")
	default:
		usb = usbsource.New()
		log.Info("usb on: a watch plugged in is imported for its owner")
	}
	imports := importer.New(importer.Options{Store: in.Store, In: in, DataDir: *dataDir, USB: usb, Logger: log})
	importsDone := make(chan struct{})
	go func() {
		imports.Run(ctx)
		close(importsDone)
	}()
	opt.Imports, opt.ImportMax = imports, int64(*importMaxGB)<<30
	srv, err := api.New(opt)
	if err != nil {
		fmt.Fprintln(stderr, "goarmin serve:", err)
		return 1
	}
	hs := &http.Server{
		Addr: *listen, Handler: srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 5 * time.Minute,
		IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 64 << 10,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				_ = in.Store.PurgeSessions(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
	errc := make(chan error, 1)
	go func() { errc <- hs.ListenAndServe() }()
	log.Info("goarmin serving", "listen", *listen, "public_url", *public, "dashboard_built", webui.Built())
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(stderr, "goarmin serve:", err)
			return 1
		}
	case <-ctx.Done():
		log.Info("goarmin stopping")
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}
	select {
	case <-importsDone:
	case <-time.After(15 * time.Second):
		log.Warn("the import did not stop within 15 s; exiting anyway")
	}
	if *withSync {
		// The store closes when this returns: let a running sync or live link end first. Both
		// stop on ctx; files a cut-short sync pulled stay in the spool for the next start.
		select {
		case <-hubDone:
		case <-time.After(45 * time.Second):
			log.Warn("the sync did not stop within 45 s; exiting anyway")
		}
	}
	log.Info("goarmin stopped")
	return 0
}

// samePath reports whether a and b name the same directory (after cleaning, made absolute).
func samePath(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && filepath.Clean(aa) == filepath.Clean(bb)
}
