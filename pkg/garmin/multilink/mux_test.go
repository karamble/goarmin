// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package multilink_test

import (
	"bytes"
	"context"
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/karamble/goarmin/pkg/clock"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/multilink/memlink"
)

type pair struct {
	host, dev   *multilink.Mux
	hostL, devL *memlink.Link
}

func newPair(t *testing.T, lopt memlink.Options, hopt, dopt multilink.Options, accept multilink.Acceptor) *pair {
	t.Helper()
	a, b := memlink.Pipe(lopt)
	p := &pair{host: multilink.NewMux(a, hopt), dev: multilink.NewDeviceMux(b, dopt, accept), hostL: a, devL: b}
	t.Cleanup(func() {
		p.host.Close()
		p.dev.Close()
		a.Close()
	})
	return p
}

func ctxT(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

// readN reads from ch until n bytes arrived.
func readN(t *testing.T, ctx context.Context, ch <-chan []byte, n int) []byte {
	t.Helper()
	var got []byte
	for len(got) < n {
		select {
		case p, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed after %d of %d bytes", len(got), n)
			}
			got = append(got, p...)
		case <-ctx.Done():
			t.Fatalf("timed out after %d of %d bytes", len(got), n)
		}
	}
	return got
}

func randBytes(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, ^seed))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

func TestMuxHandshakeLikeTheFenix8(t *testing.T) {
	p := newPair(t, memlink.Options{}, multilink.Options{}, multilink.Options{}, nil)
	ctx := ctxT(t, 5*time.Second)
	if err := p.host.CloseAll(ctx); err != nil {
		t.Fatal(err)
	}
	gfdi, err := p.host.Register(ctx, multilink.ServiceGFDI, true)
	if err != nil {
		t.Fatal(err)
	}
	ka, err := p.host.Register(ctx, multilink.ServiceKeepAlive, false)
	if err != nil {
		t.Fatal(err)
	}
	if gfdi.Handle() != 0x80 || !gfdi.Reliable() || ka.Handle() != 0x01 || ka.Reliable() {
		t.Fatalf("handles GFDI 0x%02X reliable=%v, KEEP_ALIVE 0x%02X reliable=%v", gfdi.Handle(), gfdi.Reliable(), ka.Handle(), ka.Reliable())
	}
	dg, err := p.dev.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dk, _ := p.dev.Accept(ctx)
	if dg.Service() != multilink.ServiceGFDI || dk.Service() != multilink.ServiceKeepAlive {
		t.Fatalf("device accepted %s, %s", dg.Service(), dk.Service())
	}

	// GFDI is a reliable byte stream both ways, larger than one packet.
	up, down := randBytes(1, 5000), randBytes(2, 9000)
	if err := gfdi.Write(up); err != nil {
		t.Fatal(err)
	}
	if err := dg.Write(down); err != nil {
		t.Fatal(err)
	}
	if got := readN(t, ctx, dg.Recv(), len(up)); !bytes.Equal(got, up) {
		t.Fatal("host→device GFDI bytes differ")
	}
	if got := readN(t, ctx, gfdi.Recv(), len(down)); !bytes.Equal(got, down) {
		t.Fatal("device→host GFDI bytes differ")
	}
	if err := gfdi.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	// A plain channel splits writes at MaxWrite-1 and keeps packet payloads.
	if err := ka.Write(bytes.Repeat([]byte{7}, 300)); err != nil {
		t.Fatal(err)
	}
	first := readN(t, ctx, dk.Recv(), 1)
	if len(first) != 228 {
		t.Fatalf("first plain payload %d bytes, want 228", len(first))
	}
	readN(t, ctx, dk.Recv(), 300-228)

	// CLOSE_ALL closes every channel on both sides.
	if err := p.host.CloseAll(ctx); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*multilink.Channel{gfdi, ka, dg, dk} {
		select {
		case <-c.Closed():
		case <-ctx.Done():
			t.Fatalf("%s still open after CLOSE_ALL", c.Service())
		}
	}
	if err := gfdi.Write([]byte{1}); !errors.Is(err, multilink.ErrClosed) {
		t.Fatalf("write after CLOSE_ALL: %v", err)
	}
}

