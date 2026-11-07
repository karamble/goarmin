// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/karamble/goarmin/internal/config"
	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/cobs"
	"github.com/karamble/goarmin/pkg/garmin/gfdi"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
)

// Multi-Link GATT service and characteristic pairs (docs/protocol/01 §1.1, 05 §6).
const mlServiceUUID = "6a4e2800-667b-11e3-949a-0800200c9a66"

func mlCharUUID(base, i int) string {
	return fmt.Sprintf("6a4e%04x-667b-11e3-949a-0800200c9a66", base+i)
}

func runProbe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	adapter := fs.String("adapter", config.String("GOARMIN_ADAPTER", "hci0"), "Bluetooth adapter")
	device := fs.String("device", config.String("GOARMIN_DEVICE", ""), "paired watch address (AA:BB:CC:DD:EE:FF)")
	client := fs.Uint64("client", multilink.ClientGadgetbridge, "Multi-Link client ID (2 = Gadgetbridge, 1 = Garmin Connect)")
	pairIdx := fs.Int("pair", -1, "characteristic pair 0-4 (2810/2820 + n); -1 tries each in turn")
	reliable := fs.Bool("reliable", false, "ask for a reliable (MLR) GFDI handle")
	timeout := fs.Duration("timeout", 45*time.Second, "overall handshake timeout")
	tracePath := fs.String("trace", "", "write every packet as JSONL here (default ~/goarmin-captures/linux-probe-<time>.jsonl; '-' disables)")
	stay := fs.Bool("stay-connected", false, "leave the Bluetooth connection up afterwards")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: goarmin probe -device AA:BB:CC:DD:EE:FF [flags]\n\nConnects to a paired watch, runs the Multi-Link and GFDI handshake and prints what the\nwatch reports. Nothing is synced or archived.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *device == "" {
		fs.Usage()
		return 2
	}
	unlock, ok := lockBluetooth("probe", config.DataDir(), stderr)
	if !ok {
		return 1
	}
	defer unlock()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	o := probeOpts{adapter: *adapter, device: *device, client: *client, pair: *pairIdx, reliable: *reliable,
		timeout: *timeout, trace: *tracePath, stay: *stay}
	if err := probe(ctx, stdout, o); err != nil {
		fmt.Fprintln(stderr, "goarmin probe:", err)
		return 1
	}
	return 0
}

type probeOpts struct {
	adapter, device string
	client          uint64
	pair            int
	reliable        bool
	timeout         time.Duration
	trace           string
	stay            bool
}

