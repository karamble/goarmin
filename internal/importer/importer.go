// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package importer runs goarmin's imports in the background, one at a time: files uploaded in
// the browser (FIT files, folders, zip archives, the Garmin Connect export) and watches
// plugged in by USB, which it notices and imports for their owners by itself.
package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/karamble/goarmin/internal/ingest"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/usbsource"
	"github.com/karamble/goarmin/pkg/zipwalk"
)

// Options configure a Runner.
type Options struct {
	Store   *store.Store
	In      *ingest.Ingester
	DataDir string
	// USB finds and reads watches plugged in by USB; nil turns USB off.
	USB    *usbsource.Finder
	Logger *slog.Logger
	// Every is how often the USB watcher looks for watches (5 s).
	Every time.Duration
	// Save is how often a running import's report is stored (2 s).
	Save time.Duration
	// Limits bound the zip archives of an upload.
	Limits zipwalk.Limits
	// Finished, if set, hears of each import that ends.
	Finished func(store.Import)
}

// Runner queues and runs imports.
type Runner struct {
	opt  Options
	log  *slog.Logger
	wake chan struct{}

	mu      sync.Mutex
	queue   []int64
	jobs    map[int64]*job   // queued or running
	watches map[string]int64 // USB watch id → its import, queued or running
	present map[string]bool  // USB watches at the last look
	usbOf   map[int64]string // import → USB watch id
	// fresh are the uploads this runner created: recovery at start-up leaves them alone, since
	// an upload may begin while the runner starts.
	fresh map[int64]bool
}

type job struct {
	cancel   context.CancelFunc
	canceled bool // by its owner
	rep      ingest.Report
	running  bool
}

var (
	// ErrFinished: the import has ended already.
	ErrFinished = errors.New("importer: the import has finished")
	// ErrNoWatch: no such watch is plugged in.
	ErrNoWatch = errors.New("importer: that watch is not plugged in")
	// ErrUploading: the files are still arriving; the browser stops an upload.
	ErrUploading = errors.New("importer: the files are still arriving")
)

// New builds a Runner; Run starts it.
func New(opt Options) *Runner {
	if opt.Every <= 0 {
		opt.Every = 5 * time.Second
	}
	if opt.Save <= 0 {
		opt.Save = 2 * time.Second
	}
	log := opt.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Runner{opt: opt, log: log, wake: make(chan struct{}, 1), jobs: map[int64]*job{},
		watches: map[string]int64{}, present: map[string]bool{}, usbOf: map[int64]string{}, fresh: map[int64]bool{}}
}

func (r *Runner) staging(id int64) string {
	return filepath.Join(r.opt.DataDir, "imports", strconv.FormatInt(id, 10))
}

// Run runs queued imports until ctx ends, and watches USB when there is a Finder. Imports left
// unfinished by an earlier run are taken up again (a complete upload) or ended.
func (r *Runner) Run(ctx context.Context) {
	r.recover(ctx)
	if r.opt.USB != nil {
		go r.watchUSB(ctx)
	}
	for {
		if ctx.Err() != nil {
			return
		}
		r.mu.Lock()
		var id int64
		if len(r.queue) > 0 {
			id, r.queue = r.queue[0], r.queue[1:]
		}
		r.mu.Unlock()
		if id != 0 {
			r.run(ctx, id)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		}
	}
}

func (r *Runner) recover(ctx context.Context) {
	left, err := r.opt.Store.UnfinishedImports(ctx)
	if err != nil {
		r.log.Error("importer: unfinished imports", "err", err)
	}
	keep := map[string]bool{}
	r.mu.Lock()
	fresh := make(map[int64]bool, len(r.fresh))
	for id := range r.fresh {
		fresh[id] = true
	}
	r.mu.Unlock()
	for _, imp := range left {
		if fresh[imp.ID] {
			keep[strconv.FormatInt(imp.ID, 10)] = true // begun since this runner started: not left over
			continue
		}
		if imp.Kind == store.ImportUpload && imp.Status != store.ImportUploading {
			if _, err := os.Stat(filepath.Join(r.staging(imp.ID), "manifest.json")); err == nil {
				keep[strconv.FormatInt(imp.ID, 10)] = true
				_ = r.opt.Store.SetImportStatus(ctx, imp.ID, store.ImportQueued)
				r.enqueue(imp.ID)
				continue
			}
		}
		// Only if it is still as it was seen: an upload whose request is still running at start-up
		// may queue itself meanwhile.
		_, _ = r.opt.Store.FailStaleImport(ctx, imp.ID, imp.Status, imp.Report, "the server stopped before the import finished")
	}
	// Staging left from imports that are over (an upload begun meanwhile registered itself
	// before making its folder, so it is in fresh by now).
	ents, _ := os.ReadDir(filepath.Join(r.opt.DataDir, "imports"))
	r.mu.Lock()
	for id := range r.fresh {
		keep[strconv.FormatInt(id, 10)] = true
	}
	r.mu.Unlock()
	for _, e := range ents {
		if !keep[e.Name()] {
			os.RemoveAll(filepath.Join(r.opt.DataDir, "imports", e.Name()))
		}
	}
}

