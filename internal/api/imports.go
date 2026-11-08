// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/karamble/goarmin/internal/importer"
	"github.com/karamble/goarmin/internal/ingest"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/garmin/product"
)

// Upload limits (Options.ImportMax and ImportMaxFiles override them).
const (
	defaultImportMax      = 8 << 30
	defaultImportMaxFiles = 100_000
)

func (s *Server) importJSON(i store.Import) map[string]any {
	report := i.Report
	if i.Status == store.ImportRunning && s.opt.Imports != nil {
		if rep, ok := s.opt.Imports.Progress(i.ID); ok {
			if b, err := json.Marshal(rep); err == nil {
				report = b
			}
		}
	}
	if len(report) == 0 {
		report = json.RawMessage(`{}`)
	}
	m := map[string]any{"id": i.ID, "kind": i.Kind, "source": i.Source, "name": i.Name, "device_id": nil,
		"started_by": i.StartedBy, "status": i.Status, "created_at": i.CreatedAt, "started_at": nil, "finished_at": nil,
		"report": report, "error": i.Error}
	if i.DeviceID != 0 {
		m["device_id"] = i.DeviceID
	}
	if i.StartedAt != 0 {
		m["started_at"] = i.StartedAt
	}
	if i.FinishedAt != 0 {
		m["finished_at"] = i.FinishedAt
	}
	return m
}

func (s *Server) noImports(w http.ResponseWriter) bool {
	if s.opt.Imports == nil {
		writeError(w, http.StatusServiceUnavailable, "this server does not import")
		return true
	}
	return false
}

// createImport takes an upload: multipart/form-data with one part per file. A part's
// filename is its path in what was picked ("GARMIN/Activity/x.fit" for a folder), so the
// import can tell a Garmin Connect export by its folders. The files are streamed to disk; the
// import runs in the background (202).
func (s *Server) createImport(w http.ResponseWriter, r *http.Request, u store.User) {
	if s.noImports(w) {
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "multipart/form-data" {
		writeError(w, http.StatusUnsupportedMediaType, "send the files as multipart/form-data")
		return
	}
	max, maxFiles := s.opt.ImportMax, s.opt.ImportMaxFiles
	if max <= 0 {
		max = defaultImportMax
	}
	if maxFiles <= 0 {
		maxFiles = defaultImportMaxFiles
	}
	// A large upload outlasts the server's read and write timeouts: lift them for this request.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})
	r.Body = http.MaxBytesReader(w, r.Body, max)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "send the files as multipart/form-data")
		return
	}
	ctx := r.Context()
	imp, dir, err := s.opt.Imports.NewUpload(ctx, u)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	var files []ingest.StagedFile
	fail := func(status int, msg string) {
		s.opt.Imports.AbortUpload(ctx, imp.ID, msg)
		writeError(w, status, msg)
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				fail(http.StatusRequestEntityTooLarge, fmt.Sprintf("the upload is larger than %d GB", max>>30))
				return
			}
			fail(http.StatusBadRequest, "the upload stopped before it was complete")
			return
		}
		name, ok := partPath(part)
		if !ok {
			part.Close()
			continue
		}
		if len(files) >= maxFiles {
			part.Close()
			fail(http.StatusRequestEntityTooLarge, fmt.Sprintf("an upload takes at most %d files", maxFiles))
			return
		}
		disk := filepath.Join(dir, fmt.Sprintf("%06d", len(files)+1))
		f, err := os.OpenFile(disk, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			part.Close()
			s.opt.Imports.AbortUpload(ctx, imp.ID, "the server could not store the upload")
			s.internalError(w, r, err)
			return
		}
		n, err := io.Copy(f, part)
		cerr := f.Close()
		part.Close()
		if err == nil {
			err = cerr
		}
		if err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				fail(http.StatusRequestEntityTooLarge, fmt.Sprintf("the upload is larger than %d GB", max>>30))
				return
			}
			fail(http.StatusBadRequest, "the upload stopped before it was complete")
			return
		}
		files = append(files, ingest.StagedFile{Disk: disk, Name: name, Size: n})
	}
	if len(files) == 0 {
		fail(http.StatusBadRequest, "the upload holds no files")
		return
	}
	if err := s.opt.Imports.QueueUpload(ctx, imp.ID, uploadName(files), files); err != nil {
		s.opt.Imports.AbortUpload(ctx, imp.ID, "the server could not queue the import")
		s.internalError(w, r, err)
		return
	}
	imp, err = s.st.Import(ctx, u.ID, imp.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, s.importJSON(imp))
}

// partPath is a file part's path in the upload, cleaned so it cannot climb out of a folder.
// Go's Part.FileName keeps only the base name, so the header is read here.
func partPath(p *multipart.Part) (string, bool) {
	_, params, err := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
	if err != nil {
		return "", false
	}
	name := strings.ReplaceAll(params["filename"], "\\", "/")
	if name == "" || strings.ContainsRune(name, 0) || !utf8.ValidString(name) || len(name) > 1024 {
		return "", false
	}
	c := strings.TrimPrefix(path.Clean("/"+name), "/")
	if c == "" || c == "." {
		return "", false
	}
	return c, true
}

