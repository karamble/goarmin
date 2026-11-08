// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package importer

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/karamble/goarmin/internal/ingest"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/usbsource"
)

// USBWatch is a watch on USB with what goarmin knows of it.
type USBWatch struct {
	usbsource.Watch
	// Device is the watch it is (ID 0 when goarmin has not seen its files yet).
	Device store.Device
	// Owner owns Device now (0: nobody).
	Owner int64
	// Importing is its import, queued or running (0: none).
	Importing int64
}

// Watches lists the Garmin watches on USB.
func (r *Runner) Watches(ctx context.Context) ([]USBWatch, error) {
	if r.opt.USB == nil {
		return nil, nil
	}
	ws, err := r.opt.USB.Find(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]USBWatch, 0, len(ws))
	for _, w := range ws {
		u := USBWatch{Watch: w}
		if d, ok := r.resolve(ctx, w); ok {
			u.Device = d
			if o, err := r.opt.Store.Owner(ctx, d.ID, time.Now().Unix()); err == nil {
				u.Owner = o
			}
		}
		r.mu.Lock()
		u.Importing = r.watches[w.ID]
		r.mu.Unlock()
		out = append(out, u)
	}
	return out, nil
}

// resolve finds the watch a USB device is: by the serial an earlier USB import learnt, else by
// reading the serial as its unit number (decimal, or hexadecimal as some Garmins report it).
func (r *Runner) resolve(ctx context.Context, w usbsource.Watch) (store.Device, bool) {
	if w.Serial == "" {
		return store.Device{}, false
	}
	if d, err := r.opt.Store.DeviceByUSBSerial(ctx, w.Serial); err == nil {
		return d, true
	}
	s := strings.TrimLeft(w.Serial, "0")
	for _, base := range []int{10, 16} {
		if n, err := strconv.ParseUint(s, base, 32); err == nil && n != 0 {
			if d, err := r.opt.Store.DeviceByUnit(ctx, uint32(n)); err == nil {
				return d, true
			}
		}
	}
	return store.Device{}, false
}

// ErrOtherAccount: the watch belongs to another account now.
var ErrOtherAccount = errors.New("importer: this watch belongs to another account")

// ImportUSB queues an import of a watch on USB for user, or returns the one already queued or
// running for it. startedBy is "manual" or "plugged".
func (r *Runner) ImportUSB(ctx context.Context, user store.User, watchID, startedBy string) (store.Import, error) {
	w, err := r.findWatch(ctx, watchID)
	if err != nil {
		return store.Import{}, err
	}
	d, known := r.resolve(ctx, w)
	if known {
		if o, err := r.opt.Store.Owner(ctx, d.ID, time.Now().Unix()); err == nil && o != user.ID {
			return store.Import{}, ErrOtherAccount
		}
	}
	r.mu.Lock()
	running := r.watches[w.ID]
	r.mu.Unlock()
	if running != 0 {
		if imp, err := r.opt.Store.ImportByID(ctx, running); err == nil {
			if imp.UserID != user.ID {
				return store.Import{}, ErrOtherAccount
			}
			return imp, nil
		}
	}
	imp := store.Import{UserID: user.ID, Kind: store.ImportUSB, Source: store.SourceUSB, Name: w.Name, StartedBy: startedBy,
		Status: store.ImportQueued}
	if known {
		imp.DeviceID = d.ID
		if d.Nickname != "" {
			imp.Name = d.Nickname
		}
	}
	imp, err = r.opt.Store.CreateImport(ctx, imp)
	if err != nil {
		return store.Import{}, err
	}
	r.mu.Lock()
	r.watches[w.ID] = imp.ID
	r.usbOf[imp.ID] = w.ID
	r.mu.Unlock()
	r.enqueue(imp.ID)
	return imp, nil
}

func (r *Runner) findWatch(ctx context.Context, id string) (usbsource.Watch, error) {
	if r.opt.USB == nil || id == "" {
		return usbsource.Watch{}, ErrNoWatch
	}
	ws, err := r.opt.USB.Find(ctx)
	if err != nil {
		return usbsource.Watch{}, err
	}
	for _, w := range ws {
		if w.ID == id {
			return w, nil
		}
	}
	return usbsource.Watch{}, ErrNoWatch
}

