// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package fakewatch is an in-process Garmin watch for tests. It speaks the watch side of
// Multi-Link, MLR, GFDI and FileAccess the way our Fenix 8 does in the capture of
// docs/protocol/05:
//   - DEVICE_INFORMATION in a plain frame, compact frames from CONFIGURATION on;
//   - FileAccess listings paged with session IDs, a gdxml_string_key table, a
//     next_transaction_id cursor;
//   - pulls over a file pipe the watch closes itself, followed by TransferStatusRequest;
//   - the synced flag set by ModifyFlagsRequest.
//
// Faults can be injected at each step.
package fakewatch

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/karamble/goarmin/pkg/clock"
	"github.com/karamble/goarmin/pkg/garmin/gfdi"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/smart"
)

// Fenix8Capabilities is the 79-bit CONFIGURATION bitmap our Fenix 8 sent (docs/protocol/05 §3.7).
func Fenix8Capabilities() gfdi.Capabilities {
	return gfdi.CapabilitiesFromBits(17, 0, 1, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20,
		21, 25, 29, 33, 34, 35, 36, 37, 38, 39, 41, 42, 43, 44, 46, 47, 49, 50, 51, 52, 54, 57, 58, 59, 62,
		65, 67, 68, 69, 70, 71, 72, 74, 75, 76, 77, 78, 81, 82, 84, 85, 87, 88, 89, 90, 91, 92, 94, 95, 96,
		97, 98, 100, 101, 105, 112, 115, 117, 128)
}

// fenix8FileTypes is the SUPPORTED_FILE_TYPES list our Fenix 8 returned.
var fenix8FileTypes = []struct {
	dt, st byte
	name   string
}{
	{128, 4, "FIT_TYPE_4"}, {128, 32, "FIT_TYPE_32"}, {128, 35, "FIT_TYPE_35"}, {128, 38, "FIT_TYPE_38"},
	{128, 44, "FIT_TYPE_44"}, {128, 41, "FIT_TYPE_41"}, {128, 49, "FIT_TYPE_49"},
	{255, 245, "ErrorShutdownReports"}, {255, 244, "IQErrorReports"}, {128, 58, "FIT_TYPE_58"},
	{255, 247, "ULFLogs"}, {128, 68, "FIT_TYPE_68"}, {128, 70, "FIT_TYPE_70"}, {128, 73, "FIT_TYPE_73"},
	{128, 72, "FIT_TYPE_72"}, {128, 74, "FIT_TYPE_74"},
}

// File is one item on the fake watch.
type File struct {
	UID           smart.UUID
	TypeName      string
	Data          []byte
	TransactionID uint32
	Urgency       smart.Urgency
	Synced        bool
}

// Faults injects failures. Each hook gets the file and how many times the operation has been
// attempted on it before (0 the first time).
type Faults struct {
	// PullResult, if it returns ok, answers PullItemDataRequest with that result instead.
	PullResult func(f *File, attempt int) (r smart.ItemAccessResult, ok bool)
	// StallPull: answer the pull but never send data on the pipe.
	StallPull func(f *File, attempt int) bool
	// AbortPull: instead of data, report FailureReason TRANSPORT_FAILED.
	AbortPull func(f *File, attempt int) bool
	// CorruptPull: flip a byte in the compressed stream.
	CorruptPull func(f *File, attempt int) bool
	// DropOnArchive: disconnect when the ModifyFlagsRequest arrives, before applying it.
	DropOnArchive func(f *File, attempt int) bool
	// DeleteResult, if it returns ok, answers DeleteItemRequest with that result and keeps
	// the file.
	DeleteResult func(f *File, attempt int) (r smart.DeleteItemResult, ok bool)
}

