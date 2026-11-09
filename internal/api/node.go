// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/karamble/goarmin/internal/ingest"
	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/nodeproto"
	"github.com/karamble/goarmin/internal/nodes"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/auth"
)

// enrollTTL is how long a node's approval code stays valid.
const enrollTTL = 10 * time.Minute

// syncInterval is the time between a node's scheduled syncs, as the server tells it.
const syncInterval = 30 * time.Minute

// routesNode registers the API sync nodes speak (nodeproto) and the dashboard's Sync nodes routes.
func (s *Server) routesNode(mux *http.ServeMux) {
	if s.opt.Nodes == nil {
		return
	}
	p := nodeproto.Prefix
	mux.HandleFunc("POST "+p+"/login", s.nodeLogin)
	mux.HandleFunc("POST "+p+"/enroll", s.nodeEnroll)
	mux.HandleFunc("GET "+p+"/enroll/{poll}", s.nodeEnrollPoll)
	mux.HandleFunc("POST "+p+"/hello", s.needNode(s.nodeHello))
	mux.HandleFunc("PUT "+p+"/files/{sha256}", s.needNode(s.nodeUpload))
	mux.HandleFunc("POST "+p+"/stored", s.needNode(s.nodeStored))
	mux.HandleFunc("POST "+p+"/runs", s.needNode(s.nodeRun))
	mux.HandleFunc("POST "+p+"/inventory", s.needNode(s.nodeInventory))
	mux.HandleFunc("GET "+p+"/history", s.needNode(s.nodeHistory))
	mux.HandleFunc("POST "+p+"/progress", s.needNode(s.nodeProgress))
	mux.HandleFunc("GET "+p+"/commands", s.needNode(s.nodeCommands))
	mux.HandleFunc("POST "+p+"/commands/{id}", s.needNode(s.nodeResult))
	mux.HandleFunc("POST "+p+"/live", s.needNode(s.nodeLive))
	mux.HandleFunc("POST "+p+"/pairing", s.needNode(s.nodePairView))
	mux.HandleFunc("POST "+p+"/pairing/owners", s.needNode(s.nodePairOwners))
	mux.HandleFunc("POST "+p+"/pairing/identified", s.needNode(s.nodePairIdentified))
	mux.HandleFunc("POST "+p+"/pairing/claim", s.needNode(s.nodePairClaim))
	mux.HandleFunc(p+"/", func(w http.ResponseWriter, r *http.Request) { writeError(w, http.StatusNotFound, "no such node route") })

	mux.HandleFunc("GET /api/nodes", s.needUser(s.listNodes))
	mux.HandleFunc("PATCH /api/nodes/{id}", s.needUser(s.renameNode))
	mux.HandleFunc("DELETE /api/nodes/{id}", s.needUser(s.revokeNode))
	mux.HandleFunc("POST /api/nodes/approve", s.needUser(s.approveNode))
}

type nodeCtx struct {
	node store.Node
	user store.User
}

// info is the node as the registry knows it.
func (n nodeCtx) info() nodes.NodeInfo {
	return nodes.NodeInfo{ID: n.node.ID, Name: n.node.Name, User: n.user.ID, Admin: n.user.IsAdmin}
}

// needNode resolves the Bearer token to a live node and its (enabled) account.
func (s *Server) needNode(h func(http.ResponseWriter, *http.Request, nodeCtx)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || tok == "" || len(tok) > 200 {
			writeError(w, http.StatusUnauthorized, "sign the node in first (goarmin node login)")
			return
		}
		n, u, err := s.st.NodeByToken(r.Context(), auth.HashToken(tok))
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusUnauthorized, "this node's sign-in was revoked or is unknown (goarmin node login)")
			return
		}
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		h(w, r, nodeCtx{node: n, user: u})
	}
}

