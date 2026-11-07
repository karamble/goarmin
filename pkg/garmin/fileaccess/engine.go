// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package fileaccess runs the FileAccess ("new sync") protocol against a connected watch:
// list the items not yet flagged as synced, pull each over a Multi-Link file pipe, store it,
// and only then flag it as synced (docs/protocol/04 §3, 05 §8).
//
// The watch deletes health files once they are flagged, so the order is strict: a file is
// flagged only after Store.Persist returned, and the listing cursor moves only together with
// the recorded list of pending items.
package fileaccess

import (
	"cmp"
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
	"github.com/karamble/goarmin/pkg/garmin/watch"
)

// Options configures an Engine.
type Options struct {
	// Policy decides what to pull and archive (DefaultPolicy by default).
	Policy Policy
	// NoArchive never flags items as synced. Items that would be archived stay pending,
	// stored, and are archived by the first run without NoArchive.
	NoArchive bool
	// Priority is the transfer priority sent with pulls (24, Garmin Connect's value).
	Priority uint32
	// CompressionWindow is the zlib window requested (15; the Fenix 8 answers 11).
	CompressionWindow uint32
	// Verify checks a pulled file before it is stored, e.g. its FIT CRC. An error counts as a
	// failed pull; the item stays pending.
	Verify func(it Item, data []byte) error
	// IdleTimeout aborts a pull when the pipe stays silent this long (30 s).
	IdleTimeout time.Duration
	// StatusTimeout is how long to wait for the watch's TransferStatusRequest after the data
	// (10 s); its absence is only logged.
	StatusTimeout time.Duration
	// MaxFailures consecutive failed items abort the sync (3).
	MaxFailures int
	// MaxRelist bounds how often the sync lists again because the watch announced new items
	// during it (3).
	MaxRelist int
	// CutWellness asks the watch at the start of the sync to write its current wellness data
	// into files (GarminConnect CutWellnessFilesRequest), as Garmin Connect does; the Fenix 8
	// announces them a moment later, so use it with Settle.
	CutWellness bool
	// Settle is how long the sync waits after its last listing for the watch to announce new
	// items (ItemAddedNotification) before it ends; 0 does not wait. The Fenix 8 writes out its
	// current monitoring data a moment after a sync starts (docs/protocol/06, M2).
	Settle time.Duration
	// MaxFileSize caps a file whose size the listing did not give (256 MiB).
	MaxFileSize uint64
	// Delete, if set, picks archived items to delete from the watch as well, e.g. activities,
	// which the watch keeps after the archive. It never applies with NoArchive. A watch that
	// refuses the delete leaves the item archived.
	Delete func(Item) bool
	// FullListing ignores the stored cursor and lists every unsynced item.
	FullListing bool
	// IncludeSynced lists items already flagged as synced too (by goarmin or another host),
	// as long as the watch keeps them. It implies FullListing for the first listing.
	IncludeSynced bool
	Clock         clock.Clock
	Logger        *slog.Logger
	// OnEvent, if set, is told about every item as it progresses.
	OnEvent func(Event)
}

// EventKind classifies an Event.
type EventKind int

// Event kinds.
const (
	EventListed EventKind = iota + 1
	EventPulled
	EventArchived
	EventKept
	EventDeferred
	EventGone
	EventFailed
	EventDeleted
	// EventQueued opens each round of work: Queued is how many items it will go through.
	EventQueued
)

func (k EventKind) String() string {
	return [...]string{"", "listed", "pulled", "archived", "kept", "deferred", "gone", "failed", "deleted", "queued"}[k]
}

// Event reports progress on one item (or, for EventQueued, on a round).
type Event struct {
	Kind   EventKind
	Item   Item
	Bytes  int
	Err    error
	Queued int
}