// Options configures a fake watch. The zero value is a Fenix 8 on firmware 23.40.
type Options struct {
	Device gfdi.DeviceInfo
	// PlainMaxPacket is announced when GFDI is not reliable (400 on the Fenix 8).
	PlainMaxPacket uint16
	Capabilities   gfdi.Capabilities
	// PageSize is the number of items per ItemListResponse page (25).
	PageSize int
	// CompressionWindow is announced in PullItemDataResponse (11).
	CompressionWindow uint32
	// AskTime sends a CURRENT_TIME_REQUEST after the handshake.
	AskTime bool
	MLR     multilink.MLRConfig
	Clock   clock.Clock
	Logger  *slog.Logger
	Faults  Faults
	// CutDelay is how long after a CutWellnessFilesRequest the watch announces the files it
	// wrote (50 ms).
	CutDelay time.Duration
	// RemoveOnArchive, if set, picks files the watch removes as soon as they are flagged
	// synced, as the Fenix 8 does with its health files (docs/protocol/06, M2).
	RemoveOnArchive func(f *File) bool
	// Battery is the level it reports when asked (DeviceStatus); 0 means 80 %.
	Battery int
}

// Watch is a fake watch with its files. One Watch can serve several connections in turn.
type Watch struct {
	opt Options
	log *slog.Logger

	mu        sync.Mutex
	files     []*File
	byUID     map[smart.UUID]*File
	nextTxn   uint32
	nextItem  uint32
	attempts  map[string]int // "<op> <uid>" → count
	stats     Stats
	conn      *conn  // the connection being served, if any
	unwritten []File // wellness data the watch writes out on a CutWellnessFilesRequest
}

// Stats counts what hosts did.
type Stats struct {
	Connections, Listings, Pages, Pulls, Archives, Cancels, Deletes, Cuts int
	BytesSent                                                             int
	TimeReply                                                             []byte // data of the last CURRENT_TIME RESPONSE
	HostCaps                                                              gfdi.Capabilities
	HostInfo                                                              []byte // data of the DEVICE_INFORMATION RESPONSE
	PipeServices                                                          []multilink.Service
	SystemEvents                                                          []byte // the event of each SYSTEM_EVENT, in order
	FeatureRequests                                                       int    // FeatureCapabilitiesRequests answered
}

// New returns a fake watch without files.
func New(opt Options) *Watch {
	if opt.Device.ProductNumber == 0 {
		opt.Device = gfdi.DeviceInfo{ProtocolVersion: 151, ProductNumber: 4536, UnitNumber: 3_500_000_001,
			SoftwareVersion: 2340, MaxPacketSize: 4000, BluetoothName: "fenix 8 - 47mm", DeviceName: "fenix",
			DeviceModel: "8 - 47mm", Tail: []byte{0, 0}}
	}
	if opt.PlainMaxPacket == 0 {
		opt.PlainMaxPacket = 400
	}
	if opt.Capabilities == nil {
		opt.Capabilities = Fenix8Capabilities()
	}
	if opt.PageSize <= 0 {
		opt.PageSize = 25
	}
	if opt.CompressionWindow == 0 {
		opt.CompressionWindow = 11
	}
	if opt.Clock == nil {
		opt.Clock = clock.Real()
	}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Watch{opt: opt, log: opt.Logger, byUID: map[smart.UUID]*File{}, nextTxn: 38000, nextItem: 0x70,
		attempts: map[string]int{}}
}

// AddFile adds a file and returns it. During a connection the host is told with an
// ItemAddedNotification.
func (w *Watch) AddFile(typeName string, data []byte) *File {
	w.mu.Lock()
	w.nextItem++
	ts := uint64(w.opt.Clock.Now().Unix() - 631065600)
	f := &File{
		// The uid layout the capture suggests: Garmin timestamp << 32 | item number, then
		// 0x01 << 56 | size.
		UID:           smart.UUID{MSB: ts<<32 | uint64(w.nextItem), LSB: 1<<56 | uint64(len(data))},
		TypeName:      typeName,
		Data:          bytes.Clone(data),
		TransactionID: w.nextTxn,
		Urgency:       smart.UrgencySlow,
	}
	w.nextTxn++
	w.files = append(w.files, f)
	w.byUID[f.UID] = f
	c := w.conn
	w.mu.Unlock()
	if c != nil {
		c.notifyAdded(f)
	}
	return f
}