// uploadName says in words what an upload was: the one file, or the folder most files are in.
func uploadName(files []ingest.StagedFile) string {
	if len(files) == 1 {
		return path.Base(files[0].Name)
	}
	tops := map[string]int{}
	best := ""
	for _, f := range files {
		top, _, _ := strings.Cut(f.Name, "/")
		tops[top]++
		if tops[top] > tops[best] {
			best = top
		}
	}
	if tops[best] == len(files) && strings.Contains(files[0].Name, "/") {
		return fmt.Sprintf("%s (%d files)", best, len(files))
	}
	return fmt.Sprintf("%d files", len(files))
}

func (s *Server) imports(w http.ResponseWriter, r *http.Request, u store.User) {
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	list, err := s.st.Imports(r.Context(), u.ID, before, limit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	items := []map[string]any{}
	for _, i := range list {
		items = append(items, s.importJSON(i))
	}
	resp := map[string]any{"items": items, "next_before": nil}
	if len(list) == limit {
		resp["next_before"] = list[len(list)-1].ID
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) userImport(w http.ResponseWriter, r *http.Request, u store.User) (store.Import, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such import")
		return store.Import{}, false
	}
	imp, err := s.st.Import(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such import")
		return store.Import{}, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return store.Import{}, false
	}
	return imp, true
}

func (s *Server) getImport(w http.ResponseWriter, r *http.Request, u store.User) {
	if imp, ok := s.userImport(w, r, u); ok {
		writeJSON(w, http.StatusOK, s.importJSON(imp))
	}
}

func (s *Server) cancelImport(w http.ResponseWriter, r *http.Request, u store.User) {
	if s.noImports(w) {
		return
	}
	imp, ok := s.userImport(w, r, u)
	if !ok {
		return
	}
	switch err := s.opt.Imports.Cancel(r.Context(), u, imp.ID); {
	case errors.Is(err, importer.ErrFinished):
		writeError(w, http.StatusConflict, "the import has finished already")
		return
	case errors.Is(err, importer.ErrUploading):
		writeError(w, http.StatusConflict, "the files are still arriving; stop the upload in the browser that sends them")
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	imp, _ = s.st.Import(r.Context(), u.ID, imp.ID)
	writeJSON(w, http.StatusOK, s.importJSON(imp))
}

// usbWatchJSON is a watch on USB as u may see it: another account's watch shows no name of
// its owner's choosing.
func (s *Server) usbWatchJSON(r *http.Request, u store.User, w importer.USBWatch) map[string]any {
	owner := "unknown"
	switch {
	case w.Device.ID == 0:
	case w.Owner == u.ID:
		owner = "yours"
	case w.Owner == 0:
		owner = "nobody"
	default:
		owner = "other"
	}
	m := map[string]any{"id": w.ID, "name": w.Name, "how": w.How, "port": w.Port, "mounted": w.Mount != "",
		"owner": owner, "device": nil, "auto": nil, "importing": nil, "last": nil}
	if owner == "yours" || owner == "nobody" {
		dev := map[string]any{"id": w.Device.ID, "model": product.Name(w.Device.Product), "name": w.Device.Name, "nickname": w.Device.Nickname}
		if owner == "nobody" {
			dev["nickname"] = ""
		}
		m["device"] = dev
	}
	if owner == "yours" {
		m["auto"] = w.Device.USBAuto
		if last, err := s.st.LastDeviceImport(r.Context(), u.ID, w.Device.ID); err == nil {
			m["last"] = s.importJSON(last)
		}
	}
	if w.Importing != 0 {
		if imp, err := s.st.Import(r.Context(), u.ID, w.Importing); err == nil {
			m["importing"] = s.importJSON(imp)
		}
	}
	return m
}

func (s *Server) usbWatches(w http.ResponseWriter, r *http.Request, u store.User) {
	out := []map[string]any{}
	if s.opt.Imports != nil {
		list, err := s.opt.Imports.Watches(r.Context())
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		for _, uw := range list {
			out = append(out, s.usbWatchJSON(r, u, uw))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) importUSB(w http.ResponseWriter, r *http.Request, u store.User) {
	if s.noImports(w) {
		return
	}
	imp, err := s.opt.Imports.ImportUSB(r.Context(), u, r.PathValue("id"), "manual")
	switch {
	case errors.Is(err, importer.ErrNoWatch):
		writeError(w, http.StatusNotFound, "that watch is not plugged in")
		return
	case errors.Is(err, importer.ErrOtherAccount):
		writeError(w, http.StatusConflict, "this watch belongs to another account")
		return
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	status := http.StatusAccepted
	if imp.Status == store.ImportRunning {
		status = http.StatusOK // the import already running for this watch
	}
	writeJSON(w, status, s.importJSON(imp))
}