// File pipes rotate through services; a closed handle is reused with a fresh MLR stream.
func TestMuxPipeRotation(t *testing.T) {
	p := newPair(t, memlink.Options{}, multilink.Options{}, multilink.Options{}, nil)
	ctx := ctxT(t, 10*time.Second)
	gfdi, err := p.host.Register(ctx, multilink.ServiceGFDI, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.dev.Accept(ctx); err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		svc := multilink.FileTransferServices[i%len(multilink.FileTransferServices)]
		pipe, err := p.host.Register(ctx, svc, true)
		if err != nil {
			t.Fatalf("pipe %d: %v", i, err)
		}
		if pipe.Handle() != 0x81 {
			t.Fatalf("pipe %d got handle 0x%02X, want 0x81 again", i, pipe.Handle())
		}
		dp, err := p.dev.Accept(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := pipe.Write([]byte{0, 0, 0x81, 0, 0, 0}); err != nil {
			t.Fatal(err)
		}
		readN(t, ctx, dp.Recv(), 6)
		body := randBytes(uint64(i), 3000+i*100)
		_ = dp.Write(body)
		if i%2 == 0 {
			// The host closes the pipe.
			if got := readN(t, ctx, pipe.Recv(), len(body)); !bytes.Equal(got, body) {
				t.Fatalf("pipe %d bytes differ", i)
			}
			if err := pipe.Close(ctx); err != nil {
				t.Fatalf("closing pipe %d: %v", i, err)
			}
			select {
			case <-dp.Closed():
			case <-ctx.Done():
				t.Fatal("device pipe not closed")
			}
			continue
		}
		// The watch closes the pipe after its data was acknowledged, as the Fenix 8 does;
		// the host still reads every byte, then sees the channel end.
		if err := dp.Flush(ctx); err != nil {
			t.Fatal(err)
		}
		if err := dp.Close(ctx); err != nil {
			t.Fatal(err)
		}
		var got []byte
		for p := range pipe.Recv() {
			got = append(got, p...)
		}
		if !bytes.Equal(got, body) {
			t.Fatalf("pipe %d: %d of %d bytes before the watch's close", i, len(got), len(body))
		}
		if err := pipe.Close(ctx); err != nil {
			t.Fatalf("closing a pipe the watch closed: %v", err)
		}
	}
	select {
	case <-gfdi.Closed():
		t.Fatal("closing pipes closed GFDI")
	default:
	}
}

func TestMuxRegisterRefused(t *testing.T) {
	accept := func(_ uint64, svc multilink.Service, want bool) (bool, byte) {
		if svc == multilink.ServiceRealtimeHR {
			return false, multilink.StatusInvalidService
		}
		return want, 0
	}
	p := newPair(t, memlink.Options{}, multilink.Options{}, multilink.Options{}, accept)
	ctx := ctxT(t, 5*time.Second)
	_, err := p.host.Register(ctx, multilink.ServiceRealtimeHR, false)
	var re *multilink.RegisterError
	if !errors.As(err, &re) || re.Status != multilink.StatusInvalidService {
		t.Fatalf("got %v, want RegisterError status 1", err)
	}
	// The mux keeps working.
	if _, err := p.host.Register(ctx, multilink.ServiceGFDI, false); err != nil {
		t.Fatal(err)
	}
}

// Control responses for another client ID are ignored, so a registration times out.
func TestMuxControlTimeout(t *testing.T) {
	clk := clock.NewFake(time.Now())
	a, b := memlink.Pipe(memlink.Options{})
	defer a.Close()
	host := multilink.NewMux(a, multilink.Options{Clock: clk, ControlTimeout: 5 * time.Second})
	defer host.Close()
	ctx := ctxT(t, 5*time.Second)
	errc := make(chan error, 1)
	go func() {
		_, err := host.Register(context.Background(), multilink.ServiceGFDI, true)
		errc <- err
	}()
	// The silent watch reads the request and answers for client 1 instead of 2.
	select {
	case req := <-b.Recv():
		ctl, _ := multilink.ParseControl(req)
		_ = b.Send(ctx, multilink.EncodeRegisterResponse(multilink.ClientGarminConnect, ctl.Service, 0, 0x80, true))
	case <-ctx.Done():
		t.Fatal("no request")
	}
	if err := clk.WaitForTimers(ctx, 1); err != nil {
		t.Fatal(err)
	}
	clk.Advance(5 * time.Second)
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("registration succeeded with a response for another client")
		}
	case <-ctx.Done():
		t.Fatal("registration did not time out")
	}
}