// Files returns a snapshot of the files.
func (w *Watch) Files() []File {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]File, len(w.files))
	for i, f := range w.files {
		out[i] = *f
	}
	return out
}

// AddUnwritten queues wellness data that the watch has not written into a file yet: it
// becomes a file, announced with ItemAddedNotification, after the next CutWellnessFilesRequest.
func (w *Watch) AddUnwritten(typeName string, data []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.unwritten = append(w.unwritten, File{TypeName: typeName, Data: data})
}

func (w *Watch) cut() {
	w.mu.Lock()
	queued := w.unwritten
	w.unwritten = nil
	w.stats.Cuts++
	delay := w.opt.CutDelay
	w.mu.Unlock()
	if delay <= 0 {
		delay = 50 * time.Millisecond
	}
	if len(queued) == 0 {
		return
	}
	time.AfterFunc(delay, func() {
		for _, f := range queued {
			w.AddFile(f.TypeName, f.Data)
		}
	})
}

// Unsynced returns the number of files not flagged as synced.
func (w *Watch) Unsynced() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, f := range w.files {
		if !f.Synced {
			n++
		}
	}
	return n
}

// Stats returns a snapshot of the counters.
func (w *Watch) Stats() Stats {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.stats
	s.PipeServices = append([]multilink.Service(nil), s.PipeServices...)
	s.SystemEvents = append([]byte(nil), s.SystemEvents...)
	return s
}

func (w *Watch) attempt(op string, f *File) int {
	key := op + " " + f.UID.String()
	n := w.attempts[key]
	w.attempts[key] = n + 1
	return n
}

// Serve plays the watch on link until the link closes or ctx ends. It closes the link when it
// returns.
func (w *Watch) Serve(ctx context.Context, link LinkCloser) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	mux := multilink.NewDeviceMux(link, multilink.Options{Clock: w.opt.Clock, MLR: w.opt.MLR, Logger: w.log}, nil)
	c := &conn{w: w, mux: mux, link: link, keys: map[string]uint32{}, listings: map[uint32]*listing{},
		transfers: map[uint32]*transfer{}, nextHandle: 23, nextSession: 1, ctx: ctx}
	w.mu.Lock()
	w.conn = c
	w.stats.Connections++
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		if w.conn == c {
			w.conn = nil
		}
		w.mu.Unlock()
		if c.sess != nil {
			c.sess.Close()
		}
		mux.Close()
		link.Close()
	}()
	for {
		ch, err := mux.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return mux.Err()
		}
		switch svc := ch.Service(); {
		case svc == multilink.ServiceGFDI:
			c.startGFDI(ch)
		case svc&0x0FFF == 0x018:
			w.mu.Lock()
			w.stats.PipeServices = append(w.stats.PipeServices, svc)
			w.mu.Unlock()
			go c.servePipe(ch)
		}
	}
}

// LinkCloser is a link the fake watch can disconnect.
type LinkCloser interface {
	multilink.Link
	Close() error
}

type conn struct {
	w           *Watch
	mux         *multilink.Mux
	link        LinkCloser
	ctx         context.Context
	mu          sync.Mutex
	sess        *gfdi.Session
	ready       bool
	keys        map[string]uint32
	listings    map[uint32]*listing
	transfers   map[uint32]*transfer
	nextHandle  uint32
	nextSession uint32
}

type listing struct {
	items []*File
	next  uint32
}

type transfer struct {
	f       *File
	attempt int
	handle  uint32
}

