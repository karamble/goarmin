// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	run := func(listen string) int {
		var out, errb bytes.Buffer
		return runHealthcheck([]string{"-q", "-listen", listen}, &out, &errb)
	}
	// Any host in the listen address: the check always asks 127.0.0.1 on its port.
	if got := run("0.0.0.0:" + port); got != 0 {
		t.Fatalf("healthy server: exit %d", got)
	}
	status = http.StatusServiceUnavailable
	if got := run("127.0.0.1:" + port); got != 1 {
		t.Fatalf("server answering 503: exit %d", got)
	}
	srv.Close()
	if got := run("127.0.0.1:" + port); got != 1 {
		t.Fatalf("no server: exit %d", got)
	}
	if got := run("no-port"); got != 2 {
		t.Fatalf("bad listen address: exit %d", got)
	}
}
