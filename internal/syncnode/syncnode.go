// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// Package syncnode is goarmin's sync role: it connects to the bonded watches near this
// computer, runs a FileAccess sync and keeps the pulled files in a local spool until the server
// has them. M2 runs one sync on demand; scheduling and the upload to the server come in M7.
//
// Layout under the data directory:
//
//	node.json                  what worked per watch (characteristic pair, client ID) and its identity
//	watches/<unit number>/     fileaccess.DirStore: state.json and files/
package syncnode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/karamble/goarmin/pkg/bluez"
	"github.com/karamble/goarmin/pkg/fitid"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/gattlink"
	"github.com/karamble/goarmin/pkg/garmin/gfdi"
	"github.com/karamble/goarmin/pkg/garmin/mltrace"
	"github.com/karamble/goarmin/pkg/garmin/multilink"
	"github.com/karamble/goarmin/pkg/garmin/smart"
	"github.com/karamble/goarmin/pkg/garmin/watch"
)

// Options configures one sync.
type Options struct {
	DataDir string
	Adapter string
	// Device is the watch's Bluetooth address (AA:BB:CC:DD:EE:FF). Empty picks the only
	// watch node.json knows.
	Device        string
	NoArchive     bool
	IncludeSynced bool
	FullListing   bool
	PlainGFDI     bool
	// DeleteActivities deletes archived activities from the watch too (the watch keeps them
	// after the archive; health files it removes on its own). It has no effect with NoArchive.
	DeleteActivities bool
	// Delete lists items (fileaccess UID strings) to delete from the watch after the sync,
	// also with NoArchive. Each must be stored safely first: in the spool as a valid file, or as
	// IsStored confirms.
	Delete []string
	// IsStored, if set, confirms that an item is stored durably elsewhere (the server's raw
	// store, once the spool copy is gone); otherwise the spool must hold it.
	IsStored func(uid string) bool
	// Settle is how long a sync waits for the watch to announce new files after its listing
	// (5 s for SyncOnce, none for SyncWith unless set).
	Settle time.Duration
	// Location answers the watch's time requests (time.Local by default).
	Location *time.Location
	// Trace, if set, receives every Multi-Link packet and GFDI frame (pkg/garmin/mltrace).
	Trace  io.Writer
	Logger *slog.Logger
	// OnEvent sees every item as the sync progresses.
	OnEvent func(fileaccess.Event)
	// Features announces the host's feature capabilities as Garmin Connect does
	// (smart.GarminConnectFeatures: wellness threshold notifications, file access, device
	// status) right after the handshake's CONFIGURATION.
	Features bool
	// Hand, if set, gives each pulled file to the server right after the spool has it (a sync
	// node). The watch flags a file as synced only once Hand returned nil for it, and its spool
	// copy is removed then. A file Hand could not give stays pending, in the spool and on the
	// watch; a later sync gives it again before it flags it. Without Hand, a file is flagged as
	// soon as the spool has it on disk, and the caller imports the spool afterwards.
	Hand func(ctx context.Context, f fileaccess.StoredFile) error
	// Inventory, if set, takes stock of these GDXML types at the end of a sync: every item of
	// them the watch keeps, synced or not (Result.Inventory). It is read-only.
	Inventory []string
	// Send lists files to push to the watch after the sync's pulls and deletes, before it takes
	// stock (Result.Sent), so the inventory lists what was just sent.
	Send []Outgoing
}

// Outgoing is a file a sync pushes to the watch (Options.Send).
type Outgoing struct {
	Key  string // the caller's name for it, echoed in its outcome
	Type string // its GDXML type name: "FIT_TYPE_5" (a workout), "FIT_TYPE_7" (a schedule)
	Data []byte
	// Announce waits for the watch to name the new item: a workout. (A schedule is not
	// announced.)
	Announce bool
}

// SendOutcome is what became of one Outgoing.
type SendOutcome struct {
	Key    string
	SHA256 string // of the bytes sent
	UID    string // the watch's id for the new item ("" if it named none)
	Err    error
}

// Refused reports whether the watch said no to the push (rather than the link failing).
func (o SendOutcome) Refused() bool {
	var pe *fileaccess.PushError
	return errors.As(o.Err, &pe)
}