func (c *conn) startGFDI(ch *multilink.Channel) {
	sess := gfdi.NewSession(ch, gfdi.SessionOptions{
		Compact: gfdi.CompactNever, Clock: c.w.opt.Clock, Logger: c.w.log,
		OnFrame: c.onFrame, OnProtobuf: c.onProtobuf,
	})
	c.mu.Lock()
	c.sess = sess
	c.mu.Unlock()
	go c.handshake(sess, ch.Reliable())
}

// handshake sends what the Fenix 8 sends unprompted: DEVICE_INFORMATION, then CONFIGURATION.
func (c *conn) handshake(sess *gfdi.Session, reliable bool) {
	ctx, cancel := context.WithTimeout(c.ctx, 20*time.Second)
	defer cancel()
	d := c.w.opt.Device
	if !reliable {
		d.MaxPacketSize = c.w.opt.PlainMaxPacket
	}
	sess.SetMaxPacket(int(d.MaxPacketSize))
	r, err := sess.Request(ctx, gfdi.DeviceInformation, d.Payload())
	if err != nil {
		c.w.log.Debug("fakewatch: DEVICE_INFORMATION", "err", err)
		return
	}
	c.w.mu.Lock()
	c.w.stats.HostInfo = r.Data
	c.w.mu.Unlock()
	sess.SetCompact(true)
	if _, err := sess.Request(ctx, gfdi.Configuration, c.w.opt.Capabilities.Payload()); err != nil {
		c.w.log.Debug("fakewatch: CONFIGURATION", "err", err)
	}
}

func (c *conn) onFrame(s *gfdi.Session, f gfdi.Frame) {
	switch f.ID {
	case gfdi.Configuration:
		caps, _ := gfdi.ParseConfiguration(f.Payload)
		c.w.mu.Lock()
		c.w.stats.HostCaps = caps
		c.w.mu.Unlock()
		_ = s.Reply(f, gfdi.StatusACK, nil)
	case gfdi.SupportedFileTypesRequest:
		data := []byte{byte(len(fenix8FileTypes))}
		for _, t := range fenix8FileTypes {
			data = append(data, t.dt, t.st, byte(len(t.name)))
			data = append(data, t.name...)
		}
		_ = s.Reply(f, gfdi.StatusACK, data)
	case gfdi.DeviceSettings:
		_ = s.Reply(f, gfdi.StatusACK, []byte{1})
	case gfdi.SystemEvent:
		_ = s.Reply(f, gfdi.StatusACK, []byte{0})
		if len(f.Payload) > 0 {
			c.w.mu.Lock()
			c.w.stats.SystemEvents = append(c.w.stats.SystemEvents, f.Payload[0])
			c.w.mu.Unlock()
		}
		if len(f.Payload) > 0 && f.Payload[0] == 8 { // SYNC_READY
			c.mu.Lock()
			c.ready = true
			c.mu.Unlock()
			if c.w.opt.AskTime {
				go c.askTime(s)
			}
		}
	default:
		_ = s.Reply(f, gfdi.StatusACK, nil)
	}
}

func (c *conn) askTime(s *gfdi.Session) {
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	r, err := s.Request(ctx, gfdi.CurrentTimeRequest, []byte{0x91, 0x3C, 0x1B, 0x00})
	if err != nil {
		return
	}
	c.w.mu.Lock()
	c.w.stats.TimeReply = r.Data
	c.w.mu.Unlock()
}

func fileAccessReply(fa smart.FileAccess) gfdi.ProtobufReply { return gfdi.Kept(fa.Smart()) }