// enqueue queues an import once: one already queued or running (the start-up recovery and a
// fresh upload can both ask for the same import) is not queued again, so it never runs twice.
func (r *Runner) enqueue(id int64) {
	r.mu.Lock()
	if r.jobs[id] != nil {
		r.mu.Unlock()
		return
	}
	r.queue = append(r.queue, id)
	r.jobs[id] = &job{}
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// NewUpload records an upload that is about to arrive and returns its staging folder.
func (r *Runner) NewUpload(ctx context.Context, user store.User) (store.Import, string, error) {
	imp, err := r.opt.Store.CreateImport(ctx, store.Import{UserID: user.ID, Kind: store.ImportUpload, Source: store.SourceUpload,
		Status: store.ImportUploading})
	if err != nil {
		return store.Import{}, "", err
	}
	r.mu.Lock()
	r.fresh[imp.ID] = true
	r.mu.Unlock()
	dir := r.staging(imp.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		_ = r.opt.Store.FinishImport(ctx, imp.ID, store.ImportFailed, nil, err.Error())
		return store.Import{}, "", err
	}
	return imp, dir, nil
}

// QueueUpload queues an upload whose files have all arrived.
func (r *Runner) QueueUpload(ctx context.Context, id int64, name string, files []ingest.StagedFile) error {
	b, err := json.Marshal(files)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.staging(id), "manifest.json"), b, 0o600); err != nil {
		return err
	}
	if err := r.opt.Store.SetImportSource(ctx, id, store.SourceUpload, name); err != nil {
		return err
	}
	if err := r.opt.Store.SetImportStatus(ctx, id, store.ImportQueued); err != nil {
		return err
	}
	r.enqueue(id)
	return nil
}

// AbortUpload ends an upload that did not arrive.
func (r *Runner) AbortUpload(ctx context.Context, id int64, reason string) {
	_ = r.opt.Store.FinishImport(ctx, id, store.ImportFailed, nil, reason)
	os.RemoveAll(r.staging(id))
}

// Cancel stops one of user's imports: a queued one never runs, a running one stops after the
// file it is on. What it stored stays.
func (r *Runner) Cancel(ctx context.Context, user store.User, id int64) error {
	imp, err := r.opt.Store.Import(ctx, user.ID, id)
	if err != nil {
		return err
	}
	if imp.Finished() {
		return ErrFinished
	}
	if imp.Status == store.ImportUploading {
		return ErrUploading
	}
	r.mu.Lock()
	j := r.jobs[id]
	queued := false
	for i, q := range r.queue {
		if q == id {
			r.queue = append(r.queue[:i:i], r.queue[i+1:]...)
			queued = true
			break
		}
	}
	if queued {
		delete(r.jobs, id)
		r.forgetWatch(id)
	}
	if j != nil && !queued {
		j.canceled = true
		if j.cancel != nil {
			j.cancel()
		}
	}
	r.mu.Unlock()
	if queued || j == nil {
		if err := r.opt.Store.FinishImport(ctx, id, store.ImportCanceled, imp.Report, ""); err != nil {
			return err
		}
		os.RemoveAll(r.staging(id))
	}
	return nil
}

// Progress is a running import's report so far.
func (r *Runner) Progress(id int64) (ingest.Report, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j := r.jobs[id]
	if j == nil || !j.running {
		return ingest.Report{}, false
	}
	return j.rep, true
}

func (r *Runner) forgetWatch(id int64) {
	if w, ok := r.usbOf[id]; ok {
		delete(r.usbOf, id)
		if r.watches[w] == id {
			delete(r.watches, w)
		}
	}
}

