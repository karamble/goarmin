// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"testing"

	"github.com/karamble/goarmin/pkg/mtp"
)

func TestPickUSB(t *testing.T) {
	watch := mtp.Info{Path: "3-8", Vendor: 0x091e, Product: 0x4ff1, Serial: "W1"}
	phone := mtp.Info{Path: "1-2", Vendor: 0x18d1, Product: 0x4ee1, Serial: "P1"}
	other := mtp.Info{Path: "3-9", Vendor: 0x091e, Product: 0x4ff1, Serial: "W2"}
	for _, tc := range []struct {
		devs []mtp.Info
		want string
		path string // "" = an error
	}{
		{[]mtp.Info{phone, watch}, "", "3-8"},
		{[]mtp.Info{phone}, "", ""},
		{[]mtp.Info{watch, other}, "", ""},
		{[]mtp.Info{watch, other}, "3-9", "3-9"},
		{[]mtp.Info{watch, other}, "W1", "3-8"},
		{[]mtp.Info{phone, watch}, "18D1:4EE1", "1-2"},
		{[]mtp.Info{watch, other}, "091e:4ff1", ""},
		{[]mtp.Info{watch}, "nope", ""},
	} {
		got, err := pickUSB(tc.devs, tc.want)
		if tc.path == "" {
			if err == nil {
				t.Errorf("%q: picked %s, want an error", tc.want, got.Path)
			}
			continue
		}
		if err != nil || got.Path != tc.path {
			t.Errorf("%q: got %s, %v; want %s", tc.want, got.Path, err, tc.path)
		}
	}
}
