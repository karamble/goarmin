// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package multilink

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/karamble/goarmin/pkg/clock"
)

// Link is a packet link over one GATT characteristic pair. pkg/bluez's Link implements it, and
// so does pkg/garmin/multilink/memlink.
type Link interface {
	// Send writes one packet of at most MaxWrite bytes.
	Send(ctx context.Context, pkt []byte) error
	// Recv delivers received packets in order.
	Recv() <-chan []byte
	MaxWrite() int
	// Done is closed when the link fails or closes; Err tells why.
	Done() <-chan struct{}
	Err() error
}

// ErrClosed is returned for operations on a closed mux or channel.
var ErrClosed = errors.New("multilink: closed")

// RegisterError reports a REGISTER_ML_RESP with a non-zero status.
type RegisterError struct {
	Service Service
	Status  byte
}

func (e *RegisterError) Error() string {
	return fmt.Sprintf("multilink: registering %s refused with status %d", e.Service, e.Status)
}

// Registration statuses a device sends (garmin-proto-lab's names).
const (
	StatusInvalidService byte = 1
	StatusPendingAuth    byte = 2
	StatusInUse          byte = 3
	StatusRejected       byte = 4
)

// Options configures a Mux.
type Options struct {
	// Client is the client ID a host puts in its control messages (ClientGadgetbridge by
	// default). A device answers with whatever ID the host used.
	Client uint64
	// Clock drives MLR timers and control timeouts; clock.Real() by default.
	Clock clock.Clock
	// MLR tunes the reliable layer; the zero value means Gadgetbridge's parameters.
	MLR MLRConfig
	// Logger receives debug messages; discarded by default.
	Logger *slog.Logger
	// ControlTimeout bounds the wait for a control response (10 s by default; the Fenix 8
	// took about 1.1 s to answer a registration).
	ControlTimeout time.Duration
	// Trace, if set, is called for every packet sent ("tx") and received ("rx").
	Trace func(dir string, pkt []byte)
}

// Acceptor decides a device's answer to REGISTER_ML_REQ: status 0 accepts, with an MLR handle if
// reliable is set.
type Acceptor func(client uint64, svc Service, wantReliable bool) (reliable bool, status byte)

// Mux owns a Link. One reader goroutine routes incoming packets to control waiters and
// channels; one writer goroutine sends queued packets, control messages first, then MLR
// acknowledgements, then data.
//
// A host Mux (NewMux) registers services. A device Mux (NewDeviceMux) answers registrations,
// as a watch does; it exists for fake watches and tests.
type Mux struct {
	link   Link
	opt    Options
	log    *slog.Logger
	device bool
	accept Acceptor

	mu       sync.Mutex
	chans    map[byte]*Channel
	regs     map[Service][]chan regResult
	ctlWait  []*ctlWaiter
	accepted []*Channel
	acceptCh chan struct{}
	closed   bool
	closeErr error

	wmu          sync.Mutex
	wcond        *sync.Cond
	ctlQ, ackQ   []func() []byte // packet builders, called when the packet goes out
	dataQ        []func() []byte
	writerClosed bool

	done chan struct{}
	once sync.Once
	wg   sync.WaitGroup
}

type regResult struct {
	ch  *Channel
	err error
}

type ctlWaiter struct {
	match func(Control) bool
	ch    chan Control
}

// NewMux starts a host mux on link.
func NewMux(link Link, opt Options) *Mux { return newMux(link, opt, false, nil) }

// NewDeviceMux starts a device (watch-side) mux on link. A nil accept accepts every
// registration as requested.
func NewDeviceMux(link Link, opt Options, accept Acceptor) *Mux {
	if accept == nil {
		accept = func(_ uint64, _ Service, want bool) (bool, byte) { return want, 0 }
	}
	return newMux(link, opt, true, accept)
}

func newMux(link Link, opt Options, device bool, accept Acceptor) *Mux {
	if opt.Client == 0 {
		opt.Client = ClientGadgetbridge
	}
	if opt.Clock == nil {
		opt.Clock = clock.Real()
	}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opt.ControlTimeout <= 0 {
		opt.ControlTimeout = 10 * time.Second
	}
	m := &Mux{
		link: link, opt: opt, log: opt.Logger, device: device, accept: accept,
		chans: map[byte]*Channel{}, regs: map[Service][]chan regResult{},
		acceptCh: make(chan struct{}, 1), done: make(chan struct{}),
	}
	m.wcond = sync.NewCond(&m.wmu)
	m.wg.Add(2)
	go m.readLoop()
	go m.writeLoop()
	return m
}

