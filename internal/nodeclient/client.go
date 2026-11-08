// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package nodeclient is a sync node's side of goarmin: it signs the computer in to a server
// (node-auth.json), syncs the watches paired with it on the hub's schedule, hands each pulled file
// to the server before the watch may let go of it, and carries out the server's commands (sync
// now, live values, the file list, unpair, …). goarmin sync -server runs it.
package nodeclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/karamble/goarmin/internal/nodeproto"
	"github.com/karamble/goarmin/pkg/httpx"
)

// ErrRevoked is returned once the server no longer accepts the node's token.
var ErrRevoked = errors.New("the server no longer accepts this node's sign-in (goarmin node login)")

// HTTPError is a server's answer other than 2xx.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("server: %d %s", e.Status, e.Message)
	}
	return fmt.Sprintf("server: %d %s", e.Status, http.StatusText(e.Status))
}

// Client talks to one server as one node.
type Client struct {
	base  *url.URL
	token string
	hc    *http.Client // ordinary requests
	poll  *http.Client // the command long poll
}

// transport dials quickly: a server that is away fails a sync's upload at once, not after a
// minute.
func transport() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	t.ResponseHeaderTimeout = 60 * time.Second
	return t
}

// NewClient makes a client for server (http or https), with token (empty before sign-in).
func NewClient(server, token string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(server), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("the server must be http(s)://host[:port], not %q", server)
	}
	tr := transport()
	return &Client{base: u, token: token,
		hc:   httpx.NewClientOn(2*time.Minute, tr),
		poll: httpx.NewClientOn(nodeproto.CommandWait+45*time.Second, tr)}, nil
}

// Server is the server's address.
func (c *Client) Server() string { return c.base.String() }

func (c *Client) do(ctx context.Context, hc *http.Client, method, path string, hdr http.Header, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base.String()+path, body)
	if err != nil {
		return err
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized && c.token != "" {
		return ErrRevoked
	}
	if resp.StatusCode/100 != 2 {
		var e struct{ Error string }
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		_ = json.Unmarshal(b, &e)
		return &HTTPError{Status: resp.StatusCode, Message: e.Error}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if p, ok := out.(*int); ok { // the caller wants the status (enroll poll)
		*p = resp.StatusCode
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) json(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	hdr := http.Header{}
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
		hdr.Set("Content-Type", "application/json")
	}
	return c.do(ctx, c.hc, method, nodeproto.Prefix+path, hdr, body, out)
}

// Login signs the node in with the account's password.
func (c *Client) Login(ctx context.Context, req nodeproto.LoginRequest) (nodeproto.LoginResponse, error) {
	var out nodeproto.LoginResponse
	return out, c.json(ctx, http.MethodPost, "/login", req, &out)
}

// Enroll starts a code approval.
func (c *Client) Enroll(ctx context.Context, req nodeproto.EnrollRequest) (nodeproto.EnrollResponse, error) {
	var out nodeproto.EnrollResponse
	return out, c.json(ctx, http.MethodPost, "/enroll", req, &out)
}

// PollEnroll returns the token once the code was approved ("" while it waits).
func (c *Client) PollEnroll(ctx context.Context, poll string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.String()+nodeproto.Prefix+"/enroll/"+url.PathEscape(poll), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusAccepted:
		return "", nil
	case http.StatusOK:
		var out nodeproto.PollResponse
		return out.Token, json.NewDecoder(resp.Body).Decode(&out)
	default:
		return "", &HTTPError{Status: resp.StatusCode}
	}
}

// Hello reports the node's watches and gets their config.
func (c *Client) Hello(ctx context.Context, req nodeproto.HelloRequest) (nodeproto.HelloResponse, error) {
	var out nodeproto.HelloResponse
	return out, c.json(ctx, http.MethodPost, "/hello", req, &out)
}

