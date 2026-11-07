// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package syncnode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"time"

	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/garmin/gattlink"
	"github.com/karamble/goarmin/pkg/garmin/gfdi"
	"github.com/karamble/goarmin/pkg/garmin/watch"
)

// garminCompanyID is Garmin's Bluetooth SIG company identifier, found in the manufacturer data
// of their advertisements.
const garminCompanyID = 0x0087

var garminNames = regexp.MustCompile(`(?i)^(fenix|epix|forerunner|enduro|instinct|venu|vivoactive|marq|tactix|descent|approach|quatix|edge|vivomove|lily)`)

// IsGarmin reports whether a device looks like a Garmin watch: Garmin's company ID in its
// advertisement, or a Garmin model name.
func IsGarmin(d bluez.Device) bool {
	if _, ok := d.ManufacturerData[garminCompanyID]; ok {
		return true
	}
	return garminNames.MatchString(d.DisplayName())
}

// Scan runs discovery and calls fn with the Garmin watches BlueZ knows on adapter (seen now, or
// paired before) about every 500 ms, until fn returns false or ctx ends. Discovery stops when
// it returns.
func Scan(ctx context.Context, adapter string, fn func([]bluez.Device) bool) error {
	c, err := bluez.Open()
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Discover(ctx, adapter, IsGarmin, fn)
}

// Pairing error codes, for the wizard's messages.
const (
	PairMismatch      = "mismatch"       // the codes did not match (our answer)
	PairTimeout       = "timeout"        // nobody confirmed in time
	PairAuthFailed    = "auth_failed"    // the watch refused or gave up
	PairConnectFailed = "connect_failed" // no connection to the watch
	PairGone          = "gone"           // the watch is no longer visible
	PairInProgress    = "in_progress"    // another pairing with it runs
	PairAdapterOff    = "adapter_off"
	PairNotBonded     = "not_bonded" // paired, but no bond stored
	PairFailed        = "failed"     // anything else
)

// PairCode classifies a pairing error ("" for nil). The answers our own prompter gave
// (mismatch, timeout) are known to the caller, not to BlueZ, which reports them as a rejected
// or cancelled authentication.
func PairCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, bluez.ErrNotBonded) {
		return PairNotBonded
	}
	if errors.Is(err, gattlink.ErrAdapterOff) {
		return PairAdapterOff
	}
	if errors.Is(err, bluez.ErrUnknownDevice) {
		return PairGone
	}
	switch bluez.ErrorName(err) {
	case bluez.ErrNameAuthenticationTimeout:
		return PairTimeout
	case bluez.ErrNameAuthenticationFailed, bluez.ErrNameAuthenticationRejected, bluez.ErrNameAuthenticationCanceled:
		return PairAuthFailed
	case bluez.ErrNameConnectionAttempt, bluez.ErrNameFailed:
		return PairConnectFailed
	case bluez.ErrNameDoesNotExist, "UnknownObject":
		return PairGone
	case bluez.ErrNameInProgress:
		return PairInProgress
	case bluez.ErrNameNotReady, bluez.ErrNameBlocked:
		return PairAdapterOff
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return PairTimeout
	}
	return PairFailed
}

// PairOptions says what to pair.
type PairOptions struct {
	Adapter string
	// Device is the watch as a scan listed it.
	Device bluez.Device
	// Repair removes an existing bond and pairs again (the way out of a broken pairing).
	Repair bool
	// Prompter answers the confirmation or passkey request.
	Prompter bluez.Prompter
	// Timeout bounds the pairing itself (90 s by default).
	Timeout time.Duration
}

// pairClient is the part of *bluez.Client that pairing uses.
type pairClient interface {
	Adapter(ctx context.Context, name string) (bluez.Adapter, error)
	SetPairable(ctx context.Context, name string, on bool) error
	Device(ctx context.Context, adapter, addr string) (bluez.Device, error)
	RemoveDevice(ctx context.Context, adapter string, d bluez.Device) error
	ScanLE(ctx context.Context, adapter string, d time.Duration, keep func(bluez.Device) bool, stopEarly func([]bluez.Device) bool) ([]bluez.Device, error)
	RegisterAgent(ctx context.Context, target bluez.Device, p bluez.Prompter, opt bluez.AgentOptions) (*bluez.Agent, func(), error)
	PairBonded(ctx context.Context, adapter string, d bluez.Device) (bluez.Device, error)
	CancelPairing(ctx context.Context, d bluez.Device) error
	SetTrusted(ctx context.Context, d bluez.Device, trusted bool) error
}

