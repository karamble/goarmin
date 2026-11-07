// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/karamble/goarmin/internal/ingest"
	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/product"
)

// Node is the sync role as the API sees it: nodehub.Hub with serve --with-sync.
type Node interface {
	Status(addr string) nodehub.Status
	Sync(req nodehub.Request) error
	Live(ctx context.Context, addr string) (*nodehub.Viewer, error)
	Files(unit uint32) ([]fileaccess.StoredFile, error)
	Bluetooth(ctx context.Context) (nodehub.Bluetooth, error)
	PowerOn(ctx context.Context) error
	// Known: this computer syncs the watch (paired, in node.json).
	Known(addr string) bool
	// Exclusive runs fn with Bluetooth held; ForgetLocked and PurgeSpool run inside it.
	Exclusive(ctx context.Context, kind string, fn func() error) error
	ForgetLocked(ctx context.Context, addr string) error
	PurgeSpool(unit uint32) error
}

// unix is t in Unix seconds, nil for the zero time.
func unix(t time.Time) *int64 {
	if t.IsZero() {
		return nil
	}
	v := t.Unix()
	return &v
}

// syncStatusJSON is nodehub.Status with Unix-second times, like every time in the API.
type syncStatusJSON struct {
	Address   string `json:"address"`
	Running   bool   `json:"running"`
	Live      bool   `json:"live"`
	LastStart *int64 `json:"last_start,omitempty"`
	LastEnd   *int64 `json:"last_end,omitempty"`
	LastOK    bool   `json:"last_ok"`
	LastGood  *int64 `json:"last_good,omitempty"`
	LastError string `json:"last_error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	Pulled    int    `json:"pulled"`
	Imported  int    `json:"imported"`
	NextSync  *int64 `json:"next_sync,omitempty"`
	Failures  int    `json:"failures"`
	Away      bool   `json:"away"`
	AwaySince *int64 `json:"away_since,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Listed    int    `json:"listed"`
	Done      int    `json:"done"`
	Total     int    `json:"total"`
}

func toSyncStatusJSON(st nodehub.Status) syncStatusJSON {
	return syncStatusJSON{Address: st.Address, Running: st.Running, Live: st.Live, LastStart: unix(st.LastStart),
		LastEnd: unix(st.LastEnd), LastOK: st.LastOK, LastGood: unix(st.LastGood), LastError: st.LastError, ErrorCode: st.ErrorCode, Pulled: st.Pulled,
		Imported: st.Imported, NextSync: unix(st.NextSync), Failures: st.Failures, Away: st.Away,
		AwaySince: unix(st.AwaySince), Phase: st.Phase, Listed: st.Listed, Done: st.Done, Total: st.Total}
}

// deviceJSON is a watch as d's user sees it: the sync status only while it is theirs.
func (s *Server) deviceJSON(d store.Device) map[string]any {
	m := map[string]any{"id": d.ID, "unit_id": d.UnitID, "product": d.Product, "model": product.Name(d.Product),
		"name": d.Name, "nickname": d.Nickname, "firmware": d.Firmware, "address": d.Address, "after_fetch": d.AfterFetch,
		"paused": d.Paused, "delete_support": d.DeleteSupport, "battery": nil, "battery_at": unix(d.BatteryAt),
		"current": d.Current, "owned_from": d.OwnedFrom, "owned_to": nil}
	if d.Battery >= 0 {
		m["battery"] = d.Battery
	}
	if d.OwnedTo != 0 {
		m["owned_to"] = d.OwnedTo
	}
	if s.opt.Node != nil && d.Address != "" {
		known := s.opt.Node.Known(d.Address)
		m["paired"] = known
		if known && d.Current {
			m["sync"] = toSyncStatusJSON(s.opt.Node.Status(d.Address))
		}
	}
	return m
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request, u store.User) {
	list, err := s.st.UserDevices(r.Context(), u.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, d := range list {
		out = append(out, s.deviceJSON(d))
	}
	writeJSON(w, http.StatusOK, out)
}

// userDevice finds a watch u has or had (reading); ownedDevice one that is u's now (acting on
// it). Anything else, another account's watch included, is 404.
func (s *Server) userDevice(w http.ResponseWriter, r *http.Request, u store.User) (store.Device, bool) {
	return s.findDevice(w, r, u, s.st.UserDevice)
}

func (s *Server) ownedDevice(w http.ResponseWriter, r *http.Request, u store.User) (store.Device, bool) {
	return s.findDevice(w, r, u, s.st.OwnedDevice)
}

func (s *Server) findDevice(w http.ResponseWriter, r *http.Request, u store.User,
	get func(context.Context, int64, int64) (store.Device, error)) (store.Device, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such device")
		return store.Device{}, false
	}
	d, err := get(r.Context(), u.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such device")
		return d, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return d, false
	}
	return d, true
}