// Report summarises a Sync.
type Report struct {
	Listed   int // items the watch listed (all of them, including ignored ones)
	Ignored  int // listed items the policy ignores (device XML, crash reports, …)
	Added    int // listed items that became pending
	Pulled   int
	Archived int
	Kept     int // stored and finished without archiving (PullOnce)
	Deferred int // stored, archive skipped because of NoArchive
	Gone     int
	Failed   int
	Deleted  int // archived, then deleted from the watch (Options.Delete)
	// WellnessCut is the watch's answer to CutWellnessFilesRequest ("" if not asked or no answer).
	WellnessCut string
	Refused     int // archived; the watch refused to delete them
	Bytes       int64
}

// ErrBusy is returned when Sync is called while another Sync runs on the same engine.
var ErrBusy = errors.New("fileaccess: a sync is already running")

var errGone = errors.New("fileaccess: the watch no longer has the item")

// Engine syncs one connected watch. It installs itself as the connection's FileAccess handler.
type Engine struct {
	conn  *watch.Conn
	store Store
	opt   Options
	log   *slog.Logger

	mu        sync.Mutex
	keys      map[uint32]string // gdxml_string_key → type name, for the whole connection
	transfers map[uint32]chan *smart.TransferStatusRequest
	added     bool
	addedSig  chan struct{} // signalled on ItemAddedNotification
	running   bool
	more      bool // another pull follows the current one
}

// New attaches an engine to conn.
func New(conn *watch.Conn, store Store, opt Options) *Engine {
	if opt.Policy == nil {
		opt.Policy = DefaultPolicy
	}
	if opt.Priority == 0 {
		opt.Priority = 24
	}
	if opt.CompressionWindow == 0 {
		opt.CompressionWindow = 15
	}
	if opt.IdleTimeout <= 0 {
		opt.IdleTimeout = 30 * time.Second
	}
	if opt.StatusTimeout <= 0 {
		opt.StatusTimeout = 10 * time.Second
	}
	if opt.MaxFailures <= 0 {
		opt.MaxFailures = 3
	}
	if opt.MaxRelist <= 0 {
		opt.MaxRelist = 3
	}
	if opt.MaxFileSize == 0 {
		opt.MaxFileSize = 256 << 20
	}
	if opt.Clock == nil {
		opt.Clock = clock.Real()
	}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	e := &Engine{conn: conn, store: store, opt: opt, log: opt.Logger,
		keys: map[uint32]string{}, transfers: map[uint32]chan *smart.TransferStatusRequest{},
		addedSig: make(chan struct{}, 1)}
	conn.Handle(smart.ServiceFileAccess, e.onMessage)
	return e
}

// Close detaches the engine from the connection.
func (e *Engine) Close() { e.conn.Handle(smart.ServiceFileAccess, nil) }

func (e *Engine) event(k EventKind, it Item, n int, err error) {
	if e.opt.OnEvent != nil {
		e.opt.OnEvent(Event{Kind: k, Item: it, Bytes: n, Err: err})
	}
}

// onMessage handles FileAccess requests and notifications from the watch.
func (e *Engine) onMessage(_ gfdi.ProtobufMessage, env smart.Envelope) (gfdi.ProtobufReply, bool) {
	fa, err := env.FileAccess()
	if err != nil {
		e.log.Debug("fileaccess: undecodable message", "err", err)
		return gfdi.ProtobufReply{ChunkStatus: gfdi.ChunkDiscarded, Code: gfdi.ProtobufParseError}, true
	}
	switch {
	case fa.TransferStatusRequest != nil:
		req := fa.TransferStatusRequest
		if req.TransferHandle != nil {
			e.mu.Lock()
			ch := e.transfers[*req.TransferHandle]
			e.mu.Unlock()
			if ch != nil {
				select {
				case ch <- req:
				default:
				}
			}
		}
		e.mu.Lock()
		var next *uint32
		if e.more {
			next = smart.U32(e.opt.Priority)
		}
		e.mu.Unlock()
		return gfdi.Kept(TransferStatusReply(next).Smart()), true
	case fa.ItemAdded != nil:
		e.mu.Lock()
		for _, ref := range fa.ItemAdded.Items {
			e.learnKeyLocked(ref)
		}
		e.added = true
		e.mu.Unlock()
		select {
		case e.addedSig <- struct{}{}:
		default:
		}
	}
	// ItemUpdated, ResourceUpdate, ItemListCancel, SyncButton: acknowledge, nothing to do.
	return gfdi.Kept(nil), true
}

