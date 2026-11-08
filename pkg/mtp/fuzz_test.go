// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mtp

import (
	"context"
	"io"
	"testing"
	"time"
)

// FuzzDatasets: whatever a device sends as a dataset is parsed or refused, never a panic.
func FuzzDatasets(f *testing.F) {
	w := watch(time.Now())
	f.Add(encodeDeviceInfo(w.info))
	f.Add(encodeObjectInfo(w.objs[2]))
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF, 1, 2})
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = parseDeviceInfo(b)
		_, _ = parseObjectInfo(b, time.UTC)
		_, _ = parseStorageInfo(b)
		d := &dataset{b: b}
		_ = d.u32s()
		_ = d.str()
	})
}

// scripted replays fixed transfers whatever the command.
type scripted struct{ parts [][]byte }

func (s *scripted) Send([]byte) error { return nil }

func (s *scripted) Receive(p []byte) (int, error) {
	if len(s.parts) == 0 {
		return 0, ErrTimeout
	}
	n := copy(p, s.parts[0])
	s.parts = s.parts[1:]
	return n, nil
}

func (s *scripted) Close() error { return nil }

// FuzzSession: a device answering nonsense ends the session with an error, never a panic or
// a hang.
func FuzzSession(f *testing.F) {
	w := watch(time.Now())
	f.Add(container(typeData, opGetDeviceInfo, 0, encodeDeviceInfo(w.info)), container(typeResponse, RespOK, 0, nil))
	f.Add(container(typeData, opGetDeviceInfo, 0, nil)[:8], []byte{})
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF, 2, 0, 1, 0x10, 0, 0, 0, 0}, []byte{12, 0, 0, 0, 3, 0, 1, 0x20, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, a, b []byte) {
		s, err := NewSession(context.Background(), &scripted{parts: [][]byte{a, b, a, b}})
		if err != nil {
			return
		}
		_, _ = s.StorageIDs(context.Background())
		_, _ = s.Get(context.Background(), 1, io.Discard)
		_ = s.Close()
	})
}