// updateDevice changes what the owner may set: the nickname, the after-fetch mode (archiving
// makes the watch delete its health files once fetched, so only one host may archive a watch)
// and whether scheduled syncs are paused.
func (s *Server) updateDevice(w http.ResponseWriter, r *http.Request, u store.User) {
	d, ok := s.ownedDevice(w, r, u)
	if !ok {
		return
	}
	var req struct {
		Nickname   *string `json:"nickname"`
		AfterFetch *string `json:"after_fetch"`
		Paused     *bool   `json:"paused"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Nickname != nil {
		n := strings.TrimSpace(*req.Nickname)
		if utf8.RuneCountInString(n) > 60 || strings.ContainsAny(n, "\x00\r\n") {
			writeError(w, http.StatusBadRequest, "the name must be at most 60 characters on one line")
			return
		}
		if err := s.st.SetNickname(r.Context(), d.ID, n); err != nil {
			s.internalError(w, r, err)
			return
		}
		d.Nickname = n
	}
	if req.AfterFetch != nil {
		if err := s.st.SetAfterFetch(r.Context(), d.ID, *req.AfterFetch); err != nil {
			writeError(w, http.StatusBadRequest, "after_fetch must be keep, archive or archive_delete")
			return
		}
		d.AfterFetch = *req.AfterFetch
	}
	if req.Paused != nil {
		if err := s.st.SetPaused(r.Context(), d.ID, *req.Paused); err != nil {
			s.internalError(w, r, err)
			return
		}
		d.Paused = *req.Paused
	}
	writeJSON(w, http.StatusOK, s.deviceJSON(d))
}

// noNode answers routes that need the sync role on a server without it.
func (s *Server) noNode(w http.ResponseWriter, d store.Device) bool {
	if s.opt.Node == nil || d.Address == "" {
		writeError(w, http.StatusServiceUnavailable, "this server has no sync node for the watch (goarmin serve --with-sync)")
		return true
	}
	return false
}

// syncDevice asks for a sync now; {"include_synced": true} also fetches what another app
// already synced (older activities still on the watch). The body is optional.
func (s *Server) syncDevice(w http.ResponseWriter, r *http.Request, u store.User) {
	d, ok := s.ownedDevice(w, r, u)
	if !ok {
		return
	}
	var req struct {
		IncludeSynced bool `json:"include_synced"`
	}
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	if s.noNode(w, d) {
		return
	}
	switch err := s.opt.Node.Sync(nodehub.Request{Addr: d.Address, By: store.StartedByManual, IncludeSynced: req.IncludeSynced}); {
	case errors.Is(err, nodehub.ErrBusy):
		writeError(w, http.StatusConflict, "too many syncs are waiting; try again in a minute")
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeJSON(w, http.StatusAccepted, s.deviceJSON(d))
	}
}

// deviceSummary counts what came from the watch into the user's account.
func (s *Server) deviceSummary(w http.ResponseWriter, r *http.Request, u store.User) {
	d, ok := s.userDevice(w, r, u)
	if !ok {
		return
	}
	sum, err := s.st.DeviceSummary(r.Context(), u.ID, d.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	types := map[string]int{}
	for t, n := range sum.ByType {
		types[strconv.Itoa(t)] = n
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": sum.Files, "bytes": sum.Bytes, "by_type": types,
		"activities": sum.Activities, "nights": sum.Nights, "days": sum.Days, "first_day": sum.FirstDay,
		"last_day": sum.LastDay, "first_at": sum.FirstAt, "last_at": sum.LastAt})
}

// fitActivity is the FIT file type of a recorded activity, the only kind goarmin deletes from a watch.
const fitActivity = 4

type watchFileJSON struct {
	UID      string `json:"uid"`
	Type     string `json:"type"`     // the watch's name for it, "FIT_TYPE_4"
	FITType  int    `json:"fit_type"` // -1 when not a FIT file
	Size     int64  `json:"size"`
	Recorded int64  `json:"recorded"` // when the watch made it (0 unknown)
	Fetched  int64  `json:"fetched"`  // when goarmin finished with it (0 while pending)
	// State on the watch after goarmin: pending (kept until archiving is on), kept, archived,
	// deleted (by goarmin) or gone (the watch removed it).
	State     string `json:"state"`
	OnWatch   bool   `json:"on_watch"`
	Deletable bool   `json:"deletable"`
}

// watchFiles lists what goarmin fetched from the watch that is the user's, newest first.
func (s *Server) watchFiles(w http.ResponseWriter, r *http.Request, u store.User) {
	d, ok := s.userDevice(w, r, u)
	if !ok {
		return
	}
	out := []watchFileJSON{}
	if s.opt.Node == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	files, err := s.opt.Node.Files(d.UnitID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	mine, err := s.st.DeviceFiles(r.Context(), u.ID, d.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	canDelete := d.Current && d.Address != "" && d.DeleteSupport != "no"
	for _, f := range files {
		uid := f.UID.String()
		rf, ok := mine[uid+".fit"]
		if !ok {
			continue // another owner's, or not imported yet
		}
		fit, isFIT := fileaccess.FITType(f.TypeName)
		if !isFIT {
			fit = -1
		}
		j := watchFileJSON{UID: uid, Type: f.TypeName, FITType: fit, Size: rf.Size, Recorded: rf.TimeCreated}
		if !f.At.IsZero() {
			j.Fetched = f.At.Unix()
		}
		switch f.Outcome {
		case fileaccess.Kept:
			j.State, j.OnWatch = "kept", true
		case fileaccess.Archived:
			// The watch deletes health files once archived; activities stay.
			j.State, j.OnWatch = "archived", fit == 4
		case fileaccess.Deleted:
			j.State = "deleted"
		case fileaccess.Gone:
			j.State = "gone"
		default:
			j.State, j.OnWatch = "pending", true
		}
		// Only recordings may be deleted: the watch's own files (settings, sport profiles,
		// schedules, …) configure it.
		j.Deletable = j.OnWatch && canDelete && fit == fitActivity
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := max(out[i].Recorded, out[i].Fetched), max(out[j].Recorded, out[j].Fetched)
		return a > b || (a == b && out[i].UID > out[j].UID)
	})
	writeJSON(w, http.StatusOK, out)
}

// deleteWatchFiles deletes files from the watch (they stay in goarmin): they go with the next
// connection, which starts now.
func (s *Server) deleteWatchFiles(w http.ResponseWriter, r *http.Request, u store.User) {
	d, ok := s.ownedDevice(w, r, u)
	if !ok {
		return
	}
	var req struct {
		UIDs []string `json:"uids"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.UIDs) == 0 || len(req.UIDs) > 500 {
		writeError(w, http.StatusBadRequest, "uids must list 1 to 500 files")
		return
	}
	if s.noNode(w, d) {
		return
	}
	if d.DeleteSupport == "no" {
		writeError(w, http.StatusConflict, "this watch does not delete files on request")
		return
	}
	mine, err := s.st.DeviceFiles(r.Context(), u.ID, d.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	for _, uid := range req.UIDs {
		f, ok := mine[uid+".fit"]
		if !ok {
			writeError(w, http.StatusBadRequest, "only files goarmin has stored for you can be deleted from the watch")
			return
		}
		if f.FITType != fitActivity {
			writeError(w, http.StatusBadRequest, "only activities can be deleted from the watch; its other files configure it")
			return
		}
	}
	switch err := s.opt.Node.Sync(nodehub.Request{Addr: d.Address, By: store.StartedByDelete, Delete: req.UIDs}); {
	case errors.Is(err, nodehub.ErrBusy):
		writeError(w, http.StatusConflict, "too many syncs are waiting; try again in a minute")
	case err != nil:
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		w.WriteHeader(http.StatusAccepted)
	}
}

// syncLog pages through the user's sync log across their watches.
func (s *Server) syncLog(w http.ResponseWriter, r *http.Request, u store.User) {
	q := r.URL.Query()
	device, _ := strconv.ParseInt(q.Get("device"), 10, 64)
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 || limit > 200 {
		limit = 50
	}
	runs, err := s.st.UserSyncRuns(r.Context(), u.ID, device, q.Get("problems") == "1", before, limit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	items := []map[string]any{}
	for _, x := range runs {
		items = append(items, syncRunJSON(x))
	}
	resp := map[string]any{"items": items}
	if len(runs) == limit {
		resp["next_before"] = runs[len(runs)-1].ID
	}
	writeJSON(w, http.StatusOK, resp)
}

func syncRunJSON(x store.SyncRun) map[string]any {
	return map[string]any{"id": x.ID, "device_id": x.DeviceID, "started_by": x.StartedBy, "started_at": x.Started.Unix(),
		"finished_at": x.Finished.Unix(), "ok": x.OK, "listed": x.Listed, "failed": x.Failed, "pulled": x.Pulled,
		"archived": x.Archived, "deleted": x.Deleted, "bytes": x.Bytes, "imported": x.Imported, "error": x.Error,
		"error_code": x.ErrorCode}
}

// bluetooth reports this computer's adapter for the Devices page.
func (s *Server) bluetooth(w http.ResponseWriter, r *http.Request, _ store.User) {
	if s.opt.Node == nil {
		writeError(w, http.StatusServiceUnavailable, "this server does not sync watches (goarmin serve --with-sync)")
		return
	}
	b, err := s.opt.Node.Bluetooth(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "error": err.Error()})
		return
	}
	state := b.PowerState
	if state == "" {
		state = map[bool]string{true: "on", false: "off"}[b.Powered]
	}
	out := map[string]any{"available": true, "adapter": b.Name, "address": b.Address, "alias": b.Alias,
		"powered": b.Powered, "power_state": state, "discovering": b.Discovering, "busy": nil}
	if b.Busy != nil {
		out["busy"] = map[string]any{"kind": b.Busy.Kind, "since": b.Busy.Since.Unix()}
	}
	writeJSON(w, http.StatusOK, out)
}

