// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package watch sets up a session with a Garmin watch over an open BLE link: Multi-Link
// registration, the GFDI handshake as Garmin Connect performs it with the Fenix 8
// (docs/protocol/05 §3), and the responders a host must keep running for the whole connection.
// What happens after the handshake, such as a FileAccess sync, builds on Conn.
package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/karamble/goarmin/pkg/clock"
	"github.com/karamble/goarmin/pkg/garmin/gfdi"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/smart"
)

// ErrNoSync2 is returned by Handshake for a watch without the SYNC_2 capability (bit 90),
// i.e. without FileAccess sync, which is all goarmin implements.
var ErrNoSync2 = errors.New("watch: the watch does not support FileAccess sync (no SYNC_2 capability)")

// Options configures a handshake.
type Options struct {
	// Client is the Multi-Link client ID (multilink.ClientGadgetbridge by default).
	Client uint64
	// PlainGFDI registers GFDI without MLR. By default GFDI is reliable, as Garmin Connect
	// registers it; the Fenix 8 then announces a 4000-byte max packet instead of 400.
	PlainGFDI bool
	// Host is our identity in the DEVICE_INFORMATION reply. The default is Garmin Connect's
	// values with the host name, "Linux" and "goarmin" as names.
	Host *gfdi.HostInfo
	// Capabilities is the host CONFIGURATION bitmap (Garmin Connect's by default).
	Capabilities gfdi.Capabilities
	// Location answers CURRENT_TIME_REQUEST (time.Local by default). Use the owner's zone.
	Location *time.Location
	// FirstSession sends PAIR_COMPLETE, SYNC_COMPLETE and SETUP_WIZARD_COMPLETE after the
	// handshake, as Gadgetbridge does on the first connection to a newly paired watch.
	FirstSession bool
	// Features, if set, announces the host's feature capabilities right after CONFIGURATION,
	// where Garmin Connect sends its own (smart.GarminConnectFeatures: wellness threshold
	// notifications, file access and device status). The answer is in Conn.Features.
	Features *smart.HostFeatures
	// HandshakeTimeout bounds the whole handshake (30 s by default).
	HandshakeTimeout time.Duration
	// MLR tunes the reliable layer (Gadgetbridge's parameters by default).
	MLR    multilink.MLRConfig
	Clock  clock.Clock
	Logger *slog.Logger
	// TracePacket sees every Multi-Link packet; TraceFrame every GFDI frame.
	TracePacket func(dir string, pkt []byte)
	TraceFrame  func(dir string, f gfdi.Frame)
}

// FileType is one entry of the watch's SUPPORTED_FILE_TYPES list.
type FileType struct {
	DataType, SubType byte
	Name              string
}

// ProtobufHandler handles a protobuf message from the watch for one Smart service. It runs on
// the session's dispatch goroutine and must not block; ok=false falls back to the default
// policy.
type ProtobufHandler func(m gfdi.ProtobufMessage, env smart.Envelope) (reply gfdi.ProtobufReply, ok bool)

// Conn is a watch after a successful handshake.
type Conn struct {
	Mux     *multilink.Mux
	GFDI    *multilink.Channel
	Session *gfdi.Session

	Device    gfdi.DeviceInfo
	WatchCaps gfdi.Capabilities
	FileTypes []FileType
	// Features is the watch's answer to Options.Features (nil: not asked, or no answer).
	Features *smart.WatchFeatures

	opt  Options
	log  *slog.Logger
	host gfdi.HostInfo

	mu        sync.Mutex
	handlers  map[int]ProtobufHandler
	devInfoCh chan gfdi.DeviceInfo
	configCh  chan gfdi.Capabilities
	syncReqCh chan struct{}
	pipeNext  int
	stats     map[gfdi.MessageID]int
	battery   Battery
	wellness  int // WellnessThresholdMetNotifications received
}

// Battery is the watch's last BATTERY_STATUS (5023): it sends one unasked soon after the
// session starts and when the level changes. Percent is -1 until one arrived.
type Battery struct {
	Percent int
	// Status is the reported condition: "good", "ok", "low", "critical", "new" or "".
	Status string
	At     time.Time
}