// Upload gives one pulled file to the server.
func (c *Client) Upload(ctx context.Context, unit uint32, name string, data []byte) (nodeproto.UploadResponse, error) {
	sum := sha256.Sum256(data)
	hdr := http.Header{}
	hdr.Set("Content-Type", "application/octet-stream")
	hdr.Set(nodeproto.HeaderUnit, strconv.FormatUint(uint64(unit), 10))
	hdr.Set(nodeproto.HeaderName, name)
	var out nodeproto.UploadResponse
	err := c.do(ctx, c.hc, http.MethodPut, nodeproto.Prefix+"/files/"+hex.EncodeToString(sum[:]), hdr, bytes.NewReader(data), &out)
	return out, err
}

// Stored asks which of a watch's items the server holds.
func (c *Client) Stored(ctx context.Context, unit uint32, uids []string) ([]string, error) {
	var out nodeproto.StoredResponse
	return out.Stored, c.json(ctx, http.MethodPost, "/stored", nodeproto.StoredRequest{Unit: unit, UIDs: uids}, &out)
}

// Run reports a finished sync.
func (c *Client) Run(ctx context.Context, req nodeproto.RunRequest) error {
	return c.json(ctx, http.MethodPost, "/runs", req, nil)
}

// History returns a watch's last sync runs.
func (c *Client) History(ctx context.Context, unit uint32) (nodeproto.HistoryResponse, error) {
	var out nodeproto.HistoryResponse
	return out, c.json(ctx, http.MethodGet, "/history?unit="+strconv.FormatUint(uint64(unit), 10), nil, &out)
}

// Progress reports the node's state.
func (c *Client) Progress(ctx context.Context, req nodeproto.ProgressRequest) error {
	return c.json(ctx, http.MethodPost, "/progress", req, nil)
}

// Commands waits for the server's commands (empty after nodeproto.CommandWait).
func (c *Client) Commands(ctx context.Context) ([]nodeproto.Command, error) {
	var out nodeproto.CommandsResponse
	err := c.do(ctx, c.poll, http.MethodGet, nodeproto.Prefix+"/commands", nil, nil, &out)
	return out.Commands, err
}

// Result answers a command.
func (c *Client) Result(ctx context.Context, id string, res nodeproto.CommandResult) error {
	return c.json(ctx, http.MethodPost, "/commands/"+url.PathEscape(id), res, nil)
}

// Live sends a batch of live readings.
func (c *Client) Live(ctx context.Context, req nodeproto.LiveRequest) error {
	return c.json(ctx, http.MethodPost, "/live", req, nil)
}

// --- Files the node keeps in its data directory ---

// Auth is the node's sign-in: <data>/node-auth.json, readable only by its user.
type Auth struct {
	Server  string `json:"server"`
	NodeID  int64  `json:"node_id"`
	Token   string `json:"token"`
	Account string `json:"account"`
	Name    string `json:"name"`
}

// AuthPath is where the sign-in lives.
func AuthPath(dataDir string) string { return filepath.Join(dataDir, "node-auth.json") }

// LoadAuth reads the node's sign-in; os.ErrNotExist when there is none.
func LoadAuth(dataDir string) (Auth, error) {
	var a Auth
	b, err := os.ReadFile(AuthPath(dataDir))
	if err != nil {
		return a, err
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return a, fmt.Errorf("%s: %w", AuthPath(dataDir), err)
	}
	return a, nil
}

// SaveAuth writes the node's sign-in (0600).
func SaveAuth(dataDir string, a Auth) error {
	return writeJSON(AuthPath(dataDir), a)
}

// RemoveAuth forgets the node's sign-in.
func RemoveAuth(dataDir string) error {
	err := os.Remove(AuthPath(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// cachePath is where the server's last config lives, so the node keeps syncing while the
// server is away.
func cachePath(dataDir string) string { return filepath.Join(dataDir, "node-cache.json") }

func loadCache(dataDir string) (nodeproto.HelloResponse, error) {
	var c nodeproto.HelloResponse
	b, err := os.ReadFile(cachePath(dataDir))
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