func (e *Engine) learnKeyLocked(ref smart.ItemReference) string {
	dt := ref.DataType
	if dt == nil {
		return ""
	}
	switch {
	case dt.GDXMLString != nil:
		if dt.GDXMLStringKey != nil {
			e.keys[*dt.GDXMLStringKey] = *dt.GDXMLString
		}
		return *dt.GDXMLString
	case dt.GDXMLStringKey != nil:
		return e.keys[*dt.GDXMLStringKey]
	}
	return ""
}

func (e *Engine) item(ref smart.ItemReference) (Item, bool) {
	if ref.UID == nil {
		return Item{}, false
	}
	it := Item{UID: *ref.UID}
	e.mu.Lock()
	it.TypeName = e.learnKeyLocked(ref)
	e.mu.Unlock()
	if ref.DataType != nil && ref.DataType.Format != nil {
		it.Format = *ref.DataType.Format
	}
	if ref.DataSize != nil {
		it.Size = *ref.DataSize
	}
	if ref.TransactionID != nil {
		it.TransactionID = *ref.TransactionID
	}
	if ref.Urgency != nil {
		it.Urgency = *ref.Urgency
	}
	return it, true
}

func (e *Engine) call(ctx context.Context, req smart.FileAccess) (smart.FileAccess, error) {
	m, err := e.conn.Call(ctx, req.Smart())
	if err != nil {
		return smart.FileAccess{}, err
	}
	env, err := smart.ParseEnvelope(m.Data)
	if err != nil {
		return smart.FileAccess{}, err
	}
	return env.FileAccess()
}

// Sync lists, pulls and archives until nothing new is left. It returns what it did even when
// it fails part-way.
func (e *Engine) Sync(ctx context.Context) (Report, error) {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return Report{}, ErrBusy
	}
	e.running = true
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
	}()

	var rep Report
	if e.opt.CutWellness {
		rep.WellnessCut = e.cutWellness(ctx)
	}
	failures := 0
	settled := e.opt.Settle <= 0
	handled := map[smart.UUID]bool{} // each item once per sync, whatever the number of rounds
	for round := 0; round <= e.opt.MaxRelist; round++ {
		e.mu.Lock()
		e.added = false
		e.mu.Unlock()
		select { // a signal from before this round's listing is covered by it
		case <-e.addedSig:
		default:
		}
		if err := e.list(ctx, round == 0 && (e.opt.FullListing || e.opt.IncludeSynced), &rep); err != nil {
			return rep, err
		}
		pend, err := e.store.Pending(ctx)
		if err != nil {
			return rep, err
		}
		// Items to archive (the health data) first, oldest first.
		slices.SortStableFunc(pend, func(a, b PendingItem) int {
			if c := cmp.Compare(e.opt.Policy(b.Item), e.opt.Policy(a.Item)); c != 0 {
				return c
			}
			return cmp.Compare(a.TransactionID, b.TransactionID)
		})
		queued := 0
		for _, p := range pend {
			if e.opt.Policy(p.Item) != Ignore && !handled[p.UID] {
				queued++
			}
		}
		if e.opt.OnEvent != nil && queued > 0 {
			e.opt.OnEvent(Event{Kind: EventQueued, Queued: queued})
		}
		for i, p := range pend {
			act := e.opt.Policy(p.Item)
			if act == Ignore || handled[p.UID] {
				continue
			}
			handled[p.UID] = true
			e.mu.Lock()
			e.more = i < len(pend)-1
			e.mu.Unlock()
			err := e.process(ctx, p, act, &rep)
			if err == nil {
				failures = 0
				continue
			}
			if ctx.Err() != nil || isFatal(err) {
				return rep, err
			}
			rep.Failed++
			failures++
			e.event(EventFailed, p.Item, 0, err)
			e.log.Warn("fileaccess: item failed", "uid", p.UID.String(), "type", p.TypeName, "err", err)
			if failures >= e.opt.MaxFailures {
				return rep, fmt.Errorf("fileaccess: %d items failed in a row, giving up: %w", failures, err)
			}
		}
		e.mu.Lock()
		again := e.added
		e.mu.Unlock()
		if !again && !settled {
			settled = true
			timer := e.opt.Clock.NewTimer(e.opt.Settle)
			select {
			case <-e.addedSig:
				again = true
			case <-timer.C():
			case <-ctx.Done():
				timer.Stop()
				return rep, ctx.Err()
			}
			timer.Stop()
		}
		if !again {
			break
		}
	}
	return rep, nil
}