// bluetoothPower switches the adapter on ({"on": true}); turning it off is not offered.
func (s *Server) bluetoothPower(w http.ResponseWriter, r *http.Request, u store.User) {
	var req struct {
		On bool `json:"on"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !req.On {
		writeError(w, http.StatusBadRequest, "goarmin only switches Bluetooth on")
		return
	}
	if s.opt.Node == nil {
		writeError(w, http.StatusServiceUnavailable, "this server does not sync watches (goarmin serve --with-sync)")
		return
	}
	if err := s.opt.Node.PowerOn(r.Context()); err != nil {
		writeError(w, http.StatusConflict, "Bluetooth could not be switched on: "+err.Error())
		return
	}
	s.bluetooth(w, r, u)
}

func (s *Server) syncRuns(w http.ResponseWriter, r *http.Request, u store.User) {
	d, ok := s.userDevice(w, r, u)
	if !ok {
		return
	}
	runs, err := s.st.SyncRuns(r.Context(), u.ID, d.ID, 50)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, x := range runs {
		out = append(out, syncRunJSON(x))
	}
	writeJSON(w, http.StatusOK, out)
}

// live streams the watch's live values as Server-Sent Events:
//
//	event: hello     data: {"since":1794…}             the link is open (or opening) since then
//	event: reading   data: {"name":"heart_rate","value":76,"unit":"bpm","t":1794…}
//	event: end       data: {"reason":"error","error_code":"not_in_range","error":"…"}
//
// The first readings are the link's last ten minutes, for a viewer who joins later. A busy
// watch is 409 with what holds Bluetooth.
func (s *Server) live(w http.ResponseWriter, r *http.Request, u store.User) {
	d, ok := s.ownedDevice(w, r, u)
	if !ok {
		return
	}
	if s.opt.Node == nil || d.Address == "" {
		writeError(w, http.StatusServiceUnavailable, "this server has no sync node for the watch (goarmin serve --with-sync)")
		return
	}
	v, err := s.opt.Node.Live(r.Context(), d.Address)
	if errors.Is(err, nodehub.ErrBusy) {
		writeBusy(w, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	defer v.Stop()
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // a stream outlives the server's write timeout
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	hello, _ := json.Marshal(map[string]any{"since": v.Since.Unix()})
	fmt.Fprintf(w, "event: hello\ndata: %s\n\n", hello)
	_ = rc.Flush()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		case rd, ok := <-v.C:
			if !ok {
				b, _ := json.Marshal(liveEnd(v.Err()))
				fmt.Fprintf(w, "event: end\ndata: %s\n\n", b)
				_ = rc.Flush()
				return
			}
			if rd.Name == "raw" {
				continue
			}
			b, _ := json.Marshal(map[string]any{"name": rd.Name, "value": rd.Value, "unit": rd.Unit, "t": rd.At.Unix()})
			if _, err := fmt.Fprintf(w, "event: reading\ndata: %s\n\n", b); err != nil {
				return
			}
		}
		_ = rc.Flush()
	}
}

// liveEnd says why a live link ended: left (its viewers left), time_limit, stopping, closed
// (by the watch) or error with the error code (not_in_range, not_bonded, …).
func liveEnd(err error) map[string]any {
	switch {
	case err == nil:
		return map[string]any{"reason": "left"}
	case errors.Is(err, nodehub.ErrTimeLimit):
		return map[string]any{"reason": "time_limit"}
	case errors.Is(err, nodehub.ErrStopping):
		return map[string]any{"reason": "stopping"}
	case errors.Is(err, nodehub.ErrLinkClosed):
		return map[string]any{"reason": "closed"}
	}
	return map[string]any{"reason": "error", "error_code": syncnode.ErrorCode(err), "error": err.Error()}
}

// busyText is the sentence for who holds Bluetooth.
var busyText = map[string]string{
	"sync":    "The watch is syncing; try again in a minute.",
	"pairing": "A watch is being added; try again when that is done.",
	"live":    "Another watch is live right now; Bluetooth serves one at a time.",
	"command": "A goarmin command on this computer is using Bluetooth; try again in a minute.",
}

// writeBusy answers 409 with what holds Bluetooth: {error, busy: {kind, since}}.
func writeBusy(w http.ResponseWriter, err error) {
	var b *nodehub.BusyError
	kind, since := "", int64(0)
	if errors.As(err, &b) {
		kind = b.Holder.Kind
		if !b.Holder.Since.IsZero() {
			since = b.Holder.Since.Unix()
		}
	}
	text, ok := busyText[kind]
	if !ok {
		text = "The watch is busy; try again in a minute."
	}
	writeJSON(w, http.StatusConflict, map[string]any{"error": text, "busy": map[string]any{"kind": kind, "since": since}})
}

// exclusive runs fn with Bluetooth held when this server syncs watches (so no sync sees the
// change half done), else at once.
func (s *Server) exclusive(r *http.Request, kind string, fn func() error) error {
	if s.opt.Node == nil {
		return fn()
	}
	return s.opt.Node.Exclusive(r.Context(), kind, fn)
}

// unpairDevice removes this computer's pairing with the watch. The watch stays the user's, with
// all its data; adding it again (Add a watch) pairs it anew.
func (s *Server) unpairDevice(w http.ResponseWriter, r *http.Request, u store.User) {
	d, ok := s.ownedDevice(w, r, u)
	if !ok {
		return
	}
	if s.noNode(w, d) {
		return
	}
	if err := s.opt.Node.Exclusive(r.Context(), "unpair", func() error { return s.opt.Node.ForgetLocked(r.Context(), d.Address) }); err != nil {
		writeError(w, http.StatusConflict, "the watch could not be unpaired: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.deviceJSON(d))
}

// releaseDevice hands the watch over: the user's ownership ends now (their data stays theirs),
// the spool's fetched files are removed, and with {"unpair": true} the pairing goes too. The next
// account that adds the watch owns it from then on.
func (s *Server) releaseDevice(w http.ResponseWriter, r *http.Request, u store.User) {
	d, ok := s.ownedDevice(w, r, u)
	if !ok {
		return
	}
	var req struct {
		Unpair bool `json:"unpair"`
	}
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	if err := s.release(r, d, u.ID, req.Unpair); err != nil {
		s.internalError(w, r, err)
		return
	}
	d, _ = s.st.UserDevice(r.Context(), u.ID, d.ID)
	writeJSON(w, http.StatusOK, s.deviceJSON(d))
}

func (s *Server) release(r *http.Request, d store.Device, owner int64, unpair bool) error {
	return s.exclusive(r, "release", func() error {
		if err := s.st.ReleaseDevice(r.Context(), d.ID, owner); err != nil {
			return err
		}
		if s.opt.Node == nil {
			return nil
		}
		if err := s.opt.Node.PurgeSpool(d.UnitID); err != nil {
			return err
		}
		if unpair && d.Address != "" {
			return s.opt.Node.ForgetLocked(r.Context(), d.Address)
		}
		return nil
	})
}

// deleteDeviceData deletes everything that came from the watch into the user's account. The
// body must name the watch (its nickname, model or Bluetooth name) as typed confirmation.
func (s *Server) deleteDeviceData(w http.ResponseWriter, r *http.Request, u store.User) {
	d, ok := s.userDevice(w, r, u)
	if !ok {
		return
	}
	var req struct {
		Confirm string `json:"confirm"`
	}
	if !decode(w, r, &req) {
		return
	}
	typed := strings.ToLower(strings.TrimSpace(req.Confirm))
	match := false
	for _, n := range []string{d.Nickname, product.Name(d.Product), d.Name} {
		if n != "" && strings.ToLower(n) == typed {
			match = true
		}
	}
	if !match {
		writeError(w, http.StatusBadRequest, "type the watch's name to confirm")
		return
	}
	in := &ingest.Ingester{Store: s.st, Blobs: s.blobs, Log: s.log}
	var gone store.DeviceDataDeleted
	var had store.DeviceSummary // the days with data, as the page showed them (not the days rebuilt)
	err := s.exclusive(r, "delete", func() error {
		var err error
		if had, err = s.st.DeviceSummary(r.Context(), u.ID, d.ID); err != nil {
			return err
		}
		if gone, err = in.DeleteDeviceData(r.Context(), u.ID, d.ID); err != nil {
			return err
		}
		if s.opt.Node != nil && d.Current {
			return s.opt.Node.PurgeSpool(d.UnitID)
		}
		return nil
	})
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.Info("deleted a watch's data", "user", u.ID, "device", d.ID, "files", gone.Files)
	writeJSON(w, http.StatusOK, map[string]any{"files": gone.Files, "activities": gone.Activities, "nights": gone.Nights,
		"days": had.Days, "syncs": gone.Runs})
}

// rawZip downloads the user's original files from the watch as one zip.
func (s *Server) rawZip(w http.ResponseWriter, r *http.Request, u store.User) {
	d, ok := s.userDevice(w, r, u)
	if !ok {
		return
	}
	files, err := s.st.RawFilesOf(r.Context(), d.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	name := strings.NewReplacer(" ", "-", "/", "-").Replace(strings.ToLower(product.Name(d.Product)))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="goarmin-`+name+`-`+time.Now().Format("20060102")+`.zip"`)
	zw := zip.NewWriter(w)
	defer zw.Close()
	for _, f := range files {
		if f.UserID != u.ID {
			continue
		}
		dir, at := "other", f.ReceivedAt
		if f.FIT != nil {
			dir = "FIT_TYPE_" + strconv.Itoa(int(f.FIT.Type))
			if f.FIT.TimeCreated != 0 {
				at = time.Unix(f.FIT.TimeCreated, 0)
			}
		}
		rd, err := s.blobs.Open(f.SHA256)
		if err != nil {
			continue // the zip carries what is there
		}
		zf, err := zw.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("%s/%s-%d.fit", dir, at.UTC().Format("20060102-150405"), f.ID),
			Method: zip.Deflate, Modified: at})
		if err == nil {
			_, err = io.Copy(zf, rd)
		}
		rd.Close()
		if err != nil {
			return
		}
	}
}

