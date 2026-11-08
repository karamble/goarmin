// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package httpx is the only place goarmin creates HTTP clients. Every request a client from
// this package sends carries BrowserUA, a regular desktop browser User-Agent, and nothing that
// identifies goarmin, its authors or its users. A guard test fails the build if code elsewhere
// uses http.DefaultClient, http.Get and friends, or constructs its own http.Client.
package httpx

import (
	"net/http"
	"time"
)

// BrowserUA is the User-Agent sent on every outbound request: a current, mainstream Chrome on
// Windows desktop string. Keep it current when Chrome releases move on.
const BrowserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36"

// NewClient returns an HTTP client with the given overall timeout whose requests always carry
// BrowserUA.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: Transport(nil)}
}

// NewClientOn is NewClient over base (e.g. a transport with its own dial timeout), still always
// carrying BrowserUA.
func NewClientOn(timeout time.Duration, base http.RoundTripper) *http.Client {
	return &http.Client{Timeout: timeout, Transport: Transport(base)}
}

// Transport wraps base (http.DefaultTransport when nil) so that every request carries
// BrowserUA, replacing any User-Agent the caller or a library set.
func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return uaTransport{base: base}
}

type uaTransport struct{ base http.RoundTripper }

func (t uaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("User-Agent", BrowserUA)
	return t.base.RoundTrip(r)
}