// cutWellness asks the watch to write out its wellness data; a failure only costs the newest
// data, so it is logged, not returned.
func (e *Engine) cutWellness(ctx context.Context) string {
	cctx, cancel := context.WithTimeout(ctx, e.opt.StatusTimeout)
	defer cancel()
	m, err := e.conn.Call(cctx, smart.CutWellnessFilesRequest(false))
	if err != nil {
		e.log.Warn("fileaccess: CutWellnessFilesRequest", "err", err)
		return ""
	}
	env, err := smart.ParseEnvelope(m.Data)
	if err != nil {
		e.log.Warn("fileaccess: CutWellnessFilesResponse", "err", err)
		return ""
	}
	st, ok := smart.ParseCutWellnessFilesResponse(env)
	if !ok {
		e.log.Warn("fileaccess: unexpected answer to CutWellnessFilesRequest", "service", smart.ServiceName(env.Service))
		return ""
	}
	return st.String()
}

// isFatal reports errors after which the connection is unusable.
func isFatal(err error) bool {
	return errors.Is(err, gfdi.ErrSessionClosed) || errors.Is(err, multilink.ErrClosed)
}

// list runs one listing (all pages) and records it with the new cursor.
func (e *Engine) list(ctx context.Context, full bool, rep *Report) error {
	cur, haveCursor, err := e.store.Cursor(ctx)
	if err != nil {
		return err
	}
	var from *uint32
	if haveCursor && !full {
		from = &cur
	}
	req := ListRequest(from)
	if e.opt.IncludeSynced {
		req = ListAllRequest(from)
	}
	var items []Item
	var next *uint32
	for page := 0; ; page++ {
		if page >= 10000 {
			return errors.New("fileaccess: listing does not end")
		}
		resp, err := e.call(ctx, req)
		if err != nil {
			return fmt.Errorf("fileaccess: listing page %d: %w", page, err)
		}
		lr := resp.ItemListResponse
		if lr == nil {
			return fmt.Errorf("fileaccess: listing page %d: answer is %s, not ItemListResponse", page, resp.Kind())
		}
		if lr.Status != nil && *lr.Status != smart.ItemListSuccess {
			return fmt.Errorf("fileaccess: listing page %d: %s", page, *lr.Status)
		}
		for _, ref := range lr.Items {
			it, ok := e.item(ref)
			if !ok {
				continue
			}
			rep.Listed++
			e.event(EventListed, it, 0, nil)
			if e.opt.Policy(it) == Ignore {
				rep.Ignored++
				continue
			}
			items = append(items, it)
		}
		if lr.NextTransactionID != nil {
			next = smart.U32(*lr.NextTransactionID)
		}
		if lr.SessionID == nil {
			break
		}
		req = ContinueListRequest(*lr.SessionID)
	}
	added, err := e.store.SaveListing(ctx, items, next)
	if err != nil {
		return fmt.Errorf("fileaccess: recording the listing: %w", err)
	}
	rep.Added += added
	return nil
}

