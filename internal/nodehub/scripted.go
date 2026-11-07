// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package nodehub

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/garmin/gattlink"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/realtime"
	"github.com/karamble/goarmin/pkg/garmin/watch"
)

// The made-up watches of the scripted Bluetooth (locally administered addresses, so they can
// never be a real device).
const (
	ScriptedNew    = "02:00:00:00:00:01" // asks for a code
	ScriptedPaired = "02:00:00:00:00:02" // paired with this computer already
)

// Scripted replaces every Bluetooth part of opt with a script, for trying the Add a watch
// wizard on a scratch server (goarmin serve -fake-pairer): two made-up watches show up, one
// asks to confirm a code, both identify as a Fenix 8. Syncs and live views find no watch in
// range. Nothing touches BlueZ.
func Scripted(opt Options) Options {
	dir := opt.DataDir
	opt.Pairer = &scriptedPairer{dir: dir, bonded: map[string]bool{ScriptedPaired: true}}
	opt.Sync = func(context.Context, syncnode.Options) (syncnode.Result, error) {
		return syncnode.Result{Battery: watch.Battery{Percent: -1}}, fmt.Errorf("%w (scripted Bluetooth)", gattlink.ErrNotInRange)
	}
	opt.Live = func(context.Context, syncnode.Options, []multilink.Service, func(realtime.Reading)) (syncnode.Result, error) {
		return syncnode.Result{Battery: watch.Battery{Percent: -1}}, fmt.Errorf("%w (scripted Bluetooth)", gattlink.ErrNotInRange)
	}
	opt.AdapterState = func(context.Context) (bluez.Adapter, error) {
		return bluez.Adapter{Name: "hci0", Address: "02:00:00:00:00:00", Alias: "scripted", Powered: true, Pairable: true,
			PowerState: "on"}, nil
	}
	opt.PowerOn = func(context.Context) error { return nil }
	opt.Forget = func(_ context.Context, addr string) error { return syncnode.ForgetRecord(dir, addr) }
	return opt
}

type scriptedPairer struct {
	dir    string
	mu     sync.Mutex
	bonded map[string]bool
}

func (p *scriptedPairer) Scan(ctx context.Context, fn func([]bluez.Device) bool) error {
	start := time.Now()
	for {
		p.mu.Lock()
		devs := []bluez.Device{{Address: ScriptedPaired, Name: "Forerunner 965", Bonded: p.bonded[ScriptedPaired],
			Paired: p.bonded[ScriptedPaired], RSSI: -79, HasRSSI: true}}
		if time.Since(start) > 1500*time.Millisecond { // the watch in pairing mode shows up a moment later
			devs = append(devs, bluez.Device{Address: ScriptedNew, Name: "fenix 8 - 47mm", Bonded: p.bonded[ScriptedNew],
				Paired: p.bonded[ScriptedNew], RSSI: -55, HasRSSI: true})
		}
		p.mu.Unlock()
		if !fn(devs) {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (p *scriptedPairer) Pair(ctx context.Context, d bluez.Device, repair bool, pr bluez.Prompter) (bluez.Device, bool, error) {
	p.mu.Lock()
	bonded := p.bonded[d.Address]
	p.mu.Unlock()
	if bonded && !repair {
		d.Bonded = true
		return d, false, nil
	}
	select {
	case <-time.After(500 * time.Millisecond): // connecting
	case <-ctx.Done():
		return d, false, ctx.Err()
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	ok, err := pr.RequestConfirmation(d, binary.BigEndian.Uint32(b[:])%1_000_000)
	if err != nil {
		return d, false, err
	}
	if !ok {
		return d, false, fmt.Errorf("scripted: %w", errRejected)
	}
	p.mu.Lock()
	p.bonded[d.Address] = true
	p.mu.Unlock()
	d.Bonded = true
	return d, true, nil
}

var errRejected = fmt.Errorf("the pairing was rejected")

func (p *scriptedPairer) Identify(ctx context.Context, addr string, _ bool, step func(string)) (syncnode.Identity, error) {
	for _, s := range []string{syncnode.StepConnected, syncnode.StepLink, syncnode.StepBattery} {
		select {
		case <-time.After(400 * time.Millisecond):
		case <-ctx.Done():
			return syncnode.Identity{}, ctx.Err()
		}
		step(s)
	}
	unit, name := uint32(3_999_000_001), "fenix 8 - 47mm"
	if addr == ScriptedPaired {
		unit, name = 3_999_000_002, "Forerunner 965"
	}
	return syncnode.Identity{Address: addr,
		Record:  syncnode.WatchRecord{Unit: unit, Product: 4536, Name: name, Firmware: "23.40", Pair: 0, Client: 2},
		Battery: watch.Battery{Percent: 77, At: time.Now()}}, nil
}

func (p *scriptedPairer) Unbond(_ context.Context, addr string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.bonded, addr)
	return nil
}

func (p *scriptedPairer) Save(addr string, rec syncnode.WatchRecord) error {
	return syncnode.Save(p.dir, addr, rec)
}
