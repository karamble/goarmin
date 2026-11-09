// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package nodehub

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/karamble/goarmin/internal/ingest"
	"github.com/karamble/goarmin/internal/outbox"
	"github.com/karamble/goarmin/internal/store"
	"github.com/karamble/goarmin/internal/syncnode"
	"github.com/karamble/goarmin/pkg/garmin/fileaccess"
	"github.com/karamble/goarmin/pkg/garmin/watch"
)

// Backend is where the hub keeps what it syncs: in goarmin serve -with-sync the server's own
// database (Local); on a sync node the server, over HTTP.
type Backend interface {
	// Watch readies a sync of the watch at addr: its device, whose it is now and how to sync it.
	// A watch nobody owns now comes back with NoOwner set (it is not synced).
	Watch(ctx context.Context, addr string, rec syncnode.WatchRecord) (Watch, error)
	// Stored reports whether the server's raw store holds the watch's item (its UID), so it
	// may be deleted from the watch after its spool copy is gone.
	Stored(ctx context.Context, w Watch, uid string) bool
	// Import takes the files waiting in the watch's spool directory, each for the account that
	// owned the watch when it was recorded. A file leaves the spool once the server has it. It
	// returns how many were new.
	Import(ctx context.Context, w Watch, dir string) (int, error)
	// Finish records a finished sync: the run, the battery and whether the watch deletes files.
	Finish(ctx context.Context, w Watch, out Outcome) error
	// History returns the watch's last n sync runs, newest first, so a restart knows when it
	// last synced and how often it failed.
	History(ctx context.Context, rec syncnode.WatchRecord, n int) ([]store.SyncRun, error)
}

// Hander is a Backend that takes each pulled file during the sync (a sync node): the watch
// flags a file as synced only once the server has it (syncnode.Options.Hand).
type Hander interface {
	Hand(ctx context.Context, w Watch, f fileaccess.StoredFile) error
}

// Inventorier is a Backend that keeps what a watch has of the types a sync takes stock of
// (syncnode.InventoryTypes: its workouts), for the workout library. A sync takes stock only for
// a backend that is one.
type Inventorier interface {
	Inventory(ctx context.Context, w Watch, inv syncnode.Inventory) error
}

// Outboxer is a Backend that has files for the watch: the workouts its owner's library wants
// on it. A sync pushes them after its pulls and deletes, and hands back what became of them.
type Outboxer interface {
	Outbox(ctx context.Context, w Watch) ([]syncnode.Outgoing, error)
	Sent(ctx context.Context, w Watch, sent []syncnode.SendOutcome, at time.Time) error
}

// Watch is how one watch is synced now.
type Watch struct {
	Addr       string
	Unit       uint32
	DeviceID   int64
	Owner      int64 // the account that owns it now (0 with NoOwner)
	Paused     bool  // scheduled syncs paused
	AfterFetch string
	// Zone is the owner's time zone, which the watch's clock follows (nil: the computer's).
	Zone *time.Location
	// NoOwner says why nobody owns it now (ErrNoOwner, or no account exists yet).
	NoOwner error
}

// Outcome is a finished sync as Finish records it.
type Outcome struct {
	Run store.SyncRun
	// Battery is the watch's last report (Percent -1 or a zero At: none).
	Battery watch.Battery
	// DeleteSupport is "yes" or "no" once a requested delete showed it, else empty.
	DeleteSupport string
}

// Local is the Backend of goarmin serve -with-sync: the server's database and ingest in this
// process.
type Local struct {
	In *ingest.Ingester
}

var _ Backend = Local{}

// Watch implements Backend.
func (l Local) Watch(ctx context.Context, addr string, rec syncnode.WatchRecord) (Watch, error) {
	st := l.In.Store
	dev, err := st.EnsureDevice(ctx, store.Device{UnitID: rec.Unit, Product: rec.Product, Name: rec.Name,
		Firmware: rec.Firmware, Address: addr})
	if err != nil {
		return Watch{}, err
	}
	w := Watch{Addr: addr, Unit: rec.Unit, DeviceID: dev.ID, Paused: dev.Paused, AfterFetch: dev.AfterFetch}
	owner, err := OwnerNow(ctx, st, dev)
	if err != nil {
		if errors.Is(err, ErrNoOwner) || owner == 0 {
			w.NoOwner = err
			return w, nil
		}
		return Watch{}, err
	}
	w.Owner = owner
	w.Zone = OwnerZone(ctx, st, owner)
	return w, nil
}

// Stored implements Backend.
func (l Local) Stored(ctx context.Context, w Watch, uid string) bool {
	ok, err := l.In.Store.HasDeviceFile(ctx, w.DeviceID, uid+".fit")
	return err == nil && ok
}

// Import implements Backend.
func (l Local) Import(ctx context.Context, w Watch, dir string) (int, error) {
	dev, err := l.In.Store.DeviceByID(ctx, w.DeviceID)
	if err != nil {
		return 0, err
	}
	rep, err := l.In.ImportWatch(ctx, dev, store.SourceBLE, dir)
	if err != nil {
		return rep.New, err
	}
	if rep.Failed > 0 {
		return rep.New, errors.New("nodehub: " + strconv.Itoa(rep.Failed) + " files could not be imported: " + rep.Errors[0])
	}
	return rep.New, nil
}