// process pulls, stores and archives one pending item.
func (e *Engine) process(ctx context.Context, p PendingItem, act Action, rep *Report) error {
	it := p.Item
	if !p.Pulled {
		data, err := e.pull(ctx, it)
		if errors.Is(err, errGone) {
			rep.Gone++
			e.event(EventGone, it, 0, nil)
			return e.store.Finish(ctx, it.UID, Gone)
		}
		if err != nil {
			return err
		}
		if e.opt.Verify != nil {
			if err := e.opt.Verify(it, data); err != nil {
				return fmt.Errorf("fileaccess: %s %s: verification: %w", it.TypeName, it.UID, err)
			}
		}
		if err := e.store.Persist(ctx, it, data); err != nil {
			return fmt.Errorf("fileaccess: storing %s: %w", it.UID, err)
		}
		rep.Pulled++
		rep.Bytes += int64(len(data))
		e.event(EventPulled, it, len(data), nil)
	}
	switch {
	case act == PullArchive && e.opt.NoArchive:
		rep.Deferred++
		e.event(EventDeferred, it, 0, nil)
		return nil
	case act == PullArchive:
		err := e.archive(ctx, it)
		if errors.Is(err, errGone) {
			rep.Gone++
			e.event(EventGone, it, 0, nil)
			return e.store.Finish(ctx, it.UID, Gone)
		}
		if err != nil {
			return err
		}
		rep.Archived++
		e.event(EventArchived, it, 0, nil)
		if e.opt.Delete == nil || !e.opt.Delete(it) {
			return e.store.Finish(ctx, it.UID, Archived)
		}
		err = e.deleteItem(ctx, it.UID)
		var de *DeleteError
		switch {
		case err == nil:
			rep.Deleted++
			e.event(EventDeleted, it, 0, nil)
			return e.store.Finish(ctx, it.UID, Deleted)
		case errors.Is(err, errGone):
			// The watch removed it on its own when it was archived, as it does with health files.
			return e.store.Finish(ctx, it.UID, Archived)
		case errors.As(err, &de):
			// The archive stands; the watch just keeps the file.
			rep.Refused++
			e.log.Warn("fileaccess: the watch refused to delete an item", "uid", it.UID.String(), "type", it.TypeName, "result", de.Result)
			return e.store.Finish(ctx, it.UID, Archived)
		default:
			return err
		}
	default:
		rep.Kept++
		e.event(EventKept, it, 0, nil)
		return e.store.Finish(ctx, it.UID, Kept)
	}
}

