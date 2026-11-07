// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package fileaccess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/karamble/goarmin/pkg/garmin/smart"
)

// DirStore is a durable Store in a directory, for one watch:
//
//	<dir>/state.json                      cursor, pending items, finished items
//	<dir>/files/<type name>/<uid>.<ext>   the pulled files (.fit for FIT data, .bin otherwise)
//
// Every write goes to a temporary file that is synced and renamed into place, then the directory
// is synced. A file is on disk before the state says it was pulled, and the state is replaced
// in one rename, so a crash leaves either the old state or the new one.
type DirStore struct {
	dir string
	now func() time.Time

	mu sync.Mutex
	st dirState
}

type dirState struct {
	Version int                  `json:"version"`
	Cursor  *uint32              `json:"cursor,omitempty"`
	Pending []dirPending         `json:"pending"`
	Done    map[string]dirRecord `json:"done"`
}

type dirItem struct {
	UID           string `json:"uid"`
	TypeName      string `json:"type"`
	Format        uint32 `json:"format"`
	Size          uint64 `json:"size"`
	TransactionID uint32 `json:"txn"`
	Urgency       uint32 `json:"urgency"`
}

type dirPending struct {
	dirItem
	Pulled bool   `json:"pulled,omitempty"`
	Path   string `json:"path,omitempty"`
}

type dirRecord struct {
	dirItem
	Outcome string `json:"outcome"`
	Path    string `json:"path,omitempty"`
	At      int64  `json:"at"` // Unix seconds
}

// StoredFile is a file in a DirStore.
type StoredFile struct {
	Item
	Path    string    // absolute
	Outcome Outcome   // 0 while it is still pending (not yet archived)
	At      time.Time // when it was finished (zero while pending)
}

// ReadDirFiles lists the files a store in dir has recorded, from its state.json, without
// opening the store for writing (a sync may be running). A dir without a store has none.
func ReadDirFiles(dir string) ([]StoredFile, error) {
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s := &DirStore{dir: dir}
	if err := json.Unmarshal(b, &s.st); err != nil {
		return nil, fmt.Errorf("fileaccess: %s: %w", filepath.Join(dir, "state.json"), err)
	}
	return s.Files()
}

// OpenDirStore opens or creates the store in dir.
func OpenDirStore(dir string) (*DirStore, error) {
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o700); err != nil {
		return nil, err
	}
	s := &DirStore{dir: dir, now: time.Now, st: dirState{Version: 1, Done: map[string]dirRecord{}}}
	b, err := os.ReadFile(filepath.Join(dir, "state.json"))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &s.st); err != nil {
			return nil, fmt.Errorf("fileaccess: %s: %w", filepath.Join(dir, "state.json"), err)
		}
		if s.st.Done == nil {
			s.st.Done = map[string]dirRecord{}
		}
	}
	return s, nil
}

// Dir returns the store's directory.
func (s *DirStore) Dir() string { return s.dir }

func toDir(it Item) dirItem {
	return dirItem{UID: it.UID.String(), TypeName: it.TypeName, Format: uint32(it.Format), Size: it.Size,
		TransactionID: it.TransactionID, Urgency: uint32(it.Urgency)}
}

func (d dirItem) item() (Item, error) {
	uid, err := ParseUID(d.UID)
	if err != nil {
		return Item{}, err
	}
	return Item{UID: uid, TypeName: d.TypeName, Format: smart.DataFormat(d.Format), Size: d.Size,
		TransactionID: d.TransactionID, Urgency: smart.Urgency(d.Urgency)}, nil
}

// ParseUID reads smart.UUID.String()'s form, "<msb hex>-<lsb hex>", as the spool's file
// names and the sync output show it.
func ParseUID(s string) (smart.UUID, error) {
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return smart.UUID{}, fmt.Errorf("fileaccess: bad uid %q", s)
	}
	msb, err1 := strconv.ParseUint(a, 16, 64)
	lsb, err2 := strconv.ParseUint(b, 16, 64)
	if err1 != nil || err2 != nil {
		return smart.UUID{}, fmt.Errorf("fileaccess: bad uid %q", s)
	}
	return smart.UUID{MSB: msb, LSB: lsb}, nil
}

// Cursor implements Store.
func (s *DirStore) Cursor(context.Context) (uint32, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.Cursor == nil {
		return 0, false, nil
	}
	return *s.st.Cursor, true, nil
}

// SaveListing implements Store.
func (s *DirStore) SaveListing(_ context.Context, items []Item, next *uint32) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.cloneLocked()
	added := 0
	for _, it := range items {
		uid := it.UID.String()
		if _, done := st.Done[uid]; done {
			continue
		}
		if slices.ContainsFunc(st.Pending, func(p dirPending) bool { return p.UID == uid }) {
			continue
		}
		st.Pending = append(st.Pending, dirPending{dirItem: toDir(it)})
		added++
	}
	if next != nil {
		n := *next
		st.Cursor = &n
	}
	if added == 0 && next == nil {
		return 0, nil
	}
	return added, s.commitLocked(st)
}

