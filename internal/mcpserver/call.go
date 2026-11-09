// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Level is the access a token's scope gives, and a tool needs.
type Level int

// Access levels, each including the one before.
const (
	Read Level = iota
	Write
	Admin
)

func (l Level) String() string {
	switch l {
	case Write:
		return "write"
	case Admin:
		return "admin"
	}
	return "read"
}

// ParseLevel reads a token's scope.
func ParseLevel(scope string) (Level, bool) {
	switch scope {
	case "read":
		return Read, true
	case "write":
		return Write, true
	case "admin":
		return Admin, true
	}
	return 0, false
}

// Where an argument goes.
const (
	InPath  = "path"
	InQuery = "query"
	InBody  = "body"
	InTool  = "tool" // read by the tool itself, never sent to the API
)

// Arg is one argument of a tool.
type Arg struct {
	Name     string
	In       string
	Type     string // string, integer, number, boolean, array, object, any
	Items    string // an array's element type
	Desc     string
	Required bool
	Enum     []string
}

// What a destructive tool's confirm must name.
const (
	confirmDevice      = "device"       // one of the account's devices, by {id}
	confirmAdminDevice = "admin_device" // any device on the server, by {id}
)

// Tool is one MCP tool: an API route, called as the token's account.
type Tool struct {
	Name   string
	Title  string
	Desc   string
	Area   string
	Level  Level
	Method string
	Path   string
	Args   []Arg
	// Hints for clients.
	ReadOnly, Destructive, Idempotent bool
	// Confirm: a confirm argument must name the device (as the dashboard asks); ConfirmWhen,
	// if set, decides from the arguments whether this call needs it.
	Confirm     string
	ConfirmWhen func(args map[string]any) bool
	// SendConfirm passes confirm on to the API as well (it checks it again).
	SendConfirm bool
	// Body builds a body that is not the body arguments as an object (a raw setting value).
	Body func(args map[string]any) ([]byte, http.Header, error)
	// Post reshapes the API's answer for an assistant (fewer fields, fewer points).
	Post func(v any, args map[string]any) any
	// Run replaces the plain API call (live readings, uploads).
	Run func(ctx context.Context, s *Server, c caller, t *Tool, args map[string]any) (any, error)
	// Covers names further API routes this tool stands for (an event stream it polls).
	Covers []string
}

// Route is the API route the tool calls, as the API registers it.
func (t *Tool) Route() string { return t.Method + " " + t.Path }

// schema is the tool's JSON input schema.
func (t *Tool) schema() map[string]any {
	props := map[string]any{}
	req := []string{}
	for _, a := range t.Args {
		p := map[string]any{"description": a.Desc}
		switch a.Type {
		case "any":
		case "array":
			p["type"] = "array"
			if a.Items == "object" {
				p["items"] = map[string]any{"type": "object"}
			} else if a.Items != "" {
				p["items"] = map[string]any{"type": a.Items}
			}
		default:
			p["type"] = a.Type
		}
		if len(a.Enum) > 0 {
			p["enum"] = a.Enum
		}
		props[a.Name] = p
		if a.Required {
			req = append(req, a.Name)
		}
	}
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(req) > 0 {
		m["required"] = req
	}
	return m
}

func (t *Tool) mcpTool() *mcp.Tool {
	f := false
	ann := &mcp.ToolAnnotations{Title: t.Title, ReadOnlyHint: t.ReadOnly, IdempotentHint: t.Idempotent || t.ReadOnly, OpenWorldHint: &f}
	if !t.ReadOnly {
		d := t.Destructive
		ann.DestructiveHint = &d
	}
	return &mcp.Tool{Name: t.Name, Title: t.Title, Description: t.Desc, InputSchema: t.schema(), Annotations: ann}
}

// toolHandler runs one tool call: level, limits, arguments, the API call and the log.
func (s *Server) toolHandler(t *Tool) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := s.now()
		c, ok := callerOf(req.Extra.TokenInfo)
		if !ok {
			return nil, errors.New("no token")
		}
		res, errText := s.runTool(ctx, c, t, req.Params.Arguments)
		s.logCall(c, t.Name, start, errText)
		return res, nil
	}
}

func toolError(msg string) (*mcp.CallToolResult, string) {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}, msg
}

func (s *Server) runTool(ctx context.Context, c caller, t *Tool, raw json.RawMessage) (*mcp.CallToolResult, string) {
	if c.level < t.Level {
		return toolError(fmt.Sprintf("this token's access (%s) does not allow %s, which needs %s access", c.level, t.Name, t.Level))
	}
	if why := s.limit(c, t.Destructive); why != "" {
		return toolError(why)
	}
	args, err := t.parseArgs(raw)
	if err != nil {
		return toolError(err.Error())
	}
	if t.Confirm != "" && (t.ConfirmWhen == nil || t.ConfirmWhen(args)) {
		if err := s.checkConfirm(ctx, c, t, args); err != nil {
			return toolError(err.Error())
		}
	}
	var v any
	if t.Run != nil {
		v, err = t.Run(ctx, s, c, t, args)
	} else {
		v, err = s.callAPI(ctx, c, t, args)
	}
	if err != nil {
		return toolError(err.Error())
	}
	if t.Post != nil {
		v = t.Post(v, args)
	}
	return result(v)
}

