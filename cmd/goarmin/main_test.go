// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	cases := []struct {
		args     []string
		code     int
		out, err string
	}{
		{[]string{"version"}, 0, "goarmin " + version, ""},
		{[]string{"help"}, 0, "usage: goarmin", ""},
		{nil, 2, "", "usage: goarmin"},
		{[]string{"frobnicate"}, 2, "", `unknown command "frobnicate"`},
	}
	for _, c := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(c.args, &stdout, &stderr); code != c.code {
			t.Errorf("%v: exit %d, want %d", c.args, code, c.code)
		}
		if c.out != "" && !strings.Contains(stdout.String(), c.out) {
			t.Errorf("%v: stdout %q lacks %q", c.args, stdout.String(), c.out)
		}
		if c.err != "" && !strings.Contains(stderr.String(), c.err) {
			t.Errorf("%v: stderr %q lacks %q", c.args, stderr.String(), c.err)
		}
	}
}