func (c *conn) onProtobuf(_ *gfdi.Session, m gfdi.ProtobufMessage) gfdi.ProtobufReply {
	env, err := smart.ParseEnvelope(m.Data)
	if err == nil && m.Kind == gfdi.ProtobufRequest && smart.IsCutWellnessFilesRequest(env) {
		c.w.cut()
		return gfdi.Kept(smart.CutWellnessFilesResponse(smart.CutWellnessSuccess))
	}
	if err == nil && m.Kind == gfdi.ProtobufRequest && smart.IsFeatureCapabilitiesRequest(env) {
		c.w.mu.Lock()
		c.w.stats.FeatureRequests++
		c.w.mu.Unlock()
		return gfdi.Kept(smart.FeatureCapabilitiesResponse())
	}
	if err == nil && m.Kind == gfdi.ProtobufRequest && smart.IsBatteryRequest(env) {
		level := c.w.opt.Battery
		if level == 0 {
			level = 80
		}
		return gfdi.Kept(smart.BatteryResponse(level))
	}
	if err != nil || env.Service != smart.ServiceFileAccess || m.Kind != gfdi.ProtobufRequest {
		return gfdi.Kept(nil)
	}
	fa, err := env.FileAccess()
	if err != nil {
		return gfdi.ProtobufReply{ChunkStatus: gfdi.ChunkDiscarded, Code: gfdi.ProtobufParseError}
	}
	switch {
	case fa.ItemListRequest != nil:
		return fileAccessReply(smart.FileAccess{ItemListResponse: c.list(fa.ItemListRequest)})
	case fa.PullItemRequest != nil:
		return fileAccessReply(smart.FileAccess{PullItemResponse: c.pull(fa.PullItemRequest)})
	case fa.ModifyFlagsRequest != nil:
		resp, drop := c.modifyFlags(fa.ModifyFlagsRequest)
		if drop {
			c.link.Close()
			return gfdi.Kept(nil)
		}
		return fileAccessReply(smart.FileAccess{ModifyFlagsResponse: resp})
	case fa.DeleteItemRequest != nil:
		return fileAccessReply(smart.FileAccess{DeleteItemResponse: c.deleteItem(fa.DeleteItemRequest)})
	case fa.CancelTransferRequest != nil:
		c.w.mu.Lock()
		c.w.stats.Cancels++
		c.w.mu.Unlock()
		return fileAccessReply(smart.FileAccess{CancelTransferResponse: &smart.CancelTransferResponse{Status: smart.Ptr(smart.CancelTransferSuccess)}})
	}
	return gfdi.Discarded()
}

// itemRef builds a listing entry; a type name is spelled out only the first time its key is
// used on the connection, as the Fenix 8 does.
func (c *conn) itemRefLocked(f *File) smart.ItemReference {
	dt := &smart.DataType{Format: smart.Ptr(smart.FormatGDXMLDataType)}
	key, ok := c.keys[f.TypeName]
	if !ok {
		key = uint32(len(c.keys))
		c.keys[f.TypeName] = key
		dt.GDXMLString = smart.S(f.TypeName)
	}
	dt.GDXMLStringKey = smart.U32(key)
	return smart.ItemReference{
		UID: smart.Ptr(f.UID), DataType: dt, DataSize: smart.U64(uint64(len(f.Data))),
		RequestedFlags: smart.U32(0), TransactionID: smart.U32(f.TransactionID), Urgency: smart.Ptr(f.Urgency),
	}
}

func (c *conn) list(req *smart.ItemListRequest) *smart.ItemListResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.w
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stats.Pages++
	var l *listing
	if req.SessionID != nil {
		l = c.listings[*req.SessionID]
		if l == nil {
			return &smart.ItemListResponse{Status: smart.Ptr(smart.ItemListFailUnknownSessionID)}
		}
		delete(c.listings, *req.SessionID)
	} else {
		w.stats.Listings++
		excludeSynced := false
		for _, u := range req.ExcludeAllFlags {
			if u == smart.SyncedFlag {
				excludeSynced = true
			}
		}
		l = &listing{next: w.nextTxn}
		for _, f := range w.files {
			if excludeSynced && f.Synced {
				continue
			}
			if req.TransactionID != nil && f.TransactionID < *req.TransactionID {
				continue
			}
			l.items = append(l.items, f)
		}
	}
	resp := &smart.ItemListResponse{Status: smart.Ptr(smart.ItemListSuccess)}
	n := min(w.opt.PageSize, len(l.items))
	for _, f := range l.items[:n] {
		resp.Items = append(resp.Items, c.itemRefLocked(f))
	}
	l.items = l.items[n:]
	if len(l.items) > 0 {
		id := c.nextSession
		c.nextSession++
		c.listings[id] = l
		resp.SessionID = smart.U32(id)
	} else {
		resp.NextTransactionID = smart.U32(l.next)
	}
	return resp
}