// ParseBattery reads a BATTERY_STATUS payload (docs/protocol/02 §5.6): status in the high
// nibble of byte 0, the charge in percent in byte 2.
func ParseBattery(payload []byte, at time.Time) (Battery, bool) {
	if len(payload) < 3 || payload[2] > 100 {
		return Battery{}, false
	}
	b := Battery{Percent: int(payload[2]), At: at}
	switch payload[0] & 0x70 {
	case 0x10:
		b.Status = "good"
	case 0x20:
		b.Status = "ok"
	case 0x30:
		b.Status = "low"
	case 0x40:
		b.Status = "critical"
	case 0x50:
		b.Status = "new"
	case 0x70:
		return Battery{}, false // invalid
	}
	return b, true
}

// RequestBattery asks the watch for its battery level (DeviceStatusService, as Gadgetbridge
// does after the handshake; the Fenix 8 sends no BATTERY_STATUS by itself during a sync) and
// keeps the answer.
func (c *Conn) RequestBattery(ctx context.Context) (Battery, error) {
	m, err := c.Call(ctx, smart.BatteryRequest())
	if err != nil {
		return Battery{Percent: -1}, err
	}
	env, err := smart.ParseEnvelope(m.Data)
	if err != nil {
		return Battery{Percent: -1}, err
	}
	lvl, ok := smart.ParseBatteryResponse(env)
	if !ok || lvl.Status != smart.BatteryStatusOK || lvl.Percent < 0 || lvl.Percent > 100 {
		return Battery{Percent: -1}, fmt.Errorf("watch: no battery level in the answer (status %d)", lvl.Status)
	}
	b := Battery{Percent: lvl.Percent, At: c.opt.Clock.Now()}
	c.mu.Lock()
	c.battery = b
	c.mu.Unlock()
	return b, nil
}

// WellnessThresholdMet counts the watch's WellnessThresholdMetNotifications in this session.
func (c *Conn) WellnessThresholdMet() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wellness
}

// Battery returns the last battery report (Percent -1: none yet).
func (c *Conn) Battery() Battery {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.battery
}