// Client returns the host client ID in use.
func (m *Mux) Client() uint64 { return m.opt.Client }

// MaxWrite is the link's packet size limit.
func (m *Mux) MaxWrite() int { return m.link.MaxWrite() }

// Done is closed when the mux stops; Err tells why.
func (m *Mux) Done() <-chan struct{} { return m.done }

// Err returns the reason the mux stopped: nil while it runs or after Close.
func (m *Mux) Err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closeErr
}

// Close stops the mux and closes every channel. It does not send CLOSE_ALL; call CloseAll
// first for a clean shutdown. The link itself is left to its owner.
func (m *Mux) Close() error {
	m.shutdown(nil)
	m.wg.Wait()
	return nil
}

func (m *Mux) shutdown(err error) {
	m.once.Do(func() {
		m.mu.Lock()
		m.closed, m.closeErr = true, err
		chans := m.chans
		m.chans = map[byte]*Channel{}
		regs := m.regs
		m.regs = map[Service][]chan regResult{}
		m.mu.Unlock()
		for _, c := range chans {
			c.terminate()
		}
		for _, waiters := range regs {
			for _, w := range waiters {
				w <- regResult{err: ErrClosed}
			}
		}
		m.wmu.Lock()
		m.writerClosed = true
		m.wcond.Broadcast()
		m.wmu.Unlock()
		close(m.done)
	})
}

// enqueue adds a ready packet to the writer queues.
func (m *Mux) enqueue(pkt []byte, control, ackOnly bool) {
	m.enqueueBuild(func() []byte { return pkt }, control, ackOnly)
}

// enqueueBuild adds a packet builder to the writer queues. The writer calls it, without any
// mux lock held, when the packet is about to be sent; a nil result is skipped.
func (m *Mux) enqueueBuild(build func() []byte, control, ackOnly bool) {
	m.wmu.Lock()
	defer m.wmu.Unlock()
	if m.writerClosed {
		return
	}
	switch {
	case control:
		m.ctlQ = append(m.ctlQ, build)
	case ackOnly:
		m.ackQ = append(m.ackQ, build)
	default:
		m.dataQ = append(m.dataQ, build)
	}
	m.wcond.Signal()
}

