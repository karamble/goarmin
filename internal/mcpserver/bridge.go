// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/karamble/goarmin/pkg/httpx"
)

// Bridge forwards a goarmin server's MCP endpoint to a local MCP client over another
// transport (stdio, for goarmin mcp): for clients that start local commands and speak no
// HTTP. It reconnects when the server ended an idle session.
type Bridge struct {
	endpoint string
	token    string
	log      *slog.Logger
	version  string

	mu sync.Mutex
	cs *mcp.ClientSession
}

// Endpoint turns a server URL into its MCP endpoint: http://host:8642 and
// http://host:8642/mcp both give http://host:8642/mcp.
func Endpoint(server string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(server))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("the server must be http(s)://host[:port]")
	}
	u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/mcp") + "/mcp"
	u.RawQuery, u.Fragment = "", ""
	return u.String(), nil
}

// NewBridge prepares a bridge to endpoint with token; nothing connects yet.
func NewBridge(endpoint, token, version string, log *slog.Logger) *Bridge {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Bridge{endpoint: endpoint, token: strings.TrimSpace(token), log: log, version: version}
}

type bearerTransport struct {
	token string
	next  http.RoundTripper
}

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.next.RoundTrip(r)
}

// session is the connection to the server, made on first use.
func (b *Bridge) session(ctx context.Context) (*mcp.ClientSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cs != nil {
		return b.cs, nil
	}
	c := mcp.NewClient(&mcp.Implementation{Name: "goarmin-mcp-bridge", Version: b.version}, nil)
	cs, err := c.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: b.endpoint,
		HTTPClient: httpx.NewClientOn(0, bearerTransport{b.token, http.DefaultTransport}), DisableStandaloneSSE: true}, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", b.endpoint, err)
	}
	b.cs = cs
	return cs, nil
}

// drop forgets a session the server no longer has.
func (b *Bridge) drop(cs *mcp.ClientSession) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cs == cs {
		_ = cs.Close()
		b.cs = nil
	}
}

// gone reports an error after which the request never reached a live session, so sending it
// again on a new session is safe.
func gone(err error) bool {
	return errors.Is(err, mcp.ErrSessionMissing) || errors.Is(err, mcp.ErrConnectionClosed)
}

// do runs f on the session, once more on a new session if the old one was gone.
func do[T any](ctx context.Context, b *Bridge, f func(*mcp.ClientSession) (T, error)) (T, error) {
	cs, err := b.session(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	v, err := f(cs)
	if err != nil && gone(err) {
		b.log.Info("mcp bridge: the session ended, connecting again")
		b.drop(cs)
		if cs, err = b.session(ctx); err != nil {
			var zero T
			return zero, err
		}
		return f(cs)
	}
	return v, err
}

// Catalogue reads what the server offers this token.
func (b *Bridge) Catalogue(ctx context.Context) ([]*mcp.Tool, []*mcp.ResourceTemplate, []*mcp.Prompt, string, error) {
	cs, err := b.session(ctx)
	if err != nil {
		return nil, nil, nil, "", err
	}
	var tools []*mcp.Tool
	for t, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return nil, nil, nil, "", err
		}
		tools = append(tools, t)
	}
	var templates []*mcp.ResourceTemplate
	for t, err := range cs.ResourceTemplates(ctx, nil) {
		if err != nil {
			return nil, nil, nil, "", err
		}
		templates = append(templates, t)
	}
	var prompts []*mcp.Prompt
	for p, err := range cs.Prompts(ctx, nil) {
		if err != nil {
			return nil, nil, nil, "", err
		}
		prompts = append(prompts, p)
	}
	instr := ""
	if r := cs.InitializeResult(); r != nil {
		instr = r.Instructions
	}
	return tools, templates, prompts, instr, nil
}

// Server is a local MCP server with everything the remote one offers this token, each call
// forwarded.
func (b *Bridge) Server(ctx context.Context) (*mcp.Server, error) {
	tools, templates, prompts, instr, err := b.Catalogue(ctx)
	if err != nil {
		return nil, err
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "goarmin", Title: "goarmin", Version: b.version},
		&mcp.ServerOptions{Instructions: instr, Capabilities: &mcp.ServerCapabilities{}, Logger: b.log})
	for _, t := range tools {
		name := t.Name
		srv.AddTool(t, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return do(ctx, b, func(cs *mcp.ClientSession) (*mcp.CallToolResult, error) {
				return cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: req.Params.Arguments})
			})
		})
	}
	for _, t := range templates {
		srv.AddResourceTemplate(t, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return do(ctx, b, func(cs *mcp.ClientSession) (*mcp.ReadResourceResult, error) {
				return cs.ReadResource(ctx, &mcp.ReadResourceParams{URI: req.Params.URI})
			})
		})
	}
	for _, p := range prompts {
		name := p.Name
		srv.AddPrompt(p, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return do(ctx, b, func(cs *mcp.ClientSession) (*mcp.GetPromptResult, error) {
				return cs.GetPrompt(ctx, &mcp.GetPromptParams{Name: name, Arguments: req.Params.Arguments})
			})
		})
	}
	return srv, nil
}

// Close ends the connection to the server.
func (b *Bridge) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cs != nil {
		_ = b.cs.Close()
		b.cs = nil
	}
}
