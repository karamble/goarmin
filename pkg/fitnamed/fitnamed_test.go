// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fitnamed

import (
	"testing"
	"time"

	"github.com/muktihari/fit/profile/typedef"
	"github.com/muktihari/fit/proto"

	"github.com/karamble/goarmin/pkg/fitid/fittest"
)

type F = fittest.F

func TestDecode(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	ts := proto.Uint32(fittest.FITTime(now))
	b := fittest.Build(typedef.File(9), 1, now,
		fittest.Message(30, F{Num: 253, Value: ts}, F{Num: 0, Value: proto.Uint16(7520)}, F{Num: 1, Value: proto.Uint16(0xFFFF)},
			F{Num: 13, Value: proto.Uint16(231)}),
		fittest.Message(275, F{Num: 253, Value: ts}, F{Num: 0, Value: proto.Uint8(3)}),
		fittest.Message(397, F{Num: 253, Value: ts}, F{Num: 1, Value: proto.Float32(-0.25)}),
		fittest.Message(64999, F{Num: 7, Value: proto.Uint8(42)}),
	)
	msgs, err := Decode(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 5 || msgs[0].Name != "file_id" {
		t.Fatalf("%d messages: %+v", len(msgs), msgs)
	}
	w := msgs[1]
	if w.Name != "weight_scale" || !w.Official {
		t.Fatalf("weight %+v", w)
	}
	if v, _ := w.Get("weight"); v != 75.2 {
		t.Fatalf("weight %v", v)
	}
	if _, ok := w.Get("percent_fat"); ok {
		t.Fatal("an invalid field is kept")
	}
	if v, _ := w.Get("timestamp"); v != now.UTC().Format(time.RFC3339) {
		t.Fatalf("timestamp %v", v)
	}
	if v, _ := w.Get("bmi"); v != 23.1 {
		t.Fatalf("bmi %v", v)
	}
	if v, _ := msgs[2].Get("sleep_level"); v != "deep" {
		t.Fatalf("an enumeration by name: %v", v)
	}
	if v, _ := msgs[3].Get("deviation"); v != -0.25 || msgs[3].Official {
		t.Fatalf("community message %+v", msgs[3])
	}
	u := msgs[4]
	if u.Name != "message_64999" || u.Known || len(u.Fields) != 1 || u.Fields[0].Name != "field_7" || u.Fields[0].Value != int64(42) {
		t.Fatalf("unknown message %+v", u)
	}
	if m := w.Map(); m["weight"] != 75.2 || len(m) != 3 {
		t.Fatalf("map %v", m)
	}
	only, _ := Decode(b, func(n uint16) bool { return n == 397 })
	if len(only) != 1 {
		t.Fatalf("keep: %d", len(only))
	}
	empty := FromProto(&proto.Message{Num: 30})
	if empty.Fields == nil {
		t.Fatal("a message without fields must list none, not nil (JSON null)")
	}
	if _, err := Decode([]byte("not fit at all"), nil); err != ErrNotFIT {
		t.Fatal(err)
	}
}
