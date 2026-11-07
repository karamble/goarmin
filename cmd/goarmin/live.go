// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/karamble/goarmin/internal/config"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/realtime"
)

func runLive(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("live", flag.ContinueOnError)
	fs.SetOutput(stderr)
	adapter := fs.String("adapter", config.String("GOARMIN_ADAPTER", "hci0"), "Bluetooth adapter")
	device := fs.String("device", config.String("GOARMIN_DEVICE", ""), "paired watch address; empty = the only known watch")
	dataDir := fs.String("data", config.DataDir(), "data directory (node state)")
	what := fs.String("services", "hr,steps,stress,body_battery,respiration", "comma-separated: "+strings.Join(serviceNames(), ", "))
	dur := fs.Duration("for", 30*time.Second, "how long to stream")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var svcs []multilink.Service
	for _, n := range strings.Split(*what, ",") {
		svc, ok := realtime.Services[strings.TrimSpace(n)]
		if !ok {
			fmt.Fprintf(stderr, "goarmin live: unknown service %q\n", n)
			return 2
		}
		svcs = append(svcs, svc)
	}
	ctx, stop := signalContext()
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *dur)
	defer cancel()
	unlock, ok := lockBluetooth("live", *dataDir, stderr)
	if !ok {
		return 1
	}
	defer unlock()
	start := time.Now()
	res, err := syncnode.Live(ctx, syncnode.Options{DataDir: *dataDir, Adapter: *adapter, Device: *device}, svcs,
		func(r realtime.Reading) {
			line := fmt.Sprintf("%6.1fs  %-13s", time.Since(start).Seconds(), r.Name)
			if r.Name == "raw" {
				line += fmt.Sprintf(" %s % x", r.Service, r.Raw)
			} else {
				line += fmt.Sprintf(" %g %s", r.Value, r.Unit)
			}
			fmt.Fprintln(stdout, line)
		})
	if res.Device.ProductNumber != 0 {
		fmt.Fprintf(stdout, "watch: %s, connected after %s\n", res.Device.BluetoothName, res.Handshake.Round(100*time.Millisecond))
		fmt.Fprintf(stdout, "services opened: %v\n", res.Opened)
	}
	if err != nil {
		fmt.Fprintln(stderr, "goarmin live:", err)
		return 1
	}
	return 0
}

func serviceNames() []string {
	var out []string
	for n := range realtime.Services {
		out = append(out, n)
	}
	return out
}
