// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package nodeproto is what a sync node and the server say to each other over HTTP, under
// Prefix, with the node's token as "Authorization: Bearer". Both sides are goarmin, so the types
// reuse goarmin's own (times as RFC 3339, the rest as Go's JSON gives it).
package nodeproto

import (
	"time"

	"github.com/karamble/goarmin/internal/nodehub"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/realtime"
)

// Prefix is where the node API lives on the server.
const Prefix = "/api/node/v1"

// Headers of a file upload (PUT Prefix+"/files/{sha256}").
const (
	HeaderUnit = "X-Goarmin-Unit" // the watch's unit number
	HeaderName = "X-Goarmin-Name" // the file's name in the spool: <uid>.fit
)

// CommandWait is how long a command poll waits on the server before it answers empty.
const CommandWait = 25 * time.Second

// LoginRequest signs a node in with the account's password.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
}

// LoginResponse carries the node's token, shown only this once.
type LoginResponse struct {
	NodeID  int64  `json:"node_id"`
	Token   string `json:"token"`
	Account string `json:"account"`
}

// EnrollRequest starts a code approval (accounts that sign in without a password).
type EnrollRequest struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
}

// EnrollResponse is the code to approve in the dashboard and the secret to poll with.
type EnrollResponse struct {
	Code    string    `json:"code"`
	Poll    string    `json:"poll"`
	Expires time.Time `json:"expires"`
}

// PollResponse carries the token once the code was approved (202 and empty while it waits).
type PollResponse struct {
	Token string `json:"token"`
}

// Watch is a watch this node is paired with (from its node.json).
type Watch struct {
	Address  string `json:"address"`
	Unit     uint32 `json:"unit"`
	Product  uint16 `json:"product"`
	Name     string `json:"name"`
	Firmware string `json:"firmware"`
}

// HelloRequest is sent on start and every few minutes.
type HelloRequest struct {
	Version  string             `json:"version"`
	Hostname string             `json:"hostname"`
	Adapter  *nodehub.Bluetooth `json:"adapter,omitempty"`
	Watches  []Watch            `json:"watches"`
}

// WatchConfig is how the server wants a watch synced.
type WatchConfig struct {
	Address  string `json:"address"`
	Unit     uint32 `json:"unit"`
	DeviceID int64  `json:"device_id"`
	// Sync is false for a watch this node may not sync: nobody owns it now (Reason no_owner),
	// or it belongs to an account the node is not allowed to sync for (other_account).
	Sync       bool   `json:"sync"`
	Reason     string `json:"reason,omitempty"`
	Owner      int64  `json:"owner,omitempty"`
	AfterFetch string `json:"after_fetch"`
	Paused     bool   `json:"paused"`
	Zone       string `json:"zone,omitempty"` // the owner's IANA time zone
}

// Reasons a watch is not synced.
const (
	ReasonNoOwner = "no_owner"
	ReasonOther   = "other_account"
)

// HelloResponse is the server's answer: who the node is, and each watch's config.
type HelloResponse struct {
	NodeID   int64         `json:"node_id"`
	Name     string        `json:"name"`
	Account  string        `json:"account"`
	Watches  []WatchConfig `json:"watches"`
	Interval int           `json:"interval"` // seconds between scheduled syncs
}

// UploadResponse answers a file upload.
type UploadResponse struct {
	SHA256 string `json:"sha256"`
	New    bool   `json:"new"`
}

// StoredRequest asks which of a watch's items the server's raw store holds.
type StoredRequest struct {
	Unit uint32   `json:"unit"`
	UIDs []string `json:"uids"`
}

// StoredResponse lists the ones it holds.
type StoredResponse struct {
	Stored []string `json:"stored"`
}

// RunRequest reports a finished sync.
type RunRequest struct {
	Unit          uint32        `json:"unit"`
	Run           store.SyncRun `json:"run"`
	Battery       *int          `json:"battery,omitempty"`
	BatteryAt     time.Time     `json:"battery_at,omitempty"`
	DeleteSupport string        `json:"delete_support,omitempty"`
}

// InventoryRequest is what a watch kept of some GDXML types at the end of a sync (its workouts,
// syncnode.InventoryTypes), for the workout library.
type InventoryRequest struct {
	Unit  uint32          `json:"unit"`
	Types []string        `json:"types"`
	At    time.Time       `json:"at"`
	Items []InventoryItem `json:"items"`
}

// InventoryItem is one item of an InventoryRequest: the watch's item id as goarmin prints it,
// its type and size.
type InventoryItem struct {
	UID  string `json:"uid"`
	Type string `json:"type"`
	Size uint64 `json:"size"`
}

// HistoryResponse is a watch's last sync runs, newest first.
type HistoryResponse struct {
	Runs []store.SyncRun `json:"runs"`
}

// ProgressRequest is the node's state, sent every second while a sync or live link runs and
// after each change.
type ProgressRequest struct {
	Status  []nodehub.Status   `json:"status"`
	Busy    *nodehub.Holder    `json:"busy,omitempty"`
	Adapter *nodehub.Bluetooth `json:"adapter,omitempty"`
}

