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

// History implements Backend.
func (l Local) History(ctx context.Context, rec syncnode.WatchRecord, n int) ([]store.SyncRun, error) {
	dev, err := l.In.Store.DeviceByUnit(ctx, rec.Unit)
	if err != nil {
		return nil, err
	}
	return l.In.Store.RecentSyncRuns(ctx, dev.ID, n)
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