// maxResult bounds a tool's answer: more than this costs an assistant too much context to be
// useful, so the call asks for a narrower question instead.
const maxResult = 1 << 20

func result(v any) (*mcp.CallToolResult, string) {
	structured := v
	switch v.(type) {
	case []any:
		structured = map[string]any{"items": v}
	case map[string]any:
	default:
		structured = map[string]any{"value": v}
	}
	b, err := json.Marshal(structured)
	if err != nil {
		return toolError("the answer could not be encoded")
	}
	if len(b) > maxResult {
		return toolError(fmt.Sprintf("the answer is %.1f MB, more than an MCP answer carries (%d KB): ask for a shorter period, fewer items or fewer points",
			float64(len(b))/(1<<20), maxResult>>10))
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}, StructuredContent: json.RawMessage(b)}, ""
}

// parseArgs reads and checks the call's arguments against the tool's list.
func (t *Tool) parseArgs(raw json.RawMessage) (map[string]any, error) {
	args := map[string]any{}
	if len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&args); err != nil {
			return nil, errors.New("the arguments must be a JSON object")
		}
	}
	known := map[string]Arg{}
	for _, a := range t.Args {
		known[a.Name] = a
	}
	for name, v := range args {
		a, ok := known[name]
		if !ok {
			return nil, fmt.Errorf("%s takes no argument %q (it takes: %s)", t.Name, name, t.argNames())
		}
		if v == nil {
			delete(args, name)
			continue
		}
		if err := checkType(a, v); err != nil {
			return nil, err
		}
	}
	for _, a := range t.Args {
		if _, ok := args[a.Name]; a.Required && !ok {
			return nil, fmt.Errorf("%s needs %q: %s", t.Name, a.Name, a.Desc)
		}
	}
	return args, nil
}

func (t *Tool) argNames() string {
	if len(t.Args) == 0 {
		return "nothing"
	}
	var n []string
	for _, a := range t.Args {
		n = append(n, a.Name)
	}
	return strings.Join(n, ", ")
}

func checkType(a Arg, v any) error {
	bad := func() error { return fmt.Errorf("%q must be %s", a.Name, article(a.Type)) }
	switch a.Type {
	case "string":
		s, ok := v.(string)
		if !ok {
			return bad()
		}
		if len(a.Enum) > 0 && !contains(a.Enum, s) {
			return fmt.Errorf("%q must be one of %s", a.Name, strings.Join(a.Enum, ", "))
		}
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return bad()
		}
		if _, err := strconv.ParseInt(n.String(), 10, 64); err != nil {
			return bad()
		}
	case "number":
		if _, ok := v.(json.Number); !ok {
			return bad()
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return bad()
		}
	case "array":
		if _, ok := v.([]any); !ok {
			return bad()
		}
	case "object":
		if _, ok := v.(map[string]any); !ok {
			return bad()
		}
	}
	return nil
}

func article(typ string) string {
	switch typ {
	case "integer", "array", "object":
		return "an " + typ
	}
	return "a " + typ
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// argString writes an argument as a path or query value.
func argString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		if x {
			return "1"
		}
		return "0"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// request builds the API request for a call.
func (s *Server) request(ctx context.Context, t *Tool, args map[string]any) (*http.Request, error) {
	p := t.Path
	q := url.Values{}
	body := map[string]any{}
	for _, a := range t.Args {
		v, ok := args[a.Name]
		if !ok {
			continue
		}
		switch a.In {
		case InPath:
			p = strings.Replace(p, "{"+a.Name+"}", url.PathEscape(argString(v)), 1)
		case InQuery:
			q.Set(a.Name, argString(v))
		case InBody:
			body[a.Name] = v
		}
	}
	if a, ok := args["confirm"]; ok && t.SendConfirm {
		body["confirm"] = a
	}
	if strings.Contains(p, "{") {
		return nil, fmt.Errorf("%s is missing a path argument", t.Name)
	}
	u := p
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd *bytes.Reader
	hdr := http.Header{}
	switch {
	case t.Body != nil:
		b, h, err := t.Body(args)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
		for k, v := range h {
			hdr[k] = v
		}
	case t.Method == http.MethodGet || t.Method == http.MethodDelete:
	default:
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
		hdr.Set("Content-Type", "application/json")
	}
	var r *http.Request
	var err error
	if rd != nil {
		r, err = http.NewRequestWithContext(ctx, t.Method, u, rd)
	} else {
		r, err = http.NewRequestWithContext(ctx, t.Method, u, nil)
	}
	if err != nil {
		return nil, err
	}
	for k, v := range hdr {
		r.Header[k] = v
	}
	r.Host = s.public.Host
	r.RemoteAddr = "mcp:0"
	r.Header.Set("Accept", "application/json")
	return r, nil
}

// serve runs an API request as the caller's account and records the answer, up to limit bytes.
func (s *Server) serve(c caller, r *http.Request, limit int) *recorder {
	rec := newRecorder(limit)
	s.opt.API.ServeAs(c.user, rec, r)
	return rec
}

// callAPI is a plain tool call: the API route with the arguments, the JSON answer decoded.
func (s *Server) callAPI(ctx context.Context, c caller, t *Tool, args map[string]any) (any, error) {
	r, err := s.request(ctx, t, args)
	if err != nil {
		return nil, err
	}
	return decodeAnswer(s.serve(c, r, maxResult+1))
}

// decodeAnswer turns an API answer into a value, or its error sentence into an error.
func decodeAnswer(rec *recorder) (any, error) {
	if rec.code >= 400 {
		return nil, apiError(rec)
	}
	if rec.over {
		return nil, fmt.Errorf("the answer is larger than an MCP answer carries (%d KB): ask for a shorter period, fewer items or fewer points", maxResult>>10)
	}
	body := bytes.TrimSpace(rec.buf.Bytes())
	if len(body) == 0 {
		return map[string]any{"ok": true}, nil
	}
	if ct := rec.header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		return nil, fmt.Errorf("this answer is a file (%s): read it as a resource instead", ct)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, errors.New("the API's answer could not be read")
	}
	return v, nil
}