// Finish implements Backend.
func (l Local) Finish(ctx context.Context, w Watch, out Outcome) error {
	st := l.In.Store
	var errs []error
	if out.Battery.Percent >= 0 && !out.Battery.At.IsZero() && w.DeviceID != 0 {
		errs = append(errs, st.SetBattery(ctx, w.DeviceID, out.Battery.Percent, out.Battery.At))
	}
	if out.DeleteSupport != "" && w.DeviceID != 0 {
		errs = append(errs, st.SetDeleteSupport(ctx, w.DeviceID, out.DeleteSupport))
	}
	errs = append(errs, st.RecordSyncRun(ctx, out.Run))
	return errors.Join(errs...)
}

var _ Outboxer = Local{}

// Outbox implements Outboxer.
func (l Local) Outbox(ctx context.Context, w Watch) ([]syncnode.Outgoing, error) {
	if w.DeviceID == 0 || w.Owner == 0 {
		return nil, nil
	}
	files, err := outbox.Build(ctx, l.In.Store, l.In.Blobs, w.Owner, w.DeviceID)
	return Outgoing(files), err
}

// Sent implements Outboxer.
func (l Local) Sent(ctx context.Context, w Watch, sent []syncnode.SendOutcome, at time.Time) error {
	if w.DeviceID == 0 || w.Owner == 0 {
		return nil
	}
	return outbox.Record(ctx, l.In.Store, w.Owner, w.DeviceID, Results(sent), at)
}

// Outgoing is an outbox as a sync pushes it.
func Outgoing(files []outbox.File) []syncnode.Outgoing {
	var out []syncnode.Outgoing
	for _, f := range files {
		out = append(out, syncnode.Outgoing{Key: f.Key, Type: f.Type, Data: f.Data, Announce: f.Announce})
	}
	return out
}

// Results is what became of a sync's pushes, as the outbox records it.
func Results(sent []syncnode.SendOutcome) []outbox.Result {
	var out []outbox.Result
	for _, o := range sent {
		r := outbox.Result{Key: o.Key, SHA256: o.SHA256, UID: o.UID, Refused: o.Refused(), Unsupported: o.Unsupported()}
		if o.Err != nil {
			r.Err = o.Err.Error()
		}
		out = append(out, r)
	}
	return out
}

var _ Inventorier = Local{}

// Inventory implements Inventorier.
func (l Local) Inventory(ctx context.Context, w Watch, inv syncnode.Inventory) error {
	if w.DeviceID == 0 {
		return nil
	}
	return l.In.Store.SetWatchInventory(ctx, w.DeviceID, StoreInventory(inv))
}

// StoreInventory is a sync's inventory as the store keeps it.
func StoreInventory(inv syncnode.Inventory) store.WatchInventory {
	out := store.WatchInventory{Types: inv.Types, At: inv.At}
	for _, it := range inv.Items {
		out.Items = append(out.Items, store.InventoryItem{UID: it.UID.String(), Type: it.TypeName, Size: it.Size})
	}
	return out
}

// History implements Backend.
func (l Local) History(ctx context.Context, rec syncnode.WatchRecord, n int) ([]store.SyncRun, error) {
	dev, err := l.In.Store.DeviceByUnit(ctx, rec.Unit)
	if err != nil {
		return nil, err
	}
	return l.In.Store.RecentSyncRuns(ctx, dev.ID, n)
}

// PairBackend is where Add a watch learns whose a watch is and adds it to an account: the
// server's own database (Local), or on a sync node the server over HTTP. A hub pairs only with
// one. Ownership is always decided where the database is, before anything is saved.
type PairBackend interface {
	// OwnerFlags says for each listed watch whose it is now, seen from owner (Owner*), by
	// address.
	OwnerFlags(ctx context.Context, owner int64, ws []PairCandidate) (map[string]string, error)
	// Identified says whose the watch with this unit number is now, seen from owner, and the
	// current settings of one that is owner's already.
	Identified(ctx context.Context, owner int64, unit uint32) (IdentifiedOwner, error)
	// Claim adds the identified watch to owner's account with its settings and returns its
	// device id; ErrOwnedElsewhere when another account owns it now.
	Claim(ctx context.Context, c PairClaim) (int64, error)
}

// PairCandidate is a listed watch: its address, and its unit number if this computer synced it
// before (0 otherwise).
type PairCandidate struct {
	Address string `json:"address"`
	Unit    uint32 `json:"unit,omitempty"`
}

// IdentifiedOwner is whose an identified watch is: Ownership is OwnerNew, OwnerNobody,
// OwnerYours or OwnerOther; Former says another account had it before. Nickname and AfterFetch
// are the settings of a watch that is owner's already.
type IdentifiedOwner struct {
	Ownership  string `json:"ownership"`
	Former     bool   `json:"former_owner"`
	Nickname   string `json:"nickname,omitempty"`
	AfterFetch string `json:"after_fetch,omitempty"`
}