// pull fetches one item over a file pipe (docs/protocol/05 §8).
func (e *Engine) pull(ctx context.Context, it Item) ([]byte, error) {
	resp, err := e.call(ctx, PullRequest(it.UID, e.opt.Priority, e.opt.CompressionWindow))
	if err != nil {
		return nil, fmt.Errorf("fileaccess: pull %s: %w", it.UID, err)
	}
	pr := resp.PullItemResponse
	if pr == nil {
		return nil, fmt.Errorf("fileaccess: pull %s: answer is %s", it.UID, resp.Kind())
	}
	if pr.Result != nil && *pr.Result != smart.ItemAccessSuccess {
		if *pr.Result == smart.ItemAccessUnknownItem {
			return nil, errGone
		}
		return nil, fmt.Errorf("fileaccess: pull %s: %s", it.UID, *pr.Result)
	}
	if pr.Transport != nil && *pr.Transport != smart.TransportMultilinkPipe {
		return nil, fmt.Errorf("fileaccess: pull %s: unsupported transport %s", it.UID, *pr.Transport)
	}
	if pr.TransferHandle == nil {
		return nil, fmt.Errorf("fileaccess: pull %s: no transfer handle", it.UID)
	}
	handle := *pr.TransferHandle
	status := make(chan *smart.TransferStatusRequest, 1)
	e.mu.Lock()
	e.transfers[handle] = status
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.transfers, handle)
		e.mu.Unlock()
	}()

	pipe, err := e.conn.OpenPipe(ctx)
	if err != nil {
		e.cancel(handle)
		return nil, fmt.Errorf("fileaccess: pull %s: %w", it.UID, err)
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pipe.Close(cctx) // returns at once if the watch closed it already
	}()
	// Pipe configure: 00 00 <transfer handle u16 LE> 00 00, the 6-byte read form.
	cfg := []byte{0, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(cfg[2:], uint16(handle))
	if err := pipe.Write(cfg); err != nil {
		e.cancel(handle)
		return nil, fmt.Errorf("fileaccess: pull %s: configuring the pipe: %w", it.UID, err)
	}
	data, st, err := e.readPipe(ctx, pipe, it, status)
	if err != nil {
		e.cancel(handle)
		return nil, fmt.Errorf("fileaccess: pull %s (%s): %w", it.UID, it.TypeName, err)
	}
	if st == nil {
		timer := e.opt.Clock.NewTimer(e.opt.StatusTimeout)
		select {
		case st = <-status:
		case <-timer.C():
			e.log.Debug("fileaccess: no TransferStatusRequest after the data", "uid", it.UID.String())
		case <-ctx.Done():
		}
		timer.Stop()
	}
	if st != nil && st.FailureReason != nil {
		return nil, fmt.Errorf("fileaccess: pull %s: the watch reports %s", it.UID, *st.FailureReason)
	}
	return data, nil
}

var (
	errIdle    = errors.New("pipe idle")
	errAborted = errors.New("transfer aborted")
	errDone    = errors.New("inflated")
)

// readPipe reads the configure reply (00 00 00) and inflates the zlib stream that follows. It
// ends when the stream is complete, the watch reports a failed transfer, or the pipe falls
// silent.
func (e *Engine) readPipe(ctx context.Context, pipe *multilink.Channel, it Item, status <-chan *smart.TransferStatusRequest) ([]byte, *smart.TransferStatusRequest, error) {
	limit := it.Size
	if limit == 0 {
		limit = e.opt.MaxFileSize
	}
	pr, pw := io.Pipe()
	type result struct {
		data []byte
		err  error
	}
	resc := make(chan result, 1)
	go func() {
		zr, err := zlib.NewReader(pr)
		if err != nil {
			pr.CloseWithError(err)
			resc <- result{err: fmt.Errorf("zlib header: %w", err)}
			return
		}
		data, err := io.ReadAll(io.LimitReader(zr, int64(limit)+1))
		pr.CloseWithError(errDone)
		resc <- result{data: data, err: err}
	}()
	abort := func(err error) error {
		pw.CloseWithError(err)
		<-resc
		return err
	}

	var reply []byte
	var st *smart.TransferStatusRequest
	idle := e.opt.Clock.NewTimer(e.opt.IdleTimeout)
	defer idle.Stop()
	recv := pipe.Recv()
	for {
		select {
		case p, ok := <-recv:
			if !ok {
				// The watch closed the pipe: whatever arrived is the whole stream.
				recv = nil
				pw.Close()
				continue
			}
			idle.Reset(e.opt.IdleTimeout)
			if len(reply) < 3 {
				n := min(3-len(reply), len(p))
				reply, p = append(reply, p[:n]...), p[n:]
				if len(reply) == 3 && reply[0] != 0 {
					return nil, st, abort(fmt.Errorf("pipe refused: % X", reply))
				}
			}
			if len(p) > 0 {
				if _, err := pw.Write(p); err != nil && !errors.Is(err, errDone) {
					recv = nil // the inflater failed; its result follows
				}
			}
		case r := <-resc:
			pw.Close()
			switch {
			case r.err != nil:
				return nil, st, fmt.Errorf("inflate: %w", r.err)
			case uint64(len(r.data)) > limit:
				return nil, st, fmt.Errorf("file exceeds %d bytes", limit)
			case it.Size > 0 && uint64(len(r.data)) != it.Size:
				return nil, st, fmt.Errorf("got %d bytes, the listing said %d", len(r.data), it.Size)
			}
			return r.data, st, nil
		case s := <-status:
			st, status = s, nil
			if s.FailureReason != nil {
				return nil, st, abort(fmt.Errorf("%w by the watch: %s", errAborted, *s.FailureReason))
			}
		case <-idle.C():
			return nil, st, abort(fmt.Errorf("%w for %s", errIdle, e.opt.IdleTimeout))
		case <-ctx.Done():
			return nil, st, abort(ctx.Err())
		}
	}
}