// apiError is the API's own sentence for a failed request.
func apiError(rec *recorder) error {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(rec.buf.Bytes(), &e) == nil && e.Error != "" {
		return fmt.Errorf("%s (HTTP %d)", e.Error, rec.code)
	}
	if msg := strings.TrimSpace(rec.buf.String()); msg != "" && len(msg) < 300 {
		return fmt.Errorf("%s (HTTP %d)", msg, rec.code)
	}
	return fmt.Errorf("the request failed (HTTP %d)", rec.code)
}

// checkConfirm makes a destructive call name its device, as the dashboard's typed confirmation
// does: the device's nickname, model or name, ignoring case.
func (s *Server) checkConfirm(ctx context.Context, c caller, t *Tool, args map[string]any) error {
	typed, _ := args["confirm"].(string)
	typed = strings.ToLower(strings.TrimSpace(typed))
	id := argString(args["id"])
	list := "/api/devices"
	if t.Confirm == confirmAdminDevice {
		list = "/api/admin/devices"
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, list, nil)
	if err != nil {
		return err
	}
	r.Host, r.RemoteAddr = s.public.Host, "mcp:0"
	v, err := decodeAnswer(s.serve(c, r, 8<<20))
	if err != nil {
		return err
	}
	items, _ := v.([]any)
	for _, it := range items {
		d, _ := it.(map[string]any)
		if argString(d["id"]) != id {
			continue
		}
		for _, k := range []string{"nickname", "model", "name"} {
			if n, _ := d[k].(string); typed != "" && strings.TrimSpace(n) != "" && strings.ToLower(strings.TrimSpace(n)) == typed {
				return nil
			}
		}
		// The names are not repeated here: the user types the name, the assistant does not copy it.
		if typed == "" {
			return fmt.Errorf("%s removes data: ask the user to type the device's name (its nickname, model or name) and pass it as confirm", t.Name)
		}
		return errors.New("confirm does not name this device; nothing was changed. Ask the user to type the device's name")
	}
	return errors.New("no such device (HTTP 404)")
}

// recorder keeps an in-process API answer.
type recorder struct {
	mu     sync.Mutex
	header http.Header
	code   int
	buf    bytes.Buffer
	limit  int
	over   bool
	wrote  chan struct{} // closed on the first body bytes
	once   sync.Once
}

func newRecorder(limit int) *recorder {
	return &recorder{header: http.Header{}, limit: limit, wrote: make(chan struct{})}
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.code == 0 {
		r.code = code
	}
}

func (r *recorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	if r.code == 0 {
		r.code = http.StatusOK
	}
	if room := r.limit - r.buf.Len(); room < len(b) {
		r.over = true
		if room > 0 {
			r.buf.Write(b[:room])
		}
	} else {
		r.buf.Write(b)
	}
	r.mu.Unlock()
	r.once.Do(func() { close(r.wrote) })
	return len(b), nil
}

// Flush lets streaming handlers stream (they flush after each event).
func (r *recorder) Flush() {}

// snapshot is the body so far.
func (r *recorder) snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.buf.Bytes()...)
}

func netipAddr(a net.Addr) (netip.Addr, error) {
	switch x := a.(type) {
	case *net.TCPAddr:
		ap, ok := netip.AddrFromSlice(x.IP)
		if !ok {
			return netip.Addr{}, errors.New("no address")
		}
		return ap.Unmap(), nil
	}
	ap, err := netip.ParseAddrPort(a.String())
	if err != nil {
		return netip.Addr{}, err
	}
	return ap.Addr(), nil
}