// adminDevices lists every watch on the server with its owner: identity, never health data.
func (s *Server) adminDevices(w http.ResponseWriter, r *http.Request, _ store.User) {
	list, err := s.st.AllDevices(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []map[string]any{}
	for _, x := range list {
		m := map[string]any{"id": x.ID, "unit_id": x.UnitID, "model": product.Name(x.Product), "name": x.Name,
			"address": x.Address, "owner": nil, "paired": false}
		if x.OwnerID != 0 {
			m["owner"] = map[string]any{"id": x.OwnerID, "email": x.OwnerEmail, "name": x.OwnerName}
		}
		if s.opt.Node != nil && x.Address != "" {
			m["paired"] = s.opt.Node.Known(x.Address)
			if st := s.opt.Node.Status(x.Address); !st.LastStart.IsZero() {
				m["last_end"], m["last_ok"], m["away"] = unix(st.LastEnd), st.LastOK, st.Away
			}
		}
		out = append(out, m)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) adminDevice(w http.ResponseWriter, r *http.Request) (store.Device, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such device")
		return store.Device{}, false
	}
	d, err := s.st.DeviceByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such device")
		return d, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return d, false
	}
	return d, true
}

// adminRelease ends whoever's ownership of the watch (a household member who left).
func (s *Server) adminRelease(w http.ResponseWriter, r *http.Request, admin store.User) {
	d, ok := s.adminDevice(w, r)
	if !ok {
		return
	}
	owner, err := s.st.Owner(r.Context(), d.ID, time.Now().Unix())
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusConflict, "nobody owns this watch")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.release(r, d, owner, false); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.Info("admin released a watch", "admin", admin.ID, "device", d.ID, "owner", owner)
	w.WriteHeader(http.StatusNoContent)
}

// adminForget removes this computer's pairing with the watch.
func (s *Server) adminForget(w http.ResponseWriter, r *http.Request, admin store.User) {
	d, ok := s.adminDevice(w, r)
	if !ok {
		return
	}
	if s.noNode(w, d) {
		return
	}
	if err := s.opt.Node.Exclusive(r.Context(), "unpair", func() error { return s.opt.Node.ForgetLocked(r.Context(), d.Address) }); err != nil {
		writeError(w, http.StatusConflict, "the watch could not be unpaired: "+err.Error())
		return
	}
	s.log.Info("admin unpaired a watch", "admin", admin.ID, "device", d.ID)
	w.WriteHeader(http.StatusNoContent)
}