func (o *Options) defaults() {
	if o.Client == 0 {
		o.Client = multilink.ClientGadgetbridge
	}
	if o.Capabilities == nil {
		o.Capabilities = gfdi.GarminConnectHostCapabilities()
	}
	if o.Location == nil {
		o.Location = time.Local
	}
	if o.HandshakeTimeout <= 0 {
		o.HandshakeTimeout = 30 * time.Second
	}
	if o.Clock == nil {
		o.Clock = clock.Real()
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
}

// DefaultHost is the identity goarmin sends: Garmin Connect's values with our own names.
func DefaultHost() gfdi.HostInfo {
	name, err := os.Hostname()
	if err != nil || name == "" {
		name = "Linux"
	}
	return gfdi.GarminConnectHost(name, "Linux", "goarmin")
}

// Handshake runs the session start on link (docs/protocol/05 §3): CLOSE_ALL, register GFDI,
// answer DEVICE_INFORMATION and CONFIGURATION, then SUPPORTED_FILE_TYPES, DEVICE_SETTINGS and
// SYNC_READY. On error everything it started is stopped; the link stays with the caller.
func Handshake(ctx context.Context, link multilink.Link, opt Options) (*Conn, error) {
	opt.defaults()
	ctx, cancel := context.WithTimeout(ctx, opt.HandshakeTimeout)
	defer cancel()
	c := &Conn{
		opt: opt, log: opt.Logger,
		handlers:  map[int]ProtobufHandler{},
		devInfoCh: make(chan gfdi.DeviceInfo, 1),
		configCh:  make(chan gfdi.Capabilities, 1),
		syncReqCh: make(chan struct{}, 1),
		stats:     map[gfdi.MessageID]int{},
		battery:   Battery{Percent: -1},
	}
	if opt.Host != nil {
		c.host = *opt.Host
	} else {
		c.host = DefaultHost()
	}
	c.Mux = multilink.NewMux(link, multilink.Options{
		Client: opt.Client, Clock: opt.Clock, MLR: opt.MLR, Logger: opt.Logger, Trace: opt.TracePacket,
	})
	ok := false
	defer func() {
		if !ok {
			c.shutdown()
		}
	}()

	if err := c.Mux.CloseAll(ctx); err != nil {
		return nil, fmt.Errorf("watch: CLOSE_ALL: %w", err)
	}
	ch, err := c.Mux.Register(ctx, multilink.ServiceGFDI, !opt.PlainGFDI)
	if err != nil {
		return nil, fmt.Errorf("watch: registering GFDI: %w", err)
	}
	c.GFDI = ch
	c.Session = gfdi.NewSession(ch, gfdi.SessionOptions{
		Clock: opt.Clock, Logger: opt.Logger, Trace: opt.TraceFrame,
		OnFrame: c.onFrame, OnProtobuf: c.onProtobuf,
	})

	select {
	case d := <-c.devInfoCh:
		c.Device = d
	case <-ctx.Done():
		return nil, fmt.Errorf("watch: waiting for DEVICE_INFORMATION: %w", ctx.Err())
	case <-c.Session.Done():
		return nil, fmt.Errorf("watch: waiting for DEVICE_INFORMATION: %w", c.sessionErr())
	}
	select {
	case caps := <-c.configCh:
		c.WatchCaps = caps
	case <-ctx.Done():
		return nil, fmt.Errorf("watch: waiting for CONFIGURATION: %w", ctx.Err())
	case <-c.Session.Done():
		return nil, fmt.Errorf("watch: waiting for CONFIGURATION: %w", c.sessionErr())
	}
	if !c.WatchCaps.Has(gfdi.CapSync2) {
		return nil, ErrNoSync2
	}
	if r, err := c.Session.Request(ctx, gfdi.Configuration, opt.Capabilities.Payload()); err != nil {
		return nil, fmt.Errorf("watch: host CONFIGURATION: %w", err)
	} else if r.Status != gfdi.StatusACK {
		return nil, fmt.Errorf("watch: host CONFIGURATION refused with status %d", r.Status)
	}
	if opt.Features != nil {
		// Best effort: a watch that does not answer is synced all the same.
		fctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if m, err := c.Call(fctx, smart.FeatureCapabilitiesRequest(*opt.Features)); err != nil {
			c.log.Info("watch: no answer to the feature capabilities", "err", err)
		} else if env, err := smart.ParseEnvelope(m.Data); err == nil {
			if f, ok := smart.ParseFeatureCapabilitiesResponse(env); ok {
				c.Features = &f
				c.log.Info("watch: feature capabilities", "guid", f.GUIDStatus, "wellness", f.WellnessSupport,
					"lifestyle_logging", f.LifestyleLoggingSupport, "fields", f.Fields)
			}
		}
		cancel()
	}
	r, err := c.Session.Request(ctx, gfdi.SupportedFileTypesRequest, nil)
	if err != nil {
		return nil, fmt.Errorf("watch: SUPPORTED_FILE_TYPES: %w", err)
	}
	c.FileTypes = ParseFileTypes(r.Data)
	// AUTO_UPLOAD on, weather conditions on, weather alerts off: Garmin Connect's and
	// Gadgetbridge's settings (docs/protocol/05 §3.11).
	if _, err := c.Session.Request(ctx, gfdi.DeviceSettings, []byte{3, 6, 1, 1, 7, 1, 1, 8, 1, 0}); err != nil {
		return nil, fmt.Errorf("watch: DEVICE_SETTINGS: %w", err)
	}
	if err := c.SystemEvent(ctx, EventSyncReady); err != nil {
		return nil, err
	}
	if opt.FirstSession {
		for _, ev := range []SystemEvent{EventPairComplete, EventSyncComplete, EventSetupWizardComplete} {
			if err := c.SystemEvent(ctx, ev); err != nil {
				return nil, err
			}
		}
	}
	ok = true
	return c, nil
}

func (c *Conn) sessionErr() error {
	if err := c.Session.Err(); err != nil {
		return err
	}
	if err := c.Mux.Err(); err != nil {
		return err
	}
	return gfdi.ErrSessionClosed
}

func (c *Conn) shutdown() {
	if c.Session != nil {
		c.Session.Close()
	}
	c.Mux.Close()
}

// Close ends the session: CLOSE_ALL (best effort, 3 s), then stops the session and the mux.
// The link stays with the caller.
func (c *Conn) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := c.Mux.CloseAll(ctx)
	c.shutdown()
	return err
}