func probe(ctx context.Context, out io.Writer, o probeOpts) error {
	addr, err := bluez.NormalizeAddress(o.device)
	if err != nil {
		return err
	}
	c, err := bluez.Open()
	if err != nil {
		return err
	}
	defer c.Close()

	d, err := c.Device(ctx, o.adapter, addr)
	if err != nil || !d.Paired {
		return fmt.Errorf("%s is not paired with %s; run goarmin pair first", addr, o.adapter)
	}
	fmt.Fprintf(out, "Watch %s (%s) paired=%v bonded=%v trusted=%v connected=%v\n",
		d.Address, d.DisplayName(), d.Paired, d.Bonded, d.Trusted, d.Connected)

	if !d.Connected {
		// A short LE scan first makes BlueZ use the LE bearer for this dual-mode device.
		scanCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, _ = c.ScanLE(scanCtx, o.adapter, 8*time.Second, func(x bluez.Device) bool { return x.Address == addr },
			func(found []bluez.Device) bool { return len(found) > 0 && found[0].HasRSSI })
		cancel()
		fmt.Fprintln(out, "Connecting…")
		connCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := c.Connect(connCtx, d)
		cancel()
		if err != nil {
			return err
		}
	}
	if !o.stay {
		defer func() {
			dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = c.Disconnect(dctx, d)
		}()
	}

	chars, err := c.Characteristics(ctx, d)
	if err != nil {
		return err
	}
	byUUID := map[string]bluez.Characteristic{}
	for _, ch := range chars {
		if ch.ServiceUUID == mlServiceUUID {
			byUUID[ch.UUID] = ch
		}
	}
	var pairs []int
	for i := 0; i < 5; i++ {
		_, okRX := byUUID[mlCharUUID(0x2810, i)]
		_, okTX := byUUID[mlCharUUID(0x2820, i)]
		if okRX && okTX && (o.pair < 0 || o.pair == i) {
			pairs = append(pairs, i)
		}
	}
	fmt.Fprintf(out, "Multi-Link service: %d characteristics, usable pairs %v\n", len(byUUID), pairs)
	if len(pairs) == 0 {
		return errors.New("no usable Multi-Link characteristic pair (is this a Garmin watch with V2 protocol?)")
	}

	tr, closeTrace, err := openTrace(o.trace)
	if err != nil {
		return err
	}
	defer closeTrace()

	deadline, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	for _, i := range pairs {
		rx, tx := byUUID[mlCharUUID(0x2810, i)], byUUID[mlCharUUID(0x2820, i)]
		link, err := c.OpenLink(deadline, rx, tx)
		if err != nil {
			fmt.Fprintf(out, "pair %d: %v\n", i, err)
			continue
		}
		fmt.Fprintf(out, "pair %d (%s/%s): link %s, max write %d\n", i, rx.UUID[4:8], tx.UUID[4:8], link.Mode(), link.MaxWrite())
		s := &probeSession{out: out, link: link, client: o.client, tr: tr, start: time.Now()}
		res, err := s.run(deadline, o.reliable)
		link.Close()
		if errors.Is(err, errNoControlResponse) {
			fmt.Fprintf(out, "pair %d: no answer to CLOSE_ALL, trying the next pair\n", i)
			continue
		}
		res.print(out, i, o.client)
		return err
	}
	return errors.New("no characteristic pair answered")
}

var errNoControlResponse = errors.New("no Multi-Link control response")

// probeSession speaks just enough Multi-Link and GFDI to complete the handshake.
type probeSession struct {
	out    io.Writer
	link   *bluez.Link
	client uint64
	tr     *json.Encoder
	start  time.Time

	gfdiHandle   byte
	registered   bool
	reliable     bool
	mlrSend      int
	mlrRecv      int
	dec          cobs.StreamDecoder
	compact      bool
	hostSeq      uint8
	pendingFTReq bool

	res probeResult
}

type probeResult struct {
	controls  []string
	devInfo   *gfdi.DeviceInfo
	watchCaps gfdi.Capabilities
	fileTypes []string
	compact   bool
	reliable  bool
	auth      bool
	other     map[string]int
	done      bool
}

func (s *probeSession) trace(dir string, b []byte, note string) {
	if s.tr == nil {
		return
	}
	_ = s.tr.Encode(map[string]any{"t_ms": time.Since(s.start).Milliseconds(), "dir": dir, "hex": hex.EncodeToString(b), "note": note})
}

func (s *probeSession) send(ctx context.Context, pkt []byte, note string) error {
	s.trace("tx", pkt, note)
	return s.link.Send(ctx, pkt)
}