// Pending implements Store.
func (s *DirStore) Pending(context.Context) ([]PendingItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PendingItem, 0, len(s.st.Pending))
	for _, p := range s.st.Pending {
		it, err := p.item()
		if err != nil {
			return nil, err
		}
		out = append(out, PendingItem{Item: it, Pulled: p.Pulled})
	}
	return out, nil
}

// Persist implements Store: the file first, synced, then the state.
func (s *DirStore) Persist(_ context.Context, it Item, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	uid := it.UID.String()
	i := slices.IndexFunc(s.st.Pending, func(p dirPending) bool { return p.UID == uid })
	if i < 0 {
		return errors.New("fileaccess: persisting an item that is not pending")
	}
	ext := ".bin"
	if len(data) >= 12 && string(data[8:12]) == ".FIT" {
		ext = ".fit"
	}
	typ := it.TypeName
	if typ == "" || strings.ContainsAny(typ, `/\`) || strings.HasPrefix(typ, ".") {
		typ = "unknown"
	}
	rel := filepath.Join("files", typ, uid+ext)
	if err := writeAtomic(filepath.Join(s.dir, rel), data); err != nil {
		return err
	}
	st := s.cloneLocked()
	st.Pending[i].Pulled, st.Pending[i].Path = true, rel
	return s.commitLocked(st)
}

// Finish implements Store.
func (s *DirStore) Finish(_ context.Context, uid smart.UUID, o Outcome) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := uid.String()
	st := s.cloneLocked()
	rec := dirRecord{dirItem: dirItem{UID: key}, Outcome: o.String(), At: s.now().Unix()}
	if i := slices.IndexFunc(st.Pending, func(p dirPending) bool { return p.UID == key }); i >= 0 {
		rec.dirItem, rec.Path = st.Pending[i].dirItem, st.Pending[i].Path
		st.Pending = slices.Delete(st.Pending, i, i+1)
	} else if old, ok := st.Done[key]; ok {
		// A finished item finishing again (deleted after it was kept): keep what is known.
		rec.dirItem, rec.Path = old.dirItem, old.Path
	}
	st.Done[key] = rec
	return s.commitLocked(st)
}

// Stored reports whether the store holds a file of uid on disk (pending or finished).
func (s *DirStore) Stored(uid smart.UUID) bool {
	s.mu.Lock()
	key := uid.String()
	path := ""
	if i := slices.IndexFunc(s.st.Pending, func(p dirPending) bool { return p.UID == key }); i >= 0 {
		path = s.st.Pending[i].Path
	} else if r, ok := s.st.Done[key]; ok {
		path = r.Path
	}
	s.mu.Unlock()
	if path == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(s.dir, path))
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// Files lists every stored file, finished or still waiting to be archived.
func (s *DirStore) Files() ([]StoredFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []StoredFile
	add := func(d dirItem, path string, o Outcome, at int64) error {
		if path == "" {
			return nil
		}
		it, err := d.item()
		if err != nil {
			return err
		}
		f := StoredFile{Item: it, Path: filepath.Join(s.dir, path), Outcome: o}
		if at != 0 {
			f.At = time.Unix(at, 0)
		}
		out = append(out, f)
		return nil
	}
	for _, p := range s.st.Pending {
		if err := add(p.dirItem, p.Path, 0, 0); err != nil {
			return nil, err
		}
	}
	for _, r := range s.st.Done {
		o := map[string]Outcome{"archived": Archived, "kept": Kept, "gone": Gone, "deleted": Deleted}[r.Outcome]
		if err := add(r.dirItem, r.Path, o, r.At); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(out, func(a, b StoredFile) int { return strings.Compare(a.Path, b.Path) })
	return out, nil
}

func (s *DirStore) cloneLocked() dirState {
	st := s.st
	st.Pending = slices.Clone(s.st.Pending)
	st.Done = make(map[string]dirRecord, len(s.st.Done)+1)
	for k, v := range s.st.Done {
		st.Done[k] = v
	}
	return st
}

// commitLocked writes st and makes it current only once it is on disk.
func (s *DirStore) commitLocked(st dirState) error {
	b, err := json.MarshalIndent(st, "", " ")
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(s.dir, "state.json"), b); err != nil {
		return err
	}
	s.st = st
	return nil
}

// writeAtomic writes data to path through a synced temporary file and a rename, then syncs the
// directory so the rename itself is durable.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // a no-op after the rename
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