// Pair bonds this computer with the watch. A watch that is bonded already is left as it is
// (fresh false) unless opt.Repair. Discovery must not run meanwhile: pairing right after the
// scan stopped is what works on the Fenix 8. When ctx ends during the pairing, it is cancelled
// in BlueZ too.
func Pair(ctx context.Context, opt PairOptions) (dev bluez.Device, fresh bool, err error) {
	c, err := bluez.Open()
	if err != nil {
		return opt.Device, false, err
	}
	defer c.Close()
	return pairWith(ctx, c, opt)
}

func pairWith(ctx context.Context, c pairClient, opt PairOptions) (bluez.Device, bool, error) {
	if opt.Timeout <= 0 {
		opt.Timeout = 90 * time.Second
	}
	ad, err := c.Adapter(ctx, opt.Adapter)
	if err != nil {
		return opt.Device, false, err
	}
	if !ad.Powered {
		return opt.Device, false, fmt.Errorf("%w (%s)", gattlink.ErrAdapterOff, opt.Adapter)
	}
	cur, err := c.Device(ctx, opt.Adapter, opt.Device.Address)
	if err != nil {
		return opt.Device, false, err
	}
	if cur.Bonded && !opt.Repair {
		if !cur.Trusted {
			if err := c.SetTrusted(ctx, cur, true); err != nil {
				return cur, false, fmt.Errorf("syncnode: marking the watch as trusted: %w", err)
			}
			cur.Trusted = true
		}
		return cur, false, nil
	}
	if cur.Paired || cur.Bonded {
		// Repair, or a pairing without a bond: start from scratch. BlueZ forgets the device
		// with its bond, so it has to be seen again before pairing.
		if err := c.RemoveDevice(ctx, opt.Adapter, cur); err != nil {
			return cur, false, fmt.Errorf("syncnode: removing the old pairing: %w", err)
		}
		addr := cur.Address
		found, err := c.ScanLE(ctx, opt.Adapter, 20*time.Second, func(d bluez.Device) bool { return d.Address == addr },
			func(f []bluez.Device) bool { return len(f) > 0 })
		if err != nil {
			return cur, false, err
		}
		if len(found) == 0 {
			return cur, false, fmt.Errorf("%w: %s did not show up again after removing the pairing", bluez.ErrUnknownDevice, addr)
		}
		cur = found[0]
	}
	if !ad.Pairable {
		if err := c.SetPairable(ctx, opt.Adapter, true); err != nil {
			return cur, false, fmt.Errorf("syncnode: making %s pairable: %w", opt.Adapter, err)
		}
	}
	_, unregister, err := c.RegisterAgent(ctx, cur, opt.Prompter, bluez.AgentOptions{})
	if err != nil {
		return cur, false, err
	}
	defer unregister()
	pctx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()
	done, err := c.PairBonded(pctx, opt.Adapter, cur)
	if err != nil {
		if pctx.Err() != nil {
			cctx, ccancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = c.CancelPairing(cctx, cur)
			ccancel()
		}
		return cur, false, err
	}
	if err := c.SetTrusted(ctx, done, true); err != nil {
		return done, true, fmt.Errorf("syncnode: marking the watch as trusted: %w", err)
	}
	done.Trusted = true
	return done, true, nil
}

// Unbond removes this computer's pairing with the watch at addr (BlueZ only; node.json is left
// alone). A watch BlueZ does not know is fine.
func Unbond(ctx context.Context, adapter, addr string) error {
	c, err := bluez.Open()
	if err != nil {
		return err
	}
	defer c.Close()
	d, err := c.Device(ctx, adapter, addr)
	if errors.Is(err, bluez.ErrUnknownDevice) {
		return nil
	}
	if err != nil {
		return err
	}
	return c.RemoveDevice(ctx, adapter, d)
}

// Identify steps, reported through IdentifyOptions.Step.
const (
	StepConnected = "connected" // Bluetooth connection with the services resolved
	StepLink      = "link"      // the Garmin link: Multi-Link, GFDI handshake, model and firmware
	StepBattery   = "battery"   // the battery level asked (answered or not)
)