func (s *probeSession) run(ctx context.Context, reliable bool) (probeResult, error) {
	s.res.other = map[string]int{}
	if err := s.send(ctx, multilink.EncodeCloseAll(s.client), "CLOSE_ALL_REQ"); err != nil {
		return s.res, err
	}
	if _, err := s.waitControl(ctx, multilink.CloseAllResponse, 5*time.Second); err != nil {
		return s.res, err
	}
	if err := s.send(ctx, multilink.EncodeRegister(s.client, multilink.ServiceGFDI, reliable), "REGISTER GFDI"); err != nil {
		return s.res, err
	}
	reg, err := s.waitControl(ctx, multilink.RegisterResponse, 5*time.Second)
	if err != nil {
		return s.res, fmt.Errorf("register GFDI: %w", err)
	}
	if reg.Status != 0 {
		return s.res, fmt.Errorf("register GFDI refused with status %d", reg.Status)
	}
	s.gfdiHandle, s.reliable, s.registered = reg.Handle, reg.Reliable, true
	s.res.reliable = reg.Reliable

	for !s.res.done {
		pkt, err := s.next(ctx)
		if err != nil {
			if s.res.devInfo != nil {
				return s.res, fmt.Errorf("handshake incomplete: %w", err)
			}
			return s.res, err
		}
		if err := s.handle(ctx, pkt); err != nil {
			return s.res, err
		}
	}
	_ = s.send(ctx, multilink.EncodeCloseAll(s.client), "CLOSE_ALL_REQ (done)")
	return s.res, nil
}

func (s *probeSession) next(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.link.Done():
		if err := s.link.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("link closed")
	case pkt := <-s.link.Recv():
		s.trace("rx", pkt, "")
		return pkt, nil
	}
}

// waitControl reads packets until a control message of type t arrives, handling GFDI traffic
// in between.
func (s *probeSession) waitControl(ctx context.Context, t multilink.ControlType, timeout time.Duration) (multilink.Control, error) {
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		pkt, err := s.next(wctx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				return multilink.Control{}, fmt.Errorf("%w (%s)", errNoControlResponse, t)
			}
			return multilink.Control{}, err
		}
		if multilink.IsControl(pkt) {
			ctl, err := multilink.ParseControl(pkt)
			if err != nil {
				continue
			}
			s.res.controls = append(s.res.controls, ctl.String())
			fmt.Fprintf(s.out, "  ML %s\n", ctl)
			if ctl.Type == t {
				return ctl, nil
			}
			continue
		}
		if err := s.handle(ctx, pkt); err != nil {
			return multilink.Control{}, err
		}
	}
}