// usbSource reads a watch for user, leaving unread the files a USB import read before and
// that are still stored for user unchanged.
func (r *Runner) usbSource(ctx context.Context, user store.User, w usbsource.Watch) (ingest.Source, func(ingest.Entry) bool, func(ingest.Entry, string)) {
	key := w.Serial
	if key == "" {
		key = w.ID
	}
	src := func(ctx context.Context, fn func(ingest.Entry) error) error {
		how, err := r.opt.USB.Read(ctx, w, func(f usbsource.File) error {
			return fn(ingest.Entry{Path: f.Path, Size: f.Size, Modified: f.Modified, Read: f.Read})
		})
		if how != w.How {
			r.log.Info("import: reading the watch through its mount", "watch", w.ID, "how", how)
		}
		return err
	}
	skip := func(e ingest.Entry) bool {
		seen, err := r.opt.Store.USBSeenFile(ctx, key, e.Path)
		if err != nil || seen.Size != e.Size || e.Modified.IsZero() || seen.Modified != e.Modified.Unix() {
			return false
		}
		f, err := r.opt.Store.RawFileBySHA(ctx, seen.SHA256)
		return err == nil && f.UserID == user.ID
	}
	stored := func(e ingest.Entry, sha string) {
		_ = r.opt.Store.SetUSBSeen(ctx, key, e.Path, store.USBSeen{Size: e.Size, Modified: e.Modified.Unix(), SHA256: sha})
	}
	return src, skip, stored
}

// learn records which watch reports this USB serial: the one that made most of the files read.
func (r *Runner) learn(ctx context.Context, imp store.Import, w usbsource.Watch, rep ingest.Report) {
	var best int64
	for dev, n := range rep.Watches {
		if dev != 0 && (best == 0 || n > rep.Watches[best]) {
			best = dev
		}
	}
	if best == 0 {
		return
	}
	if imp.DeviceID == 0 {
		_ = r.opt.Store.SetImportDevice(ctx, imp.ID, best)
	}
	if w.Serial != "" {
		if d, err := r.opt.Store.DeviceByUSBSerial(ctx, w.Serial); err != nil || d.ID != best {
			_ = r.opt.Store.SetUSBSerial(ctx, best, w.Serial)
		}
	}
}

// watchUSB looks for watches plugged in and imports each new one for its owner, if its owner
// wants that (usb_auto). A watch goarmin does not know yet is left for someone to import.
func (r *Runner) watchUSB(ctx context.Context) {
	t := time.NewTicker(r.opt.Every)
	defer t.Stop()
	for {
		r.look(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (r *Runner) look(ctx context.Context) {
	ws, err := r.opt.USB.Find(ctx)
	if err != nil {
		r.log.Debug("usb: looking for watches", "err", err)
		return
	}
	now := map[string]bool{}
	for _, w := range ws {
		now[w.ID] = true
		r.mu.Lock()
		seen := r.present[w.ID]
		r.mu.Unlock()
		if seen {
			continue
		}
		d, known := r.resolve(ctx, w)
		r.log.Info("usb: watch plugged in", "watch", w.ID, "name", w.Name, "how", w.How, "known", known)
		if !known || !d.USBAuto {
			continue
		}
		owner, err := r.opt.Store.Owner(ctx, d.ID, time.Now().Unix())
		if err != nil {
			continue
		}
		u, err := r.opt.Store.User(ctx, owner)
		if err != nil {
			continue
		}
		if imp, err := r.ImportUSB(ctx, u, w.ID, "plugged"); err != nil {
			r.log.Warn("usb: importing the watch", "watch", w.ID, "err", err)
		} else {
			r.log.Info("usb: importing the watch for its owner", "watch", w.ID, "import", imp.ID)
		}
	}
	r.mu.Lock()
	r.present = now
	r.mu.Unlock()
}