// Done is closed when the connection ends (link loss, MLR failure, Close).
func (c *Conn) Done() <-chan struct{} { return c.Session.Done() }

// Err tells why the connection ended.
func (c *Conn) Err() error { return c.sessionErr() }

// Call sends a Smart message as a PROTOBUF_REQUEST and returns the watch's answer.
func (c *Conn) Call(ctx context.Context, msg []byte) (gfdi.ProtobufMessage, error) {
	return c.Session.Call(ctx, msg)
}

// SyncRequests delivers a value whenever the watch asks for a sync (SYNCHRONIZATION 5037).
func (c *Conn) SyncRequests() <-chan struct{} { return c.syncReqCh }

// Handle installs the handler for protobuf messages of a Smart service (nil removes it).
func (c *Conn) Handle(service int, h ProtobufHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if h == nil {
		delete(c.handlers, service)
		return
	}
	c.handlers[service] = h
}

// Stats counts the frames the watch sent, by message ID.
func (c *Conn) Stats() map[gfdi.MessageID]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[gfdi.MessageID]int, len(c.stats))
	for k, v := range c.stats {
		out[k] = v
	}
	return out
}

// SystemEvent is the event of a SYSTEM_EVENT (5030) message.
type SystemEvent byte

// System events goarmin sends (docs/protocol/02 §5.4).
const (
	EventSyncComplete        SystemEvent = 0
	EventSyncFail            SystemEvent = 1
	EventPairComplete        SystemEvent = 4
	EventSyncReady           SystemEvent = 8
	EventSetupWizardComplete SystemEvent = 14
	EventTimeUpdated         SystemEvent = 16
)

// SystemEvent sends a SYSTEM_EVENT with value 0 and waits for its RESPONSE.
func (c *Conn) SystemEvent(ctx context.Context, ev SystemEvent) error {
	r, err := c.Session.Request(ctx, gfdi.SystemEvent, []byte{byte(ev), 0})
	if err != nil {
		return fmt.Errorf("watch: SYSTEM_EVENT %d: %w", ev, err)
	}
	if r.Status != gfdi.StatusACK {
		return fmt.Errorf("watch: SYSTEM_EVENT %d: status %d", ev, r.Status)
	}
	return nil
}

// OpenPipe registers the next file-transfer service as a reliable channel, rotating through
// multilink.FileTransferServices as Garmin Connect does. Services the watch refuses are
// skipped.
func (c *Conn) OpenPipe(ctx context.Context) (*multilink.Channel, error) {
	var last error
	for range multilink.FileTransferServices {
		c.mu.Lock()
		svc := multilink.FileTransferServices[c.pipeNext%len(multilink.FileTransferServices)]
		c.pipeNext++
		c.mu.Unlock()
		ch, err := c.Mux.Register(ctx, svc, true)
		if err == nil {
			if !ch.Reliable() {
				_ = ch.Close(ctx)
				return nil, fmt.Errorf("watch: the watch gave pipe %s a plain handle", svc)
			}
			return ch, nil
		}
		var re *multilink.RegisterError
		if !errors.As(err, &re) {
			return nil, err
		}
		last = err
	}
	return nil, fmt.Errorf("watch: no file-transfer service available: %w", last)
}

// ParseFileTypes decodes the data of the RESPONSE to SUPPORTED_FILE_TYPES_REQUEST:
// [count][count × (data type, sub type, lpstr name)].
func ParseFileTypes(data []byte) []FileType {
	if len(data) < 1 {
		return nil
	}
	var out []FileType
	n, i := int(data[0]), 1
	for k := 0; k < n && i+3 <= len(data); k++ {
		l := int(data[i+2])
		if i+3+l > len(data) {
			break
		}
		out = append(out, FileType{DataType: data[i], SubType: data[i+1], Name: string(data[i+3 : i+3+l])})
		i += 3 + l
	}
	return out
}