// PairClaim is the Set up step: the identified watch, for whom, with which settings.
type PairClaim struct {
	Owner      int64                `json:"owner"`
	Address    string               `json:"address"`
	Record     syncnode.WatchRecord `json:"record"`
	Battery    watch.Battery        `json:"battery"`
	Nickname   string               `json:"nickname"`
	AfterFetch string               `json:"after_fetch"`
}

var _ PairBackend = Local{}

// OwnerFlags implements PairBackend.
func (l Local) OwnerFlags(ctx context.Context, owner int64, ws []PairCandidate) (map[string]string, error) {
	st := l.In.Store
	out := make(map[string]string, len(ws))
	for _, c := range ws {
		var dev store.Device
		err := store.ErrNotFound
		if c.Unit != 0 {
			dev, err = st.DeviceByUnit(ctx, c.Unit)
		}
		if errors.Is(err, store.ErrNotFound) {
			dev, err = st.DeviceByAddress(ctx, c.Address)
		}
		if err != nil {
			out[c.Address] = OwnerNew
			continue
		}
		u, err := st.Owner(ctx, dev.ID, time.Now().Unix())
		switch {
		case errors.Is(err, store.ErrNotFound):
			out[c.Address] = OwnerNobody
		case err != nil:
			out[c.Address] = OwnerNew
		case u == owner:
			out[c.Address] = OwnerYours
		default:
			out[c.Address] = OwnerOther
		}
	}
	return out, nil
}

// Identified implements PairBackend.
func (l Local) Identified(ctx context.Context, owner int64, unit uint32) (IdentifiedOwner, error) {
	st := l.In.Store
	dev, err := st.DeviceByUnit(ctx, unit)
	if errors.Is(err, store.ErrNotFound) {
		return IdentifiedOwner{Ownership: OwnerNew}, nil
	}
	if err != nil {
		return IdentifiedOwner{}, err
	}
	former, err := st.HadOtherOwner(ctx, dev.ID, owner)
	if err != nil {
		return IdentifiedOwner{}, err
	}
	u, err := st.Owner(ctx, dev.ID, time.Now().Unix())
	switch {
	case errors.Is(err, store.ErrNotFound):
		return IdentifiedOwner{Ownership: OwnerNobody, Former: former}, nil
	case err != nil:
		return IdentifiedOwner{}, err
	case u == owner:
		return IdentifiedOwner{Ownership: OwnerYours, Former: former, Nickname: dev.Nickname, AfterFetch: dev.AfterFetch}, nil
	}
	return IdentifiedOwner{Ownership: OwnerOther, Former: former}, nil
}

// Claim implements PairBackend.
func (l Local) Claim(ctx context.Context, c PairClaim) (int64, error) {
	st := l.In.Store
	rec := c.Record
	dev, err := st.EnsureDevice(ctx, store.Device{UnitID: rec.Unit, Product: rec.Product, Name: rec.Name,
		Firmware: rec.Firmware, Address: c.Address})
	if err != nil {
		return 0, err
	}
	cur, err := st.Owner(ctx, dev.ID, time.Now().Unix()) // ownership is wall time, like the store's
	switch {
	case err == nil && cur == c.Owner:
	case err == nil:
		return 0, ErrOwnedElsewhere
	case errors.Is(err, store.ErrNotFound):
		if err := st.ClaimDevice(ctx, dev.ID, c.Owner); errors.Is(err, store.ErrOwned) {
			return 0, ErrOwnedElsewhere
		} else if err != nil {
			return 0, err
		}
	default:
		return 0, err
	}
	if err := st.SetNickname(ctx, dev.ID, c.Nickname); err != nil {
		return 0, err
	}
	if err := st.SetAfterFetch(ctx, dev.ID, c.AfterFetch); err != nil {
		return 0, err
	}
	if b := c.Battery; b.Percent >= 0 && !b.At.IsZero() {
		_ = st.SetBattery(ctx, dev.ID, b.Percent, b.At)
	}
	return dev.ID, nil
}

// OwnerNow is the device's owner now. A watch nobody ever owned goes to the only account of a
// single-person server; on a shared server, or after a hand-over, it waits to be added.
func OwnerNow(ctx context.Context, st *store.Store, dev store.Device) (int64, error) {
	if u, err := st.Owner(ctx, dev.ID, time.Now().Unix()); err == nil {
		return u, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return 0, err
	}
	sole, err := st.SoleUser(ctx)
	if err != nil {
		if ok, _ := st.CanLogIn(ctx); !ok {
			return 0, errors.New("nodehub: no account yet to give the watch to (finish the setup)")
		}
		return 0, ErrNoOwner
	}
	u, err := st.AssignIfUnowned(ctx, dev.ID, sole.ID)
	if errors.Is(err, store.ErrNotFound) {
		return 0, ErrNoOwner // it had an owner once: handed over, waiting for the next one
	}
	return u, err
}

// OwnerZone is the owner's time zone (nil: the computer's).
func OwnerZone(ctx context.Context, st *store.Store, owner int64) *time.Location {
	u, err := st.User(ctx, owner)
	if err != nil || u.Timezone == "" {
		return nil
	}
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		return nil
	}
	return loc
}