// cancel asks the watch to stop a transfer; failures are only logged.
func (e *Engine) cancel(handle uint32) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := e.call(ctx, CancelRequest(handle)); err != nil {
		e.log.Debug("fileaccess: cancel transfer", "handle", handle, "err", err)
	}
}

// archive flags an item as synced.
// DeleteError is the watch's refusal of a DeleteItemRequest.
type DeleteError struct {
	UID    smart.UUID
	Result smart.DeleteItemResult
}

func (e *DeleteError) Error() string {
	return fmt.Sprintf("fileaccess: the watch did not delete %s: %s", e.UID, e.Result)
}

// Unsupported reports a watch that does not delete this kind of item at all.
func (e *DeleteError) Unsupported() bool {
	return e.Result == smart.DeleteItemOperationNotSupported || e.Result == smart.DeleteItemCannotBeDeleted
}

// ErrNotStored is returned by Delete for an item the store does not hold a file of.
var ErrNotStored = errors.New("fileaccess: the item is not stored, refusing to delete it from the watch")

// Delete deletes one stored item from the watch, outside a sync (for example on a user's
// request) and records it as Deleted. It refuses items the store does not hold a file of;
// isStored answers that (DirStore.Stored, for one). An item the watch no longer has counts as
// deleted. A refusal by the watch is a *DeleteError.
func (e *Engine) Delete(ctx context.Context, uid smart.UUID, isStored func(smart.UUID) bool) error {
	if isStored == nil || !isStored(uid) {
		return ErrNotStored
	}
	err := e.deleteItem(ctx, uid)
	if err != nil && !errors.Is(err, errGone) {
		return err
	}
	return e.store.Finish(ctx, uid, Deleted)
}

func (e *Engine) deleteItem(ctx context.Context, uid smart.UUID) error {
	resp, err := e.call(ctx, DeleteRequest(uid))
	if err != nil {
		return fmt.Errorf("fileaccess: delete %s: %w", uid, err)
	}
	dr := resp.DeleteItemResponse
	if dr == nil {
		return fmt.Errorf("fileaccess: delete %s: answer is %s", uid, resp.Kind())
	}
	switch {
	case dr.Result == nil || *dr.Result == smart.DeleteItemSuccess: // proto2 default: SUCCESS
		return nil
	case *dr.Result == smart.DeleteItemDoesNotExist:
		return errGone
	}
	return &DeleteError{UID: uid, Result: *dr.Result}
}

func (e *Engine) archive(ctx context.Context, it Item) error {
	resp, err := e.call(ctx, ArchiveRequest(it.UID))
	if err != nil {
		return fmt.Errorf("fileaccess: archive %s: %w", it.UID, err)
	}
	mr := resp.ModifyFlagsResponse
	if mr == nil {
		return fmt.Errorf("fileaccess: archive %s: answer is %s", it.UID, resp.Kind())
	}
	if mr.Status != nil && *mr.Status != smart.ModifyFlagsSuccess {
		if *mr.Status == smart.ModifyFlagsFailItemNotFound {
			return errGone
		}
		return fmt.Errorf("fileaccess: archive %s: %s", it.UID, *mr.Status)
	}
	return nil
}