// nodeLogin signs a node in with the account's e-mail and password, like the sign-in page (and
// its limits), and hands out the node's token once.
func (s *Server) nodeLogin(w http.ResponseWriter, r *http.Request) {
	var req nodeproto.LoginRequest
	if !decode(w, r, &req) {
		return
	}
	ip := clientIP(r)
	key := ip + "|" + trimEmail(req.Email)
	if s.login.Blocked(key) || s.ipOnly.Blocked(ip) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	l, err := s.st.LoginByEmail(r.Context(), req.Email)
	switch {
	case errors.Is(err, store.ErrNotFound):
		auth.CheckUnknown(req.Password)
	case err != nil:
		s.internalError(w, r, err)
		return
	}
	ok := false
	if err == nil && l.PasswordHash != "" && !l.Disabled {
		ok, _ = auth.CheckPassword(req.Password, l.PasswordHash)
	} else if err == nil {
		auth.CheckUnknown(req.Password)
	}
	if !ok {
		s.login.Fail(key)
		s.ipOnly.Fail(ip)
		writeError(w, http.StatusUnauthorized, "wrong email or password")
		return
	}
	s.login.Reset(key)
	token, hash := auth.NewToken()
	n, err := s.st.CreateNode(r.Context(), l.User.ID, nodeName(req.Name, req.Hostname), req.Hostname, hash)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.Info("node: signed in", "node", n.ID, "user", l.User.ID)
	writeJSON(w, http.StatusCreated, nodeproto.LoginResponse{NodeID: n.ID, Token: token, Account: l.User.Email})
}

func nodeName(name, host string) string {
	if n := strings.TrimSpace(name); n != "" {
		return n
	}
	if h := strings.TrimSpace(host); h != "" {
		return h
	}
	return "Sync node"
}

// enrollAlphabet leaves out letters and digits people mix up (0/O, 1/I/L).
const enrollAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func enrollCode() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	out := make([]byte, 0, 9)
	for i, c := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, enrollAlphabet[int(c)%len(enrollAlphabet)])
	}
	return string(out)
}

// nodeEnroll starts a code approval for a node whose account signs in without a password.
func (s *Server) nodeEnroll(w http.ResponseWriter, r *http.Request) {
	var req nodeproto.EnrollRequest
	if !decode(w, r, &req) {
		return
	}
	ip := clientIP(r)
	if s.ipOnly.Blocked(ip) {
		writeError(w, http.StatusTooManyRequests, "too many attempts, try again later")
		return
	}
	s.ipOnly.Fail(ip) // each code counts: nobody may flood the table
	poll, pollHash := auth.NewToken()
	code := enrollCode()
	if err := s.st.CreateEnrollment(r.Context(), code, pollHash, nodeName(req.Name, req.Hostname), req.Hostname, enrollTTL); err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, nodeproto.EnrollResponse{Code: code, Poll: poll, Expires: time.Now().Add(enrollTTL)})
}

// nodeEnrollPoll hands the node its token once its code was approved (202 while it waits).
func (s *Server) nodeEnrollPoll(w http.ResponseWriter, r *http.Request) {
	token, err := s.st.PollEnrollment(r.Context(), auth.HashToken(r.PathValue("poll")))
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "the code expired; start again")
	case err != nil:
		s.internalError(w, r, err)
	case token == "":
		writeJSON(w, http.StatusAccepted, nodeproto.PollResponse{})
	default:
		writeJSON(w, http.StatusOK, nodeproto.PollResponse{Token: token})
	}
}

// approveNode approves a node's code for the signed-in account (Devices › Sync nodes).
func (s *Server) approveNode(w http.ResponseWriter, r *http.Request, u store.User) {
	var req struct{ Code string }
	if !decode(w, r, &req) {
		return
	}
	code := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(req.Code), " ", ""))
	if len(code) == 8 {
		code = code[:4] + "-" + code[4:]
	}
	token, hash := auth.NewToken()
	n, err := s.st.ApproveEnrollment(r.Context(), code, u.ID, token, hash)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such code, or it expired")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.log.Info("node: approved by code", "node", n.ID, "user", u.ID)
	writeJSON(w, http.StatusOK, s.nodeJSON(n))
}

// mayRun reports whether the node may sync a watch its owner is user (now): the node's own
// account's watches, every watch for an admin's node.
func mayRun(nc nodeCtx, owner int64) bool {
	return owner != 0 && (owner == nc.user.ID || nc.user.IsAdmin)
}