func (c *Conn) onFrame(s *gfdi.Session, f gfdi.Frame) {
	c.mu.Lock()
	c.stats[f.ID]++
	c.mu.Unlock()
	switch f.ID {
	case gfdi.DeviceInformation:
		d, err := gfdi.ParseDeviceInfo(f.Payload)
		if err != nil {
			c.log.Warn("watch: bad DEVICE_INFORMATION", "err", err)
			_ = s.Reply(f, gfdi.StatusDecodeError, nil)
			return
		}
		s.SetMaxPacket(int(d.MaxPacketSize))
		// One RESPONSE with our identity, no bare ACK first: Garmin Connect's form.
		_ = s.Reply(f, gfdi.StatusACK, c.host.ReplyData())
		select {
		case c.devInfoCh <- d:
		default:
		}
	case gfdi.Configuration:
		caps, err := gfdi.ParseConfiguration(f.Payload)
		if err != nil {
			_ = s.Reply(f, gfdi.StatusDecodeError, nil)
			return
		}
		_ = s.Reply(f, gfdi.StatusACK, nil)
		select {
		case c.configCh <- caps:
		default:
		}
	case gfdi.CurrentTimeRequest:
		var ref uint32
		if len(f.Payload) >= 4 {
			ref = uint32(f.Payload[0]) | uint32(f.Payload[1])<<8 | uint32(f.Payload[2])<<16 | uint32(f.Payload[3])<<24
		}
		_ = s.Reply(f, gfdi.StatusACK, CurrentTimeReply(ref, c.opt.Clock.Now(), c.opt.Location))
	case gfdi.AuthNegotiation:
		c.log.Warn("watch: AUTH_NEGOTIATION received; the link may not be OS-bonded")
		data := []byte{0} // GUESS_OK, echoing the watch's values
		if len(f.Payload) >= 5 {
			data = append(data, f.Payload[:5]...)
		}
		_ = s.Reply(f, gfdi.StatusACK, data)
	case gfdi.MusicControlCapabilities:
		_ = s.Reply(f, gfdi.StatusACK, []byte{0}) // no music commands
	case gfdi.NotificationSubscription:
		// Garmin Connect answers 00 followed by the request (capture, note 05).
		_ = s.Reply(f, gfdi.StatusACK, append([]byte{0}, f.Payload...))
	case gfdi.FitDefinition, gfdi.FitData:
		_ = s.Reply(f, gfdi.StatusACK, []byte{0}) // APPLIED
	case gfdi.Synchronization:
		_ = s.Reply(f, gfdi.StatusACK, nil)
		select {
		case c.syncReqCh <- struct{}{}:
		default:
		}
	case gfdi.BatteryStatus:
		if b, ok := ParseBattery(f.Payload, c.opt.Clock.Now()); ok {
			c.mu.Lock()
			c.battery = b
			c.mu.Unlock()
		}
		_ = s.Reply(f, gfdi.StatusACK, nil)
	case gfdi.WeatherRequest, gfdi.FileAvailable, gfdi.FindMyPhoneRequest,
		gfdi.FindMyPhoneCancel, gfdi.MusicControl, gfdi.CreateFile, gfdi.NotificationControl,
		gfdi.NotificationData, gfdi.NotificationUpdate:
		_ = s.Reply(f, gfdi.StatusACK, nil)
	case gfdi.MusicControlEntityUpdate:
		// No status for this one (Gadgetbridge).
	default:
		_ = s.Reply(f, gfdi.StatusUnsupported, nil)
	}
}

