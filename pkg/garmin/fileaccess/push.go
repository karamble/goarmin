// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fileaccess

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/adler32"
	"sync/atomic"
	"time"

	"github.com/karamble/goarmin/pkg/garmin/smart"
)

// Pushing a file to the watch, as Garmin Connect sends workouts and schedules
// (docs/protocol/08):
//
//  1. PushItemDataRequest with a host id (msb all ones), the type name and the clear size;
//  2. the watch answers with a transfer handle and the compression window it takes;
//  3. a reliable file pipe, configured for writing (00 01 <handle u16> 00 00); the watch
//     answers 00 00 00;
//  4. the file as a zlib stream within that window, then the host closes the pipe;
//  5. the watch asks for the transfer's status; the engine answers it;
//  6. the watch announces the file under its own id (ItemAddedNotification). A schedule is
//     taken into the watch's calendar and not announced.

// ErrPushRefused wraps the watch's refusal of a push.
var ErrPushRefused = errors.New("fileaccess: the watch refused the push")

// PushError is the watch's answer to a refused push.
type PushError struct {
	TypeName string
	Result   smart.ItemAccessResult
}

func (e *PushError) Error() string {
	return fmt.Sprintf("fileaccess: the watch refused the push of a %s: %s", e.TypeName, e.Result)
}

func (e *PushError) Unwrap() error { return ErrPushRefused }

// Pushed is what became of a pushed file.
type Pushed struct {
	// HostUID is the id the host offered the file under.
	HostUID smart.UUID
	// UID is the watch's own id for the file, from its ItemAddedNotification; the zero UUID if
	// the watch announced none (a schedule, or a watch that does not announce).
	UID smart.UUID
	// Announced reports whether the watch announced the file.
	Announced bool
}

var pushSeq atomic.Uint64

// newPushUID is a host id as Garmin Connect makes them: most significant half all ones, the
// least a number that differs from push to push.
func (e *Engine) newPushUID() smart.UUID {
	return smart.UUID{MSB: ^uint64(0), LSB: uint64(e.opt.Clock.Now().UnixMilli())<<8 | pushSeq.Add(1)&0xff}
}

// PushOptions tunes one push.
type PushOptions struct {
	// Announce waits up to AnnounceTimeout (5 s) for the watch to announce the file, and
	// returns its id. Leave it off for schedules, which the watch does not announce.
	Announce        bool
	AnnounceTimeout time.Duration
}

