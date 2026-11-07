// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package tiles sets up goarmin's map tile cache (github.com/karamble/tilecache): tiles are
// fetched on request with a browser User-Agent, kept under the data directory, and served to
// signed-in users only, privately cached.
//
// There is no default tile server. OpenStreetMap's own servers require an identifying
// User-Agent and answer a browser one with an "Access blocked" picture (as a normal 200 PNG,
// so the cache cannot tell it from a tile); goarmin never identifies itself, so it refuses
// them. The operator names a tile service that serves this kind of use: a preset such as
// jawg-matrix with an access token, or any URL template.
package tiles

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/karamble/goarmin/pkg/httpx"
	"github.com/karamble/tilecache"
)

// CacheControl keeps tiles out of shared caches: which tiles someone looks at shows where they go.
const CacheControl = "private, max-age=86400"

// ErrNoUpstream: no tile server is configured, so there are no map tiles.
var ErrNoUpstream = errors.New("tiles: no tile server configured (GOARMIN_TILE_URL)")

// ErrOSM refuses OpenStreetMap's own tile servers (see the package comment).
var ErrOSM = errors.New("tiles: OpenStreetMap's tile servers block requests without an identifying User-Agent, which goarmin never sends; set GOARMIN_TILE_URL to another tile server")

// ErrNoToken: the tile server's URL has a {token} and none is set.
var ErrNoToken = errors.New("tiles: the tile server needs an access token (GOARMIN_TILE_TOKEN)")

const (
	osmAttribution  = `© <a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noreferrer">OpenStreetMap</a> contributors`
	jawgAttribution = `<a href="https://www.jawg.io" target="_blank" rel="noreferrer">© Jawg Maps</a> · ` + osmAttribution
)

// Preset is a known tile style.
type Preset struct {
	URL         string
	Dark        bool // the style is dark already: no dark filter over it
	Attribution string
}

// Presets are the tile styles goarmin knows by name (GOARMIN_TILE_URL=jawg-matrix).
var Presets = map[string]Preset{
	"jawg-matrix": {URL: "https://tile.jawg.io/jawg-matrix/{z}/{x}/{y}.png?access-token={token}", Dark: true, Attribution: jawgAttribution},
	"jawg-dark":   {URL: "https://tile.jawg.io/jawg-dark/{z}/{x}/{y}.png?access-token={token}", Dark: true, Attribution: jawgAttribution},
}

// Options configures the cache.
type Options struct {
	DataDir string
	// Upstream is a preset name or a tile URL template ({z}, {x}, {y}, optionally {s} and
	// {token}); required. Tiles are fetched only when someone looks at them, never preloaded.
	Upstream string
	// Token replaces {token} in the URL (an access token). It never appears in the logs.
	Token string
	// Dark says a custom style is dark already ("" = guess from its URL: dark, matrix, night).
	Dark string
	// Attribution is the map's credit as HTML ("" = the preset's, else OpenStreetMap's).
	Attribution string
	Logger      *slog.Logger
	// Client replaces the HTTP client (tests); it gets the browser User-Agent either way.
	Client *http.Client
}

// Map is a ready tile cache with what the dashboard needs to show it.
type Map struct {
	Handler     http.Handler
	Dark        bool
	Attribution string
	close       func() error
}

// Close stops the cache's background fetches.
func (m *Map) Close() error { return m.close() }

// New opens the tile cache under <data>/tiles.
func New(opt Options) (*Map, error) {
	up := strings.TrimSpace(opt.Upstream)
	if up == "" {
		return nil, ErrNoUpstream
	}
	attribution := osmAttribution
	dark := false
	if p, ok := Presets[up]; ok {
		up, dark, attribution = p.URL, p.Dark, p.Attribution
	} else {
		switch strings.ToLower(strings.TrimSpace(opt.Dark)) {
		case "1", "true", "yes":
			dark = true
		case "":
			l := strings.ToLower(up)
			dark = strings.Contains(l, "dark") || strings.Contains(l, "matrix") || strings.Contains(l, "night")
		}
	}
	if opt.Attribution != "" {
		attribution = opt.Attribution
	}
	if strings.Contains(up, "tile.openstreetmap.org") {
		return nil, ErrOSM
	}
	if strings.Contains(up, "{token}") {
		if opt.Token == "" {
			return nil, ErrNoToken
		}
		up = strings.ReplaceAll(up, "{token}", opt.Token)
	}
	client := opt.Client
	if client == nil {
		client = httpx.NewClient(15 * time.Second)
	}
	logger := opt.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opt.Token != "" {
		logger = slog.New(redact{h: logger.Handler(), secret: opt.Token})
	}
	c, err := tilecache.New(tilecache.Options{
		Dir:            filepath.Join(opt.DataDir, "tiles"),
		Upstream:       up,
		UserAgent:      httpx.BrowserUA,
		HTTPClient:     client,
		MaxAge:         30 * 24 * time.Hour,
		Concurrency:    2,
		CacheControl:   CacheControl,
		Logger:         logger,
		DisablePreload: true,
	})
	if err != nil {
		// tilecache names the upstream in its errors: keep the token out of them too.
		if opt.Token != "" {
			err = errors.New(strings.ReplaceAll(err.Error(), opt.Token, "…"))
		}
		return nil, err
	}
	return &Map{Handler: c.Handler(), Dark: dark, Attribution: attribution, close: c.Close}, nil
}

// redact is a log handler that replaces a secret (the tile token, part of every upstream URL
// the cache logs) wherever it appears.
type redact struct {
	h      slog.Handler
	secret string
}

func (r redact) clean(s string) string { return strings.ReplaceAll(s, r.secret, "…") }

func (r redact) attr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, r.clean(v.String()))
	case slog.KindGroup:
		var as []any
		for _, g := range v.Group() {
			as = append(as, r.attr(g))
		}
		return slog.Group(a.Key, as...)
	case slog.KindAny:
		return slog.String(a.Key, r.clean(fmt.Sprint(v.Any())))
	}
	return slog.Attr{Key: a.Key, Value: v}
}

func (r redact) Enabled(ctx context.Context, l slog.Level) bool { return r.h.Enabled(ctx, l) }

func (r redact) Handle(ctx context.Context, rec slog.Record) error {
	n := slog.NewRecord(rec.Time, rec.Level, r.clean(rec.Message), rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		n.AddAttrs(r.attr(a))
		return true
	})
	return r.h.Handle(ctx, n)
}

func (r redact) WithAttrs(as []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(as))
	for i, a := range as {
		out[i] = r.attr(a)
	}
	return redact{h: r.h.WithAttrs(out), secret: r.secret}
}

func (r redact) WithGroup(name string) slog.Handler {
	return redact{h: r.h.WithGroup(name), secret: r.secret}
}