// onProtobuf applies the installed handlers, then the default policy: the Core connection
// notification is kept without a reply, location requests are told there is no location, HTTP
// requests that there is no network, ExploreSync is rejected, FileAccess messages nobody
// handles are kept, everything else is discarded.
func (c *Conn) onProtobuf(_ *gfdi.Session, m gfdi.ProtobufMessage) gfdi.ProtobufReply {
	env, err := smart.ParseEnvelope(m.Data)
	if err != nil {
		c.log.Debug("watch: protobuf message without a Smart envelope", "err", err)
		return gfdi.ProtobufReply{ChunkStatus: gfdi.ChunkDiscarded, Code: gfdi.ProtobufParseError}
	}
	c.mu.Lock()
	h := c.handlers[env.Service]
	c.mu.Unlock()
	if h != nil {
		if rep, ok := h(m, env); ok {
			return rep
		}
	}
	switch {
	case smart.IsCoreConnectionReady(env):
		return gfdi.Kept(nil)
	case smart.IsWellnessThresholdMet(env):
		// The watch has collected enough wellness data for the host to cut files.
		c.mu.Lock()
		c.wellness++
		c.mu.Unlock()
		c.log.Info("watch: wellness threshold met")
		return gfdi.Kept(nil)
	case smart.IsLifestyleLoggingUpdate(env):
		c.log.Info("watch: lifestyle logging update requested")
		return gfdi.Kept(nil)
	case m.Kind == gfdi.ProtobufRequest:
		if reply, ok := smart.HTTPNoNetworkReply(env); ok {
			return gfdi.Kept(reply)
		}
		if reply, ok := smart.ExploreSyncReject(env); ok {
			return gfdi.Kept(reply)
		}
		if reply, ok := smart.CoreLocationReply(env); ok {
			// "No location", as Garmin Connect answers without location sharing. The watch
			// waits for these answers before it writes out its monitoring data (06, M2).
			return gfdi.Kept(reply)
		}
		if env.Service == smart.ServiceConnectIQHTTP {
			// OAuth requests and the open-webpage notification: their responses have no "no
			// network" status, so they are acknowledged without an answer, as Gadgetbridge does.
			return gfdi.Kept(nil)
		}
	}
	if env.Service == smart.ServiceFileAccess || m.Kind == gfdi.ProtobufResponse {
		return gfdi.Kept(nil)
	}
	c.log.Debug("watch: unhandled protobuf message", "service", smart.ServiceName(env.Service), "kind", m.Kind)
	return gfdi.Discarded()
}

// Opener opens the link on GATT characteristic pair n (0 = 2810/2820). closer releases it.
type Opener func(ctx context.Context, pair int) (link multilink.Link, closer io.Closer, err error)

// Attempt records one try of Dial.
type Attempt struct {
	Pair   int
	Client uint64
	Err    error
}

// Dial tries each characteristic pair with each client ID until a handshake succeeds
// (docs/protocol/06: pair 0 with client 2 works on the Fenix 8; Garmin Connect uses pair 1
// with client 1). It returns the connection, the closer of its link and every attempt made.
// opt.Client is ignored.
func Dial(ctx context.Context, open Opener, pairs []int, clients []uint64, opt Options) (*Conn, io.Closer, []Attempt, error) {
	if len(pairs) == 0 {
		pairs = []int{0, 1, 2, 3}
	}
	if len(clients) == 0 {
		clients = []uint64{multilink.ClientGadgetbridge, multilink.ClientGarminConnect}
	}
	var tries []Attempt
	for _, pair := range pairs {
		for _, client := range clients {
			if err := ctx.Err(); err != nil {
				return nil, nil, tries, err
			}
			link, closer, err := open(ctx, pair)
			if err != nil {
				tries = append(tries, Attempt{Pair: pair, Client: client, Err: err})
				break // the pair itself is unusable; the next client ID will not help
			}
			o := opt
			o.Client = client
			c, err := Handshake(ctx, link, o)
			if err == nil {
				tries = append(tries, Attempt{Pair: pair, Client: client})
				return c, closer, tries, nil
			}
			tries = append(tries, Attempt{Pair: pair, Client: client, Err: err})
			_ = closer.Close()
			if errors.Is(err, ErrNoSync2) {
				return nil, nil, tries, err
			}
		}
	}
	return nil, nil, tries, fmt.Errorf("watch: no characteristic pair and client ID worked (%d attempts)", len(tries))
}
