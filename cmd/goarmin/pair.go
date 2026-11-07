// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/karamble/goarmin/internal/config"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/garmin/product"
)

func runPair(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(stderr)
	adapter := fs.String("adapter", config.String("GOARMIN_ADAPTER", "hci0"), "Bluetooth adapter")
	device := fs.String("device", "", "watch address (AA:BB:CC:DD:EE:FF); default: the Garmin watch found by the scan")
	scan := fs.Duration("scan", 30*time.Second, "how long to scan for watches")
	yes := fs.Bool("yes", false, "accept a numeric-comparison passkey without asking")
	force := fs.Bool("force", false, "remove an existing bond and pair again")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: goarmin pair [flags]\n\nPairs a Garmin watch with this computer's Bluetooth adapter and identifies it, so this\ncomputer syncs it. A watch that is paired already is only identified. Interactive: run it in\na terminal.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	unlock, ok := lockBluetooth("pair", config.DataDir(), stderr)
	if !ok {
		return 1
	}
	defer unlock()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := pair(ctx, stdout, config.DataDir(), *adapter, *device, *scan, *yes, *force); err != nil {
		fmt.Fprintln(stderr, "goarmin pair:", err)
		return 1
	}
	return 0
}

func pair(ctx context.Context, out io.Writer, dataDir, adapter, addr string, scan time.Duration, yes, force bool) error {
	if addr != "" {
		a, err := bluez.NormalizeAddress(addr)
		if err != nil {
			return err
		}
		addr = a
	}
	c, err := bluez.Open()
	if err != nil {
		return err
	}
	defer c.Close()

	ad, err := c.Adapter(ctx, adapter)
	if err != nil {
		return err
	}
	if !ad.Powered {
		return fmt.Errorf("adapter %s is powered off (bluetoothctl power on)", adapter)
	}
	fmt.Fprintf(out, "Adapter %s (%s, %q)\n", adapter, ad.Address, ad.Alias)

	var target bluez.Device
	if addr != "" {
		if d, err := c.Device(ctx, adapter, addr); err == nil && d.Bonded && !force {
			fmt.Fprintf(out, "%s (%s) is paired already (-force pairs it again).\n", d.Address, d.DisplayName())
			target = d
		}
	}
	fresh := false
	if target.Address == "" {
		fmt.Fprintln(out, "Put the watch in pairing mode: hold the middle-left button, then")
		fmt.Fprintln(out, "Watch Settings › Connectivity › Phone › Pair Phone.")
		fmt.Fprintf(out, "Scanning for Garmin watches (up to %s)…\n", scan)
		keep := func(d bluez.Device) bool {
			if addr != "" {
				return d.Address == addr
			}
			return syncnode.IsGarmin(d)
		}
		stopEarly := func(found []bluez.Device) bool { return addr != "" && len(found) > 0 }
		found, err := c.ScanLE(ctx, adapter, scan, keep, stopEarly)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			return errors.New("no Garmin watch found; is it in pairing mode and close by?")
		}
		for i, d := range found {
			rssi := "?"
			if d.HasRSSI {
				rssi = strconv.Itoa(int(d.RSSI))
			}
			fmt.Fprintf(out, "  [%d] %s  %-22s RSSI %s dBm  paired=%v\n", i+1, d.Address, d.DisplayName(), rssi, d.Paired)
		}
		target = found[0]
		if len(found) > 1 {
			n, err := ask(out, fmt.Sprintf("Which watch? [1-%d]: ", len(found)))
			if err != nil {
				return err
			}
			i, err := strconv.Atoi(strings.TrimSpace(n))
			if err != nil || i < 1 || i > len(found) {
				return fmt.Errorf("no watch %q", n)
			}
			target = found[i-1]
		}
		fmt.Fprintf(out, "Pairing with %s (%s)…\n", target.Address, target.DisplayName())
		d, f, err := syncnode.Pair(ctx, syncnode.PairOptions{Adapter: adapter, Device: target, Repair: force,
			Prompter: terminalPrompter{out: out, yes: yes}, Timeout: 2 * time.Minute})
		if err != nil {
			return fmt.Errorf("pairing failed (%s): %w", syncnode.PairCode(err), err)
		}
		target, fresh = d, f
		if !fresh {
			fmt.Fprintf(out, "%s is paired already.\n", target.Address)
		}
	}

	first, err := config.Bool("GOARMIN_FIRST_SESSION", false)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Identifying %s…\n", target.Address)
	id, err := syncnode.Identify(ctx, syncnode.IdentifyOptions{DataDir: dataDir, Adapter: adapter, Address: target.Address,
		FirstSession: fresh && first})
	if err != nil {
		return fmt.Errorf("identifying the watch: %w", err)
	}
	if err := syncnode.Save(dataDir, target.Address, id.Record); err != nil {
		return err
	}
	battery := "not reported"
	if id.Battery.Percent >= 0 {
		battery = fmt.Sprintf("%d %%", id.Battery.Percent)
	}
	fmt.Fprintf(out, "%s: %s, unit %d, firmware %s, battery %s\n", target.Address, product.Name(id.Record.Product),
		id.Record.Unit, id.Record.Firmware, battery)
	fmt.Fprintln(out, "This computer syncs it now. Add it to an account in the dashboard: Devices › Add a watch.")
	return nil
}

var stdin = bufio.NewReader(os.Stdin)

func ask(out io.Writer, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("reading the answer: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// terminalPrompter answers pairing requests on the terminal.
type terminalPrompter struct {
	out io.Writer
	yes bool
}

func (p terminalPrompter) RequestPasskey(d bluez.Device) (uint32, error) {
	s, err := ask(p.out, "Enter the 6-digit code shown on the watch: ")
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || n > 999999 {
		return 0, fmt.Errorf("%q is not a 6-digit code", s)
	}
	return uint32(n), nil
}

func (p terminalPrompter) RequestConfirmation(d bluez.Device, passkey uint32) (bool, error) {
	fmt.Fprintf(p.out, "Pairing code: %06d. Confirm it on the watch as well.\n", passkey)
	if p.yes {
		return true, nil
	}
	s, err := ask(p.out, "Does the watch show the same code? [y/N]: ")
	if err != nil {
		return false, err
	}
	return strings.EqualFold(s, "y") || strings.EqualFold(s, "yes"), nil
}

func (p terminalPrompter) DisplayPasskey(d bluez.Device, passkey uint32, entered uint16) {
	fmt.Fprintf(p.out, "Enter %06d on the watch (%d digits typed).\n", passkey, entered)
}

func (p terminalPrompter) Cancel() { fmt.Fprintln(p.out, "The pairing request was cancelled.") }