func TestMuxLinkLoss(t *testing.T) {
	p := newPair(t, memlink.Options{}, multilink.Options{}, multilink.Options{}, nil)
	ctx := ctxT(t, 5*time.Second)
	gfdi, err := p.host.Register(ctx, multilink.ServiceGFDI, true)
	if err != nil {
		t.Fatal(err)
	}
	p.devL.Close()
	select {
	case <-p.host.Done():
	case <-ctx.Done():
		t.Fatal("mux did not notice the link loss")
	}
	if !errors.Is(p.host.Err(), memlink.ErrDisconnected) {
		t.Fatalf("mux error %v", p.host.Err())
	}
	select {
	case <-gfdi.Closed():
	default:
		t.Fatal("channel open after link loss")
	}
	if _, err := p.host.Register(ctx, multilink.ServiceGFDI, true); !errors.Is(err, multilink.ErrClosed) {
		t.Fatalf("register after link loss: %v", err)
	}
}

// 5 % of all MLR packets are lost in both directions; the streams still deliver every byte.
func TestMuxLossyMLR(t *testing.T) {
	fast := multilink.MLRConfig{InitialRTO: 20 * time.Millisecond, MaxRTO: 200 * time.Millisecond, AckDelay: 2 * time.Millisecond, MaxRetransmit: 50}
	p := newPair(t,
		memlink.Options{Filter: memlink.RandomLoss(7, 0.05, memlink.MLRAny)},
		multilink.Options{MLR: fast}, multilink.Options{MLR: fast}, nil)
	ctx := ctxT(t, 30*time.Second)
	gfdi, err := p.host.Register(ctx, multilink.ServiceGFDI, true)
	if err != nil {
		t.Fatal(err)
	}
	dg, _ := p.dev.Accept(ctx)
	pipe, err := p.host.Register(ctx, 0x2018, true)
	if err != nil {
		t.Fatal(err)
	}
	dp, _ := p.dev.Accept(ctx)

	t.Cleanup(func() {
		if t.Failed() {
			for name, c := range map[string]*multilink.Channel{"host gfdi": gfdi, "dev gfdi": dg, "host pipe": pipe, "dev pipe": dp} {
				t.Logf("%s: %s", name, c.DebugState())
			}
		}
	})
	up, down, file := randBytes(10, 40_000), randBytes(11, 60_000), randBytes(12, 200_000)
	_ = gfdi.Write(up)
	_ = dg.Write(down)
	_ = dp.Write(file)
	if got := readN(t, ctx, dg.Recv(), len(up)); !bytes.Equal(got, up) {
		t.Fatal("GFDI up corrupted")
	}
	if got := readN(t, ctx, gfdi.Recv(), len(down)); !bytes.Equal(got, down) {
		t.Fatal("GFDI down corrupted")
	}
	if got := readN(t, ctx, pipe.Recv(), len(file)); !bytes.Equal(got, file) {
		t.Fatal("pipe corrupted")
	}
	if err := dp.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if s := dp.Stats(); s.Retransmitted == 0 {
		t.Fatalf("no retransmissions at 5%% loss: %+v", s)
	}
}

func TestMuxTrace(t *testing.T) {
	var tx, rx int
	trace := func(dir string, _ []byte) {
		if dir == "tx" {
			tx++
		} else {
			rx++
		}
	}
	p := newPair(t, memlink.Options{}, multilink.Options{Trace: trace}, multilink.Options{}, nil)
	ctx := ctxT(t, 5*time.Second)
	if err := p.host.CloseAll(ctx); err != nil {
		t.Fatal(err)
	}
	p.host.Close()
	if tx != 1 || rx != 1 {
		t.Fatalf("traced tx=%d rx=%d, want 1 and 1", tx, rx)
	}
}