// Command kinds the server sends.
const (
	CmdSync      = "sync"
	CmdLiveStart = "live_start"
	CmdLiveStop  = "live_stop"
	CmdFiles     = "files"
	CmdForget    = "forget" // unpair: remove the bond and forget the watch
	CmdPurge     = "purge"  // empty the watch's spool of fetched files
	CmdPowerOn   = "power_on"
	CmdHello     = "hello" // send a hello now (the server's config changed)

	// Add a watch through the node: the session runs on the node, the server relays it.
	CmdPairStart   = "pair_start"   // Starter, Owner
	CmdPairScan    = "pair_scan"    // Session, User
	CmdPairSelect  = "pair_select"  // Session, User, Address, Repair
	CmdPairConfirm = "pair_confirm" // Session, User, Match
	CmdPairPasskey = "pair_passkey" // Session, User, Passkey
	CmdPairSetup   = "pair_setup"   // Session, User, Setup
	CmdPairCancel  = "pair_cancel"  // Session, User, Admin
	CmdPairTouch   = "pair_touch"   // Session: someone looks at it on the server
)

// Command is one thing the server asks the node to do.
type Command struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Address       string   `json:"address,omitempty"`
	Unit          uint32   `json:"unit,omitempty"`
	By            string   `json:"by,omitempty"`
	IncludeSynced bool     `json:"include_synced,omitempty"`
	Delete        []string `json:"delete,omitempty"`

	// A pairing step: the session, who acts on it (User; Admin for an admin's cancel) and the
	// step's answer.
	Session string                    `json:"session,omitempty"`
	Starter int64                     `json:"starter,omitempty"`
	Owner   int64                     `json:"owner,omitempty"`
	User    int64                     `json:"user,omitempty"`
	Admin   bool                      `json:"admin,omitempty"`
	Repair  bool                      `json:"repair,omitempty"`
	Match   bool                      `json:"match,omitempty"`
	Passkey uint32                    `json:"passkey,omitempty"`
	Setup   *nodehub.PairSetupRequest `json:"setup,omitempty"`
}

// CommandsResponse carries the commands waiting for the node (none after CommandWait).
type CommandsResponse struct {
	Commands []Command `json:"commands"`
}

// Result codes of a command.
const (
	CodeBusy    = "busy"
	CodeUnknown = "unknown"
	CodeFailed  = "failed"

	// A pairing step's errors (nodehub.Err*).
	CodeWrongState     = "wrong_state"
	CodeNoSession      = "no_session"
	CodeOwnedElsewhere = "owned_elsewhere"
	CodeNotListed      = "not_listed"
	CodePairingActive  = "pairing_active"
	CodeStopping       = "stopping"
	CodeCannotPair     = "cannot_pair"
)

// CommandResult answers a command.
type CommandResult struct {
	Error string                  `json:"error,omitempty"`
	Code  string                  `json:"code,omitempty"`
	Busy  *nodehub.Holder         `json:"busy,omitempty"`
	Files []fileaccess.StoredFile `json:"files,omitempty"`
	// Pairing is the session after a pairing step; Created says pair_start opened a new one.
	Pairing *nodehub.PairView `json:"pairing,omitempty"`
	Created bool              `json:"created,omitempty"`
}

// PairOwnersRequest asks whose the watches a pairing session lists are, seen from Owner
// (POST Prefix+"/pairing/owners").
type PairOwnersRequest struct {
	Owner   int64                   `json:"owner"`
	Watches []nodehub.PairCandidate `json:"watches"`
}

// PairOwnersResponse has nodehub.Owner* by address.
type PairOwnersResponse struct {
	Flags map[string]string `json:"flags"`
}

// PairIdentifiedRequest asks whose the identified watch is (POST Prefix+"/pairing/identified");
// the answer is a nodehub.IdentifiedOwner.
type PairIdentifiedRequest struct {
	Owner int64  `json:"owner"`
	Unit  uint32 `json:"unit"`
}

// PairClaimResponse answers a claim (POST Prefix+"/pairing/claim" with a nodehub.PairClaim): the
// device, and how to sync it, so the first sync needs no hello first.
type PairClaimResponse struct {
	DeviceID int64       `json:"device_id"`
	Watch    WatchConfig `json:"watch"`
}

// PairViewRequest carries the node's pairing session after each change (POST Prefix+"/pairing").
type PairViewRequest struct {
	View nodehub.PairView `json:"view"`
}

// LiveRequest carries a live link's readings; End, once, says why it ended.
type LiveRequest struct {
	Address  string             `json:"address"`
	Since    time.Time          `json:"since,omitempty"`
	Readings []realtime.Reading `json:"readings,omitempty"`
	End      *LiveEnd           `json:"end,omitempty"`
}

// LiveEnd is why a live link ended: Code is the error code (time_limit, stopping, …), Error the
// words; both empty when its viewers left.
type LiveEnd struct {
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
}
