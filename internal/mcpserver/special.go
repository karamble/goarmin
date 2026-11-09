// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// Tools that do more than one API call.

// intArg reads an integer argument with a default and bounds.
func intArg(args map[string]any, name string, def, lo, hi int) int {
	n := def
	if v, ok := args[name].(json.Number); ok {
		if i, err := strconv.Atoi(v.String()); err == nil {
			n = i
		}
	}
	return max(lo, min(hi, n))
}

// liveOpen is how long the watch's live link may take to open (scan, connect, handshake).
var liveOpen = 45 * time.Second

// liveReadings opens the live stream of GET /api/devices/{id}/live, reads it for a few seconds
// once it is open, and returns the readings and why it ended.
func liveReadings(ctx context.Context, s *Server, c caller, t *Tool, args map[string]any) (any, error) {
	seconds := intArg(args, "seconds", 10, 1, 30)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	r, err := s.request(ctx, t, args)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Accept", "text/event-stream")
	rec := newRecorder(4 << 20)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.opt.API.ServeAs(c.user, rec, r)
	}()
	open := time.NewTimer(liveOpen)
	defer open.Stop()
	select {
	case <-rec.wrote:
	case <-done:
	case <-open.C:
		cancel()
		<-done
		return nil, errors.New("the watch's live link did not open in time; the watch may be out of range")
	case <-ctx.Done():
		<-done
		return nil, ctx.Err()
	}
	read := time.NewTimer(time.Duration(seconds) * time.Second)
	defer read.Stop()
	ended := "time_up"
	select {
	case <-read.C:
		cancel()
		<-done
	case <-done:
		ended = ""
	case <-ctx.Done():
		<-done
	}
	if rec.code >= 400 {
		return nil, apiError(rec)
	}
	out := map[string]any{"seconds": seconds}
	readings := []any{}
	latest := map[string]any{}
	for _, ev := range parseSSE(rec.snapshot()) {
		var v map[string]any
		dec := json.NewDecoder(strings.NewReader(ev.data))
		dec.UseNumber()
		if dec.Decode(&v) != nil {
			continue
		}
		switch ev.name {
		case "hello":
			out["since"] = v["since"]
		case "reading":
			readings = append(readings, v)
			if n, ok := v["name"].(string); ok {
				latest[n] = map[string]any{"value": v["value"], "unit": v["unit"], "t": v["t"]}
			}
		case "end":
			out["end"] = v
			if r, ok := v["reason"].(string); ok {
				ended = r
			}
		}
	}
	if ended == "" {
		ended = "closed"
	}
	out["ended"], out["readings"], out["latest"] = ended, readings, latest
	return out, nil
}

type sseEvent struct{ name, data string }

func parseSSE(b []byte) []sseEvent {
	var out []sseEvent
	var ev sseEvent
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if ev.name != "" || ev.data != "" {
				out = append(out, ev)
			}
			ev = sseEvent{}
		case strings.HasPrefix(line, "event:"):
			ev.name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			ev.data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	return out
}

// maxUpload bounds what import_files carries: base64 in a JSON-RPC message is no way to move a
// multi-gigabyte export.
const maxUpload = 32 << 20

// importFiles sends base64 files as the dashboard's upload does (multipart, one part per file).
func importFiles(ctx context.Context, s *Server, c caller, t *Tool, args map[string]any) (any, error) {
	list, _ := args["files"].([]any)
	if len(list) == 0 {
		return nil, errors.New("files is empty")
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	total := 0
	for i, it := range list {
		f, ok := it.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("files[%d] must be {name, content_base64}", i)
		}
		name, _ := f["name"].(string)
		b64, _ := f["content_base64"].(string)
		for k := range f {
			if k != "name" && k != "content_base64" {
				return nil, fmt.Errorf("files[%d] has an unknown field %q (it takes name and content_base64)", i, k)
			}
		}
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("files[%d] needs a name", i)
		}
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			if data, err = base64.RawStdEncoding.DecodeString(b64); err != nil {
				return nil, fmt.Errorf("files[%d] (%s): content_base64 is not base64", i, name)
			}
		}
		if total += len(data); total > maxUpload {
			return nil, fmt.Errorf("the files are larger than %d MB in all; upload them in the dashboard (Devices › Import)", maxUpload>>20)
		}
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename=%q`, name))
		h.Set("Content-Type", "application/octet-stream")
		pw, err := mw.CreatePart(h)
		if err != nil {
			return nil, err
		}
		if _, err := pw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, t.Path, &body)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Host, r.RemoteAddr = s.public.Host, "mcp:0"
	return decodeAnswer(s.serve(c, r, maxResult+1))
}

// thinColumns keeps every k-th sample of a columnar track ({"t": […], "<field>": […]}), so at
// most max_points remain; columns of another length stay as they are.
func thinColumns(v any, args map[string]any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	keep := intArg(args, "max_points", 600, 10, 5000)
	n := 0
	for _, k := range []string{"t", "distance", "d"} {
		if a, ok := m[k].([]any); ok {
			n = len(a)
			break
		}
	}
	if n <= keep {
		return v
	}
	step := (n + keep - 1) / keep
	out := map[string]any{}
	for k, x := range m {
		a, ok := x.([]any)
		if !ok || len(a) != n {
			out[k] = x
			continue
		}
		thin := make([]any, 0, keep+1)
		for i := 0; i < n; i += step {
			thin = append(thin, a[i])
		}
		if (n-1)%step != 0 {
			thin = append(thin, a[n-1])
		}
		out[k] = thin
	}
	out["thinned"] = map[string]any{"samples": n, "every": step}
	return out
}

// filterExercises narrows the catalogue to a category or a name, and drops the figure's pose
// data an assistant has no use for.
func filterExercises(v any, args map[string]any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	cat, _ := args["category"].(string)
	search, _ := args["search"].(string)
	search = strings.ToLower(strings.TrimSpace(search))
	list, _ := m["exercises"].([]any)
	out := []any{}
	for _, it := range list {
		e, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if cat != "" && e["category"] != cat {
			continue
		}
		if search != "" {
			name, _ := e["name"].(string)
			title, _ := e["title"].(string)
			if !strings.Contains(strings.ToLower(name), search) && !strings.Contains(strings.ToLower(title), search) {
				continue
			}
		}
		out = append(out, map[string]any{"category": e["category"], "number": e["number"], "name": e["name"], "title": e["title"],
			"quality": e["quality"]})
	}
	return map[string]any{"categories": m["categories"], "exercises": out, "count": len(out)}
}

// exerciseOnly keeps an exercise's entry without its pose's quaternions.
func exerciseOnly(v any, _ map[string]any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := map[string]any{"exercise": m["exercise"]}
	if keys, ok := m["keys"].([]any); ok {
		out["key_poses"] = len(keys)
	}
	return out
}