func (c *conn) pull(req *smart.PullItemDataRequest) *smart.PullItemDataResponse {
	w := c.w
	w.mu.Lock()
	var f *File
	if req.Item != nil && req.Item.UID != nil {
		f = w.byUID[*req.Item.UID]
	}
	if f == nil {
		w.mu.Unlock()
		return &smart.PullItemDataResponse{Result: smart.Ptr(smart.ItemAccessUnknownItem)}
	}
	n := w.attempt("pull", f)
	w.stats.Pulls++
	w.mu.Unlock()
	if hook := w.opt.Faults.PullResult; hook != nil {
		if r, ok := hook(f, n); ok {
			return &smart.PullItemDataResponse{Result: smart.Ptr(r)}
		}
	}
	c.mu.Lock()
	handle := c.nextHandle
	c.nextHandle++
	c.transfers[handle] = &transfer{f: f, attempt: n, handle: handle}
	c.mu.Unlock()
	return &smart.PullItemDataResponse{
		Result: smart.Ptr(smart.ItemAccessSuccess), Transport: smart.Ptr(smart.TransportMultilinkPipe),
		TransferHandle: smart.U32(handle), CompressionWindow: smart.U32(w.opt.CompressionWindow),
	}
}

// servePipe answers the host's pipe configure with the file of that transfer.
func (c *conn) servePipe(ch *multilink.Channel) {
	ctx, cancel := context.WithTimeout(c.ctx, 60*time.Second)
	defer cancel()
	var cfg []byte
	for len(cfg) < 6 {
		select {
		case p, ok := <-ch.Recv():
			if !ok {
				return
			}
			cfg = append(cfg, p...)
		case <-ctx.Done():
			return
		}
	}
	handle := uint32(binary.LittleEndian.Uint16(cfg[2:4]))
	c.mu.Lock()
	t := c.transfers[handle]
	delete(c.transfers, handle)
	sess := c.sess
	c.mu.Unlock()
	if t == nil || cfg[0] != 0 || cfg[1] != 0 {
		_ = ch.Write([]byte{1, 0, 0})
		return
	}
	faults := c.w.opt.Faults
	if faults.StallPull != nil && faults.StallPull(t.f, t.attempt) {
		_ = ch.Write([]byte{0, 0, 0})
		<-ctx.Done()
		return
	}
	if faults.AbortPull != nil && faults.AbortPull(t.f, t.attempt) {
		_ = ch.Write([]byte{0, 0, 0})
		c.transferStatus(ctx, sess, t, smart.Ptr(smart.FailureTransportFailed))
		return
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, _ = zw.Write(t.f.Data)
	_ = zw.Close()
	body := z.Bytes()
	if faults.CorruptPull != nil && faults.CorruptPull(t.f, t.attempt) {
		body[len(body)/2] ^= 0xFF
	}
	_ = ch.Write(append([]byte{0, 0, 0}, body...))
	if err := ch.Flush(ctx); err != nil {
		return
	}
	_ = ch.Close(ctx) // the watch closes the pipe itself
	c.w.mu.Lock()
	c.w.stats.BytesSent += len(body)
	c.w.mu.Unlock()
	c.transferStatus(ctx, sess, t, nil)
}

func (c *conn) transferStatus(ctx context.Context, sess *gfdi.Session, t *transfer, failure *smart.FailureReason) {
	req := smart.FileAccess{TransferStatusRequest: &smart.TransferStatusRequest{
		UID: smart.Ptr(t.f.UID), TransferHandle: smart.U32(t.handle), FailureReason: failure,
	}}
	if _, err := sess.Call(ctx, req.Smart()); err != nil {
		c.w.log.Debug("fakewatch: TransferStatusRequest", "err", err)
	}
}

func (c *conn) modifyFlags(req *smart.ModifyFlagsRequest) (*smart.ModifyFlagsResponse, bool) {
	w := c.w
	w.mu.Lock()
	var f *File
	if req.UID != nil {
		f = w.byUID[*req.UID]
	}
	if f == nil {
		w.mu.Unlock()
		return &smart.ModifyFlagsResponse{Status: smart.Ptr(smart.ModifyFlagsFailItemNotFound)}, false
	}
	n := w.attempt("archive", f)
	w.mu.Unlock()
	if hook := w.opt.Faults.DropOnArchive; hook != nil && hook(f, n) {
		return nil, true
	}
	w.mu.Lock()
	for _, u := range req.SetFlags {
		if u == smart.SyncedFlag {
			f.Synced = true
			w.stats.Archives++
		}
	}
	for _, u := range req.ClearFlags {
		if u == smart.SyncedFlag {
			f.Synced = false
		}
	}
	if f.Synced && w.opt.RemoveOnArchive != nil && w.opt.RemoveOnArchive(f) {
		w.removeLocked(f)
	}
	w.mu.Unlock()
	go c.notify(smart.FileAccess{ItemUpdated: &smart.ItemUpdatedNotification{UID: smart.Ptr(f.UID), ChangedFlagsSet: req.SetFlags}})
	return &smart.ModifyFlagsResponse{Status: smart.Ptr(smart.ModifyFlagsSuccess)}, false
}

func (c *conn) deleteItem(req *smart.DeleteItemRequest) *smart.DeleteItemResponse {
	w := c.w
	w.mu.Lock()
	defer w.mu.Unlock()
	var f *File
	if req.UID != nil {
		f = w.byUID[*req.UID]
	}
	if f == nil {
		return &smart.DeleteItemResponse{Result: smart.Ptr(smart.DeleteItemDoesNotExist)}
	}
	n := w.attempt("delete", f)
	if hook := w.opt.Faults.DeleteResult; hook != nil {
		if r, ok := hook(f, n); ok {
			return &smart.DeleteItemResponse{Result: smart.Ptr(r)}
		}
	}
	w.removeLocked(f)
	w.stats.Deletes++
	return &smart.DeleteItemResponse{Result: smart.Ptr(smart.DeleteItemSuccess)}
}

// removeLocked takes a file off the watch.
func (w *Watch) removeLocked(f *File) {
	delete(w.byUID, f.UID)
	w.files = slices.DeleteFunc(w.files, func(x *File) bool { return x == f })
}

func (c *conn) notifyAdded(f *File) {
	c.mu.Lock()
	ready := c.ready
	var ref smart.ItemReference
	if ready {
		ref = c.itemRefLocked(f)
		ref.Urgency = smart.Ptr(smart.UrgencyNormal)
	}
	c.mu.Unlock()
	if ready {
		go c.notify(smart.FileAccess{ItemAdded: &smart.ItemAddedNotification{Items: []smart.ItemReference{ref}}})
	}
}

func (c *conn) notify(fa smart.FileAccess) {
	c.mu.Lock()
	sess := c.sess
	c.mu.Unlock()
	if sess == nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	if err := sess.Notify(ctx, fa.Smart()); err != nil && !errors.Is(err, context.Canceled) {
		c.w.log.Debug("fakewatch: notification", "kind", fa.Kind(), "err", err)
	}
}

// String describes a file for test messages.
func (f File) String() string {
	return fmt.Sprintf("%s %s (%d bytes, txn %d, synced %v)", f.TypeName, f.UID, len(f.Data), f.TransactionID, f.Synced)
}
