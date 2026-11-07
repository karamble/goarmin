// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package syncnode

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"time"

	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/garmin/gattlink"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/realtime"
	"github.com/karamble/goarmin/pkg/garmin/smart"
	"github.com/karamble/goarmin/pkg/garmin/watch"
)

// Live connects to the watch over BlueZ, streams the realtime services' values to fn until ctx
// ends, then closes the services and disconnects. No files are synced.
func Live(ctx context.Context, opt Options, services []multilink.Service, fn func(realtime.Reading)) (Result, error) {
	addr, err := resolveAddress(opt)
	if err != nil {
		return Result{Address: addr, Battery: noBattery}, err
	}
	bz, err := bluez.Open()
	if err != nil {
		return Result{Address: addr, Battery: noBattery}, err
	}
	defer bz.Close()
	start := time.Now()
	gw, err := gattlink.Connect(ctx, bz, opt.Adapter, addr)
	if err != nil {
		return Result{Address: addr, Battery: noBattery}, err
	}
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = gw.Disconnect(dctx)
	}()
	return liveWith(ctx, opt, addr, gw.Open, gw.Pairs, start, services, fn)
}

func liveWith(ctx context.Context, opt Options, addr string, open watch.Opener, pairs []int, start time.Time,
	services []multilink.Service, fn func(realtime.Reading)) (Result, error) {
	res := Result{Address: addr, Battery: noBattery}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	wopt := watch.Options{PlainGFDI: opt.PlainGFDI, Location: opt.Location, Logger: opt.Logger}
	if opt.Features {
		wopt.Features = &smart.GarminConnectFeatures
	}
	d, err := dial(ctx, opt.DataDir, addr, open, pairs, wopt)
	res.Attempts = d.tries
	if err != nil {
		return res, err
	}
	conn := d.conn
	defer d.closer.Close()
	defer conn.Close()
	res.Handshake = time.Since(start)
	res.Device, res.Pair, res.Reliable, res.Client = conn.Device, d.pair, conn.GFDI.Reliable(), d.client
	st, err := realtime.Open(ctx, conn.Mux, services)
	if err != nil {
		return res, err
	}
	res.Opened = st.Opened()
	var refused []multilink.Service
	for _, s := range services {
		if !slices.Contains(res.Opened, s) {
			refused = append(refused, s)
		}
	}
	opt.Logger.Info("realtime services", "opened", res.Opened, "refused", refused)
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = st.Close(cctx)
	}()
	for {
		select {
		case r, ok := <-st.Readings():
			if !ok {
				return res, nil
			}
			fn(r)
		case <-ctx.Done():
			return res, nil
		case <-conn.Done():
			return res, conn.Err()
		}
	}
}