// Unsupported reports whether the watch refused because it takes no pushes of that kind.
func (o SendOutcome) Unsupported() bool {
	var pe *fileaccess.PushError
	return errors.As(o.Err, &pe) && (pe.Result == smart.ItemAccessItemTypeNotSupported || pe.Result == smart.ItemAccessNoSupportedTransport)
}

// InventoryTypes are what a sync takes stock of: the watch's workouts (FIT type 5), so the
// workout library knows which are on it.
var InventoryTypes = []string{"FIT_TYPE_5"}

// Inventory is what a watch kept of some types at the end of a sync.
type Inventory struct {
	Types []string
	Items []fileaccess.Item
	At    time.Time
}

// Result describes a finished sync.
type Result struct {
	// Opened lists the realtime services the watch accepted (Live only).
	Opened    []multilink.Service
	Address   string
	Device    gfdi.DeviceInfo
	Pair      int
	Client    uint64
	Reliable  bool
	Attempts  []watch.Attempt
	Handshake time.Duration
	Report    fileaccess.Report
	SyncTime  time.Duration
	StoreDir  string
	Files     []fileaccess.StoredFile
	// Deleted holds the outcome of each Options.Delete request (nil error: deleted).
	Deleted []DeleteOutcome
	// Battery is the watch's last battery report during the session (Percent -1: none).
	Battery watch.Battery
	// Features is the watch's answer to Options.Features (nil: not asked or no answer).
	Features *smart.WatchFeatures
	// WellnessMet counts the watch's WellnessThresholdMetNotifications during the session.
	WellnessMet int
	// Inventory is what the watch kept of Options.Inventory's types after the sync (nil: not
	// asked, or the listing failed).
	Inventory *Inventory
	// Sent holds the outcome of each Options.Send push tried, in order. After a push that
	// failed without the watch refusing it (the link), the rest are not tried and have none.
	Sent []SendOutcome
}

// DeleteOutcome is the result of one requested delete.
type DeleteOutcome struct {
	UID string
	Err error
}

// noBattery is a result's battery before the watch reported one (also when the connection
// failed before the session started).
var noBattery = watch.Battery{Percent: -1}

// WatchRecord is what node.json remembers about a watch.
type WatchRecord struct {
	Unit     uint32 `json:"unit"`
	Product  uint16 `json:"product"`
	Name     string `json:"name"`
	Firmware string `json:"firmware"`
	Pair     int    `json:"pair"`
	Client   uint64 `json:"client"`
	LastSync int64  `json:"last_sync,omitempty"` // Unix seconds
}

type nodeFile struct {
	Watches map[string]WatchRecord `json:"watches"` // by Bluetooth address
}

