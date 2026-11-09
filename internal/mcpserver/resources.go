// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxResource bounds a file read through MCP; larger files are downloaded in the dashboard.
const maxResource = 16 << 20

// Resource is a file an assistant reads by URI, backed by an API download route.
type Resource struct {
	Name     string
	Title    string
	Desc     string
	Template string // goarmin://activities/{id}/fit
	Method   string
	Path     string // /api/activities/{id}/fit
	MIME     string // the type when the API's answer does not say
	re       *regexp.Regexp
	vars     []string
}

// Route is the API route the resource reads.
func (r *Resource) Route() string { return r.Method + " " + r.Path }

// Resources are the files an assistant can read.
var Resources = []Resource{
	{Name: "activity_fit", Title: "Activity FIT file", Template: "goarmin://activities/{id}/fit", Method: "GET", Path: "/api/activities/{id}/fit",
		MIME: "application/vnd.ant.fit", Desc: "The original FIT file of an activity (list_activities gives the ids)."},
	{Name: "course_fit", Title: "Course FIT file", Template: "goarmin://courses/{id}/fit", Method: "GET", Path: "/api/courses/{id}/fit",
		MIME: "application/vnd.ant.fit", Desc: "A course as a FIT file (list_courses gives the ids)."},
	{Name: "raw_file", Title: "Stored file", Template: "goarmin://files/{id}", Method: "GET", Path: "/api/files/{id}/download",
		MIME: "application/octet-stream", Desc: "Any stored raw file, as it arrived (list_files gives the ids)."},
	{Name: "device_raw_zip", Title: "A device's raw files", Template: "goarmin://devices/{id}/raw.zip", Method: "GET", Path: "/api/devices/{id}/raw.zip",
		MIME: "application/zip", Desc: "Every raw file from a device, zipped (at most 16 MB through MCP; larger in the dashboard)."},
	{Name: "exercise", Title: "Exercise", Template: "goarmin://exercises/{category}/{number}", Method: "GET", Path: "/api/exercises/{category}/{number}",
		MIME: "application/json", Desc: "An exercise of Garmin's catalogue with its figure's key poses."},
}

func init() {
	for i := range Resources {
		r := &Resources[i]
		pat := regexp.QuoteMeta(r.Template)
		for _, m := range regexp.MustCompile(`\\\{([a-z_]+)\\\}`).FindAllStringSubmatch(pat, -1) {
			r.vars = append(r.vars, m[1])
			pat = strings.Replace(pat, m[0], `([^/]+)`, 1)
		}
		r.re = regexp.MustCompile("^" + pat + "$")
	}
}

func (r *Resource) template() *mcp.ResourceTemplate {
	return &mcp.ResourceTemplate{Name: r.Name, Title: r.Title, Description: r.Desc, URITemplate: r.Template, MIMEType: r.MIME}
}

// path turns a URI into the API path, or false if it does not fit the template.
func (r *Resource) path(uri string) (string, bool) {
	m := r.re.FindStringSubmatch(uri)
	if m == nil {
		return "", false
	}
	p := r.Path
	for i, v := range r.vars {
		val, err := url.PathUnescape(m[i+1])
		if err != nil || val == "" || strings.ContainsAny(val, "/?#") {
			return "", false
		}
		p = strings.Replace(p, "{"+v+"}", url.PathEscape(val), 1)
	}
	return p, true
}

func (s *Server) resourceHandler(res *Resource) mcp.ResourceHandler {
	return func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		start := s.now()
		c, ok := callerOf(req.Extra.TokenInfo)
		if !ok {
			return nil, errors.New("no token")
		}
		out, err := s.readResource(ctx, c, res, req.Params.URI)
		errText := ""
		if err != nil {
			errText = err.Error()
		}
		s.logCall(c, "resource:"+res.Name, start, errText)
		return out, err
	}
}

func (s *Server) readResource(ctx context.Context, c caller, res *Resource, uri string) (*mcp.ReadResourceResult, error) {
	if why := s.limit(c, false); why != "" {
		return nil, errors.New(why)
	}
	p, ok := res.path(uri)
	if !ok {
		return nil, mcp.ResourceNotFoundError(uri)
	}
	r, err := http.NewRequestWithContext(ctx, res.Method, p, nil)
	if err != nil {
		return nil, err
	}
	r.Host, r.RemoteAddr = s.public.Host, "mcp:0"
	rec := s.serve(c, r, maxResource+1)
	switch {
	case rec.code == http.StatusNotFound:
		return nil, mcp.ResourceNotFoundError(uri)
	case rec.code >= 400:
		return nil, apiError(rec)
	case rec.over:
		return nil, fmt.Errorf("the file is larger than %d MB, more than MCP carries; download it in the dashboard", maxResource>>20)
	}
	mime := rec.header.Get("Content-Type")
	if mime == "" {
		mime = res.MIME
	}
	body := rec.buf.Bytes()
	if strings.HasPrefix(mime, "application/json") || strings.HasPrefix(mime, "text/") {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: mime, Text: string(body)}}}, nil
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: mime, Blob: append([]byte(nil), body...)}}}, nil
}