// IdentifyOptions says which watch to identify.
type IdentifyOptions struct {
	DataDir string
	Adapter string
	Address string
	// FirstSession tells a newly paired watch that its setup is complete (PAIR_COMPLETE,
	// SYNC_COMPLETE, SETUP_WIZARD_COMPLETE), as Garmin's apps do on the first connection.
	FirstSession bool
	// Location answers the watch's time requests (time.Local by default).
	Location *time.Location
	Logger   *slog.Logger
	// Step, if set, hears each step as it completes.
	Step func(string)
}

// Identity is what a handshake tells about a watch.
type Identity struct {
	Address string
	// Record is the watch's node.json entry; Identify does not save it (Save does).
	Record  WatchRecord
	Device  gfdi.DeviceInfo
	Battery watch.Battery
}

// Identify connects to the watch, runs the handshake and asks its battery level, then
// disconnects. No files are listed or fetched, and nothing is saved.
func Identify(ctx context.Context, opt IdentifyOptions) (Identity, error) {
	id := Identity{Address: opt.Address, Battery: noBattery}
	bz, err := bluez.Open()
	if err != nil {
		return id, err
	}
	defer bz.Close()
	gw, err := gattlink.Connect(ctx, bz, opt.Adapter, opt.Address)
	if err != nil {
		return id, err
	}
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = gw.Disconnect(dctx)
	}()
	step(opt, StepConnected)
	return identifyWith(ctx, opt, gw.Open, gw.Pairs)
}

func step(opt IdentifyOptions, name string) {
	if opt.Step != nil {
		opt.Step(name)
	}
}

// identifyWith is Identify after the Bluetooth connection.
func identifyWith(ctx context.Context, opt IdentifyOptions, open watch.Opener, pairs []int) (Identity, error) {
	id := Identity{Address: opt.Address, Battery: noBattery}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	wopt := watch.Options{Location: opt.Location, Logger: opt.Logger, FirstSession: opt.FirstSession}
	d, err := dial(ctx, opt.DataDir, opt.Address, open, pairs, wopt)
	if err != nil {
		return id, err
	}
	defer d.closer.Close()
	defer d.conn.Close()
	id.Device = d.conn.Device
	id.Record = recordOf(d.conn.Device, d.pair, d.client)
	step(opt, StepLink)
	bctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	if b, err := d.conn.RequestBattery(bctx); err == nil {
		id.Battery = b
	} else {
		opt.Logger.Info("battery level not reported", "err", err)
	}
	cancel()
	step(opt, StepBattery)
	return id, nil
}

// BluezPairer pairs and identifies watches with this computer's BlueZ: the server's wizard
// uses it through nodehub.
type BluezPairer struct {
	DataDir string
	Adapter string
	// FirstSession sends the first-session events after a fresh pairing (GOARMIN_FIRST_SESSION).
	FirstSession bool
	Logger       *slog.Logger
}

// Scan lists Garmin watches while discovery runs.
func (p BluezPairer) Scan(ctx context.Context, fn func([]bluez.Device) bool) error {
	return Scan(ctx, p.Adapter, fn)
}

// Pair bonds with d (see Pair).
func (p BluezPairer) Pair(ctx context.Context, d bluez.Device, repair bool, pr bluez.Prompter) (bluez.Device, bool, error) {
	return Pair(ctx, PairOptions{Adapter: p.Adapter, Device: d, Repair: repair, Prompter: pr})
}

// Identify runs a handshake with the watch at addr (see Identify).
func (p BluezPairer) Identify(ctx context.Context, addr string, fresh bool, steps func(string)) (Identity, error) {
	return Identify(ctx, IdentifyOptions{DataDir: p.DataDir, Adapter: p.Adapter, Address: addr,
		FirstSession: fresh && p.FirstSession, Logger: p.Logger, Step: steps})
}

// Unbond removes the pairing with the watch at addr.
func (p BluezPairer) Unbond(ctx context.Context, addr string) error {
	return Unbond(ctx, p.Adapter, addr)
}

// Save adds the watch to node.json.
func (p BluezPairer) Save(addr string, rec WatchRecord) error { return Save(p.DataDir, addr, rec) }