func (m *Mux) writeLoop() {
	defer m.wg.Done()
	for {
		m.wmu.Lock()
		for !m.writerClosed && len(m.ctlQ)+len(m.ackQ)+len(m.dataQ) == 0 {
			m.wcond.Wait()
		}
		if m.writerClosed {
			m.wmu.Unlock()
			return
		}
		var build func() []byte
		switch {
		case len(m.ctlQ) > 0:
			build, m.ctlQ = m.ctlQ[0], m.ctlQ[1:]
		case len(m.ackQ) > 0:
			build, m.ackQ = m.ackQ[0], m.ackQ[1:]
		default:
			build, m.dataQ = m.dataQ[0], m.dataQ[1:]
		}
		m.wmu.Unlock()
		pkt := build()
		if pkt == nil {
			continue
		}
		if m.opt.Trace != nil {
			m.opt.Trace("tx", pkt)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := m.link.Send(ctx, pkt)
		cancel()
		if err != nil {
			m.shutdown(fmt.Errorf("multilink: send: %w", err))
			return
		}
	}
}

func (m *Mux) readLoop() {
	defer m.wg.Done()
	for {
		select {
		case <-m.done:
			return
		case <-m.link.Done():
			err := m.link.Err()
			if err == nil {
				err = errors.New("multilink: link closed")
			}
			m.shutdown(err)
			return
		case pkt := <-m.link.Recv():
			if m.opt.Trace != nil {
				m.opt.Trace("rx", pkt)
			}
			m.route(pkt)
		}
	}
}

func (m *Mux) route(pkt []byte) {
	if len(pkt) == 0 {
		return
	}
	if IsControl(pkt) {
		if ctl, err := ParseControl(pkt); err == nil {
			if m.device {
				m.onDeviceControl(ctl)
			} else {
				m.onHostControl(ctl)
			}
		}
		return
	}
	m.mu.Lock()
	if IsMLR(pkt[0]) && len(pkt) >= 2 {
		h := ParseHeader(pkt[0], pkt[1])
		if c := m.chans[h.Handle]; c != nil && c.rel != nil {
			m.mu.Unlock()
			c.rel.onPacket(pkt)
			return
		}
	}
	c := m.chans[pkt[0]]
	m.mu.Unlock()
	if c != nil && c.rel == nil {
		c.push(pkt[1:])
		return
	}
	m.log.Debug("multilink: packet for an unknown handle", "handle", fmt.Sprintf("0x%02X", pkt[0]), "len", len(pkt))
}

func (m *Mux) onHostControl(ctl Control) {
	if ctl.Client != m.opt.Client {
		m.log.Debug("multilink: control message for another client", "control", ctl.String())
		return
	}
	switch ctl.Type {
	case RegisterResponse:
		m.mu.Lock()
		waiters := m.regs[ctl.Service]
		if len(waiters) == 0 {
			m.mu.Unlock()
			m.log.Debug("multilink: unsolicited REGISTER_ML_RESP", "control", ctl.String())
			return
		}
		w := waiters[0]
		m.regs[ctl.Service] = waiters[1:]
		if ctl.Status != 0 {
			m.mu.Unlock()
			w <- regResult{err: &RegisterError{Service: ctl.Service, Status: ctl.Status}}
			return
		}
		old := m.chans[ctl.Handle]
		c := m.newChannel(ctl.Client, ctl.Service, ctl.Handle, ctl.Reliable)
		m.chans[ctl.Handle] = c
		m.mu.Unlock()
		if old != nil {
			old.terminate() // the watch reused a handle; its MLR state starts afresh
		}
		w <- regResult{ch: c}
	case CloseHandleResponse:
		m.mu.Lock()
		c := m.chans[ctl.Handle]
		if c != nil && c.svc == ctl.Service {
			delete(m.chans, ctl.Handle)
		} else {
			c = nil
		}
		m.mu.Unlock()
		if c != nil {
			c.terminate()
		}
		m.notifyControl(ctl)
	case CloseAllResponse:
		m.terminateAll()
		m.notifyControl(ctl)
	default:
		m.notifyControl(ctl)
	}
}

func (m *Mux) terminateAll() {
	m.mu.Lock()
	chans := m.chans
	m.chans = map[byte]*Channel{}
	m.mu.Unlock()
	for _, c := range chans {
		c.terminate()
	}
}

func (m *Mux) onDeviceControl(ctl Control) {
	switch ctl.Type {
	case RegisterRequest:
		reliable, status := m.accept(ctl.Client, ctl.Service, ctl.Reliable)
		m.mu.Lock()
		handle, ok := byte(0), false
		if status == 0 {
			if handle, ok = m.freeHandleLocked(reliable); !ok {
				status = StatusRejected
			}
		}
		if status != 0 {
			m.mu.Unlock()
			m.enqueue(EncodeRegisterResponse(ctl.Client, ctl.Service, status, 0, false), true, false)
			return
		}
		c := m.newChannel(ctl.Client, ctl.Service, handle, reliable)
		m.chans[handle] = c
		m.accepted = append(m.accepted, c)
		m.mu.Unlock()
		// The response is queued before the channel is handed out, so it reaches the host
		// before any data written on the channel.
		m.enqueue(EncodeRegisterResponse(ctl.Client, ctl.Service, 0, handle, reliable), true, false)
		select {
		case m.acceptCh <- struct{}{}:
		default:
		}
	case CloseHandleRequest:
		m.mu.Lock()
		c := m.chans[ctl.Handle]
		status := byte(0)
		if c != nil && c.svc == ctl.Service {
			delete(m.chans, ctl.Handle)
		} else {
			c, status = nil, 1
		}
		m.mu.Unlock()
		if c != nil {
			c.terminate()
		}
		m.enqueue(EncodeCloseHandleResponse(ctl.Client, ctl.Service, ctl.Handle, status), true, false)
	case CloseAllRequest:
		m.terminateAll()
		m.enqueue(EncodeCloseAllResponse(ctl.Client), true, false)
	}
}

// freeHandleLocked picks the lowest free handle: 0x80..0x87 for MLR, 0x01..0x7F otherwise.
func (m *Mux) freeHandleLocked(reliable bool) (byte, bool) {
	lo, hi := byte(0x01), byte(0x7F)
	if reliable {
		lo, hi = 0x80, 0x87
	}
	for h := lo; h <= hi; h++ {
		if m.chans[h] == nil {
			return h, true
		}
	}
	return 0, false
}

// Accept returns the next channel a host registered on a device mux.
func (m *Mux) Accept(ctx context.Context) (*Channel, error) {
	for {
		m.mu.Lock()
		if len(m.accepted) > 0 {
			c := m.accepted[0]
			m.accepted = m.accepted[1:]
			m.mu.Unlock()
			return c, nil
		}
		m.mu.Unlock()
		select {
		case <-m.acceptCh:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-m.done:
			return nil, ErrClosed
		}
	}
}

func (m *Mux) notifyControl(ctl Control) {
	m.mu.Lock()
	var hit *ctlWaiter
	for i, w := range m.ctlWait {
		if w.match(ctl) {
			hit = w
			m.ctlWait = append(m.ctlWait[:i], m.ctlWait[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	if hit != nil {
		hit.ch <- ctl
	}
}

func (m *Mux) waitControl(ctx context.Context, req []byte, match func(Control) bool) (Control, error) {
	w := &ctlWaiter{match: match, ch: make(chan Control, 1)}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Control{}, ErrClosed
	}
	m.ctlWait = append(m.ctlWait, w)
	m.mu.Unlock()
	m.enqueue(req, true, false)
	timer := m.opt.Clock.NewTimer(m.opt.ControlTimeout)
	defer timer.Stop()
	select {
	case ctl := <-w.ch:
		return ctl, nil
	case <-ctx.Done():
		m.dropWaiter(w)
		return Control{}, ctx.Err()
	case <-timer.C():
		m.dropWaiter(w)
		return Control{}, fmt.Errorf("multilink: no control response within %s", m.opt.ControlTimeout)
	case <-m.done:
		return Control{}, ErrClosed
	}
}

func (m *Mux) dropWaiter(w *ctlWaiter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, x := range m.ctlWait {
		if x == w {
			m.ctlWait = append(m.ctlWait[:i], m.ctlWait[i+1:]...)
			return
		}
	}
}

// CloseAll asks the watch to close every handle of this client and waits for the answer.
// Every channel is closed.
func (m *Mux) CloseAll(ctx context.Context) error {
	if m.device {
		return errors.New("multilink: CloseAll is a host operation")
	}
	_, err := m.waitControl(ctx, EncodeCloseAll(m.opt.Client), func(c Control) bool { return c.Type == CloseAllResponse })
	return err
}

// Register registers svc and returns its channel. reliable asks for an MLR handle; the watch
// decides, see Channel.Reliable.
func (m *Mux) Register(ctx context.Context, svc Service, reliable bool) (*Channel, error) {
	if m.device {
		return nil, errors.New("multilink: Register is a host operation")
	}
	w := make(chan regResult, 1)
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrClosed
	}
	m.regs[svc] = append(m.regs[svc], w)
	m.mu.Unlock()
	m.enqueue(EncodeRegister(m.opt.Client, svc, reliable), true, false)
	timer := m.opt.Clock.NewTimer(m.opt.ControlTimeout)
	defer timer.Stop()
	select {
	case r := <-w:
		return r.ch, r.err
	case <-ctx.Done():
		m.dropReg(svc, w)
		return nil, ctx.Err()
	case <-timer.C():
		m.dropReg(svc, w)
		return nil, fmt.Errorf("multilink: no answer to registering %s within %s", svc, m.opt.ControlTimeout)
	}
}

func (m *Mux) dropReg(svc Service, w chan regResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws := m.regs[svc]
	for i, x := range ws {
		if x == w {
			m.regs[svc] = append(ws[:i], ws[i+1:]...)
			return
		}
	}
}

// Channel is one registered Multi-Link service. It carries a byte stream: over MLR the
// stream is reliable and ordered; on a plain handle each packet's payload is delivered as is.
type Channel struct {
	mux      *Mux
	client   uint64
	svc      Service
	handle   byte
	rel      *reliable
	qmu      sync.Mutex
	qcond    *sync.Cond
	q        [][]byte
	qdone    bool
	recv     chan []byte
	closed   chan struct{}
	termOnce sync.Once
}

func (m *Mux) newChannel(client uint64, svc Service, handle byte, isReliable bool) *Channel {
	c := &Channel{mux: m, client: client, svc: svc, handle: handle, recv: make(chan []byte), closed: make(chan struct{})}
	c.qcond = sync.NewCond(&c.qmu)
	if isReliable {
		c.rel = newReliable(handle, m.link.MaxWrite(), m.opt.MLR, m.opt.Clock,
			func(build func() []byte, ackOnly bool) { m.enqueueBuild(build, false, ackOnly) },
			c.push,
			func(err error) { m.shutdown(fmt.Errorf("%s (handle 0x%02X): %w", svc, handle, err)) })
	}
	go c.pump()
	return c
}

// push queues a received payload; the queue is unbounded so the reader never blocks.
func (c *Channel) push(p []byte) {
	c.qmu.Lock()
	if !c.qdone {
		c.q = append(c.q, append([]byte(nil), p...))
		c.qcond.Signal()
	}
	c.qmu.Unlock()
}

func (c *Channel) pump() {
	defer close(c.recv)
	for {
		c.qmu.Lock()
		for len(c.q) == 0 && !c.qdone {
			c.qcond.Wait()
		}
		if len(c.q) == 0 {
			c.qmu.Unlock()
			return
		}
		p := c.q[0]
		c.q = c.q[1:]
		c.qmu.Unlock()
		select {
		case c.recv <- p:
		case <-c.mux.done:
			return
		}
	}
}

// terminate marks the channel closed; payloads already received stay readable until the mux
// stops.
func (c *Channel) terminate() {
	c.termOnce.Do(func() {
		if c.rel != nil {
			c.rel.close()
		}
		close(c.closed)
		c.qmu.Lock()
		c.qdone = true
		c.qcond.Broadcast()
		c.qmu.Unlock()
	})
}

// Service returns the registered service.
func (c *Channel) Service() Service { return c.svc }

// Handle returns the handle the watch assigned.
func (c *Channel) Handle() byte { return c.handle }

// Reliable reports whether the channel is an MLR stream.
func (c *Channel) Reliable() bool { return c.rel != nil }

// Stats returns MLR counters; zero for a plain channel.
func (c *Channel) Stats() StreamStats {
	if c.rel == nil {
		return StreamStats{}
	}
	return c.rel.snapshot()
}

// Recv delivers received payloads in order. It is closed after the channel closes and every
// received payload has been read, or when the mux stops.
func (c *Channel) Recv() <-chan []byte { return c.recv }

// Closed is closed when the watch or the host closes the channel, or the mux stops.
func (c *Channel) Closed() <-chan struct{} { return c.closed }

// Write sends data. On an MLR channel it joins the reliable byte stream; on a plain channel it
// is split into packets of MaxWrite-1 bytes behind the handle byte. Write does not wait for the
// data to go out; see Flush.
func (c *Channel) Write(data []byte) error {
	select {
	case <-c.closed:
		return ErrClosed
	default:
	}
	if c.rel != nil {
		return c.rel.write(data)
	}
	chunk := max(1, c.mux.link.MaxWrite()-1)
	for off := 0; off < len(data); off += chunk {
		end := min(off+chunk, len(data))
		pkt := make([]byte, 0, 1+end-off)
		pkt = append(append(pkt, c.handle), data[off:end]...)
		c.mux.enqueue(pkt, false, false)
	}
	return nil
}

// Flush waits until everything written on an MLR channel has been acknowledged. On a plain
// channel it returns at once.
func (c *Channel) Flush(ctx context.Context) error {
	if c.rel == nil {
		return nil
	}
	return c.rel.flush(ctx.Done())
}

// Close asks the watch to close the handle and waits for the answer. Data not yet
// acknowledged is abandoned; call Flush first to deliver it.
//
// On a device mux, Close sends an unsolicited CLOSE_HANDLE_RESP, as the Fenix 8 does when it
// has sent a file over a pipe (docs/protocol/05 §8). Control messages overtake queued data,
// so Flush first.
func (c *Channel) Close(ctx context.Context) error {
	select {
	case <-c.closed:
		return nil
	default:
	}
	m := c.mux
	if m.device {
		m.mu.Lock()
		if m.chans[c.handle] == c {
			delete(m.chans, c.handle)
		}
		m.mu.Unlock()
		c.terminate()
		m.enqueue(EncodeCloseHandleResponse(c.client, c.svc, c.handle, 0), true, false)
		return nil
	}
	_, err := m.waitControl(ctx, EncodeCloseHandle(m.opt.Client, c.svc, c.handle), func(ctl Control) bool {
		return ctl.Type == CloseHandleResponse && ctl.Service == c.svc && ctl.Handle == c.handle
	})
	m.mu.Lock()
	if m.chans[c.handle] == c {
		delete(m.chans, c.handle)
	}
	m.mu.Unlock()
	c.terminate()
	return err
}