func loadNode(path string) (nodeFile, error) {
	n := nodeFile{Watches: map[string]WatchRecord{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return n, nil
	}
	if err != nil {
		return n, err
	}
	if err := json.Unmarshal(b, &n); err != nil {
		return n, fmt.Errorf("syncnode: %s: %w", path, err)
	}
	if n.Watches == nil {
		n.Watches = map[string]WatchRecord{}
	}
	return n, nil
}

func saveNode(path string, n nodeFile) error {
	b, err := json.MarshalIndent(n, "", " ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// updateNode changes node.json under <dataDir>/node.lock: it loads the current file, lets fn
// change it, and writes it back when fn reports a change. Every writer goes through here, so a
// sync that ends never overwrites a watch added meanwhile (by the wizard, or another process).
func updateNode(dataDir string, fn func(n *nodeFile) bool) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "node.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("syncnode: locking node.json: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck
	path := filepath.Join(dataDir, "node.json")
	n, err := loadNode(path)
	if err != nil {
		return err
	}
	if !fn(&n) {
		return nil
	}
	return saveNode(path, n)
}

// KnownWatches returns the watches this node has synced, by Bluetooth address.
func KnownWatches(dataDir string) (map[string]WatchRecord, error) {
	n, err := loadNode(filepath.Join(dataDir, "node.json"))
	return n.Watches, err
}

// VerifyFIT rejects a pulled FIT_TYPE_* item that is not a complete, valid FIT file.
func VerifyFIT(it fileaccess.Item, data []byte) error {
	if _, ok := fileaccess.FITType(it.TypeName); !ok {
		return nil
	}
	_, err := fitid.Inspect(data)
	return err
}

// SyncOnce connects to the watch over BlueZ, syncs it once and disconnects.
func SyncOnce(ctx context.Context, opt Options) (Result, error) {
	addr, err := resolveAddress(opt)
	if err != nil {
		return Result{Address: addr, Battery: noBattery}, err
	}
	bz, err := bluez.Open()
	if err != nil {
		return Result{Address: addr, Battery: noBattery}, err
	}
	defer bz.Close()
	start := time.Now()
	gw, err := gattlink.Connect(ctx, bz, opt.Adapter, addr)
	if err != nil {
		return Result{Address: addr, Battery: noBattery}, err
	}
	defer func() {
		dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = gw.Disconnect(dctx)
	}()
	if opt.Settle == 0 {
		opt.Settle = 5 * time.Second
	}
	return syncWith(ctx, opt, addr, gw.Open, gw.Pairs, start)
}

// SyncWith syncs a watch whose characteristic pairs open is able to open: the part of
// SyncOnce after the Bluetooth connection, for other transports and for tests.
func SyncWith(ctx context.Context, opt Options, addr string, open watch.Opener, pairs []int) (Result, error) {
	return syncWith(ctx, opt, addr, open, pairs, time.Now())
}

func resolveAddress(opt Options) (string, error) {
	addr := opt.Device
	if addr == "" {
		node, err := loadNode(filepath.Join(opt.DataDir, "node.json"))
		if err != nil {
			return "", err
		}
		if len(node.Watches) != 1 {
			return "", errors.New("syncnode: say which watch (-device AA:BB:CC:DD:EE:FF)")
		}
		for a := range node.Watches {
			addr = a
		}
	}
	return bluez.NormalizeAddress(addr)
}

func syncWith(ctx context.Context, opt Options, addr string, open watch.Opener, pairs []int, start time.Time) (Result, error) {
	res := Result{Address: addr, Battery: noBattery}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if err := os.MkdirAll(opt.DataDir, 0o700); err != nil {
		return res, err
	}
	wopt := watch.Options{PlainGFDI: opt.PlainGFDI, Location: opt.Location, Logger: opt.Logger}
	if opt.Features {
		wopt.Features = &smart.GarminConnectFeatures
	}
	if opt.Trace != nil {
		tr := mltrace.NewWriter(opt.Trace, start)
		wopt.TracePacket, wopt.TraceFrame = tr.Packet, tr.Frame
	}
	d, err := dial(ctx, opt.DataDir, addr, open, pairs, wopt)
	res.Attempts = d.tries
	if err != nil {
		return res, err
	}
	conn := d.conn
	defer d.closer.Close()
	defer conn.Close()
	res.Handshake = time.Since(start)
	res.Device, res.Pair, res.Reliable, res.Client = conn.Device, d.pair, conn.GFDI.Reliable(), d.client
	res.Features = conn.Features

	// Remember what worked before syncing, so a failed sync still speeds up the next try.
	if err := Save(opt.DataDir, addr, recordOf(conn.Device, d.pair, d.client)); err != nil {
		return res, err
	}

	// The battery level, best effort: a watch that does not answer only leaves it unknown.
	bctx, bcancel := context.WithTimeout(ctx, 5*time.Second)
	if _, err := conn.RequestBattery(bctx); err != nil {
		opt.Logger.Info("battery level not reported", "err", err)
	}
	bcancel()

	res.StoreDir = filepath.Join(opt.DataDir, "watches", strconv.FormatUint(uint64(conn.Device.UnitNumber), 10))
	store, err := fileaccess.OpenDirStore(res.StoreDir)
	if err != nil {
		return res, err
	}
	fopt := fileaccess.Options{
		NoArchive: opt.NoArchive, IncludeSynced: opt.IncludeSynced, FullListing: opt.FullListing,
		Verify: VerifyFIT, Logger: opt.Logger, OnEvent: opt.OnEvent, Settle: opt.Settle,
		CutWellness: true,
	}
	if opt.DeleteActivities {
		fopt.Delete = IsActivity
	}
	var sink fileaccess.Store = store
	if opt.Hand != nil {
		h := &handing{DirStore: store, ctx: ctx, hand: opt.Hand, stored: opt.IsStored, log: opt.Logger,
			handed: map[string]bool{}, tried: map[string]bool{}}
		sink, fopt.ArchiveAllowed = h, h.allowed
	}
	eng := fileaccess.New(conn, sink, fopt)
	defer eng.Close()
	syncStart := time.Now()
	res.Report, err = eng.Sync(ctx)
	res.SyncTime = time.Since(syncStart)
	if err == nil {
		res.Deleted = deleteStored(ctx, eng, store, opt.Delete, opt.IsStored)
		res.Sent = sendAll(ctx, eng, opt.Send, opt.Logger)
		if len(opt.Inventory) > 0 {
			ictx, icancel := context.WithTimeout(ctx, 30*time.Second)
			items, ierr := eng.Inventory(ictx, opt.Inventory)
			icancel()
			if ierr != nil {
				opt.Logger.Warn("taking stock of the watch's files", "types", opt.Inventory, "err", ierr)
			} else {
				res.Inventory = &Inventory{Types: opt.Inventory, Items: items, At: time.Now()}
			}
		}
	}
	res.Battery = conn.Battery()
	res.WellnessMet = conn.WellnessThresholdMet()
	res.Files, _ = store.Files()
	if err != nil {
		return res, err
	}
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if err := conn.SystemEvent(sctx, watch.EventSyncComplete); err != nil {
		opt.Logger.Warn("SYNC_COMPLETE", "err", err)
	}
	cancel()
	synced := time.Now().Unix()
	return res, updateNode(opt.DataDir, func(n *nodeFile) bool {
		rec, ok := n.Watches[addr]
		if !ok {
			return false // forgotten during the sync: leave it forgotten
		}
		rec.LastSync = synced
		n.Watches[addr] = rec
		return true
	})
}

// dialed is a finished handshake.
type dialed struct {
	conn   *watch.Conn
	closer io.Closer
	tries  []watch.Attempt
	pair   int
	client uint64
}

// dial runs the handshake over open, trying the characteristic pair and client ID that worked
// last time (node.json) first.
func dial(ctx context.Context, dataDir, addr string, open watch.Opener, pairs []int, wopt watch.Options) (dialed, error) {
	var d dialed
	node, err := loadNode(filepath.Join(dataDir, "node.json"))
	if err != nil {
		return d, err
	}
	clients := []uint64{multilink.ClientGadgetbridge, multilink.ClientGarminConnect}
	if known, ok := node.Watches[addr]; ok {
		pairs = preferFirst(pairs, known.Pair)
		if known.Client == multilink.ClientGarminConnect {
			clients = []uint64{multilink.ClientGarminConnect, multilink.ClientGadgetbridge}
		}
	}
	opener := func(ctx context.Context, n int) (multilink.Link, io.Closer, error) {
		d.pair = n
		return open(ctx, n)
	}
	d.conn, d.closer, d.tries, err = watch.Dial(ctx, opener, pairs, clients, wopt)
	if err != nil {
		return d, err
	}
	d.client = d.tries[len(d.tries)-1].Client
	return d, nil
}

// recordOf is node.json's entry for a watch after a handshake.
func recordOf(dev gfdi.DeviceInfo, pair int, client uint64) WatchRecord {
	return WatchRecord{Unit: dev.UnitNumber, Product: dev.ProductNumber, Name: dev.BluetoothName,
		Firmware: dev.SoftwareVersionString(), Pair: pair, Client: client}
}

// Save adds or updates the watch at addr in node.json, which makes the server sync it. The
// time of its last sync is kept.
func Save(dataDir, addr string, rec WatchRecord) error {
	return updateNode(dataDir, func(n *nodeFile) bool {
		if old, ok := n.Watches[addr]; ok {
			rec.LastSync = old.LastSync
		}
		n.Watches[addr] = rec
		return true
	})
}

// handing is a spool that gives each pulled file to the server (Options.Hand) and allows the
// watch to flag only the files the server has.
type handing struct {
	*fileaccess.DirStore
	ctx    context.Context
	hand   func(context.Context, fileaccess.StoredFile) error
	stored func(uid string) bool
	log    *slog.Logger
	handed map[string]bool // given in this sync (the engine runs items one at a time)
	tried  map[string]bool // asked once in this sync: a refused file waits for the next sync
}

// Persist stores the file, then gives it to the server; a failed hand only leaves the file
// pending, it never fails the pull.
func (h *handing) Persist(ctx context.Context, it fileaccess.Item, data []byte) error {
	if err := h.DirStore.Persist(ctx, it, data); err != nil {
		return err
	}
	h.give(it.UID)
	return nil
}

// give hands the spool's file of uid to the server and removes the spool copy once it did.
func (h *handing) give(uid smart.UUID) bool {
	key := uid.String()
	if h.handed[key] {
		return true
	}
	if h.tried[key] {
		return false
	}
	h.tried[key] = true
	f, ok := h.File(uid)
	if !ok {
		return false
	}
	if _, err := os.Stat(f.Path); err != nil {
		return false
	}
	if err := h.hand(h.ctx, f); err != nil {
		h.log.Info("syncnode: the server does not have the file yet; it stays on the watch", "uid", key, "err", err)
		return false
	}
	h.handed[key] = true
	if err := os.Remove(f.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		h.log.Warn("syncnode: removing a handed file from the spool", "path", f.Path, "err", err)
	}
	return true
}

// allowed is the engine's ArchiveAllowed: the server has the file, given now or before (a
// spool copy is only removed once the server has it, which IsStored confirms).
func (h *handing) allowed(it fileaccess.Item) bool {
	if h.give(it.UID) {
		return true
	}
	if f, ok := h.File(it.UID); ok {
		if _, err := os.Stat(f.Path); err == nil {
			return false // still in the spool: the server did not take it
		}
	}
	return h.stored != nil && h.stored(it.UID.String())
}

// IsActivity picks FIT activity files (FIT_TYPE_4).
func IsActivity(it fileaccess.Item) bool {
	n, ok := fileaccess.FITType(it.TypeName)
	return ok && n == 4
}

// deleteStored deletes the listed items from the watch, each only if the spool holds it and,
// for FIT items, the stored file is still a valid FIT file.
// sendAll pushes files to the watch in order, until a push fails other than by the watch's
// refusal (the link is gone, the rest would fail too).
func sendAll(ctx context.Context, eng *fileaccess.Engine, files []Outgoing, log *slog.Logger) []SendOutcome {
	var out []SendOutcome
	for _, f := range files {
		sum := sha256.Sum256(f.Data)
		o := SendOutcome{Key: f.Key, SHA256: hex.EncodeToString(sum[:])}
		pctx, cancel := context.WithTimeout(ctx, time.Minute)
		p, err := eng.Push(pctx, f.Type, f.Data, fileaccess.PushOptions{Announce: f.Announce})
		cancel()
		o.Err = err
		if err == nil && p.Announced {
			o.UID = p.UID.String()
		}
		out = append(out, o)
		if err != nil {
			log.Warn("sending a file to the watch", "type", f.Type, "size", len(f.Data), "err", err)
			if !o.Refused() {
				break
			}
			continue
		}
		log.Info("sent a file to the watch", "type", f.Type, "size", len(f.Data), "uid", o.UID)
	}
	return out
}

func deleteStored(ctx context.Context, eng *fileaccess.Engine, store *fileaccess.DirStore, uids []string, elsewhere func(string) bool) []DeleteOutcome {
	if len(uids) == 0 {
		return nil
	}
	files, err := store.Files()
	byUID := map[string]fileaccess.StoredFile{}
	for _, f := range files {
		byUID[f.UID.String()] = f
	}
	safe := func(uid smart.UUID) bool {
		if elsewhere != nil && elsewhere(uid.String()) {
			return true
		}
		f, ok := byUID[uid.String()]
		if !ok || !store.Stored(uid) {
			return false
		}
		data, err := os.ReadFile(f.Path)
		return err == nil && VerifyFIT(f.Item, data) == nil
	}
	var out []DeleteOutcome
	for _, s := range uids {
		o := DeleteOutcome{UID: s, Err: err}
		if err == nil {
			var uid smart.UUID
			if uid, o.Err = fileaccess.ParseUID(s); o.Err == nil {
				o.Err = eng.Delete(ctx, uid, safe)
			}
		}
		out = append(out, o)
	}
	return out
}

func preferFirst(pairs []int, p int) []int {
	out := []int{}
	for _, n := range pairs {
		if n == p {
			out = append(out, n)
		}
	}
	for _, n := range pairs {
		if n != p {
			out = append(out, n)
		}
	}
	return out
}