// handle routes one notification: control, the GFDI handle (plain or MLR), or anything else.
func (s *probeSession) handle(ctx context.Context, pkt []byte) error {
	if multilink.IsControl(pkt) {
		ctl, err := multilink.ParseControl(pkt)
		if err == nil {
			s.res.controls = append(s.res.controls, ctl.String())
			fmt.Fprintf(s.out, "  ML %s\n", ctl)
		}
		return nil
	}
	if !s.registered || len(pkt) < 1 {
		return nil
	}
	var payload []byte
	switch {
	case s.reliable && multilink.IsMLR(pkt[0]) && len(pkt) >= 2:
		h := multilink.ParseHeader(pkt[0], pkt[1])
		if h.Handle != s.gfdiHandle || len(pkt) == 2 {
			return nil // another reliable handle, or a pure ACK for our data
		}
		if h.SN != s.mlrRecv {
			return s.sendMLRAck(ctx) // duplicate or out of order: repeat our ACK
		}
		s.mlrRecv = (s.mlrRecv + 1) % multilink.SeqModulus
		payload = pkt[2:]
		if err := s.sendMLRAck(ctx); err != nil {
			return err
		}
	case !s.reliable && pkt[0] == s.gfdiHandle:
		payload = pkt[1:]
	default:
		s.res.other[fmt.Sprintf("handle 0x%02X", pkt[0])]++
		return nil
	}
	frames, err := s.dec.Feed(payload)
	if err != nil {
		fmt.Fprintf(s.out, "  COBS: %v\n", err)
	}
	for _, fb := range frames {
		f, err := gfdi.ParseFrame(fb)
		if err != nil {
			fmt.Fprintf(s.out, "  GFDI: %v\n", err)
			continue
		}
		if err := s.onFrame(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

func (s *probeSession) sendMLRAck(ctx context.Context) error {
	h := multilink.Header{Handle: s.gfdiHandle, RN: s.mlrRecv, SN: 0}.Bytes()
	return s.send(ctx, h[:], "MLR ACK")
}

// sendFrame COBS-encodes a GFDI frame and writes it in chunks on the GFDI handle.
func (s *probeSession) sendFrame(ctx context.Context, f gfdi.Frame) error {
	enc := cobs.Encode(f.Marshal())
	hdrLen := 1
	if s.reliable {
		hdrLen = 2
	}
	chunk := s.link.MaxWrite() - hdrLen
	for off := 0; off < len(enc); off += chunk {
		end := min(off+chunk, len(enc))
		var pkt []byte
		if s.reliable {
			h := multilink.Header{Handle: s.gfdiHandle, RN: s.mlrRecv, SN: s.mlrSend}.Bytes()
			s.mlrSend = (s.mlrSend + 1) % multilink.SeqModulus
			pkt = append(h[:], enc[off:end]...)
		} else {
			pkt = append([]byte{s.gfdiHandle}, enc[off:end]...)
		}
		if err := s.send(ctx, pkt, f.String()); err != nil {
			return err
		}
	}
	return nil
}

// request sends a host-originated message, in compact form once the watch uses it.
func (s *probeSession) request(ctx context.Context, id gfdi.MessageID, payload []byte) error {
	f := gfdi.Frame{ID: id, Compact: s.compact, Seq: s.hostSeq, Payload: payload}
	s.hostSeq = (s.hostSeq + 1) % 32
	fmt.Fprintf(s.out, "  → %v\n", f)
	return s.sendFrame(ctx, f)
}

func (s *probeSession) reply(ctx context.Context, req gfdi.Frame, st gfdi.Status, data []byte) error {
	return s.sendFrame(ctx, gfdi.ReplyTo(req, st, data))
}

func (s *probeSession) onFrame(ctx context.Context, f gfdi.Frame) error {
	if f.Compact && !s.compact {
		s.compact, s.res.compact = true, true
	}
	fmt.Fprintf(s.out, "  ← %v\n", f)
	switch f.ID {
	case gfdi.DeviceInformation:
		d, err := gfdi.ParseDeviceInfo(f.Payload)
		if err != nil {
			return err
		}
		s.res.devInfo = &d
		host := gfdi.GarminConnectHost(hostLabel(), "Linux", "goarmin")
		return s.reply(ctx, f, gfdi.StatusACK, host.ReplyData())
	case gfdi.Configuration:
		caps, err := gfdi.ParseConfiguration(f.Payload)
		if err != nil {
			return err
		}
		s.res.watchCaps = caps
		if err := s.reply(ctx, f, gfdi.StatusACK, nil); err != nil {
			return err
		}
		return s.request(ctx, gfdi.Configuration, gfdi.GarminConnectHostCapabilities().Payload())
	case gfdi.Response:
		r, err := gfdi.ParseResponse(f.Payload)
		if err != nil {
			return err
		}
		fmt.Fprintf(s.out, "    response to %v: status %d, %d data bytes\n", r.Original, r.Status, len(r.Data))
		switch r.Original {
		case gfdi.Configuration:
			s.pendingFTReq = true
			return s.request(ctx, gfdi.SupportedFileTypesRequest, nil)
		case gfdi.SupportedFileTypesRequest:
			s.res.fileTypes = parseFileTypes(r.Data)
			s.res.done = true
		}
		return nil
	case gfdi.CurrentTimeRequest:
		return s.reply(ctx, f, gfdi.StatusACK, currentTimeData(f.Payload, time.Now()))
	case gfdi.AuthNegotiation:
		s.res.auth = true
		fmt.Fprintln(s.out, "    AUTH_NEGOTIATION received: the link may not be OS-bonded")
		data := []byte{0} // GUESS_OK
		if len(f.Payload) >= 5 {
			data = append(data, f.Payload[:5]...)
		}
		return s.reply(ctx, f, gfdi.StatusACK, data)
	case gfdi.ProtobufRequest, gfdi.ProtobufResponse:
		s.res.other[f.ID.String()]++
		// Not handled in the probe: DISCARDED / UNKNOWN_REQUEST_ID, as Gadgetbridge does.
		if len(f.Payload) >= 6 {
			data := append(append([]byte(nil), f.Payload[:6]...), 1, 100)
			return s.reply(ctx, f, gfdi.StatusACK, data)
		}
		return nil
	default:
		s.res.other[f.ID.String()]++
		return s.reply(ctx, f, gfdi.StatusACK, nil)
	}
}

// currentTimeData answers CURRENT_TIME_REQUEST: reference echoed, Garmin-epoch time, the
// local UTC offset, and no DST transition information.
func currentTimeData(req []byte, now time.Time) []byte {
	const garminEpoch = 631065600
	b := make([]byte, 20)
	if len(req) >= 4 {
		copy(b[0:4], req[:4])
	}
	binary.LittleEndian.PutUint32(b[4:], uint32(now.Unix()-garminEpoch))
	_, off := now.Zone()
	binary.LittleEndian.PutUint32(b[8:], uint32(int32(off)))
	return b
}

func parseFileTypes(data []byte) []string {
	if len(data) < 1 {
		return nil
	}
	var out []string
	n, i := int(data[0]), 1
	for k := 0; k < n && i+3 <= len(data); k++ {
		dt, st, l := data[i], data[i+1], int(data[i+2])
		if i+3+l > len(data) {
			break
		}
		out = append(out, fmt.Sprintf("%d/%d %s", dt, st, data[i+3:i+3+l]))
		i += 3 + l
	}
	return out
}

func hostLabel() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "Linux"
	}
	return h
}

func (r probeResult) print(out io.Writer, pair int, client uint64) {
	fmt.Fprintln(out, "\n== probe result")
	fmt.Fprintf(out, "characteristic pair: %d (6a4e%04x / 6a4e%04x), client ID %d\n", pair, 0x2810+pair, 0x2820+pair, client)
	fmt.Fprintf(out, "GFDI handle reliable (MLR): %v, compact frames: %v, AUTH_NEGOTIATION seen: %v\n", r.reliable, r.compact, r.auth)
	if d := r.devInfo; d != nil {
		fmt.Fprintf(out, "watch: %q (%s %s) product %d, firmware %s, GFDI protocol %d, max packet %d, unit %d\n",
			d.BluetoothName, d.DeviceName, d.DeviceModel, d.ProductNumber, d.SoftwareVersionString(),
			d.ProtocolVersion, d.MaxPacketSize, d.UnitNumber)
	}
	if r.watchCaps != nil {
		fmt.Fprintf(out, "capabilities: %d bits; SYNC_2=%v MULTI_LINK=%v FEATURE_CAPS_EXCHANGE=%v\n", len(r.watchCaps.Bits()),
			r.watchCaps.Has(gfdi.CapSync2), r.watchCaps.Has(gfdi.CapMultiLinkService), r.watchCaps.Has(gfdi.CapFeatureCapabilitiesExchange))
	}
	if len(r.fileTypes) > 0 {
		fmt.Fprintf(out, "supported file types (%d): %s\n", len(r.fileTypes), strings.Join(r.fileTypes, ", "))
	}
	if len(r.other) > 0 {
		fmt.Fprintf(out, "other traffic: %v\n", r.other)
	}
	fmt.Fprintf(out, "handshake complete: %v\n", r.done)
}

// openTrace opens the JSONL packet trace. Traces hold personal identifiers (unit ID, names),
// so the default location is ~/goarmin-captures, outside any repository.
func openTrace(path string) (*json.Encoder, func(), error) {
	if path == "-" {
		return nil, func() {}, nil
	}
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, err
		}
		path = filepath.Join(home, "goarmin-captures", "linux-probe-"+time.Now().Format("20060102-150405")+".jsonl")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, err
	}
	fmt.Fprintln(os.Stderr, "trace:", path)
	return json.NewEncoder(f), func() { f.Close() }, nil
}