func (r *Runner) run(ctx context.Context, id int64) {
	r.mu.Lock()
	j := r.jobs[id]
	watchID := r.usbOf[id]
	r.mu.Unlock()
	if j == nil {
		return // canceled while queued
	}
	imp, err := r.opt.Store.ImportByID(ctx, id)
	if err == nil && imp.Finished() {
		err = ErrFinished
	}
	var user store.User
	if err == nil {
		user, err = r.opt.Store.User(ctx, imp.UserID)
	}
	if err != nil {
		r.finish(ctx, id, imp, ingest.Report{}, err)
		return
	}
	jctx, cancel := context.WithCancel(ctx)
	defer cancel()
	r.mu.Lock()
	j.cancel, j.running = cancel, true
	canceled := j.canceled
	r.mu.Unlock()
	if canceled {
		cancel()
	}
	_ = r.opt.Store.SetImportStatus(ctx, id, store.ImportRunning)
	r.log.Info("import started", "import", id, "kind", imp.Kind, "user", user.ID)

	zw := &zipwalk.Walker{Limits: r.opt.Limits}
	opt := ingest.ImportOptions{Source: imp.Source}
	last := time.Now()
	plain := 0 // uploaded files that are no archive: known from the start
	opt.Progress = func(rep ingest.Report) {
		rep.Archives = zw.Archives()
		if zw.Archives() > 0 || plain > 0 {
			rep.Found = plain + zw.Found()
		}
		r.mu.Lock()
		j.rep = snapshot(rep)
		r.mu.Unlock()
		if time.Since(last) >= r.opt.Save {
			last = time.Now()
			if b, err := json.Marshal(rep); err == nil {
				_ = r.opt.Store.SaveImportReport(ctx, id, b)
			}
		}
	}
	var src ingest.Source
	var watch usbsource.Watch
	switch imp.Kind {
	case store.ImportUpload:
		var files []ingest.StagedFile
		b, err := os.ReadFile(filepath.Join(r.staging(id), "manifest.json"))
		if err == nil {
			err = json.Unmarshal(b, &files)
		}
		if err != nil {
			r.finish(ctx, id, imp, ingest.Report{}, fmt.Errorf("the uploaded files are gone: %w", err))
			return
		}
		zw.TempDir = r.staging(id)
		src = ingest.Staged(files, zw)
		for _, f := range files {
			if !strings.EqualFold(filepath.Ext(f.Name), ".zip") {
				plain++
			}
		}
	case store.ImportUSB:
		watch, err = r.findWatch(ctx, watchID)
		if err == nil {
			var unlock func()
			unlock, err = usbsource.Lock(r.opt.DataDir)
			if err == nil {
				defer unlock()
			}
		}
		if err != nil {
			r.finish(ctx, id, imp, ingest.Report{}, err)
			return
		}
		src, opt.Skip, opt.Stored = r.usbSource(ctx, user, watch)
	default:
		r.finish(ctx, id, imp, ingest.Report{}, fmt.Errorf("unknown import kind %q", imp.Kind))
		return
	}
	rep, err := r.opt.In.ImportFrom(jctx, user, src, opt)
	rep.Archives = zw.Archives()
	if zw.Archives() > 0 || plain > 0 {
		rep.Found = plain + zw.Found()
	}
	if rep.Export && imp.Source != store.SourceGCExport {
		imp.Source = store.SourceGCExport
		_ = r.opt.Store.SetImportSource(ctx, id, store.SourceGCExport, imp.Name)
	}
	if imp.Kind == store.ImportUSB {
		r.learn(ctx, imp, watch, rep)
	}
	r.finish(ctx, id, imp, rep, err)
}

// snapshot copies a report so that readers do not share its maps with the running import.
func snapshot(rep ingest.Report) ingest.Report {
	if rep.KeptBy != nil {
		m := make(map[string]int, len(rep.KeptBy))
		for k, v := range rep.KeptBy {
			m[k] = v
		}
		rep.KeptBy = m
	}
	rep.Watches = nil
	rep.Errors = append([]string(nil), rep.Errors...)
	return rep
}

// finish ends an import: done, canceled by its owner, or failed (the server stopping too).
func (r *Runner) finish(ctx context.Context, id int64, imp store.Import, rep ingest.Report, err error) {
	r.mu.Lock()
	j := r.jobs[id]
	delete(r.jobs, id)
	r.forgetWatch(id)
	r.mu.Unlock()
	status, text := store.ImportDone, ""
	switch {
	case j != nil && j.canceled:
		status = store.ImportCanceled
	case err != nil && ctx.Err() != nil:
		status, text = store.ImportFailed, "the server stopped before the import finished"
	case err != nil:
		status, text = store.ImportFailed, err.Error()
	}
	b, _ := json.Marshal(rep)
	// The server stopping must not keep the record from being written.
	wctx := context.WithoutCancel(ctx)
	if err := r.opt.Store.FinishImport(wctx, id, status, b, text); err != nil {
		r.log.Error("import: recording the end", "import", id, "err", err)
	}
	os.RemoveAll(r.staging(id))
	r.log.Info("import finished", "import", id, "status", status, "seen", rep.Seen, "new", rep.New, "known", rep.Known,
		"kept", rep.Kept, "unchanged", rep.Unchanged, "failed", rep.Failed, "err", text)
	if r.opt.Finished != nil {
		if done, err := r.opt.Store.ImportByID(wctx, id); err == nil {
			r.opt.Finished(done)
		}
	}
}