// nodeHello records the node's watches and answers with how each is synced.
func (s *Server) nodeHello(w http.ResponseWriter, r *http.Request, nc nodeCtx) {
	var req nodeproto.HelloRequest
	if !decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	if err := s.st.TouchNode(ctx, nc.node.ID, req.Version, req.Hostname); err != nil {
		s.internalError(w, r, err)
		return
	}
	resp := nodeproto.HelloResponse{NodeID: nc.node.ID, Name: nc.node.Name, Account: nc.user.Email, Interval: int(syncInterval / time.Second)}
	var known []nodeproto.Watch
	for _, wt := range req.Watches {
		dev, err := s.st.EnsureDevice(ctx, store.Device{UnitID: wt.Unit, Product: wt.Product, Name: wt.Name,
			Firmware: wt.Firmware, Address: wt.Address})
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		cfg := nodeproto.WatchConfig{Address: wt.Address, Unit: wt.Unit, DeviceID: dev.ID, AfterFetch: dev.AfterFetch, Paused: dev.Paused}
		owner, oerr := nodehub.OwnerNow(ctx, s.st, dev)
		switch {
		case oerr != nil:
			cfg.Reason = nodeproto.ReasonNoOwner
		case !mayRun(nc, owner):
			cfg.Reason = nodeproto.ReasonOther
		default:
			cfg.Sync, cfg.Owner = true, owner
			if loc := nodehub.OwnerZone(ctx, s.st, owner); loc != nil {
				cfg.Zone = loc.String()
			}
			known = append(known, wt)
			if err := s.st.SetDeviceNode(ctx, dev.ID, nc.node.ID); err != nil {
				s.internalError(w, r, err)
				return
			}
		}
		resp.Watches = append(resp.Watches, cfg)
	}
	// Only the watches the node may sync are routed to it: another account's watch stays with
	// whatever syncs it for that account.
	req.Watches = known
	s.opt.Nodes.Hello(nc.info(), req)
	writeJSON(w, http.StatusOK, resp)
}

// nodeDevice finds the watch of a request's unit number that the node may sync now.
func (s *Server) nodeDevice(w http.ResponseWriter, r *http.Request, nc nodeCtx, unit uint32) (store.Device, bool) {
	dev, err := s.st.DeviceByUnit(r.Context(), unit)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "no such watch here")
		return store.Device{}, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return store.Device{}, false
	}
	owner, err := nodehub.OwnerNow(r.Context(), s.st, dev)
	if err != nil || !mayRun(nc, owner) {
		writeError(w, http.StatusForbidden, "this node may not sync that watch")
		return store.Device{}, false
	}
	return dev, true
}

