// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fileaccess

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/karamble/goarmin/pkg/garmin/smart"
)

// Item is one file the watch listed.
type Item struct {
	UID smart.UUID
	// TypeName is the GDXML type, e.g. "FIT_TYPE_32", resolved through the connection's
	// gdxml_string_key table.
	TypeName      string
	Format        smart.DataFormat
	Size          uint64 // clear (inflated) size
	TransactionID uint32
	Urgency       smart.Urgency
}

// PendingItem is a listed item that is not finished yet.
type PendingItem struct {
	Item
	// Pulled means the file is stored durably and only the archive step is missing (the
	// previous run ended between the two).
	Pulled bool
}

// Outcome is how an item left the pending list.
type Outcome int

const (
	// Archived: stored, and flagged as synced on the watch (which may then delete it).
	Archived Outcome = iota + 1
	// Kept: stored, deliberately left unflagged on the watch.
	Kept
	// Gone: the watch no longer has the item.
	Gone
	// Deleted: stored, then deleted from the watch on the host's request (DeleteItemRequest).
	Deleted
)

func (o Outcome) String() string {
	switch o {
	case Archived:
		return "archived"
	case Kept:
		return "kept"
	case Gone:
		return "gone"
	case Deleted:
		return "deleted"
	}
	return "unknown"
}

// Store keeps one watch's sync state durably. Its methods may be called from several
// goroutines.
type Store interface {
	// Cursor returns the next_transaction_id of the last complete listing.
	Cursor(ctx context.Context) (next uint32, ok bool, err error)
	// SaveListing adds the listed items to the pending list and, if next is not nil, stores
	// the new cursor, in one atomic step: a crash must never leave a cursor that skips items
	// which were not recorded. Items that are pending or finished already are ignored.
	SaveListing(ctx context.Context, items []Item, next *uint32) (added int, err error)
	// Pending returns the unfinished items.
	Pending(ctx context.Context) ([]PendingItem, error)
	// Persist stores a pulled file and marks its item pulled. The data must be on stable
	// storage when it returns, because the item may be archived (and deleted by the watch)
	// right after.
	Persist(ctx context.Context, it Item, data []byte) error
	// Finish removes an item from the pending list.
	Finish(ctx context.Context, uid smart.UUID, o Outcome) error
}

// MemStore is an in-memory Store for tests and tools. It is not durable.
type MemStore struct {
	mu      sync.Mutex
	cursor  *uint32
	pending []PendingItem
	files   map[smart.UUID][]byte
	done    map[smart.UUID]Outcome
	// FailPersist, if set, is consulted before each Persist; a non-nil result fails it.
	FailPersist func(it Item) error
}

// NewMemStore returns an empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{files: map[smart.UUID][]byte{}, done: map[smart.UUID]Outcome{}}
}

// Cursor implements Store.
func (s *MemStore) Cursor(context.Context) (uint32, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cursor == nil {
		return 0, false, nil
	}
	return *s.cursor, true, nil
}

// SaveListing implements Store.
func (s *MemStore) SaveListing(_ context.Context, items []Item, next *uint32) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	added := 0
	for _, it := range items {
		if _, done := s.done[it.UID]; done {
			continue
		}
		if slices.ContainsFunc(s.pending, func(p PendingItem) bool { return p.UID == it.UID }) {
			continue
		}
		s.pending = append(s.pending, PendingItem{Item: it})
		added++
	}
	if next != nil {
		n := *next
		s.cursor = &n
	}
	return added, nil
}

// Pending implements Store.
func (s *MemStore) Pending(context.Context) ([]PendingItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.pending), nil
}

// Persist implements Store.
func (s *MemStore) Persist(_ context.Context, it Item, data []byte) error {
	if s.FailPersist != nil {
		if err := s.FailPersist(it); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := slices.IndexFunc(s.pending, func(p PendingItem) bool { return p.UID == it.UID })
	if i < 0 {
		return errors.New("fileaccess: persisting an item that is not pending")
	}
	s.files[it.UID] = slices.Clone(data)
	s.pending[i].Pulled = true
	return nil
}

// Finish implements Store.
func (s *MemStore) Finish(_ context.Context, uid smart.UUID, o Outcome) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = slices.DeleteFunc(s.pending, func(p PendingItem) bool { return p.UID == uid })
	s.done[uid] = o
	return nil
}

// File returns a stored file.
func (s *MemStore) File(uid smart.UUID) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.files[uid]
	return b, ok
}

// Stored reports whether a file of uid is stored.
func (s *MemStore) Stored(uid smart.UUID) bool {
	_, ok := s.File(uid)
	return ok
}

// Files returns the number of stored files.
func (s *MemStore) Files() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.files)
}

// Outcome returns how an item finished, if it did.
func (s *MemStore) Outcome(uid smart.UUID) (Outcome, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.done[uid]
	return o, ok
}