// Push sends data to the watch as a new item of type typeName ("FIT_TYPE_5" for a workout,
// "FIT_TYPE_7" for a schedule). It must not run while a Sync runs on the engine.
func (e *Engine) Push(ctx context.Context, typeName string, data []byte, opt PushOptions) (Pushed, error) {
	if len(data) == 0 {
		return Pushed{}, errors.New("fileaccess: push: no data")
	}
	if opt.AnnounceTimeout <= 0 {
		opt.AnnounceTimeout = 5 * time.Second
	}
	uid := e.newPushUID()
	out := Pushed{HostUID: uid}

	// Catch the watch's announcement from the moment the push is offered.
	var announced chan smart.ItemReference
	if opt.Announce {
		announced = make(chan smart.ItemReference, 8)
		e.mu.Lock()
		e.addedSubs = append(e.addedSubs, announced)
		e.mu.Unlock()
		defer func() {
			e.mu.Lock()
			for i, c := range e.addedSubs {
				if c == announced {
					e.addedSubs = append(e.addedSubs[:i], e.addedSubs[i+1:]...)
					break
				}
			}
			e.mu.Unlock()
		}()
	}

	resp, err := e.call(ctx, PushRequest(uid, typeName, uint64(len(data)), e.opt.CompressionWindow))
	if err != nil {
		return out, fmt.Errorf("fileaccess: push %s: %w", typeName, err)
	}
	pr := resp.PushItemResponse
	if pr == nil {
		return out, fmt.Errorf("fileaccess: push %s: answer is %s", typeName, resp.Kind())
	}
	if pr.Result != nil && *pr.Result != smart.ItemAccessSuccess {
		return out, &PushError{TypeName: typeName, Result: *pr.Result}
	}
	if pr.Transport != nil && *pr.Transport != smart.TransportMultilinkPipe {
		return out, fmt.Errorf("fileaccess: push %s: unsupported transport %s", typeName, *pr.Transport)
	}
	if pr.TransferHandle == nil {
		return out, fmt.Errorf("fileaccess: push %s: no transfer handle", typeName)
	}
	handle := *pr.TransferHandle
	window := uint32(15)
	if pr.CompressionWindow != nil {
		window = *pr.CompressionWindow
	}
	if window < 9 || window > 15 {
		return out, fmt.Errorf("fileaccess: push %s: the watch asks for compression window %d", typeName, window)
	}
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
		return out, fmt.Errorf("fileaccess: push %s: %w", typeName, err)
	}
	closed := false
	defer func() {
		if !closed {
			cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = pipe.Close(cctx)
		}
	}()
	// Pipe configure: 00 01 <transfer handle u16 LE> 00 00, the 6-byte write form.
	cfg := []byte{0, 1, 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(cfg[2:], uint16(handle))
	if err := pipe.Write(cfg); err != nil {
		e.cancel(handle)
		return out, fmt.Errorf("fileaccess: push %s: configuring the pipe: %w", typeName, err)
	}
	// The watch's configure reply: 00 00 00.
	var reply []byte
	idle := e.opt.Clock.NewTimer(e.opt.IdleTimeout)
	for len(reply) < 3 {
		select {
		case p, ok := <-pipe.Recv():
			if !ok {
				idle.Stop()
				e.cancel(handle)
				return out, fmt.Errorf("fileaccess: push %s: the watch closed the pipe before accepting it", typeName)
			}
			reply = append(reply, p...)
		case <-idle.C():
			e.cancel(handle)
			return out, fmt.Errorf("fileaccess: push %s: no answer to the pipe configure in %s", typeName, e.opt.IdleTimeout)
		case <-ctx.Done():
			idle.Stop()
			e.cancel(handle)
			return out, ctx.Err()
		}
	}
	idle.Stop()
	if reply[0] != 0 || reply[1] != 0 || reply[2] != 0 {
		e.cancel(handle)
		return out, fmt.Errorf("fileaccess: push %s: pipe refused: % X", typeName, reply[:3])
	}
	if err := pipe.Write(StoredZlib(data, window)); err != nil {
		e.cancel(handle)
		return out, fmt.Errorf("fileaccess: push %s: %w", typeName, err)
	}
	if err := pipe.Flush(ctx); err != nil {
		e.cancel(handle)
		return out, fmt.Errorf("fileaccess: push %s: sending: %w", typeName, err)
	}
	closed = true
	if err := pipe.Close(ctx); err != nil {
		e.log.Debug("fileaccess: closing the push pipe", "err", err)
	}
	timer := e.opt.Clock.NewTimer(e.opt.StatusTimeout)
	select {
	case st := <-status:
		timer.Stop()
		if st != nil && st.FailureReason != nil {
			return out, fmt.Errorf("fileaccess: push %s: the watch reports %s", typeName, *st.FailureReason)
		}
	case <-timer.C():
		e.log.Debug("fileaccess: no TransferStatusRequest after the push", "type", typeName)
	case <-ctx.Done():
		timer.Stop()
		return out, ctx.Err()
	}
	if !opt.Announce {
		return out, nil
	}
	wait := e.opt.Clock.NewTimer(opt.AnnounceTimeout)
	defer wait.Stop()
	for {
		select {
		case ref := <-announced:
			if ref.UID == nil || ref.DataSize == nil || *ref.DataSize != uint64(len(data)) {
				continue
			}
			if name := e.typeNameOf(ref); name != "" && name != typeName {
				continue
			}
			out.UID, out.Announced = *ref.UID, true
			return out, nil
		case <-wait.C():
			return out, nil
		case <-ctx.Done():
			return out, ctx.Err()
		}
	}
}

func (e *Engine) typeNameOf(ref smart.ItemReference) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.learnKeyLocked(ref)
}

// StoredZlib wraps data in a zlib stream of stored (uncompressed) deflate blocks whose header
// declares a 2^window-byte window. Stored blocks never refer back, so the stream fits any
// window the watch asks for; Go's zlib writer always declares 32 KiB.
func StoredZlib(data []byte, window uint32) []byte {
	cmf := byte((window-8)<<4 | 8)
	flg := byte(31 - (uint16(cmf)<<8)%31)
	if flg == 31 {
		flg = 0
	}
	out := make([]byte, 0, len(data)+len(data)/65535*5+11)
	out = append(out, cmf, flg)
	for off := 0; ; {
		n := min(len(data)-off, 65535)
		final := byte(0)
		if off+n == len(data) {
			final = 1
		}
		out = append(out, final, byte(n), byte(n>>8), ^byte(n), ^byte(n>>8))
		out = append(out, data[off:off+n]...)
		off += n
		if final == 1 {
			break
		}
	}
	return binary.BigEndian.AppendUint32(out, adler32.Checksum(data))
}