// nodeUpload takes one file a node pulled: checked against its SHA-256, stored and ingested for
// the account that owned the watch when it was recorded.
func (s *Server) nodeUpload(w http.ResponseWriter, r *http.Request, nc nodeCtx) {
	want := strings.ToLower(r.PathValue("sha256"))
	if len(want) != 64 {
		writeError(w, http.StatusBadRequest, "the path needs the file's SHA-256")
		return
	}
	unit, err := strconv.ParseUint(r.Header.Get(nodeproto.HeaderUnit), 10, 32)
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing "+nodeproto.HeaderUnit)
		return
	}
	dev, ok := s.nodeDevice(w, r, nc, uint32(unit))
	if !ok {
		return
	}
	name := r.Header.Get(nodeproto.HeaderName)
	if name == "" || strings.ContainsAny(name, `/\`) || len(name) > 120 {
		writeError(w, http.StatusBadRequest, "missing or bad "+nodeproto.HeaderName)
		return
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, ingest.MaxFileSize))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "the file is too large or arrived cut short")
		return
	}
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != want {
		writeError(w, http.StatusConflict, "the file does not match its SHA-256")
		return
	}
	in := &ingest.Ingester{Store: s.st, Blobs: s.blobs, Log: s.log}
	rep, err := in.ImportWatchFile(context.WithoutCancel(r.Context()), dev, store.SourceBLE, name, b)
	switch {
	case errors.Is(err, ingest.ErrWaiting):
		writeError(w, http.StatusConflict, "nobody owned the watch when this file was recorded; it waits on the node")
		return
	case err != nil:
		s.log.Warn("node: upload", "node", nc.node.ID, "err", err)
		writeError(w, http.StatusUnprocessableEntity, "the file could not be stored: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, nodeproto.UploadResponse{SHA256: want, New: rep.New > 0})
}

// nodeStored tells which of a watch's items the raw store holds.
func (s *Server) nodeStored(w http.ResponseWriter, r *http.Request, nc nodeCtx) {
	var req nodeproto.StoredRequest
	if !decode(w, r, &req) {
		return
	}
	dev, ok := s.nodeDevice(w, r, nc, req.Unit)
	if !ok {
		return
	}
	out := nodeproto.StoredResponse{Stored: []string{}}
	for _, uid := range req.UIDs {
		if has, err := s.st.HasDeviceFile(r.Context(), dev.ID, uid+".fit"); err == nil && has {
			out.Stored = append(out.Stored, uid)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// nodeRun records a node's finished sync, with the battery and delete support it saw.
func (s *Server) nodeRun(w http.ResponseWriter, r *http.Request, nc nodeCtx) {
	var req nodeproto.RunRequest
	if !decode(w, r, &req) {
		return
	}
	dev, err := s.st.DeviceByUnit(r.Context(), req.Unit)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such watch here")
		return
	}
	owner, _ := nodehub.OwnerNow(r.Context(), s.st, dev)
	if owner != 0 && !mayRun(nc, owner) {
		writeError(w, http.StatusForbidden, "this node may not sync that watch")
		return
	}
	run := req.Run
	run.ID, run.DeviceID, run.OwnerID = 0, dev.ID, owner
	ctx := r.Context()
	if req.Battery != nil && !req.BatteryAt.IsZero() {
		_ = s.st.SetBattery(ctx, dev.ID, *req.Battery, req.BatteryAt)
	}
	if req.DeleteSupport == "yes" || req.DeleteSupport == "no" {
		_ = s.st.SetDeleteSupport(ctx, dev.ID, req.DeleteSupport)
	}
	if err := s.st.RecordSyncRun(ctx, run); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// nodeInventory records what a watch kept of some types at the end of a sync (its workouts).
func (s *Server) nodeInventory(w http.ResponseWriter, r *http.Request, nc nodeCtx) {
	var req nodeproto.InventoryRequest
	if !decode(w, r, &req) {
		return
	}
	if len(req.Types) == 0 || req.At.IsZero() || len(req.Items) > 10000 {
		writeError(w, http.StatusBadRequest, "an inventory needs its types, its time and at most 10,000 items")
		return
	}
	dev, ok := s.nodeDevice(w, r, nc, req.Unit)
	if !ok {
		return
	}
	inv := store.WatchInventory{Types: req.Types, At: req.At}
	for _, it := range req.Items {
		inv.Items = append(inv.Items, store.InventoryItem{UID: it.UID, Type: it.Type, Size: it.Size})
	}
	if err := s.st.SetWatchInventory(r.Context(), dev.ID, inv); err != nil {
		s.internalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// nodeHistory returns a watch's last sync runs, for a node that starts.
func (s *Server) nodeHistory(w http.ResponseWriter, r *http.Request, nc nodeCtx) {
	unit, err := strconv.ParseUint(r.URL.Query().Get("unit"), 10, 32)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unit is a number")
		return
	}
	dev, ok := s.nodeDevice(w, r, nc, uint32(unit))
	if !ok {
		return
	}
	runs, err := s.st.RecentSyncRuns(r.Context(), dev.ID, 50)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, nodeproto.HistoryResponse{Runs: runs})
}

func (s *Server) nodeProgress(w http.ResponseWriter, r *http.Request, nc nodeCtx) {
	var req nodeproto.ProgressRequest
	if !decode(w, r, &req) {
		return
	}
	s.opt.Nodes.Progress(nc.info(), req)
	w.WriteHeader(http.StatusNoContent)
}

// nodeCommands is the node's long poll for commands.
func (s *Server) nodeCommands(w http.ResponseWriter, r *http.Request, nc nodeCtx) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(nodeproto.CommandWait + 30*time.Second))
	cmds := s.opt.Nodes.Poll(r.Context(), nc.info())
	if cmds == nil {
		cmds = []nodeproto.Command{}
	}
	writeJSON(w, http.StatusOK, nodeproto.CommandsResponse{Commands: cmds})
}

func (s *Server) nodeResult(w http.ResponseWriter, r *http.Request, nc nodeCtx) {
	var res nodeproto.CommandResult
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)) // a file list can be long
	if err := dec.Decode(&res); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	s.opt.Nodes.Result(nc.info(), r.PathValue("id"), res)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) nodeLive(w http.ResponseWriter, r *http.Request, nc nodeCtx) {
	var req nodeproto.LiveRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	s.opt.Nodes.LiveBatch(nc.info(), req)
	w.WriteHeader(http.StatusNoContent)
}

// --- Add a watch through a node: the session runs there, ownership is decided here ---

// nodePairSession finds the pairing session the node runs for owner now: the pairing routes
// answer only within one, for an account the node may sync.
func (s *Server) nodePairSession(w http.ResponseWriter, nc nodeCtx, owner int64) (nodehub.PairView, bool) {
	v, ok := s.opt.Nodes.RelayedSession(nc.node.ID)
	if !ok || v.Owner != owner || !mayRun(nc, owner) {
		writeError(w, http.StatusForbidden, "this node adds no watch for that account now")
		return nodehub.PairView{}, false
	}
	return v, true
}

// pairBackend decides ownership for a node's session in this server's database.
func (s *Server) pairBackend() nodehub.Local {
	return nodehub.Local{In: &ingest.Ingester{Store: s.st, Blobs: s.blobs, Log: s.log}}
}

// nodePairOwners says whose the watches a node's session lists are.
func (s *Server) nodePairOwners(w http.ResponseWriter, r *http.Request, nc nodeCtx) {
	var req nodeproto.PairOwnersRequest
	if !decode(w, r, &req) {
		return
	}
	if _, ok := s.nodePairSession(w, nc, req.Owner); !ok {
		return
	}
	if len(req.Watches) > 500 {
		writeError(w, http.StatusBadRequest, "too many watches at once")
		return
	}
	flags, err := s.pairBackend().OwnerFlags(r.Context(), req.Owner, req.Watches)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, nodeproto.PairOwnersResponse{Flags: flags})
}

// nodePairIdentified says whose the watch a node's session identified is.
func (s *Server) nodePairIdentified(w http.ResponseWriter, r *http.Request, nc nodeCtx) {
	var req nodeproto.PairIdentifiedRequest
	if !decode(w, r, &req) {
		return
	}
	if _, ok := s.nodePairSession(w, nc, req.Owner); !ok {
		return
	}
	who, err := s.pairBackend().Identified(r.Context(), req.Owner, req.Unit)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, who)
}

// nodePairClaim adds the watch a node's session identified to the owner's account (its Set up
// step) and routes it to the node, answering with how to sync it. 409: another account owns it.
func (s *Server) nodePairClaim(w http.ResponseWriter, r *http.Request, nc nodeCtx) {
	var c nodehub.PairClaim
	if !decode(w, r, &c) {
		return
	}
	v, ok := s.nodePairSession(w, nc, c.Owner)
	if !ok {
		return
	}
	if v.State != nodehub.PairSetup || v.Identified == nil || v.Identified.Unit != c.Record.Unit || v.Selected != c.Address {
		writeError(w, http.StatusForbidden, "that is not the watch this node is setting up")
		return
	}
	c.Nickname = strings.TrimSpace(c.Nickname)
	if utf8.RuneCountInString(c.Nickname) > 60 || strings.ContainsAny(c.Nickname, "\x00\r\n") {
		writeError(w, http.StatusBadRequest, "the name must be at most 60 characters on one line")
		return
	}
	if c.AfterFetch != store.AfterFetchKeep && c.AfterFetch != store.AfterFetchArchive {
		writeError(w, http.StatusBadRequest, "after_fetch is keep or archive")
		return
	}
	ctx := r.Context()
	id, err := s.pairBackend().Claim(ctx, c)
	if errors.Is(err, nodehub.ErrOwnedElsewhere) {
		writeError(w, http.StatusConflict, "This watch belongs to another account on this server.")
		return
	}
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if err := s.st.SetDeviceNode(ctx, id, nc.node.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	dev, err := s.st.DeviceByID(ctx, id)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	cfg := nodeproto.WatchConfig{Address: c.Address, Unit: c.Record.Unit, DeviceID: id, Sync: true, Owner: c.Owner,
		AfterFetch: dev.AfterFetch, Paused: dev.Paused}
	if loc := nodehub.OwnerZone(ctx, s.st, c.Owner); loc != nil {
		cfg.Zone = loc.String()
	}
	s.opt.Nodes.AddWatch(nc.info(), nodeproto.Watch{Address: c.Address, Unit: c.Record.Unit, Product: c.Record.Product,
		Name: c.Record.Name, Firmware: c.Record.Firmware})
	s.log.Info("node: watch added", "node", nc.node.ID, "device", id, "owner", c.Owner)
	writeJSON(w, http.StatusOK, nodeproto.PairClaimResponse{DeviceID: id, Watch: cfg})
}

// nodePairView takes the node's pairing session after a change.
func (s *Server) nodePairView(w http.ResponseWriter, r *http.Request, nc nodeCtx) {
	var req nodeproto.PairViewRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	s.opt.Nodes.PairPush(nc.info(), req.View)
	w.WriteHeader(http.StatusNoContent)
}

// --- The dashboard's Sync nodes tab ---

type nodeJSON struct {
	ID       int64             `json:"id"`
	Name     string            `json:"name"`
	Hostname string            `json:"hostname"`
	UserID   int64             `json:"user_id"`
	Created  int64             `json:"created_at"`
	LastSeen *int64            `json:"last_seen,omitempty"`
	Version  string            `json:"version"`
	Revoked  bool              `json:"revoked"`
	Online   bool              `json:"online"`
	Adapter  *nodeAdapterJSON  `json:"adapter,omitempty"`
	Watches  []nodeproto.Watch `json:"watches"`
}

type nodeAdapterJSON struct {
	Name       string      `json:"name"`
	Powered    bool        `json:"powered"`
	PowerState string      `json:"power_state"`
	Busy       *holderJSON `json:"busy,omitempty"`
}

type holderJSON struct {
	Kind  string `json:"kind"`
	Since int64  `json:"since"`
}

func (s *Server) nodeJSON(n store.Node) nodeJSON {
	j := nodeJSON{ID: n.ID, Name: n.Name, Hostname: n.Hostname, UserID: n.UserID, Created: n.CreatedAt.Unix(),
		LastSeen: unix(n.LastSeen), Version: n.Version, Revoked: n.Revoked, Watches: []nodeproto.Watch{}}
	if st, ok := s.opt.Nodes.State(n.ID); ok && !n.Revoked {
		j.Online, j.LastSeen, j.Version = st.Online, unix(st.LastSeen), st.Version
		if a := st.Adapter; a != nil {
			state := a.PowerState
			if state == "" {
				state = map[bool]string{true: "on", false: "off"}[a.Powered]
			}
			j.Adapter = &nodeAdapterJSON{Name: a.Name, Powered: a.Powered, PowerState: state}
			if st.Busy != nil {
				j.Adapter.Busy = &holderJSON{Kind: st.Busy.Kind, Since: st.Busy.Since.Unix()}
			}
		}
		j.Watches = st.Watches
	}
	return j
}

// listNodes lists the signed-in account's nodes (every node for an admin).
func (s *Server) listNodes(w http.ResponseWriter, r *http.Request, u store.User) {
	ns, err := s.st.Nodes(r.Context(), u.ID, u.IsAdmin)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := []nodeJSON{}
	for _, n := range ns {
		out = append(out, s.nodeJSON(n))
	}
	writeJSON(w, http.StatusOK, out)
}

// ownNode finds a node the user may manage: their own, any for an admin.
func (s *Server) ownNode(w http.ResponseWriter, r *http.Request, u store.User) (store.Node, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such node")
		return store.Node{}, false
	}
	n, err := s.st.NodeByID(r.Context(), id)
	if err != nil || (n.UserID != u.ID && !u.IsAdmin) {
		writeError(w, http.StatusNotFound, "no such node")
		return store.Node{}, false
	}
	return n, true
}

func (s *Server) renameNode(w http.ResponseWriter, r *http.Request, u store.User) {
	n, ok := s.ownNode(w, r, u)
	if !ok {
		return
	}
	var req struct{ Name string }
	if !decode(w, r, &req) {
		return
	}
	if name := strings.TrimSpace(req.Name); name == "" || len(name) > 60 {
		writeError(w, http.StatusBadRequest, "a name of 1 to 60 characters")
		return
	}
	if err := s.st.RenameNode(r.Context(), n.ID, req.Name); err != nil {
		s.internalError(w, r, err)
		return
	}
	n, _ = s.st.NodeByID(r.Context(), n.ID)
	writeJSON(w, http.StatusOK, s.nodeJSON(n))
}

// revokeNode ends a node's sign-in; it stops syncing for this server at its next request.
func (s *Server) revokeNode(w http.ResponseWriter, r *http.Request, u store.User) {
	n, ok := s.ownNode(w, r, u)
	if !ok {
		return
	}
	if err := s.st.RevokeNode(r.Context(), n.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, r, err)
		return
	}
	s.opt.Nodes.Drop(n.ID)
	s.log.Info("node: revoked", "node", n.ID, "by", u.ID)
	w.WriteHeader(http.StatusNoContent)
}
