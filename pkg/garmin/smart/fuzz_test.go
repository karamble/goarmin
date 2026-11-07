// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package smart

import (
	"bytes"
	"reflect"
	"testing"
)

// fuzzSeeds returns FileAccess payloads: the golden messages, one fully populated instance of
// each service field, all of them at once, and some unknown fields.
func fuzzSeeds() [][]byte {
	seeds := [][]byte{
		FileAccess{ItemListRequest: &ItemListRequest{ExcludeAllFlags: []UUID{SyncedFlag}, GetFlags: []UUID{SyncedFlag}}}.Marshal(),
		FileAccess{ItemListRequest: &ItemListRequest{SessionID: U32(1)}}.Marshal(),
		FileAccess{TransferStatusResponse: &TransferStatusResponse{NextTransferPriority: U32(24)}}.Marshal(),
		lfield(3, lfield(3, []byte{0, 0, 0})), // packed enum
		unknownFields(),
	}
	typ := reflect.TypeOf(FileAccess{})
	for i := 0; i < typ.NumField(); i++ {
		if !typ.Field(i).IsExported() {
			continue
		}
		var f FileAccess
		fl := filler{}
		fl.fill(reflect.ValueOf(&f).Elem().Field(i))
		seeds = append(seeds, f.Marshal())
	}
	fl := filler{}
	seeds = append(seeds, fl.value(typ).Interface().(FileAccess).Marshal())
	return seeds
}

func FuzzParseFileAccess(f *testing.F) {
	for _, s := range fuzzSeeds() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := ParseFileAccess(b)
		if err != nil {
			return
		}
		_ = m.Kind()
		b1 := m.Marshal()
		m2, err := ParseFileAccess(b1)
		if err != nil {
			t.Fatalf("re-parse of %x: %v", b1, err)
		}
		if !reflect.DeepEqual(m, m2) {
			t.Fatalf("parse -> marshal -> parse changed the message\n first  %#v\n second %#v", m, m2)
		}
		if b2 := m2.Marshal(); !bytes.Equal(b1, b2) {
			t.Fatalf("marshal not stable\n first  %x\n second %x", b1, b2)
		}
		if env, err := ParseEnvelope(m.Smart()); err != nil || env.Service != ServiceFileAccess || !bytes.Equal(env.Payload, b1) {
			t.Fatalf("Smart() does not wrap the payload: %+v, %v", env, err)
		}
	})
}

func FuzzParseEnvelope(f *testing.F) {
	for _, s := range fuzzSeeds() {
		f.Add(Envelope{Service: ServiceFileAccess, Payload: s}.Marshal())
	}
	f.Add([]byte{0x6A, 0x02, 0x72, 0x00})                         // Core ConnectionReadyNotification
	f.Add([]byte{0x12, 0x05, 0x2A, 0x03, 0x0A, 0x01, 'u'})        // RawResourceRequest
	f.Add([]byte{0x12, 0x05, 0x0A, 0x03, 0x0A, 0x01, 'u'})        // ConnectIQHTTPRequest
	f.Add([]byte{0xB2, 0x01, 0x04, 0x0A, 0x02, 0x08, 0x01})       // ExploreSync StartSyncRequest
	f.Add([]byte{0xDA, 0x02, 0x05, 0xAA, 0x01, 0x02, 0x08, 0x18}) // TransferStatusResponse
	f.Fuzz(func(t *testing.T, b []byte) {
		env, err := ParseEnvelope(b)
		if err != nil {
			return
		}
		b1 := env.Marshal()
		env2, err := ParseEnvelope(b1)
		if err != nil {
			t.Fatalf("re-parse of %x: %v", b1, err)
		}
		if !reflect.DeepEqual(env, env2) || !bytes.Equal(env2.Marshal(), b1) {
			t.Fatalf("envelope not stable: %+v vs %+v", env, env2)
		}
		// The dispatch helpers must cope with any payload, and their replies must parse.
		_ = IsCoreConnectionReady(env)
		_, _ = env.FileAccess()
		for _, reply := range []func(Envelope) ([]byte, bool){HTTPNoNetworkReply, ExploreSyncReject, CoreLocationReply} {
			if r, ok := reply(env); ok {
				if _, err := ParseEnvelope(r); err != nil {
					t.Fatalf("reply %x does not parse: %v", r, err)
				}
			}
		}
	})
}
